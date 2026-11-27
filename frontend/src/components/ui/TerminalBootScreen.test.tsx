import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { APP_PRODUCT_NAME, APP_VERSION } from '../../version'
import { TerminalBootScreen } from './TerminalBootScreen'

describe('TerminalBootScreen', () => {
  it('renders product header, version badge, initializing message, and synchronizing status via SSR', () => {
    const html = renderToString(createElement(TerminalBootScreen))

    expect(html).toContain(APP_PRODUCT_NAME)
    expect(html).toContain('PLANISPHERE TERMINAL v')
    expect(html).toContain(APP_VERSION)
    expect(html).toContain('Initializing Planisphere Terminal...')
    expect(html).toContain('Mounting journal archive and stellar coordinate index')
    expect(html).toContain('STATUS: SYNCHRONIZING')
    expect(html).toContain('UTC')
  })
})
