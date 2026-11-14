import { memo, useState } from 'react'
import { app, stats } from '../../../wailsjs/go/models'
import { CarrierConfigModal } from './CarrierConfigModal'
import { ExplorationValuationSection } from './ExplorationValuationSection'
import { ModeFilterPanel } from './ModeFilterPanel'
import { NavigationTelemetrySection } from './NavigationTelemetrySection'

/**
 * StatsSidebarProps defines the telemetry data, mode filter state, and user
 * action callbacks for the expedition and career statistics sidebar.
 */
export interface StatsSidebarProps {
  sidebarStats: app.SidebarStats | null
  statsLoading: boolean
  modesState: app.ModesState | null
  filter: stats.Filter
  collapsed?: boolean
  carrierRouteOpen?: boolean
  onFilterChange: (next: stats.Filter) => void
  onQuickSwitchMode: (modeId: number) => void
  onToggleCollapse?: () => void
  onLogCarrierJump?: () => void
}

/**
 * StatsSidebar renders the persistent left cockpit telemetry panel with mode
 * filtering, Expedition / All Time scope switching, navigation stats, and valuations.
 */
export const StatsSidebar = memo(function StatsSidebar({
  sidebarStats,
  statsLoading,
  modesState,
  filter,
  collapsed = false,
  carrierRouteOpen = false,
  onFilterChange,
  onQuickSwitchMode,
  onToggleCollapse,
  onLogCarrierJump,
}: StatsSidebarProps) {
  const [viewScope, setViewScope] = useState<'expedition' | 'allTime'>('expedition')
  const [flightOpsExpanded, setFlightOpsExpanded] = useState(true)
  const [carrierOpsExpanded, setCarrierOpsExpanded] = useState(true)
  const [carrierCostExpanded, setCarrierCostExpanded] = useState(true)
  const [scienceOpsExpanded, setScienceOpsExpanded] = useState(true)
  const [scanExpanded, setScanExpanded] = useState(true)
  const [organicExpanded, setOrganicExpanded] = useState(true)
  const [realizedExpanded, setRealizedExpanded] = useState(true)
  const [carrierConfigOpen, setCarrierConfigOpen] = useState(false)

  const displayedMetrics: stats.Metrics = (() => {
    if (!sidebarStats) return new stats.Metrics()
    if (viewScope === 'allTime') {
      return sidebarStats.allJournals ?? new stats.Metrics()
    }
    return sidebarStats.expedition ?? new stats.Metrics()
  })()

  const displayedRealization: stats.RealizationBreakdown = (() => {
    if (!sidebarStats) return new stats.RealizationBreakdown()
    if (viewScope === 'allTime') {
      return sidebarStats.allJournalsRealization ?? new stats.RealizationBreakdown()
    }
    return sidebarStats.realization ?? new stats.RealizationBreakdown()
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
          <span>▶</span>
        </button>
      </aside>
    )
  }

  return (
    <aside className="w-[390px] shrink-0 h-full bg-[#090705] border-r border-[#3d2816] flex flex-col overflow-hidden">
      <ModeFilterPanel
        modesState={modesState}
        filter={filter}
        onFilterChange={onFilterChange}
        onQuickSwitchMode={onQuickSwitchMode}
        onToggleCollapse={onToggleCollapse}
      />

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

      {/* Tracked Metrics List */}
      <div className="flex-1 overflow-y-auto p-3.5 space-y-2.5">
        <NavigationTelemetrySection
          viewScope={viewScope}
          sidebarStats={sidebarStats}
          displayedMetrics={displayedMetrics}
          carrierRouteOpen={carrierRouteOpen}
          flightOpsExpanded={flightOpsExpanded}
          carrierOpsExpanded={carrierOpsExpanded}
          carrierCostExpanded={carrierCostExpanded}
          onToggleFlightOpsExpanded={() => setFlightOpsExpanded((previous) => !previous)}
          onToggleCarrierOpsExpanded={() => setCarrierOpsExpanded((previous) => !previous)}
          onToggleCarrierCostExpanded={() => setCarrierCostExpanded((previous) => !previous)}
          onLogCarrierJump={onLogCarrierJump}
          onOpenCarrierConfig={() => setCarrierConfigOpen(true)}
        />

        <ExplorationValuationSection
          displayedMetrics={displayedMetrics}
          displayedRealization={displayedRealization}
          scienceOpsExpanded={scienceOpsExpanded}
          scanExpanded={scanExpanded}
          organicExpanded={organicExpanded}
          realizedExpanded={realizedExpanded}
          onToggleScienceOpsExpanded={() => setScienceOpsExpanded((previous) => !previous)}
          onToggleScanExpanded={() => setScanExpanded((previous) => !previous)}
          onToggleOrganicExpanded={() => setOrganicExpanded((previous) => !previous)}
          onToggleRealizedExpanded={() => setRealizedExpanded((previous) => !previous)}
        />
      </div>

      {carrierConfigOpen && sidebarStats && sidebarStats.expeditionId > 0 && (
        <CarrierConfigModal
          expeditionId={sidebarStats.expeditionId}
          initialCarrierType={
            sidebarStats.carrierCost?.carrierType === 'squadron' ? 'squadron' : 'player'
          }
          initialWeeklyUpkeep={sidebarStats.carrierCost?.weeklyUpkeep ?? 0}
          onClose={() => setCarrierConfigOpen(false)}
        />
      )}
    </aside>
  )
})

