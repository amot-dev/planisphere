// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { renderToString } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { expedition } from '../../api/models'

const mockPlotCarrierRoute = vi.fn()
const mockRecordCarrierJumpAndSaveRoute = vi.fn()
const mockCompleteCarrierRouteArrival = vi.fn()
const mockGetActiveCarrierRoute = vi.fn()
const mockSaveActiveCarrierRoute = vi.fn()
const mockSaveActiveCarrierRouteWithUndo = vi.fn()
const mockClearActiveCarrierRoute = vi.fn()
const mockClearActiveCarrierRouteWithUndo = vi.fn()
const mockGetCarrierLastLocation = vi.fn()
const mockGetCarrierDepotInfo = vi.fn()

vi.mock('../../api/backend', () => ({
  PlotCarrierRoute: (...args: unknown[]) => mockPlotCarrierRoute(...args),
  RecordCarrierJumpAndSaveRoute: (...args: unknown[]) => mockRecordCarrierJumpAndSaveRoute(...args),
  CompleteCarrierRouteArrival: (...args: unknown[]) => mockCompleteCarrierRouteArrival(...args),
  GetActiveCarrierRoute: (...args: unknown[]) => mockGetActiveCarrierRoute(...args),
  SaveActiveCarrierRoute: (...args: unknown[]) => mockSaveActiveCarrierRoute(...args),
  SaveActiveCarrierRouteWithUndo: (...args: unknown[]) =>
    mockSaveActiveCarrierRouteWithUndo(...args),
  ClearActiveCarrierRoute: (...args: unknown[]) => mockClearActiveCarrierRoute(...args),
  ClearActiveCarrierRouteWithUndo: (...args: unknown[]) =>
    mockClearActiveCarrierRouteWithUndo(...args),
  GetCarrierLastLocation: (...args: unknown[]) => mockGetCarrierLastLocation(...args),
  GetCarrierDepotInfo: (...args: unknown[]) => mockGetCarrierDepotInfo(...args),
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
      route: expedition.CarrierRouteResult.createFrom({ origin: 'Sol', destination: 'Colonia', carrierType: 'player', manualMode: true, jumps: [] }),
      hasNextFcStop: true,
      nextFcStopSystem: 'Colonia',
      loggingJump: true,
    })
    expect(manualArrivalBusyHtml).toMatch(/disabled=""[^>]*>MARK FC STOP ARRIVAL/)
  })
})

