import { app } from '../../api/models'
import { SquareCheckbox } from '../../components/ui/SquareCheckbox'

/**
 * CommandersOnlineStepProps defines the discovered commander list, online
 * lookup configuration, GEC sync counters, and callbacks for step four of onboarding.
 */
export interface CommandersOnlineStepProps {
  localSettings: app.Settings | null
  onlineLookups: boolean
  gecBusy: boolean
  gecSyncedCount: number | null
  isBusy: boolean
  onToggleCommander: (commanderName: string, enabled: boolean) => void
  onToggleOnline: (enabled: boolean) => void
  onSyncGecNow: () => void
}

/**
 * CommandersOnlineStep renders commander tracking checkboxes and the EDSM /
 * EDAstro Galactic Exploration Catalog sync toggle in the setup wizard.
 */
export function CommandersOnlineStep({
  localSettings,
  onlineLookups,
  gecBusy,
  gecSyncedCount,
  isBusy,
  onToggleCommander,
  onToggleOnline,
  onSyncGecNow,
}: CommandersOnlineStepProps) {
  return (
    <div className="space-y-4">
      {/* Commander Selection (always shown even if 0 or 1 CMDR) */}
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <label className="font-bold uppercase tracking-wider text-ed-amber">
            Commander Selection
          </label>
          <span className="font-mono-tabular text-[10px] text-ed-text-muted">
            {localSettings?.commanders?.length ?? 0} DISCOVERED
          </span>
        </div>
        <p className="text-[11px] text-ed-text-muted leading-relaxed">
          Select which Commanders to include in tracking and telemetry:
        </p>

        {localSettings?.commanders && localSettings.commanders.length > 0 ? (
          <div className="max-h-32 overflow-y-auto space-y-1.5 pr-1">
            {localSettings.commanders.map((commander) => (
              <label
                key={commander.name}
                className={`flex items-center justify-between p-2.5 border border-ed-border bg-ed-card ${
                  isBusy ? 'cursor-not-allowed' : 'cursor-pointer hover:border-ed-border-strong'
                }`}
              >
                <div className="flex items-center gap-2.5">
                  <SquareCheckbox
                    checked={commander.enabled}
                    disabled={isBusy}
                    onChange={(checked) => onToggleCommander(commander.name, checked)}
                  />
                  <div>
                    <div className="font-bold text-ed-text-main tracking-wider">
                      CMDR {commander.name}
                    </div>
                    <div className="text-[10px] font-mono-tabular text-ed-text-muted">
                      {commander.eventCount.toLocaleString()} journal events
                    </div>
                  </div>
                </div>
                <span
                  className={
                    commander.enabled ? 'ed-badge ed-badge-active' : 'ed-badge ed-badge-planned'
                  }
                >
                  {commander.enabled ? 'TRACKING' : 'IGNORED'}
                </span>
              </label>
            ))}
          </div>
        ) : (
          <div className="p-3 border border-ed-border bg-ed-card text-[11px] text-ed-text-muted italic">
            No Commanders found in journals yet. Any Commander detected in future logs will be
            enabled automatically.
          </div>
        )}
      </div>

      {/* EDSM / GEC Online Sync */}
      <div className="p-3 border border-ed-border bg-ed-card space-y-3">
        <label
          className={`flex items-start gap-2.5 ${isBusy ? 'cursor-not-allowed' : 'cursor-pointer'}`}
        >
          <SquareCheckbox
            checked={onlineLookups}
            disabled={isBusy}
            onChange={(checked) => onToggleOnline(checked)}
            className="mt-0.5"
          />
          <div className="flex-1">
            <div className="font-bold uppercase tracking-wider text-ed-text-main">
              EDSM and EDAstro GEC Online Sync
            </div>
            <p className="text-[11px] text-ed-text-muted mt-0.5 leading-relaxed">
              Resolves coordinates for unvisited waypoint systems and downloads the
              galactic POI catalog.
            </p>
          </div>
        </label>
        <div className="flex items-center justify-between pt-2 border-t border-ed-border-dim">
          <div className="text-[11px] font-mono-tabular text-ed-text-muted">
            POI Catalog:{' '}
            <span className="text-ed-text-main font-bold">
              {(gecSyncedCount ?? localSettings?.gecCount ?? 0).toLocaleString()}
            </span>{' '}
            entries
            {localSettings?.gecFetchedAt
              ? ` (synced ${localSettings.gecFetchedAt.slice(0, 10)})`
              : ' (syncs automatically on finish)'}
          </div>
          <button
            type="button"
            disabled={!onlineLookups || isBusy}
            onClick={onSyncGecNow}
            className="ed-btn"
          >
            {gecBusy ? 'SYNCING...' : 'SYNC CATALOG NOW'}
          </button>
        </div>
      </div>
    </div>
  )
}

