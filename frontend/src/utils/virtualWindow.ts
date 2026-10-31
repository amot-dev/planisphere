/**
 * Calculates a windowed slice and top/bottom spacer heights for virtualized lists/tables.
 * Ensures the sum of topSpacerHeight + rendered heights + bottomSpacerHeight exactly equals
 * the total height of all items, preventing scrollbar thumb jumping and scroll drift.
 */
export function calculateVirtualWindow<T>(
  items: T[],
  getItemHeight: (item: T) => number,
  scrollTop: number,
  containerHeight: number,
  overscanPx = 600,
  minThreshold = 25,
): {
  visibleItems: T[]
  startIndex: number
  endIndex: number
  topSpacerHeight: number
  bottomSpacerHeight: number
} {
  const total = items?.length || 0
  if (total <= minThreshold) {
    return {
      visibleItems: items ? [...items] : [],
      startIndex: 0,
      endIndex: Math.max(0, total - 1),
      topSpacerHeight: 0,
      bottomSpacerHeight: 0,
    }
  }

  const safeScrollTop = Math.max(0, Number.isFinite(scrollTop) ? scrollTop : 0)
  const safeContainerHeight = Math.max(0, Number.isFinite(containerHeight) ? containerHeight : 800)
  const viewTop = Math.max(0, safeScrollTop - overscanPx)
  const viewBottom = safeScrollTop + safeContainerHeight + overscanPx

  let accY = 0
  let startIdx = 0
  let endIdx = total - 1
  let foundStart = false

  const heights = new Array<number>(total)

  for (let i = 0; i < total; i++) {
    const rawH = getItemHeight(items[i])
    const h = Math.max(1, Number.isFinite(rawH) ? rawH : 44)
    heights[i] = h
    const nextY = accY + h

    if (!foundStart && nextY >= viewTop) {
      startIdx = i
      foundStart = true
    }
    if (foundStart && accY <= viewBottom) {
      endIdx = i
    }
    accY = nextY
  }

  endIdx = Math.min(total - 1, endIdx + 1)
  if (endIdx < startIdx) {
    endIdx = Math.min(total - 1, startIdx + 15)
  }

  let topSpacer = 0
  for (let i = 0; i < startIdx; i++) {
    topSpacer += heights[i]
  }

  let bottomSpacer = 0
  for (let i = endIdx + 1; i < total; i++) {
    bottomSpacer += heights[i]
  }

  return {
    visibleItems: items.slice(startIdx, endIdx + 1),
    startIndex: startIdx,
    endIndex: endIdx,
    topSpacerHeight: Math.max(0, Math.round(topSpacer)),
    bottomSpacerHeight: Math.max(0, Math.round(bottomSpacer)),
  }
}
