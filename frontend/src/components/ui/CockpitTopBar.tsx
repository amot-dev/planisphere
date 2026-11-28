import { app } from '../../api/models'
import type { SyncPauseState } from '../../domain/syncState'
import { APP_PRODUCT_NAME, APP_TAGLINE } from '../../version'
import { CogIcon, SearchIcon } from './HudIcons'
import { SyncStatusButton } from './SyncStatusButton'
import { VersionIndicator } from './VersionIndicator'

/**
 * CockpitTopBarProps defines the sidebar collapse state, live ingestion
 * telemetry, and modal trigger callbacks for the top bar.
 */
export interface CockpitTopBarProps {
  sidebarCollapsed: boolean
  activeStatusMessage: string | null
  syncPauseState: SyncPauseState
  hasPendingSyncUpdates: boolean
  ingest: app.IngestStatus | null
  settings: app.Settings | null
  onToggleSyncPause: () => void
  onOpenExpeditionsModal: () => void
  onOpenSettingsModal: () => void
  onOpenSearchModal?: () => void
}

/**
 * CockpitTopBar renders the persistent 48px top cockpit header containing the
 * application emblem, version indicator, expedition and search triggers, and status strip.
 */
export function CockpitTopBar({
  sidebarCollapsed,
  activeStatusMessage,
  syncPauseState,
  hasPendingSyncUpdates,
  ingest,
  settings,
  onToggleSyncPause,
  onOpenExpeditionsModal,
  onOpenSettingsModal,
  onOpenSearchModal,
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
        {/* Expedition and search actions */}
        <div className="flex items-center gap-1.5 min-w-0">
          <button
            type="button"
            onClick={onOpenExpeditionsModal}
            className="ed-btn shrink-0 h-[30px]"
            title="Switch, create, clone, edit, or delete expeditions"
          >
            EXPEDITIONS
          </button>
          {onOpenSearchModal && (
            <button
              type="button"
              onClick={onOpenSearchModal}
              className="ed-btn shrink-0 h-[30px] px-2! xl:px-2.5!"
              title="Search waypoints and visit history"
              aria-label="Search waypoints and visit history"
            >
              <SearchIcon />
              <span className="hidden xl:inline">SEARCH</span>
            </button>
          )}
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

