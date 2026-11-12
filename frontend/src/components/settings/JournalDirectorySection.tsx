import { useState } from 'react'
import { BrowseJournalDir, SetJournalDir } from '../../../wailsjs/go/app/App'
import { app } from '../../../wailsjs/go/models'

/**
 * JournalDirectorySectionProps defines the current settings and callbacks for
 * configuring the player journal directory inside Terminal Configuration.
 */
export interface JournalDirectorySectionProps {
  settings: app.Settings | null
  onRefresh: () => void
  onSetError: (errorMessage: string) => void
}

/**
 * JournalDirectorySection renders the Player Journal Directory path selector,
 * status badge, and reset controls.
 */
export function JournalDirectorySection({
  settings,
  onRefresh,
  onSetError,
}: JournalDirectorySectionProps) {
  const [directoryPath, setDirectoryPath] = useState(settings?.journalDir ?? '')

  async function handleBrowse() {
    try {
      const pickedDirectory = await BrowseJournalDir()
      if (pickedDirectory) setDirectoryPath(pickedDirectory)
    } catch (browseError) {
      onSetError(String(browseError))
    }
  }

  async function handleSaveDirectory() {
    onSetError('')
    try {
      await SetJournalDir(directoryPath)
      onRefresh()
    } catch (saveError) {
      onSetError(String(saveError))
    }
  }

  async function handleResetDefaultDirectory() {
    onSetError('')
    try {
      await SetJournalDir('')
      setDirectoryPath(settings?.defaultJournalDir ?? '')
      onRefresh()
    } catch (resetError) {
      onSetError(String(resetError))
    }
  }

  return (
    <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
          Player Journal Directory
        </span>
        <span
          className={`text-[10px] font-mono-tabular font-bold uppercase px-2 py-0.5 border ${
            settings?.journalDirExists
              ? 'bg-[#08151c] border-[#00D2FF] text-[#00D2FF]'
              : 'bg-[#211307] border-[#FF7100] text-[#FF7100]'
          }`}
        >
          {settings?.journalDirExists ? 'FOUND' : 'NOT FOUND'}
        </span>
      </div>

      <div className="flex items-center gap-2">
        <input
          type="text"
          value={directoryPath}
          onChange={(event) => setDirectoryPath(event.target.value)}
          className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-xs text-[#F4ECE4]"
        />
        <button
          type="button"
          onClick={handleBrowse}
          className="ed-btn"
        >
          Browse…
        </button>
        <button
          type="button"
          onClick={handleSaveDirectory}
          className="ed-btn-primary"
        >
          Apply
        </button>
        <button
          type="button"
          onClick={handleResetDefaultDirectory}
          title="Reset journal directory to the game default"
          className="ed-btn-cyan"
        >
          Reset to Default
        </button>
      </div>
    </div>
  )
}

