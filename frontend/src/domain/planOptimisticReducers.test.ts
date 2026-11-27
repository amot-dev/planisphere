import { describe, expect, it } from 'vitest'
import { expedition } from '../api/models'
import {
  buildOptimisticDeletedRowPlan,
  buildOptimisticExpeditionModePlan,
  buildOptimisticFcArrivedPlan,
  buildOptimisticFcStopTogglePlan,
  buildOptimisticLegModePlan,
  buildOptimisticMovedRowPlan,
} from './planOptimisticReducers'

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
          permissions: {
            systemLocked: false,
            allowInsertWaypoint: true,
            canReorder: true,
            canAddFcStop: true,
            canRemoveFcStop: true,
            canOverrideFcBadge: true,
            hasDrawer: true,
            isNextFcStop: true,
          },
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
          permissions: {
            systemLocked: false,
            allowInsertWaypoint: true,
            canReorder: true,
            canAddFcStop: true,
            canRemoveFcStop: true,
            canOverrideFcBadge: true,
            hasDrawer: false,
          },
        },
      ],
      modes: [
        {
          id: 2,
          name: 'Exobiology',
          color: '#00E5A3',
          description: '',
          isDefault: false,
          sortOrder: 2,
        },
      ],
    })

    // Move index 1 <-> index 2: index 0 (before span) and index 3 (after span) retain === identity.
    const reorderedPlan = buildOptimisticMovedRowPlan(initialPlan, 1, 2)
    expect(reorderedPlan).toBeInstanceOf(expedition.PlanView)
    expect(reorderedPlan.expedition).toBe(initialPlan.expedition)
    expect(reorderedPlan.modes).toBe(initialPlan.modes)
    expect(reorderedPlan.rows.map((rowView) => rowView.row.id)).toEqual([10, 12, 11, 13])
    expect(reorderedPlan.rows.map((rowView) => rowView.row.position)).toEqual([1, 2, 3, 4])
    expect(reorderedPlan.rows.map((rowView) => rowView.number)).toEqual([0, 1, 2, 3])
    expect(reorderedPlan.lastVisitedIndex).toBe(2)
    expect(reorderedPlan.lastFcArrivedIndex).toBe(-1)
    expect(reorderedPlan.rows[0]).toBe(initialPlan.rows[0])
    expect(reorderedPlan.rows[3]).toBe(initialPlan.rows[3])
    expect(reorderedPlan.rows[2].gec).toBe(initialPlan.rows[1].gec)
    for (const rowView of reorderedPlan.rows) {
      expect('sortOrder' in (rowView.row as unknown as Record<string, unknown>)).toBe(false)
    }

    // FC stop toggle, FC arrival, and leg mode updates clone only the targeted row and sync permissions.
    const fcToggledPlan = buildOptimisticFcStopTogglePlan(initialPlan, initialPlan.rows[3].row, true)
    expect(fcToggledPlan.rows[0]).toBe(initialPlan.rows[0])
    expect(fcToggledPlan.rows[1]).toBe(initialPlan.rows[1])
    expect(fcToggledPlan.rows[2]).toBe(initialPlan.rows[2])
    expect(fcToggledPlan.rows[3].row.fcStop).toBe(true)
    expect(fcToggledPlan.rows[3].permissions?.canRemoveFcStop).toBe(true)
    expect(fcToggledPlan.rows[3].permissions?.isNextFcStop).toBe(false)

    const fcArrivedBeaglePlan = buildOptimisticFcArrivedPlan(fcToggledPlan, 13, true)
    expect(fcArrivedBeaglePlan.lastFcArrivedIndex).toBe(3)
    expect(fcArrivedBeaglePlan.rows[3].permissions?.systemLocked).toBe(true)
    expect(fcArrivedBeaglePlan.rows[3].permissions?.canRemoveFcStop).toBe(false)
    expect(fcArrivedBeaglePlan.rows[3].permissions?.isNextFcStop).toBe(false)

    const fcArrivedPlan = buildOptimisticFcArrivedPlan(fcToggledPlan, 12, true)
    expect(fcArrivedPlan.rows[1]).toBe(initialPlan.rows[1])
    expect(fcArrivedPlan.rows[3]).toBe(fcToggledPlan.rows[3])
    expect(fcArrivedPlan.nextFcStopRowId).toBe(13)
    expect(fcArrivedPlan.lastFcArrivedIndex).toBe(2)
    expect(fcArrivedPlan.rows[2].permissions?.isNextFcStop).toBe(false)

    const removedStopPlan = buildOptimisticFcStopTogglePlan(
      fcArrivedPlan,
      fcArrivedPlan.rows[3].row,
      false,
    )
    expect(removedStopPlan.nextFcStopRowId).toBe(0)
    expect(removedStopPlan.rows[3].permissions?.isNextFcStop).toBe(false)

    const firstPendingStopPlan = buildOptimisticFcStopTogglePlan(
      removedStopPlan,
      removedStopPlan.rows[3].row,
      true,
    )
    expect(firstPendingStopPlan.nextFcStopRowId).toBe(13)
    expect(firstPendingStopPlan.rows[3].permissions?.isNextFcStop).toBe(true)

    const legModePlan = buildOptimisticLegModePlan(initialPlan, 11, 2)
    expect(legModePlan.rows[0]).toBe(initialPlan.rows[0])
    expect(legModePlan.rows[2]).toBe(initialPlan.rows[2])
    expect(legModePlan.rows[1].row).toBe(initialPlan.rows[1].row)
    expect(legModePlan.rows[1].legModeColor).toBe('#00E5A3')
    expect(legModePlan.expedition).toBe(initialPlan.expedition)

    // Updating the active current leg syncs expedition.activeModeId; updating expedition mode updates current leg.
    const currentLegModePlan = buildOptimisticLegModePlan(initialPlan, 12, 2)
    expect(currentLegModePlan.expedition.activeModeId).toBe(2)
    expect(currentLegModePlan.rows[2].legModeId).toBe(2)
    expect(currentLegModePlan.rows[1]).toBe(initialPlan.rows[1])

    const expeditionModePlan = buildOptimisticExpeditionModePlan(initialPlan, 2)
    expect(expeditionModePlan.expedition.activeModeId).toBe(2)
    expect(expeditionModePlan.rows[2].legModeId).toBe(2)
    expect(expeditionModePlan.rows[2].legModeColor).toBe('#00E5A3')
    expect(expeditionModePlan.rows[1]).toBe(initialPlan.rows[1])
    expect(expeditionModePlan.rows[3]).toBe(initialPlan.rows[3])

    // Cascade forward updates visited legs from the target onward while leaving active current leg and activeModeId untouched.
    const multiVisitedPlan = expedition.PlanView.createFrom({
      expedition: { id: 7, name: 'Cascade Check', status: 'active', activeModeId: 1 },
      rows: [
        { row: { id: 21, rowType: 'waypoint', systemName: 'Sol' }, number: 1, visitState: 'visited' },
        { row: { id: 22, rowType: 'waypoint', systemName: 'Alpha' }, number: 2, legModeId: 1, visitState: 'visited' },
        { row: { id: 23, rowType: 'waypoint', systemName: 'Beta' }, number: 3, legModeId: 1, visitState: 'visited' },
        { row: { id: 24, rowType: 'waypoint', systemName: 'Gamma' }, number: 4, legModeId: 1, visitState: 'current' },
      ],
      modes: [
        {
          id: 2,
          name: 'Exobiology',
          color: '#00E5A3',
          description: '',
          isDefault: false,
          sortOrder: 2,
        },
      ],
    })
    const cascadedPlan = buildOptimisticLegModePlan(multiVisitedPlan, 22, 2, true)
    expect(cascadedPlan.rows[0]).toBe(multiVisitedPlan.rows[0])
    expect(cascadedPlan.rows[1].legModeId).toBe(2)
    expect(cascadedPlan.rows[2].legModeId).toBe(2)
    expect(cascadedPlan.rows[3]).toBe(multiVisitedPlan.rows[3])
    expect(cascadedPlan.expedition).toBe(multiVisitedPlan.expedition)

    // Deleting row 12 preserves rows before index 2 and only renumbers subsequent waypoints.
    const deletedPlan = buildOptimisticDeletedRowPlan(initialPlan, 12)
    expect(deletedPlan.rows).toHaveLength(3)
    expect(deletedPlan.rows[0]).toBe(initialPlan.rows[0])
    expect(deletedPlan.rows[1]).toBe(initialPlan.rows[1])
    expect(deletedPlan.rows[2].number).toBe(2)
    expect(deletedPlan.rows[2].row).toBe(initialPlan.rows[3].row)
  })
})
