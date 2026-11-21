import { useState } from 'react'
import { RescanJournals, SetJournalDebounceMs } from '../../api/backend'
import { app } from '../../api/models'
import { JournalDebounceControl } from '../../components/ui/JournalDebounceControl'
import { SettingsCard } from '../../components/ui/SettingsCard'
import { DEFAULT_JOURNAL_DEBOUNCE_MILLISECONDS } from '../../domain/journalDebounce'

/**
 * DatabaseArchiveSectionProps defines the settings, live ingestion telemetry,
 * and callbacks for the Database and Event Archive configuration card.
 */
export interface DatabaseArchiveSectionProps {
  settings: app.Settings | null
  ingestStatus: app.IngestStatus | null
  onSetError: (errorMessage: string) => void
}

/**
 * DatabaseArchiveSection renders journal ingestion counters, rescan trigger,
 * live watcher debounce slider, and active SQLite database path readout.
 */
export function DatabaseArchiveSection({
  settings,
  ingestStatus,
  onSetError,
}: DatabaseArchiveSectionProps) {
  const [rescanning, setRescanning] = useState(false)
  const isScanning = rescanning || Boolean(ingestStatus?.scanning)

  async function handleRescanJournals() {
    if (isScanning) {
      return
    }
    onSetError('')
    setRescanning(true)
    try {
      await RescanJournals()
    } catch (rescanError) {
      onSetError(String(rescanError))
    } finally {
      setRescanning(false)
    }
  }

  async function handleCommitDebounceMilliseconds(milliseconds: number) {
    onSetError('')
    try {
      await SetJournalDebounceMs(milliseconds)
    } catch (commitError) {
      onSetError(String(commitError))
    }
  }

  return (
    <SettingsCard
      title="Database and Event Archive"
      headerRight={
        <button
          type="button"
          disabled={isScanning}
          onClick={handleRescanJournals}
          className="ed-btn-cyan"
        >
          <span>{isScanning ? 'RESCANNING...' : 'RESCAN JOURNALS'}</span>
        </button>
      }
    >
      <div className="grid grid-cols-2 sm:grid-cols-3 gap-2 font-mono-tabular text-xs">
        <div className="p-2 bg-[#050506] border border-[#3d2816]">
          <div className="text-[10px] uppercase text-[#a6907c]">Tracked Files</div>
          <div className="text-sm font-bold text-[#F4ECE4]">
            {ingestStatus?.filesTracked ?? 0}
          </div>
        </div>
        <div className="p-2 bg-[#050506] border border-[#3d2816]">
          <div className="text-[10px] uppercase text-[#a6907c]">Stored Events</div>
          <div className="text-sm font-bold text-[#F4ECE4]">
            {(ingestStatus?.eventsStored ?? 0).toLocaleString()}
          </div>
        </div>
        <div className="p-2 bg-[#050506] border border-[#3d2816]">
          <div className="text-[10px] uppercase text-[#a6907c]">Live Watcher</div>
          <div
            className={`text-sm font-bold ${
              ingestStatus?.watcherActive ? 'text-[#00D2FF]' : 'text-[#a6907c]'
            }`}
          >
            {ingestStatus?.watcherActive ? 'ACTIVE' : 'STANDBY'}
          </div>
        </div>
      </div>
      <JournalDebounceControl
        configuredDebounceMilliseconds={
          settings?.journalDebounceMs ?? DEFAULT_JOURNAL_DEBOUNCE_MILLISECONDS
        }
        defaultDebounceMilliseconds={
          settings?.defaultJournalDebounceMs ?? DEFAULT_JOURNAL_DEBOUNCE_MILLISECONDS
        }
        onCommitDebounceMilliseconds={handleCommitDebounceMilliseconds}
      />
      <div className="text-[11px] text-[#a6907c] font-mono-tabular flex items-center justify-between">
        <span>Database Path:</span>
        <span className="text-[#F4ECE4] font-bold">{settings?.dbPath}</span>
      </div>
    </SettingsCard>
  )
}

