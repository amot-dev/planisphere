// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import {
  makeMode,
  makeOriginRowView,
  makeRowView,
  makeWaypointDetails,
} from '../../../test/fixtures'
import { mountTestRoot } from '../../../test/render'
import { WaypointDetailsDrawer } from './WaypointDetailsDrawer'

describe('WaypointDetailsDrawer', () => {
  it('renders WaypointDetailsDrawer with matching border-l-2, Vessel Flown, and 1-Click Mode toggle for leg waypoints (number > 1)', () => {
    const mockRowView = makeRowView({
      row: {
        id: 15,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Alpha Centauri',
      },
      number: 2,
      visitState: 'visited',
      legModeId: 10,
      legModeName: 'Exploration',
      legModeColor: '#00D2FF',
    })

    const mockDetails = makeWaypointDetails({
      kind: 'historical',
      number: 2,
      shipsUsed: ['Anaconda'],
      metrics: {
        distanceLy: 120.5,
        shipDistanceLy: 120.5,
        jumps: 3,
      },
    })

    const mockModes = [
      makeMode({ id: 10, name: 'Exploration', color: '#00D2FF' }),
      makeMode({ id: 20, name: 'Travel', color: '#FF7100' }),
    ]

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: mockDetails,
        loading: false,
        rowView: mockRowView,
        modeId: 10,
        modeColor: '#00D2FF',
        modesList: mockModes,
        onChangeLegMode: () => {},
      }),
    )

    expect(html).toContain('style="border-left-color:#00D2FF"')
    expect(html).toContain(
      'border-l-2 shadow-[inset_0_1px_0_0_var(--color-ed-border),inset_0_-1px_0_0_var(--color-ed-border-subtle)]',
    )
    expect(html).not.toContain('border-l-3')
    expect(html).toContain('[VESSEL FLOWN]:')
    expect(html).toContain('Anaconda')
    expect(html).toContain('[FLIGHT MODE]:')
    expect(html).toContain('Exploration')
    expect(html).toContain('Flight mode for this leg (click to change)')
  })

  it('colors origin drawers using muted neutral border and omits Vessel Flown and Mode toggle on baseline waypoint #1', () => {
    const originRowView = makeOriginRowView()

    const originLoadingDrawerHtml = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: null,
        loading: true,
        rowView: originRowView,
      }),
    )
    expect(originLoadingDrawerHtml).toContain('style="border-left-color:#3d2816"')

    const originDrawerHtml = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: makeWaypointDetails({ kind: 'origin', number: 1 }),
        loading: false,
        rowView: originRowView,
      }),
    )
    expect(originDrawerHtml).toContain('style="border-left-color:#3d2816"')
    expect(originDrawerHtml).toContain('HISTORICAL BASELINE')
    expect(originDrawerHtml).not.toContain('[VESSEL FLOWN]:')
    expect(originDrawerHtml).not.toContain('[FLIGHT MODE]:')
  })

  it('renders Realized Value card with combined total sold and 6 orange negative lines without emojis', () => {
    const mockRowView = makeRowView({
      row: {
        id: 2,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Colonia',
      },
      number: 2,
      visitState: 'visited',
    })

    const mockDetails = makeWaypointDetails({
      kind: 'leg',
      number: 2,
      metrics: {
        scanValueTotal: 50000000,
        organicValueTotal: 95000000,
      },
      realization: {
        totalRealized: 120000000,
        cartoLostDeath: 5000000,
        organicLostDeath: 10000000,
        cartoStolen: 2000000,
        organicStolen: 8000000,
        carrierTariffLoss: 3500000,
        npcCrewLoss: 1200000,
      },
    })

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: mockDetails,
        loading: false,
        rowView: mockRowView,
      }),
    )

    // Check Realized Value card header
    expect(html).toContain('Realized Value')
    expect(html).toContain('120,000,000 Cr')

    // Check the six orange negative deduction lines
    expect(html).toContain('Cartographics Lost (Death)')
    expect(html).toContain('-5,000,000 Cr')

    expect(html).toContain('Exobiology Lost (Death)')
    expect(html).toContain('-10,000,000 Cr')

    expect(html).toContain('Cartographics Stolen')
    expect(html).toContain('-2,000,000 Cr')

    expect(html).toContain('Exobiology Stolen')
    expect(html).toContain('-8,000,000 Cr')

    expect(html).toContain('Carrier Tariffs')
    expect(html).toContain('-3,500,000 Cr')

    expect(html).toContain('NPC Crew Payouts')
    expect(html).toContain('-1,200,000 Cr')

    // Ensure the header and all six deduction values have orange text-ed-amber styling
    const orangeMatches = html.match(/text-ed-amber\b/g)
    expect(orangeMatches && orangeMatches.length).toBeGreaterThanOrEqual(7)

    // Ensure strictly no emojis anywhere in the drawer HTML
    const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u
    expect(emojiRegex.test(html)).toBe(false)
  })

  it('renders WaypointDetailsDrawer for active current leg with IN PROGRESS and TBD fallback', () => {
    const currentLegRowView = makeRowView({
      row: {
        id: 22,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: '',
      },
      number: 3,
      visitState: 'current',
    })

    const currentLegDetails = makeWaypointDetails({
      kind: 'leg',
      number: 3,
      prevSystem: 'Alpha Centauri',
      fromGalactic: '3312-01-19 12:00 UTC',
      toGalactic: '',
      elapsed: '4h 15m',
    })

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: currentLegDetails,
        loading: false,
        rowView: currentLegRowView,
      }),
    )

    expect(html).toContain('LEG #2 <span class="relative -top-px">→</span> #3 // DELTA TELEMETRY')
    expect(html).toContain(
      'Alpha Centauri <span class="relative -top-0.5">→</span> <span class="text-ed-text-main font-bold">TBD</span>',
    )
    expect(html).toContain(
      '3312-01-19 12:00 UTC <span class="relative -top-0.5">→</span> <span class="text-ed-cyan font-bold">IN PROGRESS</span>',
    )
    expect(html).not.toContain('[4h 15m]')
    expect(html).toContain('[IN TRANSIT]:')
    expect(html).toContain('4h 15m')
    expect(html).not.toContain('[IN SYSTEM]:')
  })

  it('renders [IN SYSTEM]: duration for departed, completed, and live stays without arrived or departed indicators', () => {
    const legRowView = makeRowView({
      row: { id: 25, expeditionId: 1, rowType: 'waypoint', systemName: 'Rohini' },
      number: 2,
      visitState: 'visited',
    })

    const departedHtml = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: makeWaypointDetails({
          kind: 'leg',
          number: 2,
          prevSystem: 'Sol',
          fromGalactic: '3312-01-19 12:00 UTC',
          toGalactic: '3312-01-19 16:30 UTC',
          arrivedAt: '2026-01-19T15:00:00Z',
          departedAt: '2026-01-19T16:30:00Z',
          elapsed: '4h 30m',
        }),
        loading: false,
        rowView: legRowView,
      }),
    )
    expect(departedHtml).toContain('[IN SYSTEM]:')
    expect(departedHtml).toContain('1h 30m')
    expect(departedHtml).not.toContain('[ARRIVED]:')
    expect(departedHtml).not.toContain('[DEPARTED]:')
    expect(departedHtml).not.toContain('[FINAL ACTIVITY]:')

    const completedFinalHtml = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: makeWaypointDetails({
          kind: 'leg',
          number: 2,
          prevSystem: 'Sol',
          fromGalactic: '3312-01-19 12:00 UTC',
          to: '2026-01-19T15:40:00Z',
          toGalactic: '3312-01-19 15:40 UTC',
          arrivedAt: '2026-01-19T15:00:00Z',
          departedAt: '',
          inSystem: false,
          elapsed: '3h 40m',
        }),
        loading: false,
        rowView: legRowView,
      }),
    )
    expect(completedFinalHtml).toContain('[IN SYSTEM]:')
    expect(completedFinalHtml).toContain('0h 40m')
    expect(completedFinalHtml).not.toContain('[ARRIVED]:')
    expect(completedFinalHtml).not.toContain('[FINAL ACTIVITY]:')

    const unvisitedCurrentHtml = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: makeWaypointDetails({
          kind: 'leg',
          number: 2,
          prevSystem: 'Sol',
          fromGalactic: '3312-01-19 12:00 UTC',
          toGalactic: '',
          arrivedAt: '',
          inSystem: false,
          elapsed: '2h 00m',
        }),
        loading: false,
        rowView: legRowView,
      }),
    )
    expect(unvisitedCurrentHtml).not.toContain('[IN SYSTEM]:')

    const stagedOriginHtml = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: makeWaypointDetails({
          kind: 'origin',
          number: 1,
          fromGalactic: '3310-01-01 00:00 UTC',
          toGalactic: '',
          arrivedAt: '2026-01-19T10:00:00Z',
          inSystem: true,
        }),
        loading: false,
        rowView: makeOriginRowView(),
      }),
    )
    expect(stagedOriginHtml).toContain(
      '3310-01-01 00:00 UTC <span class="relative -top-0.5">→</span> <span class="text-ed-cyan font-bold">IN SYSTEM</span>',
    )
    expect(stagedOriginHtml).not.toContain('[IN SYSTEM]:')
  })

  it('renders computing placeholder when details has an empty kind so zero-valued payloads never render LEG #-1', () => {
    const legRowView = makeRowView({
      row: { id: 30, expeditionId: 1, rowType: 'waypoint', systemName: 'Colonia' },
      number: 2,
      visitState: 'current',
    })

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: makeWaypointDetails({ rowId: 0, kind: '', number: 0 }),
        loading: false,
        rowView: legRowView,
      }),
    )

    expect(html).toContain('[ COMPUTING WAYPOINT TELEMETRY... ]')
    expect(html).not.toContain('LEG #-1')
  })

  it('updates live [IN SYSTEM]: duration every minute while in system and pauses when clockSuspended is true', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-19T15:10:00Z'))

    const legRowView = makeRowView({
      row: { id: 26, expeditionId: 1, rowType: 'waypoint', systemName: 'Rohini' },
      number: 2,
      visitState: 'visited',
    })
    const liveDetails = makeWaypointDetails({
      kind: 'leg',
      number: 2,
      prevSystem: 'Sol',
      fromGalactic: '3312-01-19 12:00 UTC',
      toGalactic: '',
      arrivedAt: '2026-01-19T15:00:00Z',
      inSystem: true,
      elapsed: '3h 10m',
    })

    const mounted = mountTestRoot(
      createElement(WaypointDetailsDrawer, {
        details: liveDetails,
        loading: false,
        rowView: legRowView,
        clockSuspended: false,
      }),
    )
    expect(mounted.container.textContent).toContain('[IN SYSTEM]:0h 10m')

    await act(async () => {
      vi.advanceTimersByTime(60_000)
    })
    expect(mounted.container.textContent).toContain('[IN SYSTEM]:0h 11m')

    // Pausing live sync suspends the 60-second timer until resumed.
    mounted.rerender(
      createElement(WaypointDetailsDrawer, {
        details: liveDetails,
        loading: false,
        rowView: legRowView,
        clockSuspended: true,
      }),
    )
    await act(async () => {
      vi.advanceTimersByTime(180_000)
    })
    expect(mounted.container.textContent).toContain('[IN SYSTEM]:0h 11m')

    // Resuming immediately refreshes to current elapsed duration.
    mounted.rerender(
      createElement(WaypointDetailsDrawer, {
        details: liveDetails,
        loading: false,
        rowView: legRowView,
        clockSuspended: false,
      }),
    )
    expect(mounted.container.textContent).toContain('[IN SYSTEM]:0h 14m')
  })

  it('places [IN TRANSIT]: left of [IN SYSTEM]: with plain-text primary/hover swap and keeps origin header duration swap', () => {
    const legRowView = makeRowView({
      row: { id: 40, expeditionId: 1, rowType: 'waypoint', systemName: 'Colonia' },
      number: 2,
      visitState: 'visited',
    })
    const legDetails = makeWaypointDetails({
      kind: 'leg',
      number: 2,
      prevSystem: 'Sol',
      from: '2026-01-19T12:00:00Z',
      arrivedAt: '2026-01-19T15:00:00Z',
      departedAt: '2026-01-19T16:30:00Z',
      to: '2026-01-19T16:30:00Z',
      fromGalactic: '3312-01-19 12:00 UTC',
      toGalactic: '3312-01-19 16:30 UTC',
      elapsed: '4h 30m',
      transitElapsed: '3h 00m',
      transitInGameDuration: '1h 10m',
      inSystemInGameDuration: '45m',
      inGameDuration: '1h 55m',
    })

    const calendarHtml = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: legDetails,
        loading: false,
        rowView: legRowView,
        durationDisplayMode: 'calendar',
      }),
    )
    expect(calendarHtml).not.toContain('[4h 30m]')
    expect(calendarHtml).not.toContain('underline')
    const transitIndex = calendarHtml.indexOf('[IN TRANSIT]:')
    const inSystemIndex = calendarHtml.indexOf('[IN SYSTEM]:')
    expect(transitIndex).toBeGreaterThan(-1)
    expect(inSystemIndex).toBeGreaterThan(transitIndex)
    expect(calendarHtml).toContain('title="In-game time: 1h 10m"')
    expect(calendarHtml).toContain('>3h 00m</span>')
    expect(calendarHtml).toContain('title="In-game time: 45m"')
    expect(calendarHtml).toContain('>1h 30m</span>')

    const inGameHtml = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: legDetails,
        loading: false,
        rowView: legRowView,
        durationDisplayMode: 'in_game',
      }),
    )
    expect(inGameHtml).toContain('title="Calendar elapsed: 3h 00m"')
    expect(inGameHtml).toContain('>1h 10m</span>')
    expect(inGameHtml).toContain('title="Calendar elapsed: 1h 30m"')
    expect(inGameHtml).toContain('>45m</span>')

    const originInGameHtml = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: makeWaypointDetails({
          kind: 'origin',
          number: 1,
          elapsed: '45d 02h 10m',
          inGameDuration: '110h 15m',
        }),
        loading: false,
        rowView: makeOriginRowView(),
        durationDisplayMode: 'in_game',
      }),
    )
    expect(originInGameHtml).toContain('title="Calendar elapsed: 45d 02h 10m"')
    expect(originInGameHtml).toContain('[110h 15m]')
  })
})
