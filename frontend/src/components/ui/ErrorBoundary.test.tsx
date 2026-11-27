// @vitest-environment happy-dom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { mountTestRoot } from '../../test/render'
import { ErrorBoundary } from './ErrorBoundary'

describe('ErrorBoundary', () => {
  it('renders children normally when no error is thrown', () => {
    const mounted = mountTestRoot(
      <ErrorBoundary fallbackTitle="Test Error">
        <div id="child-content">Operational Child Content</div>
      </ErrorBoundary>,
    )

    expect(mounted.container.querySelector('#child-content')?.textContent).toBe(
      'Operational Child Content',
    )
    expect(mounted.container.textContent).not.toContain('SUBSYSTEM EXCEPTION')
  })

  it('catches child render errors, renders fallback HUD UI with console logging, and recovers on DISMISS AND RETRY', () => {
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    let shouldThrow = true

    function ThrowingChild() {
      if (shouldThrow) {
        throw new Error('Critical telemetry failure')
      }
      return <div id="recovered-child">Operational Child Content</div>
    }

    try {
      const mounted = mountTestRoot(
        <ErrorBoundary fallbackTitle="Subsystem Sensor Malfunction">
          <ThrowingChild />
        </ErrorBoundary>,
      )

      const fallbackText = mounted.container.textContent ?? ''
      expect(fallbackText).toContain('SUBSYSTEM EXCEPTION')
      expect(fallbackText).toContain('Subsystem Sensor Malfunction')
      expect(fallbackText).toContain('Critical telemetry failure')
      expect(mounted.findButtonByText('REBOOT PLANISPHERE')).toBeTruthy()

      const loggedCall = consoleErrorSpy.mock.calls.find(
        (callArgs) =>
          typeof callArgs[0] === 'string' &&
          callArgs[0].includes('[TerminalErrorBoundary caught error]'),
      )
      expect(loggedCall).toBeDefined()
      expect((loggedCall?.[1] as Error).message).toBe('Critical telemetry failure')
      expect(typeof (loggedCall?.[2] as { componentStack?: string })?.componentStack).toBe('string')

      shouldThrow = false
      act(() => {
        mounted.findButtonByText('DISMISS AND RETRY').click()
      })

      expect(mounted.container.querySelector('#recovered-child')?.textContent).toBe(
        'Operational Child Content',
      )
      expect(mounted.container.textContent).not.toContain('SUBSYSTEM EXCEPTION')
    } finally {
      consoleErrorSpy.mockRestore()
    }
  })
})
