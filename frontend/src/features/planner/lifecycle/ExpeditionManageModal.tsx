import { useMemo, useState } from 'react'
import {
  BrowseWaypointImportFile,
  CloneExpedition,
  CreateExpedition,
  CreateExpeditionWithWaypoints,
  DeleteExpedition,
  UpdateExpedition,
} from '../../../api/backend'
import { expedition } from '../../../api/models'
import { ModalShell } from '../../../components/ui/ModalShell'
import {
  deriveExpeditionNameFromFilename,
  parseWaypointImportText,
} from '../../../domain/waypointImport'
import { WaypointImportSection } from './WaypointImportSection'

/**
 * ExpeditionManageModalProps defines the saved expedition list, active selection,
 * and callbacks for creating, cloning, renaming, switching, and deleting flight
 * plans. Passing `{ persist: false }` to `onSelect` updates local selection
 * without issuing a redundant `SetActiveExpedition` RPC when the backend already
 * persisted the active expedition ID.
 */
export interface ExpeditionManageModalProps {
  expeditions: expedition.Expedition[]
  activeId: number
  onSelect: (id: number, options?: { persist?: boolean }) => void
  onClose: () => void
}

/**
 * ExpeditionManageModal renders the dialog for creating new expedition plans,
 * switching between saved expeditions, cloning plans, and renaming or deleting plans.
 */
