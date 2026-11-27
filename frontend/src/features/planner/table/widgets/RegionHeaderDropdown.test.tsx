// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app } from '../../../../api/models'
import { mountTestRoot } from '../../../../test/render'
import { installWailsAppMock } from '../../../../test/wailsMock'
import { RegionHeaderDropdown } from './RegionHeaderDropdown'

describe('RegionHeaderDropdown', () => {
  it('renders static label when locked and opens grouped region menu supporting selection and outside backdrop dismissal', async () => {
    const onSelect = vi.fn()
    const { spies } = installWailsAppMock({
      GetRegionOptions: async () =>
        app.RegionOptions.createFrom({
          anchorName: 'Inner Orion Spur',
          suggestedRegionId: 18,
          options: [
            { id: 18, name: 'Inner Orion Spur', anchor: true, adjacent: false },
            { id: 19, name: 'Hawking Gap', anchor: false, adjacent: true },
            { id: 35, name: 'Sanguineous Rim', anchor: false, adjacent: true },
            { id: 42, name: 'Beagle Point Sector', anchor: false, adjacent: false },
          ],
        }),
    })

    // Locked header renders static text without interactive button.
    const lockedView = mountTestRoot(
      createElement(RegionHeaderDropdown, {
        expeditionId: 4,
        rowId: 50,
        currentRegionId: 18,
        regionName: 'Inner Orion Spur',
        locked: true,
        onSelect,
      }),
    )
    expect(lockedView.container.textContent).toContain('Inner Orion Spur')
    expect(lockedView.container.querySelector('button')).toBeNull()
    lockedView.unmount()

    // Interactive header loads options via GetRegionOptions when opened.
    const mounted = mountTestRoot(
      createElement(RegionHeaderDropdown, {
        expeditionId: 4,
        rowId: 50,
        currentRegionId: 19,
        regionName: '',
        onSelect,
      }),
    )

    const triggerButton = mounted.container.querySelector(
      'button[title="Click to switch Region"]',
    ) as HTMLButtonElement
    expect(triggerButton.textContent).toContain('SELECT REGION')

    await act(async () => {
      triggerButton.click()
      await Promise.resolve()
    })
    expect(spies.GetRegionOptions).toHaveBeenCalledWith(4, 50, true)

    const menuText = mounted.container.textContent ?? ''
    expect(menuText).toContain('Suggested from next waypoint: Inner Orion Spur')
    expect(menuText).toContain('Adjacent to Inner Orion Spur (2)')
    expect(menuText).toContain('Anchor Region')
    expect(menuText).toContain('All Other Regions (1)')

    // Selecting an adjacent region invokes onSelect and closes the dropdown.
    act(() => {
      mounted.findButtonByText('Sanguineous Rim').click()
    })
    expect(onSelect).toHaveBeenCalledWith(35)
    expect(mounted.container.textContent).not.toContain('All Other Regions')

    // Reopening and clicking the Suggested region button selects suggestedRegionId.
    await act(async () => {
      triggerButton.click()
      await Promise.resolve()
    })
    act(() => {
      mounted.findButtonByText('Suggested from next waypoint: Inner Orion Spur').click()
    })
    expect(onSelect).toHaveBeenLastCalledWith(18)

    // Reopening and clicking the outside backdrop closes the menu without invoking onSelect.
    onSelect.mockClear()
    await act(async () => {
      triggerButton.click()
      await Promise.resolve()
    })
    const backdrop = mounted.container.querySelector('.fixed.inset-0.z-30') as HTMLDivElement
    expect(backdrop).not.toBeNull()
    act(() => {
      backdrop.click()
    })
    expect(onSelect).not.toHaveBeenCalled()
    expect(mounted.container.querySelector('.fixed.inset-0.z-30')).toBeNull()
  })
})
