/**
 * CarrierRoutePromptState holds the form inputs and loading status for the
 * Fleet Carrier Spansh route configuration modal before plotting begins.
 */
export interface CarrierRoutePromptState {
  targetRowId: number
  origin: string
  destination: string
  capacityUsed: string
  depotTritium: string
  carrierType: 'player' | 'squadron'
  loading: boolean
}

/**
 * CarrierRouteModalProps defines the state and event callbacks for the Fleet
 * Carrier Spansh route setup dialog.
 */
export interface CarrierRouteModalProps {
  prompt: CarrierRoutePromptState
  onClose: () => void
  onUpdate: (updater: (prev: CarrierRoutePromptState) => CarrierRoutePromptState) => void
  onStart: () => void
}

/**
 * CarrierRouteModal renders the Fleet Carrier Spansh route configuration modal
 * for specifying origin, destination, vessel class, cargo mass, and depot fuel.
 */
export function CarrierRouteModal({
  prompt,
  onClose,
  onUpdate,
  onStart,
}: CarrierRouteModalProps) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4">
      <div className="w-full max-w-md border border-[#3d2816] border-t-2 border-t-[#00D2FF] bg-[#0b0907] shadow-2xl overflow-hidden text-xs">
        <div className="px-4 py-3 bg-[#140e09] border-b border-[#3d2816] flex items-center justify-between">
          <h3 className="text-xs font-extrabold uppercase tracking-[0.15em] text-[#00D2FF]">
            Fleet Carrier Navigation Plotter
          </h3>
          <button
            type="button"
            disabled={prompt.loading}
            onClick={onClose}
            className="text-[#a6907c] hover:text-[#F4ECE4] text-base leading-none cursor-pointer"
          >
            ×
          </button>
        </div>

        <div className="p-4 space-y-3.5">
          <div className="grid grid-cols-2 gap-2.5">
            <div>
              <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
                Origin System
              </label>
              <input
                type="text"
                value={prompt.origin}
                onChange={(event) =>
                  onUpdate((prev) => ({ ...prev, origin: event.target.value }))
                }
                placeholder="Origin system..."
                className="w-full px-2.5 py-1.5 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
              />
            </div>
            <div>
              <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
                Destination Waypoint
              </label>
              <input
                type="text"
                value={prompt.destination}
                onChange={(event) =>
                  onUpdate((prev) => ({ ...prev, destination: event.target.value }))
                }
                placeholder="Destination system..."
                className="w-full px-2.5 py-1.5 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
              />
            </div>
          </div>

          <div>
            <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
              Vessel Classification
            </label>
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() =>
                  onUpdate((prev) => ({ ...prev, carrierType: 'player' }))
                }
                className={`px-3 py-2 font-bold uppercase tracking-wider border transition cursor-pointer ${
                  prompt.carrierType === 'player'
                    ? 'bg-[#08151c] text-[#00D2FF] border-[#00D2FF] border-l-3'
                    : 'bg-[#120d09] text-[#a6907c] border-[#2e1f12] hover:text-[#F4ECE4]'
                }`}
              >
                Personal Carrier
              </button>
              <button
                type="button"
                onClick={() =>
                  onUpdate((prev) => ({ ...prev, carrierType: 'squadron' }))
                }
                className={`px-3 py-2 font-bold uppercase tracking-wider border transition cursor-pointer ${
                  prompt.carrierType === 'squadron'
                    ? 'bg-[#211307] text-[#FF7100] border-[#FF7100] border-l-3'
                    : 'bg-[#120d09] text-[#a6907c] border-[#2e1f12] hover:text-[#F4ECE4]'
                }`}
              >
                Squadron Carrier
              </button>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-2.5">
            <div>
              <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
                Used Capacity (Tonnes, 0 – {prompt.carrierType === 'squadron' ? '60,000' : '25,000'})
              </label>
              <input
                type="number"
                min={0}
                max={prompt.carrierType === 'squadron' ? 60000 : 25000}
                value={prompt.capacityUsed}
                onChange={(event) =>
                  onUpdate((prev) => ({ ...prev, capacityUsed: event.target.value }))
                }
                className="w-full px-2.5 py-1.5 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
              />
              <p className="text-[10px] text-[#a6907c] mt-1">
                Found in Carrier Management in top right (Capacity).
              </p>
            </div>
            <div>
              <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
                Depot Tritium (Tonnes, 0 – 1,000)
              </label>
              <input
                type="number"
                min={0}
                max={1000}
                value={prompt.depotTritium}
                onChange={(event) =>
                  onUpdate((prev) => ({ ...prev, depotTritium: event.target.value }))
                }
                className="w-full px-2.5 py-1.5 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
              />
              <p className="text-[10px] text-[#a6907c] mt-1">
                Found in Carrier Management under Navigation.
              </p>
            </div>
          </div>
        </div>

        <div className="px-4 py-3 bg-[#140e09] border-t border-[#3d2816] flex items-center justify-end gap-2">
          <button
            type="button"
            disabled={prompt.loading}
            onClick={onClose}
            className="px-3 py-1.5 text-[#a6907c] hover:text-[#F4ECE4] uppercase font-bold tracking-wider cursor-pointer whitespace-nowrap"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={prompt.loading}
            onClick={onStart}
            className="ed-btn-cyan whitespace-nowrap"
          >
            {prompt.loading ? 'QUERYING SPANSH...' : 'PLOT VIA SPANSH'}
          </button>
        </div>
      </div>
    </div>
  )
}

