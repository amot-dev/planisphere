// @vitest-environment happy-dom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { expedition } from '../../api/models'
import { createDeferred } from '../../test/async'
import { makePlanView } from '../../test/fixtures'
import { renderHook } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { executePlanMutation, usePlanMutations } from './usePlanMutations'

function buildSamplePlan(): expedition.PlanView {
  return makePlanView({
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
          fcStop: false,
        },
        number: 1,
        legModeId: 1,
        visitState: 'visited',
        permissions: {
          systemLocked: true,
          canAddFcStop: false,
          canRemoveFcStop: false,
        },
      },
      {
        row: {
          id: 12,
          expeditionId: 7,
          position: 2,
          rowType: 'waypoint',
          systemName: 'Colonia',
          fcStop: true,
          fcArrived: false,
        },
        number: 2,
        legModeId: 1,
        visitState: 'future',
        permissions: {
          systemLocked: false,
          canAddFcStop: true,
          canRemoveFcStop: true,
          canOverrideFcBadge: true,
          overrideFcCascadeCount: 3,
        },
      },
    ],
  })
}

describe('executePlanMutation', () => {
  it('rolls back via onRefreshPlan and clears busy state when mutation fails', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      const samplePlan = buildSamplePlan()
      const optimisticUpdateCalls: expedition.PlanView[] = []
      const busyStates: Array<string | null> = []
      const refreshPlan = vi.fn(async () => {})
      const onSuccess = vi.fn()

      const mutationSucceeded = await executePlanMutation(
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

      expect(mutationSucceeded).toBe(false)
      expect(optimisticUpdateCalls).toEqual([samplePlan])
      expect(onSuccess).not.toHaveBeenCalled()
      expect(refreshPlan).toHaveBeenCalledTimes(1)
      expect(busyStates).toEqual(['REORDERING ROUTE', null])
      expect(errorSpy).toHaveBeenCalledTimes(1)

      const retrySucceeded = await executePlanMutation(
        'REORDERING ROUTE',
        'Failed to reorder route row',
        async () => {},
        {
          onBusyChange: (busyMessage) => busyStates.push(busyMessage),
          onPlanOptimisticUpdate: (nextPlan) => optimisticUpdateCalls.push(nextPlan),
          onRefreshPlan: refreshPlan,
        },
        { onSuccess },
      )
      expect(retrySucceeded).toBe(true)
      expect(onSuccess).toHaveBeenCalledTimes(1)
    } finally {
      errorSpy.mockRestore()
    }
  })
})

describe('usePlanMutations overlapping busy coordination and Go permission guards', () => {
  it('preserves remaining mutation busy message when an overlapping mutation settles and enforces Go carrier permissions', async () => {
    const { spies } = installWailsAppMock({
      UpdateRow: async () => {},
    })
    const samplePlan = buildSamplePlan()
    const busyStates: Array<string | null> = []

    const mountedHook = renderHook(() =>
      usePlanMutations({
        plan: samplePlan,
        isPlanning: false,
        onBusyChange: (busyMessage) => busyStates.push(busyMessage),
        applyOptimisticPlan: () => {},
        onRefreshPlan: () => {},
      }),
    )

    // Later mutation settles first: restores earlier mutation's message until it finishes.
    const firstAction = createDeferred()
    const secondAction = createDeferred()
    const firstPromise = mountedHook.getResult().handleLifecycleAction(
      'COMPLETING EXPEDITION',
      () => firstAction.promise,
    )
    const secondPromise = mountedHook.getResult().handleLifecycleAction(
      'UPDATING WAYPOINT',
      () => secondAction.promise,
    )

    expect(busyStates).toEqual(['COMPLETING EXPEDITION', 'UPDATING WAYPOINT'])

    secondAction.resolve()
    await secondPromise
    expect(busyStates).toEqual([
      'COMPLETING EXPEDITION',
      'UPDATING WAYPOINT',
      'COMPLETING EXPEDITION',
    ])

    firstAction.resolve()
    await firstPromise
    expect(busyStates).toEqual([
      'COMPLETING EXPEDITION',
      'UPDATING WAYPOINT',
      'COMPLETING EXPEDITION',
      null,
    ])

    // Verify toggleFcStop respects Go permissions (canAddFcStop=false on row 11, canRemoveFcStop=true on row 12).
    spies.UpdateRow.mockResolvedValueOnce(undefined)
    await act(async () => {
      await mountedHook.getResult().toggleFcStop(samplePlan.rows[0].row)
    })
    expect(spies.UpdateRow).not.toHaveBeenCalled()

    await act(async () => {
      await mountedHook.getResult().toggleFcStop(samplePlan.rows[1].row)
    })
    expect(spies.UpdateRow).toHaveBeenCalledWith(
      expect.objectContaining({ id: 12, fcStop: false }),
    )
    spies.UpdateRow.mockClear()

    // Verify handleRequestOverrideFcArrived reads overrideFcCascadeCount directly from rowView.permissions.
    act(() => {
      mountedHook.getResult().handleRequestOverrideFcArrived(samplePlan.rows[1])
    })
    expect(mountedHook.getResult().confirmState).toEqual({
      kind: 'override_fc_arrival',
      rowId: 12,
      waypointNumber: 2,
      systemName: 'Colonia',
      targetArrived: true,
      cascadedCount: 3,
    })

    // Verify setFcArrived and commitField propagate boolean resolution outcomes.
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      spies.UpdateRow.mockRejectedValueOnce(new Error('locked')).mockResolvedValueOnce(undefined)
      await expect(mountedHook.getResult().setFcArrived(11, true)).resolves.toBe(false)
      await expect(mountedHook.getResult().setFcArrived(11, true)).resolves.toBe(true)
      await expect(
        mountedHook.getResult().commitField(samplePlan.rows[0].row, 'systemName', 'Sol'),
      ).resolves.toBe(true)
      spies.UpdateRow.mockRejectedValueOnce(new Error('invalid'))
      await expect(
        mountedHook.getResult().commitField(samplePlan.rows[0].row, 'poiName', 'Earth'),
      ).resolves.toBe(false)
    } finally {
      errorSpy.mockRestore()
    }
  })
})
