import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { ExpeditionPlanner } from './ExpeditionPlanner'
import { CarrierRouteModal } from './CarrierRouteModal'
import { CarrierRouteBottomPanel } from './CarrierRouteBottomPanel'
import { NavigationTelemetrySection } from '../sidebar/NavigationTelemetrySection'
import { StatsSidebar } from '../sidebar/StatsSidebar'
import { app, expedition, stats } from '../../../wailsjs/go/models'

describe('ExpeditionPlanner Hook Stability and Lifecycle', () => {
  const renderPlanner = (plan: expedition.PlanView | null) =>
    renderToString(
      createElement(ExpeditionPlanner, {
        plan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

  it('renders safely when plan is null without throwing hook mismatch or null access', () => {
    const html = renderPlanner(null)

    expect(html).toContain('LOADING EXPEDITION FLIGHT PLAN')
  })

  it('renders active expedition rows, expansion chevrons, consecutive legs without dividers, and last-visited centering marker', () => {
    const mockPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 1,
        name: 'Beagle Point Survey',
        description: 'Deep reconnaissance',
        status: 'active',
        armedAt: '3310-01-01T00:00:00Z',
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
          visitState: 'current',
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
          visitState: 'future',
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
          visitState: 'unvisited',
        },
      ],
      modes: [
        { id: 1, name: 'Travel', color: '#FF7100' } as any,
      ],
      visitedCount: 2,
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
    // Only visited (Sol) and current (Alpha Centauri) rows have dropdown chevrons; future and unvisited do not
    const activeChevrons = html.match(/title="Toggle per-waypoint leg telemetry \/ distance drawer"/g)
    expect(activeChevrons).toHaveLength(2)
    // Waypoint 2 (row 402, index 1) is the last visited/current waypoint and gets data-last-visited="true"
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
        armedAt: '',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: [],
      modes: [],
      visitedCount: 0,
      waypointCount: 0,
      activeRowId: 0,
    })

    const html = renderPlanner(mockPlan)

    expect(html).toContain('Empty Survey')
    expect(html).toContain('Add Waypoint')
  })

  it('renders safely when plan.rows is null or missing', () => {
    const mockPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 3,
        name: 'Null Rows Survey',
        description: '',
        status: 'planning',
        armedAt: '',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: null as any,
      modes: [],
      visitedCount: 0,
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
        armedAt: '3310-01-01T00:00:00Z',
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
      visitedCount: 3,
      waypointCount: 3,
      activeRowId: 32,
      plannedLy: 10.3,
      coveredLy: 10.3,
      remainingLy: 0,
    })

    const html = renderPlanner(completedPlan)

    expect(html).toContain('Completed Voyage')
    expect(html).toContain('Reopen Expedition')
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
        armedAt: '',
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
          visitState: 'unvisited',
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
          visitState: 'unvisited',
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
      visitedCount: 0,
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
    const planningChevrons = html.match(/title="Toggle per-waypoint leg telemetry \/ distance drawer"/g)
    expect(planningChevrons).toBeNull()
    expect(html).not.toContain('data-last-visited="true"')
    expect(html).toContain('Add Waypoint')
    expect(html).toContain('Add Galactic Region Header')
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
        armedAt: '',
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
          visitState: 'future',
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
      visitedCount: 1,
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

    // With only Waypoint #1 visited (visitedCount: 1), Complete button is disabled until Waypoint #2 is visited:
    expect(html).toContain(
      'disabled="" title="Waypoint #2 not visited yet — complete requires at least 2 visited waypoints (or delete flight plan in Plans)"',
    )

    // Once Waypoint #2 is visited, Covered and Remaining update, Complete button becomes enabled,
    // and lastVisitedIndex advances across visitState === 'visited' rows without a 'current' row:
    const activePlanWithSecondVisited = expedition.PlanView.createFrom({
      ...activePlan,
      rows: activePlan.rows.map((rowView) =>
        rowView.row.id === 2 ? { ...rowView, visitState: 'visited', isNextWaypoint: false } : rowView,
      ),
      visitedCount: 2,
      coveredLy: 4.3,
      remainingLy: 5.0,
    })
    const midflightHtml = renderPlanner(activePlanWithSecondVisited)
    expect(midflightHtml).toContain('COVERED:')
    expect(midflightHtml).toContain('4.3 Ly')
    expect(midflightHtml).toContain('REMAINING:')
    expect(midflightHtml).toContain('5.0 Ly')
    expect(midflightHtml).toContain('title="Complete this expedition and finalize flight logs"')
    expect(midflightHtml).toMatch(/data-row-id="2"[^>]*data-last-visited="true"/)
    expect(midflightHtml).not.toMatch(/data-row-id="1"[^>]*data-last-visited="true"/)
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

  it('StatsSidebar renders LOG JUMP button in Expedition scope, disables it when carrierRouteOpen is true, and hides it when completed', () => {
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

    const html = renderStatsSidebar()
    expect(html).toContain('LOG JUMP')
    expect(html).not.toContain('CARRIER LOG')
    expect(html).not.toMatch(/disabled=""[^>]*>LOG JUMP/)

    const openRouteHtml = renderStatsSidebar({ carrierRouteOpen: true })
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
    const renderCarrierPanel = (overrides: Partial<Parameters<typeof CarrierRouteBottomPanel>[0]> & { route: app.CarrierRouteResult }) =>
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
      route: app.CarrierRouteResult.createFrom({
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
    expect(manualWithStop).toContain('Close')
    expect(manualWithStop).toContain('Existing jumps remain logged in your expedition statistics.')
    expect(manualWithStop).toContain('Sol')
    expect(manualWithStop).toContain('Colonia')
    expect(manualWithStop).toContain('value="412.5"')
    expect(manualWithStop).toContain('value="59"')
    expect(manualWithStop).toContain('Mark FC Stop Arrival')
    expect(manualWithStop).toContain(
      'Mark Fleet Carrier arrival at Colonia and close the carrier plotter',
    )
    expect(manualWithStop).not.toContain('ARRIVE AT WAYPOINT')
    expect(manualWithStop).not.toMatch(/disabled=""[^>]*>Mark FC Stop Arrival/)

    const manualWithoutStop = renderCarrierPanel({
      route: app.CarrierRouteResult.createFrom({
        origin: 'Sol',
        destination: '',
        carrierType: 'player',
        manualMode: true,
        jumps: [],
      }),
    })

    expect(manualWithoutStop).toContain('Sol')
    expect(manualWithoutStop).toContain('???')
    expect(manualWithoutStop).toContain('Mark FC Stop Arrival')
    expect(manualWithoutStop).toContain(
      'No next Fleet Carrier stop designated in the expedition plan',
    )
    expect(manualWithoutStop).toMatch(/disabled=""[^>]*>Mark FC Stop Arrival/)

    const plottedSpanshRoute = app.CarrierRouteResult.createFrom({
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
})

