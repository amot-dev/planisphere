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
    `<style>${EXPORT_CARD_CSS}</style>` +
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
 * the data-URI prefix) using an offscreen DOM measurement pass and SVG
 * foreignObject canvas capture.
 */
export async function renderCartBlockToPngBase64(
  exportData: ExpeditionExportData,
  item: CartBlockItem,
): Promise<string> {
  const staticMarkup = renderToStaticMarkup(
    createElement(ExportBlockRenderer, {
      exportData,
      config: item.config,
    }),
  )
  const { serializedXhtml, width, height } = measureAndSerializeCard(staticMarkup)
  const svgMarkup = buildSvgForeignObjectDocument(serializedXhtml, width, height)
  return rasterizeSvgToPngBase64(svgMarkup, width, height, PNG_SCALE_FACTOR)
}

/**
 * Renders all queued cart items sequentially to 2x DPI PNG ExportBundleFile entries,
 * reporting progress after each completed card.
 */
export async function renderCartToPngFiles(
  exportData: ExpeditionExportData,
  cartItems: CartBlockItem[],
  onProgress?: (completedCount: number, totalCount: number) => void,
): Promise<ExportBundleFile[]> {
  const outputFiles: ExportBundleFile[] = []
  for (let index = 0; index < cartItems.length; index++) {
    const item = cartItems[index]
    const pngBase64 = await renderCartBlockToPngBase64(exportData, item)
    outputFiles.push({
      name: buildPngBundleFileName(item, index + 1, cartItems.length),
      contentBase64: pngBase64,
    })
    onProgress?.(index + 1, cartItems.length)
  }
  return outputFiles
}

function measureAndSerializeCard(staticMarkup: string): {
  serializedXhtml: string
  width: number
  height: number
} {
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

  const wrapper = document.createElement('div')
  wrapper.innerHTML = staticMarkup
  const cardElement = wrapper.firstElementChild as HTMLElement | null
  if (!cardElement) {
    throw new Error('Export card rendered empty markup')
  }
  host.appendChild(cardElement)
  document.body.appendChild(host)

  try {
    const bounds = cardElement.getBoundingClientRect()
    const width = Math.max(EXPORT_CARD_WIDTH, Math.ceil(bounds.width || EXPORT_CARD_WIDTH))
    const height = Math.max(120, Math.ceil(bounds.height || cardElement.scrollHeight || 200))
    cardElement.setAttribute('xmlns', 'http://www.w3.org/1999/xhtml')
    const serializedXhtml = new XMLSerializer().serializeToString(cardElement)
    return { serializedXhtml, width, height }
  } finally {
    document.body.removeChild(host)
  }
}

async function rasterizeSvgToPngBase64(
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

  const canvas = document.createElement('canvas')
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
  const commaIndex = pngDataUri.indexOf(',')
  return commaIndex >= 0 ? pngDataUri.slice(commaIndex + 1) : pngDataUri
}

