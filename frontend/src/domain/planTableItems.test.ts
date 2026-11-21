import { describe, expect, it } from 'vitest'
import { expedition } from '../api/models'
import {
  buildOptimisticDeletedRowPlan,
  buildOptimisticFcArrivedPlan,
  buildOptimisticFcStopTogglePlan,
  buildOptimisticLegModePlan,
  buildOptimisticMovedRowPlan,
} from './planOptimisticReducers'
import { buildPlannerTableItems, gapToTargetIndex } from './planTableItems'

describe('planOptimisticReducers structural sharing', () => {
  it('updates 1-based row positions without sortOrder, renumbers waypoints, and preserves untouched RowView identities across all reducers', () => {
    const initialPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 7,
        name: 'Norma Reconnaissance',
        status: 'active',
        startTimestamp: '3312-01-01T00:00:00Z',
      },
      rows: [
        {
          row: { id: 10, expeditionId: 7, position: 1, rowType: 'region', regionId: 18 },
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
          gec: { id: 501, name: 'Cradle of Humanity', type: 'Historical', url: '', count: 1 },
        },
        {
          row: {
            id: 12,
            expeditionId: 7,
            position: 3,
            rowType: 'waypoint',
            systemName: 'Colonia',
            fcStop: true,
            fcArrived: false,
            visitedAt: '3312-01-03T12:00:00Z',
          },
          number: 2,
          legModeId: 1,
          visitState: 'current',
        },
        {
          row: {
            id: 13,
            expeditionId: 7,
            position: 4,
            rowType: 'waypoint',
            systemName: 'Beagle Point',
          },
          number: 3,
          legModeId: 1,
          visitState: 'future',
        },
      ],
      modes: [{ id: 2, name: 'Exobiology', color: '#00E5A3', description: '', isDefault: false, sortOrder: 2 }],
    })

    // Move index 1 <-> index 2: index 0 (before span) and index 3 (after span) retain === identity.
    const reorderedPlan = buildOptimisticMovedRowPlan(initialPlan, 1, 2)
    expect(reorderedPlan).toBeInstanceOf(expedition.PlanView)
    expect(reorderedPlan.expedition).toBe(initialPlan.expedition)
    expect(reorderedPlan.modes).toBe(initialPlan.modes)
    expect(reorderedPlan.rows.map((rowView) => rowView.row.id)).toEqual([10, 12, 11, 13])
    expect(reorderedPlan.rows.map((rowView) => rowView.row.position)).toEqual([1, 2, 3, 4])
    expect(reorderedPlan.rows.map((rowView) => rowView.number)).toEqual([0, 1, 2, 3])
    expect(reorderedPlan.rows[0]).toBe(initialPlan.rows[0])
    expect(reorderedPlan.rows[3]).toBe(initialPlan.rows[3])
    expect(reorderedPlan.rows[2].gec).toBe(initialPlan.rows[1].gec)
    for (const rowView of reorderedPlan.rows) {
      expect('sortOrder' in (rowView.row as unknown as Record<string, unknown>)).toBe(false)
    }

    // FC stop toggle, FC arrival, and leg mode updates clone only the targeted row.
    const fcToggledPlan = buildOptimisticFcStopTogglePlan(initialPlan, initialPlan.rows[3].row, true)
    expect(fcToggledPlan.rows[0]).toBe(initialPlan.rows[0])
    expect(fcToggledPlan.rows[1]).toBe(initialPlan.rows[1])
    expect(fcToggledPlan.rows[2]).toBe(initialPlan.rows[2])
    expect(fcToggledPlan.rows[3].row.fcStop).toBe(true)

    const fcArrivedPlan = buildOptimisticFcArrivedPlan(fcToggledPlan, 12, true)
    expect(fcArrivedPlan.rows[1]).toBe(initialPlan.rows[1])
    expect(fcArrivedPlan.rows[3]).toBe(fcToggledPlan.rows[3])
    expect(fcArrivedPlan.nextFcStopRowId).toBe(13)

    const legModePlan = buildOptimisticLegModePlan(initialPlan, 11, 2)
    expect(legModePlan.rows[0]).toBe(initialPlan.rows[0])
    expect(legModePlan.rows[2]).toBe(initialPlan.rows[2])
    expect(legModePlan.rows[1].row).toBe(initialPlan.rows[1].row)
    expect(legModePlan.rows[1].legModeColor).toBe('#00E5A3')

    // Deleting row 12 preserves rows before index 2 and only renumbers subsequent waypoints.
    const deletedPlan = buildOptimisticDeletedRowPlan(initialPlan, 12)
    expect(deletedPlan.rows).toHaveLength(3)
    expect(deletedPlan.rows[0]).toBe(initialPlan.rows[0])
    expect(deletedPlan.rows[1]).toBe(initialPlan.rows[1])
    expect(deletedPlan.rows[2].number).toBe(2)
    expect(deletedPlan.rows[2].row).toBe(initialPlan.rows[3].row)
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
    ].map((item) => expedition.DropSlotPreview.createFrom(item))
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
        },
      ],
    })

    const items = buildPlannerTableItems(plan, {}, false, 5, 4, dropPreviews)
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
    ].map((item) => expedition.DropSlotPreview.createFrom(item))
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
        },
      ],
    })

    // lastVisitedIndex is 0 (Schee Eaec), Region Header is at index 1, first unvisited waypoint is at index 2 (Hypo Aeb),
    // and grabbed row is index 3 (Syreadiae). Both Gap 1 (above Formidine Rift) and Gap 2 (below Formidine Rift) get drop_slots!
    const items = buildPlannerTableItems(plan, {}, false, 3, 0, dropPreviews)
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
    ].map((item) => expedition.DropSlotPreview.createFrom(item))
    const plan = expedition.PlanView.createFrom({
      expedition: { id: 1, status: 'active' },
      rows: [
        { row: { id: 1, rowType: 'waypoint', systemName: 'HR 839' }, number: 1, visitState: 'visited' },
        { row: { id: 2, rowType: 'waypoint', systemName: 'V509 Cassiopeiae' }, number: 2, visitState: 'visited' },
        {
          row: { id: 3, rowType: 'waypoint', systemName: 'NGC 7822' },
          number: 3,
          visitState: 'visited',
        },
        { row: { id: 4, rowType: 'waypoint', systemName: 'BD+83 357' }, number: 4, visitState: 'visited' },
        { row: { id: 5, rowType: 'waypoint', systemName: 'Schee Eaec' }, number: 5, visitState: 'visited' },
      ],
    })

    // Grabbed row is index 2 (NGC 7822). Its neighbors index 1 (V509 Cassiopeiae) and index 3 (BD+83 357)
    // remain uncollapsed to frame the original position, while index 0 and index 4 collapse.
    const items = buildPlannerTableItems(plan, {}, false, 2, 4, dropPreviews)
    expect(
      items
        .map((item) => item.kind)
        .slice(0, 5),
    ).toEqual([
      'collapsed_visited', // Row 0 (HR 839)
      'waypoint', // Row 1 (V509 Cassiopeiae) frames original position above
      'waypoint', // Row 2 (NGC 7822, grabbed)
      'waypoint', // Row 3 (BD+83 357) frames original position below
      'collapsed_visited', // Row 4 (Schee Eaec)
    ])
  })
})

