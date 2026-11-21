import { useEffect, useState } from 'react'
import { app, stats } from '../../api/models'
import { CollapsibleSectionCard } from '../../components/ui/CollapsibleSectionCard'
import { ExpandableMetricRow } from '../../components/ui/ExpandableMetricRow'
import { CogIcon } from '../../components/ui/HudIcons'
import {
  computeShipDistanceLy,
  formatCredits,
  formatInt,
  formatLy,
  nowGalacticTime,
} from '../../domain/galacticTime'
import { StatRow } from './StatRow'

/**
 * NavigationTelemetrySectionProps defines the active telemetry scope, sidebar
 * duration metadata, navigation metrics, and carrier router launch state.
 */
export interface NavigationTelemetrySectionProps {
  viewScope: 'expedition' | 'allTime'
  sidebarStats: app.SidebarStats | null
  displayedMetrics: stats.Metrics
  carrierRouteOpen: boolean
  flightOpsExpanded?: boolean
  carrierOpsExpanded?: boolean
  carrierCostExpanded?: boolean
  onToggleFlightOpsExpanded?: () => void
  onToggleCarrierOpsExpanded?: () => void
  onToggleCarrierCostExpanded?: () => void
  onLogCarrierJump?: () => void
  onOpenCarrierConfig?: () => void
}

/**
 * NavigationTelemetrySection renders the Duration, Flight Operations,
 * and Carrier Operations cards in the telemetry sidebar.
 */
