import { useEffect, useRef, useState } from 'react'
import { CreateMode, DeleteMode, GetModeUsage, UpdateMode } from '../../api/backend'
import { app, modes } from '../../api/models'
import { ModalShell } from '../../components/ui/ModalShell'
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

interface PendingModeDeletion {
  mode: modes.Mode
  usage: modes.ExpeditionUsage[]
}

/**
 * FlightModesSection renders the list of configured flight modes with color
 * pickers, a form to create custom flight modes, and a usage-aware confirmation
 * dialog before deleting a custom mode.
 */
export function FlightModesSection({
  modesState,
  onSetError,
}: FlightModesSectionProps) {
  const [newModeName, setNewModeName] = useState('')
  const [newModeDescription, setNewModeDescription] = useState('')
  const [newModeColor, setNewModeColor] = useState(ED_AMBER_HEX)
  const [colorDraftsById, setColorDraftsById] = useState<Record<number, string>>({})
  const [pendingDelete, setPendingDelete] = useState<PendingModeDeletion | null>(null)
  const [loadingDeleteUsageId, setLoadingDeleteUsageId] = useState<number | null>(null)
  const [deleting, setDeleting] = useState(false)
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

  async function handleRequestDeleteMode(modeItem: modes.Mode) {
    onSetError('')
    setLoadingDeleteUsageId(modeItem.id)
    try {
      const usageList = (await GetModeUsage(modeItem.id)) ?? []
      setPendingDelete({ mode: modeItem, usage: usageList })
    } catch (usageError) {
      onSetError(String(usageError))
    } finally {
      setLoadingDeleteUsageId(null)
    }
  }

  async function handleConfirmDeleteMode() {
    if (!pendingDelete) return
    const targetModeId = pendingDelete.mode.id
    const existingPending = pendingColorUpdatesRef.current[targetModeId]
    if (existingPending) {
      clearTimeout(existingPending.timer)
      delete pendingColorUpdatesRef.current[targetModeId]
    }
    onSetError('')
    setDeleting(true)
    try {
      await DeleteMode(targetModeId)
      setPendingDelete(null)
    } catch (deleteError) {
      onSetError(String(deleteError))
    } finally {
      setDeleting(false)
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
                disabled={loadingDeleteUsageId === modeItem.id}
                onClick={() => void handleRequestDeleteMode(modeItem)}
                className="ed-btn-danger"
              >
                {loadingDeleteUsageId === modeItem.id ? 'CHECKING...' : 'DELETE'}
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

      {pendingDelete && (
        <ModalShell
          title="Delete Flight Mode"
          onClose={() => {
            if (!deleting) setPendingDelete(null)
          }}
          maxWidth="max-w-md"
        >
          <div className="space-y-3 text-xs text-ed-text-body">
            <div className="flex items-center gap-2 p-2.5 border border-ed-border bg-ed-card">
              <span
                className="w-2.5 h-4 shrink-0"
                style={{ backgroundColor: pendingDelete.mode.color || ED_AMBER_HEX }}
              />
              <span className="font-mono-tabular font-bold uppercase tracking-wider text-ed-text-main">
                {pendingDelete.mode.name}
              </span>
            </div>

            {pendingDelete.usage.length > 0 ? (
              <div className="space-y-2">
                <p>
                  This flight mode is currently used by the following{' '}
                  {pendingDelete.usage.length === 1 ? 'expedition' : 'expeditions'}:
                </p>
                <div className="divide-y divide-ed-border-subtle border border-ed-border bg-ed-void max-h-48 overflow-y-auto">
                  {pendingDelete.usage.map((entry) => (
                    <div
                      key={entry.expeditionId}
                      className="px-2.5 py-2 flex items-center justify-between gap-2"
                    >
                      <span className="font-bold text-ed-text-main truncate">
                        {entry.expeditionName}
                      </span>
                      <div className="flex items-center gap-2 shrink-0 font-mono-tabular text-[11px] text-ed-text-muted">
                        {entry.legCount > 0 && (
                          <span>
                            {entry.legCount} {entry.legCount === 1 ? 'leg' : 'legs'}
                          </span>
                        )}
                        {entry.isActiveMode && (
                          <span className="px-1.5 py-0.5 text-[9px] font-bold uppercase bg-ed-amber-tint border border-ed-amber text-ed-amber">
                            ACTIVE MODE
                          </span>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
                <p className="text-[11px] text-ed-text-muted">
                  Deleting this flight mode will reassign all affected legs and expedition active modes back to Travel.
                </p>
              </div>
            ) : (
              <p className="text-ed-text-muted">
                No expeditions or visited legs currently use this flight mode.
              </p>
            )}
          </div>

          <div className="flex items-center justify-end gap-2 pt-2 border-t border-ed-border">
            <button
              type="button"
              disabled={deleting}
              onClick={() => setPendingDelete(null)}
              className="ed-btn"
            >
              CANCEL
            </button>
            <button
              type="button"
              disabled={deleting}
              onClick={() => void handleConfirmDeleteMode()}
              className="ed-btn-danger"
            >
              {deleting ? 'DELETING...' : 'DELETE FLIGHT MODE'}
            </button>
          </div>
        </ModalShell>
      )}
    </SettingsCard>
  )
}
