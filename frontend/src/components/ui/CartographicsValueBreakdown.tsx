import { stats } from '../../api/models'
import {
  formatCredits,
  formatEfficiencyBonusLabel,
  formatEfficiencyBonusTooltip,
} from '../../domain/galacticTime'

/**
 * CartographicsValueBreakdownProps defines the discovery metrics and styling
 * options for cartographics breakdown lists.
 */
export interface CartographicsValueBreakdownProps {
  /** Discovery and mapping metrics containing cartographic value totals. */
  metrics: stats.Metrics
  /** Optional container CSS classes for layout and border styling. */
  className?: string
  /** Applies bold styling to bonus credit values when true. */
  boldBonuses?: boolean
}

/**
 * CartographicsValueBreakdown renders the three standardized cartographics
 * valuation rows shared between the sidebar valuation panel and waypoint details drawers.
 */
export function CartographicsValueBreakdown({
  metrics,
  className = '',
  boldBonuses = false,
}: CartographicsValueBreakdownProps) {
  const bonusValueClassName = `font-mono-tabular text-[#00D2FF]${boldBonuses ? ' font-bold' : ''} whitespace-nowrap shrink-0`

  return (
    <div className={className}>
      <div className="flex items-center justify-between gap-2 text-[#a6907c]">
        <span className="truncate">Base Scans and Mapping</span>
        <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
          {formatCredits(metrics.scanValueBase)}
        </span>
      </div>
      <div className="flex items-center justify-between gap-2 text-[#a6907c]">
        <span className="truncate">First Discovered / Mapped</span>
        <span className={bonusValueClassName}>+{formatCredits(metrics.scanValueBonus)}</span>
      </div>
      <div
        className="flex items-center justify-between gap-2 text-[#a6907c]"
        title={formatEfficiencyBonusTooltip(metrics.efficientMappings, metrics.bodiesMapped)}
      >
        <span className="truncate">
          {formatEfficiencyBonusLabel(metrics.efficientMappings, metrics.bodiesMapped)}
        </span>
        <span className={bonusValueClassName}>
          +{formatCredits(metrics.scanValueEfficiencyBonus)}
        </span>
      </div>
    </div>
  )
}

