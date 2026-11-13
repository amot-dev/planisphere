import { describe, expect, it, vi } from 'vitest'
import { expedition } from '../../../wailsjs/go/models'
import { buildOptimisticMovedRowPlan, executePlanMutation } from './plannerPlanHelpers'

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
