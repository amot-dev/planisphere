import { describe, expect, it, vi } from 'vitest'
import { expedition } from '../../../wailsjs/go/models'
import {
  buildOptimisticMovedRowPlan,
  buildPlannerTableItems,
  executePlanMutation,
  gapToTargetIndex,
} from './plannerPlanHelpers'

function buildSamplePlan(overrides: Partial<expedition.PlanView> = {}): expedition.PlanView {
  return expedition.PlanView.createFrom({
    expedition: {
      id: 7,
      name: 'Norma Reconnaissance',
      status: 'active',
      startTimestamp: '3312-01-01T00:00:00Z',
    },
    rows: [
      {
        row: {
          id: 10,
          expeditionId: 7,
          position: 1,
          rowType: 'region',
          regionId: 18,
        },
        number: 0,
        visitState: 'visited',
      },
      {
        row: {
          id: 11,
          expeditionId: 7,
          position: 2,
          rowType: 'waypoint',
          systemName: 'Sol',
          visitedAt: '3312-01-01T01:00:00Z',
        },
        number: 1,
        legModeId: 1,
        visitState: 'visited',
      },
      {
        row: {
          id: 12,
          expeditionId: 7,
          position: 3,
          rowType: 'waypoint',
          systemName: 'Colonia',
          visitedAt: '3312-01-03T12:00:00Z',
        },
        number: 2,
        legModeId: 1,
        visitState: 'current',
      },
    ],
    modes: [],
    ...overrides,
  })
}

describe('buildOptimisticMovedRowPlan', () => {
  it('updates 1-based row positions without sortOrder and renumbers waypoints', () => {
    const initialPlan = buildSamplePlan()
    const reorderedPlan = buildOptimisticMovedRowPlan(initialPlan, 1, 2)

    expect(reorderedPlan.rows.map((rowView) => rowView.row.id)).toEqual([10, 12, 11])
    expect(reorderedPlan.rows.map((rowView) => rowView.row.position)).toEqual([1, 2, 3])
    expect(reorderedPlan.rows.map((rowView) => rowView.number)).toEqual([0, 1, 2])
    for (const rowView of reorderedPlan.rows) {
      expect('sortOrder' in (rowView.row as unknown as Record<string, unknown>)).toBe(false)
    }
  })
})

describe('executePlanMutation', () => {
  it('rolls back via onRefreshPlan and clears busy state when mutation fails', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const samplePlan = buildSamplePlan()
    const optimisticUpdateCalls: expedition.PlanView[] = []
    const busyStates: Array<string | null> = []
    const refreshPlan = vi.fn(async () => {})
    const onSuccess = vi.fn()

    await executePlanMutation(
      'REORDERING ROUTE',
      'Failed to reorder route row',
      async () => {
        throw new Error('sqlite busy')
      },
      {
        onBusyChange: (busyMessage) => busyStates.push(busyMessage),
        onPlanOptimisticUpdate: (nextPlan) => optimisticUpdateCalls.push(nextPlan),
        onRefreshPlan: refreshPlan,
      },
      {
        optimisticPlan: samplePlan,
        onSuccess,
      },
    )

    expect(optimisticUpdateCalls).toEqual([samplePlan])
    expect(onSuccess).not.toHaveBeenCalled()
    expect(refreshPlan).toHaveBeenCalledTimes(1)
    expect(busyStates).toEqual(['REORDERING ROUTE', null])
    expect(errorSpy).toHaveBeenCalledTimes(1)
    errorSpy.mockRestore()
  })
})

