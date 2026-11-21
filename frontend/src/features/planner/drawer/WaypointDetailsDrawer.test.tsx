import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { expedition } from '../../../api/models'
import { WaypointDetailsDrawer } from './WaypointDetailsDrawer'

function createOriginRowView() {
  return expedition.RowView.createFrom({
    row: { id: 1, expeditionId: 1, rowType: 'waypoint', systemName: 'Sol' },
    number: 1,
    visitState: 'visited',
  })
}

describe('WaypointDetailsDrawer', () => {
  it('renders WaypointDetailsDrawer with matching border-l-2, Vessel Flown, and 1-Click Mode toggle for leg waypoints (number > 1)', () => {
    const mockRowView = expedition.RowView.createFrom({
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

    const mockDetails = expedition.WaypointDetails.createFrom({
      kind: 'historical',
      number: 2,
      shipsUsed: ['Anaconda'],
      metrics: {
        distanceLy: 120.5,
        shipDistanceLy: 120.5,
        jumps: 3,
      } as any,
    })

    const mockModes = [
      { id: 10, name: 'Exploration', color: '#00D2FF' } as any,
      { id: 20, name: 'Travel', color: '#FF7100' } as any,
    ]

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: mockDetails,
        loading: false,
        rowView: mockRowView,
        modeId: 10,
        modeColor: '#00D2FF',
        modesList: mockModes,
        onChangeMode: () => {},
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

  it('colors origin drawers using muted neutral border for loading and loaded baseline views', () => {
    const originRowView = createOriginRowView()

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
        details: expedition.WaypointDetails.createFrom({ kind: 'origin', number: 1, metrics: {} }),
        loading: false,
        rowView: originRowView,
      }),
    )
    expect(originDrawerHtml).toContain('style="border-left-color:#3d2816"')
  })

  it('omits Vessel Flown and Mode toggle in WaypointDetailsDrawer for origin baseline waypoint #1', () => {
    const mockRowView = createOriginRowView()

    const mockDetails = expedition.WaypointDetails.createFrom({
      kind: 'origin',
      number: 1,
      metrics: {} as any,
    })

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: mockDetails,
        loading: false,
        rowView: mockRowView,
      }),
    )

    expect(html).toContain('HISTORICAL BASELINE')
    expect(html).not.toContain('[VESSEL FLOWN]:')
    expect(html).not.toContain('[FLIGHT MODE]:')
  })

  it('renders Realized Value card with combined total sold and 5 orange negative lines without emojis', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 2,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Colonia',
      },
      number: 2,
      visitState: 'visited',
    })

    const mockDetails = expedition.WaypointDetails.createFrom({
      kind: 'leg',
      number: 2,
      metrics: {
        scanValueTotal: 50000000,
        organicValueTotal: 95000000,
      } as any,
      realization: {
        totalRealized: 120000000,
        cartoLostDeath: 5000000,
        organicLostDeath: 10000000,
        cartoStolen: 2000000,
        organicStolen: 8000000,
        carrierTariffLoss: 3500000,
        npcCrewLoss: 1200000,
      } as any,
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
    const currentLegRowView = expedition.RowView.createFrom({
      row: {
        id: 22,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: '',
      },
      number: 3,
      visitState: 'current',
    })

    const currentLegDetails = expedition.WaypointDetails.createFrom({
      kind: 'leg',
      number: 3,
      prevSystem: 'Alpha Centauri',
      fromGalactic: '3312-01-19 12:00 UTC',
      toGalactic: '',
      elapsed: '4h 15m',
      metrics: {} as any,
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
    expect(html).toContain('[4h 15m]')
  })
})

