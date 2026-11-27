// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot, setFormControlValue } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { FleetCarrierSection } from './FleetCarrierSection'

describe('FleetCarrierSection', () => {
  it('edits Tritium price per tonne, clamps non-positive or invalid values to default, and resets or reports errors', async () => {
    const onSetError = vi.fn()
    const { spies } = installWailsAppMock({
      SetTritiumPricePerTonne: async (pricePerTonne: number) =>
        makeSettings({
          tritiumPricePerTonne: pricePerTonne,
          defaultTritiumPricePerTonne: 50000,
        }),
    })

    const mounted = mountTestRoot(
      createElement(FleetCarrierSection, {
        settings: makeSettings({
          tritiumPricePerTonne: 0,
          defaultTritiumPricePerTonne: 50000,
        }),
        onSetError,
      }),
    )

    expect(mounted.container.textContent).toContain('50,000 Cr / t')
    const priceInput = mounted.container.querySelector(
      'input[aria-label="Tritium Price per Tonne (Cr)"]',
    ) as HTMLInputElement
    expect(priceInput.value).toBe('50000')
    expect(mounted.findButtonByText('RESET TO DEFAULT').disabled).toBe(true)

    // Rounding positive price and saving via APPLY.
    act(() => setFormControlValue(priceInput, '64750.6'))
    expect(mounted.findButtonByText('RESET TO DEFAULT').disabled).toBe(false)
    await act(async () => {
      mounted.findButtonByText('APPLY').click()
      await Promise.resolve()
    })
    expect(spies.SetTritiumPricePerTonne).toHaveBeenCalledWith(64751)
    expect(priceInput.value).toBe('64751')

    // Zero or negative input falls back to default price on APPLY.
    act(() => setFormControlValue(priceInput, '-2000'))
    await act(async () => {
      mounted.findButtonByText('APPLY').click()
      await Promise.resolve()
    })
    expect(spies.SetTritiumPricePerTonne).toHaveBeenLastCalledWith(50000)

    // Rerendering with non-default configured price enables RESET TO DEFAULT.
    mounted.rerender(
      createElement(FleetCarrierSection, {
        settings: makeSettings({
          tritiumPricePerTonne: 75000,
          defaultTritiumPricePerTonne: 50000,
        }),
        onSetError,
      }),
    )
    expect(mounted.container.textContent).toContain('75,000 Cr / t')
    expect(priceInput.value).toBe('75000')

    await act(async () => {
      mounted.findButtonByText('RESET TO DEFAULT').click()
      await Promise.resolve()
    })
    expect(spies.SetTritiumPricePerTonne).toHaveBeenLastCalledWith(50000)

    // RPC rejection surfaces error via onSetError.
    spies.SetTritiumPricePerTonne.mockRejectedValueOnce(new Error('price persistence failed'))
    await act(async () => {
      mounted.findButtonByText('APPLY').click()
      await Promise.resolve()
    })
    expect(onSetError).toHaveBeenLastCalledWith('Error: price persistence failed')
  })
})
