import { stats } from '../../api/models'
import { formatCredits } from '../../domain/galacticTime'

/**
 * ExobiologyValueBreakdownProps defines the discovery metrics and styling
 * options for exobiology organic valuation breakdown lists.
 */
export interface ExobiologyValueBreakdownProps {
  metrics: stats.Metrics
  className?: string
  boldBonuses?: boolean
}

/**
 * ExobiologyValueBreakdown renders the Base Organic Value and First Logged Bonus
 * rows shared between the sidebar valuation panel and waypoint details drawers.
 */
export function ExobiologyValueBreakdown({
  metrics,
  className = '',
  boldBonuses = false,
}: ExobiologyValueBreakdownProps) {
  const bonusValueClass = boldBonuses
    ? 'font-mono-tabular text-ed-emerald font-bold whitespace-nowrap shrink-0'
    : 'font-mono-tabular text-ed-emerald whitespace-nowrap shrink-0'

  return (
    <div className={className}>
      <div className="flex items-center justify-between gap-2 text-ed-text-muted">
        <span className="truncate">Base Organic Value</span>
        <span className="font-mono-tabular text-ed-text-main whitespace-nowrap shrink-0">
          {formatCredits(metrics.organicValueBase)}
        </span>
      </div>
      <div className="flex items-center justify-between gap-2 text-ed-text-muted">
        <span className="truncate">First Logged Bonus</span>
        <span className={bonusValueClass}>+{formatCredits(metrics.organicValueBonus)}</span>
      </div>
    </div>
  )
}

