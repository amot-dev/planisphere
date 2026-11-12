import { app } from '../../../wailsjs/go/models'

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
        <div className="text-sm font-bold uppercase tracking-wider text-[#F4ECE4]">
          Journal Folder
        </div>
        <p className="text-[11px] text-[#c7b3a1] leading-relaxed">
          Select your Elite Dangerous journal directory to track flights, scans, and exobiology.
        </p>
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <label className="font-bold uppercase tracking-wider text-[#FF7100]">
            Journal Path
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
            className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-[11px] text-[#F4ECE4] focus:border-[#FF7100] focus:outline-none"
          />
          <button
            type="button"
            onClick={onBrowseJournals}
            disabled={isBusy}
            className="ed-btn"
          >
            Browse…
          </button>
        </div>
        {localSettings?.defaultJournalDir &&
          journalDir !== localSettings.defaultJournalDir && (
            <div className="flex justify-end">
              <button
                type="button"
                onClick={onResetDefaultJournals}
                className="text-[11px] text-[#FF7100] hover:underline cursor-pointer"
              >
                Reset Default Path
              </button>
            </div>
          )}
      </div>

      <div className="p-3 border border-[#3d2816] bg-[#110c08] text-[11px] text-[#a6907c] leading-relaxed">
        Next will scan and index your existing journal files.
      </div>
    </div>
  )
}

