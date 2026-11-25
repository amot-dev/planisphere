import { replay } from '../../../api/models'
import { CommanderBadges } from '../../../components/ui/CommanderBadges'
import { SquareCheckbox } from '../../../components/ui/SquareCheckbox'

/**
 * CommanderScopeMode identifies whether an expedition includes all globally
 * enabled commander save slots or an explicit subset of profile IDs.
 */
export type CommanderScopeMode = 'all_enabled' | 'selected'

/**
 * ExpeditionCommanderScopePickerProps defines the available commander profiles,
 * active scope mode, selected profile IDs, and change callbacks.
 */
export interface ExpeditionCommanderScopePickerProps {
  commanders: replay.Commander[]
  scopeMode: CommanderScopeMode
  selectedProfileIds: number[]
  disabled?: boolean
  onChangeScopeMode: (nextMode: CommanderScopeMode) => void
  onChangeSelectedProfileIds: (nextProfileIds: number[]) => void
}

function defaultSelectedCommanderIds(commanders: replay.Commander[]): number[] {
  const enabledIds = commanders.filter((commander) => commander.enabled && commander.id > 0).map((commander) => commander.id)
  if (enabledIds.length > 0) return enabledIds
  const firstValid = commanders.find((commander) => commander.id > 0)
  return firstValid ? [firstValid.id] : []
}

/**
 * ExpeditionCommanderScopePicker renders a two-mode save-slot selector and
 * per-profile checkboxes whenever multiple commander profiles exist.
 */
export function ExpeditionCommanderScopePicker({
  commanders,
  scopeMode,
  selectedProfileIds,
  disabled = false,
  onChangeScopeMode,
  onChangeSelectedProfileIds,
}: ExpeditionCommanderScopePickerProps) {
  if (commanders.length <= 1) return null

  function handleSelectMode(nextMode: CommanderScopeMode) {
    if (disabled || nextMode === scopeMode) return
    onChangeScopeMode(nextMode)
    if (nextMode === 'selected' && selectedProfileIds.length === 0) {
      onChangeSelectedProfileIds(defaultSelectedCommanderIds(commanders))
    }
  }

  function handleToggleProfile(profileId: number, checked: boolean) {
    const nextIds = checked
      ? Array.from(new Set([...selectedProfileIds, profileId]))
      : selectedProfileIds.filter((existingId) => existingId !== profileId)
    onChangeSelectedProfileIds(nextIds)
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="font-bold uppercase tracking-wider text-ed-text-main">Save-Slot Scope</span>
        <div role="group" aria-label="Expedition save-slot scope" className="inline-flex gap-1">
          {([
            { value: 'selected', label: 'Selected Save Slot(s)' },
            { value: 'all_enabled', label: 'All Enabled Save Slots' },
          ] as const).map((optionItem) => {
            const isSelected = scopeMode === optionItem.value
            return (
              <button
                key={optionItem.value} type="button" disabled={disabled} aria-pressed={isSelected}
                onClick={() => handleSelectMode(optionItem.value)}
                className={`px-2.5 py-1 text-[10px] font-mono-tabular font-bold uppercase tracking-wider border transition-colors cursor-pointer whitespace-nowrap disabled:opacity-40 disabled:cursor-not-allowed ${
                  isSelected ? 'bg-ed-amber-tint text-ed-amber border-ed-amber' : 'bg-ed-surface text-ed-text-muted border-ed-border-dim hover:text-ed-text-main hover:border-ed-border-strong'
                }`}
              >
                {optionItem.label}
              </button>
            )
          })}
        </div>
      </div>
      {scopeMode === 'selected' && (
        <div className="divide-y divide-ed-border-subtle border border-ed-border bg-ed-void">
          {commanders.map((commander) => (
            <label key={commander.profileKey || commander.id || commander.name} className={`px-2.5 py-1.5 flex items-center justify-between gap-2 ${disabled ? 'cursor-not-allowed' : 'cursor-pointer hover:bg-ed-control'}`}>
              <div className="flex items-center gap-2 min-w-0">
                <SquareCheckbox size="sm" checked={selectedProfileIds.includes(commander.id)} disabled={disabled} onChange={(checked) => handleToggleProfile(commander.id, checked)} />
                <span className="font-bold uppercase tracking-wider text-ed-text-main truncate">CMDR {commander.name}</span>
                <CommanderBadges universe={commander.universe} isArchivedCareer={commander.isArchivedCareer} hasManualOverride={commander.hasManualOverride} />
                {commander.fid && <span className="text-[10px] font-mono-tabular text-ed-text-muted shrink-0">{commander.fid}</span>}
              </div>
              <span className="text-[10px] font-mono-tabular text-ed-text-muted shrink-0">{(commander.eventCount ?? 0).toLocaleString()} events</span>
            </label>
          ))}
          {selectedProfileIds.length === 0 && <div className="px-2.5 py-1.5 text-[10px] text-ed-danger-text">Select at least one save slot.</div>}
        </div>
      )}
    </div>
  )
}

/**
 * ScopedExpeditionInlineEditor renders the multi-commander inline expedition
 * metadata and save-slot scope editor inside ExpeditionManageModal.
 */
export function ScopedExpeditionInlineEditor(props: {
  editName: string; editDescription: string; commanders: replay.Commander[]
  scopeMode: CommanderScopeMode; selectedProfileIds: number[]; busy: boolean
  onChangeName: (nextName: string) => void; onChangeDescription: (nextDescription: string) => void
  onChangeScopeMode: (nextMode: CommanderScopeMode) => void; onChangeSelectedProfileIds: (nextIds: number[]) => void
  onCancel: () => void; onSave: () => void
}) {
  return (
    <div className="flex-1 space-y-2.5">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
        <input type="text" disabled={props.busy} value={props.editName} onChange={(event) => props.onChangeName(event.target.value)} className="bg-ed-void border border-ed-border px-2 py-1 text-ed-text-main" />
        <input type="text" disabled={props.busy} value={props.editDescription} onChange={(event) => props.onChangeDescription(event.target.value)} className="bg-ed-void border border-ed-border px-2 py-1 text-ed-text-main" />
      </div>
      <ExpeditionCommanderScopePicker commanders={props.commanders} scopeMode={props.scopeMode} selectedProfileIds={props.selectedProfileIds} disabled={props.busy} onChangeScopeMode={props.onChangeScopeMode} onChangeSelectedProfileIds={props.onChangeSelectedProfileIds} />
      <div className="flex items-center justify-end gap-1.5">
        <button type="button" disabled={props.busy} onClick={props.onCancel} className="ed-btn">CANCEL</button>
        <button type="button" disabled={props.busy} onClick={props.onSave} className="ed-btn-cyan">SAVE</button>
      </div>
    </div>
  )
}