export function NavigationTelemetrySection({
  viewScope,
  sidebarStats,
  displayedMetrics,
  carrierRouteOpen,
  flightOpsExpanded = true,
  carrierOpsExpanded = true,
  carrierCostExpanded = true,
  onToggleFlightOpsExpanded,
  onToggleCarrierOpsExpanded,
  onToggleCarrierCostExpanded,
  onLogCarrierJump,
  onOpenCarrierConfig,
}: NavigationTelemetrySectionProps) {
  const isActiveExpeditionClock = Boolean(sidebarStats?.started && sidebarStats?.endIsNow)
  const [currentGalacticTime, setCurrentGalacticTime] = useState<string>(() => nowGalacticTime())
  const carrierCost = sidebarStats?.carrierCost ?? new app.CarrierCostBreakdown()

  useEffect(() => {
    if (!isActiveExpeditionClock) {
      return
    }
    setCurrentGalacticTime(nowGalacticTime())
    const clockInterval = setInterval(() => {
      setCurrentGalacticTime(nowGalacticTime())
    }, 60_000)
    return () => clearInterval(clockInterval)
  }, [isActiveExpeditionClock])

  const carrierHeaderActions =
    viewScope !== 'allTime' ? (
      <div className="flex items-center gap-1.5">
        {sidebarStats?.status === 'active' && (
          <button
            type="button"
            disabled={carrierRouteOpen}
            onClick={onLogCarrierJump}
            title={
              carrierRouteOpen
                ? 'Carrier routing panel is already open'
                : 'Open Fleet Carrier router in manual jump logging mode'
            }
            className={`px-1.5 py-0.5 text-[9px] font-mono-tabular font-bold uppercase tracking-wider border transition whitespace-nowrap ${
              carrierRouteOpen
                ? 'bg-ed-surface text-ed-text-disabled border-ed-border-dim cursor-not-allowed'
                : 'bg-ed-cyan-tint hover:bg-ed-cyan text-ed-cyan hover:text-ed-void border border-ed-cyan cursor-pointer'
            }`}
          >
            LOG JUMP
          </button>
        )}
        <button
          type="button"
          onClick={onOpenCarrierConfig}
          title="Configure expedition Fleet Carrier vessel classification and weekly upkeep"
          className="h-5 w-5 flex items-center justify-center text-ed-text-muted hover:text-ed-cyan hover:bg-ed-hover cursor-pointer"
        >
          <CogIcon />
        </button>
      </div>
    ) : null

  return (
    <>
      {/* Duration */}
      <div className="p-2.5 border border-ed-border border-l-2 border-l-ed-amber bg-ed-card-alt">
        {viewScope === 'allTime' ? (
          <>
            <div className="flex items-center justify-between text-[10px] font-bold uppercase tracking-[0.12em] text-ed-text-muted">
              <span>Career Duration</span>
              <span className="font-mono-tabular text-ed-text-main font-bold">
                {sidebarStats?.careerStart ? sidebarStats.careerDuration : 'UNKNOWN'}
              </span>
            </div>
            {sidebarStats?.careerStart && (
              <div className="mt-1.5 font-mono-tabular text-[11px] text-ed-text-main space-y-0.5">
                <div className="flex items-center justify-between">
                  <span className="text-ed-text-muted">START:</span>
                  <span className="text-ed-text-main font-bold text-right">
                    {sidebarStats.careerStartGalactic}
                  </span>
                </div>
              </div>
            )}
          </>
        ) : (
          <>
            <div className="flex items-center justify-between text-[10px] font-bold uppercase tracking-[0.12em] text-ed-text-muted">
              <span>Expedition Duration</span>
              <span className="font-mono-tabular text-ed-text-main font-bold">
                {sidebarStats?.started ? sidebarStats.duration : 'NOT STARTED'}
              </span>
            </div>
            {sidebarStats?.started && (
              <div className="mt-1.5 font-mono-tabular text-[11px] text-ed-text-main space-y-0.5">
                <div className="flex items-center justify-between">
                  <span className="text-ed-text-muted">START:</span>
                  <span className="text-ed-text-main font-bold text-right">
                    {sidebarStats.startGalactic}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-ed-text-muted">
                    {sidebarStats.endIsNow ? 'CURRENT:' : 'END:'}
                  </span>
                  <span className="text-ed-text-main font-bold text-right">
                    {sidebarStats.endIsNow ? currentGalacticTime : sidebarStats.endGalactic}
                  </span>
                </div>
              </div>
            )}
          </>
        )}
      </div>

      {/* Flight Operations (Ship and Carrier Passenger Travel) */}
      <CollapsibleSectionCard
        title="Flight Operations"
        toggleTitle="Toggle Flight Operations section"
        expanded={flightOpsExpanded}
        onToggle={onToggleFlightOpsExpanded}
      >
        {/* Distance Travelled (Ly) — Total and Breakdown */}
        <div className="p-2.5">
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold uppercase tracking-wider text-ed-text-main">
              Distance Travelled (Ly)
            </span>
            <span className="font-mono-tabular text-xs font-extrabold text-ed-amber">
              {formatLy(displayedMetrics.distanceLy)}
            </span>
          </div>
          <div className="mt-1.5 pl-3.5 space-y-0.5 border-l-2 border-ed-amber/50 text-[11px]">
            <div className="flex items-center justify-between text-ed-text-muted">
              <span>By Ship (FSD)</span>
              <span className="font-mono-tabular text-ed-text-main">
                {formatLy(computeShipDistanceLy(displayedMetrics))}
              </span>
            </div>
            <div className="flex items-center justify-between text-ed-text-muted">
              <span>By Carrier (Passenger)</span>
              <span className="font-mono-tabular text-ed-text-main">
                {formatLy(displayedMetrics.carrierPassengerLy)}
              </span>
            </div>
          </div>
        </div>

        <StatRow label="Hyperspace Jumps" value={formatInt(displayedMetrics.jumps)} />
        <StatRow label="Jet Cone Boosts" value={formatInt(displayedMetrics.jetConeBoosts)} />
        <StatRow label="Premium Boosts" value={formatInt(displayedMetrics.premiumBoosts)} />
        <StatRow label="Standard Boosts" value={formatInt(displayedMetrics.standardBoosts)} />
        <StatRow label="Basic Boosts" value={formatInt(displayedMetrics.basicBoosts)} />
      </CollapsibleSectionCard>

      {/* Expedition / Personal Carrier Operations */}
      <CollapsibleSectionCard
        title={
          viewScope === 'allTime'
            ? 'Personal Carrier Operations'
            : 'Expedition Carrier Operations'
        }
        toggleTitle="Toggle Carrier Operations section"
        expanded={carrierOpsExpanded}
        onToggle={onToggleCarrierOpsExpanded}
        headerActions={carrierHeaderActions}
      >
        <StatRow
          label={
            viewScope === 'allTime'
              ? 'Personal Carrier Dist (Ly)'
              : 'Expedition Carrier Dist (Ly)'
          }
          value={formatLy(displayedMetrics.carrierDistanceLy)}
        />
        <StatRow
          label={
            viewScope === 'allTime' ? 'Personal Carrier Jumps' : 'Expedition Carrier Jumps'
          }
          value={formatInt(displayedMetrics.carrierJumps)}
        />
        {viewScope !== 'allTime' && (
          <>
            <StatRow
              label="Expedition Carrier Tritium"
              value={`${formatInt(displayedMetrics.tritiumUsed || 0)} t`}
            />
            <ExpandableMetricRow
              label="Carrier Cost"
              value={
                carrierCost.totalEstimated && (carrierCost.totalCost || 0) > 0
                  ? `~${formatCredits(carrierCost.totalCost || 0)}`
                  : formatCredits(carrierCost.totalCost || 0)
              }
              accent="amber"
              expanded={carrierCostExpanded}
              onToggle={onToggleCarrierCostExpanded}
            >
              <div className="mt-2 pl-3.5 space-y-1 border-l-2 border-ed-amber/50 text-[11px]">
                <div className="flex items-center justify-between gap-2 text-ed-text-muted">
                  <span className="truncate">Tritium Cost</span>
                  <span className="font-mono-tabular text-ed-text-main whitespace-nowrap shrink-0">
                    {(carrierCost.tritiumCost || 0) > 0
                      ? `~${formatCredits(carrierCost.tritiumCost || 0)}`
                      : formatCredits(0)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-ed-text-muted">
                  <span className="truncate">Jump Maintenance</span>
                  <span className="font-mono-tabular text-ed-text-main whitespace-nowrap shrink-0">
                    {formatCredits(carrierCost.jumpCost || 0)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-ed-text-muted">
                  <span className="truncate">Weekly Upkeep</span>
                  <span className="font-mono-tabular text-ed-text-main whitespace-nowrap shrink-0">
                    {(carrierCost.upkeepCost || 0) > 0
                      ? `~${formatCredits(carrierCost.upkeepCost || 0)}`
                      : formatCredits(0)}
                  </span>
                </div>
              </div>
            </ExpandableMetricRow>
          </>
        )}
      </CollapsibleSectionCard>
    </>
  )
}

