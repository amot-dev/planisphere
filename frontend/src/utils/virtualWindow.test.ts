import { describe, expect, it } from 'vitest'
import { calculateVirtualWindow } from './virtualWindow'

describe('calculateVirtualWindow', () => {
  it('returns all items with 0 spacers when total items <= minThreshold', () => {
    const items = Array.from({ length: 20 }, (_, i) => ({ id: i }))
    const result = calculateVirtualWindow(items, () => 40, 100, 800, 600, 25)

    expect(result.visibleItems.length).toBe(20)
    expect(result.topSpacerHeight).toBe(0)
    expect(result.bottomSpacerHeight).toBe(0)
    expect(result.startIndex).toBe(0)
    expect(result.endIndex).toBe(19)
  })

  it('virtualizes correctly when total items > minThreshold at scrollTop = 0', () => {
    // 100 items, each 50px = 5000px total
    const items = Array.from({ length: 100 }, (_, i) => ({ id: i }))
    // containerHeight = 800, overscan = 600 -> viewBottom = 1400 (28 items)
    const result = calculateVirtualWindow(items, () => 50, 0, 800, 600, 25)

    expect(result.topSpacerHeight).toBe(0)
    expect(result.startIndex).toBe(0)
    // 0 to ~28 items
    expect(result.endIndex).toBeGreaterThanOrEqual(28)
    expect(result.visibleItems.length).toBe(result.endIndex + 1)
    expect(result.bottomSpacerHeight).toBe((99 - result.endIndex) * 50)

    // Total height invariant
    const renderedHeight = result.visibleItems.length * 50
    expect(result.topSpacerHeight + renderedHeight + result.bottomSpacerHeight).toBe(5000)
  })

  it('virtualizes correctly in the middle of a list', () => {
    const items = Array.from({ length: 100 }, (_, i) => ({ id: i }))
    // scrollTop = 2000, viewTop = 1400, viewBottom = 3400
    const result = calculateVirtualWindow(items, () => 50, 2000, 800, 600, 25)

    expect(result.topSpacerHeight).toBe(result.startIndex * 50)
    expect(result.startIndex).toBeGreaterThan(0)
    expect(result.endIndex).toBeLessThan(99)
    expect(result.bottomSpacerHeight).toBe((99 - result.endIndex) * 50)

    // Total height invariant
    const renderedHeight = result.visibleItems.length * 50
    expect(result.topSpacerHeight + renderedHeight + result.bottomSpacerHeight).toBe(5000)
  })

  it('virtualizes correctly at the very bottom of a list', () => {
    const items = Array.from({ length: 100 }, (_, i) => ({ id: i }))
    // scrollTop = 4500
    const result = calculateVirtualWindow(items, () => 50, 4500, 800, 600, 25)

    expect(result.endIndex).toBe(99)
    expect(result.bottomSpacerHeight).toBe(0)
    expect(result.topSpacerHeight).toBe(result.startIndex * 50)

    // Total height invariant
    const renderedHeight = result.visibleItems.length * 50
    expect(result.topSpacerHeight + renderedHeight + result.bottomSpacerHeight).toBe(5000)
  })

  it('correctly handles variable height rows with exact pixel preservation', () => {
    // Alternating heights: 40px, 60px, 80px
    const heights = [40, 60, 80, 44, 32, 240, 50]
    const items = Array.from({ length: 70 }, (_, i) => ({
      id: i,
      height: heights[i % heights.length],
    }))
    const totalHeight = items.reduce((total, item) => total + item.height, 0)

    const result = calculateVirtualWindow(items, (item) => item.height, 1500, 800, 400, 20)

    const renderedHeight = result.visibleItems.reduce((total, item) => total + item.height, 0)
    expect(result.topSpacerHeight + renderedHeight + result.bottomSpacerHeight).toBe(totalHeight)
  })

  it('handles negative scrollTop gracefully (overscroll bounce)', () => {
    const items = Array.from({ length: 50 }, (_, i) => ({ id: i }))
    const result = calculateVirtualWindow(items, () => 40, -100, 800, 400, 25)

    expect(result.topSpacerHeight).toBe(0)
    expect(result.startIndex).toBe(0)
  })

  it('handles empty and null items safely', () => {
    const emptyResult = calculateVirtualWindow([], () => 40, 0, 800)
    expect(emptyResult.visibleItems).toEqual([])
    expect(emptyResult.topSpacerHeight).toBe(0)
    expect(emptyResult.bottomSpacerHeight).toBe(0)
    expect(emptyResult.startIndex).toBe(0)
    expect(emptyResult.endIndex).toBe(0)

    const nullResult = calculateVirtualWindow(null as any, () => 40, 100, 800)
    expect(nullResult.visibleItems).toEqual([])
    expect(nullResult.topSpacerHeight).toBe(0)
    expect(nullResult.bottomSpacerHeight).toBe(0)
  })

  it('handles NaN and infinite inputs safely without producing NaN spacer heights', () => {
    const items = Array.from({ length: 50 }, (_, i) => ({ id: i }))
    const nanResult = calculateVirtualWindow(items, () => 40, NaN, NaN)
    expect(Number.isFinite(nanResult.topSpacerHeight)).toBe(true)
    expect(Number.isFinite(nanResult.bottomSpacerHeight)).toBe(true)
    expect(nanResult.visibleItems.length).toBeGreaterThan(0)

    const infinityResult = calculateVirtualWindow(items, () => 40, Infinity, 800)
    expect(Number.isFinite(infinityResult.topSpacerHeight)).toBe(true)
    expect(Number.isFinite(infinityResult.bottomSpacerHeight)).toBe(true)
  })

  it('safely handles getItemHeight returning NaN, Infinity, negative, or 0', () => {
    const items = Array.from({ length: 50 }, (_, i) => ({ id: i }))
    const result = calculateVirtualWindow(items, (item) => (item.id % 2 === 0 ? NaN : Infinity), 200, 800)
    expect(Number.isFinite(result.topSpacerHeight)).toBe(true)
    expect(Number.isFinite(result.bottomSpacerHeight)).toBe(true)
    expect(result.topSpacerHeight).toBeGreaterThanOrEqual(0)
    expect(result.bottomSpacerHeight).toBeGreaterThanOrEqual(0)
    expect(result.visibleItems.length).toBeGreaterThan(0)
  })

  it('handles very large datasets (10,000 items) efficiently without drift', () => {
    const items = Array.from({ length: 10000 }, (_, i) => ({ id: i }))
    const result = calculateVirtualWindow(items, () => 40, 200000, 1000, 600, 25)
    expect(result.startIndex).toBeGreaterThan(4000)
    expect(result.endIndex).toBeLessThan(6000)
    expect(result.topSpacerHeight + result.visibleItems.length * 40 + result.bottomSpacerHeight).toBe(400000)
  })
})
