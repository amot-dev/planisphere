import { RescanJournals, SetJournalDebounceMs } from '../../../wailsjs/go/app/App'
import { app } from '../../../wailsjs/go/models'
import {
  DEFAULT_JOURNAL_DEBOUNCE_MILLISECONDS,
  JournalDebounceControl,
} from '../common/JournalDebounceControl'

/**
 * DatabaseArchiveSectionProps defines the settings, live ingestion telemetry,
 * and callbacks for the Database and Event Archive configuration card.
 */
export interface DatabaseArchiveSectionProps {
  settings: app.Settings | null
  ingestStatus: app.IngestStatus | null
  onRefresh: () => void
  onSetError: (errorMessage: string) => void
}

/**
 * DatabaseArchiveSection renders journal ingestion counters, rescan trigger,
 * live watcher debounce slider, and active SQLite database path readout.
 */
export function DatabaseArchiveSection({
  settings,
  ingestStatus,
  onRefresh,
  onSetError,
}: DatabaseArchiveSectionProps) {
  async function handleCommitDebounceMilliseconds(milliseconds: number) {
    onSetError('')
    try {
      await SetJournalDebounceMs(milliseconds)
      onRefresh()
    } catch (commitError) {
      onSetError(String(commitError))
    }
  }

  return (
    <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#00D2FF]">
          Database and Event Archive
        </span>
        <button
          type="button"
          onClick={() => RescanJournals().then(onRefresh)}
          className="ed-btn-cyan"
        >
          <span>Rescan Journals</span>
        </button>
      </div>
      <div className="grid grid-cols-2 sm:grid-cols-3 gap-2 font-mono-tabular text-xs">
        <div className="p-2 bg-[#050506] border border-[#3d2816]">
          <div className="text-[10px] uppercase text-[#a6907c]">Tracked Files</div>
          <div className="text-sm font-bold text-[#F4ECE4]">
            {ingestStatus?.filesTracked ?? 0}
          </div>
        </div>
        <div className="p-2 bg-[#050506] border border-[#3d2816]">
          <div className="text-[10px] uppercase text-[#a6907c]">Stored Events</div>
          <div className="text-sm font-bold text-[#FF7100]">
            {(ingestStatus?.eventsStored ?? 0).toLocaleString()}
          </div>
        </div>
        <div className="p-2 bg-[#050506] border border-[#3d2816]">
          <div className="text-[10px] uppercase text-[#a6907c]">Live Watcher</div>
          <div className="text-sm font-bold text-[#00D2FF]">
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
    </div>
  )
}

