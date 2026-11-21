import { app, expedition } from '../../api/models'
import type { SyncPauseState } from '../../features/sync/useSyncPause'
import { APP_PRODUCT_NAME, APP_TAGLINE } from '../../version'
import { CogIcon } from './HudIcons'
import { SyncStatusButton } from './SyncStatusButton'
import { VersionIndicator } from './VersionIndicator'

/**
 * CockpitTopBarProps defines the sidebar collapse state, expedition selector
 * data, live ingestion telemetry, and modal trigger callbacks for the top bar.
 */
export interface CockpitTopBarProps {
  sidebarCollapsed: boolean
  expeditions: expedition.Expedition[]
  activeExpeditionId: number
  activeStatusMessage: string | null
  syncPauseState: SyncPauseState
  hasPendingSyncUpdates: boolean
  ingest: app.IngestStatus | null
  settings: app.Settings | null
  onToggleSyncPause: () => void
  onSelectExpedition: (selectedExpeditionId: number) => void
  onOpenExpeditionsModal: () => void
  onOpenSettingsModal: () => void
}

/**
 * CockpitTopBar renders the persistent 48px top cockpit header containing the
 * application emblem, version indicator, expedition switcher, and status strip.
 */
export function CockpitTopBar({
  sidebarCollapsed,
  expeditions,
  activeExpeditionId,
  activeStatusMessage,
  syncPauseState,
  hasPendingSyncUpdates,
  ingest,
  settings,
  onToggleSyncPause,
  onSelectExpedition,
  onOpenExpeditionsModal,
  onOpenSettingsModal,
}: CockpitTopBarProps) {
  return (
    <header className="relative h-12 shrink-0 bg-ed-panel border-b border-ed-border border-t-2 border-t-ed-amber flex items-center">
      {/* Telemetry Sidebar Header Column */}
      <div
        className={`${
          sidebarCollapsed ? 'w-auto' : 'w-[390px]'
        } px-4 shrink-0 h-full flex items-center transition-all`}
      >
        <div className="flex items-center gap-2.5 shrink-0">
          <div className="w-2 h-5 bg-ed-amber" />
          <div className="leading-none">
            <div className="flex items-baseline gap-2">
              <span className="text-[11px] font-extrabold tracking-[0.18em] uppercase text-ed-amber leading-none">
                {APP_PRODUCT_NAME}
              </span>
              <VersionIndicator settings={settings} />
            </div>
            <div className="text-[9px] font-mono-tabular uppercase tracking-[0.14em] text-ed-text-muted mt-1">
              {APP_TAGLINE}
            </div>
          </div>
        </div>
      </div>

      {/* Expedition Planner Header Row (flex-1, px-5 aligned with ExpeditionPlanner section below) */}
      <div className="flex-1 h-full px-5 flex items-center justify-between gap-4 min-w-0">
        {/* Expedition selector */}
        <div className="flex items-center gap-1.5 min-w-0">
          <button
            type="button"
            onClick={onOpenExpeditionsModal}
            className="ed-btn shrink-0 h-[30px]"
            title="Create, clone, rename, or manage expeditions"
          >
            EXPEDITIONS
          </button>
          <select
            value={activeExpeditionId}
            onChange={(event) => onSelectExpedition(Number(event.target.value))}
            title="Select active expedition"
            className="h-[30px] bg-ed-header border border-ed-border-strong border-l-2 border-l-ed-amber px-2.5 text-xs font-bold uppercase tracking-wider text-ed-text-main focus:border-ed-amber focus:outline-none cursor-pointer max-w-[180px] sm:max-w-xs md:max-w-sm truncate"
          >
            {expeditions.map((expeditionItem) => (
              <option
                key={expeditionItem.id}
                value={expeditionItem.id}
                className="bg-ed-panel text-ed-text-main"
              >
                {expeditionItem.name} [{expeditionItem.status.toUpperCase()}]
              </option>
            ))}
          </select>
        </div>

        {/* Right Telemetry Strip */}
        <div className="flex items-center gap-2.5 text-xs shrink-0">
          {/* System Status Readout and Pause Toggle */}
          <SyncStatusButton
            activeStatusMessage={activeStatusMessage}
            syncPauseState={syncPauseState}
            hasPendingSyncUpdates={hasPendingSyncUpdates}
            onToggleSyncPause={onToggleSyncPause}
          />

          {ingest?.currentSystem && (
            <div
              title={`Current location: ${ingest.currentSystem}`}
              className="h-[30px] hidden lg:flex items-center gap-1.5 px-2.5 bg-ed-surface border border-ed-border-dim border-l-2 border-l-ed-amber"
            >
              <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted">
                LOC
              </span>
              <span className="font-mono-tabular font-bold uppercase tracking-wider text-ed-text-main">
                {ingest.currentSystem}
              </span>
            </div>
          )}

          {ingest?.currentShip && (
            <div
              title={`Current vessel: ${ingest.currentShip}`}
              className="h-[30px] hidden xl:flex items-center gap-1.5 px-2.5 bg-ed-surface border border-ed-border-dim border-l-2 border-l-ed-amber"
            >
              <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted">
                VESSEL
              </span>
              <span className="font-bold uppercase tracking-wider text-ed-text-main">
                {ingest.currentShip}
              </span>
            </div>
          )}

          <button
            type="button"
            onClick={onOpenSettingsModal}
            className="ed-btn h-[30px]"
          >
            <CogIcon />
            <span>CONFIG</span>
          </button>
        </div>
      </div>

      {/* Animated HUD activity scanline along bottom edge of header when busy */}
      {activeStatusMessage && (
        <div className="absolute bottom-0 left-0 right-0 h-[2px] bg-ed-elevated overflow-hidden">
          <div className="w-1/3 h-full bg-ed-amber ed-scan-bar" />
        </div>
      )}
    </header>
  )
}

