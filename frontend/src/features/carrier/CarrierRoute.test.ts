// @vitest-environment happy-dom
import { act, createElement, useEffect, useState } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { expedition } from '../../api/models'
import { renderHook } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { CarrierRouteBottomPanel } from './CarrierRouteBottomPanel'
import { CarrierRouteModal } from './CarrierRouteModal'
import { useCarrierRoute } from './useCarrierRoute'

describe('CarrierRoute UI Components', () => {
  it('CarrierRouteModal omits Manual Entry Mode and disables only PLOT button while querying Spansh', () => {
    const renderCarrierModal = (
      overrides: Partial<Parameters<typeof CarrierRouteModal>[0]['prompt']> = {},
    ) =>
      renderToStaticMarkup(
        createElement(CarrierRouteModal, {
          prompt: {
            targetRowId: 10,
            origin: 'Sol',
            destination: 'Colonia',
            capacityUsed: '5000',
            depotTritium: '1000',
            carrierType: 'player',
            loading: false,
            ...overrides,
          },
          onClose: () => {},
          onUpdate: () => {},
          onStart: () => {},
        }),
      )

    const idleHtml = renderCarrierModal()
    expect(idleHtml).not.toContain('Manual Entry Mode')
    expect(idleHtml).toContain('PLOT VIA SPANSH')

    const loadingHtml = renderCarrierModal({ loading: true })
    expect(loadingHtml).toContain('QUERYING SPANSH...')
    expect(loadingHtml.match(/<button[^>]*disabled=""/g)).toHaveLength(1)
    expect(loadingHtml).not.toMatch(/disabled=""[^>]*>CANCEL<\/button>/)
    expect(loadingHtml).toMatch(/disabled=""[^>]*>QUERYING SPANSH\.\.\.<\/button>/)
  })

  it('CarrierRouteBottomPanel renders Close button with tooltip, start/end indicator with ??? fallback, and Mark FC Stop Arrival button', () => {
    const renderCarrierPanel = (
      overrides: Partial<Parameters<typeof CarrierRouteBottomPanel>[0]> & {
        route: expedition.CarrierRouteResult
      },
    ) =>
      renderToStaticMarkup(
        createElement(CarrierRouteBottomPanel, {
          jumpIndex: 0,
          manualMode: true,
          hasNextFcStop: false,
          nextFcStopSystem: '',
          onCancelRoute: () => {},
          onToggleManualMode: () => {},
          onCompleteSpanshJump: () => {},
          manualDistanceInput: '',
          onChangeManualDistance: () => {},
          manualTritiumInput: '',
          onChangeManualTritium: () => {},
          onLogManualJump: () => {},
          manualLegJumps: 0,
          manualLegDistance: 0,
          manualLegTritium: 0,
          loggingJump: false,
          copiedNextJump: false,
          onCopySystem: () => {},
          onArrive: () => {},
          ...overrides,
        }),
      )

    const manualWithStop = renderCarrierPanel({
      route: expedition.CarrierRouteResult.createFrom({
        origin: 'Sol',
        destination: 'Colonia',
        carrierType: 'player',
        manualMode: true,
        jumps: [],
      }),
      hasNextFcStop: true,
      nextFcStopSystem: 'Colonia',
      manualDistanceInput: '412.5',
      manualTritiumInput: '59',
      manualLegJumps: 2,
      manualLegDistance: 950,
      manualLegTritium: 180,
    })

    expect(manualWithStop).not.toContain('Cancel Remaining Route')
    expect(manualWithStop).toContain('CLOSE')
    expect(manualWithStop).toContain(
      'Existing jumps remain logged in your expedition statistics.',
    )
    expect(manualWithStop).toContain('Sol')
    expect(manualWithStop).toContain('Colonia')
    expect(manualWithStop).toContain('value="412.5"')
    expect(manualWithStop).toContain('value="59"')
    expect(manualWithStop).toContain('MARK FC STOP ARRIVAL')
    expect(manualWithStop).toContain(
      'Mark Fleet Carrier arrival at Colonia and close the carrier router',
    )
    expect(manualWithStop).not.toContain('ARRIVE AT WAYPOINT')
    expect(manualWithStop).not.toMatch(/disabled=""[^>]*>MARK FC STOP ARRIVAL/)

    const manualWithoutStop = renderCarrierPanel({
      route: expedition.CarrierRouteResult.createFrom({
        origin: 'Sol',
        destination: '',
        carrierType: 'player',
        manualMode: true,
        jumps: [],
      }),
    })

    expect(manualWithoutStop).toContain('Sol')
    expect(manualWithoutStop).toContain('???')
    expect(manualWithoutStop).toContain('MARK FC STOP ARRIVAL')
    expect(manualWithoutStop).toContain(
      'No next Fleet Carrier stop designated in the expedition plan',
    )
    expect(manualWithoutStop).toMatch(/disabled=""[^>]*>MARK FC STOP ARRIVAL/)

    const plottedSpanshRoute = expedition.CarrierRouteResult.createFrom({
      origin: 'Sol',
      destination: 'Colonia',
      carrierType: 'player',
      manualMode: false,
      jumps: [
        {
          systemName: 'Rohini',
          distanceLy: 495.2,
          tritiumUsed: 68,
          fuelInTank: 932,
          hasIcyRing: true,
        },
        {
          systemName: 'Colonia',
          distanceLy: 310.4,
          tritiumUsed: 44,
          fuelInTank: 888,
          hasIcyRing: false,
        },
      ],
    })

    const spanshModeHtml = renderCarrierPanel({
      route: plottedSpanshRoute,
      jumpIndex: 0,
      manualMode: false,
      hasNextFcStop: true,
      nextFcStopSystem: 'Beagle Point',
    })

    expect(spanshModeHtml).toContain('Colonia')
    expect(spanshModeHtml).not.toContain('Beagle Point')
    expect(spanshModeHtml).toContain('805.6 Ly')
    expect(spanshModeHtml).toContain('~112 t')
    expect(spanshModeHtml).toContain('>COMPLETE JUMP #1</button>')

    const spanshFinalJumpHtml = renderCarrierPanel({
      route: plottedSpanshRoute,
      jumpIndex: 1,
      manualMode: false,
      hasNextFcStop: true,
      nextFcStopSystem: 'Colonia',
    })

    expect(spanshFinalJumpHtml).toContain(
      'title="Complete final carrier jump to Colonia and mark waypoint arrival"',
    )
    expect(spanshFinalJumpHtml).toContain('>COMPLETE JUMP #2</button>')
    expect(spanshFinalJumpHtml).not.toContain('CARRIER ARRIVED')

    const spanshAllRecordedHtml = renderCarrierPanel({
      route: plottedSpanshRoute,
      jumpIndex: 2,
      depotTritium: 6,
      manualMode: false,
      hasNextFcStop: true,
      nextFcStopSystem: 'Colonia',
    })
    expect(spanshAllRecordedHtml).toContain('0 JUMPS')
    expect(spanshAllRecordedHtml).toContain('0.0 Ly')
    expect(spanshAllRecordedHtml).toContain('~0 t')
    expect(spanshAllRecordedHtml).toContain('>MARK FC STOP ARRIVAL</button>')
    expect(spanshAllRecordedHtml).not.toMatch(/disabled=""[^>]*>MARK FC STOP ARRIVAL/)

    const manualArrivalBusyHtml = renderCarrierPanel({
      route: expedition.CarrierRouteResult.createFrom({
        origin: 'Sol',
        destination: 'Colonia',
        carrierType: 'player',
        manualMode: true,
        jumps: [],
      }),
      hasNextFcStop: true,
      nextFcStopSystem: 'Colonia',
      loggingJump: true,
    })
    expect(manualArrivalBusyHtml).toMatch(/disabled=""[^>]*>MARK FC STOP ARRIVAL/)
  })
})

