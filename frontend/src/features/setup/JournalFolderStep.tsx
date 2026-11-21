import { app } from '../../api/models'

/**
 * JournalFolderStepProps defines the player journal directory state and
 * browse/reset callbacks for the second step of the onboarding wizard.
 */
export interface JournalFolderStepProps {
  journalDir: string
  localSettings: app.Settings | null
  isBusy: boolean
  onChangeJournalDir: (nextJournalDir: string) => void
  onBrowseJournals: () => void
  onResetDefaultJournals: () => void
}

/**
 * JournalFolderStep renders the Elite Dangerous player journal directory
 * selector and auto-detection badge in the setup wizard.
 */
export function JournalFolderStep({
  journalDir,
  localSettings,
  isBusy,
  onChangeJournalDir,
  onBrowseJournals,
  onResetDefaultJournals,
}: JournalFolderStepProps) {
  const isDetectedDefault =
    Boolean(localSettings?.journalDirExists) &&
    journalDir === (localSettings?.journalDir || localSettings?.defaultJournalDir)

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <div className="text-sm font-bold uppercase tracking-wider text-ed-text-main">
          Journal Directory
        </div>
        <p className="text-[11px] text-ed-text-body leading-relaxed">
          Select your Elite Dangerous journal directory to track flights, scans, and exobiology.
        </p>
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <label className="font-bold uppercase tracking-wider text-ed-amber">
            Journal Directory
          </label>
          <span
            className={
              isDetectedDefault ? 'ed-badge ed-badge-visited' : 'ed-badge ed-badge-planned'
            }
          >
            {isDetectedDefault ? 'DETECTED' : 'CUSTOM'}
          </span>
        </div>
        <div className="flex gap-1.5">
          <input
            type="text"
            value={journalDir}
            onChange={(event) => onChangeJournalDir(event.target.value)}
            placeholder={localSettings?.defaultJournalDir || ''}
            className="flex-1 bg-ed-void border border-ed-border px-2.5 py-1.5 font-mono-tabular text-[11px] text-ed-text-main focus:border-ed-amber focus:outline-none"
          />
          <button
            type="button"
            onClick={onBrowseJournals}
            disabled={isBusy}
            className="ed-btn"
          >
            BROWSE...
          </button>
        </div>
        {localSettings?.defaultJournalDir &&
          journalDir !== localSettings.defaultJournalDir && (
            <div className="flex justify-end">
              <button
                type="button"
                onClick={onResetDefaultJournals}
                className="text-[11px] text-ed-amber hover:underline cursor-pointer"
              >
                Reset to Default
              </button>
            </div>
          )}
      </div>

      <div className="p-3 border border-ed-border bg-ed-card text-[11px] text-ed-text-muted leading-relaxed">
        Next will scan and index your existing journal files.
      </div>
    </div>
  )
}