function buildCarrierPlan(expeditionId: number, stopRowId: number, stopSystem: string) {
  return expedition.PlanView.createFrom({
    expedition: { id: expeditionId, name: `Expedition ${expeditionId}`, status: 'active' },
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

describe('useCarrierRoute async race and arrival persistence guards', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('discards late PlotCarrierRoute and reload results after switching expeditions or closing the modal and flushes pending manual inputs silently to the outgoing expedition', async () => {
    mockGetActiveCarrierRoute.mockResolvedValue(null)
    mockSaveActiveCarrierRoute.mockResolvedValue(undefined)
    mockSaveActiveCarrierRouteWithUndo.mockResolvedValue(undefined)
    mockClearActiveCarrierRoute.mockResolvedValue(undefined)
    mockClearActiveCarrierRouteWithUndo.mockResolvedValue(undefined)
    mockGetCarrierLastLocation.mockResolvedValue('Sol')
    mockGetCarrierDepotInfo.mockResolvedValue({ usedCapacity: 4000, depotTritium: 900 })

    let resolveFirstPlot!: (route: expedition.CarrierRouteResult) => void
    mockPlotCarrierRoute.mockImplementationOnce(
      () =>
        new Promise<expedition.CarrierRouteResult>((resolve) => {
          resolveFirstPlot = resolve
        }),
    )

    const planOne = buildCarrierPlan(1, 11, 'Colonia')
    const planTwo = buildCarrierPlan(2, 21, 'Beagle Point')
    const busyMessages: Array<string | null> = []
    let latestHook!: ReturnType<typeof useCarrierRoute>

    function HookHarness({
      activePlan,
      manualRequestCount = 0,
    }: {
      activePlan: expedition.PlanView
      manualRequestCount?: number
    }) {
      latestHook = useCarrierRoute({
        plan: activePlan,
        lastVisitedIndex: 0,
        manualPlotterRequestCount: manualRequestCount,
        onBusyChange: (message) => busyMessages.push(message),
      })
      return null
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(createElement(HookHarness, { activePlan: planOne }))
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

      // Switch to expedition 2 while expedition 1's Spansh plot is still pending.
      await act(async () => {
        root.render(createElement(HookHarness, { activePlan: planTwo }))
        await Promise.resolve()
      })
      expect(busyMessages.at(-1)).toBe(null)
      expect(latestHook.routePrompt).toBeNull()

      await act(async () => {
        resolveFirstPlot(
          expedition.CarrierRouteResult.createFrom({
            origin: 'Sol',
            destination: 'Colonia',
            carrierType: 'player',
            manualMode: false,
            jumps: [{ systemName: 'Colonia', distanceLy: 400, tritiumUsed: 60, fuelInTank: 840 }],
          }),
        )
        await pendingPlotPromise
      })

      expect(latestHook.activeCarrierRoute).toBeNull()
      expect(mockSaveActiveCarrierRoute).not.toHaveBeenCalled()
      expect(mockSaveActiveCarrierRouteWithUndo).not.toHaveBeenCalled()

      // Closing the modal mid-plot on expedition 2 also discards late settlement.
      let rejectSecondPlot!: (reason: Error) => void
      mockPlotCarrierRoute.mockImplementationOnce(
        () =>
          new Promise<expedition.CarrierRouteResult>((_, reject) => {
            rejectSecondPlot = reject
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
      expect(latestHook.routePrompt).toBeNull()
      expect(busyMessages.at(-1)).toBe(null)

      await act(async () => {
        rejectSecondPlot(new Error('Spansh timeout'))
        await pendingPlotPromise
      })

      expect(latestHook.activeCarrierRoute).toBeNull()
      expect(mockSaveActiveCarrierRoute).not.toHaveBeenCalled()
      expect(mockSaveActiveCarrierRouteWithUndo).not.toHaveBeenCalled()

      // Activate manual plotter on expedition 2 (undoable), queue a debounced manual distance edit,
      // and switch back to expedition 1: cleanup flushes silently without polluting the undo stack.
      await act(async () => {
        root.render(createElement(HookHarness, { activePlan: planTwo, manualRequestCount: 1 }))
        await Promise.resolve()
      })
      expect(latestHook.activeCarrierRoute).not.toBeNull()
      expect(mockSaveActiveCarrierRouteWithUndo).toHaveBeenCalledWith(
        expect.objectContaining({ expeditionId: 2, manualMode: true }),
        'Open Manual Carrier Plotter',
      )
      mockSaveActiveCarrierRouteWithUndo.mockClear()
      mockSaveActiveCarrierRoute.mockClear()

      act(() => {
        latestHook.handleChangeManualDistance('325.5')
      })
      expect(mockSaveActiveCarrierRoute).not.toHaveBeenCalled()
      expect(mockSaveActiveCarrierRouteWithUndo).not.toHaveBeenCalled()

      await act(async () => {
        root.render(createElement(HookHarness, { activePlan: planOne, manualRequestCount: 1 }))
        await Promise.resolve()
      })
      expect(mockSaveActiveCarrierRoute).toHaveBeenCalledTimes(1)
      expect(mockSaveActiveCarrierRoute).toHaveBeenCalledWith(
        expect.objectContaining({ expeditionId: 2, manualDistInput: '325.5' }),
      )
      expect(mockSaveActiveCarrierRouteWithUndo).not.toHaveBeenCalled()

      // Slow reloadPersistedCarrierRoute from expedition 1 must never overwrite expedition 2.
      let resolveStaleReload!: (state: expedition.ActiveCarrierRouteState | null) => void
      mockGetActiveCarrierRoute.mockImplementationOnce(
        () =>
          new Promise<expedition.ActiveCarrierRouteState | null>((resolve) => {
            resolveStaleReload = resolve
          }),
      )
      let pendingReload!: Promise<void>
      act(() => {
        pendingReload = latestHook.reloadPersistedCarrierRoute()
      })
      await act(async () => {
        root.render(createElement(HookHarness, { activePlan: planTwo, manualRequestCount: 1 }))
        await Promise.resolve()
      })
      await act(async () => {
        resolveStaleReload(
          expedition.ActiveCarrierRouteState.createFrom({
            expeditionId: 1,
            targetRowId: 11,
            manualMode: true,
            route: expedition.CarrierRouteResult.createFrom({
              origin: 'Sol',
              destination: 'Colonia',
              carrierType: 'player',
              manualMode: true,
              jumps: [],
            }),
          }),
        )
        await pendingReload
      })
      expect(latestHook.activeCarrierRoute).toBeNull()
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })

  it('cancels pending manual-input timers, blocks concurrent arrival clicks, preserves activeCarrierRoute on arrival failure, and records undoable cancel and manual jumps atomically', async () => {
    vi.useFakeTimers()
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const savedRoute = expedition.CarrierRouteResult.createFrom({
      origin: 'Sol',
      destination: 'Colonia',
      carrierType: 'player',
      manualMode: true,
      jumps: [],
    })
    mockGetActiveCarrierRoute.mockResolvedValue(
      expedition.ActiveCarrierRouteState.createFrom({
        expeditionId: 1,
        targetRowId: 11,
        route: savedRoute,
        manualMode: true,
      }),
    )
    mockSaveActiveCarrierRoute.mockResolvedValue(undefined)
    mockSaveActiveCarrierRouteWithUndo.mockResolvedValue(undefined)
    mockClearActiveCarrierRoute.mockResolvedValue(undefined)
    mockClearActiveCarrierRouteWithUndo.mockResolvedValue(undefined)
    mockRecordCarrierJumpAndSaveRoute.mockResolvedValue(undefined)

    let rejectFirstArrival!: (reason: Error) => void
    mockCompleteCarrierRouteArrival
      .mockImplementationOnce(
        () =>
          new Promise<void>((_, reject) => {
            rejectFirstArrival = reject
          }),
      )
      .mockResolvedValueOnce(undefined)

    const planOne = buildCarrierPlan(1, 11, 'Colonia')
    let latestHook!: ReturnType<typeof useCarrierRoute>

    function HookHarness() {
      latestHook = useCarrierRoute({
        plan: planOne,
        lastVisitedIndex: 0,
        onBusyChange: () => {},
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
      expect(latestHook.activeCarrierRoute).not.toBeNull()

      // Manual jump logging uses atomic RecordCarrierJumpAndSaveRoute and updates local counters.
      act(() => {
        latestHook.handleChangeManualDistance('245.5')
        latestHook.handleChangeManualTritium('42')
      })
      await act(async () => {
        await latestHook.handleLogManualJump()
      })
      expect(mockRecordCarrierJumpAndSaveRoute).toHaveBeenCalledWith(
        1,
        245.5,
        42,
        expect.objectContaining({
          expeditionId: 1,
          manualLegJumps: 1,
          manualLegDist: 245.5,
          manualLegTrit: 42,
          manualDistInput: '',
          manualTritInput: '',
        }),
      )
      expect(latestHook.manualLegJumps).toBe(1)
      expect(latestHook.manualLegDistance).toBe(245.5)
      expect(latestHook.manualLegTritium).toBe(42)

      act(() => {
        latestHook.handleChangeManualDistance('180.0')
      })

      let firstArrivalPromise!: Promise<void>
      act(() => {
        firstArrivalPromise = latestHook.handleCarrierArriveAtWaypoint()
      })
      expect(latestHook.loggingJump).toBe(true)

      // Concurrent click while loggingJump is true and 250 ms timer window elapsing must both no-op.
      await act(async () => {
        await latestHook.handleCarrierArriveAtWaypoint()
        vi.advanceTimersByTime(300)
      })
      expect(mockCompleteCarrierRouteArrival).toHaveBeenCalledTimes(1)
      expect(mockCompleteCarrierRouteArrival).toHaveBeenCalledWith(1, 11, 0, 0)
      expect(mockSaveActiveCarrierRoute).not.toHaveBeenCalled()
      expect(mockSaveActiveCarrierRouteWithUndo).not.toHaveBeenCalled()

      await act(async () => {
        rejectFirstArrival(new Error('sqlite busy'))
        await firstArrivalPromise
      })
      expect(latestHook.loggingJump).toBe(false)
      expect(mockClearActiveCarrierRoute).not.toHaveBeenCalled()
      expect(mockClearActiveCarrierRouteWithUndo).not.toHaveBeenCalled()
      expect(latestHook.activeCarrierRoute).not.toBeNull()

      await act(async () => {
        await latestHook.handleCarrierArriveAtWaypoint()
      })
      expect(mockCompleteCarrierRouteArrival).toHaveBeenCalledTimes(2)
      expect(mockClearActiveCarrierRoute).not.toHaveBeenCalled()
      expect(mockClearActiveCarrierRouteWithUndo).not.toHaveBeenCalled()
      expect(latestHook.activeCarrierRoute).toBeNull()

      // Reload restored route and verify user cancel calls ClearActiveCarrierRouteWithUndo.
      await act(async () => {
        await latestHook.reloadPersistedCarrierRoute()
      })
      expect(latestHook.activeCarrierRoute).not.toBeNull()
      act(() => {
        latestHook.handleCancelCarrierRoute()
      })
      expect(mockClearActiveCarrierRouteWithUndo).toHaveBeenCalledWith(1)
      expect(latestHook.activeCarrierRoute).toBeNull()
    } finally {
      errorSpy.mockRestore()
      vi.useRealTimers()
      act(() => root.unmount())
      container.remove()
    }
  })

  it('uses atomic RPCs for intermediate Spansh hops, final arrival, already-recorded arrival, depot refill, and manual mode switch', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const twoHopSpanshRoute = expedition.CarrierRouteResult.createFrom({
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
    })
    mockGetActiveCarrierRoute.mockResolvedValueOnce(
      expedition.ActiveCarrierRouteState.createFrom({
        expeditionId: 1,
        targetRowId: 12,
        route: twoHopSpanshRoute,
        jumpIdx: 0,
        manualMode: false,
      }),
    )
    mockRecordCarrierJumpAndSaveRoute.mockResolvedValue(undefined)
    mockSaveActiveCarrierRouteWithUndo.mockResolvedValue(undefined)
    mockCompleteCarrierRouteArrival
      .mockRejectedValueOnce(new Error('arrival failed'))
      .mockResolvedValueOnce(undefined)
      .mockResolvedValueOnce(undefined)

    const multiStopPlan = expedition.PlanView.createFrom({
      expedition: { id: 1, name: 'Expedition 1', status: 'active' },
      nextFcStopRowId: 11,
      rows: [
        { row: { id: 10, expeditionId: 1, rowType: 'waypoint', systemName: 'Sol', visitedAt: '3312-01-01T00:00:00Z' }, number: 1, visitState: 'visited' },
        { row: { id: 11, expeditionId: 1, rowType: 'waypoint', systemName: 'Rohini', fcStop: true, fcArrived: false }, number: 2, visitState: 'future' },
        { row: { id: 12, expeditionId: 1, rowType: 'waypoint', systemName: 'Colonia', fcStop: true, fcArrived: false }, number: 3, visitState: 'future' },
      ],
    })

    let latestHook!: ReturnType<typeof useCarrierRoute>
    const HookHarness = () => {
      latestHook = useCarrierRoute({ plan: multiStopPlan, lastVisitedIndex: 0, onBusyChange: () => {} })
      return null
    }

    const container = document.createElement('div')
    const root = createRoot(container)
    try {
      await act(async () => {
        root.render(createElement(HookHarness))
        await Promise.resolve()
      })
      expect(latestHook.nextFcStopEntry?.row.id).toBe(11)
      expect(latestHook.activeCarrierTargetRowId).toBe(12)

      // Depot refill records an undoable route save.
      act(() => {
        latestHook.handleRefillDepot()
      })
      expect(mockSaveActiveCarrierRouteWithUndo).toHaveBeenCalledWith(
        expect.objectContaining({
          expeditionId: 1,
          route: expect.objectContaining({ depotTritium: 1000 }),
        }),
        'Refill Carrier Depot',
      )

      // Intermediate Spansh hop persists jump and updated route atomically.
      await act(async () => {
        await latestHook.handleCompleteSpanshJump()
      })
      expect(mockRecordCarrierJumpAndSaveRoute).toHaveBeenCalledTimes(1)
      expect(mockRecordCarrierJumpAndSaveRoute).toHaveBeenCalledWith(
        1,
        420,
        expect.any(Number),
        expect.objectContaining({ expeditionId: 1, targetRowId: 12, jumpIdx: 1 }),
      )
      expect(latestHook.carrierJumpIndex).toBe(1)

      // Final Spansh hop calls CompleteCarrierRouteArrival atomically; on error local state stays untouched.
      await act(async () => {
        await latestHook.handleCompleteSpanshJump()
      })
      expect(mockCompleteCarrierRouteArrival).toHaveBeenNthCalledWith(
        1,
        1,
        12,
        400,
        expect.any(Number),
      )
      expect(latestHook.carrierJumpIndex).toBe(1)
      expect(latestHook.activeCarrierRoute).not.toBeNull()

      // Retry final Spansh hop succeeds and clears local state without redundant backend clear.
      await act(async () => {
        await latestHook.handleCompleteSpanshJump()
      })
      expect(mockCompleteCarrierRouteArrival).toHaveBeenNthCalledWith(
        2,
        1,
        12,
        400,
        expect.any(Number),
      )
      expect(latestHook.activeCarrierRoute).toBeNull()
      expect(mockClearActiveCarrierRoute).not.toHaveBeenCalled()
      expect(mockClearActiveCarrierRouteWithUndo).not.toHaveBeenCalled()

      // Already-recorded Spansh route (jumpIdx >= totalJumps) completes arrival with zero distance and tritium.
      mockGetActiveCarrierRoute.mockResolvedValueOnce(
        expedition.ActiveCarrierRouteState.createFrom({
          expeditionId: 1,
          targetRowId: 12,
          route: twoHopSpanshRoute,
          jumpIdx: 2,
          manualMode: false,
        }),
      )
      await act(async () => {
        await latestHook.reloadPersistedCarrierRoute()
      })
      expect(latestHook.carrierJumpIndex).toBe(2)

      await act(async () => {
        await latestHook.handleCompleteSpanshJump()
      })
      expect(mockCompleteCarrierRouteArrival).toHaveBeenNthCalledWith(3, 1, 12, 0, 0)
      expect(latestHook.activeCarrierRoute).toBeNull()

      // Switching an active Spansh route to manual mode records an undoable route save.
      mockGetActiveCarrierRoute.mockResolvedValueOnce(
        expedition.ActiveCarrierRouteState.createFrom({
          expeditionId: 1,
          targetRowId: 12,
          route: twoHopSpanshRoute,
          jumpIdx: 0,
          manualMode: false,
        }),
      )
      await act(async () => {
        await latestHook.reloadPersistedCarrierRoute()
      })
      mockSaveActiveCarrierRouteWithUndo.mockClear()
      act(() => {
        latestHook.switchActiveRouteToManualMode()
      })
      expect(mockSaveActiveCarrierRouteWithUndo).toHaveBeenCalledWith(
        expect.objectContaining({ expeditionId: 1, manualMode: true }),
        'Switch Carrier Route to Manual Mode',
      )
    } finally {
      errorSpy.mockRestore()
      act(() => root.unmount())
    }
  })

  it('ignores in-flight jump or arrival settlement after clearLocalCarrierRoute or reloadPersistedCarrierRoute and invalidates stale route loads on completed jumps', async () => {
    const twoHopRoute = expedition.CarrierRouteResult.createFrom({
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
    })
    const initialRouteState = expedition.ActiveCarrierRouteState.createFrom({
      expeditionId: 1,
      targetRowId: 11,
      route: twoHopRoute,
      jumpIdx: 0,
      manualMode: false,
    })
    const restoredRouteState = expedition.ActiveCarrierRouteState.createFrom({
      expeditionId: 1,
      targetRowId: 11,
      route: twoHopRoute,
      jumpIdx: 1,
      manualMode: false,
    })

    mockGetActiveCarrierRoute.mockResolvedValueOnce(initialRouteState)
    mockSaveActiveCarrierRouteWithUndo.mockResolvedValue(undefined)

    const planOne = buildCarrierPlan(1, 11, 'Colonia')
    let latestHook!: ReturnType<typeof useCarrierRoute>
    const HookHarness = () => {
      latestHook = useCarrierRoute({ plan: planOne, lastVisitedIndex: 0, onBusyChange: () => {} })
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
      expect(latestHook.activeCarrierRoute).not.toBeNull()
      expect(latestHook.carrierJumpIndex).toBe(0)

      // In-flight RecordCarrierJumpAndSaveRoute settling after clearLocalCarrierRoute must not restore route.
      let resolveInFlightJump!: () => void
      mockRecordCarrierJumpAndSaveRoute.mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            resolveInFlightJump = resolve
          }),
      )
      let pendingJumpPromise!: Promise<void>
      act(() => {
        pendingJumpPromise = latestHook.handleCompleteSpanshJump()
      })
      act(() => {
        latestHook.clearLocalCarrierRoute()
      })
      expect(latestHook.activeCarrierRoute).toBeNull()

      await act(async () => {
        resolveInFlightJump()
        await pendingJumpPromise
      })
      expect(latestHook.activeCarrierRoute).toBeNull()
      expect(latestHook.carrierJumpIndex).toBe(0)

      // Load route at hop 1, start CompleteCarrierRouteArrival, then reloadPersistedCarrierRoute (undo) before settlement.
      mockGetActiveCarrierRoute.mockResolvedValueOnce(restoredRouteState)
      await act(async () => {
        await latestHook.reloadPersistedCarrierRoute()
      })
      expect(latestHook.carrierJumpIndex).toBe(1)

      let resolveInFlightArrival!: () => void
      mockCompleteCarrierRouteArrival.mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            resolveInFlightArrival = resolve
          }),
      )
      let pendingArrivalPromise!: Promise<void>
      act(() => {
        pendingArrivalPromise = latestHook.handleCompleteSpanshJump()
      })

      mockGetActiveCarrierRoute.mockResolvedValueOnce(initialRouteState)
      await act(async () => {
        await latestHook.reloadPersistedCarrierRoute()
      })
      expect(latestHook.activeCarrierRoute).not.toBeNull()
      expect(latestHook.carrierJumpIndex).toBe(0)

      // Late arrival settlement must not clear the reloaded route.
      await act(async () => {
        resolveInFlightArrival()
        await pendingArrivalPromise
      })
      expect(latestHook.activeCarrierRoute).not.toBeNull()
      expect(latestHook.carrierJumpIndex).toBe(0)

      // Slow GetActiveCarrierRoute started before a completed jump must not overwrite jumpIdx 1 with stale jumpIdx 0.
      let resolveSlowLoad!: (state: expedition.ActiveCarrierRouteState | null) => void
      mockGetActiveCarrierRoute.mockImplementationOnce(
        () =>
          new Promise<expedition.ActiveCarrierRouteState | null>((resolve) => {
            resolveSlowLoad = resolve
          }),
      )
      let slowReloadPromise!: Promise<void>
      act(() => {
        slowReloadPromise = latestHook.reloadPersistedCarrierRoute()
      })
      mockRecordCarrierJumpAndSaveRoute.mockResolvedValueOnce(undefined)
      await act(async () => {
        await latestHook.handleCompleteSpanshJump()
      })
      expect(latestHook.carrierJumpIndex).toBe(1)

      await act(async () => {
        resolveSlowLoad(initialRouteState)
        await slowReloadPromise
      })
      expect(latestHook.carrierJumpIndex).toBe(1)
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })
})

