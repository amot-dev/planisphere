import { app, modes, stats } from '../../../wailsjs/go/models'
import { SquareCheckbox } from '../common/SquareCheckbox'

/**
 * ModeFilterPanelProps defines the flight modes state, telemetry filter, and
 * mode-switching / collapse callbacks for the top panel of the telemetry sidebar.
 */
export interface ModeFilterPanelProps {
  modesState: app.ModesState | null
  filter: stats.Filter
  onFilterChange: (nextFilter: stats.Filter) => void
  onQuickSwitchMode: (modeId: number) => void
  onToggleCollapse?: () => void
}

/**
 * ModeFilterPanel renders the active flight mode selector and tri-state
 * telemetry mode filter checkboxes (All Data, Scans Only, Off).
 */
export function ModeFilterPanel({
  modesState,
  filter,
  onFilterChange,
  onQuickSwitchMode,
  onToggleCollapse,
}: ModeFilterPanelProps) {
  const modeList: modes.Mode[] = modesState?.modes ?? []
  const travelMode = modeList.find((modeItem) => modeItem.name.toLowerCase() === 'travel')
  const activeModeId = modesState?.activeModeId || travelMode?.id || modeList[0]?.id || 0

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
    <div className="p-3 border-b border-[#3d2816] bg-[#110c08] space-y-2">
      {/* Live Mode Quick Switcher */}
      <div className="flex items-center gap-2 text-xs">
        <span className="text-[10px] font-bold uppercase tracking-[0.12em] text-[#a6907c] shrink-0">
          Active Mode
        </span>
        <select
          value={activeModeId}
          onChange={(event) => onQuickSwitchMode(Number(event.target.value))}
          title="Current gameplay mode (logs newly recorded jumps and scans to this mode)"
          className="flex-1 bg-[#050506] border border-[#472e19] border-l-2 border-l-[#FF7100] px-2.5 py-1 text-xs font-bold uppercase tracking-wider text-[#F4ECE4] focus:border-[#FF7100] focus:outline-none cursor-pointer"
        >
          {modeList.map((mode) => (
            <option key={mode.id} value={mode.id} className="bg-[#0b0907] text-[#F4ECE4]">
              {mode.name}
            </option>
          ))}
        </select>
        {onToggleCollapse && (
          <button
            type="button"
            onClick={onToggleCollapse}
            title="Collapse Telemetry Sidebar"
            className="h-[26px] w-[26px] flex items-center justify-center text-[#a6907c] hover:text-[#FF7100] bg-[#140e09] border border-[#3d2816] hover:bg-[#1f150d] cursor-pointer shrink-0"
          >
            ◀
          </button>
        )}
      </div>

      {/* Mode Filter Checkboxes (Tri-state: All -> Scans Only -> Off) */}
      <div className="space-y-1.5 pt-1.5 border-t border-[#2e1f12]">
        <div className="flex items-center justify-between text-[10px] font-bold uppercase tracking-[0.12em] text-[#a6907c]">
          <span title="Filter sidebar metrics by which mode was active when events were logged">
            Telemetry Filter
          </span>
          {filter.enabled && (
            <button
              type="button"
              onClick={resetAllModes}
              className="text-[#FF7100] hover:underline cursor-pointer"
              title="Include all modes in sidebar metrics"
            >
              [Reset]
            </button>
          )}
        </div>

        <div className="grid grid-cols-2 gap-1.5">
          {modeList.map((mode) => {
            const selection = getSelection(mode.id)
            const isFull = selection.travel && selection.discovery
            const isPartial = !selection.travel && selection.discovery

            const titleText = isFull
              ? `${mode.name}: All telemetry included. Click to switch to Scans and Exobio only.`
              : isPartial
                ? `${mode.name}: Scans and Exobio only (jumps/Ly hidden). Click to exclude.`
                : `${mode.name}: Excluded from metrics. Click to include all.`

            return (
              <button
                key={mode.id}
                type="button"
                onClick={() => cycleModeSelection(mode.id)}
                title={titleText}
                className={`flex items-center gap-2 px-2 py-1 border text-xs cursor-pointer transition select-none text-left ${
                  isFull
                    ? 'bg-[#18110b] border-[#472e19] border-l-2 border-l-[#FF7100] text-[#F4ECE4]'
                    : isPartial
                      ? 'bg-[#08151c] border-[#00D2FF]/60 border-l-2 border-l-[#00D2FF] text-[#F4ECE4]'
                      : 'bg-[#060504] border-[#24180e] text-[#6e5b4b] opacity-60 hover:opacity-100'
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
        <div className="pt-1.5 border-t border-[#24180e] text-[10px] text-[#a6907c] leading-tight space-y-1">
          <div className="flex items-center gap-2 font-mono-tabular text-[9px]">
            <span className="inline-flex items-center gap-1 text-[#FF7100] font-bold">
              <SquareCheckbox checked={true} size="xs" /> ALL DATA
            </span>
            <span>·</span>
            <span className="inline-flex items-center gap-1 text-[#00D2FF] font-bold">
              <SquareCheckbox checked="partial" size="xs" /> SCANS ONLY
            </span>
            <span>·</span>
            <span className="inline-flex items-center gap-1 text-[#6e5b4b]">
              <SquareCheckbox checked={false} size="xs" /> OFF
            </span>
          </div>
          <p className="text-[9px] text-[#6e5b4b] flex items-center gap-1 flex-wrap">
            <span>Partial</span>
            <SquareCheckbox checked="partial" size="xs" />
            <span>includes scans, mapping and exobio, excluding travel distance/jumps.</span>
          </p>
        </div>
      </div>
    </div>
  )
}

