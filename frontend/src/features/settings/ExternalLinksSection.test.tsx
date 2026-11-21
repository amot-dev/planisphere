import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { app } from '../../api/models'
import { ExternalLinksSection } from './ExternalLinksSection'

describe('ExternalLinksSection', () => {
  it('renders External Links rows with default Inara On, EDSM If No GEC, GEC On highlighted in amber', () => {
    const defaultHtml = renderToString(
      createElement(ExternalLinksSection, {
        settings: null,
        onSetError: () => {},
      }),
    )

    expect(defaultHtml).toContain('External Links')
    expect(defaultHtml).toContain('Inara')
    expect(defaultHtml).toContain('EDSM')
    expect(defaultHtml).toContain('GEC')
    expect(defaultHtml).toContain('If No GEC')
    expect(defaultHtml.match(/aria-pressed="true"/g)).toHaveLength(3)
    expect(defaultHtml.match(/bg-ed-amber-tint text-ed-amber border-ed-amber/g)).toHaveLength(3)
  })

  it('highlights configured Off and On selections in amber across all three rows', () => {
    const customSettings = app.Settings.createFrom({
      externalLinkInara: 'off',
      externalLinkEdsm: 'off',
      externalLinkGec: 'on',
    })
    const customHtml = renderToString(
      createElement(ExternalLinksSection, {
        settings: customSettings,
        onSetError: () => {},
      }),
    )

    expect(customHtml.match(/aria-pressed="true"/g)).toHaveLength(3)
    expect(customHtml.match(/bg-ed-amber-tint text-ed-amber border-ed-amber/g)).toHaveLength(3)
  })
})

