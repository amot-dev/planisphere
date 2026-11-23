import { app, modes, stats } from '../../api/models'
import { SquareCheckbox } from '../../components/ui/SquareCheckbox'

/**
 * ModeFilterPanelProps defines the flight modes state, telemetry filter, and
 * collapse callback for the top panel of the telemetry sidebar.
 */
export interface ModeFilterPanelProps {
  modesState: app.ModesState | null
  filter: stats.Filter
  onFilterChange: (nextFilter: stats.Filter) => void
  onToggleCollapse?: () => void
}

/**
 * ModeFilterPanel renders the tri-state telemetry mode filter checkboxes
 * (All Data, Scans Only, Off) and the sidebar collapse button.
 */
export function ModeFilterPanel({
  modesState,
  filter,
  onFilterChange,
  onToggleCollapse,
}: ModeFilterPanelProps) {
  const modeList: modes.Mode[] = modesState?.modes ?? []

  function getSelection(modeId: number): stats.ModeSelection {
    const found = filter.modes?.find((modeSelection) => modeSelection.modeId === modeId)
    if (found) return found
    return stats.ModeSelection.createFrom({ modeId, travel: true, discovery: true })
  }

  function setModeSelection(modeId: number, travel: boolean, discovery: boolean) {
    const allIds = modeList.map((mode) => mode.id)
    const updated = allIds.map((targetModeId) => {
      if (targetModeId === modeId) {
        return stats.ModeSelection.createFrom({ modeId: targetModeId, travel, discovery })
      }
      return getSelection(targetModeId)
    })
    const anyHidden = updated.some((selection) => !selection.travel || !selection.discovery)
    onFilterChange(
      stats.Filter.createFrom({
        enabled: anyHidden,
        modes: updated,
      }),
    )
  }

  function cycleModeSelection(modeId: number) {
    const selection = getSelection(modeId)
    if (selection.travel && selection.discovery) {
      // Full -> Partial (Scans and Exobio only)
      setModeSelection(modeId, false, true)
    } else if (!selection.travel && selection.discovery) {
      // Partial -> Excluded (None)
      setModeSelection(modeId, false, false)
    } else {
      // Excluded -> Full (All)
      setModeSelection(modeId, true, true)
    }
  }

  function resetAllModes() {
    onFilterChange(
      stats.Filter.createFrom({
        enabled: false,
        modes: modeList.map((modeItem) => ({
          modeId: modeItem.id,
          travel: true,
          discovery: true,
        })),
      }),
    )
  }

  return (
    <div className="p-3 border-b border-ed-border bg-ed-card space-y-2">
      {/* Mode Filter Checkboxes (Tri-state: All -> Scans Only -> Off) */}
      <div className="space-y-1.5">
        <div className="flex items-center justify-between gap-2 text-[10px] font-bold uppercase tracking-[0.12em] text-ed-text-muted">
          <span title="Filter expedition sidebar metrics by each leg's flight mode">
            Telemetry Filter
          </span>
          <div className="flex items-center gap-2">
            {filter.enabled && (
              <button
                type="button"
                onClick={resetAllModes}
                className="text-ed-amber hover:underline cursor-pointer"
                title="Include all flight modes in sidebar metrics"
              >
                [Reset]
              </button>
            )}
            {onToggleCollapse && (
              <button
                type="button"
                onClick={onToggleCollapse}
                title="Collapse Telemetry Sidebar"
                className="h-[26px] w-[26px] flex items-center justify-center text-ed-text-muted hover:text-ed-amber bg-ed-header border border-ed-border hover:bg-ed-hover cursor-pointer shrink-0"
              >
                <span>◀</span>
              </button>
            )}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-1.5">
          {modeList.map((mode) => {
            const selection = getSelection(mode.id)
            const isFull = selection.travel && selection.discovery
            const isPartial = !selection.travel && selection.discovery

            return (
              <button
                key={mode.id}
                type="button"
                onClick={() => cycleModeSelection(mode.id)}
                className={`flex items-center gap-2 px-2 py-1 border text-xs cursor-pointer transition select-none text-left ${
                  isFull
                    ? 'bg-ed-control border-ed-border-strong border-l-2 border-l-ed-amber text-ed-text-main'
                    : isPartial
                      ? 'bg-ed-cyan-tint border-ed-cyan/60 border-l-2 border-l-ed-cyan text-ed-text-main'
                      : 'bg-ed-void border-ed-border-subtle text-ed-text-dim opacity-60 hover:opacity-100'
                }`}
              >
                {/* Tri-state indicator badge */}
                <SquareCheckbox
                  checked={isFull ? true : isPartial ? 'partial' : false}
                  size="sm"
                />

                <span
                  className="w-1.5 h-3 shrink-0"
                  style={{ backgroundColor: mode.color }}
                />
                <span className="truncate font-bold uppercase tracking-wider text-[11px] flex-1">
                  {mode.name}
                </span>
              </button>
            )
          })}
        </div>

        {/* Tri-state explanation */}
        <div className="pt-1.5 border-t border-ed-border-subtle text-[10px] text-ed-text-muted leading-tight">
          <div className="flex items-center gap-2 font-mono-tabular text-[9px]">
            <span className="inline-flex items-center gap-1 text-ed-amber font-bold">
              <SquareCheckbox checked={true} size="xs" /> ALL DATA
            </span>
            <span>·</span>
            <span className="inline-flex items-center gap-1 text-ed-cyan font-bold">
              <SquareCheckbox checked="partial" size="xs" /> SCANS ONLY
            </span>
            <span>·</span>
            <span className="inline-flex items-center gap-1 text-ed-text-dim">
              <SquareCheckbox checked={false} size="xs" /> OFF
            </span>
          </div>
        </div>
      </div>
    </div>
  )
}
