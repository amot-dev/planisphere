import { realization as realizationModel } from '../../api/models'
import { formatLoss } from '../../domain/galacticTime'

/**
 * RealizationLossBreakdownProps defines the realization loss payload and container
 * styling classes for breakdown lists.
 */
export interface RealizationLossBreakdownProps {
  realization?: realizationModel.Breakdown | null
  className?: string
}

const LOSS_ROWS: Array<{
  label: string
  getValue: (realization?: realizationModel.Breakdown | null) => number | null | undefined
}> = [
  { label: 'Cartographics Lost (Death)', getValue: (realization) => realization?.cartoLostDeath },
  { label: 'Exobiology Lost (Death)', getValue: (realization) => realization?.organicLostDeath },
  { label: 'Cartographics Stolen', getValue: (realization) => realization?.cartoStolen },
  { label: 'Exobiology Stolen', getValue: (realization) => realization?.organicStolen },
  { label: 'Carrier Tariffs', getValue: (realization) => realization?.carrierTariffLoss },
  { label: 'NPC Crew Payouts', getValue: (realization) => realization?.npcCrewLoss },
]

/**
 * RealizationLossBreakdown renders the six standardized realization loss rows
 * shared between the sidebar valuation panel and waypoint details drawers.
 */
export function RealizationLossBreakdown({
  realization,
  className = '',
}: RealizationLossBreakdownProps) {
  return (
    <div className={className}>
      {LOSS_ROWS.map((lossRow) => (
        <div key={lossRow.label} className="flex items-center justify-between gap-2 text-ed-text-muted">
          <span className="truncate">{lossRow.label}</span>
          <span className="font-mono-tabular text-ed-amber whitespace-nowrap shrink-0">
            {formatLoss(lossRow.getValue(realization))}
          </span>
        </div>
      ))}
    </div>
  )
}

