import { useState } from 'react'
import {
  CloneExpedition,
  CreateExpedition,
  DeleteExpedition,
  UpdateExpedition,
} from '../../../api/backend'
import { expedition } from '../../../api/models'
import { ModalShell } from '../../../components/ui/ModalShell'

/**
 * ExpeditionManageModalProps defines the saved expedition list, active selection,
 * and callbacks for creating, cloning, renaming, switching, and deleting flight plans.
 */
export interface ExpeditionManageModalProps {
  expeditions: expedition.Expedition[]
  activeId: number
  onSelect: (id: number) => void
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
  const [editingId, setEditingId] = useState<number | null>(null)
  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null)
  const [editName, setEditName] = useState('')
  const [editDescription, setEditDescription] = useState('')
  const [errorMessage, setErrorMessage] = useState('')

  async function runExpeditionAction(action: () => Promise<void>) {
    setErrorMessage('')
    try {
      await action()
    } catch (actionError) {
      setErrorMessage(String(actionError))
    }
  }

  function handleCreate() {
    if (!name.trim()) return
    return runExpeditionAction(async () => {
      const created = await CreateExpedition(name.trim(), description.trim())
      setName('')
      setDescription('')
      onSelect(created.id)
      onClose()
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
      onSelect(cloned.id)
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
        <div className="flex justify-end">
          <button
            type="button"
            onClick={handleCreate}
            className="ed-btn-primary"
          >
            CREATE EXPEDITION
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
                  value={editName}
                  onChange={(event) => setEditName(event.target.value)}
                  className="bg-ed-void border border-ed-border px-2 py-1 text-ed-text-main"
                />
                <input
                  type="text"
                  value={editDescription}
                  onChange={(event) => setEditDescription(event.target.value)}
                  className="flex-1 bg-ed-void border border-ed-border px-2 py-1 text-ed-text-main"
                />
                <button
                  type="button"
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
                  onClick={() => {
                    onSelect(expeditionItem.id)
                    onClose()
                  }}
                  className="flex-1 text-left cursor-pointer"
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
                        onClick={() => setConfirmDeleteId(null)}
                        className="ed-btn"
                      >
                        CANCEL
                      </button>
                      <button
                        type="button"
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
                        onClick={() => handleClone(expeditionItem.id)}
                        title="Clone this expedition into a new expedition in the Planning phase"
                        className="ed-btn-cyan"
                      >
                        CLONE
                      </button>
                      <button
                        type="button"
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

