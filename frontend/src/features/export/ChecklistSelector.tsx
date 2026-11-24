import { useMemo } from 'react'
import { SquareCheckbox } from '../../components/ui/SquareCheckbox'

/** Metadata for a single toggleable entry inside ChecklistSelector. */
export interface ChecklistItem<T extends string> {
  id: T
  label: string
  available?: boolean
  disabledTooltip?: string
}

/** Props for the reusable Select All and Select None scrollable checkbox list. */
export interface ChecklistSelectorProps<T extends string> {
  label: string
  items: ReadonlyArray<ChecklistItem<T>>
  selectedIds: ReadonlyArray<T>
  disabled?: boolean
  maxHeightClassName?: string
  onChangeSelectedIds: (nextSelectedIds: T[]) => void
}

/**
 * ChecklistSelector renders a section header with [All] and [None] actions above a
 * scrollable list of SquareCheckbox toggle rows with optional availability gating.
 */
export function ChecklistSelector<T extends string>(props: ChecklistSelectorProps<T>) {
  const { label, items, selectedIds, disabled = false, maxHeightClassName = 'max-h-48', onChangeSelectedIds } = props
  const selectedIdSet = useMemo(() => new Set<T>(selectedIds), [selectedIds])

  function handleToggleItem(itemId: T) {
    const nextSelectedIds = selectedIdSet.has(itemId)
      ? selectedIds.filter((existingId) => existingId !== itemId)
      : [...selectedIds, itemId]
    onChangeSelectedIds(nextSelectedIds)
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-1.5">
        <span className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted">{label}</span>
        <div className="flex items-center gap-2 text-[10px] font-mono-tabular">
          <button
            type="button"
            disabled={disabled}
            onClick={() => onChangeSelectedIds(items.map((item) => item.id))}
            className={`text-ed-amber ${disabled ? 'opacity-40 cursor-not-allowed' : 'hover:underline cursor-pointer'}`}
          >
            [All]
          </button>
          <button
            type="button"
            disabled={disabled}
            onClick={() => onChangeSelectedIds([])}
            className={`text-ed-text-muted ${disabled ? 'opacity-40 cursor-not-allowed' : 'hover:text-ed-text-main hover:underline cursor-pointer'}`}
          >
            [None]
          </button>
        </div>
      </div>
      <div className={`space-y-1 ${maxHeightClassName} overflow-y-auto pr-1`}>
        {items.map((item) => (
          <ChecklistItemButton key={item.id} item={item} checked={selectedIdSet.has(item.id)} disabled={disabled} onToggle={handleToggleItem} />
        ))}
      </div>
    </div>
  )
}

function ChecklistItemButton<T extends string>(props: {
  item: ChecklistItem<T>
  checked: boolean
  disabled: boolean
  onToggle: (itemId: T) => void
}) {
  const { item, checked, disabled, onToggle } = props
  const available = item.available ?? true
  const isItemDisabled = disabled || !available
  const stateClasses = !available
    ? 'bg-ed-void border-ed-border-subtle text-ed-text-disabled opacity-50 cursor-not-allowed'
    : `${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'} ${
        checked ? 'bg-ed-control border-ed-border-strong text-ed-text-main' : 'bg-ed-void border-ed-border-subtle text-ed-text-muted hover:text-ed-text-main'
      }`

  return (
    <button
      type="button"
      disabled={isItemDisabled}
      title={!available ? item.disabledTooltip : undefined}
      onClick={() => onToggle(item.id)}
      className={`w-full flex items-center gap-2 px-2 py-1 border text-left text-[11px] ${stateClasses}`}
    >
      <SquareCheckbox checked={checked && available} size="xs" />
      <span className="truncate">{item.label}</span>
    </button>
  )
}
