import { memo } from 'react'
import { expedition } from '../../../../wailsjs/go/models'

/**
 * CoordStatusIndicatorProps defines the waypoint row view whose stellar
 * coordinate resolution state is displayed.
 */
export interface CoordStatusIndicatorProps {
  rowView: expedition.RowView
}

/**
 * CoordStatusIndicator shows the resolution state and coordinate origin for a waypoint:
 * - Green (#00E599): Coordinate known from player's local journal history (previously visited).
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
    return (
      <span
        title={`Coordinates (${row.x.toFixed(1)}, ${row.y.toFixed(1)}, ${row.z.toFixed(1)}) via ${row.coordsSource.toUpperCase()}${
          isJournal ? ' (Previously Visited in Flight Logs)' : ' (EDSM Star Map)'
        }${rowView.detectedRegion ? ` — ${rowView.detectedRegion}` : ''}`}
        className={`w-1.5 h-3 shrink-0 ${isJournal ? 'bg-[#00E599]' : 'bg-[#00D2FF]'}`}
      />
    )
  }
  if (row.coordsSource === 'notfound' || row.coordsSource === 'error') {
    return (
      <span
        title="Coordinates not yet found in local journals or EDSM"
        className="w-1.5 h-3 bg-[#FF7100] shrink-0"
      />
    )
  }
  return (
    <span
      title="Resolving coordinates..."
      className="w-1.5 h-3 bg-[#a6907c] animate-pulse shrink-0"
    />
  )
})

