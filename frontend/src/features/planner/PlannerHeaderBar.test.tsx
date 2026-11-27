import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { expedition } from '../../api/models'
import { makeMode } from '../../test/fixtures'
import { PlannerHeaderBar, type PlannerHeaderBarProps } from './PlannerHeaderBar'

function renderHeaderBar(
  plan: expedition.PlanView,
  overrides: Partial<PlannerHeaderBarProps> = {},
): string {
  return renderToString(
    <PlannerHeaderBar
      plan={plan}
      hasOriginWaypointSystem={true}
      onOpenStartModal={() => {}}
      onLifecycleAction={async () => {}}
      {...overrides}
    />,
  )
}

describe('PlannerHeaderBar', () => {
  it('renders Planning badge, flight mode selector, Planned Route distance, and Engage origin requirement', () => {
    const planningPlan = expedition.PlanView.createFrom({
      expedition: { id: 5, name: 'Native Scrolling Voyage', status: 'planning' },
      modes: [makeMode({ id: 1, name: 'Travel', color: '#FF7100' })],
      waypointCount: 2,
      visitedWaypointCount: 0,
      plannedLy: 4.3,
      remainingLy: 4.3,
    })

    const readyHtml = renderHeaderBar(planningPlan, { hasOriginWaypointSystem: true })
    expect(readyHtml).toContain('2 WAYPOINTS PLANNED')
    expect(readyHtml).toContain('FLIGHT MODE:')
    expect(readyHtml).toContain('PLANNED ROUTE:')
    expect(readyHtml).toContain('4.3 Ly')
    expect(readyHtml).not.toContain('REMAINING:')
    expect(readyHtml).not.toContain('COVERED:')
    expect(readyHtml).not.toContain('COMPLETED ROUTE:')
    expect(readyHtml).toContain('title="Engage expedition (lock arrival at Waypoint #1)"')

    const missingOriginHtml = renderHeaderBar(planningPlan, { hasOriginWaypointSystem: false })
    expect(missingOriginHtml).toContain(
      'disabled="" title="Enter a star system for Waypoint #1 before engaging the expedition"',
    )
  })

  it('gates Complete button on two visited waypoints and closed carrier route while updating Covered and Remaining Ly', () => {
    const stagedActivePlan = expedition.PlanView.createFrom({
      expedition: { id: 7, name: 'Live Telemetry Expedition', status: 'active' },
      visitedWaypointCount: 1,
      waypointCount: 3,
      coveredLy: 0,
      remainingLy: 8.1,
    })

    const stagedHtml = renderHeaderBar(stagedActivePlan)
    expect(stagedHtml).toContain('1 / 3 WAYPOINTS VISITED')
    expect(stagedHtml).toContain('COVERED:')
    expect(stagedHtml).toContain('REMAINING:')
    expect(stagedHtml).toContain('8.1 Ly')
    expect(stagedHtml).toContain(
      'disabled="" title="Waypoint #2 not visited yet — complete requires at least 2 visited waypoints (or delete expedition in Expeditions)"',
    )

    const underwayActivePlan = expedition.PlanView.createFrom({
      ...stagedActivePlan,
      visitedWaypointCount: 2,
      coveredLy: 4.3,
      remainingLy: 5.0,
    })
    const underwayHtml = renderHeaderBar(underwayActivePlan)
    expect(underwayHtml).toContain('4.3 Ly')
    expect(underwayHtml).toContain('5.0 Ly')
    expect(underwayHtml).toContain('>COMPLETE</button>')

    const carrierRouteOpenHtml = renderHeaderBar(underwayActivePlan, { carrierRouteOpen: true })
    expect(carrierRouteOpenHtml).toContain(
      'disabled="" title="Close or finish the active Fleet Carrier route before completing the expedition"',
    )
  })

  it('renders Completed badge, Reopen action, and Completed Route summary without flight mode selector', () => {
    const completedPlan = expedition.PlanView.createFrom({
      expedition: { id: 4, name: 'Completed Voyage', status: 'completed' },
      modes: [makeMode({ id: 1, name: 'Travel', color: '#FF7100' })],
      visitedWaypointCount: 3,
      waypointCount: 3,
      plannedLy: 10.3,
      coveredLy: 10.3,
      remainingLy: 0,
    })

    const completedHtml = renderHeaderBar(completedPlan)
    expect(completedHtml).toContain('3 WAYPOINTS VISITED')
    expect(completedHtml).not.toContain('3 / 3 WAYPOINTS VISITED')
    expect(completedHtml).not.toContain('FLIGHT MODE:')
    expect(completedHtml).toContain('REOPEN')
    expect(completedHtml).toContain('COMPLETED ROUTE:')
    expect(completedHtml).toContain('10.3 Ly')
    expect(completedHtml).not.toContain('PLANNED ROUTE:')
    expect(completedHtml).not.toContain('REMAINING:')
    expect(completedHtml).not.toContain('COVERED:')
  })
})
