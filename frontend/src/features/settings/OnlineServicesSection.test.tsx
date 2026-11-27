// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { OnlineServicesSection } from './OnlineServicesSection'

describe('OnlineServicesSection', () => {
  it('toggles SetOnlineLookups with rollback on failure and refreshes the GEC catalog via RefreshGEC', async () => {
    const onSetError = vi.fn()
    let resolveGecRefresh!: (entryCount: number) => void

    const { spies } = installWailsAppMock({
      SetOnlineLookups: async (enabled: boolean) => makeSettings({ onlineLookups: enabled }),
      RefreshGEC: () =>
        new Promise<number>((resolve) => {
          resolveGecRefresh = resolve
        }),
    })

    const mounted = mountTestRoot(
      createElement(OnlineServicesSection, {
        settings: makeSettings({ onlineLookups: true, gecCount: 1420 }),
        onSetError,
      }),
    )

    expect(mounted.container.textContent).toContain('1,420')
    const onlineCheckbox = mounted.container.querySelector(
      'input[type="checkbox"]',
    ) as HTMLInputElement
    expect(onlineCheckbox.checked).toBe(true)

    // Toggling off invokes SetOnlineLookups(false) and updates checkbox state.
    await act(async () => {
      onlineCheckbox.click()
      await Promise.resolve()
    })
    expect(spies.SetOnlineLookups).toHaveBeenCalledWith(false)
    expect(onlineCheckbox.checked).toBe(false)

    // Rejected toggle rolls back checkbox to settings.onlineLookups and reports error.
    mounted.rerender(
      createElement(OnlineServicesSection, {
        settings: makeSettings({ onlineLookups: false, gecCount: 1420 }),
        onSetError,
      }),
    )
    spies.SetOnlineLookups.mockRejectedValueOnce(new Error('lookup setting locked'))
    await act(async () => {
      onlineCheckbox.click()
      await Promise.resolve()
    })
    expect(spies.SetOnlineLookups).toHaveBeenLastCalledWith(true)
    expect(onlineCheckbox.checked).toBe(false)
    expect(onSetError).toHaveBeenLastCalledWith('Error: lookup setting locked')

    // Manual catalog refresh disables button with SYNCING... until RefreshGEC settles.
    act(() => {
      mounted.findButtonByText('SYNC CATALOG NOW').click()
    })
    expect(mounted.findButtonByText('SYNCING...').disabled).toBe(true)

    await act(async () => {
      resolveGecRefresh(1580)
      await Promise.resolve()
    })
    expect(spies.RefreshGEC).toHaveBeenCalledTimes(1)
    expect(mounted.findButtonByText('SYNC CATALOG NOW').disabled).toBe(false)

    // Rejected RefreshGEC restores button and surfaces error.
    spies.RefreshGEC.mockRejectedValueOnce(new Error('catalog network timeout'))
    await act(async () => {
      mounted.findButtonByText('SYNC CATALOG NOW').click()
      await Promise.resolve()
    })
    expect(onSetError).toHaveBeenLastCalledWith('Error: catalog network timeout')
    expect(mounted.findButtonByText('SYNC CATALOG NOW').disabled).toBe(false)
  })
})
