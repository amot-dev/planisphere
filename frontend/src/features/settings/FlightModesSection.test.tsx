// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { app, modes } from '../../api/models'

const mockUpdateMode = vi.fn()
const mockGetModeUsage = vi.fn()
const mockDeleteMode = vi.fn()

vi.mock('../../api/backend', () => ({
  CreateMode: vi.fn(),
  UpdateMode: (...args: unknown[]) => mockUpdateMode(...args),
  GetModeUsage: (...args: unknown[]) => mockGetModeUsage(...args),
  DeleteMode: (...args: unknown[]) => mockDeleteMode(...args),
}))

import { FlightModesSection } from './FlightModesSection'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

describe('FlightModesSection delete confirmation', () => {
  it('loads expedition usage breakdown before confirming custom mode deletion', async () => {
    mockGetModeUsage.mockResolvedValue([
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
    ])
    mockDeleteMode.mockResolvedValue(undefined)

    const modesState = app.ModesState.createFrom({
      modes: [
        { id: 1, name: 'Travel', description: '', color: '#FF7100', isDefault: true, sortOrder: 0 },
        { id: 9, name: 'Buckyball', description: 'Sprint', color: '#00E5A3', isDefault: false, sortOrder: 3 },
      ],
    })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      act(() => {
        root.render(
          createElement(FlightModesSection, {
            modesState,
            onSetError: () => {},
          }),
        )
      })

      const deleteButton = Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent?.trim() === 'DELETE',
      ) as HTMLButtonElement
      expect(deleteButton).toBeTruthy()

      await act(async () => {
        deleteButton.click()
        await Promise.resolve()
      })

      expect(mockGetModeUsage).toHaveBeenCalledWith(9)
      expect(mockDeleteMode).not.toHaveBeenCalled()
      expect(container.textContent).toContain('Delete Flight Mode')
      expect(container.textContent).toContain('Colonia Express')
      expect(container.textContent).toContain('4 legs')
      expect(container.textContent).toContain('ACTIVE MODE')
      expect(container.textContent).toContain('Beagle Survey')
      expect(container.textContent).toContain('1 leg')
      expect(container.textContent).toContain('reassign all affected legs and expedition active modes back to Travel')

      const confirmButton = Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent?.trim() === 'DELETE FLIGHT MODE',
      ) as HTMLButtonElement
      expect(confirmButton).toBeTruthy()

      await act(async () => {
        confirmButton.click()
        await Promise.resolve()
      })

      expect(mockDeleteMode).toHaveBeenCalledWith(9)
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })

  it('flushes pending debounced color updates on unmount without invoking onSetError after unmount', async () => {
    let rejectColorUpdate!: (reason: Error) => void
    mockUpdateMode.mockImplementationOnce(
      () =>
        new Promise<void>((_, reject) => {
          rejectColorUpdate = reject
        }),
    )

    const onSetError = vi.fn()
    const modesState = app.ModesState.createFrom({
      modes: [
        { id: 1, name: 'Travel', description: 'Fast transit', color: '#FF7100', isDefault: true, sortOrder: 0 },
      ],
    })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    act(() => {
      root.render(createElement(FlightModesSection, { modesState, onSetError }))
    })

    const colorInput = container.querySelector('input[type="color"]') as HTMLInputElement
    act(() => {
      const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')
      descriptor?.set?.call(colorInput, '#00e5a3')
      colorInput.dispatchEvent(new Event('input', { bubbles: true }))
    })

    expect(mockUpdateMode).not.toHaveBeenCalled()

    act(() => {
      root.unmount()
    })
    container.remove()

    expect(mockUpdateMode).toHaveBeenCalledWith(1, 'Travel', 'Fast transit', '#00e5a3')
    expect(onSetError).not.toHaveBeenCalled()

    await act(async () => {
      rejectColorUpdate(new Error('database closed'))
      await Promise.resolve()
    })

    expect(onSetError).not.toHaveBeenCalled()
  })
})
