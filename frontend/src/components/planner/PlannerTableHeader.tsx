import { useState } from 'react'
import { SquareCheckbox } from '../common/SquareCheckbox'

/**
 * PlannerTableHeaderProps defines the planning-mode flag and optional column
 * visibility state and toggles for the expedition flight planner table header.
 */
export interface PlannerTableHeaderProps {
  isPlanning: boolean
  showNotes: boolean
  showCarrierStop: boolean
  onToggleShowNotes: () => void
  onToggleShowCarrierStop: () => void
}

/**
 * PlannerTableHeader renders the sticky column header row and visible-columns
 * customization dropdown for the expedition flight planner table.
 */
export function PlannerTableHeader({
  isPlanning,
  showNotes,
  showCarrierStop,
  onToggleShowNotes,
  onToggleShowCarrierStop,
}: PlannerTableHeaderProps) {
  const [columnMenuOpen, setColumnMenuOpen] = useState(false)

  return (
    <thead className="sticky top-0 z-20 bg-[#120d09] border-b-2 border-[#FF7100]/70 text-[10px] font-extrabold uppercase tracking-[0.14em] text-[#a6907c] select-none">
      <tr className="align-top">
        {!isPlanning && (
          <th
            className="w-8 px-1.5 py-2.5 text-center align-top border-b border-[#FF7100]/70"
            title="Expand Leg Telemetry"
          >
            Leg
          </th>
        )}
        <th className="w-10 px-1.5 py-2.5 text-right font-mono-tabular align-top border-b border-[#FF7100]/70">
          #
        </th>
        <th className="w-60 px-3 py-2.5 align-top border-b border-[#FF7100]/70">Star System</th>
        <th className="w-48 px-3 py-2.5 align-top border-b border-[#FF7100]/70">POI / Body</th>
        {showNotes && (
          <th className="w-56 px-3 py-2.5 align-top border-b border-[#FF7100]/70">Flight Notes</th>
        )}
        {showCarrierStop && (
          <th
            className="w-40 px-2 py-2.5 text-center align-top border-b border-[#FF7100]/70"
            title="Fleet Carrier Stop, Arrival Status and Spansh Router"
          >
            Carrier Stop
          </th>
        )}
        <th className="w-60 px-3 py-2.5 align-top border-b border-[#FF7100]/70">
          Telemetry / Timestamp
        </th>
        <th className="w-[380px] px-3 py-2.5 text-right whitespace-nowrap align-top border-b border-[#FF7100]/70">
          <div className="inline-flex items-center justify-end gap-2">
            <span>Controls</span>
            <div className="relative inline-block text-left">
              <button
                type="button"
                onClick={() => setColumnMenuOpen((previousOpen) => !previousOpen)}
                title="Customize visible columns"
                className="h-5 w-5 inline-flex items-center justify-center p-0 cursor-pointer bg-transparent transition-colors group"
              >
                <svg
                  viewBox="0 0 16 16"
                  className="w-4 h-4 pointer-events-none shrink-0"
                  fill="none"
                  xmlns="http://www.w3.org/2000/svg"
                >
                  {/* Outer outline border matching SquareCheckbox */}
                  <rect
                    x="1"
                    y="1"
                    width="14"
                    height="14"
                    fill="#050506"
                    stroke={columnMenuOpen ? '#FF7100' : '#3d2816'}
                    strokeWidth="1.5"
                    className="group-hover:stroke-[#FF7100] transition-colors"
                  />
                  {/* Inner 4-square grid matching the 8x8 active footprint and ratio of SquareCheckbox */}
                  <rect
                    x="4"
                    y="4"
                    width="3.5"
                    height="3.5"
                    fill={columnMenuOpen ? '#FF7100' : '#a6907c'}
                    className="group-hover:fill-[#FF7100] transition-colors"
                  />
                  <rect
                    x="8.5"
                    y="4"
                    width="3.5"
                    height="3.5"
                    fill={columnMenuOpen ? '#FF7100' : '#a6907c'}
                    className="group-hover:fill-[#FF7100] transition-colors"
                  />
                  <rect
                    x="4"
                    y="8.5"
                    width="3.5"
                    height="3.5"
                    fill={columnMenuOpen ? '#FF7100' : '#a6907c'}
                    className="group-hover:fill-[#FF7100] transition-colors"
                  />
                  <rect
                    x="8.5"
                    y="8.5"
                    width="3.5"
                    height="3.5"
                    fill={columnMenuOpen ? '#FF7100' : '#a6907c'}
                    className="group-hover:fill-[#FF7100] transition-colors"
                  />
                </svg>
              </button>
              {columnMenuOpen && (
                <>
                  <div
                    className="fixed inset-0 z-30 cursor-default"
                    onClick={() => setColumnMenuOpen(false)}
                  />
                  <div className="absolute right-0 top-full mt-1 w-48 border border-[#FF7100] bg-[#140e09] shadow-2xl shadow-black z-40 p-2.5 text-left text-xs space-y-2 font-normal">
                    <div className="text-[9px] font-extrabold uppercase tracking-wider text-[#a6907c] pb-1 border-b border-[#2e1f12]">
                      Visible Columns
                    </div>
                    <label className="flex items-center gap-2 cursor-pointer text-[#F4ECE4] hover:text-[#FF7100] py-0.5 select-none">
                      <SquareCheckbox
                        checked={showCarrierStop}
                        onChange={onToggleShowCarrierStop}
                        size="sm"
                        color="#FF7100"
                      />
                      <span className="text-[11px] font-mono-tabular uppercase">Carrier Stop</span>
                    </label>
                    <label className="flex items-center gap-2 cursor-pointer text-[#F4ECE4] hover:text-[#FF7100] py-0.5 select-none">
                      <SquareCheckbox
                        checked={showNotes}
                        onChange={onToggleShowNotes}
                        size="sm"
                        color="#FF7100"
                      />
                      <span className="text-[11px] font-mono-tabular uppercase">Flight Notes</span>
                    </label>
                  </div>
                </>
              )}
            </div>
          </div>
        </th>
      </tr>
    </thead>
  )
}

