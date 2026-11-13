import { useEffect, useState } from 'react'
import { app, stats } from '../../../wailsjs/go/models'
import { formatInt, formatLy, nowGalacticTime } from '../../utils/galacticTime'
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
  onLogCarrierJump?: () => void
}

/**
 * NavigationTelemetrySection renders the Duration, Personal Navigation Metrics,
 * and Carrier Operations cards in the telemetry sidebar.
 */
export function NavigationTelemetrySection({
  viewScope,
  sidebarStats,
  displayedMetrics,
  carrierRouteOpen,
  onLogCarrierJump,
}: NavigationTelemetrySectionProps) {
  const isActiveExpeditionClock = Boolean(sidebarStats?.started && sidebarStats?.endIsNow)
  const [currentGalacticTime, setCurrentGalacticTime] = useState<string>(() => nowGalacticTime())

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

      {/* Personal Navigation Metrics (Ship and Carrier Passenger Travel) */}
      <div className="border border-[#3d2816] bg-[#0e0a07] divide-y divide-[#24180e]">
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

      {/* Expedition / Personal Carrier Operations */}
      <div className="border border-[#3d2816] bg-[#0e0a07] overflow-hidden">
        <div className="px-3 py-1.5 bg-[#140e09] border-b border-[#3d2816] text-[10px] font-extrabold uppercase tracking-[0.14em] text-[#F4ECE4] flex items-center justify-between">
          <span>
            {viewScope === 'allTime'
              ? 'Personal Carrier Operations'
              : 'Expedition Carrier Operations'}
          </span>
          {viewScope === 'allTime' ? (
            <span className="text-[9px] text-[#a6907c] font-mono-tabular">
              OWNED VESSEL
            </span>
          ) : sidebarStats?.status !== 'completed' ? (
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
          ) : null}
        </div>
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
          <StatRow
            label={
              viewScope === 'allTime' ? 'Personal Carrier Tritium' : 'Expedition Carrier Tritium'
            }
            value={`${formatInt(displayedMetrics.tritiumUsed || 0)} t`}
          />
        </div>
      </div>
    </>
  )
}

