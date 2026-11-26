// @vitest-environment happy-dom
import { act, createElement, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { renderToString } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { expedition } from '../../api/models'

const mockPrepareCarrierRoutePrompt = vi.fn()
const mockStartCarrierRoute = vi.fn()
const mockCancelCarrierRoutePlot = vi.fn()
const mockOpenManualCarrierRoute = vi.fn()
const mockRefillCarrierDepot = vi.fn()
const mockSwitchCarrierRouteToManual = vi.fn()
const mockCompleteSpanshCarrierJump = vi.fn()
const mockLogManualCarrierJump = vi.fn()
const mockSaveCarrierManualDraftInputs = vi.fn()
const mockCompleteCarrierRouteArrival = vi.fn()
const mockClearActiveCarrierRouteWithUndo = vi.fn()

vi.mock('../../api/backend', () => ({
  PrepareCarrierRoutePrompt: (...args: unknown[]) => mockPrepareCarrierRoutePrompt(...args),
  StartCarrierRoute: (...args: unknown[]) => mockStartCarrierRoute(...args),
  CancelCarrierRoutePlot: (...args: unknown[]) => mockCancelCarrierRoutePlot(...args),
  OpenManualCarrierRoute: (...args: unknown[]) => mockOpenManualCarrierRoute(...args),
  RefillCarrierDepot: (...args: unknown[]) => mockRefillCarrierDepot(...args),
  SwitchCarrierRouteToManual: (...args: unknown[]) => mockSwitchCarrierRouteToManual(...args),
  CompleteSpanshCarrierJump: (...args: unknown[]) => mockCompleteSpanshCarrierJump(...args),
  LogManualCarrierJump: (...args: unknown[]) => mockLogManualCarrierJump(...args),
  SaveCarrierManualDraftInputs: (...args: unknown[]) => mockSaveCarrierManualDraftInputs(...args),
  CompleteCarrierRouteArrival: (...args: unknown[]) => mockCompleteCarrierRouteArrival(...args),
  ClearActiveCarrierRouteWithUndo: (...args: unknown[]) =>
    mockClearActiveCarrierRouteWithUndo(...args),
}))

import { CarrierRouteBottomPanel } from './CarrierRouteBottomPanel'
import { CarrierRouteModal } from './CarrierRouteModal'
import { useCarrierRoute } from './useCarrierRoute'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

describe('CarrierRoute UI Components', () => {
  it('CarrierRouteModal omits Manual Entry Mode and keeps QUERYING SPANSH... single-line', () => {
    const renderCarrierModal = (
      overrides: Partial<Parameters<typeof CarrierRouteModal>[0]['prompt']> = {},
    ) =>
      renderToString(
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
    expect(idleHtml).toContain('whitespace-nowrap')

    const loadingHtml = renderCarrierModal({ loading: true })
    expect(loadingHtml).toContain('QUERYING SPANSH...')
    expect(loadingHtml).toContain('whitespace-nowrap')
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
      renderToString(
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
    expect(spanshModeHtml).toMatch(/~(?:<!-- -->)?112(?:<!-- -->)? t/)
    expect(spanshModeHtml).toContain('class="ed-btn-primary">COMPLETE JUMP #1</button>')

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
    expect(spanshFinalJumpHtml).toContain('class="ed-btn-cyan">COMPLETE JUMP #2</button>')
    expect(spanshFinalJumpHtml).not.toContain('CARRIER ARRIVED')

    const spanshAllRecordedHtml = renderCarrierPanel({
      route: plottedSpanshRoute,
      jumpIndex: 2,
      depotTritium: 6,
      manualMode: false,
      hasNextFcStop: true,
      nextFcStopSystem: 'Colonia',
    })
    expect(spanshAllRecordedHtml).toMatch(/0(?:<!-- -->|\s)+JUMPS/)
    expect(spanshAllRecordedHtml).toContain('0.0 Ly')
    expect(spanshAllRecordedHtml).toMatch(/~(?:<!-- -->)?0(?:<!-- -->)? t/)
    expect(spanshAllRecordedHtml).toContain('class="ed-btn-cyan">MARK FC STOP ARRIVAL</button>')
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
  afterEach(() => {
    vi.clearAllMocks()
    vi.useRealTimers()
  })

  it('cancels in-flight Spansh plots on modal close or expedition switch and prevents cross-database manual draft leaks', async () => {
    mockCancelCarrierRoutePlot.mockResolvedValue(undefined)
    mockSaveCarrierManualDraftInputs.mockResolvedValue(undefined)
    mockPrepareCarrierRoutePrompt.mockImplementation(async (_expId: number, targetRowId: number) =>
      expedition.CarrierRoutePromptDefaults.createFrom({
        targetRowId,
        origin: 'Sol',
        destination: targetRowId === 11 ? 'Colonia' : 'Beagle Point',
        carrierType: 'player',
        capacityUsed: 4000,
        depotTritium: 900,
      }),
    )

    let resolveFirstPlot!: (state: expedition.ActiveCarrierRouteState | null) => void
    mockStartCarrierRoute.mockImplementationOnce(
      () =>
        new Promise<expedition.ActiveCarrierRouteState | null>((resolve) => {
          resolveFirstPlot = resolve
        }),
    )

    const planOne = buildCarrierPlan(1, 11, 'Colonia')
    const planTwo = buildCarrierPlan(2, 21, 'Beagle Point')
    const busyMessages: Array<string | null> = []
    let latestHook!: ReturnType<typeof useCarrierRoute>

    function HookHarness({
      externalPlan,
      dbPath = '/db/alpha.sqlite',
      manualRequestCount = 0,
    }: {
      externalPlan: expedition.PlanView
      dbPath?: string
      manualRequestCount?: number
    }) {
      const [currentPlan, setCurrentPlan] = useState(externalPlan)
      useEffect(() => {
        setCurrentPlan(externalPlan)
      }, [externalPlan])
      latestHook = useCarrierRoute({
        plan: currentPlan,
        dbPath,
        manualPlotterRequestCount: manualRequestCount,
        onBusyChange: (message) => busyMessages.push(message),
        onPlanOptimisticUpdate: setCurrentPlan,
      })
      return null
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(createElement(HookHarness, { externalPlan: planOne }))
        await Promise.resolve()
      })

      await act(async () => {
        await latestHook.openCarrierRoutePrompt(1, planOne.rows[1].row)
      })
      expect(latestHook.routePrompt?.destination).toBe('Colonia')

      let pendingPlotPromise!: Promise<void>
      act(() => {
        pendingPlotPromise = latestHook.handleStartCarrierRoute()
      })
      expect(busyMessages.at(-1)).toBe('PLOTTING SPANSH CARRIER ROUTE')

      // Switching expeditions cancels the backend plot immediately and discards late resolution.
      await act(async () => {
        root.render(createElement(HookHarness, { externalPlan: planTwo }))
        await Promise.resolve()
      })
      expect(mockCancelCarrierRoutePlot).toHaveBeenCalledTimes(1)
      expect(busyMessages.at(-1)).toBe(null)
      expect(latestHook.routePrompt).toBeNull()

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
      expect(latestHook.activeCarrierRoute).toBeNull()

      // Closing the modal mid-plot also calls CancelCarrierRoutePlot.
      let resolveSecondPlot!: (state: expedition.ActiveCarrierRouteState | null) => void
      mockStartCarrierRoute.mockImplementationOnce(
        () =>
          new Promise<expedition.ActiveCarrierRouteState | null>((resolve) => {
            resolveSecondPlot = resolve
          }),
      )
      await act(async () => {
        await latestHook.openCarrierRoutePrompt(1, planTwo.rows[1].row)
      })
      act(() => {
        pendingPlotPromise = latestHook.handleStartCarrierRoute()
      })
      act(() => {
        latestHook.handleCloseRoutePrompt()
      })
      expect(mockCancelCarrierRoutePlot).toHaveBeenCalledTimes(2)
      await act(async () => {
        resolveSecondPlot(null)
        await pendingPlotPromise
      })
      expect(latestHook.activeCarrierRoute).toBeNull()

      // External backend cancellation returning null resets prompt loading to false without leaving modal stuck.
      let resolveExternallyCancelledPlot!: (state: expedition.ActiveCarrierRouteState | null) => void
      mockStartCarrierRoute.mockImplementationOnce(
        () =>
          new Promise<expedition.ActiveCarrierRouteState | null>((resolve) => {
            resolveExternallyCancelledPlot = resolve
          }),
      )
      await act(async () => {
        await latestHook.openCarrierRoutePrompt(1, planTwo.rows[1].row)
      })
      act(() => {
        pendingPlotPromise = latestHook.handleStartCarrierRoute()
      })
      expect(latestHook.routePrompt?.loading).toBe(true)
      await act(async () => {
        resolveExternallyCancelledPlot(null)
        await pendingPlotPromise
      })
      expect(latestHook.routePrompt).not.toBeNull()
      expect(latestHook.routePrompt?.loading).toBe(false)
      act(() => {
        latestHook.handleCloseRoutePrompt()
      })

      // Open manual plotter on expedition 2, queue a draft edit, and switch to expedition 1 on the SAME dbPath: flushes draft.
      mockOpenManualCarrierRoute.mockResolvedValueOnce(
        expedition.ActiveCarrierRouteState.createFrom({
          expeditionId: 2,
          targetRowId: 21,
          manualMode: true,
          route: { origin: 'Sol', destination: 'Beagle Point', manualMode: true, jumps: [] },
        }),
      )
      await act(async () => {
        root.render(
          createElement(HookHarness, { externalPlan: planTwo, manualRequestCount: 1 }),
        )
        await Promise.resolve()
      })
      expect(latestHook.activeCarrierRoute).not.toBeNull()

      act(() => {
        latestHook.handleChangeManualDistance('325.5')
      })
      expect(mockSaveCarrierManualDraftInputs).not.toHaveBeenCalled()

      const manualStateOne = expedition.ActiveCarrierRouteState.createFrom({
        expeditionId: 1,
        targetRowId: 11,
        manualMode: true,
        route: { origin: 'Sol', destination: 'Colonia', manualMode: true, jumps: [] },
      })
      await act(async () => {
        root.render(
          createElement(HookHarness, {
            externalPlan: buildCarrierPlan(1, 11, 'Colonia', manualStateOne),
            dbPath: '/db/alpha.sqlite',
            manualRequestCount: 1,
          }),
        )
        await Promise.resolve()
      })
      expect(mockSaveCarrierManualDraftInputs).toHaveBeenCalledTimes(1)
      expect(mockSaveCarrierManualDraftInputs).toHaveBeenCalledWith(2, '325.5', '')
      mockSaveCarrierManualDraftInputs.mockClear()

      // Queue another draft edit on DB alpha (expeditionId === 1), then switch dbPath to DB beta (also expeditionId === 1): MUST NOT flush!
      act(() => {
        latestHook.handleChangeManualDistance('999.9')
      })
      await act(async () => {
        root.render(
          createElement(HookHarness, {
            externalPlan: buildCarrierPlan(1, 11, 'Colonia'),
            dbPath: '/db/beta.sqlite',
            manualRequestCount: 1,
          }),
        )
        await Promise.resolve()
      })
      expect(mockSaveCarrierManualDraftInputs).not.toHaveBeenCalled()
      expect(latestHook.manualDistanceInput).toBe('')
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })

  it('coordinates Spansh hop completion, depot refill, manual switch, manual jump logging, arrival error recovery, and route cancel', async () => {
    vi.useFakeTimers()
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

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

    mockRefillCarrierDepot.mockResolvedValueOnce(
      expedition.ActiveCarrierRouteState.createFrom({
        ...initialSpanshState,
        route: { ...initialSpanshState.route, depotTritium: 1000, capacityUsed: 4500 },
      }),
    )
    mockCompleteSpanshCarrierJump.mockResolvedValueOnce(
      expedition.ActiveCarrierRouteState.createFrom({
        ...initialSpanshState,
        jumpIdx: 1,
        route: { ...initialSpanshState.route, depotTritium: 940 },
      }),
    )
    mockSwitchCarrierRouteToManual.mockResolvedValueOnce(
      expedition.ActiveCarrierRouteState.createFrom({
        ...initialSpanshState,
        jumpIdx: 1,
        manualMode: true,
        manualDistInput: '400',
        manualTritInput: '44',
      }),
    )
    mockLogManualCarrierJump.mockResolvedValueOnce(
      expedition.ActiveCarrierRouteState.createFrom({
        ...initialSpanshState,
        jumpIdx: 1,
        manualMode: true,
        manualLegJumps: 1,
        manualLegDist: 400,
        manualLegTrit: 44,
      }),
    )
    mockSaveCarrierManualDraftInputs.mockResolvedValue(undefined)
    mockClearActiveCarrierRouteWithUndo.mockResolvedValue(undefined)

    let rejectFirstArrival!: (reason: Error) => void
    mockCompleteCarrierRouteArrival
      .mockImplementationOnce(
        () =>
          new Promise<void>((_, reject) => {
            rejectFirstArrival = reject
          }),
      )
      .mockResolvedValueOnce(undefined)

    let refreshPlanCalls = 0
    let latestHook!: ReturnType<typeof useCarrierRoute>
    let replaceExternalPlan!: (nextPlan: expedition.PlanView) => void
    function HookHarness() {
      const [currentPlan, setCurrentPlan] = useState(initialPlan)
      replaceExternalPlan = setCurrentPlan
      latestHook = useCarrierRoute({
        plan: currentPlan,
        onBusyChange: () => {},
        onPlanOptimisticUpdate: setCurrentPlan,
        onRefreshPlan: () => {
          refreshPlanCalls += 1
        },
      })
      return null
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(createElement(HookHarness))
        await Promise.resolve()
      })
      expect(latestHook.nextFcStopEntry?.row.id).toBe(11)
      expect(latestHook.routedCarrierStopIndex).toBe(2)

      await act(async () => {
        latestHook.handleRefillDepot()
        await Promise.resolve()
      })
      expect(mockRefillCarrierDepot).toHaveBeenCalledWith(1)
      expect(latestHook.activeCarrierRoute?.depotTritium).toBe(1000)

      await act(async () => {
        await latestHook.handleCompleteSpanshJump()
      })
      expect(mockCompleteSpanshCarrierJump).toHaveBeenCalledWith(1)
      expect(latestHook.carrierJumpIndex).toBe(1)

      await act(async () => {
        latestHook.switchActiveRouteToManualMode()
        await Promise.resolve()
      })
      expect(mockSwitchCarrierRouteToManual).toHaveBeenCalledWith(1)
      expect(latestHook.isManualCarrierRoute).toBe(true)
      expect(latestHook.manualDistanceInput).toBe('400')
      expect(latestHook.manualTritiumInput).toBe('44')
      expect(latestHook.routedCarrierStopIndex).toBe(1)

      await act(async () => {
        await latestHook.handleLogManualJump()
      })
      expect(mockLogManualCarrierJump).toHaveBeenCalledWith(1, 400, 44)
      expect(latestHook.manualLegJumps).toBe(1)
      expect(latestHook.manualDistanceInput).toBe('')

      // Queue debounced draft input, start arrival (cancels pending timer and blocks concurrent clicks), reject, then succeed.
      act(() => {
        latestHook.handleChangeManualDistance('180')
      })
      let firstArrivalPromise!: Promise<void>
      act(() => {
        firstArrivalPromise = latestHook.handleCarrierArriveAtWaypoint()
      })
      expect(latestHook.loggingJump).toBe(true)
      await act(async () => {
        await latestHook.handleCarrierArriveAtWaypoint()
        vi.advanceTimersByTime(300)
      })
      expect(mockCompleteCarrierRouteArrival).toHaveBeenCalledTimes(1)
      expect(mockCompleteCarrierRouteArrival).toHaveBeenCalledWith(1, 0, 0, 0)
      expect(mockSaveCarrierManualDraftInputs).not.toHaveBeenCalled()

      await act(async () => {
        rejectFirstArrival(new Error('sqlite busy'))
        await firstArrivalPromise
      })
      expect(latestHook.loggingJump).toBe(false)
      expect(latestHook.activeCarrierRoute).not.toBeNull()

      // In-flight SaveCarrierManualDraftInputs protects newer user edits from stale backend plan updates.
      let resolveDraftSave!: () => void
      mockSaveCarrierManualDraftInputs.mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            resolveDraftSave = resolve
          }),
      )
      act(() => {
        latestHook.handleChangeManualDistance('275')
      })
      act(() => {
        vi.advanceTimersByTime(250)
      })
      expect(mockSaveCarrierManualDraftInputs).toHaveBeenCalledWith(1, '275', '')
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
      expect(latestHook.manualDistanceInput).toBe('275')
      await act(async () => {
        resolveDraftSave()
        await Promise.resolve()
      })

      // If GetPlan settles a newer PlanView while RefillCarrierDepot is in flight, stale optimistic clone is skipped.
      let resolveLateRefill!: (state: expedition.ActiveCarrierRouteState | null) => void
      mockRefillCarrierDepot.mockImplementationOnce(
        () =>
          new Promise<expedition.ActiveCarrierRouteState | null>((resolve) => {
            resolveLateRefill = resolve
          }),
      )
      act(() => {
        latestHook.handleRefillDepot()
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
      expect(latestHook.activeCarrierRoute?.depotTritium).toBe(1000)

      // Cancel route calls ClearActiveCarrierRouteWithUndo, clears optimistic route immediately, and rolls back via onRefreshPlan on rejection.
      mockClearActiveCarrierRouteWithUndo.mockRejectedValueOnce(new Error('cancel failed'))
      await act(async () => {
        latestHook.handleCancelCarrierRoute()
        await Promise.resolve()
      })
      expect(mockClearActiveCarrierRouteWithUndo).toHaveBeenCalledWith(1)
      expect(latestHook.activeCarrierRoute).toBeNull()
      expect(refreshPlanCalls).toBe(1)
    } finally {
      errorSpy.mockRestore()
      act(() => root.unmount())
      container.remove()
    }
  })
})
