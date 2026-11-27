// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { expedition } from '../../api/models'

const mockUpdateRow = vi.fn()

vi.mock('../../api/backend', () => ({
  AddRow: vi.fn(),
  DeleteRow: vi.fn(),
  MoveRow: vi.fn(),
  OverrideCarrierStopArrival: vi.fn(),
  SetExpeditionFlightMode: vi.fn(),
  SetLegMode: vi.fn(),
  UpdateRow: (...args: unknown[]) => mockUpdateRow(...args),
}))

import { executePlanMutation, usePlanMutations } from './usePlanMutations'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

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
    modes: [],
  })
}

afterEach(() => {
  vi.resetAllMocks()
})

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

function createDeferredPromise() {
  let resolvePromise!: () => void
  const promise = new Promise<void>((resolve) => {
    resolvePromise = resolve
  })
  return { promise, resolve: resolvePromise }
}

describe('usePlanMutations overlapping busy coordination and Go permission guards', () => {

  it('preserves remaining mutation busy message regardless of settlement order and enforces Go carrier permissions', async () => {
    const samplePlan = buildSamplePlan()
    const busyStates: Array<string | null> = []

    let latestMutations!: ReturnType<typeof usePlanMutations>
    function HookProbe() {
      latestMutations = usePlanMutations({
        plan: samplePlan,
        isPlanning: false,
        onBusyChange: (busyMessage) => busyStates.push(busyMessage),
        applyOptimisticPlan: () => {},
        onRefreshPlan: () => {},
      })
      return null
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      act(() => {
        root.render(createElement(HookProbe))
      })

      // Later mutation settles first: restores earlier mutation's message until it finishes.
      const firstAction = createDeferredPromise()
      const secondAction = createDeferredPromise()
      const firstPromise = latestMutations.handleLifecycleAction(
        'COMPLETING EXPEDITION',
        () => firstAction.promise,
      )
      const secondPromise = latestMutations.handleLifecycleAction(
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

      // Earlier mutation settles first: preserves later mutation's message until it finishes.
      busyStates.length = 0
      const thirdAction = createDeferredPromise()
      const fourthAction = createDeferredPromise()
      const thirdPromise = latestMutations.handleLifecycleAction(
        'REORDERING ROUTE',
        () => thirdAction.promise,
      )
      const fourthPromise = latestMutations.handleLifecycleAction(
        'UPDATING REGION',
        () => fourthAction.promise,
      )

      thirdAction.resolve()
      await thirdPromise
      expect(busyStates).toEqual(['REORDERING ROUTE', 'UPDATING REGION', 'UPDATING REGION'])

      fourthAction.resolve()
      await fourthPromise
      expect(busyStates).toEqual(['REORDERING ROUTE', 'UPDATING REGION', 'UPDATING REGION', null])

      // Verify toggleFcStop respects Go permissions (canAddFcStop=false on row 11, canRemoveFcStop=true on row 12).
      mockUpdateRow.mockResolvedValueOnce(undefined)
      await act(async () => {
        await latestMutations.toggleFcStop(samplePlan.rows[0].row)
      })
      expect(mockUpdateRow).not.toHaveBeenCalled()

      await act(async () => {
        await latestMutations.toggleFcStop(samplePlan.rows[1].row)
      })
      expect(mockUpdateRow).toHaveBeenCalledWith(
        expect.objectContaining({ id: 12, fcStop: false }),
      )
      mockUpdateRow.mockClear()

      // Verify handleRequestOverrideFcArrived reads overrideFcCascadeCount directly from rowView.permissions.
      act(() => {
        latestMutations.handleRequestOverrideFcArrived(samplePlan.rows[1])
      })
      expect(latestMutations.confirmState).toEqual({
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
        mockUpdateRow.mockRejectedValueOnce(new Error('locked')).mockResolvedValueOnce(undefined)
        await expect(latestMutations.setFcArrived(11, true)).resolves.toBe(false)
        await expect(latestMutations.setFcArrived(11, true)).resolves.toBe(true)
        await expect(
          latestMutations.commitField(samplePlan.rows[0].row, 'systemName', 'Sol'),
        ).resolves.toBe(true)
        mockUpdateRow.mockRejectedValueOnce(new Error('invalid'))
        await expect(
          latestMutations.commitField(samplePlan.rows[0].row, 'poiName', 'Earth'),
        ).resolves.toBe(false)
      } finally {
        errorSpy.mockRestore()
      }
    } finally {
      act(() => {
        root.unmount()
      })
      container.remove()
    }
  })
})
