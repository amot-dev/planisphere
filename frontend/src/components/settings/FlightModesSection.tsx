import { useState } from 'react'
import { CreateMode, DeleteMode, UpdateMode } from '../../../wailsjs/go/app/App'
import { app } from '../../../wailsjs/go/models'

/**
 * FlightModesSectionProps defines the current flight modes state and callbacks
 * for creating, recoloring, and deleting custom flight modes.
 */
export interface FlightModesSectionProps {
  modesState: app.ModesState | null
  onRefresh: () => void
  onSetError: (errorMessage: string) => void
}

/**
 * FlightModesSection renders the list of configured flight modes with color
 * pickers and a form to create custom flight modes.
 */
export function FlightModesSection({
  modesState,
  onRefresh,
  onSetError,
}: FlightModesSectionProps) {
  const [newModeName, setNewModeName] = useState('')
  const [newModeDescription, setNewModeDescription] = useState('')
  const [newModeColor, setNewModeColor] = useState('#FF7100')

  async function handleCreateMode() {
    onSetError('')
    try {
      await CreateMode(newModeName, newModeDescription, newModeColor)
      setNewModeName('')
      setNewModeDescription('')
      onRefresh()
    } catch (createError) {
      onSetError(String(createError))
    }
  }

  return (
    <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2.5">
      <div className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
        Flight Modes
      </div>
      <div className="divide-y divide-[#24180e] border border-[#3d2816] bg-[#050506]">
        {(modesState?.modes ?? []).map((modeItem) => (
          <div key={modeItem.id} className="p-2 flex items-center justify-between gap-3">
            <div className="flex items-center gap-2.5 min-w-0">
              <input
                type="color"
                value={modeItem.color}
                onChange={(event) =>
                  UpdateMode(
                    modeItem.id,
                    modeItem.name,
                    modeItem.description,
                    event.target.value,
                  ).then(onRefresh)
                }
                title={`Change color for ${modeItem.name}`}
                className="w-5 h-5 ed-color-picker shrink-0 transition-colors"
              />
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-bold uppercase tracking-wider text-[#F4ECE4]">
                    {modeItem.name}
                  </span>
                  {modeItem.isDefault && (
                    <span className="text-[9px] font-mono-tabular uppercase px-1.5 py-0.2 bg-[#16100b] border border-[#3d2816] text-[#a6907c]">
                      DEFAULT
                    </span>
                  )}
                </div>
                {modeItem.description && (
                  <div className="text-[10px] text-[#a6907c] truncate">{modeItem.description}</div>
                )}
              </div>
            </div>
            {!modeItem.isDefault && (
              <button
                type="button"
                onClick={() => DeleteMode(modeItem.id).then(onRefresh)}
                className="ed-btn-danger"
              >
                Delete
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
          placeholder="New mode name..."
          className="bg-[#050506] border border-[#3d2816] px-2.5 py-1 text-[#F4ECE4]"
        />
        <input
          type="text"
          value={newModeDescription}
          onChange={(event) => setNewModeDescription(event.target.value)}
          placeholder="Optional description..."
          className="bg-[#050506] border border-[#3d2816] px-2.5 py-1 text-[#F4ECE4]"
        />
        <div className="flex items-center gap-2">
          <input
            type="color"
            value={newModeColor}
            onChange={(event) => setNewModeColor(event.target.value)}
            title="Select mode color"
            className="w-7 h-7 ed-color-picker shrink-0 transition-colors"
          />
          <button
            type="button"
            onClick={handleCreateMode}
            className="flex-1 ed-btn-primary"
          >
            Add Mode
          </button>
        </div>
      </div>
    </div>
  )
}

