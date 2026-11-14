import { SetCommanderEnabled } from '../../../wailsjs/go/app/App'
import { app } from '../../../wailsjs/go/models'
import { SquareCheckbox } from '../common/SquareCheckbox'

/**
 * CommandersSectionProps defines the discovered commanders list and callbacks
 * for toggling commander inclusion in statistics and route tracking.
 */
export interface CommandersSectionProps {
  settings: app.Settings | null
  onRefresh: () => void
  onSetError: (errorMessage: string) => void
}

/**
 * CommandersSection renders the list of discovered Elite Dangerous commanders
 * and allows enabling or excluding individual commanders from telemetry.
 */
export function CommandersSection({
  settings,
  onRefresh,
  onSetError,
}: CommandersSectionProps) {
  async function handleToggleCommander(commanderName: string, enabled: boolean) {
    onSetError('')
    try {
      await SetCommanderEnabled(commanderName, enabled)
      onRefresh()
    } catch (toggleError) {
      onSetError(String(toggleError))
    }
  }

  return (
    <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
          Commanders
        </span>
        <span className="text-[10px] text-[#a6907c]">
          Select commanders to track
        </span>
      </div>
      {(settings?.commanders ?? []).length === 0 ? (
        <div className="text-[#6e5b4b] font-mono-tabular uppercase py-1 text-[10px]">
          No commanders detected yet.
        </div>
      ) : (
        <div className="divide-y divide-[#24180e] border border-[#3d2816] bg-[#050506]">
          {(settings?.commanders ?? []).map((commanderItem) => (
            <label
              key={commanderItem.name}
              className="px-3 py-2 flex items-center justify-between gap-3 cursor-pointer hover:bg-[#16100b]"
            >
              <div className="flex items-center gap-2.5 min-w-0">
                <SquareCheckbox
                  checked={commanderItem.enabled}
                  onChange={(checked) => handleToggleCommander(commanderItem.name, checked)}
                />
                <div className="min-w-0">
                  <span className="font-bold uppercase tracking-wider text-[#F4ECE4]">
                    CMDR {commanderItem.name}
                  </span>
                </div>
              </div>
              <span className="font-mono-tabular text-xs font-bold text-[#F4ECE4] shrink-0">
                {commanderItem.eventCount.toLocaleString()} events
              </span>
            </label>
          ))}
        </div>
      )}
    </div>
  )
}

