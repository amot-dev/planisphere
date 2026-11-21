import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { expedition } from '../../../../api/models'
import { HoverOpenInLinks } from './HoverOpenInLinks'

describe('HoverOpenInLinks', () => {
  it('renders external links without arrow icons and obeys Inara, EDSM (including If No GEC), and GEC settings', () => {
    const rowWithGec = expedition.RowView.createFrom({
      row: {
        id: 70,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Sagittarius A*',
      },
      number: 2,
      visitState: 'future',
      gec: {
        id: 1,
        name: 'Sagittarius A*',
        type: 'Black Hole',
        url: 'https://edastro.com/gec/view/1',
        count: 1,
      },
    })
    const rowWithoutGec = expedition.RowView.createFrom({
      row: {
        id: 71,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Beagle Point',
      },
      number: 3,
      visitState: 'future',
    })

    // Default (Inara: on, EDSM: if_no_gec, GEC: on) hides EDSM when GEC is shown and omits arrow icons
    const defaultWithGecHtml = renderToString(
      createElement(HoverOpenInLinks, { rowView: rowWithGec }),
    )
    expect(defaultWithGecHtml).not.toContain('\u2197')
    expect(defaultWithGecHtml).toContain('<span>INARA</span>')
    expect(defaultWithGecHtml).not.toContain('<span>EDSM</span>')
    expect(defaultWithGecHtml).toContain('<span>GEC</span>')

    // Default on a row without GEC renders INARA and EDSM
    const defaultWithoutGecHtml = renderToString(
      createElement(HoverOpenInLinks, { rowView: rowWithoutGec }),
    )
    expect(defaultWithoutGecHtml).toContain('<span>INARA</span>')
    expect(defaultWithoutGecHtml).toContain('<span>EDSM</span>')
    expect(defaultWithoutGecHtml).not.toContain('<span>GEC</span>')

    // If No GEC acts as On when GEC is Off (even when rowView.gec.url exists)
    const gecOffWithEntryHtml = renderToString(
      createElement(HoverOpenInLinks, {
        rowView: rowWithGec,
        externalLinkInara: 'off',
        externalLinkEdsm: 'if_no_gec',
        externalLinkGec: 'off',
      }),
    )
    expect(gecOffWithEntryHtml).not.toContain('<span>INARA</span>')
    expect(gecOffWithEntryHtml).toContain('<span>EDSM</span>')
    expect(gecOffWithEntryHtml).not.toContain('<span>GEC</span>')

    // EDSM: on shows all three simultaneously on a GEC row; all off renders none
    const allOnHtml = renderToString(
      createElement(HoverOpenInLinks, {
        rowView: rowWithGec,
        externalLinkInara: 'on',
        externalLinkEdsm: 'on',
        externalLinkGec: 'on',
      }),
    )
    expect(allOnHtml).toContain('<span>INARA</span>')
    expect(allOnHtml).toContain('<span>EDSM</span>')
    expect(allOnHtml).toContain('<span>GEC</span>')

    const allOffHtml = renderToString(
      createElement(HoverOpenInLinks, {
        rowView: rowWithGec,
        externalLinkInara: 'off',
        externalLinkEdsm: 'off',
        externalLinkGec: 'off',
      }),
    )
    expect(allOffHtml).not.toContain('<span>INARA</span>')
    expect(allOffHtml).not.toContain('<span>EDSM</span>')
    expect(allOffHtml).not.toContain('<span>GEC</span>')
  })
})

