import { memo, useState } from 'react'
import { expedition } from '../../../api/models'

const DEFAULT_VISIBLE_NON_WAYPOINT_VISITS = 3

function formatWaypointNumber(waypointNumber: number): string {
  return String(waypointNumber).padStart(2, '0')
}

function ResultCardHeader({
  systemName,
  poiName,
  expeditionName,
  rightTag,
}: {
  systemName: string
  poiName: string
  expeditionName: string
  rightTag: string
}) {
  return (
    <div className="flex items-center justify-between gap-3 min-w-0">
      <div className="flex items-center gap-1.5 min-w-0">
        <span className="font-mono-tabular font-bold text-xs text-ed-text-main truncate">
          {systemName || 'Unnamed System'}
        </span>
        {poiName && (
          <>
            <span className="text-ed-text-dim shrink-0">·</span>
            <span className="text-[11px] text-ed-text-muted truncate">{poiName}</span>
          </>
        )}
      </div>
      <div className="flex items-center gap-1.5 shrink-0 max-w-[48%]">
        <span className="text-[11px] font-bold uppercase tracking-wider text-ed-text-body truncate">
          {expeditionName}
        </span>
        <span className="text-ed-text-dim shrink-0">·</span>
        <span className="font-mono-tabular text-[11px] font-bold text-ed-amber shrink-0">
          {rightTag}
        </span>
      </div>
    </div>
  )
}

function ResultCardBadges({
  isCurrentExpedition,
  status,
  className = '',
}: {
  isCurrentExpedition: boolean
  status: string
  className?: string
}) {
  const statusLabel = (status || 'planning').toUpperCase()
  return (
    <div className={`flex items-center gap-1.5 shrink-0 ${className}`}>
      {isCurrentExpedition && (
        <span className="text-[9px] font-mono-tabular font-extrabold uppercase px-1.5 py-0.5 bg-ed-cyan-tint border border-ed-cyan text-ed-cyan shrink-0">
          CURRENT
        </span>
      )}
      <span className="text-[9px] font-mono-tabular font-bold uppercase px-1.5 py-0.5 border bg-ed-control border-ed-border text-ed-text-muted shrink-0">
        {statusLabel}
      </span>
    </div>
  )
}

/**
 * WaypointSearchResultCardProps defines a single or condensed multi-stop search
 * match, selection highlight state, and waypoint navigation callback.
 */
export interface WaypointSearchResultCardProps {
  result: expedition.WaypointSearchResult
  resultIndex: number
  isCurrentExpedition: boolean
  isSelected: boolean
  onHoverSelect: (resultIndex: number) => void
  onNavigateToWaypoint: (expeditionId: number, rowId: number) => void
}

/**
 * WaypointSearchResultCard renders either a compact single-stop clickable card
 * or a condensed hub card with individual clickable waypoint stop chips.
 */
export const WaypointSearchResultCard = memo(function WaypointSearchResultCard({
  result,
  resultIndex,
  isCurrentExpedition,
  isSelected,
  onHoverSelect,
  onNavigateToWaypoint,
}: WaypointSearchResultCardProps) {
  const stops = result.stops ?? []
  const firstStop = stops[0]
  if (!firstStop) return null

  const cardSurfaceClass = isSelected
    ? 'bg-ed-hover border-ed-amber'
    : 'bg-ed-surface border-ed-border hover:bg-ed-hover hover:border-ed-border-strong'

  if (stops.length === 1) {
    const visitedLabel = firstStop.visitedAtGalactic || firstStop.visitedAt
    return (
      <button
        type="button"
        role="option"
        aria-selected={isSelected}
        data-search-option-index={resultIndex}
        onMouseEnter={() => onHoverSelect(resultIndex)}
        onClick={() => onNavigateToWaypoint(result.expeditionId, firstStop.rowId)}
        className={`w-full text-left px-3 py-2 border transition-colors cursor-pointer space-y-1 ${cardSurfaceClass}`}
      >
        <ResultCardHeader
          systemName={result.systemName}
          poiName={result.poiName}
          expeditionName={result.expeditionName}
          rightTag={`WP #${formatWaypointNumber(firstStop.number)}`}
        />
        <div className="flex items-center justify-between gap-2 min-w-0">
          <span
            className={`font-mono-tabular text-[11px] truncate ${
              visitedLabel ? 'text-ed-emerald' : 'text-ed-text-dim'
            }`}
          >
            {visitedLabel ? `Visited ${visitedLabel}` : 'Unvisited'}
          </span>
          <ResultCardBadges
            isCurrentExpedition={isCurrentExpedition}
            status={result.expeditionStatus}
          />
        </div>
      </button>
    )
  }

  return (
    <div
      role="option"
      aria-selected={isSelected}
      data-search-option-index={resultIndex}
      onMouseEnter={() => onHoverSelect(resultIndex)}
      onClick={() => onNavigateToWaypoint(result.expeditionId, firstStop.rowId)}
      className={`w-full text-left px-3 py-2 border transition-colors cursor-pointer space-y-1.5 ${cardSurfaceClass}`}
    >
      <ResultCardHeader
        systemName={result.systemName}
        poiName={result.poiName}
        expeditionName={result.expeditionName}
        rightTag={`${stops.length} WAYPOINTS`}
      />
      <div className="flex items-start justify-between gap-2">
        <div className="flex flex-wrap items-center gap-1.5 min-w-0">
          {stops.map((stopMatch) => {
            const stopVisitedLabel = stopMatch.visitedAtGalactic || stopMatch.visitedAt
            const formattedNumber = formatWaypointNumber(stopMatch.number)
            return (
              <button
                key={stopMatch.rowId}
                type="button"
                title={
                  stopMatch.poiName
                    ? `Jump to WP #${formattedNumber} (${stopMatch.poiName})`
                    : `Jump to WP #${formattedNumber}`
                }
                onClick={(event) => {
                  event.stopPropagation()
                  onNavigateToWaypoint(result.expeditionId, stopMatch.rowId)
                }}
                className="px-1.5 py-0.5 text-[10px] font-mono-tabular bg-ed-header border border-ed-border text-ed-text-main hover:border-ed-amber hover:text-ed-amber cursor-pointer"
              >
                {`WP #${formattedNumber} · ${stopVisitedLabel || 'Unvisited'}`}
              </button>
            )
          })}
        </div>
        <ResultCardBadges
          isCurrentExpedition={isCurrentExpedition}
          status={result.expeditionStatus}
          className="pt-0.5"
        />
      </div>
    </div>
  )
})

