import { useCallback, useEffect, useRef, useState } from 'react'
import { CockpitTopBar } from './components/ui/CockpitTopBar'
import { ErrorBoundary } from './components/ui/ErrorBoundary'
import { TerminalBootScreen } from './components/ui/TerminalBootScreen'
import { normalizeDurationDisplayMode } from './domain/durationDisplay'
import { ExportExpeditionModal } from './features/export/ExportExpeditionModal'
import { ExpeditionPlanner } from './features/planner/ExpeditionPlanner'
import { ExpeditionManageModal } from './features/planner/lifecycle/ExpeditionManageModal'
import { StartExpeditionModal } from './features/planner/lifecycle/StartExpeditionModal'
import { WaypointSearchModal } from './features/planner/search/WaypointSearchModal'
import type { PlannerScrollTarget } from './features/planner/useInitialExpeditionScroll'
import { SettingsModal } from './features/settings/SettingsModal'
import { SetupWizardModal } from './features/setup/SetupWizard'
import { StatsSidebar } from './features/sidebar/StatsSidebar'
import { isExpeditionExportEnabled, useBackendSync } from './features/sync/useBackendSync'
import { useLocalStorageBoolean } from './hooks/useLocalStorageBoolean'

/**
 * App renders the root cockpit terminal shell, wiring backend state synchronization
 * into the top telemetry bar, statistics sidebar, expedition planner, and modal dialogs.
 */