export function ExpeditionManageModal({
  expeditions,
  activeId,
  onSelect,
  onClose,
}: ExpeditionManageModalProps) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [importExpanded, setImportExpanded] = useState(false)
  const [rawImportText, setRawImportText] = useState('')
  const [loadedFileNote, setLoadedFileNote] = useState('')
  const [loadingFile, setLoadingFile] = useState(false)
  const [creating, setCreating] = useState(false)
  const [mutating, setMutating] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null)
  const [editName, setEditName] = useState('')
  const [editDescription, setEditDescription] = useState('')
  const [errorMessage, setErrorMessage] = useState('')

  const busy = creating || loadingFile || mutating
  const parsedImport = useMemo(() => parseWaypointImportText(rawImportText), [rawImportText])
  const hasRawImportText = rawImportText.trim().length > 0
  const hasInvalidImport = hasRawImportText && parsedImport.systems.length === 0
  const stagedWaypointCount = parsedImport.systems.length

  async function runExpeditionAction(action: () => Promise<void>) {
    if (busy) return
    setErrorMessage('')
    setMutating(true)
    try {
      await action()
    } catch (actionError) {
      setErrorMessage(String(actionError))
    } finally {
      setMutating(false)
    }
  }

  async function handleLoadImportFile() {
    if (busy) return
    setErrorMessage('')
    setLoadingFile(true)
    try {
      const selectedFile = await BrowseWaypointImportFile()
      if (!selectedFile?.fileName && !selectedFile?.content) {
        return
      }
      const parsedFile = parseWaypointImportText(selectedFile.content ?? '')
      if (parsedFile.systems.length === 0) {
        setErrorMessage(`No star systems found in "${selectedFile.fileName}".`)
        return
      }
      setRawImportText(parsedFile.systems.join('\n'))
      const duplicateNote =
        parsedFile.collapsedDuplicateCount > 0
          ? ` (${parsedFile.collapsedDuplicateCount} duplicate body ${
              parsedFile.collapsedDuplicateCount === 1 ? 'row' : 'rows'
            } collapsed)`
          : ''
      setLoadedFileNote(`Loaded from ${selectedFile.fileName}${duplicateNote}`)
      if (!name.trim() && selectedFile.fileName) {
        setName(deriveExpeditionNameFromFilename(selectedFile.fileName))
      }
    } catch (loadError) {
      setErrorMessage(String(loadError))
    } finally {
      setLoadingFile(false)
    }
  }

  function handleChangeImportText(nextText: string) {
    setRawImportText(nextText)
    if (loadedFileNote) {
      setLoadedFileNote('')
    }
  }

  function handleClearImportText() {
    setRawImportText('')
    setLoadedFileNote('')
    setErrorMessage('')
  }

  function handleCreate() {
    if (!name.trim() || busy || hasInvalidImport) return
    return runExpeditionAction(async () => {
      setCreating(true)
      try {
        const created =
          parsedImport.systems.length > 0
            ? await CreateExpeditionWithWaypoints(
                name.trim(),
                description.trim(),
                parsedImport.systems,
              )
            : await CreateExpedition(name.trim(), description.trim())
        setName('')
        setDescription('')
        setRawImportText('')
        setLoadedFileNote('')
        setImportExpanded(false)
        onSelect(created.id, { persist: false })
        onClose()
      } finally {
        setCreating(false)
      }
    })
  }

  function handleSaveEdit(id: number) {
    return runExpeditionAction(async () => {
      await UpdateExpedition(id, editName, editDescription)
      setEditingId(null)
    })
  }

  function handleClone(id: number) {
    return runExpeditionAction(async () => {
      const cloned = await CloneExpedition(id)
      onSelect(cloned.id, { persist: false })
      onClose()
    })
  }

  function handleDelete(expeditionId: number) {
    return runExpeditionAction(async () => {
      await DeleteExpedition(expeditionId)
      setConfirmDeleteId(null)
    })
  }

  return (
    <ModalShell
      title="Expeditions"
      subtitle="Create a new expedition or switch between saved expeditions"
      onClose={onClose}
      closeDisabled={busy}
    >
      {errorMessage && (
        <div className="p-2.5 border border-ed-danger bg-ed-danger-tint text-ed-danger-text">
          {errorMessage}
        </div>
      )}
      <div className="p-3 border border-ed-border bg-ed-card space-y-2">
        <div className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-ed-amber">
          + Create New Expedition
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          <input
            type="text"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Expedition Name (e.g. Colonia and Sagittarius A*)"
            className="bg-ed-void border border-ed-border px-2.5 py-1.5 text-ed-text-main"
          />
          <input
            type="text"
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            placeholder="Optional description..."
            className="bg-ed-void border border-ed-border px-2.5 py-1.5 text-ed-text-main"
          />
        </div>
        {importExpanded && (
          <WaypointImportSection
            rawImportText={rawImportText}
            parsedImport={parsedImport}
            loadedFileNote={loadedFileNote}
            loadingFile={loadingFile}
            disabled={creating || mutating}
            onChangeText={handleChangeImportText}
            onLoadFile={() => void handleLoadImportFile()}
            onClear={handleClearImportText}
          />
        )}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <button
            type="button"
            disabled={busy}
            onClick={() => setImportExpanded((previous) => !previous)}
            className="ed-btn"
          >
            {importExpanded
              ? 'HIDE IMPORT'
              : stagedWaypointCount > 0
                ? `+ IMPORT WAYPOINTS (${stagedWaypointCount})`
                : '+ IMPORT WAYPOINTS'}
          </button>
          <button
            type="button"
            disabled={!name.trim() || busy || hasInvalidImport}
            onClick={handleCreate}
            className={`ed-btn-primary ${
              !name.trim() || busy || hasInvalidImport
                ? 'opacity-40 cursor-not-allowed hover:bg-transparent'
                : ''
            }`}
          >
            {creating
              ? 'CREATING...'
              : stagedWaypointCount > 0
                ? `CREATE AND IMPORT (${stagedWaypointCount})`
                : 'CREATE EXPEDITION'}
          </button>
        </div>
      </div>

      <div className="divide-y divide-ed-border-subtle border border-ed-border bg-ed-card">
        {expeditions.map((expeditionItem) => (
          <div key={expeditionItem.id} className="p-2.5 flex items-center justify-between gap-2">
            {editingId === expeditionItem.id ? (
              <div className="flex-1 flex items-center gap-2">
                <input
                  type="text"
                  disabled={busy}
                  value={editName}
                  onChange={(event) => setEditName(event.target.value)}
                  className="bg-ed-void border border-ed-border px-2 py-1 text-ed-text-main"
                />
                <input
                  type="text"
                  disabled={busy}
                  value={editDescription}
                  onChange={(event) => setEditDescription(event.target.value)}
                  className="flex-1 bg-ed-void border border-ed-border px-2 py-1 text-ed-text-main"
                />
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => handleSaveEdit(expeditionItem.id)}
                  className="ed-btn-cyan"
                >
                  SAVE
                </button>
              </div>
            ) : (
              <>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    onSelect(expeditionItem.id)
                    onClose()
                  }}
                  className="flex-1 text-left cursor-pointer disabled:cursor-not-allowed"
                >
                  <div className="flex items-center gap-2">
                    <span
                      className={`font-bold uppercase tracking-wider ${
                        expeditionItem.id === activeId ? 'text-ed-amber' : 'text-ed-text-main'
                      }`}
                    >
                      {expeditionItem.name}
                    </span>
                    <span className="text-[9px] font-mono-tabular uppercase px-1.5 py-0.5 bg-ed-control border border-ed-border text-ed-text-muted">
                      {expeditionItem.status}
                    </span>
                  </div>
                  {expeditionItem.description && (
                    <div className="text-[11px] text-ed-text-muted">{expeditionItem.description}</div>
                  )}
                </button>
                <div className="flex items-center gap-1.5">
                  {confirmDeleteId === expeditionItem.id ? (
                    <>
                      <span className="text-[10px] font-bold uppercase tracking-wider text-ed-danger-text mr-1">
                        Delete expedition?
                      </span>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => setConfirmDeleteId(null)}
                        className="ed-btn"
                      >
                        CANCEL
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => handleDelete(expeditionItem.id)}
                        className="ed-btn-danger"
                      >
                        CONFIRM DELETE
                      </button>
                    </>
                  ) : (
                    <>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => handleClone(expeditionItem.id)}
                        title="Clone this expedition into a new expedition in the Planning phase"
                        className="ed-btn-cyan"
                      >
                        CLONE
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => {
                          setConfirmDeleteId(null)
                          setEditingId(expeditionItem.id)
                          setEditName(expeditionItem.name)
                          setEditDescription(expeditionItem.description)
                        }}
                        className="ed-btn"
                      >
                        RENAME
                      </button>
                      {expeditions.length > 1 && (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => {
                            setEditingId(null)
                            setConfirmDeleteId(expeditionItem.id)
                          }}
                          className="ed-btn-danger"
                        >
                          DELETE
                        </button>
                      )}
                    </>
                  )}
                </div>
              </>
            )}
          </div>
        ))}
      </div>
    </ModalShell>
  )
}

