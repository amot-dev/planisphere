import { memo, useState } from 'react'
import { app, realization, stats } from '../../api/models'
import { CarrierConfigModal } from '../carrier/CarrierConfigModal'
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
  clockSuspended?: boolean
  onFilterChange: (next: stats.Filter) => void
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
  clockSuspended = false,
  onFilterChange,
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

  const displayedRealization: realization.Breakdown = (() => {
    if (!sidebarStats) return new realization.Breakdown()
    if (viewScope === 'allTime') {
      return sidebarStats.allJournalsRealization ?? new realization.Breakdown()
    }
    return sidebarStats.realization ?? new realization.Breakdown()
  })()

  if (collapsed) {
    return (
      <aside className="w-8 shrink-0 h-full bg-ed-rail border-r border-ed-border flex flex-col items-center pt-3 select-none">
        <button
          type="button"
          onClick={onToggleCollapse}
          title="Expand Telemetry Sidebar"
          className="h-6 w-6 flex items-center justify-center text-ed-text-muted hover:text-ed-amber bg-ed-header border border-ed-border hover:bg-ed-hover cursor-pointer"
        >
          <span>▶</span>
        </button>
      </aside>
    )
  }

  return (
    <aside className="w-[390px] shrink-0 h-full bg-ed-rail border-r border-ed-border flex flex-col overflow-hidden">
      <ModeFilterPanel
        modesState={modesState}
        filter={filter}
        onFilterChange={onFilterChange}
        onToggleCollapse={onToggleCollapse}
      />

      {/* Scope Switcher */}
      <div className="px-3.5 py-2 border-b border-ed-border bg-ed-card-alt flex items-center justify-between gap-2 text-xs">
        <div className="inline-flex border border-ed-border bg-ed-void">
          <button
            type="button"
            onClick={() => setViewScope('expedition')}
            title="Show metrics recorded during the active expedition"
            className={`px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.12em] transition cursor-pointer ${
              viewScope === 'expedition'
                ? 'bg-ed-amber text-ed-void'
                : 'text-ed-text-muted hover:text-ed-text-main'
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
                ? 'bg-ed-amber text-ed-void'
                : 'text-ed-text-muted hover:text-ed-text-main'
            }`}
          >
            All Time
          </button>
        </div>

        {statsLoading && (
          <span className="font-mono-tabular text-[10px] uppercase tracking-wider text-ed-amber animate-pulse">
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
          clockSuspended={clockSuspended}
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

