import { describe, expect, it, vi } from 'vitest'
import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { ErrorBoundary } from './ErrorBoundary'

describe('ErrorBoundary', () => {
  it('renders children when no error is thrown', () => {
    const html = renderToString(
      createElement(
        ErrorBoundary,
        { fallbackTitle: 'Test Error' },
        createElement('div', { id: 'child-content' }, 'Operational Child Content'),
      ),
    )

    expect(html).toContain('Operational Child Content')
    expect(html).toContain('id="child-content"')
    expect(html).not.toContain('SUBSYSTEM EXCEPTION')
  })

  it('updates state via getDerivedStateFromError', () => {
    const error = new Error('Simulated reactor breach')
    const state = ErrorBoundary.getDerivedStateFromError(error)

    expect(state.hasError).toBe(true)
    expect(state.error).toBe(error)
    expect(state.errorInfo).toBeNull()
  })

  it('renders fallback error HUD UI when in error state', () => {
    const boundary = new ErrorBoundary({
      children: createElement('div', null, 'child'),
      fallbackTitle: 'Subsystem Sensor Malfunction',
    })

    const testError = new Error('Critical telemetry failure')
    boundary.state = {
      hasError: true,
      error: testError,
      errorInfo: { componentStack: ' at WaypointTable\n at App' } as any,
    }

    const rendered = boundary.render()
    const html = renderToString(rendered as any)

    expect(html).toContain('SUBSYSTEM EXCEPTION')
    expect(html).toContain('Subsystem Sensor Malfunction')
    expect(html).toContain('Critical telemetry failure')
    expect(html).toContain('Reboot Terminal')
    expect(html).toContain('Dismiss and Retry')
  })

  it('logs caught errors to console via componentDidCatch', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const boundary = new ErrorBoundary({
      children: createElement('div', null, 'child'),
    })

    const testError = new Error('Test log error')
    const testInfo = { componentStack: 'stack trace' } as any

    boundary.componentDidCatch(testError, testInfo)

    expect(spy).toHaveBeenCalledWith(
      expect.stringContaining('[TerminalErrorBoundary caught error]'),
      testError,
      testInfo,
    )

    spy.mockRestore()
  })
})