function buildCarrierPlan(
  expeditionId: number,
  stopRowId: number,
  stopSystem: string,
  activeCarrierRoute?: expedition.ActiveCarrierRouteState,
) {
  return expedition.PlanView.createFrom({
    expedition: { id: expeditionId, name: `Expedition ${expeditionId}`, status: 'active' },
    nextFcStopRowId: stopRowId,
    routedCarrierStopRowId: activeCarrierRoute ? stopRowId : undefined,
    activeCarrierRoute,
    rows: [
      {
        row: {
          id: stopRowId - 1,
          expeditionId,
          rowType: 'waypoint',
          systemName: 'Sol',
          visitedAt: '3312-01-01T00:00:00Z',
        },
        number: 1,
        visitState: 'visited',
      },
      {
        row: {
          id: stopRowId,
          expeditionId,
          rowType: 'waypoint',
          systemName: stopSystem,
          fcStop: true,
          fcArrived: false,
        },
        number: 2,
        visitState: 'future',
      },
    ],
  })
}

describe('useCarrierRoute backend RPC coordination and cross-DB guards', () => {
  it('cancels in-flight Spansh plots on modal close or expedition switch and prevents cross-database manual draft leaks', async () => {
    let resolveFirstPlot!: (state: expedition.ActiveCarrierRouteState | null) => void
    const { spies } = installWailsAppMock({
      CancelCarrierRoutePlot: async () => {},
      SaveCarrierManualDraftInputs: async () => {},
      PrepareCarrierRoutePrompt: async (_expId: number, targetRowId: number) =>
        expedition.CarrierRoutePromptDefaults.createFrom({
          targetRowId,
          origin: 'Sol',
          destination: targetRowId === 11 ? 'Colonia' : 'Beagle Point',
          carrierType: 'player',
          capacityUsed: 4000,
          depotTritium: 900,
        }),
      StartCarrierRoute: async () => null,
      OpenManualCarrierRoute: async () => null,
    })
    spies.StartCarrierRoute.mockImplementationOnce(
      () =>
        new Promise<expedition.ActiveCarrierRouteState | null>((resolve) => {
          resolveFirstPlot = resolve
        }),
    )

    const planOne = buildCarrierPlan(1, 11, 'Colonia')
    const planTwo = buildCarrierPlan(2, 21, 'Beagle Point')
    const busyMessages: Array<string | null> = []

    const mountedHook = renderHook(
      ({
        externalPlan,
        dbPath = '/db/alpha.sqlite',
        manualRequestCount = 0,
      }: {
        externalPlan: expedition.PlanView
        dbPath?: string
        manualRequestCount?: number
      }) => {
        const [currentPlan, setCurrentPlan] = useState(externalPlan)
        useEffect(() => {
          setCurrentPlan(externalPlan)
        }, [externalPlan])
        return useCarrierRoute({
          plan: currentPlan,
          dbPath,
          manualPlotterRequestCount: manualRequestCount,
          onBusyChange: (message) => busyMessages.push(message),
          onPlanOptimisticUpdate: setCurrentPlan,
        })
      },
      { externalPlan: planOne },
    )

    await act(async () => {
      await Promise.resolve()
    })

    await act(async () => {
      await mountedHook.getResult().openCarrierRoutePrompt(1, planOne.rows[1].row)
    })
    expect(mountedHook.getResult().routePrompt?.destination).toBe('Colonia')

    let pendingPlotPromise!: Promise<void>
    act(() => {
      pendingPlotPromise = mountedHook.getResult().handleStartCarrierRoute()
    })
    expect(busyMessages.at(-1)).toBe('PLOTTING SPANSH CARRIER ROUTE')

    // Switching expeditions cancels the backend plot immediately and discards late resolution.
    await act(async () => {
      mountedHook.rerender({ externalPlan: planTwo })
      await Promise.resolve()
    })
    expect(spies.CancelCarrierRoutePlot).toHaveBeenCalledTimes(1)
    expect(busyMessages.at(-1)).toBe(null)
    expect(mountedHook.getResult().routePrompt).toBeNull()

    await act(async () => {
      resolveFirstPlot(
        expedition.ActiveCarrierRouteState.createFrom({
          expeditionId: 1,
          targetRowId: 11,
          route: { origin: 'Sol', destination: 'Colonia', jumps: [] },
        }),
      )
      await pendingPlotPromise
    })
    expect(mountedHook.getResult().activeCarrierRoute).toBeNull()

    // Closing the modal mid-plot also calls CancelCarrierRoutePlot.
    let resolveSecondPlot!: (state: expedition.ActiveCarrierRouteState | null) => void
    spies.StartCarrierRoute.mockImplementationOnce(
      () =>
        new Promise<expedition.ActiveCarrierRouteState | null>((resolve) => {
          resolveSecondPlot = resolve
        }),
    )
    await act(async () => {
      await mountedHook.getResult().openCarrierRoutePrompt(1, planTwo.rows[1].row)
    })
    act(() => {
      pendingPlotPromise = mountedHook.getResult().handleStartCarrierRoute()
    })
    act(() => {
      mountedHook.getResult().handleCloseRoutePrompt()
    })
    expect(spies.CancelCarrierRoutePlot).toHaveBeenCalledTimes(2)
    await act(async () => {
      resolveSecondPlot(null)
      await pendingPlotPromise
    })
    expect(mountedHook.getResult().activeCarrierRoute).toBeNull()

    // External backend cancellation returning null resets prompt loading to false without leaving modal stuck.
    let resolveExternallyCancelledPlot!: (state: expedition.ActiveCarrierRouteState | null) => void
    spies.StartCarrierRoute.mockImplementationOnce(
      () =>
        new Promise<expedition.ActiveCarrierRouteState | null>((resolve) => {
          resolveExternallyCancelledPlot = resolve
        }),
    )
    await act(async () => {
      await mountedHook.getResult().openCarrierRoutePrompt(1, planTwo.rows[1].row)
    })
    act(() => {
      pendingPlotPromise = mountedHook.getResult().handleStartCarrierRoute()
    })
    expect(mountedHook.getResult().routePrompt?.loading).toBe(true)
    await act(async () => {
      resolveExternallyCancelledPlot(null)
      await pendingPlotPromise
    })
    expect(mountedHook.getResult().routePrompt).not.toBeNull()
    expect(mountedHook.getResult().routePrompt?.loading).toBe(false)
    act(() => {
      mountedHook.getResult().handleCloseRoutePrompt()
    })

    // Open manual plotter on expedition 2, queue a draft edit, and switch to expedition 1 on the SAME dbPath: flushes draft.
    spies.OpenManualCarrierRoute.mockResolvedValueOnce(
      expedition.ActiveCarrierRouteState.createFrom({
        expeditionId: 2,
        targetRowId: 21,
        manualMode: true,
        route: { origin: 'Sol', destination: 'Beagle Point', manualMode: true, jumps: [] },
      }),
    )
    await act(async () => {
      mountedHook.rerender({ externalPlan: planTwo, manualRequestCount: 1 })
      await Promise.resolve()
    })
    expect(mountedHook.getResult().activeCarrierRoute).not.toBeNull()

    act(() => {
      mountedHook.getResult().handleChangeManualDistance('325.5')
    })
    expect(spies.SaveCarrierManualDraftInputs).not.toHaveBeenCalled()

    const manualStateOne = expedition.ActiveCarrierRouteState.createFrom({
      expeditionId: 1,
      targetRowId: 11,
      manualMode: true,
      route: { origin: 'Sol', destination: 'Colonia', manualMode: true, jumps: [] },
    })
    await act(async () => {
      mountedHook.rerender({
        externalPlan: buildCarrierPlan(1, 11, 'Colonia', manualStateOne),
        dbPath: '/db/alpha.sqlite',
        manualRequestCount: 1,
      })
      await Promise.resolve()
    })
    expect(spies.SaveCarrierManualDraftInputs).toHaveBeenCalledTimes(1)
    expect(spies.SaveCarrierManualDraftInputs).toHaveBeenCalledWith(2, '325.5', '')
    spies.SaveCarrierManualDraftInputs.mockClear()

    // Queue another draft edit on DB alpha (expeditionId === 1), then switch dbPath to DB beta (also expeditionId === 1): MUST NOT flush!
    act(() => {
      mountedHook.getResult().handleChangeManualDistance('999.9')
    })
    await act(async () => {
      mountedHook.rerender({
        externalPlan: buildCarrierPlan(1, 11, 'Colonia'),
        dbPath: '/db/beta.sqlite',
        manualRequestCount: 1,
      })
      await Promise.resolve()
    })
    expect(spies.SaveCarrierManualDraftInputs).not.toHaveBeenCalled()
    expect(mountedHook.getResult().manualDistanceInput).toBe('')
  })

  it('coordinates Spansh hop completion, depot refill, manual switch, manual jump logging, arrival error recovery, and route cancel', async () => {
    vi.useFakeTimers()

    const initialSpanshState = expedition.ActiveCarrierRouteState.createFrom({
      expeditionId: 1,
      targetRowId: 12,
      jumpIdx: 0,
      manualMode: false,
      route: {
        origin: 'Sol',
        destination: 'Colonia',
        carrierType: 'player',
        capacityUsed: 5000,
        depotTritium: 500,
        manualMode: false,
        jumps: [
          { systemName: 'Rohini', distanceLy: 420, tritiumUsed: 60, fuelInTank: 440 },
          { systemName: 'Colonia', distanceLy: 400, tritiumUsed: 44, fuelInTank: 396 },
        ],
      },
    })
    const initialPlan = expedition.PlanView.createFrom({
      expedition: { id: 1, name: 'Expedition 1', status: 'active' },
      nextFcStopRowId: 11,
      routedCarrierStopRowId: 12,
      activeCarrierRoute: initialSpanshState,
      rows: [
        { row: { id: 10, expeditionId: 1, rowType: 'waypoint', systemName: 'Sol', visitedAt: '3312-01-01T00:00:00Z' }, number: 1, visitState: 'visited' },
        { row: { id: 11, expeditionId: 1, rowType: 'waypoint', systemName: 'Rohini', fcStop: true, fcArrived: false }, number: 2, visitState: 'future' },
        { row: { id: 12, expeditionId: 1, rowType: 'waypoint', systemName: 'Colonia', fcStop: true, fcArrived: false }, number: 3, visitState: 'future' },
      ],
    })

    let rejectFirstArrival!: (reason: Error) => void
    const { spies } = installWailsAppMock({
      RefillCarrierDepot: async () => null,
      CompleteSpanshCarrierJump: async () => null,
      SwitchCarrierRouteToManual: async () => null,
      LogManualCarrierJump: async () => null,
      SaveCarrierManualDraftInputs: async () => {},
      ClearActiveCarrierRouteWithUndo: async () => {},
      CompleteCarrierRouteArrival: async () => {},
    })
    spies.RefillCarrierDepot.mockResolvedValueOnce(
      expedition.ActiveCarrierRouteState.createFrom({
        ...initialSpanshState,
        route: { ...initialSpanshState.route, depotTritium: 1000, capacityUsed: 4500 },
      }),
    )
    spies.CompleteSpanshCarrierJump.mockResolvedValueOnce(
      expedition.ActiveCarrierRouteState.createFrom({
        ...initialSpanshState,
        jumpIdx: 1,
        route: { ...initialSpanshState.route, depotTritium: 940 },
      }),
    )
    spies.SwitchCarrierRouteToManual.mockResolvedValueOnce(
      expedition.ActiveCarrierRouteState.createFrom({
        ...initialSpanshState,
        jumpIdx: 1,
        manualMode: true,
        manualDistInput: '400',
        manualTritInput: '44',
      }),
    )
    spies.LogManualCarrierJump.mockResolvedValueOnce(
      expedition.ActiveCarrierRouteState.createFrom({
        ...initialSpanshState,
        jumpIdx: 1,
        manualMode: true,
        manualLegJumps: 1,
        manualLegDist: 400,
        manualLegTrit: 44,
      }),
    )
    spies.CompleteCarrierRouteArrival
      .mockImplementationOnce(
        () =>
          new Promise<void>((_, reject) => {
            rejectFirstArrival = reject
          }),
      )
      .mockResolvedValueOnce(undefined)

    let refreshPlanCalls = 0
    let replaceExternalPlan!: (nextPlan: expedition.PlanView) => void
    const mountedHook = renderHook(() => {
      const [currentPlan, setCurrentPlan] = useState(initialPlan)
      replaceExternalPlan = setCurrentPlan
      return useCarrierRoute({
        plan: currentPlan,
        onBusyChange: () => {},
        onPlanOptimisticUpdate: setCurrentPlan,
        onRefreshPlan: () => {
          refreshPlanCalls += 1
        },
      })
    })
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    try {
      await act(async () => {
        await Promise.resolve()
      })
      expect(mountedHook.getResult().nextFcStopEntry?.row.id).toBe(11)
      expect(mountedHook.getResult().routedCarrierStopIndex).toBe(2)

      await act(async () => {
        mountedHook.getResult().handleRefillDepot()
        await Promise.resolve()
      })
      expect(spies.RefillCarrierDepot).toHaveBeenCalledWith(1)
      expect(mountedHook.getResult().activeCarrierRoute?.depotTritium).toBe(1000)

      await act(async () => {
        await mountedHook.getResult().handleCompleteSpanshJump()
      })
      expect(spies.CompleteSpanshCarrierJump).toHaveBeenCalledWith(1)
      expect(mountedHook.getResult().carrierJumpIndex).toBe(1)

      await act(async () => {
        mountedHook.getResult().switchActiveRouteToManualMode()
        await Promise.resolve()
      })
      expect(spies.SwitchCarrierRouteToManual).toHaveBeenCalledWith(1)
      expect(mountedHook.getResult().isManualCarrierRoute).toBe(true)
      expect(mountedHook.getResult().manualDistanceInput).toBe('400')
      expect(mountedHook.getResult().manualTritiumInput).toBe('44')
      expect(mountedHook.getResult().routedCarrierStopIndex).toBe(1)

      await act(async () => {
        await mountedHook.getResult().handleLogManualJump()
      })
      expect(spies.LogManualCarrierJump).toHaveBeenCalledWith(1, 400, 44)
      expect(mountedHook.getResult().manualLegJumps).toBe(1)
      expect(mountedHook.getResult().manualDistanceInput).toBe('')

      // Queue debounced draft input, start arrival (cancels pending timer and blocks concurrent clicks), reject, then succeed.
      act(() => {
        mountedHook.getResult().handleChangeManualDistance('180')
      })
      let firstArrivalPromise!: Promise<void>
      act(() => {
        firstArrivalPromise = mountedHook.getResult().handleCarrierArriveAtWaypoint()
      })
      expect(mountedHook.getResult().loggingJump).toBe(true)
      await act(async () => {
        await mountedHook.getResult().handleCarrierArriveAtWaypoint()
        vi.advanceTimersByTime(300)
      })
      expect(spies.CompleteCarrierRouteArrival).toHaveBeenCalledTimes(1)
      expect(spies.CompleteCarrierRouteArrival).toHaveBeenCalledWith(1, 0, 0, 0)
      expect(spies.SaveCarrierManualDraftInputs).not.toHaveBeenCalled()

      await act(async () => {
        rejectFirstArrival(new Error('sqlite busy'))
        await firstArrivalPromise
      })
      expect(mountedHook.getResult().loggingJump).toBe(false)
      expect(mountedHook.getResult().activeCarrierRoute).not.toBeNull()

      // In-flight SaveCarrierManualDraftInputs protects newer user edits from stale backend plan updates.
      let resolveDraftSave!: () => void
      spies.SaveCarrierManualDraftInputs.mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            resolveDraftSave = resolve
          }),
      )
      act(() => {
        mountedHook.getResult().handleChangeManualDistance('275')
      })
      act(() => {
        vi.advanceTimersByTime(250)
      })
      expect(spies.SaveCarrierManualDraftInputs).toHaveBeenCalledWith(1, '275', '')
      act(() => {
        replaceExternalPlan(
          expedition.PlanView.createFrom({
            ...initialPlan,
            activeCarrierRoute: {
              ...initialSpanshState,
              manualMode: true,
              manualDistInput: 'stale-old',
              manualTritInput: '',
            },
          }),
        )
      })
      expect(mountedHook.getResult().manualDistanceInput).toBe('275')
      await act(async () => {
        resolveDraftSave()
        await Promise.resolve()
      })

      // If GetPlan settles a newer PlanView while RefillCarrierDepot is in flight, stale optimistic clone is skipped.
      let resolveLateRefill!: (state: expedition.ActiveCarrierRouteState | null) => void
      spies.RefillCarrierDepot.mockImplementationOnce(
        () =>
          new Promise<expedition.ActiveCarrierRouteState | null>((resolve) => {
            resolveLateRefill = resolve
          }),
      )
      act(() => {
        mountedHook.getResult().handleRefillDepot()
      })
      const authoritativeBackendPlan = expedition.PlanView.createFrom({
        ...initialPlan,
        waypointCount: 99,
        activeCarrierRoute: {
          ...initialSpanshState,
          manualMode: true,
          route: { ...initialSpanshState.route, depotTritium: 1000, capacityUsed: 4200 },
        },
      })
      act(() => {
        replaceExternalPlan(authoritativeBackendPlan)
      })
      await act(async () => {
        resolveLateRefill(
          expedition.ActiveCarrierRouteState.createFrom({
            ...initialSpanshState,
            manualMode: true,
            route: { ...initialSpanshState.route, depotTritium: 777 },
          }),
        )
        await Promise.resolve()
      })
      expect(mountedHook.getResult().activeCarrierRoute?.depotTritium).toBe(1000)

      // Cancel route calls ClearActiveCarrierRouteWithUndo, clears optimistic route immediately, and rolls back via onRefreshPlan on rejection.
      spies.ClearActiveCarrierRouteWithUndo.mockRejectedValueOnce(new Error('cancel failed'))
      await act(async () => {
        mountedHook.getResult().handleCancelCarrierRoute()
        await Promise.resolve()
      })
      expect(spies.ClearActiveCarrierRouteWithUndo).toHaveBeenCalledWith(1)
      expect(mountedHook.getResult().activeCarrierRoute).toBeNull()
      expect(refreshPlanCalls).toBe(1)
    } finally {
      errorSpy.mockRestore()
    }
  })
})
