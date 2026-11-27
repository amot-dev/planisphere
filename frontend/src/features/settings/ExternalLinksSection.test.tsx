import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { app } from '../../api/models'
import { ExternalLinksSection } from './ExternalLinksSection'

describe('ExternalLinksSection', () => {
  const extractGroupHtml = (markup: string, serviceLabel: string) => {
    const groupMatch = markup.match(
      new RegExp(`aria-label="${serviceLabel} external link visibility"[\\s\\S]*?<\\/div>`),
    )
    return groupMatch ? groupMatch[0] : ''
  }

  it('renders External Links rows with default Inara On, EDSM If No GEC, and GEC On selected', () => {
    const defaultHtml = renderToString(
      createElement(ExternalLinksSection, {
        settings: null,
        onSetError: () => {},
      }),
    )

    expect(defaultHtml).toContain('External Links')
    expect(defaultHtml.match(/aria-pressed="true"/g)).toHaveLength(3)
    expect(extractGroupHtml(defaultHtml, 'Inara')).toMatch(/aria-pressed="true"[^>]*>On<\/button>/)
    expect(extractGroupHtml(defaultHtml, 'EDSM')).toMatch(
      /aria-pressed="true"[^>]*>If No GEC<\/button>/,
    )
    expect(extractGroupHtml(defaultHtml, 'GEC')).toMatch(/aria-pressed="true"[^>]*>On<\/button>/)
  })

  it('marks configured Off and On selections via aria-pressed across all three rows', () => {
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
    expect(extractGroupHtml(customHtml, 'Inara')).toMatch(/aria-pressed="true"[^>]*>Off<\/button>/)
    expect(extractGroupHtml(customHtml, 'EDSM')).toMatch(/aria-pressed="true"[^>]*>Off<\/button>/)
    expect(extractGroupHtml(customHtml, 'GEC')).toMatch(/aria-pressed="true"[^>]*>On<\/button>/)
  })
})

