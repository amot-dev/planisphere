import { memo, useState } from 'react'
import { app, modes, stats } from '../../wailsjs/go/models'
import { formatCredits, formatInt, formatLy } from '../utils/galacticTime'
import { SquareCheckbox } from './SquareCheckbox'

export interface StatsSidebarProps {
  sidebarStats: app.SidebarStats | null
  statsLoading: boolean
  modesState: app.ModesState | null
  filter: stats.Filter
  collapsed?: boolean
  onFilterChange: (next: stats.Filter) => void
  onQuickSwitchMode: (modeId: number) => void
  onToggleCollapse?: () => void
  onRefreshStats?: () => void
}

export const StatsSidebar = memo(function StatsSidebar({
  sidebarStats,
  statsLoading,
  modesState,
  filter,
  collapsed = false,
  onFilterChange,
  onQuickSwitchMode,
  onToggleCollapse,
}: StatsSidebarProps) {
  const [scanExpanded, setScanExpanded] = useState(true)
  const [organicExpanded, setOrganicExpanded] = useState(true)
  const [realizedExpanded, setRealizedExpanded] = useState(true)
  const [viewScope, setViewScope] = useState<'expedition' | 'allTime'>('expedition')

  const modeList: modes.Mode[] = modesState?.modes ?? []
  const travelMode = modeList.find((modeItem) => modeItem.name.toLowerCase() === 'travel')
  const activeModeId =
    modesState?.activeModeId || travelMode?.id || modeList[0]?.id || 0

  function getSelection(modeId: number): stats.ModeSelection {
    const found = filter.modes?.find((modeSelection) => modeSelection.modeId === modeId)
    if (found) return found
    return stats.ModeSelection.createFrom({ modeId, travel: true, discovery: true })
  }

  function setModeSelection(modeId: number, travel: boolean, discovery: boolean) {
    const allIds = modeList.map((mode) => mode.id)
    const updated = allIds.map((targetModeId) => {
      if (targetModeId === modeId) {
        return stats.ModeSelection.createFrom({ modeId: targetModeId, travel, discovery })
      }
      return getSelection(targetModeId)
    })
    const anyHidden = updated.some((selection) => !selection.travel || !selection.discovery)
    onFilterChange(
      stats.Filter.createFrom({
        enabled: anyHidden,
        modes: updated,
      }),
    )
  }

  function cycleModeSelection(modeId: number) {
    const selection = getSelection(modeId)
    if (selection.travel && selection.discovery) {
      // Full -> Partial (Scans and Exobio only)
      setModeSelection(modeId, false, true)
    } else if (!selection.travel && selection.discovery) {
      // Partial -> Excluded (None)
      setModeSelection(modeId, false, false)
    } else {
      // Excluded -> Full (All)
      setModeSelection(modeId, true, true)
    }
  }

  function resetAllModes() {
    onFilterChange(
      stats.Filter.createFrom({
        enabled: false,
        modes: modeList.map((modeItem) => ({
          modeId: modeItem.id,
          travel: true,
          discovery: true,
        })),
      }),
    )
  }

  const displayedMetrics: stats.Metrics = (() => {
    if (!sidebarStats) return new stats.Metrics()
    if (viewScope === 'allTime') {
      return sidebarStats.allJournals ?? new stats.Metrics()
    }
    return sidebarStats.expedition ?? new stats.Metrics()
  })()

  if (collapsed) {
    return (
      <aside className="w-8 shrink-0 h-full bg-[#090705] border-r border-[#3d2816] flex flex-col items-center pt-3 select-none">
        <button
          type="button"
          onClick={onToggleCollapse}
          title="Expand Telemetry Sidebar"
          className="h-6 w-6 flex items-center justify-center text-[#a6907c] hover:text-[#FF7100] bg-[#140e09] border border-[#3d2816] hover:bg-[#1f150d] cursor-pointer"
        >
          ▶
        </button>
      </aside>
    )
  }

  return (
    <aside className="w-[390px] shrink-0 h-full bg-[#090705] border-r border-[#3d2816] flex flex-col overflow-hidden">
      {/* Active Mode Switcher and Mode Filter */}
      <div className="p-3 border-b border-[#3d2816] bg-[#110c08] space-y-2">
        {/* Live Mode Quick Switcher */}
        <div className="flex items-center gap-2 text-xs">
          <span className="text-[10px] font-bold uppercase tracking-[0.12em] text-[#a6907c] shrink-0">
            Active Mode
          </span>
          <select
            value={activeModeId}
            onChange={(event) => onQuickSwitchMode(Number(event.target.value))}
            title="Current gameplay mode (logs newly recorded jumps and scans to this mode)"
            className="flex-1 bg-[#050506] border border-[#472e19] border-l-2 border-l-[#FF7100] px-2.5 py-1 text-xs font-bold uppercase tracking-wider text-[#F4ECE4] focus:border-[#FF7100] focus:outline-none cursor-pointer"
          >
            {modeList.map((mode) => (
              <option key={mode.id} value={mode.id} className="bg-[#0b0907] text-[#F4ECE4]">
                {mode.name}
              </option>
            ))}
          </select>
          {onToggleCollapse && (
            <button
              type="button"
              onClick={onToggleCollapse}
              title="Collapse Telemetry Sidebar"
              className="h-[26px] w-[26px] flex items-center justify-center text-[#a6907c] hover:text-[#FF7100] bg-[#140e09] border border-[#3d2816] hover:bg-[#1f150d] cursor-pointer shrink-0"
            >
              ◀
            </button>
          )}
        </div>

        {/* Mode Filter Checkboxes (Tri-state: All -> Scans Only -> Off) */}
        <div className="space-y-1.5 pt-1.5 border-t border-[#2e1f12]">
          <div className="flex items-center justify-between text-[10px] font-bold uppercase tracking-[0.12em] text-[#a6907c]">
            <span title="Filter sidebar metrics by which mode was active when events were logged">
              Telemetry Filter
            </span>
            {filter.enabled && (
              <button
                type="button"
                onClick={resetAllModes}
                className="text-[#FF7100] hover:underline cursor-pointer"
                title="Include all modes in sidebar metrics"
              >
                [Reset]
              </button>
            )}
          </div>

          <div className="grid grid-cols-2 gap-1.5">
            {modeList.map((mode) => {
              const selection = getSelection(mode.id)
              const isFull = selection.travel && selection.discovery
              const isPartial = !selection.travel && selection.discovery

              const titleText = isFull
                ? `${mode.name}: All telemetry included. Click to switch to Scans and Exobio only.`
                : isPartial
                  ? `${mode.name}: Scans and Exobio only (jumps/Ly hidden). Click to exclude.`
                  : `${mode.name}: Excluded from metrics. Click to include all.`

              return (
                <button
                  key={mode.id}
                  type="button"
                  onClick={() => cycleModeSelection(mode.id)}
                  title={titleText}
                  className={`flex items-center gap-2 px-2 py-1 border text-xs cursor-pointer transition select-none text-left ${
                    isFull
                      ? 'bg-[#18110b] border-[#472e19] border-l-2 border-l-[#FF7100] text-[#F4ECE4]'
                      : isPartial
                        ? 'bg-[#08151c] border-[#00D2FF]/60 border-l-2 border-l-[#00D2FF] text-[#F4ECE4]'
                        : 'bg-[#060504] border-[#24180e] text-[#6e5b4b] opacity-60 hover:opacity-100'
                  }`}
                >
                  {/* Tri-state indicator badge */}
                  <SquareCheckbox
                    checked={isFull ? true : isPartial ? 'partial' : false}
                    size="sm"
                  />

                  <span
                    className="w-1.5 h-3 shrink-0"
                    style={{ backgroundColor: mode.color }}
                  />
                  <span className="truncate font-bold uppercase tracking-wider text-[11px] flex-1">
                    {mode.name}
                  </span>
                </button>
              )
            })}
          </div>

          {/* Tri-state explanation */}
          <div className="pt-1.5 border-t border-[#24180e] text-[10px] text-[#a6907c] leading-tight space-y-1">
            <div className="flex items-center gap-2 font-mono-tabular text-[9px]">
              <span className="inline-flex items-center gap-1 text-[#FF7100] font-bold">
                <SquareCheckbox checked={true} size="xs" /> ALL DATA
              </span>
              <span>·</span>
              <span className="inline-flex items-center gap-1 text-[#00D2FF] font-bold">
                <SquareCheckbox checked="partial" size="xs" /> SCANS ONLY
              </span>
              <span>·</span>
              <span className="inline-flex items-center gap-1 text-[#6e5b4b]">
                <SquareCheckbox checked={false} size="xs" /> OFF
              </span>
            </div>
            <p className="text-[9px] text-[#6e5b4b] flex items-center gap-1 flex-wrap">
              <span>Partial</span>
              <SquareCheckbox checked="partial" size="xs" />
              <span>includes scans, mapping and exobio, excluding travel distance/jumps.</span>
            </p>
          </div>
        </div>
      </div>

      {/* Scope Switcher */}
      <div className="px-3.5 py-2 border-b border-[#3d2816] bg-[#0d0a07] flex items-center justify-between gap-2 text-xs">
        <div className="inline-flex border border-[#3d2816] bg-[#050506]">
          <button
            type="button"
            onClick={() => setViewScope('expedition')}
            title="Show metrics recorded during the active expedition"
            className={`px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.12em] transition cursor-pointer ${
              viewScope === 'expedition'
                ? 'bg-[#FF7100] text-[#050506]'
                : 'text-[#a6907c] hover:text-[#F4ECE4]'
            }`}
          >
            Expedition
          </button>
          <button
            type="button"
            onClick={() => setViewScope('allTime')}
            title="Show cumulative metrics across all loaded journal history"
            className={`px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.12em] transition cursor-pointer ${
              viewScope === 'allTime'
                ? 'bg-[#FF7100] text-[#050506]'
                : 'text-[#a6907c] hover:text-[#F4ECE4]'
            }`}
          >
            All Time
          </button>
        </div>

        {statsLoading && (
          <span className="font-mono-tabular text-[10px] uppercase tracking-wider text-[#FF7100] animate-pulse">
            SYNCING...
          </span>
        )}
      </div>

      {/* Tracked Metrics List (Row numbers removed) */}
      <div className="flex-1 overflow-y-auto p-3.5 space-y-2.5">
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
                    <span className="text-[#F4ECE4] font-bold text-right">{sidebarStats.careerStartGalactic}</span>
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
                    <span className="text-[#F4ECE4] font-bold text-right">{sidebarStats.startGalactic}</span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-[#a6907c]">
                      {sidebarStats.endIsNow ? 'CURRENT:' : 'END:'}
                    </span>
                    <span className="text-[#F4ECE4] font-bold text-right">{sidebarStats.endGalactic}</span>
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
          {/* Distance Travelled (Ly) — Total + Breakdown */}
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

          <StatRow
            label="Hyperspace Jumps"
            value={formatInt(displayedMetrics.jumps)}
          />
          <StatRow
            label="Jet Cone Boosts"
            value={formatInt(displayedMetrics.jetConeBoosts)}
          />
          <StatRow
            label="Premium Boosts"
            value={formatInt(displayedMetrics.premiumBoosts)}
          />
          <StatRow
            label="Standard Boosts"
            value={formatInt(displayedMetrics.standardBoosts)}
          />
          <StatRow label="Basic Boosts" value={formatInt(displayedMetrics.basicBoosts)} />
        </div>

        {/* Expedition / Personal Carrier Operations (Flat cockpit header, no gradient) */}
        <div className="border border-[#3d2816] bg-[#0e0a07] overflow-hidden">
          <div className="px-3 py-1.5 bg-[#140e09] border-b border-[#3d2816] text-[10px] font-extrabold uppercase tracking-[0.14em] text-[#F4ECE4] flex items-center justify-between">
            <span>
              {viewScope === 'allTime'
                ? 'Personal Carrier Operations'
                : 'Expedition Carrier Operations'}
            </span>
            <span className="text-[9px] text-[#a6907c] font-mono-tabular">
              {viewScope === 'allTime' ? 'OWNED VESSEL' : 'CARRIER LOG'}
            </span>
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

        {/* Exploration Cartographics and Exobiology */}
        <div className="border border-[#3d2816] bg-[#0e0a07] divide-y divide-[#24180e]">
          <StatRow
            label="Bodies Scanned"
            value={formatInt(displayedMetrics.bodiesScanned)}
            sub={
              displayedMetrics.firstDiscoveries > 0
                ? `${formatInt(displayedMetrics.firstDiscoveries)} undiscovered`
                : undefined
            }
            subClass="text-[#00D2FF]"
          />
          <StatRow
            label="Bodies Mapped"
            value={formatInt(displayedMetrics.bodiesMapped)}
            sub={
              displayedMetrics.firstMapped > 0
                ? `${formatInt(displayedMetrics.firstMapped)} first mapped`
                : undefined
            }
            subClass="text-[#00D2FF]"
          />

          {/* Cartographics Value — Astrometric Cyan */}
          <div className="p-2.5">
            <button
              type="button"
              onClick={() => setScanExpanded((previous) => !previous)}
              className="w-full flex items-center justify-between gap-2 text-left cursor-pointer"
            >
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="text-xs font-bold uppercase tracking-wider text-[#F4ECE4] truncate">
                  Cartographics Value
                </span>
                <span className="text-[10px] text-[#00D2FF] shrink-0">{scanExpanded ? '▾' : '▸'}</span>
              </div>
              <span className="font-mono-tabular text-xs font-extrabold text-[#00D2FF] whitespace-nowrap shrink-0">
                {formatCredits(displayedMetrics.scanValueTotal)}
              </span>
            </button>
            {scanExpanded && (
              <div className="mt-2 pl-3.5 space-y-1 border-l-2 border-[#00D2FF]/50 text-[11px]">
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">Base Scans and Mapping</span>
                  <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
                    {formatCredits(displayedMetrics.scanValueBase)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">First Discovered / Mapped</span>
                  <span className="font-mono-tabular text-[#00D2FF] font-bold whitespace-nowrap shrink-0">
                    +{formatCredits(displayedMetrics.scanValueBonus)}
                  </span>
                </div>
              </div>
            )}
          </div>

          {/* Organic Samples — Vista Genomics Biological Emerald */}
          <StatRow
            label="Organic Samples"
            value={formatInt(displayedMetrics.organicSamples)}
            sub={
              displayedMetrics.firstLogged > 0
                ? `${formatInt(displayedMetrics.firstLogged)} first-logged species`
                : undefined
            }
            subClass="text-[#00E5A3]"
          />

          {/* Exobiology Value — Vista Genomics Biological Emerald */}
          <div className="p-2.5">
            <button
              type="button"
              onClick={() => setOrganicExpanded((previous) => !previous)}
              className="w-full flex items-center justify-between gap-2 text-left cursor-pointer"
            >
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="text-xs font-bold uppercase tracking-wider text-[#F4ECE4] truncate">
                  Exobiology Value
                </span>
                <span className="text-[10px] text-[#00E5A3] shrink-0">{organicExpanded ? '▾' : '▸'}</span>
              </div>
              <span className="font-mono-tabular text-xs font-extrabold text-[#00E5A3] whitespace-nowrap shrink-0">
                {formatCredits(displayedMetrics.organicValueTotal)}
              </span>
            </button>
            {organicExpanded && (
              <div className="mt-2 pl-3.5 space-y-1 border-l-2 border-[#00E5A3]/50 text-[11px]">
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">Base Organic Value</span>
                  <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
                    {formatCredits(displayedMetrics.organicValueBase)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">First Logged Bonus</span>
                  <span className="font-mono-tabular text-[#00E5A3] font-bold whitespace-nowrap shrink-0">
                    +{formatCredits(displayedMetrics.organicValueBonus)}
                  </span>
                </div>
              </div>
            )}
          </div>

          {/* Realized Value — Orange Realized Profit & Loss Ledger */}
          <div className="p-2.5">
            <button
              type="button"
              onClick={() => setRealizedExpanded((previous) => !previous)}
              className="w-full flex items-center justify-between gap-2 text-left cursor-pointer"
            >
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="text-xs font-bold uppercase tracking-wider text-[#F4ECE4] truncate">
                  Realized Value
                </span>
                <span className="text-[10px] text-[#FF7100] shrink-0">{realizedExpanded ? '▾' : '▸'}</span>
              </div>
              <span className="font-mono-tabular text-xs font-extrabold text-[#FF7100] whitespace-nowrap shrink-0">
                {formatCredits(sidebarStats?.realization?.totalRealized)}
              </span>
            </button>
            {realizedExpanded && (
              <div className="mt-2 pl-3.5 space-y-1 border-l-2 border-[#FF7100]/50 text-[11px]">
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">Cartographics Lost (Death)</span>
                  <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                    {formatLoss(sidebarStats?.realization?.cartoLostDeath)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">Exobiology Lost (Death)</span>
                  <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                    {formatLoss(sidebarStats?.realization?.organicLostDeath)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">Cartographics Stolen</span>
                  <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                    {formatLoss(sidebarStats?.realization?.cartoStolen)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">Exobiology Stolen</span>
                  <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                    {formatLoss(sidebarStats?.realization?.organicStolen)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">NPC Crew Payouts</span>
                  <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                    {formatLoss(sidebarStats?.realization?.npcCrewLoss)}
                  </span>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </aside>
  )
})

function formatLoss(val?: number): string {
  if (!val || val === 0) {
    return '0 Cr'
  }
  return `-${formatCredits(val)}`
}

function StatRow({
  label,
  value,
  sub,
  subClass,
  accent,
  valueClass,
  actionLabel,
  onAction,
}: {
  label: string
  value: string
  sub?: string
  subClass?: string
  accent?: 'amber' | 'cyan'
  valueClass?: string
  actionLabel?: string
  onAction?: () => void
}) {
  return (
    <div className="px-2.5 py-2 flex items-center justify-between gap-2">
      <div className="min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-semibold text-[#F4ECE4] truncate">{label}</span>
          {actionLabel && onAction && (
            <button
              type="button"
              onClick={onAction}
              className="text-[9px] font-mono-tabular font-bold uppercase tracking-wider px-1.5 py-0.5 bg-[#16100b] hover:bg-[#FF7100] text-[#FF7100] hover:text-[#050506] border border-[#472e19] cursor-pointer"
            >
              {actionLabel}
            </button>
          )}
        </div>
        {sub && (
          <div className={`text-[10px] font-mono-tabular ${subClass ? subClass : 'text-[#a6907c]'}`}>
            {sub}
          </div>
        )}
      </div>
      <div
        className={`font-mono-tabular text-xs font-bold shrink-0 ${
          valueClass
            ? valueClass
            : accent === 'amber'
              ? 'text-[#FF7100]'
              : accent === 'cyan'
                ? 'text-[#00D2FF]'
                : 'text-[#F4ECE4]'
        }`}
      >
        {value}
      </div>
    </div>
  )
}
