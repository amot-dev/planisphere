import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { ExpeditionPlanner } from './ExpeditionPlanner'
import { PlannerConfirmModal } from './PlannerConfirmModal'
import { PlannerHeaderBar } from './PlannerHeaderBar'
import { PlannerRegionRow } from './PlannerRegionRow'
import { PlannerRouteTable } from './PlannerRouteTable'
import { StartExpeditionModal } from './StartExpeditionModal'
import { CarrierRouteModal } from './CarrierRouteModal'
import { CarrierRouteBottomPanel } from './CarrierRouteBottomPanel'
import { NavigationTelemetrySection } from '../sidebar/NavigationTelemetrySection'
import { StatsSidebar } from '../sidebar/StatsSidebar'
import {
  createCachedRowDropPropsGetter,
  resolveActiveDragRowIndex,
  stepDragAutoScroll,
} from './usePlannerRowDrag'
import { app, expedition, stats } from '../../../wailsjs/go/models'

describe('ExpeditionPlanner Hook Stability and Lifecycle', () => {
  const renderPlanner = (plan: expedition.PlanView | null) =>
    renderToString(
      createElement(ExpeditionPlanner, {
        plan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onBusyChange: () => {},
      }),
    )

  it('renders safely when plan is null without throwing hook mismatch or null access', () => {
    const html = renderPlanner(null)

    expect(html).toContain('LOADING EXPEDITION...')
  })

  it('renders active expedition rows, expansion chevrons, consecutive legs without dividers, and last-visited centering marker', () => {
    const mockPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 1,
        name: 'Beagle Point Survey',
        description: 'Deep reconnaissance',
        status: 'active',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: [
        {
          row: {
            id: 401,
            expeditionId: 1,
            rowType: 'waypoint',
            systemName: 'Sol',
            seq: 0,
            poiName: 'Earth',
            notes: 'Home',
          },
          number: 1,
          visitState: 'visited',
        },
        {
          row: {
            id: 402,
            expeditionId: 1,
            rowType: 'waypoint',
            systemName: 'Alpha Centauri',
            seq: 1,
          },
          number: 2,
          visitState: 'visited',
          legModeId: 1,
          legModeName: 'Travel',
          legModeColor: '#FF7100',
          transitions: [
            { kind: 'ship', to: 'Krait Phantom' },
          ],
        },
        {
          row: {
            id: 403,
            expeditionId: 1,
            rowType: 'waypoint',
            systemName: 'Rohini',
            seq: 2,
          },
          number: 3,
          visitState: 'current',
        },
        {
          row: {
            id: 404,
            expeditionId: 1,
            rowType: 'waypoint',
            systemName: 'Colonia',
            seq: 3,
            poiName: 'Jaques',
            notes: 'Refuel',
          },
          number: 4,
          visitState: 'future',
        },
      ],
      modes: [
        { id: 1, name: 'Travel', color: '#FF7100' } as any,
      ],
      visitedWaypointCount: 2,
      waypointCount: 4,
      activeRowId: 402,
    })

    const html = renderPlanner(mockPlan)

    expect(html).toContain('Beagle Point Survey')
    expect(html).toContain('WAYPOINTS VISITED')
    expect(html).toContain('Sol')
    expect(html).toContain('Alpha Centauri')
    expect(html).toContain('Colonia')
    // Visited waypoints are rendered without intermediate lines or banners
    expect(html).not.toContain('h-[3px]')
    expect(html).not.toContain('title="Leg Mode:')
    // Visited (Sol, Alpha Centauri) and current (Rohini) rows have dropdown chevrons; future (Colonia) does not
    const activeChevrons = html.match(/aria-label="Toggle per-waypoint leg telemetry drawer"/g)
    expect(activeChevrons).toHaveLength(3)
    // Waypoint 2 (row 402, index 1) is the last visited waypoint and gets data-last-visited="true"
    expect(html).toContain('data-row-id="402"')
    expect(html).toContain('data-row-index="1"')
    expect(html).toMatch(/data-row-id="402"[^>]*data-last-visited="true"/)
    expect(html).not.toMatch(/data-row-id="401"[^>]*data-last-visited="true"/)
  })

  it('renders safely when plan has empty rows array', () => {
    const mockPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 2,
        name: 'Empty Survey',
        description: '',
        status: 'planning',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: [],
      modes: [],
      visitedWaypointCount: 0,
      waypointCount: 0,
      activeRowId: 0,
    })

    const html = renderPlanner(mockPlan)

    expect(html).toContain('Empty Survey')
    expect(html).toContain('ADD WAYPOINT')
  })

  it('renders safely when plan.rows is null or missing', () => {
    const mockPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 3,
        name: 'Null Rows Survey',
        description: '',
        status: 'planning',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: null as any,
      modes: [],
      visitedWaypointCount: 0,
      waypointCount: 0,
      activeRowId: 0,
    })

    const html = renderPlanner(mockPlan)

    expect(html).toContain('Null Rows Survey')
  })

  it('renders completed expedition with Reopen action and Completed Route summary', () => {
    const completedPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 4,
        name: 'Completed Voyage',
        description: 'Done',
        status: 'completed',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: [
        {
          row: { id: 201, expeditionId: 4, rowType: 'region', regionId: 1, seq: 0 },
          number: 0,
          regionName: 'Galactic Core',
          visitState: 'visited',
        },
        {
          row: { id: 30, expeditionId: 4, rowType: 'waypoint', systemName: 'Sol', seq: 1, visitedAt: '3310-01-01T10:00:00Z' },
          number: 1,
          visitState: 'visited',
        },
        {
          row: { id: 31, expeditionId: 4, rowType: 'waypoint', systemName: 'Alpha Centauri', seq: 2, visitedAt: '3310-01-01T11:00:00Z' },
          number: 2,
          visitState: 'visited',
          distFromPrevLy: 4.3,
        },
        {
          row: { id: 32, expeditionId: 4, rowType: 'waypoint', systemName: "Barnard's Star", seq: 3, visitedAt: '3310-01-01T12:00:00Z' },
          number: 3,
          visitState: 'visited',
          distFromPrevLy: 6.0,
        },
      ],
      modes: [],
      visitedWaypointCount: 3,
      waypointCount: 3,
      activeRowId: 32,
      plannedLy: 10.3,
      coveredLy: 10.3,
      remainingLy: 0,
    })

    const html = renderPlanner(completedPlan)

    expect(html).toContain('Completed Voyage')
    expect(html).toContain('REOPEN')
    expect(html).toContain('COMPLETED ROUTE:')
    expect(html).toContain('10.3 Ly')
    expect(html).not.toContain('PLANNED ROUTE:')
    expect(html).not.toContain('REMAINING:')
    expect(html).not.toContain('COVERED:')
  })

  it('renders populated planning expedition with native content-visibility, bottom add controls, and Planned Route summary', () => {
    const planningPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 5,
        name: 'Native Scrolling Voyage',
        description: '',
        status: 'planning',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: [
        {
          row: {
            id: 301,
            expeditionId: 5,
            rowType: 'region',
            regionId: 1,
            seq: 0,
          },
          number: 0,
          regionName: 'Galactic Core',
          visitState: 'future',
        },
        {
          row: {
            id: 302,
            expeditionId: 5,
            rowType: 'waypoint',
            systemName: 'Sagittarius A*',
            seq: 1,
            poiName: 'Supermassive Black Hole',
            notes: 'Center of Galaxy',
          },
          number: 1,
          visitState: 'future',
        },
        {
          row: {
            id: 303,
            expeditionId: 5,
            rowType: 'waypoint',
            systemName: 'Alpha Centauri',
            seq: 2,
          },
          number: 2,
          visitState: 'future',
          distFromPrevLy: 4.3,
        },
      ],
      modes: [],
      visitedWaypointCount: 0,
      waypointCount: 2,
      activeRowId: 0,
      plannedLy: 4.3,
      remainingLy: 4.3,
    })

    const html = renderPlanner(planningPlan)

    // Verify row styles include content-visibility and contain-intrinsic-size without virtualization spacers
    expect(html).toContain('content-visibility:auto')
    expect(html).toContain('contain-intrinsic-size:44px')
    expect(html).not.toContain('aria-hidden="true"><td')
    expect(html).toContain('Sagittarius A*')
    expect(html).toContain('Supermassive Black Hole')

    // Planning rows omit dropdown chevrons and data-last-visited markers, and include bottom add buttons + spacer
    const planningChevrons = html.match(/aria-label="Toggle per-waypoint leg telemetry drawer"/g)
    expect(planningChevrons).toBeNull()
    expect(html).not.toContain('data-last-visited="true"')
    expect(html).toContain('ADD WAYPOINT')
    expect(html).toContain('ADD REGION HEADER')
    expect(html).toContain('h-48 shrink-0 pointer-events-none')

    // Header summary shows only Planned Route in planning mode
    expect(html).toContain('PLANNED ROUTE:')
    expect(html).toContain('4.3 Ly')
    expect(html).not.toContain('REMAINING:')
    expect(html).not.toContain('COVERED:')
    expect(html).not.toContain('COMPLETED ROUTE:')
  })

  it('renders remaining Ly reflecting distance from player and shows player distance on next waypoint', () => {
    const activePlan = expedition.PlanView.createFrom({
      expedition: {
        id: 7,
        name: 'Live Telemetry Expedition',
        description: '',
        status: 'active',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: [
        {
          row: { id: 1, expeditionId: 7, rowType: 'waypoint', systemName: 'Sol', seq: 1, visitedAt: '3310-01-01T10:00:00Z' },
          number: 1,
          visitState: 'visited',
          visitedAtGalactic: '3310-01-01 10:00:00',
        },
        {
          row: { id: 2, expeditionId: 7, rowType: 'waypoint', systemName: 'Alpha Centauri', seq: 2 },
          number: 2,
          visitState: 'current',
          isNextWaypoint: true,
          distFromPrevLy: 4.3,
          distFromCurrentLy: 2.1,
        },
        {
          row: { id: 3, expeditionId: 7, rowType: 'waypoint', systemName: "Barnard's Star", seq: 3 },
          number: 3,
          visitState: 'future',
          isNextWaypoint: false,
          distFromPrevLy: 6.0,
        },
      ],
      nextWaypointRowId: 2,
      playerSystem: 'Interim Sol System',
      modes: [],
      visitedWaypointCount: 1,
      waypointCount: 3,
      plannedLy: 10.3,
      remainingLy: 8.1, // 2.1 (player to WP2) + 6.0 (WP2 to WP3)
    })

    const html = renderPlanner(activePlan)

    // Route readout at top:
    expect(html).toContain('COVERED:')
    expect(html).toContain('REMAINING:')
    expect(html).toContain('8.1 Ly')
    expect(html).not.toContain('PLANNED ROUTE:')
    expect(html).not.toContain('COMPLETED ROUTE:')

    // WP2 (next waypoint) shows player distance (2.1 Ly) with player location tooltip:
    expect(html).toContain('2.1 Ly')
    expect(html).toContain('Estimated straight-line distance from player location (Interim Sol System)')

    // WP3 (subsequent waypoint) shows distance from previous waypoint (6.0 Ly):
    expect(html).toContain('6.0 Ly')
    expect(html).toContain('Estimated straight-line distance from previous waypoint')

    // With only Waypoint #1 visited (visitedWaypointCount: 1), Complete button is disabled until Waypoint #2 is visited:
    expect(html).toContain(
      'disabled="" title="Waypoint #2 not visited yet — complete requires at least 2 visited waypoints (or delete expedition in Expeditions)"',
    )

    // Once Waypoint #2 is visited, Covered and Remaining update, Complete button becomes enabled,
    // and lastVisitedIndex advances across visitState === 'visited' rows without a 'current' row:
    const activePlanWithSecondVisited = expedition.PlanView.createFrom({
      ...activePlan,
      rows: activePlan.rows.map((rowView) =>
        rowView.row.id === 2 ? { ...rowView, visitState: 'visited', isNextWaypoint: false } : rowView,
      ),
      visitedWaypointCount: 2,
      coveredLy: 4.3,
      remainingLy: 5.0,
    })
    const midflightHtml = renderPlanner(activePlanWithSecondVisited)
    expect(midflightHtml).toContain('COVERED:')
    expect(midflightHtml).toContain('4.3 Ly')
    expect(midflightHtml).toContain('REMAINING:')
    expect(midflightHtml).toContain('5.0 Ly')
    expect(midflightHtml).toContain('>COMPLETE</button>')
    expect(midflightHtml).toMatch(/data-row-id="2"[^>]*data-last-visited="true"/)
    expect(midflightHtml).not.toMatch(/data-row-id="1"[^>]*data-last-visited="true"/)

    const carrierRouteOpenHeaderHtml = renderToString(
      createElement(PlannerHeaderBar, {
        plan: activePlanWithSecondVisited,
        hasOriginWaypointSystem: true,
        carrierRouteOpen: true,
        onOpenStartModal: () => {},
        onLifecycleAction: async () => {},
      }),
    )
    expect(carrierRouteOpenHeaderHtml).toContain(
      'disabled="" title="Close or finish the active Fleet Carrier route before completing the expedition"',
    )
    expect(carrierRouteOpenHeaderHtml).toContain('opacity-40 cursor-not-allowed hover:bg-transparent')
  })

  it('CarrierRouteModal omits Manual Entry Mode and keeps QUERYING SPANSH... single-line', () => {
    const renderCarrierModal = (overrides: Partial<Parameters<typeof CarrierRouteModal>[0]['prompt']> = {}) =>
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

  it('StatsSidebar renders LOG JUMP button only in active Expedition scope and disables it when carrierRouteOpen is true', () => {
    const emptyFilter = stats.Filter.createFrom({ enabled: false, modes: [] })
    const renderStatsSidebar = (overrides: Partial<Parameters<typeof StatsSidebar>[0]> = {}) =>
      renderToString(
        createElement(StatsSidebar, {
          sidebarStats: null,
          statsLoading: false,
          modesState: null,
          filter: emptyFilter,
          onFilterChange: () => {},
          onQuickSwitchMode: () => {},
          onLogCarrierJump: () => {},
          ...overrides,
        }),
      )

    expect(renderStatsSidebar()).not.toContain('LOG JUMP')

    const planningHtml = renderStatsSidebar({
      sidebarStats: app.SidebarStats.createFrom({ expeditionId: 1, status: 'planning' }),
    })
    expect(planningHtml).not.toContain('LOG JUMP')

    const activeSidebarStats = app.SidebarStats.createFrom({ expeditionId: 1, status: 'active' })
    const html = renderStatsSidebar({ sidebarStats: activeSidebarStats })
    expect(html).toContain('LOG JUMP')
    expect(html).not.toContain('CARRIER LOG')
    expect(html).not.toMatch(/disabled=""[^>]*>LOG JUMP/)

    const openRouteHtml = renderStatsSidebar({
      sidebarStats: activeSidebarStats,
      carrierRouteOpen: true,
    })
    expect(openRouteHtml).toContain('Carrier routing panel is already open')
    expect(openRouteHtml).toMatch(/disabled=""[^>]*>LOG JUMP/)

    const completedHtml = renderStatsSidebar({
      sidebarStats: app.SidebarStats.createFrom({
        expeditionId: 1,
        status: 'completed',
        expedition: {
          carrierJumps: 2,
          carrierDistanceLy: 900,
          tritiumUsed: 150,
        },
        carrierCost: {
          carrierType: 'squadron',
          weeklyUpkeep: 14000000,
          tritiumPrice: 50000,
          tritiumCost: 7500000,
          jumpCost: 200000,
          upkeepCost: 14000000,
          totalCost: 21700000,
          totalEstimated: true,
        },
      }),
    })

    expect(completedHtml).not.toContain('LOG JUMP')
    expect(completedHtml).toContain('Flight Operations')
    expect(completedHtml).toContain('Expedition Carrier Operations')
    expect(completedHtml).toContain('Science Operations')
    expect(completedHtml).toContain('Carrier Cost')
    expect(completedHtml).toContain('~21,700,000 Cr')
    expect(completedHtml).toContain('Tritium Cost')
    expect(completedHtml).toContain('~7,500,000 Cr')
    expect(completedHtml).toContain('Jump Maintenance')
    expect(completedHtml).toContain('200,000 Cr')
    expect(completedHtml).not.toContain('~200,000 Cr')
    expect(completedHtml).toContain('Weekly Upkeep')
    expect(completedHtml).toContain('~14,000,000 Cr')
    expect(completedHtml).toContain(
      'Configure expedition Fleet Carrier vessel classification and weekly upkeep',
    )

    const allTimeSectionHtml = renderToString(
      createElement(NavigationTelemetrySection, {
        viewScope: 'allTime',
        sidebarStats: app.SidebarStats.createFrom({
          allJournals: {
            carrierDistanceLy: 12500,
            carrierJumps: 28,
            tritiumUsed: 1900,
          },
        }),
        displayedMetrics: stats.Metrics.createFrom({
          carrierDistanceLy: 12500,
          carrierJumps: 28,
          tritiumUsed: 1900,
        }),
        carrierRouteOpen: false,
      }),
    )

    expect(allTimeSectionHtml).toContain('Personal Carrier Operations')
    expect(allTimeSectionHtml).not.toContain('OWNED VESSEL')
    expect(allTimeSectionHtml).toContain('Personal Carrier Dist (Ly)')
    expect(allTimeSectionHtml).toContain('Personal Carrier Jumps')
    expect(allTimeSectionHtml).not.toContain('Personal Carrier Tritium')
    expect(allTimeSectionHtml).not.toContain('Carrier Cost')
  })

  it('CarrierRouteBottomPanel renders Close button with tooltip, start/end indicator with ??? fallback, and Mark FC Stop Arrival button', () => {
    const renderCarrierPanel = (overrides: Partial<Parameters<typeof CarrierRouteBottomPanel>[0]> & { route: expedition.CarrierRouteResult }) =>
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
    expect(manualWithStop).toContain('Existing jumps remain logged in your expedition statistics.')
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
  })

  it('PlannerConfirmModal renders delete visited, reset carrier arrival on move, and manual state-repair override modals', () => {
    const deleteVisitedHtml = renderToString(
      createElement(PlannerConfirmModal, {
        confirmState: {
          kind: 'delete_waypoint',
          rowId: 12,
          waypointNumber: 2,
          systemName: 'Alpha Centauri',
          visitedAtGalactic: '3312-01-10 10:00 UTC',
          isArrivedFcStop: false,
        },
        onConfirm: () => {},
        onCancel: () => {},
      }),
    )
    expect(deleteVisitedHtml).toContain('Delete Visited Waypoint')
    expect(deleteVisitedHtml).toContain('3312-01-10 10:00 UTC')
    expect(deleteVisitedHtml).toContain('DELETE WAYPOINT')

    const deleteArrivedFcHtml = renderToString(
      createElement(PlannerConfirmModal, {
        confirmState: {
          kind: 'delete_waypoint',
          rowId: 14,
          waypointNumber: 3,
          systemName: 'Colonia',
          isArrivedFcStop: true,
        },
        onConfirm: () => {},
        onCancel: () => {},
      }),
    )
    expect(deleteArrivedFcHtml).toContain('Delete Arrived Carrier Stop')
    expect(deleteArrivedFcHtml).toContain('ARRIVED')
    expect(deleteArrivedFcHtml).toContain('DELETE WAYPOINT')

    const deleteVisitedAndArrivedFcHtml = renderToString(
      createElement(PlannerConfirmModal, {
        confirmState: {
          kind: 'delete_waypoint',
          rowId: 16,
          waypointNumber: 4,
          systemName: 'Sagittarius A*',
          visitedAtGalactic: '3312-02-01 18:00 UTC',
          isArrivedFcStop: true,
        },
        onConfirm: () => {},
        onCancel: () => {},
      }),
    )
    expect(deleteVisitedAndArrivedFcHtml).toContain('Delete Visited Carrier Stop')
    expect(deleteVisitedAndArrivedFcHtml).toContain('3312-02-01 18:00 UTC')
    expect(deleteVisitedAndArrivedFcHtml).toContain('ARRIVED')

    const moveResetFcHtml = renderToString(
      createElement(PlannerConfirmModal, {
        confirmState: {
          kind: 'move_reset_fc',
          rowId: 15,
          fromIndex: 1,
          toIndex: 4,
          waypointNumber: 3,
          systemName: 'Colonia',
        },
        onConfirm: () => {},
        onCancel: () => {},
      }),
    )
    expect(moveResetFcHtml).toContain('Reset Fleet Carrier Arrival')
    expect(moveResetFcHtml).toContain('MOVE AND RESET TO PENDING')

    const overrideFcHtml = renderToString(
      createElement(PlannerConfirmModal, {
        confirmState: {
          kind: 'override_fc_arrival',
          rowId: 18,
          waypointNumber: 4,
          systemName: 'Rohini',
          targetArrived: true,
          cascadedCount: 2,
        },
        onConfirm: () => {},
        onCancel: () => {},
      }),
    )
    expect(overrideFcHtml).toContain('Override Fleet Carrier Arrival Status')
    expect(overrideFcHtml).toContain('Manual State Repair Override')
    expect(overrideFcHtml).toContain('2 preceding pending carrier stops will also be marked ARRIVED')
    expect(overrideFcHtml).toContain('MARK AS ARRIVED')

    const startModalHtml = renderToString(
      createElement(StartExpeditionModal, {
        expeditionId: 1,
        onClose: () => {},
        onStarted: () => {},
      }),
    )
    expect(startModalHtml).not.toContain('Arm and Wait for First Arrival')

    const routePlan = expedition.PlanView.createFrom({
      expedition: { id: 1, status: 'active' },
      rows: [
        { row: { id: 10, rowType: 'waypoint', systemName: 'Sol', visitedAt: '3312-01-01T00:00:00Z' }, number: 1, visitState: 'visited' },
        { row: { id: 11, rowType: 'waypoint', systemName: 'Colonia', fcStop: true, fcArrived: false }, number: 2, visitState: 'future' },
      ],
    })
    const tableItems = routePlan.rows.map((rowView, index) => ({
      kind: 'waypoint' as const,
      rowView,
      index,
      key: `row-${rowView.row.id}`,
    }))
    const baseTableProps = {
      plan: routePlan,
      tableItems,
      dropSlotGaps: new Set<number>(),
      modesList: [],
      isPlanning: false,
      isCompleted: false,
      lastVisitedIndex: 0,
      lastFcArrivedIndex: -1,
      visibleColumnCount: 8,
      tableMinWidthPixels: 1200,
      showNotes: true,
      showCarrierStop: true,
      expandedRowIds: {},
      detailsById: {},
      loadingDetails: {},
      copiedSystemRowId: null,
      insertMenuAfterId: null,
      heldOrDraggedIndex: null,
      draggedIndex: null,
      dragOverGapIndex: null,
      activeCarrierRoute: expedition.CarrierRouteResult.createFrom({ origin: 'Sol', destination: 'Colonia', jumps: [] }),
      activeCarrierTargetRowId: 11,
      nextFcStopEntry: { row: routePlan.rows[1].row, index: 1 },
      routedCarrierStopIndex: 1,
      getRowDropProps: () => ({}),
      getSlotDropProps: () => ({ onDragOver: () => {}, onDrop: () => {} }),
      onClearDragOverGap: () => {},
      onToggleShowNotes: () => {},
      onToggleShowCarrierStop: () => {},
      onChangeRegion: () => {},
      onSetInsertMenuAfterId: () => {},
      onInsert: () => {},
      onDeleteRow: () => {},
      onLegModeChange: () => {},
      onToggleExpand: () => {},
      onFieldBlur: () => {},
      onCopySystem: () => {},
      onToggleFcStop: () => {},
      onOverrideFcArrived: () => {},
      onOpenCarrierRoutePrompt: () => {},
      onRowHoldStart: () => {},
      onRowHoldEnd: () => {},
      onRowDragStart: () => {},
      onRowDragEnd: () => {},
    }
    const manualTableHtml = renderToString(
      createElement(PlannerRouteTable, { ...baseTableProps, isManualCarrierRoute: true }),
    )
    expect(manualTableHtml).toContain('placeholder="Star System"')
    expect(manualTableHtml).not.toContain('Locked while Fleet Carrier route to this waypoint is active')

    const spanshTableHtml = renderToString(
      createElement(PlannerRouteTable, { ...baseTableProps, isManualCarrierRoute: false }),
    )
    expect(spanshTableHtml).toContain('Locked while Fleet Carrier route to this waypoint is active')
  })

  it('resolveActiveDragRowIndex locates shifted rows by row.id after route updates and returns -1 when removed', () => {
    const firstRow = expedition.RowView.createFrom({ row: { id: 101, rowType: 'waypoint', systemName: 'Sol' } })
    const grabbedRow = expedition.RowView.createFrom({ row: { id: 102, rowType: 'waypoint', systemName: 'Colonia' } })
    const insertedRow = expedition.RowView.createFrom({ row: { id: 103, rowType: 'waypoint', systemName: 'Rohini' } })

    expect(resolveActiveDragRowIndex([firstRow, grabbedRow], 102)).toBe(1)
    expect(resolveActiveDragRowIndex([firstRow, null, insertedRow, grabbedRow], 102)).toBe(3)
    expect(resolveActiveDragRowIndex([firstRow, insertedRow], 102)).toBe(-1)
    expect(resolveActiveDragRowIndex(null, 102)).toBe(-1)
    expect(resolveActiveDragRowIndex([firstRow, grabbedRow], null)).toBe(-1)
  })

  it('stepDragAutoScroll scrolls only inside edge zones and sleeps when delta is zero', () => {
    const mockContainer = {
      scrollTop: 150,
      clientHeight: 400,
      scrollHeight: 1200,
      getBoundingClientRect: () => ({ top: 100, bottom: 500 }),
      querySelector: () => null,
    } as unknown as HTMLElement

    // Idle middle of viewport does not scroll and stops the animation frame loop.
    expect(stepDragAutoScroll(mockContainer, 300)).toBe(false)
    expect(mockContainer.scrollTop).toBe(150)

    // Null cursor also sleeps without modifying scrollTop.
    expect(stepDragAutoScroll(mockContainer, null)).toBe(false)
    expect(mockContainer.scrollTop).toBe(150)

    // Top edge zone scrolls upward until hitting scrollTop 0, then sleeps.
    expect(stepDragAutoScroll(mockContainer, 120)).toBe(true)
    expect(mockContainer.scrollTop).toBeLessThan(150)
    mockContainer.scrollTop = 0
    expect(stepDragAutoScroll(mockContainer, 120)).toBe(false)

    // Bottom edge zone scrolls downward until reaching bottom, then sleeps.
    mockContainer.scrollTop = 100
    expect(stepDragAutoScroll(mockContainer, 485)).toBe(true)
    expect(mockContainer.scrollTop).toBeGreaterThan(100)
    mockContainer.scrollTop = 800
    expect(stepDragAutoScroll(mockContainer, 485)).toBe(false)
  })

  it('createCachedRowDropPropsGetter keeps per-row dropProps references stable across gap hover updates and PlannerRegionRow stays memoized', () => {
    const dragPlan = expedition.PlanView.createFrom({
      expedition: { id: 1, status: 'planning' },
      rows: [
        { row: { id: 1, rowType: 'waypoint', systemName: 'Sol' }, number: 1 },
        { row: { id: 2, rowType: 'waypoint', systemName: 'Alpha Centauri' }, number: 2 },
        { row: { id: 3, rowType: 'waypoint', systemName: 'Colonia' }, number: 3 },
      ],
    })
    const dragOverGapIndexRef = { current: null as number | null }
    const gapUpdates: Array<number | null> = []
    const dropMoves: Array<{ fromIndex: number; targetIndex: number }> = []
    const scrollContainerElement = {
      getBoundingClientRect: () => ({ top: 0 }),
    } as unknown as HTMLDivElement

    const getRowDropProps = createCachedRowDropPropsGetter({
      plan: dragPlan,
      isPlanning: true,
      isCompleted: false,
      lastVisitedIndex: -1,
      draggedIndex: 0,
      dragOverGapIndexRef,
      updateDragOverGapIndex: (gapIndex) => {
        dragOverGapIndexRef.current = gapIndex
        gapUpdates.push(gapIndex)
      },
      dropSlotGaps: new Set<number>(),
      activeDropPreviews: null,
      scrollContainerRef: { current: scrollContainerElement },
      recordDragOverClientY: () => {},
      finishDropMove: (fromIndex, targetIndex) => {
        dropMoves.push({ fromIndex, targetIndex })
      },
    })

    const initialRowTwoProps = getRowDropProps(2)
    expect(getRowDropProps(2)).toBe(initialRowTwoProps)

    let defaultPrevented = false
    const makeDragEvent = (clientY: number) =>
      ({
        clientY,
        currentTarget: { getBoundingClientRect: () => ({ top: 100, height: 40 }) },
        dataTransfer: { dropEffect: '' },
        preventDefault: () => {
          defaultPrevented = true
        },
      }) as unknown as Parameters<typeof initialRowTwoProps.onDragOver>[0]

    // Hovering top half then bottom half of row 2 updates the gap via ref without recreating dropProps.
    initialRowTwoProps.onDragOver(makeDragEvent(110))
    expect(defaultPrevented).toBe(true)
    expect(dragOverGapIndexRef.current).toBe(2)
    expect(getRowDropProps(2)).toBe(initialRowTwoProps)

    initialRowTwoProps.onDragOver(makeDragEvent(130))
    expect(dragOverGapIndexRef.current).toBe(3)
    expect(gapUpdates).toEqual([2, 3])
    expect(getRowDropProps(2)).toBe(initialRowTwoProps)

    initialRowTwoProps.onDrop(makeDragEvent(130))
    expect(dropMoves).toEqual([{ fromIndex: 0, targetIndex: 2 }])

    expect((PlannerRegionRow as unknown as { $$typeof?: symbol }).$$typeof).toBe(
      Symbol.for('react.memo'),
    )
  })

  it('adds a left border to region headers if and only if the following row has a border, matching its color', () => {
    const regionBorderPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 21,
        name: 'Region Border Test',
        description: '',
        status: 'active',
        createdAt: '3310-01-01T00:00:00Z',
        startTimestamp: '3310-01-01T00:00:00Z',
      },
      rows: [
        {
          row: { id: 901, expeditionId: 21, position: 1000, rowType: 'region', regionId: 18 },
          number: 0,
          regionName: 'Inner Orion Spur',
          visitState: 'future',
        },
        {
          row: { id: 902, expeditionId: 21, position: 2000, rowType: 'waypoint', systemName: 'Sol' },
          number: 1,
          visitState: 'visited',
          visitedAtGalactic: '3310-01-01 00:00',
        },
        {
          row: { id: 903, expeditionId: 21, position: 3000, rowType: 'region', regionId: 17 },
          number: 0,
          regionName: 'Norma Expanse',
          visitState: 'future',
        },
        {
          row: { id: 904, expeditionId: 21, position: 4000, rowType: 'region', regionId: 16 },
          number: 0,
          regionName: 'Norma Arm',
          visitState: 'future',
        },
        {
          row: { id: 905, expeditionId: 21, position: 5000, rowType: 'waypoint', systemName: 'Rohini' },
          number: 2,
          visitState: 'visited',
          visitedAtGalactic: '3310-01-02 00:00',
          legModeId: 1,
          legModeColor: '#00D2FF',
        },
        {
          row: { id: 906, expeditionId: 21, position: 6000, rowType: 'region', regionId: 1 },
          number: 0,
          regionName: 'Galactic Centre',
          visitState: 'future',
        },
        {
          row: { id: 907, expeditionId: 21, position: 7000, rowType: 'waypoint', systemName: 'Sagittarius A*' },
          number: 3,
          visitState: 'current',
          legModeId: 2,
          legModeColor: '#00E5A3',
        },
        {
          row: { id: 908, expeditionId: 21, position: 8000, rowType: 'region', regionId: 2 },
          number: 0,
          regionName: 'Empyrean Straits',
          visitState: 'future',
        },
        {
          row: { id: 909, expeditionId: 21, position: 9000, rowType: 'waypoint', systemName: 'Beagle Point' },
          number: 4,
          visitState: 'future',
        },
        {
          row: { id: 910, expeditionId: 21, position: 10000, rowType: 'region', regionId: 3 },
          number: 0,
          regionName: 'Ryker\'s Hope',
          visitState: 'future',
        },
      ],
      waypointCount: 4,
      visitedWaypointCount: 2,
    })

    const html = renderPlanner(regionBorderPlan)
    const rowHtml = (rowId: number) => {
      const match = html.match(new RegExp(`<tr[^>]*data-row-id="${rowId}"[\\s\\S]*?<\\/tr>`))
      return match ? match[0] : ''
    }

    // Region before Origin (#01) matches Origin muted neutral border
    expect(rowHtml(901)).toContain('style="border-left-color:#3d2816"')
    expect(rowHtml(901)).toContain('border-l-2 shadow-[inset_0_-1px_0_0_#3a2615]')

    // Consecutive regions before visited waypoint (#02) both inherit its flight mode border
    expect(rowHtml(903)).toContain('style="border-left-color:#00D2FF"')
    expect(rowHtml(904)).toContain('style="border-left-color:#00D2FF"')

    // Region before in-progress current waypoint (#03) inherits its active flight mode border
    expect(rowHtml(906)).toContain('style="border-left-color:#00E5A3"')

    // Region before future waypoint (#04) and trailing region have no left border
    expect(rowHtml(908)).not.toContain('border-l-2')
    expect(rowHtml(910)).not.toContain('border-l-2')
  })
})

