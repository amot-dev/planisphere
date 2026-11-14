import { useEffect, useState } from 'react'
import { app, stats } from '../../../wailsjs/go/models'
import { formatCredits, formatInt, formatLy, nowGalacticTime } from '../../utils/galacticTime'
import { StatRow } from './StatRow'

/**
 * NavigationTelemetrySectionProps defines the active telemetry scope, sidebar
 * duration metadata, navigation metrics, and carrier plotter launch state.
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

  return (
    <>
      {/* Duration */}
      <div className="p-2.5 border border-[#3d2816] border-l-2 border-l-[#FF7100] bg-[#0e0a07]">
        {viewScope === 'allTime' ? (
          <>
            <div className="flex items-center justify-between text-[10px] font-bold uppercase tracking-[0.12em] text-[#a6907c]">
              <span>Career Duration</span>
              {sidebarStats?.careerDuration && (
                <span className="font-mono-tabular text-[#F4ECE4] font-bold">
                  {sidebarStats.careerDuration}
                </span>
              )}
            </div>
            {sidebarStats?.careerStart ? (
              <div className="mt-1.5 font-mono-tabular text-[11px] text-[#F4ECE4] space-y-0.5">
                <div className="flex items-center justify-between">
                  <span className="text-[#a6907c]">START:</span>
                  <span className="text-[#F4ECE4] font-bold text-right">
                    {sidebarStats.careerStartGalactic}
                  </span>
                </div>
              </div>
            ) : (
              <div className="mt-1 text-xs text-[#6e5b4b] uppercase font-mono-tabular text-right">
                STANDBY — AWAITING JOURNALS
              </div>
            )}
          </>
        ) : (
          <>
            <div className="flex items-center justify-between text-[10px] font-bold uppercase tracking-[0.12em] text-[#a6907c]">
              <span>Expedition Duration</span>
              {sidebarStats?.duration && (
                <span className="font-mono-tabular text-[#F4ECE4] font-bold">
                  {sidebarStats.duration}
                </span>
              )}
            </div>
            {sidebarStats?.started ? (
              <div className="mt-1.5 font-mono-tabular text-[11px] text-[#F4ECE4] space-y-0.5">
                <div className="flex items-center justify-between">
                  <span className="text-[#a6907c]">START:</span>
                  <span className="text-[#F4ECE4] font-bold text-right">
                    {sidebarStats.startGalactic}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-[#a6907c]">
                    {sidebarStats.endIsNow ? 'CURRENT:' : 'END:'}
                  </span>
                  <span className="text-[#F4ECE4] font-bold text-right">
                    {sidebarStats.endIsNow ? currentGalacticTime : sidebarStats.endGalactic}
                  </span>
                </div>
              </div>
            ) : (
              <div className="mt-1 text-xs text-[#6e5b4b] uppercase font-mono-tabular text-right">
                STANDBY — ENGAGE EXPEDITION TO START
              </div>
            )}
          </>
        )}
      </div>

      {/* Flight Operations (Ship and Carrier Passenger Travel) */}
      <div className="border border-[#3d2816] bg-[#0e0a07] overflow-hidden">
        <div
          className={`px-2 py-1.5 bg-[#140e09] ${
            flightOpsExpanded ? 'border-b border-[#3d2816]' : ''
          } text-[10px] font-extrabold uppercase tracking-[0.14em] text-[#F4ECE4] flex items-center justify-between`}
        >
          <div className="flex items-center gap-1.5">
            <button
              type="button"
              onClick={onToggleFlightOpsExpanded}
              title="Toggle Flight Operations section"
              className={`h-5 w-5 flex items-center justify-center hover:bg-[#24180e] cursor-pointer ${
                flightOpsExpanded ? 'text-[#FF7100]' : 'text-[#a6907c]'
              }`}
            >
              <svg
                className={`w-3.5 h-3.5 transition-transform ${
                  flightOpsExpanded ? 'rotate-90' : ''
                }`}
                viewBox="0 0 20 20"
                fill="currentColor"
              >
                <path
                  fillRule="evenodd"
                  d="M7.21 14.77a.75.75 0 01.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.25 4.5a.75.75 0 01-1.06-.02z"
                  clipRule="evenodd"
                />
              </svg>
            </button>
            <span>Flight Operations</span>
          </div>
        </div>
        {flightOpsExpanded && (
          <div className="divide-y divide-[#24180e]">
            {/* Distance Travelled (Ly) — Total and Breakdown */}
            <div className="p-2.5">
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold uppercase tracking-wider text-[#F4ECE4]">
                  Distance Travelled (Ly)
                </span>
                <span className="font-mono-tabular text-xs font-extrabold text-[#FF7100]">
                  {formatLy(displayedMetrics.distanceLy)}
                </span>
              </div>
              <div className="mt-1.5 pl-3.5 space-y-0.5 border-l-2 border-[#FF7100]/50 text-[11px]">
                <div className="flex items-center justify-between text-[#a6907c]">
                  <span>By Ship (FSD)</span>
                  <span className="font-mono-tabular text-[#F4ECE4]">
                    {formatLy(
                      displayedMetrics.shipDistanceLy ||
                        Math.max(
                          0,
                          displayedMetrics.distanceLy - (displayedMetrics.carrierPassengerLy || 0),
                        ),
                    )}
                  </span>
                </div>
                <div className="flex items-center justify-between text-[#a6907c]">
                  <span>By Carrier (Passenger)</span>
                  <span className="font-mono-tabular text-[#F4ECE4]">
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
          </div>
        )}
      </div>

      {/* Expedition / Personal Carrier Operations */}
      <div className="border border-[#3d2816] bg-[#0e0a07] overflow-hidden">
        <div
          className={`px-2 py-1.5 bg-[#140e09] ${
            carrierOpsExpanded ? 'border-b border-[#3d2816]' : ''
          } text-[10px] font-extrabold uppercase tracking-[0.14em] text-[#F4ECE4] flex items-center justify-between`}
        >
          <div className="flex items-center gap-1.5">
            <button
              type="button"
              onClick={onToggleCarrierOpsExpanded}
              title="Toggle Carrier Operations section"
              className={`h-5 w-5 flex items-center justify-center hover:bg-[#24180e] cursor-pointer ${
                carrierOpsExpanded ? 'text-[#FF7100]' : 'text-[#a6907c]'
              }`}
            >
              <svg
                className={`w-3.5 h-3.5 transition-transform ${
                  carrierOpsExpanded ? 'rotate-90' : ''
                }`}
                viewBox="0 0 20 20"
                fill="currentColor"
              >
                <path
                  fillRule="evenodd"
                  d="M7.21 14.77a.75.75 0 01.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.25 4.5a.75.75 0 01-1.06-.02z"
                  clipRule="evenodd"
                />
              </svg>
            </button>
            <span>
              {viewScope === 'allTime'
                ? 'Personal Carrier Operations'
                : 'Expedition Carrier Operations'}
            </span>
          </div>
          {viewScope !== 'allTime' ? (
            <div className="flex items-center gap-1.5">
              {sidebarStats?.status !== 'completed' && (
                <button
                  type="button"
                  disabled={carrierRouteOpen}
                  onClick={onLogCarrierJump}
                  title={
                    carrierRouteOpen
                      ? 'Carrier routing panel is already open'
                      : 'Open Fleet Carrier plotter in manual jump logging mode'
                  }
                  className={`px-1.5 py-0.5 text-[9px] font-mono-tabular font-bold uppercase tracking-wider border transition whitespace-nowrap ${
                    carrierRouteOpen
                      ? 'bg-[#120d09] text-[#554030] border-[#2e1f12] cursor-not-allowed'
                      : 'bg-[#08151c] hover:bg-[#00D2FF] text-[#00D2FF] hover:text-[#050506] border border-[#00D2FF] cursor-pointer'
                  }`}
                >
                  LOG JUMP
                </button>
              )}
              <button
                type="button"
                onClick={onOpenCarrierConfig}
                title="Configure expedition Fleet Carrier vessel classification and weekly upkeep"
                className="h-5 w-5 flex items-center justify-center text-[#a6907c] hover:text-[#00D2FF] hover:bg-[#24180e] cursor-pointer"
              >
                <svg className="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                  <path
                    fillRule="evenodd"
                    d="M11.49 3.17c-.38-1.56-2.6-1.56-2.98 0a1.532 1.532 0 01-2.286.948c-1.372-.836-2.942.734-2.106 2.106.54.886.061 2.042-.947 2.287-1.561.379-1.561 2.6 0 2.978a1.532 1.532 0 01.947 2.287c-.836 1.372.734 2.942 2.106 2.106a1.532 1.532 0 012.287.947c.379 1.561 2.6 1.561 2.978 0a1.533 1.533 0 012.287-.947c1.372.836 2.942-.734 2.106-2.106a1.533 1.533 0 01.947-2.287c1.561-.379 1.561-2.6 0-2.978a1.532 1.532 0 01-.947-2.287c.836-1.372-.734-2.942-2.106-2.106a1.532 1.532 0 01-2.287-.947zM10 13a3 3 0 100-6 3 3 0 000 6z"
                    clipRule="evenodd"
                  />
                </svg>
              </button>
            </div>
          ) : null}
        </div>
        {carrierOpsExpanded && (
          <div className="divide-y divide-[#24180e]">
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
                <div className="p-2.5">
                  <button
                    type="button"
                    onClick={onToggleCarrierCostExpanded}
                    className="w-full flex items-center justify-between gap-2 text-left cursor-pointer"
                  >
                    <div className="flex items-center gap-1.5 min-w-0">
                      <span className="text-xs font-bold uppercase tracking-wider text-[#F4ECE4] truncate">
                        Carrier Cost
                      </span>
                      <span className="text-[10px] text-[#00D2FF] shrink-0">
                        {carrierCostExpanded ? '▾' : '▸'}
                      </span>
                    </div>
                    <span className="font-mono-tabular text-xs font-extrabold text-[#00D2FF] whitespace-nowrap shrink-0">
                      {carrierCost.totalEstimated && (carrierCost.totalCost || 0) > 0
                        ? `~${formatCredits(carrierCost.totalCost || 0)}`
                        : formatCredits(carrierCost.totalCost || 0)}
                    </span>
                  </button>
                  {carrierCostExpanded && (
                    <div className="mt-2 pl-3.5 space-y-1 border-l-2 border-[#00D2FF]/50 text-[11px]">
                      <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                        <span className="truncate">Tritium Cost</span>
                        <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
                          {(carrierCost.tritiumCost || 0) > 0
                            ? `~${formatCredits(carrierCost.tritiumCost || 0)}`
                            : formatCredits(0)}
                        </span>
                      </div>
                      <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                        <span className="truncate">Jump Maintenance</span>
                        <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
                          {formatCredits(carrierCost.jumpCost || 0)}
                        </span>
                      </div>
                      <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                        <span className="truncate">Weekly Upkeep</span>
                        <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
                          {(carrierCost.upkeepCost || 0) > 0
                            ? `~${formatCredits(carrierCost.upkeepCost || 0)}`
                            : formatCredits(0)}
                        </span>
                      </div>
                    </div>
                  )}
                </div>
              </>
            )}
          </div>
        )}
      </div>
    </>
  )
}

