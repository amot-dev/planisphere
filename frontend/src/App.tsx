import { useCallback, useEffect, useRef, useState } from 'react'
import {
  APP_PRODUCT_NAME,
  APP_TAGLINE,
  APP_VERSION,
} from './version'
import {
  CreateExpedition,
  GetIngestStatus,
  GetModesState,
  GetPlan,
  GetSettings,
  GetSidebarStats,
  ListExpeditions,
  RescanJournals,
  SetActiveExpedition,
  SwitchMode,
} from '../wailsjs/go/app/App'
import { app, expedition, stats } from '../wailsjs/go/models'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { ExpeditionPlanner } from './components/ExpeditionPlanner'
import {
  ExpeditionManageModal,
  SettingsModal,
  StartExpeditionModal,
} from './components/Modals'
import { SetupWizardModal } from './components/SetupWizard'
import { StatsSidebar } from './components/StatsSidebar'
import { GalacticClockBadge } from './components/PlannerWidgets'
import { ErrorBoundary } from './components/ErrorBoundary'
import { nowGalacticTime } from './utils/galacticTime'

export default function App() {
  const [booting, setBooting] = useState<boolean>(true)
  const [busyAction, setBusyAction] = useState<string | null>('INITIALIZING TERMINAL')
  const [statsLoading, setStatsLoading] = useState<boolean>(false)

  const [settings, setSettings] = useState<app.Settings | null>(null)
  const [ingest, setIngest] = useState<app.IngestStatus | null>(null)
  const [modesState, setModesState] = useState<app.ModesState | null>(null)
  const [expeditions, setExpeditions] = useState<expedition.Expedition[]>([])
  const [activeExpId, setActiveExpId] = useState<number>(0)
  const [plan, setPlan] = useState<expedition.PlanView | null>(null)
  const [sidebarStats, setSidebarStats] = useState<app.SidebarStats | null>(null)
  const [filter, setFilter] = useState<stats.Filter>(() =>
    stats.Filter.createFrom({ enabled: false, modes: [] }),
  )

  // Active modal dialog
  const [activeModal, setActiveModal] = useState<
    null | 'start' | 'settings' | 'expeditions' | 'setup'
  >(null)
  const [eddProgress, setEddProgress] = useState<{
    active: boolean
    stage: string
    current: number
    total: number
    percent: number
    files: number
  } | null>(null)

  const activeExpIdRef = useRef(0)
  const planReqSeq = useRef(0)
  const statsReqSeq = useRef(0)
  const filterRef = useRef(filter)

  useEffect(() => {
    activeExpIdRef.current = activeExpId
  }, [activeExpId])

  useEffect(() => {
    filterRef.current = filter
  }, [filter])

  const loadCoreState = useCallback(async () => {
    try {
      const [st, ing, mState, expList] = await Promise.all([
        GetSettings(),
        GetIngestStatus(),
        GetModesState(),
        ListExpeditions(),
      ])
      setSettings(st)
      setIngest(ing)
      setModesState(mState)

      let list = expList
      if (!list || list.length === 0) {
        const seeded = await CreateExpedition(
          'Deep Space Expedition I',
          'Deep Space Exploration & Scientific Survey Route',
        )
        list = [seeded]
      }
      setExpeditions(list)

      let chosenId = 0
      const current = activeExpIdRef.current
      if (current && list.some((e) => e.id === current)) {
        chosenId = current
      } else if (st.activeExpeditionId && list.some((e) => e.id === st.activeExpeditionId)) {
        chosenId = st.activeExpeditionId
      } else if (list.length > 0) {
        chosenId = list[0].id
      }

      if (chosenId > 0) {
        setActiveExpId(chosenId)
        activeExpIdRef.current = chosenId
        try {
          const pv = await GetPlan(chosenId)
          if (pv) {
            setPlan(pv)
          }
        } catch (planErr) {
          console.error('Failed to preload initial plan:', planErr)
        }
      }
    } catch (err) {
      console.error('Failed to load core state:', err)
    }
  }, [])

  // Fast plan loader (< 5ms) — never blocked by sidebar telemetry computation
  const loadPlanOnly = useCallback(async (expId: number) => {
    if (!expId) return
    const seq = ++planReqSeq.current
    try {
      const pv = await GetPlan(expId)
      if (seq === planReqSeq.current && pv?.expedition?.id === activeExpIdRef.current) {
        setPlan(pv)
      }
    } catch (err) {
      console.error('Failed to load expedition plan:', err)
    }
  }, [])

  // Asynchronous sidebar telemetry loader
  const loadStatsOnly = useCallback(async (expId: number, currentFilter: stats.Filter) => {
    if (!expId) return
    const seq = ++statsReqSeq.current
    setStatsLoading(true)
    try {
      const sb = await GetSidebarStats(expId, currentFilter)
      if (seq === statsReqSeq.current && activeExpIdRef.current === expId) {
        setSidebarStats(sb)
      }
    } catch (err) {
      console.error('Failed to load sidebar stats:', err)
    } finally {
      if (seq === statsReqSeq.current && activeExpIdRef.current === expId) {
        setStatsLoading(false)
      }
    }
  }, [])

  // Initial boot sequence
  useEffect(() => {
    let mounted = true
    setBusyAction('INITIALIZING TERMINAL')
    loadCoreState().finally(() => {
      if (mounted) {
        setBooting(false)
        setBusyAction(null)
      }
    })
    return () => {
      mounted = false
    }
  }, [loadCoreState])

  // Load plan immediately when expedition changes
  useEffect(() => {
    if (activeExpId > 0) {
      loadPlanOnly(activeExpId)
    }
  }, [activeExpId, loadPlanOnly])

  // Reload sidebar stats when expedition or filter changes
  useEffect(() => {
    if (activeExpId > 0) {
      loadStatsOnly(activeExpId, filter)
    }
  }, [activeExpId, filter, loadStatsOnly])

  // Subscribe to Wails backend events (live fsnotify journal tailing, coordinate resolution, etc.)
  useEffect(() => {
    if (typeof window === 'undefined' || !(window as any).runtime?.EventsOnMultiple) {
      return
    }

    const offChanged = EventsOn('data:changed', (payload?: { kind?: string; expId?: number }) => {
      const kind = payload?.kind ?? ''
      const currentActive = activeExpIdRef.current

      if (payload?.expId && currentActive > 0 && payload.expId !== currentActive) {
        ListExpeditions().then((l) => l && setExpeditions(l)).catch(console.error)
        return
      }

      if (kind === 'plan' || kind === 'coords') {
        if (currentActive > 0) {
          loadPlanOnly(currentActive)
        }
        return
      }

      loadCoreState()
      if (currentActive > 0) {
        loadPlanOnly(currentActive)
        loadStatsOnly(currentActive, filterRef.current)
      }
    })
    const offIngest = EventsOn('ingest:status', () => {
      GetIngestStatus().then(setIngest).catch(console.error)
    })
    const offEdd = EventsOn('edd:progress', (p) => {
      setEddProgress(p)
    })
    return () => {
      offChanged()
      offIngest()
      offEdd()
    }
  }, [loadCoreState, loadPlanOnly, loadStatsOnly])

  const handleSelectExpedition = useCallback(
    async (id: number) => {
      if (!id) return
      setActiveExpId(id)
      activeExpIdRef.current = id
      try {
        await SetActiveExpedition(id)
      } catch (err) {
        console.error('Failed to set active expedition:', err)
      }
      const [list] = await Promise.all([
        ListExpeditions(),
        loadPlanOnly(id),
        loadStatsOnly(id, filter),
      ])
      if (list && list.length > 0) {
        setExpeditions(list)
      }
    },
    [filter, loadPlanOnly, loadStatsOnly],
  )

  async function handleQuickSwitchMode(modeId: number) {
    setBusyAction('SWITCHING ENVIRONMENT MODE')
    try {
      const next = await SwitchMode(modeId)
      setModesState(next)
      if (activeExpId > 0) {
        loadPlanOnly(activeExpId)
        loadStatsOnly(activeExpId, filter)
      }
    } finally {
      setBusyAction(null)
    }
  }

  async function handleRescanJournals() {
    setBusyAction('SCANNING JOURNAL ARCHIVE')
    try {
      await RescanJournals()
      await loadCoreState()
      if (activeExpId > 0) {
        await loadPlanOnly(activeExpId)
        loadStatsOnly(activeExpId, filter)
      }
    } finally {
      setBusyAction(null)
    }
  }

  const refreshAll = useCallback(() => {
    loadCoreState()
    if (activeExpIdRef.current > 0) {
      loadPlanOnly(activeExpIdRef.current)
      loadStatsOnly(activeExpIdRef.current, filter)
    }
  }, [loadCoreState, loadPlanOnly, loadStatsOnly, filter])

  const refreshPlanAndStats = useCallback(() => {
    if (activeExpIdRef.current > 0) {
      loadPlanOnly(activeExpIdRef.current)
      loadStatsOnly(activeExpIdRef.current, filter)
    }
  }, [loadPlanOnly, loadStatsOnly, filter])

  const handleOpenStartModal = useCallback(() => setActiveModal('start'), [])
  const handleRefreshPlan = useCallback(() => {
    if (activeExpIdRef.current > 0) {
      loadPlanOnly(activeExpIdRef.current)
    }
  }, [loadPlanOnly])
  const handleRefreshStats = useCallback(() => {
    if (activeExpIdRef.current > 0) {
      loadStatsOnly(activeExpIdRef.current, filter)
    }
  }, [loadStatsOnly, filter])

  const activeStatusMessage =
    busyAction ||
    (eddProgress?.active ? `EDD IMPORT ${eddProgress.percent}%` : null) ||
    (ingest?.scanning ? 'INGESTING JOURNALS' : null) ||
    (statsLoading ? 'COMPUTING TELEMETRY' : null)

  if (booting && !plan) {
    return (
      <div className="h-screen w-screen flex flex-col items-center justify-center cockpit-grid-bg text-[#F4ECE4] select-none p-8">
        <div className="w-full max-w-md border border-[#3d2816] border-t-2 border-t-[#FF7100] bg-[#0b0907] p-6 space-y-4">
          <div className="flex items-center justify-between">
            <span className="text-xs font-extrabold tracking-[0.2em] uppercase text-[#FF7100]">
              {APP_PRODUCT_NAME}
            </span>
            <span className="font-mono-tabular text-[11px] text-[#00D2FF]">TERMINAL v{APP_VERSION}</span>
          </div>
          <div className="space-y-1">
            <div className="text-sm font-bold uppercase tracking-wider text-[#F4ECE4]">
              Initializing Exploration Terminal…
            </div>
            <div className="text-xs text-[#a6907c]">
              Mounting journal archive &amp; stellar coordinate index
            </div>
          </div>
          <div className="w-full h-1.5 bg-[#16100b] border border-[#3d2816] overflow-hidden">
            <div className="w-2/5 h-full bg-[#FF7100] ed-scan-bar" />
          </div>
          <div className="flex items-center justify-between font-mono-tabular text-[10px] text-[#a6907c]">
            <span>STATUS: SYNCHRONIZING</span>
            <span className="text-[#00D2FF]">{nowGalacticTime()}</span>
          </div>
        </div>
      </div>
    )
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
      {/* Top Cockpit Telemetry Header */}
      <header className="relative h-12 shrink-0 px-4 bg-[#0b0907] border-b border-[#3d2816] border-t-2 border-t-[#FF7100] flex items-center justify-between gap-4">
        <div className="flex items-center gap-3 min-w-0">
          {/* ED Emblem + Title */}
          <div className="flex items-center gap-2.5 shrink-0">
            <div className="w-2 h-5 bg-[#FF7100]" />
            <div className="leading-none">
              <div className="text-[11px] font-extrabold tracking-[0.18em] uppercase text-[#FF7100]">
                {APP_PRODUCT_NAME}
              </div>
              <div className="text-[9px] font-mono-tabular uppercase tracking-[0.14em] text-[#a6907c] mt-0.5">
                {APP_TAGLINE}
              </div>
            </div>
          </div>

          <div className="h-5 w-px bg-[#2b1d11] shrink-0" />

          {/* Expedition selector */}
          <div className="flex items-center gap-1.5 min-w-0">
            <span className="text-[10px] font-bold uppercase tracking-[0.12em] text-[#a6907c] shrink-0">
              Expedition
            </span>
            <select
              value={activeExpId}
              onChange={(e) => handleSelectExpedition(Number(e.target.value))}
              title="Select active expedition flight plan"
              className="bg-[#140e09] border border-[#472e19] border-l-2 border-l-[#FF7100] px-2.5 py-1 text-xs font-bold uppercase tracking-wider text-[#F4ECE4] focus:border-[#FF7100] focus:outline-none cursor-pointer"
            >
              {expeditions.map((e) => (
                <option key={e.id} value={e.id} className="bg-[#0b0907] text-[#F4ECE4]">
                  {e.name} [{e.status.toUpperCase()}]
                </option>
              ))}
            </select>
            <button
              type="button"
              onClick={() => setActiveModal('expeditions')}
              className="ed-btn"
              title="Create, clone, rename, or manage expedition plans"
            >
              Plans
            </button>
          </div>
        </div>

        {/* Right Telemetry Strip */}
        <div className="flex items-center gap-2.5 text-xs shrink-0">
          {/* System Status Readout */}
          <div
            className={`flex items-center gap-2 px-2.5 py-1 border font-mono-tabular text-[11px] uppercase tracking-wider ${
              activeStatusMessage
                ? 'bg-[#211307] border-[#FF7100] text-[#FF7100]'
                : 'bg-[#081318] border-[#16485c] text-[#00D2FF]'
            }`}
            title={
              activeStatusMessage
                ? `Background task: ${activeStatusMessage}`
                : 'Terminal idle and ready for input'
            }
          >
            <span
              className={`w-2 h-2 ${
                activeStatusMessage ? 'bg-[#FF7100] animate-ping' : 'bg-[#00D2FF]'
              }`}
            />
            <span className="font-bold">
              {activeStatusMessage ? activeStatusMessage : 'READY'}
            </span>
          </div>

          {ingest?.currentSystem && (
            <div
              title={`Current location: ${ingest.currentSystem}`}
              className="hidden lg:flex items-center gap-1.5 px-2.5 py-1 bg-[#120d09] border border-[#2e1f12] border-l-2 border-l-[#FF7100]"
            >
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c]">
                LOC
              </span>
              <span className="font-mono-tabular font-bold uppercase tracking-wider text-[#F4ECE4]">
                {ingest.currentSystem}
              </span>
            </div>
          )}

          {ingest?.currentShip && (
            <div
              title={`Current vessel: ${ingest.currentShip}`}
              className="hidden xl:flex items-center gap-1.5 px-2.5 py-1 bg-[#120d09] border border-[#2e1f12] border-l-2 border-l-[#FF7100]"
            >
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c]">
                VESSEL
              </span>
              <span className="font-bold uppercase tracking-wider text-[#F4ECE4]">
                {ingest.currentShip}
              </span>
            </div>
          )}

          <button
            type="button"
            onClick={handleRescanJournals}
            title={`${ingest?.eventsStored ?? 0} events stored across ${
              ingest?.filesTracked ?? 0
            } files. Click to rescan journals.`}
            className="flex items-center gap-1.5 px-2.5 py-1 bg-[#120d09] hover:bg-[#24180e] border border-[#3d2816] cursor-pointer transition"
          >
            <span
              className={`w-1.5 h-3 ${
                ingest?.scanning
                  ? 'bg-[#FF7100] animate-pulse'
                  : ingest?.watcherActive
                    ? 'bg-[#00D2FF]'
                    : 'bg-[#a6907c]'
              }`}
            />
            <span className="font-mono-tabular text-[11px] text-[#F4ECE4]">
              {(ingest?.eventsStored ?? 0).toLocaleString()} EV
            </span>
          </button>

          {/* In-Universe Galactic Standard Time (+1286 Years) */}
          <GalacticClockBadge />

          <button
            type="button"
            onClick={() => setActiveModal('settings')}
            className="ed-btn"
            title="Open terminal settings, environment modes, and journal configuration"
          >
            <svg className="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
              <path
                fillRule="evenodd"
                d="M11.49 3.17c-.38-1.56-2.6-1.56-2.98 0a1.532 1.532 0 01-2.286.948c-1.372-.836-2.942.734-2.106 2.106.54.886.061 2.042-.947 2.287-1.561.379-1.561 2.6 0 2.978a1.532 1.532 0 01.947 2.287c-.836 1.372.734 2.942 2.106 2.106a1.532 1.532 0 012.287.947c.379 1.561 2.6 1.561 2.978 0a1.533 1.533 0 012.287-.947c1.372.836 2.942-.734 2.106-2.106a1.533 1.533 0 01.947-2.287c1.561-.379 1.561-2.6 0-2.978a1.532 1.532 0 01-.947-2.287c.836-1.372-.734-2.942-2.106-2.106a1.532 1.532 0 01-2.287-.947zM10 13a3 3 0 100-6 3 3 0 000 6z"
                clipRule="evenodd"
              />
            </svg>
            <span>Config</span>
          </button>
        </div>

        {/* Animated HUD activity scanline along bottom edge of header when busy */}
        {activeStatusMessage && (
          <div className="absolute bottom-0 left-0 right-0 h-[2px] bg-[#1a1109] overflow-hidden">
            <div className="w-1/3 h-full bg-[#FF7100] ed-scan-bar" />
          </div>
        )}
      </header>

      {/* Main Split Workspace: Persistent Stats Sidebar + Expedition Planner */}
      <main className="flex-1 flex min-h-0 overflow-hidden">
        <ErrorBoundary fallbackTitle="Telemetry Sidebar Error">
          <StatsSidebar
            sidebarStats={sidebarStats}
            statsLoading={statsLoading}
            modesState={modesState}
            filter={filter}
            onFilterChange={setFilter}
            onQuickSwitchMode={handleQuickSwitchMode}
            onRefreshStats={handleRefreshStats}
          />
        </ErrorBoundary>

        <ErrorBoundary fallbackTitle="Expedition Planner Error">
          <ExpeditionPlanner
            plan={plan}
            onPlanOptimisticUpdate={setPlan}
            onOpenStartModal={handleOpenStartModal}
            onRefreshPlan={handleRefreshPlan}
            onRefreshAll={refreshPlanAndStats}
            onBusyChange={setBusyAction}
          />
        </ErrorBoundary>
      </main>

      {/* Modal Dialogs */}
      {activeModal === 'start' && activeExpId > 0 && (
        <StartExpeditionModal
          expeditionId={activeExpId}
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
          activeId={activeExpId}
          onSelect={handleSelectExpedition}
          onClose={() => setActiveModal(null)}
          onRefresh={refreshAll}
        />
      )}
    </div>
  )
}
