import { useState } from 'react'
import {
  BrowseEDDiscoveryDB,
  ImportEDDiscoveryHistory,
} from '../../../wailsjs/go/app/App'
import { app } from '../../../wailsjs/go/models'

/**
 * EddImportProgress describes live progress events emitted while importing
 * historical journal entries from an external EDDiscovery SQLite database.
 */
export interface EddImportProgress {
  active: boolean
  stage: string
  current: number
  total: number
  percent: number
  files: number
}

/**
 * EDDiscoveryImportSectionProps defines the settings, live import progress,
 * and callbacks for the Historical Journal Import configuration card.
 */
export interface EDDiscoveryImportSectionProps {
  settings: app.Settings | null
  eddProgress?: EddImportProgress | null
  onRefresh: () => void
  onSetError: (errorMessage: string) => void
}

/**
 * EDDiscoveryImportSection renders the EDDUser.sqlite path selector, import
 * trigger button, and live progress bar for historical journal recovery.
 */
export function EDDiscoveryImportSection({
  settings,
  eddProgress,
  onRefresh,
  onSetError,
}: EDDiscoveryImportSectionProps) {
  const [eddPath, setEddPath] = useState(settings?.edDiscoveryDbPath ?? '')
  const [eddBusy, setEddBusy] = useState(false)
  const [eddStatus, setEddStatus] = useState('')

  async function handleBrowseEdd() {
    try {
      const pickedFile = await BrowseEDDiscoveryDB()
      if (pickedFile) setEddPath(pickedFile)
    } catch (browseError) {
      onSetError(String(browseError))
    }
  }

  async function handleImportEdd() {
    setEddBusy(true)
    onSetError('')
    setEddStatus('')
    try {
      const importedCount = await ImportEDDiscoveryHistory(eddPath)
      setEddStatus(
        `Imported ${importedCount.toLocaleString()} historical journal events into local database.`,
      )
      onRefresh()
    } catch (importError) {
      onSetError(String(importError))
    } finally {
      setEddBusy(false)
    }
  }

  return (
    <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#00D2FF]">
          Historical Journal Import (EDDiscovery)
        </span>
      </div>
      <p className="text-[11px] text-[#a6907c]">
        Import past journal history from an existing EDDiscovery database.
      </p>
      <div className="flex items-center gap-2">
        <input
          type="text"
          value={eddPath}
          onChange={(event) => setEddPath(event.target.value)}
          placeholder="Path to EDDUser.sqlite..."
          className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-xs text-[#F4ECE4]"
        />
        <button
          type="button"
          onClick={handleBrowseEdd}
          className="ed-btn"
        >
          Browse…
        </button>
        <button
          type="button"
          disabled={eddBusy || Boolean(eddProgress?.active)}
          onClick={handleImportEdd}
          className="ed-btn-cyan"
        >
          {eddBusy || eddProgress?.active ? 'Importing…' : 'Import'}
        </button>
      </div>
      {(eddBusy || eddProgress?.active) && (
        <div className="space-y-1.5 pt-1">
          <div className="flex items-center justify-between text-[11px] font-mono-tabular">
            <span className="text-[#00D2FF]">
              {eddProgress?.stage || 'Reading database…'}
            </span>
            <span className="text-[#F4ECE4] font-bold">
              {eddProgress && eddProgress.total > 0
                ? `${eddProgress.percent}% (${eddProgress.current.toLocaleString()} / ${eddProgress.total.toLocaleString()} events)`
                : 'Preparing…'}
            </span>
          </div>
          <div className="w-full h-2 bg-[#050506] border border-[#3d2816] overflow-hidden">
            <div
              className="h-full bg-[#FF7100] transition-all duration-150"
              style={{ width: `${eddProgress?.total ? Math.max(2, eddProgress.percent) : 15}%` }}
            />
          </div>
        </div>
      )}
      {eddStatus && <div className="text-[11px] text-[#00D2FF] font-mono-tabular">{eddStatus}</div>}
    </div>
  )
}

