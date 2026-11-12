import { app } from '../../../wailsjs/go/models'
import { SquareCheckbox } from '../common/SquareCheckbox'

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
          <label className="font-bold uppercase tracking-wider text-[#FF7100]">
            Commander Selection
          </label>
          <span className="font-mono-tabular text-[10px] text-[#a6907c]">
            {localSettings?.commanders?.length ?? 0} DISCOVERED
          </span>
        </div>
        <p className="text-[11px] text-[#a6907c] leading-relaxed">
          Select which Commanders to include in tracking and telemetry:
        </p>

        {localSettings?.commanders && localSettings.commanders.length > 0 ? (
          <div className="max-h-32 overflow-y-auto space-y-1.5 pr-1">
            {localSettings.commanders.map((commander) => (
              <label
                key={commander.name}
                className="flex items-center justify-between p-2.5 border border-[#3d2816] bg-[#110c08] cursor-pointer hover:border-[#6e4726]"
              >
                <div className="flex items-center gap-2.5">
                  <SquareCheckbox
                    checked={commander.enabled}
                    onChange={(checked) => onToggleCommander(commander.name, checked)}
                  />
                  <div>
                    <div className="font-bold text-[#F4ECE4] tracking-wider">
                      CMDR {commander.name}
                    </div>
                    <div className="text-[10px] font-mono-tabular text-[#a6907c]">
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
          <div className="p-3 border border-[#3d2816] bg-[#110c08] text-[11px] text-[#a6907c] italic">
            No Commanders found in journals yet. Any Commander detected in future logs will be
            enabled automatically.
          </div>
        )}
      </div>

      {/* EDSM / GEC Online Sync */}
      <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-3">
        <label className="flex items-start gap-2.5 cursor-pointer">
          <SquareCheckbox
            checked={onlineLookups}
            onChange={(checked) => onToggleOnline(checked)}
            className="mt-0.5"
          />
          <div className="flex-1">
            <div className="font-bold uppercase tracking-wider text-[#F4ECE4]">
              EDSM and EDAstro GEC Online Sync
            </div>
            <p className="text-[11px] text-[#a6907c] mt-0.5 leading-relaxed">
              Resolves coordinates for unvisited waypoint systems and downloads the
              galactic POI catalog.
            </p>
          </div>
        </label>
        <div className="flex items-center justify-between pt-2 border-t border-[#2b1d11]">
          <div className="text-[11px] font-mono-tabular text-[#a6907c]">
            POI Catalog:{' '}
            <span className="text-[#F4ECE4] font-bold">
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
            {gecBusy ? 'Syncing…' : 'Sync Catalog Now'}
          </button>
        </div>
      </div>
    </div>
  )
}

