// @vitest-environment happy-dom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { renderHook } from '../test/render'
import { useCopyFeedback } from './useCopyFeedback'

describe('useCopyFeedback', () => {
  it('sets copiedKey, resets after delay, extends timeout on rapid repeat copy, falls back to execCommand, and clears timer on unmount', () => {
    vi.useFakeTimers()

    const writeTextMock = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: writeTextMock },
    })

    const mountedHook = renderHook(() => useCopyFeedback<string>(1200))

    // Empty text is ignored.
    act(() => {
      mountedHook.getResult().copyWithFeedback('row-empty', '')
    })
    expect(mountedHook.getResult().copiedKey).toBeNull()
    expect(writeTextMock).not.toHaveBeenCalled()

    // Copying sets copiedKey and resets after 1200ms.
    act(() => {
      mountedHook.getResult().copyWithFeedback('row-1', 'Sol')
    })
    expect(writeTextMock).toHaveBeenCalledWith('Sol')
    expect(mountedHook.getResult().copiedKey).toBe('row-1')

    // Rapid repeat copy at 800ms replaces the timer so copiedKey stays active past the original 1200ms deadline.
    act(() => {
      vi.advanceTimersByTime(800)
    })
    expect(mountedHook.getResult().copiedKey).toBe('row-1')

    act(() => {
      mountedHook.getResult().copyWithFeedback('row-2', 'Alpha Centauri')
    })
    expect(mountedHook.getResult().copiedKey).toBe('row-2')

    act(() => {
      vi.advanceTimersByTime(800)
    })
    expect(mountedHook.getResult().copiedKey).toBe('row-2')

    act(() => {
      vi.advanceTimersByTime(400)
    })
    expect(mountedHook.getResult().copiedKey).toBeNull()

    // Fallback textarea copy executes when navigator.clipboard is unavailable, and unmount clears pending timer.
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: undefined,
    })
    const execCommandMock = vi.fn(() => true)
    Object.defineProperty(document, 'execCommand', {
      configurable: true,
      value: execCommandMock,
    })
    const clearTimeoutSpy = vi.spyOn(globalThis, 'clearTimeout')

    act(() => {
      mountedHook.getResult().copyWithFeedback('row-3', 'Colonia')
    })
    expect(execCommandMock).toHaveBeenCalledWith('copy')
    expect(document.body.querySelector('textarea')).toBeNull()
    expect(mountedHook.getResult().copiedKey).toBe('row-3')

    mountedHook.unmount()
    expect(clearTimeoutSpy).toHaveBeenCalled()
    act(() => {
      vi.advanceTimersByTime(2000)
    })
  })
})
