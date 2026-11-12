import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import {
  DEFAULT_JOURNAL_DEBOUNCE_MILLISECONDS,
  JournalDebounceControl,
  MAX_JOURNAL_DEBOUNCE_MILLISECONDS,
  MIN_JOURNAL_DEBOUNCE_MILLISECONDS,
  clampDebounceMilliseconds,
  formatDebounceSeconds,
} from './JournalDebounceControl'

describe('JournalDebounceControl', () => {
  it('clamps and formats debounce durations within configured bounds', () => {
    expect(clampDebounceMilliseconds(50)).toBe(MIN_JOURNAL_DEBOUNCE_MILLISECONDS)
    expect(clampDebounceMilliseconds(95000)).toBe(MAX_JOURNAL_DEBOUNCE_MILLISECONDS)
    expect(clampDebounceMilliseconds(Number.NaN)).toBe(DEFAULT_JOURNAL_DEBOUNCE_MILLISECONDS)
    expect(clampDebounceMilliseconds(2500.4)).toBe(2500)
    expect(formatDebounceSeconds(2000)).toBe('2.0s')
    expect(formatDebounceSeconds(300)).toBe('0.3s')
  })

  it('renders range bounds, active preset state, and disabled reset at default value', () => {
    const defaultHtml = renderToString(
      createElement(JournalDebounceControl, {
        configuredDebounceMilliseconds: 2000,
        defaultDebounceMilliseconds: 2000,
        onCommitDebounceMilliseconds: () => {},
      }),
    )
    expect(defaultHtml).toContain('min="300"')
    expect(defaultHtml).toContain('max="30000"')
    expect(defaultHtml).toContain('2.0s')
    expect(defaultHtml).toContain('disabled=""')

    const customHtml = renderToString(
      createElement(JournalDebounceControl, {
        configuredDebounceMilliseconds: 15000,
        defaultDebounceMilliseconds: 2000,
        onCommitDebounceMilliseconds: () => {},
      }),
    )
    expect(customHtml).toContain('15.0s')
    expect(customHtml).not.toContain('disabled=""')
  })
})

