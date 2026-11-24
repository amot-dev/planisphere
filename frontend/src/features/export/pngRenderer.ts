import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { ExportBlockRenderer } from './blocks/ExportBlockRenderer'
import { EXPORT_CARD_CSS, EXPORT_CARD_WIDTH } from './exportCardStyles'
import {
  CartBlockItem,
  ExpeditionExportData,
  ExportBundleFile,
} from './exportTypes'

const PNG_SCALE_FACTOR = 2
const IMAGES_SUBFOLDER = 'images'
const PNG_DATA_URI_PREFIX = 'data:image/png;base64,'

/**
 * Wraps serialized XHTML card markup and EXPORT_CARD_CSS inside an SVG
 * foreignObject document sized to width by height. Uses data:image/svg+xml
 * encoding (required by Linux WebKitGTK 4.1 to avoid canvas tainting on blob URLs).
 */
export function buildSvgForeignObjectDocument(
  serializedXhtml: string,
  width: number,
  height: number,
): string {
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}">` +
    `<foreignObject width="100%" height="100%">` +
    `<div xmlns="http://www.w3.org/1999/xhtml" style="width:${width}px;background:#050506;color:#f4ece4;">` +
    `<style><![CDATA[${EXPORT_CARD_CSS}]]></style>` +
    serializedXhtml +
    `</div></foreignObject></svg>`
  )
}

/**
 * Resolves the target bundle path inside images/ with dynamically zero-padded
 * ordering for a queued cart item at oneBasedIndex.
 */
export function buildPngBundleFileName(
  item: CartBlockItem,
  oneBasedIndex: number,
  totalPngCount: number,
): string {
  const padWidth = Math.max(2, String(Math.max(1, totalPngCount)).length)
  const numericPrefix = String(oneBasedIndex).padStart(padWidth, '0')
  return `${IMAGES_SUBFOLDER}/${numericPrefix}-${item.filenameSlug}.png`
}

/**
 * Renders a single CartBlockItem to a crisp 2x DPI PNG base64 string (without
 * the data-URI prefix) using a shared offscreen DOM and canvas session helper.
 */
export async function renderCartBlockToPngBase64(
  exportData: ExpeditionExportData,
  item: CartBlockItem,
): Promise<string> {
  return runWithPngRenderSession(exportData, (renderCard) => renderCard(item))
}

/**
 * Renders all queued cart items sequentially to 2x DPI PNG ExportBundleFile entries
 * using one shared offscreen host, stylesheet, XMLSerializer, and canvas across the batch.
 */
export async function renderCartToPngFiles(
  exportData: ExpeditionExportData,
  cartItems: CartBlockItem[],
  onProgress?: (completedCount: number, totalCount: number) => void,
): Promise<ExportBundleFile[]> {
  if (cartItems.length === 0) return []
  return runWithPngRenderSession(exportData, async (renderCard) => {
    const outputFiles: ExportBundleFile[] = []
    for (let index = 0; index < cartItems.length; index++) {
      const item = cartItems[index]
      const pngBase64 = await renderCard(item)
      outputFiles.push({
        name: buildPngBundleFileName(item, index + 1, cartItems.length),
        contentBase64: pngBase64,
      })
      onProgress?.(index + 1, cartItems.length)
    }
    return outputFiles
  })
}

interface BatchDomResources {
  host: HTMLDivElement
  parserElement: HTMLDivElement
  xmlSerializer: XMLSerializer
  canvas: HTMLCanvasElement
  mountedCardElement: HTMLElement | null
}

async function runWithPngRenderSession<T>(
  exportData: ExpeditionExportData,
  work: (renderCard: (item: CartBlockItem) => Promise<string>) => Promise<T>,
): Promise<T> {
  const resources = createBatchDomResources()
  document.body.appendChild(resources.host)
  try {
    return await work(async (item) => {
      const staticMarkup = renderToStaticMarkup(
        createElement(ExportBlockRenderer, { exportData, config: item.config }),
      )
      const { serializedXhtml, width, height } = measureAndSerializeCardInHost(
        resources,
        staticMarkup,
      )
      const svgMarkup = buildSvgForeignObjectDocument(serializedXhtml, width, height)
      return rasterizeSvgOntoSharedCanvas(
        resources.canvas,
        svgMarkup,
        width,
        height,
        PNG_SCALE_FACTOR,
      )
    })
  } finally {
    document.body.removeChild(resources.host)
  }
}

function createBatchDomResources(): BatchDomResources {
  const host = document.createElement('div')
  host.style.position = 'fixed'
  host.style.left = '-100000px'
  host.style.top = '0'
  host.style.width = `${EXPORT_CARD_WIDTH}px`
  host.style.pointerEvents = 'none'
  host.style.visibility = 'hidden'

  const styleElement = document.createElement('style')
  styleElement.textContent = EXPORT_CARD_CSS
  host.appendChild(styleElement)

  return {
    host,
    parserElement: document.createElement('div'),
    xmlSerializer: new XMLSerializer(),
    canvas: document.createElement('canvas'),
    mountedCardElement: null,
  }
}

function measureAndSerializeCardInHost(
  resources: BatchDomResources,
  staticMarkup: string,
): {
  serializedXhtml: string
  width: number
  height: number
} {
  resources.parserElement.innerHTML = staticMarkup
  const cardElement = resources.parserElement.firstElementChild as HTMLElement | null
  if (!cardElement) {
    throw new Error('Export card rendered empty markup')
  }
  if (resources.mountedCardElement) {
    resources.host.replaceChild(cardElement, resources.mountedCardElement)
  } else {
    resources.host.appendChild(cardElement)
  }
  resources.mountedCardElement = cardElement

  const bounds = cardElement.getBoundingClientRect()
  const width = Math.max(EXPORT_CARD_WIDTH, Math.ceil(bounds.width || EXPORT_CARD_WIDTH))
  const height = Math.max(120, Math.ceil(bounds.height || cardElement.scrollHeight || 200))
  cardElement.setAttribute('xmlns', 'http://www.w3.org/1999/xhtml')
  const serializedXhtml = resources.xmlSerializer.serializeToString(cardElement)
  return { serializedXhtml, width, height }
}

async function rasterizeSvgOntoSharedCanvas(
  canvas: HTMLCanvasElement,
  svgMarkup: string,
  width: number,
  height: number,
  scaleFactor: number,
): Promise<string> {
  const image = new Image()
  const svgDataUri = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svgMarkup)}`
  await new Promise<void>((resolve, reject) => {
    image.onload = () => resolve()
    image.onerror = () => reject(new Error('Failed to load SVG foreignObject image for PNG capture'))
    image.src = svgDataUri
  })

  // Resizing canvas dimensions automatically resets the 2D rendering context state and transform.
  canvas.width = width * scaleFactor
  canvas.height = height * scaleFactor
  const renderingContext = canvas.getContext('2d')
  if (!renderingContext) {
    throw new Error('2D canvas rendering context is unavailable')
  }
  renderingContext.scale(scaleFactor, scaleFactor)
  renderingContext.fillStyle = '#050506'
  renderingContext.fillRect(0, 0, width, height)
  renderingContext.drawImage(image, 0, 0, width, height)

  const pngDataUri = canvas.toDataURL('image/png')
  const base64Payload = pngDataUri.startsWith(PNG_DATA_URI_PREFIX)
    ? pngDataUri.slice(PNG_DATA_URI_PREFIX.length)
    : ''
  if (base64Payload.length === 0) {
    throw new Error('Canvas PNG export failed or exceeded maximum dimensions')
  }
  return base64Payload
}

