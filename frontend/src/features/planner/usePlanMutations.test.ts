import { describe, expect, it, vi } from 'vitest'
import { expedition } from '../../api/models'
import { executePlanMutation } from './usePlanMutations'

function buildSamplePlan(): expedition.PlanView {
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
          id: 11,
          expeditionId: 7,
          position: 1,
          rowType: 'waypoint',
          systemName: 'Sol',
          visitedAt: '3312-01-01T01:00:00Z',
        },
        number: 1,
        legModeId: 1,
        visitState: 'visited',
      },
    ],
    modes: [],
  })
}

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

