import { useState } from 'react'
import {
  CloneExpedition,
  CreateExpedition,
  DeleteExpedition,
  UpdateExpedition,
} from '../../../wailsjs/go/app/App'
import { expedition } from '../../../wailsjs/go/models'
import { ModalShell } from '../common/ModalShell'

/**
 * ExpeditionManageModalProps defines the saved expedition list, active selection,
 * and callbacks for creating, cloning, renaming, switching, and deleting flight plans.
 */
export interface ExpeditionManageModalProps {
  expeditions: expedition.Expedition[]
  activeId: number
  onSelect: (id: number) => void
  onClose: () => void
  onRefresh: () => void
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
  onRefresh,
}: ExpeditionManageModalProps) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [editingId, setEditingId] = useState<number | null>(null)
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
      onRefresh()
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
      onRefresh()
    })
  }

  return (
    <ModalShell
      title="Expedition Flight Plans"
      subtitle="Create a new expedition or switch between saved expedition plans"
      onClose={onClose}
    >
      {errorMessage && (
        <div className="p-2.5 border border-[#ff3b2f] bg-[#190909] text-[#ff4d3d]">
          {errorMessage}
        </div>
      )}
      <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
        <div className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
          + Create New Expedition Plan
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          <input
            type="text"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Expedition Name (e.g. Colonia and Sagittarius A*)"
            className="bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 text-[#F4ECE4]"
          />
          <input
            type="text"
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            placeholder="Optional description..."
            className="bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 text-[#F4ECE4]"
          />
        </div>
        <div className="flex justify-end">
          <button
            type="button"
            onClick={handleCreate}
            className="ed-btn-primary"
          >
            Create Expedition
          </button>
        </div>
      </div>

      <div className="divide-y divide-[#24180e] border border-[#3d2816] bg-[#110c08]">
        {expeditions.map((expeditionItem) => (
          <div key={expeditionItem.id} className="p-2.5 flex items-center justify-between gap-2">
            {editingId === expeditionItem.id ? (
              <div className="flex-1 flex items-center gap-2">
                <input
                  type="text"
                  value={editName}
                  onChange={(event) => setEditName(event.target.value)}
                  className="bg-[#050506] border border-[#3d2816] px-2 py-1 text-[#F4ECE4]"
                />
                <input
                  type="text"
                  value={editDescription}
                  onChange={(event) => setEditDescription(event.target.value)}
                  className="flex-1 bg-[#050506] border border-[#3d2816] px-2 py-1 text-[#F4ECE4]"
                />
                <button
                  type="button"
                  onClick={() => handleSaveEdit(expeditionItem.id)}
                  className="ed-btn-cyan"
                >
                  Save
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
                        expeditionItem.id === activeId ? 'text-[#FF7100]' : 'text-[#F4ECE4]'
                      }`}
                    >
                      {expeditionItem.name}
                    </span>
                    <span className="text-[9px] font-mono-tabular uppercase px-1.5 py-0.5 bg-[#16100b] border border-[#3d2816] text-[#a6907c]">
                      {expeditionItem.status}
                    </span>
                  </div>
                  {expeditionItem.description && (
                    <div className="text-[11px] text-[#a6907c]">{expeditionItem.description}</div>
                  )}
                </button>
                <div className="flex items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => handleClone(expeditionItem.id)}
                    title="Clone this expedition into a new expedition in the Planning phase"
                    className="ed-btn-cyan"
                  >
                    Clone
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setEditingId(expeditionItem.id)
                      setEditName(expeditionItem.name)
                      setEditDescription(expeditionItem.description)
                    }}
                    className="ed-btn"
                  >
                    Rename
                  </button>
                  {expeditions.length > 1 && (
                    <button
                      type="button"
                      onClick={() => handleDelete(expeditionItem.id)}
                      className="ed-btn-danger"
                    >
                      Delete
                    </button>
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

