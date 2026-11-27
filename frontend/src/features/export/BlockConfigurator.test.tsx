// @vitest-environment happy-dom
import { act } from 'react'
import { describe, expect, it } from 'vitest'
import { buildSampleExportData } from '../../test/fixtures/exportFixtures'
import { mountTestRoot, setFormControlValue, type MountedTestRoot } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { BlockConfigurator } from './BlockConfigurator'
import { useExportStudio } from './useExportStudio'

async function flushMicrotasks(): Promise<void> {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
}

function BlockConfiguratorHarness({ expeditionId }: { expeditionId: number }) {
  const studio = useExportStudio(expeditionId)
  if (!studio.exportData) {
    return null
  }
  return (
    <div>
      <BlockConfigurator
        exportData={studio.exportData}
        config={studio.config}
        availableDetailsMetricIds={studio.availableDetailsMetricIds}
        addCount={studio.expandedPreviewItems.length}
        canAddToCart={studio.canAddToCart}
        disabled={studio.exporting}
        onSelectBlockKind={studio.handleSelectBlockKind}
        onChangeConfig={studio.handleConfigChange}
        onAddToCart={studio.handleAddToCart}
      />
      <div data-testid="cart-count">{`Export Cart (${studio.cartItems.length})`}</div>
    </div>
  )
}

describe('BlockConfigurator', () => {
  it('omits Custom Timeframe and keeps 24h-gated Details metrics selectable when Slice 0 is under 24h and a later slice reaches 24h', async () => {
    const baseData = buildSampleExportData()
    const sub24hFirstLegData = buildSampleExportData({
      rowDetails: baseData.rowDetails.map((detail, index) =>
        index === 0
          ? ({
              ...detail,
              from: '2026-04-01T10:00:00Z',
              to: '2026-04-01T16:00:00Z',
              elapsed: '06h 00m',
            } as typeof detail)
          : detail,
      ),
    })

    installWailsAppMock({
      GetExpeditionExportData: async () => sub24hFirstLegData,
    })

    let mounted!: MountedTestRoot
    await act(async () => {
      mounted = mountTestRoot(<BlockConfiguratorHarness expeditionId={7} />)
      await flushMicrotasks()
    })

    const initialScopeSelect = mounted.container.querySelector('select') as HTMLSelectElement
    expect(initialScopeSelect).toBeTruthy()
    const scopeOptionValues = Array.from(initialScopeSelect.options).map((option) => option.value)
    expect(scopeOptionValues).not.toContain('customTime')
    expect(mounted.container.textContent).not.toContain('Custom Timeframe')

    const clickAndFlush = async (element: HTMLElement) => {
      await act(async () => {
        element.click()
        await flushMicrotasks()
      })
    }

    await clickAndFlush(mounted.findButtonByText('Details', { exact: true }))
    const [scopeSelect, sectionSelect] = Array.from(mounted.container.querySelectorAll('select'))
    await act(async () => {
      setFormControlValue(scopeSelect, 'allLegs')
      await flushMicrotasks()
    })

    // Slice 0 lasted 6h (< 24h), while Slice 1 lasted 50h (>= 24h): Avg Distance per Day must stay enabled.
    expect(mounted.findButtonByText('Avg Distance per Day', { exact: false }).disabled).toBe(false)
    await clickAndFlush(mounted.findButtonByText('[None]', { exact: true }))
    expect(mounted.findButtonByText('+ ADD 3 BLOCKS TO CART', { exact: false }).disabled).toBe(
      true,
    )
    await clickAndFlush(mounted.findButtonByText('Avg Distance per Day', { exact: false }))
    expect(mounted.findButtonByText('+ ADD 3 BLOCKS TO CART', { exact: false }).disabled).toBe(
      false,
    )

    // Exploration Value likewise keeps Avg Credits per Day enabled and addable across All Legs.
    await act(async () => {
      setFormControlValue(sectionSelect, 'explorationValue')
      await flushMicrotasks()
    })
    expect(mounted.findButtonByText('Avg Credits per Day', { exact: false }).disabled).toBe(false)
    await clickAndFlush(mounted.findButtonByText('[None]', { exact: true }))
    await clickAndFlush(mounted.findButtonByText('Avg Credits per Day', { exact: false }))
    await clickAndFlush(mounted.findButtonByText('+ ADD 3 BLOCKS TO CART', { exact: false }))
    expect(mounted.container.textContent).toContain('Export Cart (6)')

    // Scoping down to the single 6h leg alone disables Avg Credits per Day and blocks + ADD TO CART.
    await act(async () => {
      setFormControlValue(scopeSelect, 'leg')
      await flushMicrotasks()
    })
    expect(mounted.findButtonByText('Avg Credits per Day', { exact: false }).disabled).toBe(true)
    expect(mounted.findButtonByText('+ ADD TO CART', { exact: false }).disabled).toBe(true)
  })
})
