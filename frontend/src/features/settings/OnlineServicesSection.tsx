import { useEffect, useState } from 'react'
import { RefreshGEC, SetOnlineLookups } from '../../api/backend'
import { app } from '../../api/models'
import { SettingsCard } from '../../components/ui/SettingsCard'
import { SquareCheckbox } from '../../components/ui/SquareCheckbox'

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
    <SettingsCard title="Online Services" spacingClassName="space-y-2.5">
      <label className="flex items-center justify-between cursor-pointer">
        <div>
          <div className="font-bold uppercase tracking-wider text-ed-text-main">
            EDSM and EDAstro GEC Online Sync
          </div>
          <div className="text-[11px] text-ed-text-muted">
            Resolves coordinates for unvisited waypoint systems and downloads the galactic POI catalog.
          </div>
        </div>
        <SquareCheckbox
          checked={online}
          onChange={(checked) => handleToggleOnline(checked)}
        />
      </label>

      <div className="flex items-center justify-between pt-2 border-t border-ed-border-dim">
        <div className="text-[11px] text-ed-text-muted">
          POI Catalog:{' '}
          <span className="font-mono-tabular text-ed-text-main font-bold">
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
    </SettingsCard>
  )
}

