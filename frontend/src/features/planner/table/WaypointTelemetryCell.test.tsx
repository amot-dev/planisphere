import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { makeRowView } from '../../../test/fixtures'
import { WaypointTelemetryCell, type WaypointTelemetryCellProps } from './WaypointTelemetryCell'

function renderTelemetryCell(props: WaypointTelemetryCellProps): string {
  return renderToStaticMarkup(
    <table>
      <tbody>
        <tr>
          <WaypointTelemetryCell {...props} />
        </tr>
      </tbody>
    </table>,
  )
}

describe('WaypointTelemetryCell', () => {
  it('renders distance from player on active next waypoint and distance from previous waypoint in planning', () => {
    const activeNextHtml = renderTelemetryCell({
      rowView: makeRowView({
        row: { id: 18, systemName: 'Maia', poiName: 'Black Hole' },
        number: 2,
        visitState: 'current',
        distFromPrevLy: 380.5,
        distFromCurrentLy: 42.1,
        isNextWaypoint: true,
      }),
      isVisited: false,
      isNext: true,
      playerSystem: 'Merope',
    })
    expect(activeNextHtml).toContain('+42.1 Ly')
    expect(activeNextHtml).not.toContain('380.5 Ly')
    expect(activeNextHtml).toContain(
      'Estimated straight-line distance from player location (Merope)',
    )

    const planningHtml = renderTelemetryCell({
      rowView: makeRowView({
        row: { id: 19, systemName: 'Maia' },
        number: 2,
        visitState: 'future',
        distFromPrevLy: 380.5,
        distFromCurrentLy: 42.1,
      }),
      isVisited: false,
      isNext: false,
      playerSystem: 'Merope',
    })
    expect(planningHtml).toContain('+380.5 Ly')
    expect(planningHtml).not.toContain('42.1 Ly')
    expect(planningHtml).toContain('Estimated straight-line distance from previous waypoint')
  })

  it('renders origin AWAITING DEPARTURE vs DEPARTED and destination LOGGED VISIT timestamps', () => {
    const stagedOriginHtml = renderTelemetryCell({
      rowView: makeRowView({
        row: { id: 60, visitedAt: '2026-01-18T10:00:00Z', departedAt: '' },
        number: 1,
        visitState: 'visited',
        visitedAtGalactic: '3312-01-18 10:00 UTC',
      }),
      isVisited: true,
      isNext: false,
    })
    expect(stagedOriginHtml).toContain('AWAITING DEPARTURE')
    expect(stagedOriginHtml).not.toContain('LOGGED VISIT')

    const departedOriginHtml = renderTelemetryCell({
      rowView: makeRowView({
        row: {
          id: 61,
          visitedAt: '2026-01-18T10:00:00Z',
          departedAt: '2026-01-18T12:00:00Z',
        },
        number: 1,
        visitState: 'visited',
        visitedAtGalactic: '3312-01-18 10:00 UTC',
        departedAtGalactic: '3312-01-18 12:00 UTC',
      }),
      isVisited: true,
      isNext: false,
    })
    expect(departedOriginHtml).toContain('DEPARTED')
    expect(departedOriginHtml).toContain('3312-01-18 12:00 UTC')

    const visitedDestinationHtml = renderTelemetryCell({
      rowView: makeRowView({
        row: {
          id: 62,
          systemName: 'Alpha Centauri',
          visitedAt: '2026-01-18T13:00:00Z',
          departedAt: '2026-01-18T14:30:00Z',
        },
        number: 2,
        visitState: 'visited',
        visitedAtGalactic: '3312-01-18 13:00 UTC',
        departedAtGalactic: '3312-01-18 14:30 UTC',
      }),
      isVisited: true,
      isNext: false,
    })
    expect(visitedDestinationHtml).toContain('LOGGED VISIT')
    expect(visitedDestinationHtml).toContain('3312-01-18 13:00 UTC')
  })
})
