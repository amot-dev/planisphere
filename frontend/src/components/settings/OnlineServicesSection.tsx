import { useEffect, useState } from 'react'
import { RefreshGEC, SetOnlineLookups } from '../../api/backend'
import { app } from '../../api/models'
import { SquareCheckbox } from '../common/SquareCheckbox'

/**
 * OnlineServicesSectionProps defines the settings and callbacks for toggling
 * online EDSM coordinate lookups and syncing the EDAstro GEC catalog.
 */
export interface OnlineServicesSectionProps {
  settings: app.Settings | null
  onSetError: (errorMessage: string) => void
}

/**
 * OnlineServicesSection renders the online lookup toggle and Galactic
 * Exploration Catalog sync button inside Terminal Configuration.
 */
export function OnlineServicesSection({
  settings,
  onSetError,
}: OnlineServicesSectionProps) {
  const [online, setOnline] = useState(settings?.onlineLookups ?? true)
  const [gecBusy, setGecBusy] = useState(false)

  const configuredOnline = settings?.onlineLookups
  useEffect(() => {
    if (configuredOnline !== undefined) {
      setOnline(configuredOnline)
    }
  }, [configuredOnline])

  async function handleToggleOnline(nextEnabled: boolean) {
    onSetError('')
    setOnline(nextEnabled)
    try {
      const updatedSettings = await SetOnlineLookups(nextEnabled)
      setOnline(updatedSettings.onlineLookups)
    } catch (toggleError) {
      setOnline(settings?.onlineLookups ?? !nextEnabled)
      onSetError(String(toggleError))
    }
  }

  async function handleRefreshGec() {
    setGecBusy(true)
    onSetError('')
    try {
      await RefreshGEC()
    } catch (syncError) {
      onSetError(String(syncError))
    } finally {
      setGecBusy(false)
    }
  }

  return (
    <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2.5">
      <div className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
        Online Services
      </div>
      <label className="flex items-center justify-between cursor-pointer">
        <div>
          <div className="font-bold uppercase tracking-wider text-[#F4ECE4]">
            EDSM and EDAstro GEC Online Sync
          </div>
          <div className="text-[11px] text-[#a6907c]">
            Resolves coordinates for unvisited waypoint systems and downloads the galactic POI catalog.
          </div>
        </div>
        <SquareCheckbox
          checked={online}
          onChange={(checked) => handleToggleOnline(checked)}
        />
      </label>

      <div className="flex items-center justify-between pt-2 border-t border-[#2e1f12]">
        <div className="text-[11px] text-[#a6907c]">
          POI Catalog:{' '}
          <span className="font-mono-tabular text-[#F4ECE4] font-bold">
            {(settings?.gecCount ?? 0).toLocaleString()}
          </span>{' '}
          entries
        </div>
        <button
          type="button"
          disabled={gecBusy}
          onClick={handleRefreshGec}
          className="ed-btn-cyan"
        >
          {gecBusy ? 'SYNCING...' : 'SYNC CATALOG NOW'}
        </button>
      </div>
    </div>
  )
}

