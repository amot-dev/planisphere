import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { ExpeditionPlanner } from './ExpeditionPlanner'
import { CarrierRouteModal } from './CarrierRouteModal'
import { CarrierRouteBottomPanel } from './PlannerWidgets'
import { StatsSidebar } from './StatsSidebar'
import { WaypointRowItem } from './WaypointRowItem'
import { app, expedition, stats } from '../../wailsjs/go/models'

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

  it('renders safely when plan is populated with expedition rows', () => {
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
            id: 101,
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
            id: 102,
            expeditionId: 1,
            rowType: 'waypoint',
            systemName: 'Colonia',
            seq: 1,
            poiName: 'Jaques',
            notes: 'Refuel',
          },
          number: 2,
          visitState: 'unvisited',
        },
      ],
      modes: [],
      visitedCount: 1,
      waypointCount: 2,
      activeRowId: 101,
    })

    const html = renderPlanner(mockPlan)

    expect(html).toContain('Beagle Point Survey')
    expect(html).toContain('WAYPOINTS VISITED')
    expect(html).toContain('Sol')
    expect(html).toContain('Colonia')
    // Sol is visited, so it has the dropdown chevron button.
    // Colonia is unvisited (not arrived at yet), so it must NOT have the dropdown chevron button.
    const matches = html.match(/title="Toggle per-waypoint leg telemetry \/ distance drawer"/g)
    expect(matches).toHaveLength(1)
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

  it('renders safely in completed expedition status', () => {
    const mockPlan = expedition.PlanView.createFrom({
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
          row: {
            id: 201,
            expeditionId: 4,
            rowType: 'region',
            regionId: 1,
            seq: 0,
          },
          number: 0,
          regionName: 'Galactic Core',
          visitState: 'visited',
        },
      ],
      modes: [],
      visitedCount: 1,
      waypointCount: 1,
      activeRowId: 201,
    })

    const html = renderPlanner(mockPlan)

    expect(html).toContain('Completed Voyage')
    expect(html).toContain('Reopen Expedition')
  })

  it('applies CSS content-visibility: auto and contain-intrinsic-size: 44px on rows without spacer elements', () => {
    const mockPlan = expedition.PlanView.createFrom({
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
      ],
      modes: [],
      visitedCount: 0,
      waypointCount: 1,
      activeRowId: 0,
    })

    const html = renderPlanner(mockPlan)

    // Verify row styles include content-visibility and contain-intrinsic-size
    expect(html).toContain('content-visibility:auto')
    expect(html).toContain('contain-intrinsic-size:44px')

    // Verify no virtualization spacers exist
    expect(html).not.toContain('aria-hidden="true"><td')

    // Verify all rows are mounted directly
    expect(html).toContain('Sagittarius A*')
    expect(html).toContain('Supermassive Black Hole')
  })

  it('renders visited waypoints consecutively without inter-waypoint lines', () => {
    const mockPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 6,
        name: 'Multi-leg Voyage',
        description: '',
        status: 'active',
        armedAt: '3310-01-01T00:00:00Z',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: [
        {
          row: {
            id: 401,
            expeditionId: 6,
            rowType: 'waypoint',
            systemName: 'Sol',
            seq: 0,
          },
          number: 1,
          visitState: 'visited',
        },
        {
          row: {
            id: 402,
            expeditionId: 6,
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
      ],
      modes: [
        { id: 1, name: 'Travel', color: '#FF7100' } as any,
      ],
      visitedCount: 2,
      waypointCount: 2,
      activeRowId: 402,
    })

    const html = renderPlanner(mockPlan)

    expect(html).toContain('Sol')
    expect(html).toContain('Alpha Centauri')
    // Verify that visited waypoints are rendered without intermediate lines or banners
    expect(html).not.toContain('h-[3px]')
    expect(html).not.toContain('title="Leg Mode:')
  })

  it('does not render dropdown chevrons for unvisited waypoints or during planning', () => {
    const activePlan = expedition.PlanView.createFrom({
      expedition: {
        id: 7,
        name: 'Active Expedition',
        status: 'active',
      },
      rows: [
        {
          row: { id: 701, expeditionId: 7, rowType: 'waypoint', systemName: 'Visited-1' },
          number: 1,
          visitState: 'visited',
        },
        {
          row: { id: 702, expeditionId: 7, rowType: 'waypoint', systemName: 'Current-2' },
          number: 2,
          visitState: 'current',
        },
        {
          row: { id: 703, expeditionId: 7, rowType: 'waypoint', systemName: 'Future-3' },
          number: 3,
          visitState: 'future',
        },
        {
          row: { id: 704, expeditionId: 7, rowType: 'waypoint', systemName: 'Unvisited-4' },
          number: 4,
          visitState: 'unvisited',
        },
      ],
      modes: [],
      visitedCount: 2,
      waypointCount: 4,
    })

    const activeHtml = renderPlanner(activePlan)

    // Only Visited-1 and Current-2 should have dropdown chevrons
    const activeChevrons = activeHtml.match(/title="Toggle per-waypoint leg telemetry \/ distance drawer"/g)
    expect(activeChevrons).toHaveLength(2)

    const planningPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 8,
        name: 'Planning Expedition',
        status: 'planning',
      },
      rows: [
        {
          row: { id: 801, expeditionId: 8, rowType: 'waypoint', systemName: 'Planned-1' },
          number: 1,
          visitState: 'unvisited',
        },
      ],
      modes: [],
      visitedCount: 0,
      waypointCount: 1,
    })

    const planningHtml = renderPlanner(planningPlan)

    // Planning rows must not have dropdown chevrons
    const planningChevrons = planningHtml.match(/title="Toggle per-waypoint leg telemetry \/ distance drawer"/g)
    expect(planningChevrons).toBeNull()
  })

  it('marks the last visited waypoint with data-last-visited="true" for active expeditions to support middle centering', () => {
    const activePlan = expedition.PlanView.createFrom({
      expedition: {
        id: 9,
        name: 'Deep Space Active',
        status: 'active',
      },
      rows: [
        {
          row: { id: 901, expeditionId: 9, rowType: 'waypoint', systemName: 'Start-1' },
          number: 1,
          visitState: 'visited',
        },
        {
          row: { id: 902, expeditionId: 9, rowType: 'waypoint', systemName: 'Visited-2' },
          number: 2,
          visitState: 'visited',
        },
        {
          row: { id: 903, expeditionId: 9, rowType: 'waypoint', systemName: 'Next-3' },
          number: 3,
          visitState: 'unvisited',
        },
      ],
      modes: [],
      visitedCount: 2,
      waypointCount: 3,
    })

    const html = renderPlanner(activePlan)

    // Waypoint 2 is the last visited waypoint (index 1), so it must have data-last-visited="true"
    expect(html).toContain('data-row-id="902"')
    expect(html).toContain('data-row-index="1"')
    expect(html).toMatch(/data-row-id="902"[^>]*data-last-visited="true"/)

    // Waypoint 1 (index 0) was visited earlier, so it must not have data-last-visited="true"
    expect(html).not.toMatch(/data-row-id="901"[^>]*data-last-visited="true"/)
  })

  it('renders planning expedition with bottom add buttons and breathing room for scrolling to the bottom', () => {
    const planningPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 10,
        name: 'Planning Survey',
        status: 'planning',
      },
      rows: [
        {
          row: { id: 1001, expeditionId: 10, rowType: 'waypoint', systemName: 'WP-1' },
          number: 1,
          visitState: 'unvisited',
        },
      ],
      modes: [],
      visitedCount: 0,
      waypointCount: 1,
    })

    const html = renderPlanner(planningPlan)

    // Planning must not mark rows as data-last-visited
    expect(html).not.toContain('data-last-visited="true"')
    // Must contain Add Waypoint button and breathing room spacer at bottom
    expect(html).toContain('Add Waypoint')
    expect(html).toContain('Add Galactic Region Header')
    expect(html).toContain('h-48 shrink-0 pointer-events-none')
  })

  it('skips reloading details for visited open drawers when visitedAt has not changed', () => {
    const existingDetails: Record<number, { to: string }> = {
      101: { to: '3310-01-01T12:00:00Z' },
    }
    const currentExpanded: Record<number, boolean> = { 101: true, 102: true }
    const rows = [
      { row: { id: 101, visitedAt: '3310-01-01T12:00:00Z' }, visitState: 'visited' },
      { row: { id: 102, visitedAt: '3310-01-02T15:00:00Z' }, visitState: 'visited' },
    ]

    const toLoad: number[] = []
    for (const id of [101, 102]) {
      if (currentExpanded[id]) {
        const rowView = rows.find((r) => r.row.id === id)
        const isVisited = rowView && (rowView.visitState === 'visited' || rowView.visitState === 'current')
        if (isVisited) {
          const loaded = existingDetails[id]
          if (loaded && loaded.to === (rowView.row.visitedAt ?? '')) {
            continue
          }
          toLoad.push(id)
        }
      }
    }

    // Row 101's visitedAt has not changed, so it is skipped.
    // Row 102 is not in existingDetails, so it must be loaded.
    expect(toLoad).toEqual([102])
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

    // Once Waypoint #2 is visited, Covered and Remaining update and Complete button becomes enabled:
    const activePlanWithSecondVisited = expedition.PlanView.createFrom({
      ...activePlan,
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
  })

  it('renders only Planned Route in planning state', () => {
    const planningPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 8,
        name: 'Planning State Expedition',
        description: '',
        status: 'planning',
        armedAt: '',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: [
        {
          row: { id: 10, expeditionId: 8, rowType: 'waypoint', systemName: 'Sol', seq: 1 },
          number: 1,
          visitState: 'future',
        },
        {
          row: { id: 11, expeditionId: 8, rowType: 'waypoint', systemName: 'Alpha Centauri', seq: 2 },
          number: 2,
          visitState: 'future',
          distFromPrevLy: 4.3,
        },
      ],
      modes: [],
      visitedCount: 0,
      waypointCount: 2,
      plannedLy: 4.3,
      remainingLy: 4.3,
    })

    const html = renderPlanner(planningPlan)

    expect(html).toContain('PLANNED ROUTE:')
    expect(html).toContain('4.3 Ly')
    expect(html).not.toContain('REMAINING:')
    expect(html).not.toContain('COVERED:')
    expect(html).not.toContain('COMPLETED ROUTE:')
  })

  it('renders only Completed Route in completed state', () => {
    const completedPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 10,
        name: 'Finished Expedition',
        description: '',
        status: 'completed',
        armedAt: '',
        createdAt: '3310-01-01T00:00:00Z',
        updatedAt: '3310-01-01T00:00:00Z',
      },
      rows: [
        {
          row: { id: 30, expeditionId: 10, rowType: 'waypoint', systemName: 'Sol', seq: 1, visitedAt: '3310-01-01T10:00:00Z' },
          number: 1,
          visitState: 'visited',
        },
        {
          row: { id: 31, expeditionId: 10, rowType: 'waypoint', systemName: 'Alpha Centauri', seq: 2, visitedAt: '3310-01-01T11:00:00Z' },
          number: 2,
          visitState: 'visited',
          distFromPrevLy: 4.3,
        },
        {
          row: { id: 32, expeditionId: 10, rowType: 'waypoint', systemName: "Barnard's Star", seq: 3, visitedAt: '3310-01-01T12:00:00Z' },
          number: 3,
          visitState: 'visited',
          distFromPrevLy: 6.0,
        },
      ],
      modes: [],
      visitedCount: 3,
      waypointCount: 3,
      plannedLy: 10.3,
      coveredLy: 10.3,
      remainingLy: 0,
    })

    const html = renderPlanner(completedPlan)

    expect(html).toContain('COMPLETED ROUTE:')
    expect(html).toContain('10.3 Ly')
    expect(html).not.toContain('PLANNED ROUTE:')
    expect(html).not.toContain('REMAINING:')
    expect(html).not.toContain('COVERED:')
  })

  it('CarrierRouteModal omits Manual Entry Mode and keeps QUERYING SPANSH... single-line', () => {
    const idleHtml = renderToString(
      createElement(CarrierRouteModal, {
        prompt: {
          targetRowId: 10,
          origin: 'Sol',
          destination: 'Colonia',
          capacityUsed: '5000',
          depotTritium: '1000',
          carrierType: 'player',
          loading: false,
        },
        onClose: () => {},
        onUpdate: () => {},
        onStart: () => {},
      }),
    )

    expect(idleHtml).not.toContain('Manual Entry Mode')
    expect(idleHtml).toContain('PLOT VIA SPANSH')
    expect(idleHtml).toContain('whitespace-nowrap')

    const loadingHtml = renderToString(
      createElement(CarrierRouteModal, {
        prompt: {
          targetRowId: 10,
          origin: 'Sol',
          destination: 'Colonia',
          capacityUsed: '5000',
          depotTritium: '1000',
          carrierType: 'player',
          loading: true,
        },
        onClose: () => {},
        onUpdate: () => {},
        onStart: () => {},
      }),
    )

    expect(loadingHtml).toContain('QUERYING SPANSH...')
    expect(loadingHtml).toContain('whitespace-nowrap')
  })

  it('StatsSidebar renders LOG JUMP button in Expedition scope, disables it when carrierRouteOpen is true, and hides it when completed', () => {
    const emptyFilter = stats.Filter.createFrom({ enabled: false, modes: [] })
    const html = renderToString(
      createElement(StatsSidebar, {
        sidebarStats: null,
        statsLoading: false,
        modesState: null,
        filter: emptyFilter,
        onFilterChange: () => {},
        onQuickSwitchMode: () => {},
        onLogCarrierJump: () => {},
      }),
    )

    expect(html).toContain('LOG JUMP')
    expect(html).not.toContain('CARRIER LOG')
    expect(html).not.toMatch(/disabled=""[^>]*>LOG JUMP/)

    const openRouteHtml = renderToString(
      createElement(StatsSidebar, {
        sidebarStats: null,
        statsLoading: false,
        modesState: null,
        filter: emptyFilter,
        carrierRouteOpen: true,
        onFilterChange: () => {},
        onQuickSwitchMode: () => {},
        onLogCarrierJump: () => {},
      }),
    )

    expect(openRouteHtml).toContain('Carrier routing panel is already open')
    expect(openRouteHtml).toMatch(/disabled=""[^>]*>LOG JUMP/)

    const completedHtml = renderToString(
      createElement(StatsSidebar, {
        sidebarStats: app.SidebarStats.createFrom({ status: 'completed' }),
        statsLoading: false,
        modesState: null,
        filter: emptyFilter,
        onFilterChange: () => {},
        onQuickSwitchMode: () => {},
        onLogCarrierJump: () => {},
      }),
    )

    expect(completedHtml).not.toContain('LOG JUMP')
  })

  it('CarrierRouteBottomPanel renders Close button with tooltip, start/end indicator with ??? fallback, and Mark FC Stop Arrival button', () => {
    const manualWithStop = renderToString(
      createElement(CarrierRouteBottomPanel, {
        route: app.CarrierRouteResult.createFrom({
          origin: 'Sol',
          destination: 'Colonia',
          carrierType: 'player',
          manualMode: true,
          jumps: [],
        }),
        jumpIndex: 0,
        manualMode: true,
        hasNextFcStop: true,
        nextFcStopSystem: 'Colonia',
        onCancelRoute: () => {},
        onToggleManualMode: () => {},
        onCompleteSpanshJump: () => {},
        manualDistanceInput: '412.5',
        onChangeManualDistance: () => {},
        manualTritiumInput: '59',
        onChangeManualTritium: () => {},
        onLogManualJump: () => {},
        manualLegJumps: 2,
        manualLegDistance: 950,
        manualLegTritium: 180,
        loggingJump: false,
        copiedNextJump: false,
        onCopySystem: () => {},
        onArrive: () => {},
      }),
    )

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

    const manualWithoutStop = renderToString(
      createElement(CarrierRouteBottomPanel, {
        route: app.CarrierRouteResult.createFrom({
          origin: 'Sol',
          destination: '',
          carrierType: 'player',
          manualMode: true,
          jumps: [],
        }),
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
      }),
    )

    expect(manualWithoutStop).toContain('Sol')
    expect(manualWithoutStop).toContain('???')
    expect(manualWithoutStop).toContain('Mark FC Stop Arrival')
    expect(manualWithoutStop).toContain(
      'No next Fleet Carrier stop designated in the expedition plan',
    )
    expect(manualWithoutStop).toMatch(/disabled=""[^>]*>Mark FC Stop Arrival/)

    const spanshModeHtml = renderToString(
      createElement(CarrierRouteBottomPanel, {
        route: app.CarrierRouteResult.createFrom({
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
              systemName: 'Gandharvi',
              distanceLy: 310.4,
              tritiumUsed: 44,
              fuelInTank: 888,
              hasIcyRing: false,
            },
          ],
        }),
        jumpIndex: 0,
        manualMode: false,
        hasNextFcStop: true,
        nextFcStopSystem: 'Beagle Point',
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
      }),
    )

    expect(spanshModeHtml).toContain('Colonia')
    expect(spanshModeHtml).not.toContain('Beagle Point')
    expect(spanshModeHtml).toContain('805.6 Ly')
    expect(spanshModeHtml).toMatch(/~(?:<!-- -->)?112(?:<!-- -->)? t/)

    const lockedFcRowHtml = renderToString(
      createElement(WaypointRowItem, {
        rowView: expedition.RowView.createFrom({
          row: {
            id: 501,
            expeditionId: 1,
            rowType: 'waypoint',
            systemName: 'Rohini',
            fcStop: true,
            fcArrived: false,
          },
          number: 2,
          visitState: 'unvisited',
        }),
        index: 1,
        isPlanning: false,
        isLocked: false,
        isFcStopLocked: true,
        isBeingDragged: false,
        isDragTarget: false,
        isExpanded: false,
        isNextFcStop: false,
        hasActiveCarrierRoute: true,
        draft: { systemName: 'Rohini', poiName: '', notes: '' },
        copiedSystemRowId: null,
        insertMenuOpen: false,
        lastVisitedIndex: 0,
        totalRows: 3,
        rowStyle: {},
        dropProps: {},
        onToggleExpand: () => {},
        onDraftChange: () => {},
        onFieldFocus: () => {},
        onFieldBlur: () => {},
        onCopySystem: () => {},
        onToggleFcStop: () => {},
        onOpenCarrierRoutePrompt: () => {},
        onToggleInsertMenu: () => {},
        onInsert: () => {},
        onMove: () => {},
        onDelete: () => {},
        onDragStart: () => {},
        onDragEnd: () => {},
      }),
    )

    expect(lockedFcRowHtml).toContain('ROUTING')
    expect(lockedFcRowHtml).not.toContain('Remove Fleet Carrier stop designation')
    expect(lockedFcRowHtml).not.toContain('+ FC STOP')
  })
})

