// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { mountTestRoot, setFormControlValue } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { CarrierConfigModal } from './CarrierConfigModal'

describe('CarrierConfigModal', () => {
  it('switches vessel classification, clamps and rounds weekly upkeep, guards invalid expeditionId, and submits UpdateExpeditionCarrierConfig', async () => {
    const onClose = vi.fn()
    let resolveSave!: () => void

    const { spies } = installWailsAppMock({
      UpdateExpeditionCarrierConfig: () =>
        new Promise<void>((resolve) => {
          resolveSave = resolve
        }),
    })

    // Zero expeditionId is guarded without calling backend RPC.
    const guardedView = mountTestRoot(
      createElement(CarrierConfigModal, {
        expeditionId: 0,
        initialCarrierType: 'player',
        initialWeeklyUpkeep: -500,
        onClose,
      }),
    )
    const guardedInput = guardedView.container.querySelector(
      '#expedition-carrier-weekly-upkeep',
    ) as HTMLInputElement
    expect(guardedInput.value).toBe('0')

    await act(async () => {
      guardedView.findButtonByText('SAVE CONFIGURATION').click()
      await Promise.resolve()
    })
    expect(spies.UpdateExpeditionCarrierConfig).not.toHaveBeenCalled()
    act(() => {
      guardedView.findButtonByText('CANCEL').click()
    })
    expect(onClose).toHaveBeenCalledTimes(1)
    guardedView.unmount()

    // Active expedition supports carrier mode switching, upkeep rounding/clamping, busy state, error display, and close on save.
    onClose.mockClear()
    const mounted = mountTestRoot(
      createElement(CarrierConfigModal, {
        expeditionId: 12,
        initialCarrierType: 'player',
        initialWeeklyUpkeep: 9500000,
        onClose,
      }),
    )

    const upkeepInput = mounted.container.querySelector(
      '#expedition-carrier-weekly-upkeep',
    ) as HTMLInputElement
    expect(upkeepInput.value).toBe('9500000')

    // Switch to Squadron Carrier, then back to Personal Carrier, then Squadron Carrier with decimal upkeep.
    act(() => {
      mounted.findButtonByText('Squadron Carrier').click()
      mounted.findButtonByText('Personal Carrier').click()
      mounted.findButtonByText('Squadron Carrier').click()
      setFormControlValue(upkeepInput, '14250000.4')
    })

    act(() => {
      mounted.findButtonByText('SAVE CONFIGURATION').click()
    })
    expect(mounted.findButtonByText('SAVING...').disabled).toBe(true)
    expect(mounted.findButtonByText('CANCEL').disabled).toBe(true)

    await act(async () => {
      resolveSave()
      await Promise.resolve()
    })
    expect(spies.UpdateExpeditionCarrierConfig).toHaveBeenCalledWith(12, 'squadron', 14250000)
    expect(onClose).toHaveBeenCalledTimes(1)

    // Negative input clamps to 0 and RPC rejection surfaces error banner without closing.
    onClose.mockClear()
    spies.UpdateExpeditionCarrierConfig.mockRejectedValueOnce(new Error('carrier update failed'))
    act(() => {
      setFormControlValue(upkeepInput, '-250000')
    })
    await act(async () => {
      mounted.findButtonByText('SAVE CONFIGURATION').click()
      await Promise.resolve()
    })
    expect(spies.UpdateExpeditionCarrierConfig).toHaveBeenLastCalledWith(12, 'squadron', 0)
    expect(mounted.container.textContent).toContain('carrier update failed')
    expect(onClose).not.toHaveBeenCalled()
  })
})
