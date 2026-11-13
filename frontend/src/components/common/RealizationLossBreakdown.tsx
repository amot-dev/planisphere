import { stats } from '../../../wailsjs/go/models'
import { formatLoss } from '../../utils/galacticTime'

/**
 * RealizationLossBreakdownProps defines the realization loss payload, container
 * styling classes, and optional label colon formatting for breakdown lists.
 */
export interface RealizationLossBreakdownProps {
  realization?: stats.RealizationBreakdown | null
  className?: string
  withColons?: boolean
}

const LOSS_ROWS: Array<{
  label: string
  getValue: (realization?: stats.RealizationBreakdown | null) => number | null | undefined
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
  withColons = false,
}: RealizationLossBreakdownProps) {
  return (
    <div className={className}>
      {LOSS_ROWS.map((lossRow) => (
        <div key={lossRow.label} className="flex items-center justify-between gap-2 text-[#a6907c]">
          <span className="truncate">{withColons ? `${lossRow.label}:` : lossRow.label}</span>
          <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
            {formatLoss(lossRow.getValue(realization))}
          </span>
        </div>
      ))}
    </div>
  )
}