export default function App() {
  const {
    booting,
    setBusyAction,
    settings,
    ingest,
    modesState,
    expeditions,
    activeExpeditionId,
    plan,
    setPlan,
    sidebarStats,
    sidebarViewScope,
    setSidebarViewScope,
    filter,
    setFilter,
    eddProgress,
    activeStatusMessage,
    syncPauseState,
    hasPendingSyncUpdates,
    isClockSuspended,
    handleToggleSyncPause,
    handleSelectExpedition,
    refreshAll,
    handleRefreshPlan,
    refreshSettings,
    refreshIngestStatus,
    setIncludeArchiveIngest,
  } = useBackendSync()

  const [activeModal, setActiveModal] = useState<
    null | 'start' | 'settings' | 'expeditions' | 'export'
  >(null)

  const isSetupIncomplete = Boolean(settings && !settings.setupCompleted)
  useEffect(() => {
    setIncludeArchiveIngest(activeModal === 'settings' || isSetupIncomplete)
  }, [activeModal, isSetupIncomplete, setIncludeArchiveIngest])

  const [sidebarCollapsed, toggleSidebarCollapsed] = useLocalStorageBoolean(
    'planisphere_sidebar_collapsed',
    false,
  )

  const [manualPlotterRequestCount, setManualPlotterRequestCount] = useState(0)
  const [carrierRouteOpen, setCarrierRouteOpen] = useState(false)
  const [searchModalQuery, setSearchModalQuery] = useState<string | null>(null)
  const [scrollTarget, setScrollTarget] = useState<PlannerScrollTarget | null>(null)
  const scrollTargetNonceRef = useRef(0)

  const handleLogCarrierJump = useCallback(() => {
    setManualPlotterRequestCount((requestCount) => requestCount + 1)
  }, [])

  const handleOpenStartModal = useCallback(() => setActiveModal('start'), [])
  const handleOpenExportModal = useCallback(() => setActiveModal('export'), [])
  const handleOpenSearchModal = useCallback(() => setSearchModalQuery(''), [])
  const handleCloseSearchModal = useCallback(() => setSearchModalQuery(null), [])
  const handleScrollTargetConsumed = useCallback(() => setScrollTarget(null), [])

  const handleManualSelectExpedition = useCallback(
    (selectedExpeditionId: number, options?: { persist?: boolean }) => {
      setScrollTarget(null)
      return handleSelectExpedition(selectedExpeditionId, options)
    },
    [handleSelectExpedition],
  )

  const handleNavigateToWaypoint = useCallback(
    (expeditionId: number, rowId: number) => {
      setSearchModalQuery(null)
      scrollTargetNonceRef.current += 1
      setScrollTarget({ expeditionId, rowId, nonce: scrollTargetNonceRef.current })
      if (expeditionId !== activeExpeditionId) {
        void handleSelectExpedition(expeditionId)
      }
    },
    [activeExpeditionId, handleSelectExpedition],
  )

  const handleSelectSearchExpedition = useCallback(
    (expeditionId: number) => {
      setSearchModalQuery(null)
      setScrollTarget(null)
      if (expeditionId !== activeExpeditionId) {
        void handleSelectExpedition(expeditionId)
      }
    },
    [activeExpeditionId, handleSelectExpedition],
  )

  const handleOpenSettingsModal = useCallback(() => {
    setIncludeArchiveIngest(true)
    void refreshSettings()
    void refreshIngestStatus(true)
    setActiveModal('settings')
  }, [refreshIngestStatus, refreshSettings, setIncludeArchiveIngest])

  const exportEnabled = isExpeditionExportEnabled(activeExpeditionId, plan, sidebarStats)
  const durationDisplayMode = normalizeDurationDisplayMode(settings?.durationDisplayMode)

  if (booting) {
    return <TerminalBootScreen />
  }

  // First-time setup: show the floating setup window before entering the main workspace
  if (settings && !settings.setupCompleted) {
    return (
      <div className="h-screen w-screen flex flex-col items-center justify-center cockpit-grid-bg text-ed-text-main overflow-hidden">
        <SetupWizardModal
          settings={settings}
          ingestStatus={ingest}
          eddProgress={eddProgress}
          onComplete={refreshAll}
        />
      </div>
    )
  }

  return (
    <div className="h-screen w-screen flex flex-col cockpit-grid-bg text-ed-text-main overflow-hidden">
      <CockpitTopBar
        sidebarCollapsed={sidebarCollapsed}
        activeStatusMessage={activeStatusMessage}
        syncPauseState={syncPauseState}
        hasPendingSyncUpdates={hasPendingSyncUpdates}
        ingest={ingest}
        settings={settings}
        onToggleSyncPause={handleToggleSyncPause}
        onOpenExpeditionsModal={() => setActiveModal('expeditions')}
        onOpenSettingsModal={handleOpenSettingsModal}
        onOpenSearchModal={handleOpenSearchModal}
      />

      {/* Main Split Workspace: Persistent Stats Sidebar + Expedition Planner */}
      <main className="flex-1 flex min-h-0 overflow-hidden">
        <ErrorBoundary fallbackTitle="Telemetry Sidebar Error">
          <StatsSidebar
            sidebarStats={sidebarStats}
            modesState={modesState}
            filter={filter}
            viewScope={sidebarViewScope}
            collapsed={sidebarCollapsed}
            carrierRouteOpen={carrierRouteOpen}
            clockSuspended={isClockSuspended}
            durationDisplayMode={durationDisplayMode}
            onFilterChange={setFilter}
            onViewScopeChange={setSidebarViewScope}
            onToggleCollapse={toggleSidebarCollapsed}
            onLogCarrierJump={handleLogCarrierJump}
          />
        </ErrorBoundary>

        <ErrorBoundary fallbackTitle="Expedition Planner Error">
          <ExpeditionPlanner
            plan={plan}
            dbPath={settings?.dbPath ?? ''}
            telemetryRevision={ingest?.telemetryRevision ?? 0}
            closedWindowRevision={ingest?.closedWindowRevision ?? 0}
            clockSuspended={isClockSuspended}
            durationDisplayMode={durationDisplayMode}
            externalLinkInara={settings?.externalLinkInara}
            externalLinkEdsm={settings?.externalLinkEdsm}
            externalLinkGec={settings?.externalLinkGec}
            manualPlotterRequestCount={manualPlotterRequestCount}
            exportEnabled={exportEnabled}
            hasModalOpen={activeModal !== null || searchModalQuery !== null}
            scrollTarget={scrollTarget}
            onScrollTargetConsumed={handleScrollTargetConsumed}
            onSearchSystemVisits={setSearchModalQuery}
            onPlanOptimisticUpdate={setPlan}
            onOpenStartModal={handleOpenStartModal}
            onOpenExportModal={handleOpenExportModal}
            onRefreshPlan={handleRefreshPlan}
            onBusyChange={setBusyAction}
            onCarrierRouteOpenChange={setCarrierRouteOpen}
          />
        </ErrorBoundary>
      </main>

      {/* Modal Dialogs */}
      {activeModal === 'start' && activeExpeditionId > 0 && (
        <StartExpeditionModal
          expeditionId={activeExpeditionId}
          modesList={plan?.modes ?? modesState?.modes ?? []}
          initialModeId={plan?.expedition?.activeModeId ?? 0}
          commanders={settings?.commanders ?? []}
          initialCommanderProfileId={plan?.expedition?.commanderProfileId}
          onClose={() => setActiveModal(null)}
        />
      )}

      {activeModal === 'settings' && (
        <SettingsModal
          settings={settings}
          ingestStatus={ingest}
          modesState={modesState}
          eddProgress={eddProgress}
          onClose={() => setActiveModal(null)}
        />
      )}

      {activeModal === 'expeditions' && (
        <ExpeditionManageModal
          expeditions={expeditions}
          activeId={activeExpeditionId}
          commanders={settings?.commanders ?? []}
          onSelect={handleManualSelectExpedition}
          onClose={() => setActiveModal(null)}
        />
      )}

      {activeModal === 'export' && activeExpeditionId > 0 && (
        <ExportExpeditionModal
          expeditionId={activeExpeditionId}
          durationDisplayMode={durationDisplayMode}
          onClose={() => setActiveModal(null)}
        />
      )}

      {searchModalQuery !== null && (
        <WaypointSearchModal
          initialQuery={searchModalQuery}
          activeExpeditionId={activeExpeditionId}
          onClose={handleCloseSearchModal}
          onNavigateToWaypoint={handleNavigateToWaypoint}
          onSelectExpedition={handleSelectSearchExpedition}
        />
      )}
    </div>
  )
}
