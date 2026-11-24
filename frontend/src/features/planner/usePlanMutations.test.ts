// @vitest-environment happy-dom
import { act, createElement, createRef } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { expedition } from '../../api/models'
import {
  executePlanMutation,
  usePlanMutations,
  type CarrierMutationLockContext,
} from './usePlanMutations'

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

function createDeferredPromise() {
  let resolvePromise!: () => void
  const promise = new Promise<void>((resolve) => {
    resolvePromise = resolve
  })
  return { promise, resolve: resolvePromise }
}

describe('usePlanMutations overlapping busy coordination', () => {
  it('preserves remaining mutation busy message regardless of settlement order', async () => {
    const samplePlan = buildSamplePlan()
    const busyStates: Array<string | null> = []
    const carrierContextRef = createRef<CarrierMutationLockContext>() as {
      current: CarrierMutationLockContext
    }
    carrierContextRef.current = {
      activeCarrierRoute: null,
      isManualCarrierRoute: false,
      routedCarrierStopIndex: -1,
      onCancelCarrierRoute: () => {},
    }

    let latestMutations!: ReturnType<typeof usePlanMutations>
    function HookProbe() {
      latestMutations = usePlanMutations({
        plan: samplePlan,
        isPlanning: false,
        onBusyChange: (busyMessage) => busyStates.push(busyMessage),
        applyOptimisticPlan: () => {},
        onRefreshPlan: () => {},
        carrierContextRef,
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
    } finally {
      act(() => {
        root.unmount()
      })
      container.remove()
    }
  })
})

