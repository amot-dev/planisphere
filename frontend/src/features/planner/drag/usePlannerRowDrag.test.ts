import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { expedition } from '../../../api/models'
import { PlannerRegionRow } from '../table/PlannerRegionRow'
import { PlannerCollapsedVisitedRow, PlannerDropSlotRow } from './PlannerDragPreviewRows'
import { stepDragAutoScroll } from './useDragViewportScroll'
import { createCachedRowDropPropsGetter } from './usePlannerDropHandlers'
import { resolveActiveDragRowIndex } from './useRowDropPreviewCache'

describe('usePlannerRowDrag Helpers and Preview Rows', () => {
  it('resolveActiveDragRowIndex locates shifted rows by row.id after route updates and returns -1 when removed', () => {
    const firstRow = expedition.RowView.createFrom({
      row: { id: 101, rowType: 'waypoint', systemName: 'Sol' },
    })
    const grabbedRow = expedition.RowView.createFrom({
      row: { id: 102, rowType: 'waypoint', systemName: 'Colonia' },
    })
    const insertedRow = expedition.RowView.createFrom({
      row: { id: 103, rowType: 'waypoint', systemName: 'Rohini' },
    })

    expect(resolveActiveDragRowIndex([firstRow, grabbedRow], 102)).toBe(1)
    expect(resolveActiveDragRowIndex([firstRow, null, insertedRow, grabbedRow], 102)).toBe(3)
    expect(resolveActiveDragRowIndex([firstRow, insertedRow], 102)).toBe(-1)
    expect(resolveActiveDragRowIndex(null, 102)).toBe(-1)
    expect(resolveActiveDragRowIndex([firstRow, grabbedRow], null)).toBe(-1)
  })

  it('stepDragAutoScroll scrolls only inside edge zones and sleeps when delta is zero', () => {
    const mockContainer = {
      scrollTop: 150,
      clientHeight: 400,
      scrollHeight: 1200,
      getBoundingClientRect: () => ({ top: 100, bottom: 500 }),
      querySelector: () => null,
    } as unknown as HTMLElement

    // Idle middle of viewport does not scroll and stops the animation frame loop.
    expect(stepDragAutoScroll(mockContainer, 300)).toBe(false)
    expect(mockContainer.scrollTop).toBe(150)

    // Null cursor also sleeps without modifying scrollTop.
    expect(stepDragAutoScroll(mockContainer, null)).toBe(false)
    expect(mockContainer.scrollTop).toBe(150)

    // Top edge zone scrolls upward until hitting scrollTop 0, then sleeps.
    expect(stepDragAutoScroll(mockContainer, 120)).toBe(true)
    expect(mockContainer.scrollTop).toBeLessThan(150)
    mockContainer.scrollTop = 0
    expect(stepDragAutoScroll(mockContainer, 120)).toBe(false)

    // Bottom edge zone scrolls downward until reaching bottom, then sleeps.
    mockContainer.scrollTop = 100
    expect(stepDragAutoScroll(mockContainer, 485)).toBe(true)
    expect(mockContainer.scrollTop).toBeGreaterThan(100)
    mockContainer.scrollTop = 800
    expect(stepDragAutoScroll(mockContainer, 485)).toBe(false)
  })

  it('createCachedRowDropPropsGetter keeps per-row dropProps references stable across gap hover updates and PlannerRegionRow stays memoized', () => {
    const dragPlan = expedition.PlanView.createFrom({
      expedition: { id: 1, status: 'planning' },
      rows: [
        { row: { id: 1, rowType: 'waypoint', systemName: 'Sol' }, number: 1 },
        { row: { id: 2, rowType: 'waypoint', systemName: 'Alpha Centauri' }, number: 2 },
        { row: { id: 3, rowType: 'waypoint', systemName: 'Colonia' }, number: 3 },
      ],
    })
    const dragOverGapIndexRef = { current: null as number | null }
    const gapUpdates: Array<number | null> = []
    const dropMoves: Array<{ fromIndex: number; targetIndex: number }> = []
    const scrollContainerElement = {
      getBoundingClientRect: () => ({ top: 0 }),
    } as unknown as HTMLDivElement

    const getRowDropProps = createCachedRowDropPropsGetter({
      plan: dragPlan,
      isPlanning: true,
      isCompleted: false,
      lastVisitedIndex: -1,
      draggedIndex: 0,
      dragOverGapIndexRef,
      updateDragOverGapIndex: (gapIndex) => {
        dragOverGapIndexRef.current = gapIndex
        gapUpdates.push(gapIndex)
      },
      dropSlotGaps: new Set<number>(),
      activeDropPreviews: null,
      scrollContainerRef: { current: scrollContainerElement },
      recordDragOverClientY: () => {},
      finishDropMove: (fromIndex, targetIndex) => {
        dropMoves.push({ fromIndex, targetIndex })
      },
    })

    const initialRowTwoProps = getRowDropProps(2)
    expect(getRowDropProps(2)).toBe(initialRowTwoProps)

    let defaultPrevented = false
    const makeDragEvent = (clientY: number) =>
      ({
        clientY,
        currentTarget: { getBoundingClientRect: () => ({ top: 100, height: 40 }) },
        dataTransfer: { dropEffect: '' },
        preventDefault: () => {
          defaultPrevented = true
        },
      }) as unknown as Parameters<typeof initialRowTwoProps.onDragOver>[0]

    // Hovering top half then bottom half of row 2 updates the gap via ref without recreating dropProps.
    initialRowTwoProps.onDragOver(makeDragEvent(110))
    expect(defaultPrevented).toBe(true)
    expect(dragOverGapIndexRef.current).toBe(2)
    expect(getRowDropProps(2)).toBe(initialRowTwoProps)

    initialRowTwoProps.onDragOver(makeDragEvent(130))
    expect(dragOverGapIndexRef.current).toBe(3)
    expect(gapUpdates).toEqual([2, 3])
    expect(getRowDropProps(2)).toBe(initialRowTwoProps)

    initialRowTwoProps.onDrop(makeDragEvent(130))
    expect(dropMoves).toEqual([{ fromIndex: 0, targetIndex: 2 }])

    expect((PlannerRegionRow as unknown as { $$typeof?: symbol }).$$typeof).toBe(
      Symbol.for('react.memo'),
    )
  })

  it('renders dashed drop-slot rows and collapsed visited summary rows during hold and drag', () => {
    // Dashed valid historical drop-slot row
    const historicalSlotHtml = renderToString(
      createElement(
        'table',
        null,
        createElement(
          'tbody',
          null,
          createElement(PlannerDropSlotRow, {
            gapIndex: 2,
            visibleColumnCount: 8,
            grabbedSystemName: 'LP 355-65',
            preview: expedition.DropSlotPreview.createFrom({
              targetIndex: 2,
              status: 'valid_visited',
              visitedAt: '2026-01-18T20:10:00Z',
              visitedAtGalactic: '3312-01-18 20:10 UTC',
            }),
            isDragTarget: false,
            onDragOver: () => {},
            onDrop: () => {},
          }),
        ),
      ),
    )
    expect(historicalSlotHtml).toContain('border-dashed border-[#00D2FF]/80')
    expect(historicalSlotHtml).toContain('+ DROP LP 355-65 HERE')
    expect(historicalSlotHtml).toContain('LOG VISIT')
    expect(historicalSlotHtml).toContain('3312-01-18 20:10 UTC')

    // Dashed orange FC-blocked drop-slot row
    const fcBlockedSlotHtml = renderToString(
      createElement(
        'table',
        null,
        createElement(
          'tbody',
          null,
          createElement(PlannerDropSlotRow, {
            gapIndex: 1,
            visibleColumnCount: 8,
            grabbedSystemName: 'LP 355-65',
            preview: expedition.DropSlotPreview.createFrom({
              targetIndex: 1,
              status: 'blocked_fc',
            }),
            isDragTarget: false,
            onDragOver: () => {},
            onDrop: () => {},
          }),
        ),
      ),
    )
    expect(fcBlockedSlotHtml).toContain('border-dashed border-[#FF7100]')
    expect(fcBlockedSlotHtml).toContain('FC STOP ORDER BLOCKED')

    // Collapsed visited summary row
    const collapsedRowHtml = renderToString(
      createElement(
        'table',
        null,
        createElement(
          'tbody',
          null,
          createElement(PlannerCollapsedVisitedRow, {
            visibleColumnCount: 8,
            waypointCount: 6,
            regionCount: 0,
            firstWaypointNumber: 1,
            firstSystemName: 'HR 839',
            lastWaypointNumber: 6,
            lastSystemName: 'BD+41 4773',
          }),
        ),
      ),
    )
    expect(collapsedRowHtml).toContain('⋯ #01 HR 839 → #06 BD+41 4773')
    expect(collapsedRowHtml).toContain('(6 visited waypoints collapsed) ⋯')
  })
})

