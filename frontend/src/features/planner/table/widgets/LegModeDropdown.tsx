import { useState } from 'react'
import { modes } from '../../../../api/models'
import { ED_AMBER_HEX } from '../../../../domain/theme'

/**
 * LegModeDropdownProps defines the active flight mode metadata, available modes,
 * visual variant, optional cascade-forward eligibility, and selection callback
 * for a waypoint leg drawer or expedition header selector.
 */
export interface LegModeDropdownProps {
  modeId: number
  modeColor?: string
  modesList: modes.Mode[]
  variant?: 'drawer' | 'header'
  title?: string
  canCascade?: boolean
  onChangeMode: (nextModeId: number, cascade: boolean) => void
}

/**
 * LegModeDropdown renders a compact cockpit mode button that opens an inline
 * selector for reassigning the flight mode of an expedition header or leg
 * (optionally cascading across all subsequent visited legs).
 */
export function LegModeDropdown({
  modeId,
  modeColor,
  modesList,
  variant = 'drawer',
  title,
  canCascade = false,
  onChangeMode,
}: LegModeDropdownProps) {
  const [open, setOpen] = useState(false)
  const [applyOnward, setApplyOnward] = useState(false)
  const currentMode = modesList.find((mode) => mode.id === modeId) || modesList[0]
  const isHeader = variant === 'header'

  function closeMenu() {
    setOpen(false)
    setApplyOnward(false)
  }

  return (
    <div className="relative inline-flex">
      <button
        type="button"
        onClick={() => {
          setOpen((previousOpen) => {
            if (previousOpen) {
              setApplyOnward(false)
            }
            return !previousOpen
          })
        }}
        title={title ?? 'Flight mode for this leg (click to change)'}
        aria-label={isHeader ? 'Expedition flight mode' : undefined}
        className={
          isHeader
            ? 'inline-flex items-center gap-2 px-3 py-1.5 text-xs border border-ed-border border-l-2 border-l-ed-amber bg-ed-rail hover:bg-ed-hover transition cursor-pointer'
            : 'inline-flex items-center gap-2 px-2.5 py-0.5 text-[10px] font-mono-tabular uppercase tracking-wider border border-ed-border-strong border-l-2 border-l-ed-amber bg-ed-control text-ed-amber hover:bg-ed-hover transition cursor-pointer'
        }
      >
        {isHeader ? (
          <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted shrink-0">
            FLIGHT MODE:
          </span>
        ) : (
          <span className="font-bold">[FLIGHT MODE]:</span>
        )}
        <span
          className={isHeader ? 'w-1.5 h-3 shrink-0' : 'w-1.5 h-2.5 shrink-0'}
          style={{ backgroundColor: modeColor || currentMode?.color || ED_AMBER_HEX }}
        />
        <span
          className={
            isHeader
              ? 'font-mono-tabular font-bold uppercase tracking-wider text-ed-text-main'
              : 'font-bold text-ed-text-main'
          }
        >
          {currentMode?.name || 'TRAVEL'}
        </span>
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
          <div className="fixed inset-0 z-30" onClick={closeMenu} />
          <div
            className={`absolute top-full ${isHeader ? 'right-0 w-48' : 'left-0 w-56'} mt-1 border border-ed-amber bg-ed-menu shadow-2xl shadow-black z-40 py-1 text-xs`}
          >
            <div className="px-2.5 py-1 text-[9px] font-extrabold uppercase tracking-wider text-ed-text-muted bg-ed-hover border-b border-ed-border">
              Select Flight Mode
            </div>

            {canCascade && (
              <div className="px-2 py-1.5 border-b border-ed-border bg-ed-surface space-y-1">
                <div className="grid grid-cols-2 gap-1">
                  <button
                    type="button"
                    onClick={() => setApplyOnward(false)}
                    className={`px-2 py-1 text-[9px] font-mono-tabular font-bold uppercase tracking-wider border cursor-pointer transition ${
                      !applyOnward
                        ? 'bg-ed-amber text-ed-void border-ed-amber'
                        : 'bg-ed-void text-ed-text-muted border-ed-border hover:text-ed-text-main'
                    }`}
                  >
                    This Leg
                  </button>
                  <button
                    type="button"
                    onClick={() => setApplyOnward(true)}
                    title="Apply selected mode to this leg and all following visited legs (active flight mode stays unchanged)"
                    className={`px-2 py-1 text-[9px] font-mono-tabular font-bold uppercase tracking-wider border cursor-pointer transition ${
                      applyOnward
                        ? 'bg-ed-amber text-ed-void border-ed-amber'
                        : 'bg-ed-void text-ed-text-muted border-ed-border hover:text-ed-text-main'
                    }`}
                  >
                    From Here Onward
                  </button>
                </div>
                {applyOnward && (
                  <div className="text-[10px] leading-tight text-ed-text-muted">
                    Overwrites this leg and all following visited legs. Active flight mode stays unchanged.
                  </div>
                )}
              </div>
            )}

            {modesList.map((mode) => {
              const isSelected = mode.id === modeId && !applyOnward
              return (
                <button
                  key={mode.id}
                  type="button"
                  onClick={() => {
                    onChangeMode(mode.id, Boolean(canCascade && applyOnward))
                    closeMenu()
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
