// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app, modes } from '../../api/models'
import { mountTestRoot, setFormControlValue } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { FlightModesSection } from './FlightModesSection'

describe('FlightModesSection delete confirmation', () => {
  it('loads expedition usage breakdown before confirming custom mode deletion', async () => {
    const { spies } = installWailsAppMock({
      GetModeUsage: async () => [
        modes.ExpeditionUsage.createFrom({
          expeditionId: 11,
          expeditionName: 'Colonia Express',
          status: 'active',
          legCount: 4,
          isActiveMode: true,
        }),
        modes.ExpeditionUsage.createFrom({
          expeditionId: 12,
          expeditionName: 'Beagle Survey',
          status: 'completed',
          legCount: 1,
          isActiveMode: false,
        }),
      ],
      DeleteMode: async () => {},
    })

    const modesState = app.ModesState.createFrom({
      modes: [
        { id: 1, name: 'Travel', description: '', color: '#FF7100', isDefault: true, sortOrder: 0 },
        { id: 9, name: 'Buckyball', description: 'Sprint', color: '#00E5A3', isDefault: false, sortOrder: 3 },
      ],
    })

    const mounted = mountTestRoot(
      createElement(FlightModesSection, {
        modesState,
        onSetError: () => {},
      }),
    )

    const deleteButton = mounted.findButtonByText('DELETE', { exact: true })
    await act(async () => {
      deleteButton.click()
      await Promise.resolve()
    })

    expect(spies.GetModeUsage).toHaveBeenCalledWith(9)
    expect(spies.DeleteMode).not.toHaveBeenCalled()
    expect(mounted.container.textContent).toContain('Delete Flight Mode')
    expect(mounted.container.textContent).toContain('Colonia Express')
    expect(mounted.container.textContent).toContain('4 legs')
    expect(mounted.container.textContent).toContain('ACTIVE MODE')
    expect(mounted.container.textContent).toContain('Beagle Survey')
    expect(mounted.container.textContent).toContain('1 leg')
    expect(mounted.container.textContent).toContain(
      'reassign all affected legs and expedition active modes back to Travel',
    )

    const confirmButton = mounted.findButtonByText('DELETE FLIGHT MODE', { exact: true })
    await act(async () => {
      confirmButton.click()
      await Promise.resolve()
    })

    expect(spies.DeleteMode).toHaveBeenCalledWith(9)
  })

  it('flushes pending debounced color updates on unmount without invoking onSetError after unmount', async () => {
    let rejectColorUpdate!: (reason: Error) => void
    const { spies } = installWailsAppMock({
      UpdateMode: () =>
        new Promise<void>((_, reject) => {
          rejectColorUpdate = reject
        }),
    })

    const onSetError = vi.fn()
    const modesState = app.ModesState.createFrom({
      modes: [
        { id: 1, name: 'Travel', description: 'Fast transit', color: '#FF7100', isDefault: true, sortOrder: 0 },
      ],
    })

    const mounted = mountTestRoot(
      createElement(FlightModesSection, { modesState, onSetError }),
    )

    const colorInput = mounted.container.querySelector('input[type="color"]') as HTMLInputElement
    act(() => {
      setFormControlValue(colorInput, '#00e5a3')
    })

    expect(spies.UpdateMode).not.toHaveBeenCalled()

    mounted.unmount()

    expect(spies.UpdateMode).toHaveBeenCalledWith(1, 'Travel', 'Fast transit', '#00e5a3')
    expect(onSetError).not.toHaveBeenCalled()

    await act(async () => {
      rejectColorUpdate(new Error('database closed'))
      await Promise.resolve()
    })

    expect(onSetError).not.toHaveBeenCalled()
  })
})
