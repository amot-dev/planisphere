import { useState } from 'react'
import { UpdateExpeditionCarrierConfig } from '../../api/backend'
import { ModalShell } from '../../components/ui/ModalShell'
import { VesselClassificationSelector } from '../../components/ui/VesselClassificationSelector'

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
    <ModalShell
      title="Expedition Carrier Configuration"
      accent="cyan"
      maxWidth="max-w-md"
      onClose={onClose}
      closeDisabled={saving}
      bodyClassName="p-4 space-y-3.5 text-xs overflow-y-auto"
      footer={
        <div className="px-4 py-3 bg-ed-header border-t border-ed-border flex items-center justify-end gap-2 text-xs">
          <button
            type="button"
            disabled={saving}
            onClick={onClose}
            className="px-3 py-1.5 text-ed-text-muted hover:text-ed-text-main uppercase font-bold tracking-wider cursor-pointer whitespace-nowrap"
          >
            CANCEL
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
      }
    >
      {errorMessage && (
        <div className="p-2 border border-ed-danger bg-ed-danger-tint text-ed-danger-text">
          {errorMessage}
        </div>
      )}

      <VesselClassificationSelector
        carrierType={carrierType}
        onSelectCarrierType={setCarrierType}
        description="Sets the default vessel classification when opening the Spansh Fleet Carrier Router for this expedition."
      />

      <div>
        <label
          htmlFor="expedition-carrier-weekly-upkeep"
          className="block text-[10px] font-bold uppercase tracking-wider text-ed-text-muted mb-1"
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
          className="w-full px-2.5 py-1.5 bg-ed-void border border-ed-border font-mono-tabular text-ed-text-main focus:border-ed-cyan focus:outline-none"
        />
        <p className="text-[10px] text-ed-text-muted mt-1">
          Found in Carrier Management under Budget. Continuously pro-rated across the
          expedition duration (set to 0 if no carrier upkeep applies).
        </p>
      </div>
    </ModalShell>
  )
}

