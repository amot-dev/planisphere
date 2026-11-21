import { memo, useCallback } from 'react'
import { expedition, modes, realization as realizationModel, stats } from '../../../api/models'
import { CartographicsValueBreakdown } from '../../../components/ui/CartographicsValueBreakdown'
import { ExobiologyValueBreakdown } from '../../../components/ui/ExobiologyValueBreakdown'
import { RealizationLossBreakdown } from '../../../components/ui/RealizationLossBreakdown'
import {
  computeShipDistanceLy,
  formatCredits,
  formatInt,
  formatLy,
} from '../../../domain/galacticTime'
import { getWaypointLegBorderColor } from '../../../domain/planTableItems'
import { DEFAULT_LEG_BORDER_COLOR } from '../../../domain/theme'
import { LegModeDropdown } from '../table/widgets/LegModeDropdown'

/**
 * WaypointDetailsDrawerProps defines the telemetry payload, loading state,
 * waypoint row view, and leg flight mode callbacks for an expanded waypoint drawer.
 */
export interface WaypointDetailsDrawerProps {
  details: expedition.WaypointDetails | null
  loading: boolean
  rowView: expedition.RowView
  modeId?: number
  modeColor?: string
  modesList?: modes.Mode[]
  onChangeLegMode?: (rowId: number, nextModeId: number) => void
}

/**
 * WaypointDetailsDrawer renders the expandable row drawer displaying delta leg
 * telemetry and exploration valuations for visited and active current waypoints.
 */
export const WaypointDetailsDrawer = memo(function WaypointDetailsDrawer({
  details,
  loading,
  rowView,
  modeId,
  modeColor,
  modesList,
  onChangeLegMode,
}: WaypointDetailsDrawerProps) {
  const rowId = rowView.row.id
  const handleSelectMode = useCallback(
    (nextModeId: number) => {
      onChangeLegMode?.(rowId, nextModeId)
    },
    [onChangeLegMode, rowId],
  )
  const isOrigin = details?.kind === 'origin' || rowView.number === 1
  const drawerBorderColor =
    getWaypointLegBorderColor(rowView, false, {
      overrideModeColor: modeColor,
      forceOrigin: isOrigin,
    }) ?? DEFAULT_LEG_BORDER_COLOR

  if (loading || !details) {
    return (
      <div
        style={{ borderLeftColor: drawerBorderColor }}
        className="px-6 py-3 bg-ed-panel border-l-2 shadow-[inset_0_1px_0_0_var(--color-ed-border),inset_0_-1px_0_0_var(--color-ed-border-subtle)] text-xs text-ed-amber font-mono-tabular uppercase tracking-wider"
      >
        [ COMPUTING WAYPOINT TELEMETRY... ]
      </div>
    )
  }

  const metrics = details.metrics
  const realization = details.realization

  const shipsUsed = Array.from(
    new Set((details.shipsUsed ?? []).filter((ship) => ship.trim() !== '')),
  )

  return (
    <div
      style={{ borderLeftColor: drawerBorderColor }}
      className="px-6 py-4 bg-ed-panel border-l-2 shadow-[inset_0_1px_0_0_var(--color-ed-border),inset_0_-1px_0_0_var(--color-ed-border-subtle)] text-xs space-y-3"
    >
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-ed-border-dim pb-2">
        <div className="flex items-center gap-2">
          <span className="px-2 py-0.5 text-[10px] font-mono-tabular font-extrabold uppercase tracking-[0.14em] bg-ed-amber-tint text-ed-amber border border-ed-amber">
            {isOrigin ? (
              'HISTORICAL BASELINE'
            ) : (
              <>
                {`LEG #${details.number - 1} `}
                <span className="relative -top-px">→</span>
                {` #${details.number} // DELTA TELEMETRY`}
              </>
            )}
          </span>
          {details.prevSystem && !isOrigin && (
            <span className="font-mono-tabular text-ed-text-muted">
              {`${details.prevSystem} `}
              <span className="relative -top-0.5">→</span>{' '}
              <span className="text-ed-text-main font-bold">
                {rowView.row.systemName?.trim() || 'TBD'}
              </span>
            </span>
          )}
        </div>
        <div className="font-mono-tabular text-[11px] text-ed-text-muted">
          {`${details.fromGalactic || 'Earliest Archive'} `}
          <span className="relative -top-0.5">→</span>{' '}
          <span className="text-ed-cyan font-bold">
            {details.toGalactic || 'IN PROGRESS'}
          </span>
          {details.elapsed && <span className="ml-2 text-ed-text-main">{`[${details.elapsed}]`}</span>}
        </div>
      </div>

      {!isOrigin && (
        <div className="flex flex-wrap items-center gap-3 pt-0.5 pb-0.5">
          <div className="inline-flex items-center gap-2 px-2.5 py-0.5 text-[10px] font-mono-tabular uppercase tracking-wider border border-ed-border-strong border-l-2 border-l-ed-amber bg-ed-control text-ed-amber">
            <span className="font-bold">
              {shipsUsed.length > 1 ? '[VESSELS FLOWN]:' : '[VESSEL FLOWN]:'}
            </span>
            <span className={`font-bold ${shipsUsed.length > 0 ? 'text-ed-text-main' : 'text-ed-text-muted'}`}>
              {shipsUsed.length > 0 ? shipsUsed.join(' // ') : 'None'}
            </span>
          </div>

          {modesList && modesList.length > 0 && onChangeLegMode && (
            <LegModeDropdown
              modeId={modeId || 0}
              modeColor={modeColor}
              modesList={modesList}
              onChangeMode={handleSelectMode}
            />
          )}
        </div>
      )}

      <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
        <MetricMiniCard
          label="Distance Travelled"
          value={formatLy(metrics.distanceLy)}
          sub={`Ship: ${formatLy(computeShipDistanceLy(metrics), 1)} · Carrier: ${formatLy(
            metrics.carrierPassengerLy || 0,
            1,
          )}`}
          accent="amber"
        />
        <MetricMiniCard label="Hyperspace Jumps" value={formatInt(metrics.jumps)} />
        <MetricMiniCard
          label="Boosts (Jet/Pr/St/Ba)"
          value={`${metrics.jetConeBoosts} / ${metrics.premiumBoosts} / ${metrics.standardBoosts} / ${metrics.basicBoosts}`}
        />
        <MetricMiniCard
          label="Scanned / Mapped"
          value={`${formatInt(metrics.bodiesScanned)} / ${formatInt(metrics.bodiesMapped)}`}
          sub={`${formatInt(metrics.firstDiscoveries)} first discovered · ${formatInt(metrics.firstMapped)} first mapped`}
        />
      </div>

      <WaypointValuationCards metrics={metrics} realization={realization} />
    </div>
  )
})

