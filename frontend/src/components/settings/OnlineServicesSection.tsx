import { useState } from 'react'
import { RefreshGEC, SetOnlineLookups } from '../../../wailsjs/go/app/App'
import { app } from '../../../wailsjs/go/models'
import { SquareCheckbox } from '../common/SquareCheckbox'

/**
 * OnlineServicesSectionProps defines the settings and callbacks for toggling
 * online EDSM coordinate lookups and syncing the EDAstro GEC catalog.
 */
export interface OnlineServicesSectionProps {
  settings: app.Settings | null
  onRefresh: () => void
  onSetError: (errorMessage: string) => void
}

/**
 * OnlineServicesSection renders the online lookup toggle and Galactic
 * Exploration Catalog sync button inside Terminal Configuration.
 */
export function OnlineServicesSection({
  settings,
  onRefresh,
  onSetError,
}: OnlineServicesSectionProps) {
  const [online, setOnline] = useState(settings?.onlineLookups ?? true)
  const [gecBusy, setGecBusy] = useState(false)

  async function handleToggleOnline(nextEnabled: boolean) {
    setOnline(nextEnabled)
    await SetOnlineLookups(nextEnabled)
    onRefresh()
  }

  async function handleRefreshGec() {
    setGecBusy(true)
    onSetError('')
    try {
      await RefreshGEC()
      onRefresh()
    } catch (syncError) {
      onSetError(String(syncError))
    } finally {
      setGecBusy(false)
    }
  }

  return (
    <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2.5">
      <label className="flex items-center justify-between cursor-pointer">
        <div>
          <div className="font-bold uppercase tracking-wider text-[#F4ECE4]">
            Enable Online EDSM Coordinates and EDAstro GEC Sync
          </div>
          <div className="text-[11px] text-[#a6907c]">
            Fetches coordinates for unvisited systems and downloads catalog POIs
          </div>
        </div>
        <SquareCheckbox
          checked={online}
          onChange={(checked) => handleToggleOnline(checked)}
        />
      </label>

      <div className="flex items-center justify-between pt-2 border-t border-[#2e1f12]">
        <div className="text-[11px] text-[#a6907c]">
          Cached POIs:{' '}
          <span className="font-mono-tabular text-[#F4ECE4] font-bold">
            {settings?.gecCount ?? 0}
          </span>
        </div>
        <button
          type="button"
          disabled={gecBusy}
          onClick={handleRefreshGec}
          className="ed-btn"
        >
          {gecBusy ? 'Syncing…' : 'Sync GEC Catalog'}
        </button>
      </div>
    </div>
  )
}

