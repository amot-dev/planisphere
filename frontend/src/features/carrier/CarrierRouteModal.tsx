import { ModalShell } from '../../components/ui/ModalShell'
import { VesselClassificationSelector } from '../../components/ui/VesselClassificationSelector'

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
    <ModalShell
      title="Spansh Fleet Carrier Router"
      accent="cyan"
      maxWidth="max-w-md"
      onClose={onClose}
      closeDisabled={prompt.loading}
      bodyClassName="p-4 space-y-3.5 text-xs overflow-y-auto"
      footer={
        <div className="px-4 py-3 bg-[#140e09] border-t border-[#3d2816] flex items-center justify-end gap-2 text-xs">
          <button
            type="button"
            disabled={prompt.loading}
            onClick={onClose}
            className="px-3 py-1.5 text-[#a6907c] hover:text-[#F4ECE4] uppercase font-bold tracking-wider cursor-pointer whitespace-nowrap"
          >
            CANCEL
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
      }
    >
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

      <VesselClassificationSelector
        carrierType={prompt.carrierType}
        onSelectCarrierType={(nextType) =>
          onUpdate((prev) => ({ ...prev, carrierType: nextType }))
        }
      />

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
            Fuel Depot (Tonnes, 0 – 1,000)
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
            Found in Carrier Management under Navigation Tritium Reserves.
          </p>
        </div>
      </div>
    </ModalShell>
  )
}

