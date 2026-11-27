// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { makeMode } from '../../../../test/fixtures'
import { mountTestRoot } from '../../../../test/render'
import { LegModeDropdown } from './LegModeDropdown'

const SAMPLE_MODES = [
  makeMode({ id: 1, name: 'Exploration', color: '#00B3FF', isDefault: true, sortOrder: 0 }),
  makeMode({ id: 3, name: 'Travel', color: '#FF7100', isDefault: true, sortOrder: 2 }),
]

describe('LegModeDropdown', () => {
  it('supports single-leg selection and cascade-forward toggle when canCascade is true, resetting scope on reopen', () => {
    const onChangeMode = vi.fn()
    const mounted = mountTestRoot(
      createElement(LegModeDropdown, {
        modeId: 3,
        modeColor: '#FF7100',
        modesList: SAMPLE_MODES,
        canCascade: true,
        onChangeMode,
      }),
    )

    const trigger = mounted.container.querySelector(
      'button[title="Flight mode for this leg (click to change)"]',
    ) as HTMLButtonElement
    act(() => trigger.click())

    const onwardButton = mounted.findButtonByText('From Here Onward', { exact: true })
    act(() => onwardButton.click())
    expect(mounted.container.textContent).toContain('Overwrites this leg and all following visited legs.')

    act(() => mounted.findButtonByText('Exploration', { exact: true }).click())
    expect(onChangeMode).toHaveBeenLastCalledWith(1, true)

    // Reopening resets scope to This Leg by default.
    act(() => trigger.click())
    expect(mounted.container.textContent).not.toContain('Overwrites this leg and all following visited legs.')
    act(() => mounted.findButtonByText('Travel', { exact: true }).click())
    expect(onChangeMode).toHaveBeenLastCalledWith(3, false)

    // Without canCascade, the cascade scope toggle is omitted.
    mounted.rerender(
      createElement(LegModeDropdown, {
        modeId: 3,
        modeColor: '#FF7100',
        modesList: SAMPLE_MODES,
        canCascade: false,
        onChangeMode,
      }),
    )
    act(() => trigger.click())
    expect(mounted.container.textContent).not.toContain('From Here Onward')
    act(() => trigger.click())

    // Header variant matches distance readout height/colors without brackets and places swatch after label.
    mounted.rerender(
      createElement(LegModeDropdown, {
        variant: 'header',
        modeId: 3,
        modeColor: '#FF7100',
        modesList: SAMPLE_MODES,
        title: 'Flight mode for the current in-progress leg and upcoming visits',
        onChangeMode,
      }),
    )
    const headerTrigger = mounted.container.querySelector(
      'button[aria-label="Expedition flight mode"]',
    ) as HTMLButtonElement
    expect(headerTrigger).toBeTruthy()
    expect(headerTrigger.className).toContain('py-1.5')
    expect(headerTrigger.textContent).toContain('FLIGHT MODE:')
    expect(headerTrigger.textContent).not.toContain('[FLIGHT MODE]:')
    act(() => headerTrigger.click())
    act(() => mounted.findButtonByText('Exploration', { exact: true }).click())
    expect(onChangeMode).toHaveBeenLastCalledWith(1, false)
  })
})
