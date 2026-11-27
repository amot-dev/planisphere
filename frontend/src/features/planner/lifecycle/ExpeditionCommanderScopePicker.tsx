import { useId } from 'react'
import { replay } from '../../../api/models'
import { CommanderBadges } from '../../../components/ui/CommanderBadges'

interface ExpeditionCommanderScopePickerProps {
  commanders: replay.Commander[]
  selectedProfileId: number
  disabled?: boolean
  onChangeProfileId: (nextProfileId: number) => void
}

/**
 * resolveSelectedCommanderProfileId returns commanderProfileId when it matches an
 * entry in commanders, falling back to the most recently active commander
 * (enabled first, then latest lastSeen, then highest ID).
 */
export function resolveSelectedCommanderProfileId(
  commanders: replay.Commander[],
  commanderProfileId?: number,
): number {
  if (commanderProfileId && commanderProfileId > 0 && commanders.some((commander) => commander.id === commanderProfileId)) {
    return commanderProfileId
  }
  let latestCommander: replay.Commander | undefined
  for (const commander of commanders) {
    if (commander.id <= 0) continue
    if (!latestCommander) {
      latestCommander = commander
      continue
    }
    if (commander.enabled !== latestCommander.enabled) {
      if (commander.enabled) latestCommander = commander
      continue
    }
    const currentLastSeen = commander.lastSeen ?? ''
    const bestLastSeen = latestCommander.lastSeen ?? ''
    if (currentLastSeen > bestLastSeen || (currentLastSeen === bestLastSeen && commander.id > latestCommander.id)) {
      latestCommander = commander
    }
  }
  return latestCommander?.id ?? 0
}

/**
 * ExpeditionCommanderScopePicker renders a single-commander save-slot selector
 * whenever multiple commander profiles exist.
 */
export function ExpeditionCommanderScopePicker({
  commanders,
  selectedProfileId,
  disabled = false,
  onChangeProfileId,
}: ExpeditionCommanderScopePickerProps) {
  const radioGroupName = useId()
  if (commanders.length <= 1) return null

  return (
    <div className="space-y-2">
      <div className="font-bold uppercase tracking-wider text-ed-text-main">Commander Save Slot</div>
      <div role="radiogroup" aria-label="Expedition commander save slot" className="divide-y divide-ed-border-subtle border border-ed-border bg-ed-void">
        {commanders.map((commander) => {
          const isSelected = commander.id === selectedProfileId
          return (
            <label
              key={commander.profileKey || commander.id || commander.name}
              className={`px-2.5 py-1.5 flex items-center justify-between gap-2 transition-colors ${
                disabled ? 'cursor-not-allowed opacity-60' : isSelected ? 'cursor-pointer bg-ed-amber-tint/30' : 'cursor-pointer hover:bg-ed-control'
              }`}
            >
              <div className="flex items-center gap-2 min-w-0">
                <input type="radio" name={radioGroupName} checked={isSelected} disabled={disabled} onChange={() => onChangeProfileId(commander.id)} className="sr-only" />
                <span aria-hidden="true" className={`w-3 h-3 shrink-0 border flex items-center justify-center ${isSelected ? 'border-ed-amber bg-ed-amber-tint' : 'border-ed-border-strong bg-ed-surface'}`}>
                  {isSelected && <span className="w-1.5 h-1.5 bg-ed-amber" />}
                </span>
                <span className={`font-bold uppercase tracking-wider truncate ${isSelected ? 'text-ed-amber' : 'text-ed-text-main'}`}>CMDR {commander.name}</span>
                <CommanderBadges universe={commander.universe} isArchivedCareer={commander.isArchivedCareer} hasManualOverride={commander.hasManualOverride} />
                {commander.fid && <span className="text-[10px] font-mono-tabular text-ed-text-muted shrink-0">{commander.fid}</span>}
              </div>
              <span className="text-[10px] font-mono-tabular text-ed-text-muted shrink-0">{(commander.eventCount ?? 0).toLocaleString()} events</span>
            </label>
          )
        })}
      </div>
    </div>
  )
}

/**
 * ScopedExpeditionInlineEditor renders the multi-commander inline expedition
 * metadata and save-slot selector inside ExpeditionManageModal.
 */
export function ScopedExpeditionInlineEditor(props: {
  editName: string; editDescription: string; commanders: replay.Commander[]
  selectedProfileId: number; busy: boolean
  onChangeName: (nextName: string) => void; onChangeDescription: (nextDescription: string) => void
  onChangeProfileId: (nextProfileId: number) => void
  onCancel: () => void; onSave: () => void
}) {
  return (
    <div className="flex-1 space-y-2.5">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
        <input type="text" disabled={props.busy} value={props.editName} onChange={(event) => props.onChangeName(event.target.value)} className="bg-ed-void border border-ed-border px-2 py-1 text-ed-text-main" />
        <input type="text" disabled={props.busy} value={props.editDescription} onChange={(event) => props.onChangeDescription(event.target.value)} className="bg-ed-void border border-ed-border px-2 py-1 text-ed-text-main" />
      </div>
      <ExpeditionCommanderScopePicker commanders={props.commanders} selectedProfileId={props.selectedProfileId} disabled={props.busy} onChangeProfileId={props.onChangeProfileId} />
      <div className="flex items-center justify-end gap-1.5">
        <button type="button" disabled={props.busy} onClick={props.onCancel} className="ed-btn">CANCEL</button>
        <button type="button" disabled={props.busy} onClick={props.onSave} className="ed-btn-cyan">SAVE</button>
      </div>
    </div>
  )
}
