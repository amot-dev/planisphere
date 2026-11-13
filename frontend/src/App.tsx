import { useCallback, useRef, useState } from 'react'
import { CockpitTopBar } from './components/common/CockpitTopBar'
import { ErrorBoundary } from './components/common/ErrorBoundary'
import { TerminalBootScreen } from './components/common/TerminalBootScreen'
import { ExpeditionManageModal } from './components/planner/ExpeditionManageModal'
import { ExpeditionPlanner } from './components/planner/ExpeditionPlanner'
import { StartExpeditionModal } from './components/planner/StartExpeditionModal'
import { SettingsModal } from './components/settings/SettingsModal'
import { SetupWizardModal } from './components/setup/SetupWizard'
import { StatsSidebar } from './components/sidebar/StatsSidebar'
import { useBackendSync } from './hooks/useBackendSync'

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
    refreshPlanAndStats,
    handleRefreshPlan,
  } = useBackendSync()

  const [activeModal, setActiveModal] = useState<
    null | 'start' | 'settings' | 'expeditions' | 'setup'
  >(null)

  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(() => {
    try {
      const saved = localStorage.getItem('planisphere_sidebar_collapsed')
      return saved !== null ? saved === 'true' : false
    } catch {
      return false
    }
  })

  const toggleSidebarCollapsed = useCallback(() => {
    setSidebarCollapsed((prev) => {
      const next = !prev
      try {
        localStorage.setItem('planisphere_sidebar_collapsed', String(next))
      } catch {}
      return next
    })
  }, [])

  const openManualPlotterRef = useRef<(() => void) | null>(null)
  const [carrierRouteOpen, setCarrierRouteOpen] = useState(false)

  const handleRegisterManualPlotter = useCallback((openManualPlotter: () => void) => {
    openManualPlotterRef.current = openManualPlotter
  }, [])

  const handleLogCarrierJump = useCallback(() => {
    openManualPlotterRef.current?.()
  }, [])

  const handleOpenStartModal = useCallback(() => setActiveModal('start'), [])

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
        onOpenSettingsModal={() => setActiveModal('settings')}
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
            onPlanOptimisticUpdate={setPlan}
            onOpenStartModal={handleOpenStartModal}
            onRefreshPlan={handleRefreshPlan}
            onRefreshAll={refreshPlanAndStats}
            onBusyChange={setBusyAction}
            onRegisterManualPlotter={handleRegisterManualPlotter}
            onCarrierRouteOpenChange={setCarrierRouteOpen}
          />
        </ErrorBoundary>
      </main>

      {/* Modal Dialogs */}
      {activeModal === 'start' && activeExpeditionId > 0 && (
        <StartExpeditionModal
          expeditionId={activeExpeditionId}
          onClose={() => setActiveModal(null)}
          onStarted={refreshAll}
        />
      )}

      {activeModal === 'settings' && (
        <SettingsModal
          settings={settings}
          ingestStatus={ingest}
          modesState={modesState}
          eddProgress={eddProgress}
          onClose={() => setActiveModal(null)}
          onRefresh={refreshAll}
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
          onRefresh={refreshAll}
        />
      )}
    </div>
  )
}
