import { useState } from 'react'
import { modes } from '../../../../wailsjs/go/models'

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
        title="Active mode for this leg (click to change)"
        className="inline-flex items-center gap-2 px-2.5 py-0.5 text-[10px] font-mono-tabular uppercase tracking-wider border border-[#472e19] border-l-2 border-l-[#FF7100] bg-[#16100b] text-[#FF7100] hover:bg-[#24180e] transition cursor-pointer"
      >
        <span className="font-bold">[MODE]:</span>
        <span
          className="w-1.5 h-2.5 shrink-0"
          style={{ backgroundColor: modeColor || currentMode?.color || '#FF7100' }}
        />
        <span className="font-bold text-[#F4ECE4]">{currentMode?.name || 'TRAVEL'}</span>
        <svg
          className={`w-3 h-3 text-[#a6907c] transition-transform ${open ? 'rotate-180' : ''}`}
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
          <div className="absolute left-0 mt-1 w-48 border border-[#FF7100] bg-[#1c140d] shadow-2xl shadow-black z-40 py-1 text-xs">
            <div className="px-2.5 py-1 text-[9px] font-extrabold uppercase tracking-wider text-[#a6907c] bg-[#241a10] border-b border-[#3d2816]">
              Select Leg Mode
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
                      ? 'bg-[#2b190c] text-[#FF7100] font-bold'
                      : 'text-[#F4ECE4] hover:bg-[#FF7100] hover:text-[#050506]'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <span
                      className="w-2 h-2 shrink-0"
                      style={{ backgroundColor: mode.color || '#FF7100' }}
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

