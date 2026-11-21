import { useEffect, useState } from 'react'
import { BrowseJournalDir, SetJournalDir } from '../../api/backend'
import { app } from '../../api/models'
import { SettingsCard } from '../../components/ui/SettingsCard'

/**
 * JournalDirectorySectionProps defines the current settings and callbacks for
 * configuring the player journal directory inside Terminal Configuration.
 */
export interface JournalDirectorySectionProps {
  settings: app.Settings | null
  onSetError: (errorMessage: string) => void
}

/**
 * JournalDirectorySection renders the Player Journal Directory path selector,
 * status badge, and reset controls.
 */
export function JournalDirectorySection({
  settings,
  onSetError,
}: JournalDirectorySectionProps) {
  const [directoryPath, setDirectoryPath] = useState(settings?.journalDir ?? '')

  useEffect(() => {
    setDirectoryPath(settings?.journalDir ?? '')
  }, [settings?.journalDir])

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
      const updatedSettings = await SetJournalDir(directoryPath)
      setDirectoryPath(updatedSettings.journalDir)
    } catch (saveError) {
      onSetError(String(saveError))
    }
  }

  async function handleResetDefaultDirectory() {
    onSetError('')
    try {
      const updatedSettings = await SetJournalDir('')
      setDirectoryPath(updatedSettings.journalDir)
    } catch (resetError) {
      onSetError(String(resetError))
    }
  }

  return (
    <SettingsCard
      title="Journal Directory"
      headerRight={
        <span
          className={`text-[10px] font-mono-tabular font-bold uppercase px-2 py-0.5 border ${
            settings?.journalDirExists
              ? 'bg-ed-cyan-tint border-ed-cyan text-ed-cyan'
              : 'bg-ed-amber-tint border-ed-amber text-ed-amber'
          }`}
        >
          {settings?.journalDirExists ? 'FOUND' : 'NOT FOUND'}
        </span>
      }
    >
      <div className="flex items-center gap-2">
        <input
          type="text"
          value={directoryPath}
          onChange={(event) => setDirectoryPath(event.target.value)}
          className="flex-1 bg-ed-void border border-ed-border px-2.5 py-1.5 font-mono-tabular text-xs text-ed-text-main"
        />
        <button
          type="button"
          onClick={handleBrowse}
          className="ed-btn"
        >
          BROWSE...
        </button>
        <button
          type="button"
          onClick={handleSaveDirectory}
          className="ed-btn-primary"
        >
          APPLY
        </button>
        <button
          type="button"
          onClick={handleResetDefaultDirectory}
          title="Reset journal directory to the game default"
          className="ed-btn"
        >
          RESET TO DEFAULT
        </button>
      </div>
    </SettingsCard>
  )
}

