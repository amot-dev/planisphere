import { realization as realizationModel } from '../../api/models'
import { buildRealizationLossRows } from '../../domain/telemetryBreakdowns'

/**
 * RealizationLossBreakdownProps defines the realization loss payload and container
 * styling classes for breakdown lists.
 */
export interface RealizationLossBreakdownProps {
  realization?: realizationModel.Breakdown | null
  className?: string
}

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
      {buildRealizationLossRows(realization).map((lossRow) => (
        <div key={lossRow.label} className="flex items-center justify-between gap-2 text-ed-text-muted">
          <span className="truncate">{lossRow.label}</span>
          <span className="font-mono-tabular text-ed-amber whitespace-nowrap shrink-0">
            {lossRow.value}
          </span>
        </div>
      ))}
    </div>
  )
}

