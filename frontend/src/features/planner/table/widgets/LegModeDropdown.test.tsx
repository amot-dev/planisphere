// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { modes } from '../../../../api/models'
import { LegModeDropdown } from './LegModeDropdown'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const SAMPLE_MODES = [
  modes.Mode.createFrom({ id: 1, name: 'Exploration', color: '#00B3FF', isDefault: true, sortOrder: 0 }),
  modes.Mode.createFrom({ id: 3, name: 'Travel', color: '#FF7100', isDefault: true, sortOrder: 2 }),
]

describe('LegModeDropdown', () => {
  it('supports single-leg selection and cascade-forward toggle when canCascade is true, resetting scope on reopen', () => {
    const onChangeMode = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      act(() => {
        root.render(
          createElement(LegModeDropdown, {
            modeId: 3,
            modeColor: '#FF7100',
            modesList: SAMPLE_MODES,
            canCascade: true,
            onChangeMode,
          }),
        )
      })

      const trigger = container.querySelector('button[title="Flight mode for this leg (click to change)"]') as HTMLButtonElement
      act(() => trigger.click())

      const onwardButton = Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent?.trim() === 'From Here Onward',
      ) as HTMLButtonElement
      expect(onwardButton).toBeTruthy()
      act(() => onwardButton.click())
      expect(container.textContent).toContain('Overwrites this leg and all following visited legs.')

      const explorationButton = Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent?.trim() === 'Exploration',
      ) as HTMLButtonElement
      act(() => explorationButton.click())
      expect(onChangeMode).toHaveBeenLastCalledWith(1, true)

      // Reopening resets scope to This Leg by default.
      act(() => trigger.click())
      expect(container.textContent).not.toContain('Overwrites this leg and all following visited legs.')
      const travelButton = Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent?.trim() === 'Travel',
      ) as HTMLButtonElement
      act(() => travelButton.click())
      expect(onChangeMode).toHaveBeenLastCalledWith(3, false)

      // Without canCascade, the cascade scope toggle is omitted.
      act(() => {
        root.render(
          createElement(LegModeDropdown, {
            modeId: 3,
            modeColor: '#FF7100',
            modesList: SAMPLE_MODES,
            canCascade: false,
            onChangeMode,
          }),
        )
      })
      act(() => trigger.click())
      expect(container.textContent).not.toContain('From Here Onward')
      act(() => trigger.click())

      // Header variant matches distance readout height/colors without brackets and places swatch after label.
      act(() => {
        root.render(
          createElement(LegModeDropdown, {
            variant: 'header',
            modeId: 3,
            modeColor: '#FF7100',
            modesList: SAMPLE_MODES,
            title: 'Flight mode for the current in-progress leg and upcoming visits',
            onChangeMode,
          }),
        )
      })
      const headerTrigger = container.querySelector('button[aria-label="Expedition flight mode"]') as HTMLButtonElement
      expect(headerTrigger).toBeTruthy()
      expect(headerTrigger.className).toContain('py-1.5')
      expect(headerTrigger.textContent).toContain('FLIGHT MODE:')
      expect(headerTrigger.textContent).not.toContain('[FLIGHT MODE]:')
      act(() => headerTrigger.click())
      const headerExplorationOption = Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent?.trim() === 'Exploration',
      ) as HTMLButtonElement
      act(() => headerExplorationOption.click())
      expect(onChangeMode).toHaveBeenLastCalledWith(1, false)
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })
})
