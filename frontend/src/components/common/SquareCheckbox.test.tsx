import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { SquareCheckbox } from './SquareCheckbox'

describe('SquareCheckbox', () => {
  it('renders unchecked outline without inner rect', () => {
    const html = renderToString(createElement(SquareCheckbox, { checked: false }))
    expect(html).toContain('stroke="#3d2816"')
    expect(html).not.toContain('width="8"')
  })

  it('renders checked state with filled square', () => {
    const html = renderToString(createElement(SquareCheckbox, { checked: true }))
    expect(html).toContain('stroke="#FF7100"')
    expect(html).toContain('width="8" height="8"')
    expect(html).toContain('fill="#FF7100"')
  })

  it('renders partial state with horizontally-aligned rectangle', () => {
    const html = renderToString(createElement(SquareCheckbox, { checked: 'partial' }))
    expect(html).toContain('stroke="#00D2FF"')
    expect(html).toContain('width="8" height="3"')
    expect(html).toContain('fill="#00D2FF"')
  })

  it('supports custom colors and transparent bgFill', () => {
    const html = renderToString(
      createElement(SquareCheckbox, {
        checked: true,
        color: '#ff0000',
        bgFill: 'transparent',
      }),
    )
    expect(html).toContain('stroke="#ff0000"')
    expect(html).toContain('fill="transparent"')
    expect(html).toContain('fill="#ff0000"')
  })

  it('renders accessible overlay checkbox input for keyboard and click dispatch', () => {
    const html = renderToString(
      createElement(SquareCheckbox, {
        checked: false,
        onChange: () => {},
        'aria-label': 'Track Commander',
      }),
    )
    expect(html).toContain('type="checkbox"')
    expect(html).toContain('absolute inset-0')
    expect(html).toContain('aria-label="Track Commander"')
  })
})

