import { memo } from 'react'
import { expedition } from '../../../../api/models'

/**
 * CoordStatusIndicatorProps defines the waypoint row view whose stellar
 * coordinate resolution state is displayed.
 */
export interface CoordStatusIndicatorProps {
  rowView: expedition.RowView
}

/**
 * CoordStatusIndicator shows the resolution state and coordinate origin for a waypoint:
 * - Emerald (#00E5A3): Coordinate known from player's local journal history (previously visited).
 * - Cyan (#00D2FF): Coordinate known from EDSM community star map.
 * - Amber (#FF7100): System coordinate unknown / not found.
 */
export const CoordStatusIndicator = memo(function CoordStatusIndicator({
  rowView,
}: CoordStatusIndicatorProps) {
  const row = rowView.row
  if (!row.systemName) return null
  if (row.hasCoords) {
    const isJournal = row.coordsSource?.toLowerCase() === 'journal'
    const regionPrefix = rowView.detectedRegion ? `${rowView.detectedRegion} ` : ''
    const coordsLabel = `(${row.x.toFixed(1)}, ${row.y.toFixed(1)}, ${row.z.toFixed(1)})`
    const visitCount = rowView.visitCount || 1
    const visitSuffix = isJournal
      ? `via Journal (${visitCount} ${visitCount === 1 ? 'Visit' : 'Visits'})`
      : 'via EDSM (Never Visited)'
    return (
      <span
        title={`${regionPrefix}${coordsLabel} ${visitSuffix}`}
        className={`w-1.5 h-3 shrink-0 ${isJournal ? 'bg-ed-emerald' : 'bg-ed-cyan'}`}
      />
    )
  }
  if (row.coordsSource === 'notfound' || row.coordsSource === 'error') {
    return (
      <span
        title="Coordinates not yet found in local journals or EDSM"
        className="w-1.5 h-3 bg-ed-amber shrink-0"
      />
    )
  }
  return (
    <span
      title="Resolving coordinates..."
      className="w-1.5 h-3 bg-ed-text-muted animate-pulse shrink-0"
    />
  )
})