function WaypointValuationCards({
  metrics,
  realization,
}: {
  metrics: stats.Metrics
  realization?: realizationModel.Breakdown
}) {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-3 gap-3 pt-1">
      <div className="p-2.5 border border-ed-border border-l-2 border-l-ed-cyan bg-ed-surface">
        <div className="flex items-center justify-between gap-2">
          <span className="text-[11px] uppercase tracking-wider text-ed-text-muted font-bold truncate">
            Cartographics Value
          </span>
          <span className="font-mono-tabular text-sm font-extrabold text-ed-cyan whitespace-nowrap shrink-0">
            {formatCredits(metrics.scanValueTotal)}
          </span>
        </div>
        <CartographicsValueBreakdown
          metrics={metrics}
          className="mt-1.5 space-y-1 pt-1.5 border-t border-ed-border-dim text-[11px] leading-4"
        />
      </div>

      <div className="p-2.5 border border-ed-border border-l-2 border-l-ed-emerald bg-ed-surface">
        <div className="flex items-center justify-between gap-2">
          <span className="text-[11px] uppercase tracking-wider text-ed-text-muted font-bold truncate">
            Exobiology Value ({formatInt(metrics.organicSamples)} samples)
          </span>
          <span className="font-mono-tabular text-sm font-extrabold text-ed-emerald whitespace-nowrap shrink-0">
            {formatCredits(metrics.organicValueTotal)}
          </span>
        </div>
        <ExobiologyValueBreakdown
          metrics={metrics}
          className="mt-1.5 space-y-1 pt-1.5 border-t border-ed-border-dim text-[11px] leading-4"
        />
      </div>

      <div className="p-2.5 border border-ed-border border-l-2 border-l-ed-amber bg-ed-surface">
        <div className="flex items-center justify-between gap-2">
          <span className="text-[11px] uppercase tracking-wider text-ed-text-muted font-bold truncate">
            Realized Value
          </span>
          <span className="font-mono-tabular text-sm font-extrabold text-ed-amber whitespace-nowrap shrink-0">
            {formatCredits(realization?.totalRealized)}
          </span>
        </div>
        <RealizationLossBreakdown
          realization={realization}
          className="mt-1.5 space-y-1 pt-1.5 border-t border-ed-border-dim text-[11px] leading-4"
        />
      </div>
    </div>
  )
}

function MetricMiniCard({
  label,
  value,
  sub,
  accent,
}: {
  label: string
  value: string
  sub?: string
  accent?: 'amber' | 'cyan'
}) {
  return (
    <div className="p-2 border border-ed-border bg-ed-surface">
      <div className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted truncate">
        {label}
      </div>
      <div
        className={`font-mono-tabular text-xs font-extrabold mt-0.5 ${
          accent === 'amber'
            ? 'text-ed-amber'
            : accent === 'cyan'
              ? 'text-ed-cyan'
              : 'text-ed-text-main'
        }`}
      >
        {value}
      </div>
      {sub && <div className="text-[10px] font-mono-tabular text-ed-text-muted mt-0.5">{sub}</div>}
    </div>
  )
}

