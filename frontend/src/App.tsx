import { useCallback, useState } from 'react'
import { CockpitTopBar } from './components/ui/CockpitTopBar'
import { ErrorBoundary } from './components/ui/ErrorBoundary'
import { TerminalBootScreen } from './components/ui/TerminalBootScreen'
import { ExpeditionPlanner } from './features/planner/ExpeditionPlanner'
import { ExpeditionManageModal } from './features/planner/lifecycle/ExpeditionManageModal'
import { StartExpeditionModal } from './features/planner/lifecycle/StartExpeditionModal'
import { SettingsModal } from './features/settings/SettingsModal'
import { SetupWizardModal } from './features/setup/SetupWizard'
import { StatsSidebar } from './features/sidebar/StatsSidebar'
import { useBackendSync } from './features/sync/useBackendSync'
import { useLocalStorageBoolean } from './hooks/useLocalStorageBoolean'

/**
 * App renders the root cockpit terminal shell, wiring backend state synchronization
 * into the top telemetry bar, statistics sidebar, expedition planner, and modal dialogs.
 */
export default function App() {
  const {
    booting,
    setBusyAction,
    statsLoading,
    settings,
    ingest,
    modesState,
    expeditions,
    activeExpeditionId,
    plan,
    setPlan,
    sidebarStats,
    filter,
    setFilter,
    eddProgress,
    activeStatusMessage,
    handleSelectExpedition,
    handleQuickSwitchMode,
    refreshAll,
    handleRefreshPlan,
    refreshSettings,
  } = useBackendSync()

  const [activeModal, setActiveModal] = useState<
    null | 'start' | 'settings' | 'expeditions' | 'setup'
  >(null)

  const [sidebarCollapsed, toggleSidebarCollapsed] = useLocalStorageBoolean(
    'planisphere_sidebar_collapsed',
    false,
  )

  const [manualPlotterRequestCount, setManualPlotterRequestCount] = useState(0)
  const [carrierRouteOpen, setCarrierRouteOpen] = useState(false)

  const handleLogCarrierJump = useCallback(() => {
    setManualPlotterRequestCount((requestCount) => requestCount + 1)
  }, [])

  const handleOpenStartModal = useCallback(() => setActiveModal('start'), [])

  const handleOpenSettingsModal = useCallback(() => {
    void refreshSettings()
    setActiveModal('settings')
  }, [refreshSettings])

  const archiveRevision = ingest
    ? `${ingest.eventsStored}:${ingest.firstEventAt}:${ingest.lastEventAt}`
    : ''

  if (booting) {
    return <TerminalBootScreen />
  }

  // First-time setup: show the floating setup window before entering the main workspace
  if (settings && !settings.setupCompleted) {
    return (
      <div className="h-screen w-screen flex flex-col items-center justify-center cockpit-grid-bg text-[#F4ECE4] overflow-hidden">
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
    <div className="h-screen w-screen flex flex-col cockpit-grid-bg text-[#F4ECE4] overflow-hidden">
      <CockpitTopBar
        sidebarCollapsed={sidebarCollapsed}
        expeditions={expeditions}
        activeExpeditionId={activeExpeditionId}
        activeStatusMessage={activeStatusMessage}
        ingest={ingest}
        settings={settings}
        onSelectExpedition={handleSelectExpedition}
        onOpenExpeditionsModal={() => setActiveModal('expeditions')}
        onOpenSettingsModal={handleOpenSettingsModal}
      />

      {/* Main Split Workspace: Persistent Stats Sidebar + Expedition Planner */}
      <main className="flex-1 flex min-h-0 overflow-hidden">
        <ErrorBoundary fallbackTitle="Telemetry Sidebar Error">
          <StatsSidebar
            sidebarStats={sidebarStats}
            statsLoading={statsLoading}
            modesState={modesState}
            filter={filter}
            collapsed={sidebarCollapsed}
            carrierRouteOpen={carrierRouteOpen}
            onFilterChange={setFilter}
            onQuickSwitchMode={handleQuickSwitchMode}
            onToggleCollapse={toggleSidebarCollapsed}
            onLogCarrierJump={handleLogCarrierJump}
          />
        </ErrorBoundary>

        <ErrorBoundary fallbackTitle="Expedition Planner Error">
          <ExpeditionPlanner
            plan={plan}
            archiveRevision={archiveRevision}
            externalLinkInara={settings?.externalLinkInara}
            externalLinkEdsm={settings?.externalLinkEdsm}
            externalLinkGec={settings?.externalLinkGec}
            manualPlotterRequestCount={manualPlotterRequestCount}
            onPlanOptimisticUpdate={setPlan}
            onOpenStartModal={handleOpenStartModal}
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
          onOpenSetupWizard={() => setActiveModal('setup')}
        />
      )}

      {activeModal === 'setup' && (
        <SetupWizardModal
          settings={settings}
          ingestStatus={ingest}
          eddProgress={eddProgress}
          onClose={() => setActiveModal(null)}
          onComplete={() => {
            setActiveModal(null)
            refreshAll()
          }}
        />
      )}

      {activeModal === 'expeditions' && (
        <ExpeditionManageModal
          expeditions={expeditions}
          activeId={activeExpeditionId}
          onSelect={handleSelectExpedition}
          onClose={() => setActiveModal(null)}
        />
      )}
    </div>
  )
}
