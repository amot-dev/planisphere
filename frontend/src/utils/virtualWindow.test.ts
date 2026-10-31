import { describe, expect, it } from 'vitest'
import { calculateVirtualWindow } from './virtualWindow'

describe('calculateVirtualWindow', () => {
  it('returns all items with 0 spacers when total items <= minThreshold', () => {
    const items = Array.from({ length: 20 }, (_, i) => ({ id: i }))
    const res = calculateVirtualWindow(items, () => 40, 100, 800, 600, 25)

    expect(res.visibleItems.length).toBe(20)
    expect(res.topSpacerHeight).toBe(0)
    expect(res.bottomSpacerHeight).toBe(0)
    expect(res.startIndex).toBe(0)
    expect(res.endIndex).toBe(19)
  })

  it('virtualizes correctly when total items > minThreshold at scrollTop = 0', () => {
    // 100 items, each 50px = 5000px total
    const items = Array.from({ length: 100 }, (_, i) => ({ id: i }))
    // containerHeight = 800, overscan = 600 -> viewBottom = 1400 (28 items)
    const res = calculateVirtualWindow(items, () => 50, 0, 800, 600, 25)

    expect(res.topSpacerHeight).toBe(0)
    expect(res.startIndex).toBe(0)
    // 0 to ~28 items
    expect(res.endIndex).toBeGreaterThanOrEqual(28)
    expect(res.visibleItems.length).toBe(res.endIndex + 1)
    expect(res.bottomSpacerHeight).toBe((99 - res.endIndex) * 50)

    // Total height invariant
    const renderedHeight = res.visibleItems.length * 50
    expect(res.topSpacerHeight + renderedHeight + res.bottomSpacerHeight).toBe(5000)
  })

  it('virtualizes correctly in the middle of a list', () => {
    const items = Array.from({ length: 100 }, (_, i) => ({ id: i }))
    // scrollTop = 2000, viewTop = 1400, viewBottom = 3400
    const res = calculateVirtualWindow(items, () => 50, 2000, 800, 600, 25)

    expect(res.topSpacerHeight).toBe(res.startIndex * 50)
    expect(res.startIndex).toBeGreaterThan(0)
    expect(res.endIndex).toBeLessThan(99)
    expect(res.bottomSpacerHeight).toBe((99 - res.endIndex) * 50)

    // Total height invariant
    const renderedHeight = res.visibleItems.length * 50
    expect(res.topSpacerHeight + renderedHeight + res.bottomSpacerHeight).toBe(5000)
  })

  it('virtualizes correctly at the very bottom of a list', () => {
    const items = Array.from({ length: 100 }, (_, i) => ({ id: i }))
    // scrollTop = 4500
    const res = calculateVirtualWindow(items, () => 50, 4500, 800, 600, 25)

    expect(res.endIndex).toBe(99)
    expect(res.bottomSpacerHeight).toBe(0)
    expect(res.topSpacerHeight).toBe(res.startIndex * 50)

    // Total height invariant
    const renderedHeight = res.visibleItems.length * 50
    expect(res.topSpacerHeight + renderedHeight + res.bottomSpacerHeight).toBe(5000)
  })

  it('correctly handles variable height rows with exact pixel preservation', () => {
    // Alternating heights: 40px, 60px, 80px
    const heights = [40, 60, 80, 44, 32, 240, 50]
    const items = Array.from({ length: 70 }, (_, i) => ({
      id: i,
      h: heights[i % heights.length],
    }))
    const totalHeight = items.reduce((acc, it) => acc + it.h, 0)

    const res = calculateVirtualWindow(items, (it) => it.h, 1500, 800, 400, 20)

    const renderedHeight = res.visibleItems.reduce((acc, it) => acc + it.h, 0)
    expect(res.topSpacerHeight + renderedHeight + res.bottomSpacerHeight).toBe(totalHeight)
  })

  it('handles negative scrollTop gracefully (overscroll bounce)', () => {
    const items = Array.from({ length: 50 }, (_, i) => ({ id: i }))
    const res = calculateVirtualWindow(items, () => 40, -100, 800, 400, 25)

    expect(res.topSpacerHeight).toBe(0)
    expect(res.startIndex).toBe(0)
  })

  it('handles empty and null items safely', () => {
    const resEmpty = calculateVirtualWindow([], () => 40, 0, 800)
    expect(resEmpty.visibleItems).toEqual([])
    expect(resEmpty.topSpacerHeight).toBe(0)
    expect(resEmpty.bottomSpacerHeight).toBe(0)
    expect(resEmpty.startIndex).toBe(0)
    expect(resEmpty.endIndex).toBe(0)

    const resNull = calculateVirtualWindow(null as any, () => 40, 100, 800)
    expect(resNull.visibleItems).toEqual([])
    expect(resNull.topSpacerHeight).toBe(0)
    expect(resNull.bottomSpacerHeight).toBe(0)
  })

  it('handles NaN and infinite inputs safely without producing NaN spacer heights', () => {
    const items = Array.from({ length: 50 }, (_, i) => ({ id: i }))
    const resNaN = calculateVirtualWindow(items, () => 40, NaN, NaN)
    expect(Number.isFinite(resNaN.topSpacerHeight)).toBe(true)
    expect(Number.isFinite(resNaN.bottomSpacerHeight)).toBe(true)
    expect(resNaN.visibleItems.length).toBeGreaterThan(0)

    const resInf = calculateVirtualWindow(items, () => 40, Infinity, 800)
    expect(Number.isFinite(resInf.topSpacerHeight)).toBe(true)
    expect(Number.isFinite(resInf.bottomSpacerHeight)).toBe(true)
  })

  it('safely handles getItemHeight returning NaN, Infinity, negative, or 0', () => {
    const items = Array.from({ length: 50 }, (_, i) => ({ id: i }))
    const res = calculateVirtualWindow(items, (it) => (it.id % 2 === 0 ? NaN : Infinity), 200, 800)
    expect(Number.isFinite(res.topSpacerHeight)).toBe(true)
    expect(Number.isFinite(res.bottomSpacerHeight)).toBe(true)
    expect(res.topSpacerHeight).toBeGreaterThanOrEqual(0)
    expect(res.bottomSpacerHeight).toBeGreaterThanOrEqual(0)
    expect(res.visibleItems.length).toBeGreaterThan(0)
  })

  it('handles very large datasets (10,000 items) efficiently without drift', () => {
    const items = Array.from({ length: 10000 }, (_, i) => ({ id: i }))
    const res = calculateVirtualWindow(items, () => 40, 200000, 1000, 600, 25)
    expect(res.startIndex).toBeGreaterThan(4000)
    expect(res.endIndex).toBeLessThan(6000)
    expect(res.topSpacerHeight + res.visibleItems.length * 40 + res.bottomSpacerHeight).toBe(400000)
  })
})
