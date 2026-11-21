import { useState } from 'react'
import { modes } from '../../../../api/models'
import { ED_AMBER_HEX } from '../../../../domain/theme'

/**
 * LegModeDropdownProps defines the active flight mode metadata, available modes,
 * and selection callback for a completed or in-progress expedition leg.
 */
export interface LegModeDropdownProps {
  modeId: number
  modeColor?: string
  modesList: modes.Mode[]
  onChangeMode: (nextModeId: number) => void
}

/**
 * LegModeDropdown renders a compact cockpit mode badge that opens an inline
 * selector for reassigning the flight mode of a specific expedition leg.
 */
export function LegModeDropdown({
  modeId,
  modeColor,
  modesList,
  onChangeMode,
}: LegModeDropdownProps) {
  const [open, setOpen] = useState(false)
  const currentMode = modesList.find((mode) => mode.id === modeId) || modesList[0]

  return (
    <div className="relative inline-block">
      <button
        type="button"
        onClick={() => setOpen((previousOpen) => !previousOpen)}
        title="Flight mode for this leg (click to change)"
        className="inline-flex items-center gap-2 px-2.5 py-0.5 text-[10px] font-mono-tabular uppercase tracking-wider border border-ed-border-strong border-l-2 border-l-ed-amber bg-ed-control text-ed-amber hover:bg-ed-hover transition cursor-pointer"
      >
        <span className="font-bold">[FLIGHT MODE]:</span>
        <span
          className="w-1.5 h-2.5 shrink-0"
          style={{ backgroundColor: modeColor || currentMode?.color || ED_AMBER_HEX }}
        />
        <span className="font-bold text-ed-text-main">{currentMode?.name || 'TRAVEL'}</span>
        <svg
          className={`w-3 h-3 text-ed-text-muted transition-transform ${open ? 'rotate-180' : ''}`}
          viewBox="0 0 20 20"
          fill="currentColor"
        >
          <path
            fillRule="evenodd"
            d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
            clipRule="evenodd"
          />
        </svg>
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-30" onClick={() => setOpen(false)} />
          <div className="absolute left-0 mt-1 w-48 border border-ed-amber bg-ed-menu shadow-2xl shadow-black z-40 py-1 text-xs">
            <div className="px-2.5 py-1 text-[9px] font-extrabold uppercase tracking-wider text-ed-text-muted bg-ed-hover border-b border-ed-border">
              Select Flight Mode
            </div>
            {modesList.map((mode) => {
              const isSelected = mode.id === modeId
              return (
                <button
                  key={mode.id}
                  type="button"
                  onClick={() => {
                    onChangeMode(mode.id)
                    setOpen(false)
                  }}
                  className={`w-full flex items-center justify-between px-3 py-1.5 text-left transition cursor-pointer ${
                    isSelected
                      ? 'bg-ed-amber-tint text-ed-amber font-bold'
                      : 'text-ed-text-main hover:bg-ed-amber hover:text-ed-void'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <span
                      className="w-2 h-2 shrink-0"
                      style={{ backgroundColor: mode.color || ED_AMBER_HEX }}
                    />
                    <span className="font-mono-tabular uppercase tracking-wider text-[11px]">
                      {mode.name}
                    </span>
                  </div>
                </button>
              )
            })}
          </div>
        </>
      )}
    </div>
  )
}

