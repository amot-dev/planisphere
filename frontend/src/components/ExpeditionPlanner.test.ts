import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { ExpeditionPlanner } from './ExpeditionPlanner'
import { expedition } from '../../wailsjs/go/models'

describe('ExpeditionPlanner Hook Stability & Lifecycle', () => {
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

  it('applies CSS content-visibility: auto on leg banners in active expeditions', () => {
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
    // Verify that the leg banner itself is rendered with its transition and mode data
    expect(html).toContain('[VESSEL FLOWN]:')
    expect(html).toContain('Krait Phantom')
    // Verify that the <tr> specifically wrapping the leg banner includes content-visibility and contain-intrinsic-size
    expect(html).toMatch(
      /<tr style="[^"]*content-visibility:auto[^"]*contain-intrinsic-size:44px[^"]*"><td colSpan="8"[^>]*>[\s\S]*?\[VESSEL FLOWN\]:/,
    )
  })
})

