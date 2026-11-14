import { useState } from 'react'
import { UpdateExpeditionCarrierConfig } from '../../../wailsjs/go/app/App'

/**
 * CarrierConfigModalProps defines the expedition identifier, initial Fleet
 * Carrier settings, and close callback for the per-expedition carrier modal.
 */
export interface CarrierConfigModalProps {
  expeditionId: number
  initialCarrierType: 'player' | 'squadron'
  initialWeeklyUpkeep: number
  onClose: () => void
}

/**
 * CarrierConfigModal renders the per-expedition Fleet Carrier configuration
 * dialog for selecting vessel classification and weekly upkeep cost.
 */
export function CarrierConfigModal({
  expeditionId,
  initialCarrierType,
  initialWeeklyUpkeep,
  onClose,
}: CarrierConfigModalProps) {
  const [carrierType, setCarrierType] = useState<'player' | 'squadron'>(initialCarrierType)
  const [weeklyUpkeepInput, setWeeklyUpkeepInput] = useState<string>(() =>
    String(Math.max(0, initialWeeklyUpkeep || 0)),
  )
  const [saving, setSaving] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')

  async function handleSave() {
    if (!expeditionId || saving) {
      return
    }
    const parsedWeeklyUpkeep = Math.max(0, Math.round(Number(weeklyUpkeepInput) || 0))
    setSaving(true)
    setErrorMessage('')
    try {
      await UpdateExpeditionCarrierConfig(expeditionId, carrierType, parsedWeeklyUpkeep)
      onClose()
    } catch (saveError) {
      setErrorMessage(String(saveError))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4">
      <div className="w-full max-w-md border border-[#3d2816] border-t-2 border-t-[#00D2FF] bg-[#0b0907] shadow-2xl overflow-hidden text-xs">
        <div className="px-4 py-3 bg-[#140e09] border-b border-[#3d2816] flex items-center justify-between">
          <h3 className="text-xs font-extrabold uppercase tracking-[0.15em] text-[#00D2FF]">
            Expedition Carrier Configuration
          </h3>
          <button
            type="button"
            disabled={saving}
            onClick={onClose}
            className="text-[#a6907c] hover:text-[#F4ECE4] text-base leading-none cursor-pointer"
          >
            ×
          </button>
        </div>

        <div className="p-4 space-y-3.5">
          {errorMessage && (
            <div className="p-2 border border-[#ff3b2f] bg-[#190909] text-[#ff4d3d]">
              {errorMessage}
            </div>
          )}

          <div>
            <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
              Vessel Classification
            </label>
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() => setCarrierType('player')}
                className={`px-3 py-2 font-bold uppercase tracking-wider border transition cursor-pointer ${
                  carrierType === 'player'
                    ? 'bg-[#08151c] text-[#00D2FF] border-[#00D2FF] border-l-3'
                    : 'bg-[#120d09] text-[#a6907c] border-[#2e1f12] hover:text-[#F4ECE4]'
                }`}
              >
                Personal Carrier
              </button>
              <button
                type="button"
                onClick={() => setCarrierType('squadron')}
                className={`px-3 py-2 font-bold uppercase tracking-wider border transition cursor-pointer ${
                  carrierType === 'squadron'
                    ? 'bg-[#211307] text-[#FF7100] border-[#FF7100] border-l-3'
                    : 'bg-[#120d09] text-[#a6907c] border-[#2e1f12] hover:text-[#F4ECE4]'
                }`}
              >
                Squadron Carrier
              </button>
            </div>
            <p className="text-[10px] text-[#a6907c] mt-1">
              Sets the default vessel classification when opening the Spansh Fleet Carrier Route
              Plotter for this expedition.
            </p>
          </div>

          <div>
            <label
              htmlFor="expedition-carrier-weekly-upkeep"
              className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1"
            >
              Weekly Upkeep (Cr / Week)
            </label>
            <input
              id="expedition-carrier-weekly-upkeep"
              type="number"
              min={0}
              step={50000}
              value={weeklyUpkeepInput}
              onChange={(event) => setWeeklyUpkeepInput(event.target.value)}
              className="w-full px-2.5 py-1.5 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
            />
            <p className="text-[10px] text-[#a6907c] mt-1">
              Found in Carrier Management under Budget. Continuously pro-rated across the
              expedition duration (set to 0 if no carrier upkeep applies).
            </p>
          </div>
        </div>

        <div className="px-4 py-3 bg-[#140e09] border-t border-[#3d2816] flex items-center justify-end gap-2">
          <button
            type="button"
            disabled={saving}
            onClick={onClose}
            className="px-3 py-1.5 text-[#a6907c] hover:text-[#F4ECE4] uppercase font-bold tracking-wider cursor-pointer whitespace-nowrap"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={saving}
            onClick={handleSave}
            className="ed-btn-cyan whitespace-nowrap"
          >
            {saving ? 'SAVING...' : 'SAVE CONFIGURATION'}
          </button>
        </div>
      </div>
    </div>
  )
}

