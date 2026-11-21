import { useEffect, useRef, useState } from 'react'
import { CreateMode, DeleteMode, UpdateMode } from '../../api/backend'
import { app, modes } from '../../api/models'
import { SettingsCard } from '../../components/ui/SettingsCard'
import { ED_AMBER_HEX } from '../../domain/theme'

/**
 * FlightModesSectionProps defines the current flight modes state and callbacks
 * for creating, recoloring, and deleting custom flight modes.
 */
export interface FlightModesSectionProps {
  modesState: app.ModesState | null
  onSetError: (errorMessage: string) => void
}

/**
 * FlightModesSection renders the list of configured flight modes with color
 * pickers and a form to create custom flight modes.
 */
export function FlightModesSection({
  modesState,
  onSetError,
}: FlightModesSectionProps) {
  const [newModeName, setNewModeName] = useState('')
  const [newModeDescription, setNewModeDescription] = useState('')
  const [newModeColor, setNewModeColor] = useState(ED_AMBER_HEX)
  const [colorDraftsById, setColorDraftsById] = useState<Record<number, string>>({})
  const pendingColorUpdatesRef = useRef<
    Record<number, { timer: ReturnType<typeof setTimeout>; commit: () => void }>
  >({})

  useEffect(() => {
    return () => {
      for (const pendingUpdate of Object.values(pendingColorUpdatesRef.current)) {
        clearTimeout(pendingUpdate.timer)
        pendingUpdate.commit()
      }
      pendingColorUpdatesRef.current = {}
    }
  }, [])

  function handleColorChange(modeItem: modes.Mode, nextColor: string) {
    setColorDraftsById((previous) => ({ ...previous, [modeItem.id]: nextColor }))
    const existingPending = pendingColorUpdatesRef.current[modeItem.id]
    if (existingPending) {
      clearTimeout(existingPending.timer)
    }
    const commit = () => {
      delete pendingColorUpdatesRef.current[modeItem.id]
      onSetError('')
      UpdateMode(modeItem.id, modeItem.name, modeItem.description, nextColor).catch(
        (updateError) => onSetError(String(updateError)),
      )
    }
    pendingColorUpdatesRef.current[modeItem.id] = {
      timer: setTimeout(commit, 200),
      commit,
    }
  }

  async function handleCreateMode() {
    onSetError('')
    try {
      await CreateMode(newModeName, newModeDescription, newModeColor)
      setNewModeName('')
      setNewModeDescription('')
    } catch (createError) {
      onSetError(String(createError))
    }
  }

  async function handleDeleteMode(modeId: number) {
    const existingPending = pendingColorUpdatesRef.current[modeId]
    if (existingPending) {
      clearTimeout(existingPending.timer)
      delete pendingColorUpdatesRef.current[modeId]
    }
    onSetError('')
    try {
      await DeleteMode(modeId)
    } catch (deleteError) {
      onSetError(String(deleteError))
    }
  }

  return (
    <SettingsCard title="Flight Modes" spacingClassName="space-y-2.5">
      <div className="divide-y divide-ed-border-subtle border border-ed-border bg-ed-void">
        {(modesState?.modes ?? []).map((modeItem) => (
          <div key={modeItem.id} className="p-2 flex items-center justify-between gap-3">
            <div className="flex items-center gap-2.5 min-w-0">
              <input
                type="color"
                value={colorDraftsById[modeItem.id] ?? modeItem.color}
                onChange={(event) => handleColorChange(modeItem, event.target.value)}
                title={`Change colour for ${modeItem.name}`}
                className="w-5 h-5 ed-color-picker shrink-0 transition-colors"
              />
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-bold uppercase tracking-wider text-ed-text-main">
                    {modeItem.name}
                  </span>
                  {modeItem.isDefault && (
                    <span className="text-[9px] font-mono-tabular uppercase px-1.5 py-0.2 bg-ed-control border border-ed-border text-ed-text-muted">
                      DEFAULT
                    </span>
                  )}
                </div>
                {modeItem.description && (
                  <div className="text-[10px] text-ed-text-muted truncate">{modeItem.description}</div>
                )}
              </div>
            </div>
            {!modeItem.isDefault && (
              <button
                type="button"
                onClick={() => handleDeleteMode(modeItem.id)}
                className="ed-btn-danger"
              >
                DELETE
              </button>
            )}
          </div>
        ))}
      </div>

      {/* Add Custom Mode */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-2 pt-1">
        <input
          type="text"
          value={newModeName}
          onChange={(event) => setNewModeName(event.target.value)}
          placeholder="New flight mode name..."
          className="bg-ed-void border border-ed-border px-2.5 py-1 text-ed-text-main"
        />
        <input
          type="text"
          value={newModeDescription}
          onChange={(event) => setNewModeDescription(event.target.value)}
          placeholder="Optional description..."
          className="bg-ed-void border border-ed-border px-2.5 py-1 text-ed-text-main"
        />
        <div className="flex items-center gap-2">
          <input
            type="color"
            value={newModeColor}
            onChange={(event) => setNewModeColor(event.target.value)}
            title="Select flight mode colour"
            className="w-7 h-7 ed-color-picker shrink-0 transition-colors"
          />
          <button
            type="button"
            onClick={handleCreateMode}
            className="flex-1 ed-btn-primary"
          >
            ADD FLIGHT MODE
          </button>
        </div>
      </div>
    </SettingsCard>
  )
}

