import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { ExpeditionPlanner } from './ExpeditionPlanner'
import { expedition } from '../../wailsjs/go/models'

describe('ExpeditionPlanner Hook Stability and Lifecycle', () => {
  it('renders safely when plan is null without throwing hook mismatch or null access', () => {
    const html = renderToString(
      createElement(ExpeditionPlanner, {
        plan: null,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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

    const html = renderToString(
      createElement(ExpeditionPlanner, {
        plan: mockPlan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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

    const html = renderToString(
      createElement(ExpeditionPlanner, {
        plan: mockPlan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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

    const html = renderToString(
      createElement(ExpeditionPlanner, {
        plan: mockPlan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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

    const html = renderToString(
      createElement(ExpeditionPlanner, {
        plan: mockPlan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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

    const html = renderToString(
      createElement(ExpeditionPlanner, {
        plan: mockPlan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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

    const html = renderToString(
      createElement(ExpeditionPlanner, {
        plan: mockPlan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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

    const activeHtml = renderToString(
      createElement(ExpeditionPlanner, {
        plan: activePlan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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

    const planningHtml = renderToString(
      createElement(ExpeditionPlanner, {
        plan: planningPlan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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

    const html = renderToString(
      createElement(ExpeditionPlanner, {
        plan: activePlan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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

    const html = renderToString(
      createElement(ExpeditionPlanner, {
        plan: planningPlan,
        onPlanOptimisticUpdate: () => {},
        onOpenStartModal: () => {},
        onRefreshPlan: () => {},
        onRefreshAll: () => {},
        onBusyChange: () => {},
      }),
    )

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
})

