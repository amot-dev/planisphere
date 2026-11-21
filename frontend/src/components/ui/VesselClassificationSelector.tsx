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
      <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
        Vessel Classification
      </label>
      <div className="grid grid-cols-2 gap-2">
        <button
          type="button"
          onClick={() => onSelectCarrierType('player')}
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
          onClick={() => onSelectCarrierType('squadron')}
          className={`px-3 py-2 font-bold uppercase tracking-wider border transition cursor-pointer ${
            carrierType === 'squadron'
              ? 'bg-[#211307] text-[#FF7100] border-[#FF7100] border-l-3'
              : 'bg-[#120d09] text-[#a6907c] border-[#2e1f12] hover:text-[#F4ECE4]'
          }`}
        >
          Squadron Carrier
        </button>
      </div>
      {description && <p className="text-[10px] text-[#a6907c] mt-1">{description}</p>}
    </div>
  )
}

