import { SetCommanderEnabled } from '../../api/backend'
import { app } from '../../api/models'
import { SettingsCard } from '../../components/ui/SettingsCard'
import { SquareCheckbox } from '../../components/ui/SquareCheckbox'

/**
 * CommandersSectionProps defines the discovered commanders list and callbacks
 * for toggling commander inclusion in statistics and route tracking.
 */
export interface CommandersSectionProps {
  settings: app.Settings | null
  onSetError: (errorMessage: string) => void
}

/**
 * CommandersSection renders the list of discovered Elite Dangerous commanders
 * and allows enabling or excluding individual commanders from telemetry.
 */
export function CommandersSection({
  settings,
  onSetError,
}: CommandersSectionProps) {
  async function handleToggleCommander(commanderName: string, enabled: boolean) {
    onSetError('')
    try {
      await SetCommanderEnabled(commanderName, enabled)
    } catch (toggleError) {
      onSetError(String(toggleError))
    }
  }

  return (
    <SettingsCard
      title="Commanders"
      headerRight={
        <span className="text-[10px] text-ed-text-muted">
          Select commanders to track
        </span>
      }
    >
      {(settings?.commanders ?? []).length === 0 ? (
        <div className="text-ed-text-dim font-mono-tabular uppercase py-1 text-[10px]">
          No commanders detected yet.
        </div>
      ) : (
        <div className="divide-y divide-ed-border-subtle border border-ed-border bg-ed-void">
          {(settings?.commanders ?? []).map((commanderItem) => (
            <label
              key={commanderItem.name}
              className="px-3 py-2 flex items-center justify-between gap-3 cursor-pointer hover:bg-ed-control"
            >
              <div className="flex items-center gap-2.5 min-w-0">
                <SquareCheckbox
                  checked={commanderItem.enabled}
                  onChange={(checked) => handleToggleCommander(commanderItem.name, checked)}
                />
                <div className="min-w-0">
                  <span className="font-bold uppercase tracking-wider text-ed-text-main">
                    CMDR {commanderItem.name}
                  </span>
                </div>
              </div>
              <span className="font-mono-tabular text-xs font-bold text-ed-text-main shrink-0">
                {commanderItem.eventCount.toLocaleString()} events
              </span>
            </label>
          ))}
        </div>
      )}
    </SettingsCard>
  )
}

