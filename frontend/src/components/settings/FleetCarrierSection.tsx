import { useEffect, useState } from 'react'
import { SetTritiumPricePerTonne } from '../../../wailsjs/go/app/App'
import { app } from '../../../wailsjs/go/models'

const DEFAULT_TRITIUM_PRICE_PER_TONNE = 50000

/**
 * FleetCarrierSectionProps defines the settings and callbacks for configuring
 * Fleet Carrier cost valuation parameters inside Terminal Configuration.
 */
export interface FleetCarrierSectionProps {
  settings: app.Settings | null
  onRefresh: () => void
  onSetError: (errorMessage: string) => void
}

/**
 * FleetCarrierSection renders the Tritium price-per-tonne configuration card
 * used to compute estimated Fleet Carrier fuel costs on expeditions.
 */
export function FleetCarrierSection({
  settings,
  onRefresh,
  onSetError,
}: FleetCarrierSectionProps) {
  const configuredPrice =
    settings?.tritiumPricePerTonne && settings.tritiumPricePerTonne > 0
      ? settings.tritiumPricePerTonne
      : DEFAULT_TRITIUM_PRICE_PER_TONNE
  const defaultPrice =
    settings?.defaultTritiumPricePerTonne && settings.defaultTritiumPricePerTonne > 0
      ? settings.defaultTritiumPricePerTonne
      : DEFAULT_TRITIUM_PRICE_PER_TONNE

  const [priceInput, setPriceInput] = useState<string>(() => String(configuredPrice))

  useEffect(() => {
    setPriceInput(String(configuredPrice))
  }, [configuredPrice])

  async function handleSavePrice() {
    onSetError('')
    const parsedPrice = Math.round(Number(priceInput) || 0)
    const nextPrice = parsedPrice > 0 ? parsedPrice : defaultPrice
    try {
      const updatedSettings = await SetTritiumPricePerTonne(nextPrice)
      setPriceInput(String(updatedSettings.tritiumPricePerTonne || defaultPrice))
      onRefresh()
    } catch (saveError) {
      onSetError(String(saveError))
    }
  }

  async function handleResetDefaultPrice() {
    onSetError('')
    try {
      const updatedSettings = await SetTritiumPricePerTonne(defaultPrice)
      setPriceInput(String(updatedSettings.tritiumPricePerTonne || defaultPrice))
      onRefresh()
    } catch (resetError) {
      onSetError(String(resetError))
    }
  }

  return (
    <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#00D2FF]">
          Fleet Carrier Cost Valuation
        </span>
        <span className="text-[10px] font-mono-tabular font-bold uppercase px-2 py-0.5 border bg-[#08151c] border-[#00D2FF]/60 text-[#00D2FF]">
          {configuredPrice.toLocaleString()} Cr / t
        </span>
      </div>

      <div className="text-[11px] text-[#a6907c]">
        Estimated market price per tonne of Tritium used when calculating Expedition Carrier Cost
        (defaults to {defaultPrice.toLocaleString()} Cr/t).
      </div>

      <div className="flex items-center gap-2">
        <input
          type="number"
          min={1}
          step={1000}
          aria-label="Tritium Price per Tonne (Cr)"
          value={priceInput}
          onChange={(event) => setPriceInput(event.target.value)}
          className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-xs text-[#F4ECE4]"
        />
        <button
          type="button"
          onClick={handleSavePrice}
          className="ed-btn-primary"
        >
          Apply
        </button>
        <button
          type="button"
          disabled={configuredPrice === defaultPrice && Number(priceInput) === defaultPrice}
          onClick={handleResetDefaultPrice}
          title="Reset Tritium price to the default 50,000 Cr per tonne"
          className="ed-btn-cyan"
        >
          Reset to Default
        </button>
      </div>
    </div>
  )
}