describe('buildPlannerTableItems when a waypoint is grabbed', () => {
  it('collapses non-adjacent invalid visited rows and inserts drop_slot rows at valid/FC-blocked gaps', () => {
    expect(gapToTargetIndex(5, 3)).toBe(3)
    expect(gapToTargetIndex(1, 4)).toBe(3)
    expect(gapToTargetIndex(2, 2)).toBeNull()
    expect(gapToTargetIndex(2, 3)).toBeNull()

    // 5 visited waypoints (indices 0..4) + 1 unvisited waypoint (index 5, grabbed)
    // Suppose grabbed row 5 has a valid historical slot at targetIndex = 2 (Gap 2, between WP2 and WP3)
    // and an FC-blocked slot at targetIndex = 3 (Gap 3, between WP3 and WP4).
    const dropPreviews = [
      { targetIndex: 0, status: 'invalid' },
      { targetIndex: 1, status: 'invalid' },
      { targetIndex: 2, status: 'valid_visited', visitedAtGalactic: '3312-10-31 08:41 UTC' },
      { targetIndex: 3, status: 'blocked_fc' },
      { targetIndex: 4, status: 'invalid' },
      { targetIndex: 5, status: 'valid_unvisited' },
    ]
    const plan = expedition.PlanView.createFrom({
      expedition: { id: 1, status: 'active' },
      rows: [
        { row: { id: 1, rowType: 'waypoint', systemName: 'HR 839' }, number: 1, visitState: 'visited' },
        { row: { id: 2, rowType: 'waypoint', systemName: 'Mu Cephei' }, number: 2, visitState: 'visited' },
        { row: { id: 3, rowType: 'waypoint', systemName: 'LFT 509' }, number: 3, visitState: 'visited' },
        { row: { id: 4, rowType: 'waypoint', systemName: 'Bubble Sector' }, number: 4, visitState: 'visited' },
        { row: { id: 5, rowType: 'waypoint', systemName: 'Syreadiae' }, number: 5, visitState: 'visited' },
        {
          row: { id: 6, rowType: 'waypoint', systemName: 'LP 355-65' },
          number: 6,
          visitState: 'future',
          dropPreviews,
        },
      ],
    })

    const items = buildPlannerTableItems(plan, {}, false, 5, 4)
    expect(items.map((item) => item.kind)).toEqual([
      'collapsed_visited', // Row 0 (HR 839) collapsed
      'waypoint', // Row 1 (Mu Cephei) visible above Gap 2
      'drop_slot', // Gap 2 (valid_visited)
      'waypoint', // Row 2 (LFT 509) visible between Gap 2 and Gap 3
      'drop_slot', // Gap 3 (blocked_fc)
      'waypoint', // Row 3 (Bubble Sector) visible below Gap 3
      'collapsed_visited', // Row 4 (Syreadiae) collapsed
      'waypoint', // Row 5 (LP 355-65, unvisited)
    ])
  })

  it('inserts drop_slot both above and below a Region Header when the first waypoint after the Region Header is unvisited', () => {
    const dropPreviews = [
      { targetIndex: 0, status: 'invalid' },
      { targetIndex: 1, status: 'valid_visited', visitedAtGalactic: '3312-11-16 04:26 UTC' },
      { targetIndex: 2, status: 'valid_visited', visitedAtGalactic: '3312-11-16 04:26 UTC' },
      { targetIndex: 3, status: 'valid_unvisited' },
    ]
    const plan = expedition.PlanView.createFrom({
      expedition: { id: 1, status: 'active' },
      rows: [
        { row: { id: 1, rowType: 'waypoint', systemName: 'Schee Eaec' }, number: 1, visitState: 'visited' },
        { row: { id: 2, rowType: 'region', regionId: 35 }, number: 0, regionName: 'Formidine Rift' },
        { row: { id: 3, rowType: 'waypoint', systemName: 'Hypo Aeb' }, number: 2, visitState: 'current' },
        {
          row: { id: 4, rowType: 'waypoint', systemName: 'Syreadiae' },
          number: 3,
          visitState: 'future',
          dropPreviews,
        },
      ],
    })

    // lastVisitedIndex is 0 (Schee Eaec), Region Header is at index 1, first unvisited waypoint is at index 2 (Hypo Aeb),
    // and grabbed row is index 3 (Syreadiae). Both Gap 1 (above Formidine Rift) and Gap 2 (below Formidine Rift) get drop_slots!
    const items = buildPlannerTableItems(plan, {}, false, 3, 0)
    expect(items.map((item) => item.kind)).toEqual([
      'waypoint', // Row 0 (Schee Eaec)
      'drop_slot', // Gap 1 (above Formidine Rift)
      'region', // Row 1 (Formidine Rift)
      'drop_slot', // Gap 2 (below Formidine Rift, before unvisited Hypo Aeb)
      'waypoint', // Row 2 (Hypo Aeb)
      'waypoint', // Row 3 (Syreadiae)
    ])
  })

  it('keeps surrounding visited waypoints uncollapsed around the grabbed row original position', () => {
    const dropPreviews = [
      { targetIndex: 0, status: 'invalid' },
      { targetIndex: 1, status: 'invalid' },
      { targetIndex: 2, status: 'valid_visited', visitedAtGalactic: '3312-10-28 02:48 UTC' },
      { targetIndex: 3, status: 'invalid' },
      { targetIndex: 4, status: 'invalid' },
    ]
    const plan = expedition.PlanView.createFrom({
      expedition: { id: 1, status: 'active' },
      rows: [
        { row: { id: 1, rowType: 'waypoint', systemName: 'HR 839' }, number: 1, visitState: 'visited' },
        { row: { id: 2, rowType: 'waypoint', systemName: 'V509 Cassiopeiae' }, number: 2, visitState: 'visited' },
        {
          row: { id: 3, rowType: 'waypoint', systemName: 'NGC 7822' },
          number: 3,
          visitState: 'visited',
          dropPreviews,
        },
        { row: { id: 4, rowType: 'waypoint', systemName: 'BD+83 357' }, number: 4, visitState: 'visited' },
        { row: { id: 5, rowType: 'waypoint', systemName: 'Schee Eaec' }, number: 5, visitState: 'visited' },
      ],
    })

    // Grabbed row is index 2 (NGC 7822). Its neighbors index 1 (V509 Cassiopeiae) and index 3 (BD+83 357)
    // remain uncollapsed to frame the original position, while index 0 and index 4 collapse.
    const items = buildPlannerTableItems(plan, {}, false, 2, 4)
    expect(items.map((item) => item.kind)).toEqual([
      'collapsed_visited', // Row 0 (HR 839)
      'waypoint', // Row 1 (V509 Cassiopeiae) frames original position above
      'waypoint', // Row 2 (NGC 7822, grabbed)
      'waypoint', // Row 3 (BD+83 357) frames original position below
      'collapsed_visited', // Row 4 (Schee Eaec)
      'drop_slot', // Gap 5 (end of all-visited table? only if valid_unvisited, here invalid so omitted)
    ].slice(0, 5))
  })
})
