import { expedition, modes, realization as realizationModel, stats } from '../../../wailsjs/go/models'
import {
  formatCredits,
  formatEfficiencyBonusLabel,
  formatEfficiencyBonusTooltip,
  formatInt,
  formatLy,
} from '../../utils/galacticTime'
import { RealizationLossBreakdown } from '../common/RealizationLossBreakdown'
import { LegModeDropdown } from './widgets/LegModeDropdown'

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
  onChangeMode?: (nextModeId: number) => void
}

/**
 * WaypointDetailsDrawer renders the expandable row drawer displaying delta leg
 * telemetry and exploration valuations for visited and active current waypoints.
 */
export function WaypointDetailsDrawer({
  details,
  loading,
  rowView,
  modeId,
  modeColor,
  modesList,
  onChangeMode,
}: WaypointDetailsDrawerProps) {
  if (loading || !details) {
    return (
      <div className="px-6 py-3 bg-[#0b0907] border-l-2 border-l-[#FF7100] shadow-[inset_0_1px_0_0_#3d2816] text-xs text-[#FF7100] font-mono-tabular uppercase tracking-wider">
        [ COMPUTING WAYPOINT TELEMETRY... ]
      </div>
    )
  }

  const metrics = details.metrics
  const realization = details.realization
  const isOrigin = details.kind === 'origin'

  const shipsUsed = Array.from(
    new Set(
      (rowView.transitions ?? [])
        .filter((transition) => transition.kind === 'ship' && transition.to)
        .map((transition) => transition.to),
    ),
  )

  return (
    <div className="px-6 py-4 bg-[#0b0907] border-l-2 border-l-[#FF7100] shadow-[inset_0_1px_0_0_#3d2816] text-xs space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[#2e1f12] pb-2">
        <div className="flex items-center gap-2">
          <span className="px-2 py-0.5 text-[10px] font-mono-tabular font-extrabold uppercase tracking-[0.14em] bg-[#211307] text-[#FF7100] border border-[#FF7100]">
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
            <span className="font-mono-tabular text-[#a6907c]">
              {`${details.prevSystem} `}
              <span className="relative -top-0.5">→</span>{' '}
              <span className="text-[#F4ECE4] font-bold">
                {rowView.row.systemName?.trim() || 'TBD'}
              </span>
            </span>
          )}
        </div>
        <div className="font-mono-tabular text-[11px] text-[#a6907c]">
          {`${details.fromGalactic || 'Earliest Archive'} `}
          <span className="relative -top-0.5">→</span>{' '}
          <span className="text-[#00D2FF] font-bold">
            {details.toGalactic || 'IN PROGRESS'}
          </span>
          {details.elapsed && <span className="ml-2 text-[#F4ECE4]">{`[${details.elapsed}]`}</span>}
        </div>
      </div>

      {!isOrigin && (
        <div className="flex flex-wrap items-center gap-3 pt-0.5 pb-0.5">
          <div className="inline-flex items-center gap-2 px-2.5 py-0.5 text-[10px] font-mono-tabular uppercase tracking-wider border border-[#472e19] border-l-2 border-l-[#FF7100] bg-[#16100b] text-[#FF7100]">
            <span className="font-bold">
              {shipsUsed.length > 1 ? '[VESSELS FLOWN]:' : '[VESSEL FLOWN]:'}
            </span>
            <span className={`font-bold ${shipsUsed.length > 0 ? 'text-[#F4ECE4]' : 'text-[#a6907c]'}`}>
              {shipsUsed.length > 0 ? shipsUsed.join(' // ') : 'None'}
            </span>
          </div>

          {modesList && modesList.length > 0 && onChangeMode && (
            <LegModeDropdown
              modeId={modeId || 0}
              modeColor={modeColor}
              modesList={modesList}
              onChangeMode={onChangeMode}
            />
          )}
        </div>
      )}

      <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
        <MetricMiniCard
          label="Distance Travelled"
          value={formatLy(metrics.distanceLy)}
          sub={`Ship: ${formatLy(
            metrics.shipDistanceLy || Math.max(0, metrics.distanceLy - (metrics.carrierPassengerLy || 0)),
            1,
          )} · Carrier: ${formatLy(metrics.carrierPassengerLy || 0, 1)}`}
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
}

