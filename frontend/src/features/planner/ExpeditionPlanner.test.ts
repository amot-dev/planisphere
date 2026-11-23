import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { expedition } from '../../api/models'
import { ExpeditionPlanner } from './ExpeditionPlanner'
import { StartExpeditionModal } from './lifecycle/StartExpeditionModal'
import { PlannerHeaderBar } from './PlannerHeaderBar'

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
      modes: [{ id: 1, name: 'Travel', color: '#FF7100' } as any],
      visitedWaypointCount: 2,
      waypointCount: 4,
      activeRowId: 402,
    })

    const html = renderPlanner(mockPlan)

    expect(html).toContain('Beagle Point Survey')
    expect(html).toContain('2 / 4 WAYPOINTS VISITED')
    expect(html).toContain('FLIGHT MODE:')
    expect(html).not.toContain('ACTIVE EXPEDITION')
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
          row: {
            id: 30,
            expeditionId: 4,
            rowType: 'waypoint',
            systemName: 'Sol',
            seq: 1,
            visitedAt: '3310-01-01T10:00:00Z',
          },
          number: 1,
          visitState: 'visited',
        },
        {
          row: {
            id: 31,
            expeditionId: 4,
            rowType: 'waypoint',
            systemName: 'Alpha Centauri',
            seq: 2,
            visitedAt: '3310-01-01T11:00:00Z',
          },
          number: 2,
          visitState: 'visited',
          distFromPrevLy: 4.3,
        },
        {
          row: {
            id: 32,
            expeditionId: 4,
            rowType: 'waypoint',
            systemName: "Barnard's Star",
            seq: 3,
            visitedAt: '3310-01-01T12:00:00Z',
          },
          number: 3,
          visitState: 'visited',
          distFromPrevLy: 6.0,
        },
      ],
      modes: [{ id: 1, name: 'Travel', color: '#FF7100' } as any],
      visitedWaypointCount: 3,
      waypointCount: 3,
      activeRowId: 32,
      plannedLy: 10.3,
      coveredLy: 10.3,
      remainingLy: 0,
    })

    const html = renderPlanner(completedPlan)

    expect(html).toContain('Completed Voyage')
    expect(html).toContain('3 WAYPOINTS VISITED')
    expect(html).not.toContain('3 / 3 WAYPOINTS VISITED')
    expect(html).not.toContain('FLIGHT MODE:')
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
      modes: [{ id: 1, name: 'Travel', color: '#FF7100' } as any],
      visitedWaypointCount: 0,
      waypointCount: 2,
      activeRowId: 0,
      plannedLy: 4.3,
      remainingLy: 4.3,
    })

    const html = renderPlanner(planningPlan)

    expect(html).toContain('2 WAYPOINTS PLANNED')
    expect(html).not.toContain('PLANNING PHASE')
    expect(html).toContain('FLIGHT MODE:')

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
          row: {
            id: 1,
            expeditionId: 7,
            rowType: 'waypoint',
            systemName: 'Sol',
            seq: 1,
            visitedAt: '3310-01-01T10:00:00Z',
          },
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

    const startModalHtml = renderToString(
      createElement(StartExpeditionModal, {
        expeditionId: 1,
        modesList: [
          { id: 1, name: 'Exploration', color: '#00B3FF' } as any,
          { id: 3, name: 'Travel', color: '#FF7100' } as any,
        ],
        initialModeId: 3,
        onClose: () => {},
      }),
    )
    expect(startModalHtml).toContain('Engage Expedition')
    expect(startModalHtml).not.toContain('Lock Waypoint #1 Arrival')
    expect(startModalHtml).not.toContain('Custom Timestamp')
    expect(startModalHtml).not.toContain('Arm and Wait for First Arrival')
    expect(startModalHtml).toContain('Flight Mode')
    expect(startModalHtml).toContain('applies to any systems already visited')
  })
})
