import { stats } from '../../api/models'
import { buildCartographicsBreakdownRows } from '../../domain/telemetryBreakdowns'

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
  const mainValueClassName = 'font-mono-tabular text-ed-text-main whitespace-nowrap shrink-0'
  const bonusValueClassName = `font-mono-tabular text-ed-cyan${boldBonuses ? ' font-bold' : ''} whitespace-nowrap shrink-0`

  return (
    <div className={className}>
      {buildCartographicsBreakdownRows(metrics).map((row) => (
        <div
          key={row.label}
          className="flex items-center justify-between gap-2 text-ed-text-muted"
          title={row.tooltip}
        >
          <span className="truncate">{row.label}</span>
          <span className={row.tone === 'cyan' ? bonusValueClassName : mainValueClassName}>
            {row.value}
          </span>
        </div>
      ))}
    </div>
  )
}

