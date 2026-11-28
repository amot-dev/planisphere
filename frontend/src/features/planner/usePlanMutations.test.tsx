// @vitest-environment happy-dom
import { act, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { expedition } from '../../api/models'
import { createDeferred, flushMicrotasks } from '../../test/async'
import { makePlanView } from '../../test/fixtures'
import { mountTestRoot, renderHook, setFormControlValue } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { ExpeditionPlanner } from './ExpeditionPlanner'
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

  it('normalizes systemName whitespace and flushes focused inline drafts before ENGAGE, ROUTE, or EXPORT', async () => {
    let pendingUpdateRow = createDeferred<void>()
    const { spies } = installWailsAppMock({
      UpdateRow: () => pendingUpdateRow.promise,
      PrepareCarrierRoutePrompt: async (_expId: number, targetRowId: number) =>
        expedition.CarrierRoutePromptDefaults.createFrom({
          targetRowId,
          origin: 'Sol',
          destination: 'Rohini',
          carrierType: 'player',
          capacityUsed: 4000,
          depotTritium: 900,
        }),
    })

    const initialPlanningPlan = makePlanView({
      expedition: { id: 9, name: 'Origin Flush Test', status: 'planning' },
      rows: [
        {
          row: { id: 101, expeditionId: 9, position: 1, rowType: 'waypoint', systemName: '' },
          number: 1,
          visitState: 'future',
          permissions: { systemLocked: false, canAddFcStop: true, canRemoveFcStop: true },
        },
      ],
    })

    let latestRenderedPlan = initialPlanningPlan
    let replaceHarnessPlan!: (nextPlan: expedition.PlanView) => void
    const onOpenStartModal = vi.fn()
    const onOpenExportModal = vi.fn()
    const onRefreshPlan = vi.fn(() => {
      replaceHarnessPlan(initialPlanningPlan)
    })

    function PlannerHarness() {
      const [currentPlan, setCurrentPlan] = useState(initialPlanningPlan)
      replaceHarnessPlan = setCurrentPlan
      latestRenderedPlan = currentPlan
      return (
        <ExpeditionPlanner
          plan={currentPlan}
          exportEnabled={currentPlan.expedition.status !== 'planning'}
          onPlanOptimisticUpdate={setCurrentPlan}
          onOpenStartModal={onOpenStartModal}
          onOpenExportModal={onOpenExportModal}
          onRefreshPlan={onRefreshPlan}
          onBusyChange={() => {}}
        />
      )
    }

    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      const mounted = mountTestRoot(<PlannerHarness />)
      const originInput = mounted.container.querySelector(
        'input[placeholder="Star System"]',
      ) as HTMLInputElement

      act(() => {
        originInput.focus()
        setFormControlValue(originInput, '  Sagittarius A*  ')
      })
      const engageButton = mounted.findButtonByText('ENGAGE')
      expect(engageButton.disabled).toBe(false)

      // Clicking ENGAGE directly from the focused input blurs it synchronously, applies the trimmed optimistic systemName, and waits for UpdateRow.
      act(() => {
        engageButton.click()
      })
      expect(spies.UpdateRow).toHaveBeenCalledWith(
        expect.objectContaining({ id: 101, systemName: 'Sagittarius A*' }),
      )
      expect(latestRenderedPlan.rows[0].row.systemName).toBe('Sagittarius A*')
      expect(onOpenStartModal).not.toHaveBeenCalled()

      // Rejecting UpdateRow rolls back via onRefreshPlan and skips opening StartExpeditionModal.
      await act(async () => {
        pendingUpdateRow.reject(new Error('sqlite busy'))
        await flushMicrotasks()
      })
      expect(onRefreshPlan).toHaveBeenCalledTimes(1)
      expect(latestRenderedPlan.rows[0].row.systemName).toBe('')
      expect(onOpenStartModal).not.toHaveBeenCalled()

      // Retrying collapses internal whitespace before UpdateRow and opens StartExpeditionModal only after UpdateRow settles.
      pendingUpdateRow = createDeferred<void>()
      act(() => {
        originInput.focus()
        setFormControlValue(originInput, '  Alpha   Centauri  ')
      })
      act(() => {
        mounted.findButtonByText('ENGAGE').click()
      })
      expect(spies.UpdateRow).toHaveBeenLastCalledWith(
        expect.objectContaining({ id: 101, systemName: 'Alpha Centauri' }),
      )
      expect(latestRenderedPlan.rows[0].row.systemName).toBe('Alpha Centauri')
      expect(onOpenStartModal).not.toHaveBeenCalled()

      await act(async () => {
        pendingUpdateRow.resolve()
        await flushMicrotasks()
      })
      expect(onOpenStartModal).toHaveBeenCalledTimes(1)

      // Whitespace-only edits (leading, trailing, or repeated internal spaces) normalize to the persisted systemName and skip no-op UpdateRow.
      spies.UpdateRow.mockClear()
      act(() => {
        originInput.focus()
        setFormControlValue(originInput, '  Alpha  Centauri  ')
        originInput.blur()
      })
      expect(spies.UpdateRow).not.toHaveBeenCalled()

      // Active expedition + FC STOP row: clicking ROUTE from focused systemName input waits for UpdateRow before PrepareCarrierRoutePrompt.
      const activeFcPlan = makePlanView({
        expedition: { id: 10, name: 'Carrier Flush Test', status: 'active' },
        nextFcStopRowId: 202,
        rows: [
          {
            row: {
              id: 201,
              expeditionId: 10,
              position: 1,
              rowType: 'waypoint',
              systemName: 'Sol',
              visitedAt: '3312-01-01T00:00:00Z',
            },
            number: 1,
            visitState: 'visited',
            permissions: { systemLocked: true },
          },
          {
            row: {
              id: 202,
              expeditionId: 10,
              position: 2,
              rowType: 'waypoint',
              systemName: '',
              fcStop: true,
              fcArrived: false,
            },
            number: 2,
            visitState: 'future',
            permissions: {
              systemLocked: false,
              isNextFcStop: true,
              canRemoveFcStop: true,
            },
          },
        ],
      })
      act(() => {
        replaceHarnessPlan(activeFcPlan)
      })
      const stopSystemInput = mounted.container.querySelector(
        'input[placeholder="Star System"]',
      ) as HTMLInputElement
      pendingUpdateRow = createDeferred<void>()
      act(() => {
        stopSystemInput.focus()
        setFormControlValue(stopSystemInput, '  Rohini  ')
      })
      act(() => {
        mounted.findButtonByText('ROUTE').click()
      })
      expect(spies.UpdateRow).toHaveBeenLastCalledWith(
        expect.objectContaining({ id: 202, systemName: 'Rohini' }),
      )
      expect(latestRenderedPlan.rows[1].row.systemName).toBe('Rohini')
      expect(spies.PrepareCarrierRoutePrompt).not.toHaveBeenCalled()

      await act(async () => {
        pendingUpdateRow.resolve()
        await flushMicrotasks()
      })
      expect(spies.PrepareCarrierRoutePrompt).toHaveBeenCalledWith(10, 202)

      // Clicking EXPORT directly from a focused inline notes control flushes UpdateRow before invoking onOpenExportModal.
      const notesTextarea = mounted.container.querySelector(
        'textarea[placeholder="Flight notes, reminders..."]',
      ) as HTMLTextAreaElement
      pendingUpdateRow = createDeferred<void>()
      act(() => {
        notesTextarea.focus()
        setFormControlValue(notesTextarea, 'Check tritium depot')
      })
      act(() => {
        mounted.findButtonByText('EXPORT').click()
      })
      expect(spies.UpdateRow).toHaveBeenLastCalledWith(
        expect.objectContaining({ id: 201, notes: 'Check tritium depot' }),
      )
      expect(onOpenExportModal).not.toHaveBeenCalled()

      await act(async () => {
        pendingUpdateRow.resolve()
        await flushMicrotasks()
      })
      expect(onOpenExportModal).toHaveBeenCalledTimes(1)
    } finally {
      errorSpy.mockRestore()
    }
  })
})