/**
 * NonWaypointVisitsCardProps defines the exact-system non-waypoint journal
 * visit summary and callback for jumping to an attributed expedition.
 */
export interface NonWaypointVisitsCardProps {
  summary: expedition.NonWaypointVisitSummary
  onSelectExpedition: (expeditionId: number) => void
}

/**
 * NonWaypointVisitsCard displays unlinked journal arrivals for an exact-matched
 * star system, showing the three most recent visits with expandable history.
 */
export const NonWaypointVisitsCard = memo(function NonWaypointVisitsCard({
  summary,
  onSelectExpedition,
}: NonWaypointVisitsCardProps) {
  const [visitsExpanded, setVisitsExpanded] = useState(false)
  const allReturnedVisits = summary.visits ?? []
  const visibleVisits = visitsExpanded
    ? allReturnedVisits
    : allReturnedVisits.slice(0, DEFAULT_VISIBLE_NON_WAYPOINT_VISITS)
  const earlierVisitCount = Math.max(summary.totalCount - DEFAULT_VISIBLE_NON_WAYPOINT_VISITS, 0)
  const earliestSuffix = summary.earliestGalactic ? ` (first: ${summary.earliestGalactic})` : ''
  const collapsedSummaryText = `+${earlierVisitCount} earlier ${
    earlierVisitCount === 1 ? 'visit' : 'visits'
  }${earliestSuffix}`

  return (
    <div className="space-y-2 pt-1">
      <div className="text-[10px] font-extrabold uppercase tracking-[0.16em] text-ed-text-muted">
        NON-WAYPOINT VISITS
      </div>
      <div className="border border-ed-border bg-ed-surface">
        <div className="px-3 py-2 border-b border-ed-border-subtle bg-ed-header flex items-center justify-between gap-2">
          <span className="font-mono-tabular font-bold text-xs text-ed-text-main truncate">
            {summary.systemName}
          </span>
          <span className="font-mono-tabular text-[10px] font-bold uppercase tracking-wider text-ed-emerald shrink-0">
            {`${summary.totalCount} ${summary.totalCount === 1 ? 'VISIT' : 'VISITS'}`}
          </span>
        </div>
        <div className="px-3 py-2 space-y-1.5">
          {visibleVisits.map((visitRecord, visitIndex) => (
            <div
              key={`${visitRecord.timestamp || visitRecord.galactic}-${visitIndex}`}
              className="flex flex-wrap items-center justify-between gap-2 text-[11px]"
            >
              <span className="font-mono-tabular text-ed-text-main shrink-0">
                {visitRecord.galactic}
              </span>
              {visitRecord.expeditionId && visitRecord.expeditionName ? (
                <button
                  type="button"
                  onClick={() => onSelectExpedition(visitRecord.expeditionId!)}
                  className="text-left text-[11px] text-ed-cyan hover:text-ed-amber underline decoration-ed-cyan/40 hover:decoration-ed-amber cursor-pointer truncate"
                >
                  {`During expedition: ${visitRecord.expeditionName} [${(
                    visitRecord.expeditionStatus || ''
                  ).toUpperCase()}]`}
                </button>
              ) : (
                <span className="text-[11px] text-ed-text-dim">Outside tracked expeditions</span>
              )}
            </div>
          ))}

          {summary.totalCount > DEFAULT_VISIBLE_NON_WAYPOINT_VISITS && (
            <div className="pt-1 border-t border-ed-border-subtle flex flex-wrap items-center justify-between gap-2 text-[11px]">
              {!visitsExpanded ? (
                allReturnedVisits.length > DEFAULT_VISIBLE_NON_WAYPOINT_VISITS ? (
                  <button
                    type="button"
                    onClick={() => setVisitsExpanded(true)}
                    className="font-mono-tabular text-ed-amber hover:text-ed-amber-bright cursor-pointer"
                  >
                    {`${collapsedSummaryText} · Show all`}
                  </button>
                ) : (
                  <span className="font-mono-tabular text-ed-text-muted">
                    {collapsedSummaryText}
                  </span>
                )
              ) : (
                <>
                  {summary.totalCount > allReturnedVisits.length ? (
                    <span className="font-mono-tabular text-ed-text-muted">
                      {`Showing ${allReturnedVisits.length} most recent of ${summary.totalCount} visits (earliest: ${summary.earliestGalactic})`}
                    </span>
                  ) : (
                    <span />
                  )}
                  <button
                    type="button"
                    onClick={() => setVisitsExpanded(false)}
                    className="font-mono-tabular text-ed-amber hover:text-ed-amber-bright cursor-pointer"
                  >
                    Show fewer
                  </button>
                </>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
})
