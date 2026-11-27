// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { installCanvasPngMock } from '../../test/domMocks'
import { buildSampleExportData } from '../../test/fixtures/exportFixtures'
import { EXPORT_CARD_CSS, EXPORT_CARD_WIDTH } from './exportCardStyles'
import { createQuickShareCart, expandConfigToCartItems } from './exportMetrics'
import {
  buildPngBundleFileName,
  buildSvgForeignObjectDocument,
  renderCartToPngFiles,
} from './pngRenderer'

describe('pngRenderer', () => {
  it('builds valid XHTML-namespaced SVG foreignObject documents, wraps CSS in CDATA, and rejects truncated canvas PNG payloads', async () => {
    const svgDoc = buildSvgForeignObjectDocument(
      '<div class="ed-exp-root">OK</div>',
      EXPORT_CARD_WIDTH,
      320,
    )
    expect(svgDoc).toContain('xmlns="http://www.w3.org/2000/svg"')
    expect(svgDoc).toContain('xmlns="http://www.w3.org/1999/xhtml"')
    expect(svgDoc).toContain('width="960" height="320"')
    expect(svgDoc).toContain(`<style><![CDATA[${EXPORT_CARD_CSS}]]></style>`)

    const data = buildSampleExportData()
    const quickItems = createQuickShareCart(data)
    expect(buildPngBundleFileName(quickItems[0], 1, 3)).toBe('images/01-full-expedition-summary.png')

    const legItems = expandConfigToCartItems(
      data,
      {
        kind: 'summary',
        scope: { kind: 'allLegs' },
        modeGrouping: 'combined',
        careerDiff: 'hide',
      },
      'leg',
    )
    expect(buildPngBundleFileName(legItems[0], 1, 223)).toBe('images/001-wp2-summary.png')
    expect(buildPngBundleFileName(legItems[0], 115, 223)).toBe('images/115-wp2-summary.png')

    const canvasMock = installCanvasPngMock()
    canvasMock.toDataURLSpy.mockReturnValueOnce('data:,')
    await expect(renderCartToPngFiles(data, [quickItems[0]])).rejects.toThrow(
      'Canvas PNG export failed or exceeded maximum dimensions',
    )

    canvasMock.toDataURLSpy.mockReturnValueOnce('data:image/png;base64,')
    await expect(renderCartToPngFiles(data, [quickItems[0]])).rejects.toThrow(
      'Canvas PNG export failed or exceeded maximum dimensions',
    )

    canvasMock.toDataURLSpy.mockReturnValueOnce('data:image/png;base64,iVBORw0KGgo=')
    await expect(renderCartToPngFiles(data, [quickItems[0]])).resolves.toEqual([
      {
        name: 'images/01-full-expedition-summary.png',
        contentBase64: 'iVBORw0KGgo=',
      },
    ])
  })

  it('reuses a single offscreen host and canvas across a multi-card PNG batch and cleans up on failure', async () => {
    const data = buildSampleExportData()
    const quickItems = createQuickShareCart(data)
    expect(quickItems).toHaveLength(3)

    let failOnImageIndex = -1
    const canvasMock = installCanvasPngMock({
      shouldFailImageAt: (imageIndex) => imageIndex === failOnImageIndex,
    })

    const originalCreateElement = document.createElement.bind(document)
    let createdCanvasCount = 0
    const createElementSpy = vi
      .spyOn(document, 'createElement')
      .mockImplementation(((tagName: string, options?: ElementCreationOptions) => {
        if (tagName.toLowerCase() === 'canvas') createdCanvasCount++
        return originalCreateElement(tagName, options)
      }) as typeof document.createElement)
    const appendChildSpy = vi.spyOn(document.body, 'appendChild')
    const removeChildSpy = vi.spyOn(document.body, 'removeChild')

    try {
      const progressTicks: Array<[number, number]> = []
      const renderedFiles = await renderCartToPngFiles(
        data,
        quickItems,
        (completedCount, totalCount) => progressTicks.push([completedCount, totalCount]),
      )
      expect(renderedFiles.map((file) => file.name)).toEqual([
        'images/01-full-expedition-summary.png',
        'images/02-highlights-top-1.png',
        'images/03-waypoints-visited-only.png',
      ])
      expect(progressTicks).toEqual([
        [1, 3],
        [2, 3],
        [3, 3],
      ])
      expect(appendChildSpy).toHaveBeenCalledTimes(1)
      expect(removeChildSpy).toHaveBeenCalledTimes(1)
      expect(createdCanvasCount).toBe(1)
      const firstBatchHost = appendChildSpy.mock.calls[0][0] as HTMLElement
      expect(document.body.contains(firstBatchHost)).toBe(false)

      // Mid-batch rasterization failure on card 2 must still remove the single host in finally.
      appendChildSpy.mockClear()
      removeChildSpy.mockClear()
      createdCanvasCount = 0
      canvasMock.resetImageCounter()
      failOnImageIndex = 1

      await expect(renderCartToPngFiles(data, quickItems)).rejects.toThrow(
        'Failed to load SVG foreignObject image for PNG capture',
      )
      expect(appendChildSpy).toHaveBeenCalledTimes(1)
      expect(removeChildSpy).toHaveBeenCalledTimes(1)
      expect(createdCanvasCount).toBe(1)
      const failedBatchHost = appendChildSpy.mock.calls[0][0] as HTMLElement
      expect(document.body.contains(failedBatchHost)).toBe(false)
    } finally {
      createElementSpy.mockRestore()
      appendChildSpy.mockRestore()
      removeChildSpy.mockRestore()
    }
  })
})