function WaypointValuationCards({
  metrics,
  realization,
}: {
  metrics: stats.Metrics
  realization?: realizationModel.Breakdown
}) {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-3 gap-3 pt-1">
      <div className="p-2.5 border border-[#3d2816] border-l-2 border-l-[#00D2FF] bg-[#120d09]">
        <div className="flex items-center justify-between gap-2">
          <span className="text-[11px] uppercase tracking-wider text-[#a6907c] font-bold truncate">
            Cartographics Value
          </span>
          <span className="font-mono-tabular text-sm font-extrabold text-[#00D2FF] whitespace-nowrap shrink-0">
            {formatCredits(metrics.scanValueTotal)}
          </span>
        </div>
        <div className="mt-1.5 space-y-1 pt-1.5 border-t border-[#2e1f12] text-[11px] leading-4">
          <div className="flex items-center justify-between gap-2 text-[#a6907c]">
            <span className="truncate">Base Scans and Mapping</span>
            <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
              {formatCredits(metrics.scanValueBase)}
            </span>
          </div>
          <div className="flex items-center justify-between gap-2 text-[#a6907c]">
            <span className="truncate">First Discovered / Mapped</span>
            <span className="font-mono-tabular text-[#00D2FF] whitespace-nowrap shrink-0">
              +{formatCredits(metrics.scanValueBonus)}
            </span>
          </div>
          <div
            className="flex items-center justify-between gap-2 text-[#a6907c]"
            title={formatEfficiencyBonusTooltip(metrics.efficientMappings, metrics.bodiesMapped)}
          >
            <span className="truncate">
              {formatEfficiencyBonusLabel(metrics.efficientMappings, metrics.bodiesMapped)}
            </span>
            <span className="font-mono-tabular text-[#00D2FF] whitespace-nowrap shrink-0">
              +{formatCredits(metrics.scanValueEfficiencyBonus)}
            </span>
          </div>
        </div>
      </div>

      <div className="p-2.5 border border-[#3d2816] border-l-2 border-l-[#00E5A3] bg-[#120d09]">
        <div className="flex items-center justify-between gap-2">
          <span className="text-[11px] uppercase tracking-wider text-[#a6907c] font-bold truncate">
            Exobiology Value ({formatInt(metrics.organicSamples)} samples)
          </span>
          <span className="font-mono-tabular text-sm font-extrabold text-[#00E5A3] whitespace-nowrap shrink-0">
            {formatCredits(metrics.organicValueTotal)}
          </span>
        </div>
        <div className="mt-1.5 space-y-1 pt-1.5 border-t border-[#2e1f12] text-[11px] leading-4">
          <div className="flex items-center justify-between gap-2 text-[#a6907c]">
            <span className="truncate">Base Organic Value</span>
            <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
              {formatCredits(metrics.organicValueBase)}
            </span>
          </div>
          <div className="flex items-center justify-between gap-2 text-[#a6907c]">
            <span className="truncate">First Logged Bonus</span>
            <span className="font-mono-tabular text-[#00E5A3] whitespace-nowrap shrink-0">
              +{formatCredits(metrics.organicValueBonus)}
            </span>
          </div>
        </div>
      </div>

      <div className="p-2.5 border border-[#3d2816] border-l-2 border-l-[#FF7100] bg-[#120d09]">
        <div className="flex items-center justify-between gap-2">
          <span className="text-[11px] uppercase tracking-wider text-[#a6907c] font-bold truncate">
            Realized Value
          </span>
          <span className="font-mono-tabular text-sm font-extrabold text-[#FF7100] whitespace-nowrap shrink-0">
            {formatCredits(realization?.totalRealized)}
          </span>
        </div>
        <RealizationLossBreakdown
          realization={realization}
          className="mt-1.5 space-y-1 pt-1.5 border-t border-[#2e1f12] text-[11px] leading-4"
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
    <div className="p-2 border border-[#3d2816] bg-[#120d09]">
      <div className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] truncate">
        {label}
      </div>
      <div
        className={`font-mono-tabular text-xs font-extrabold mt-0.5 ${
          accent === 'amber'
            ? 'text-[#FF7100]'
            : accent === 'cyan'
              ? 'text-[#00D2FF]'
              : 'text-[#F4ECE4]'
        }`}
      >
        {value}
      </div>
      {sub && <div className="text-[10px] font-mono-tabular text-[#a6907c] mt-0.5">{sub}</div>}
    </div>
  )
}

