/**
 * VesselClassificationSelectorProps defines the active Fleet Carrier type,
 * change callback, and optional explanatory helper text.
 */
export interface VesselClassificationSelectorProps {
  carrierType: 'player' | 'squadron'
  onSelectCarrierType: (nextType: 'player' | 'squadron') => void
  description?: string
}

/**
 * VesselClassificationSelector renders the two-button Personal Carrier vs
 * Squadron Carrier toggle shared by the carrier setup and routing modals.
 */
export function VesselClassificationSelector({
  carrierType,
  onSelectCarrierType,
  description,
}: VesselClassificationSelectorProps) {
  return (
    <div>
      <label className="block text-[10px] font-bold uppercase tracking-wider text-ed-text-muted mb-1">
        Vessel Classification
      </label>
      <div className="grid grid-cols-2 gap-2">
        <button
          type="button"
          onClick={() => onSelectCarrierType('player')}
          className={`px-3 py-2 font-bold uppercase tracking-wider border transition cursor-pointer ${
            carrierType === 'player'
              ? 'bg-ed-cyan-tint text-ed-cyan border-ed-cyan border-l-3'
              : 'bg-ed-surface text-ed-text-muted border-ed-border-dim hover:text-ed-text-main'
          }`}
        >
          Personal Carrier
        </button>
        <button
          type="button"
          onClick={() => onSelectCarrierType('squadron')}
          className={`px-3 py-2 font-bold uppercase tracking-wider border transition cursor-pointer ${
            carrierType === 'squadron'
              ? 'bg-ed-amber-tint text-ed-amber border-ed-amber border-l-3'
              : 'bg-ed-surface text-ed-text-muted border-ed-border-dim hover:text-ed-text-main'
          }`}
        >
          Squadron Carrier
        </button>
      </div>
      {description && <p className="text-[10px] text-ed-text-muted mt-1">{description}</p>}
    </div>
  )
}

