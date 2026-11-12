import { app, expedition } from '../../../wailsjs/go/models'
import { APP_PRODUCT_NAME, APP_TAGLINE } from '../../version'
import { GalacticClockBadge } from './GalacticClockBadge'

/**
 * CockpitTopBarProps defines the sidebar collapse state, expedition selector
 * data, live ingestion telemetry, and modal trigger callbacks for the top bar.
 */
export interface CockpitTopBarProps {
  sidebarCollapsed: boolean
  expeditions: expedition.Expedition[]
  activeExpeditionId: number
  activeStatusMessage: string | null
  ingest: app.IngestStatus | null
  onSelectExpedition: (selectedExpeditionId: number) => void
  onOpenExpeditionsModal: () => void
  onOpenSettingsModal: () => void
}

/**
 * CockpitTopBar renders the persistent 48px top cockpit header containing the
 * application emblem, Galactic Standard Time clock, expedition switcher, and status strip.
 */
export function CockpitTopBar({
  sidebarCollapsed,
  expeditions,
  activeExpeditionId,
  activeStatusMessage,
  ingest,
  onSelectExpedition,
  onOpenExpeditionsModal,
  onOpenSettingsModal,
}: CockpitTopBarProps) {
  return (
    <header className="relative h-12 shrink-0 bg-[#0b0907] border-b border-[#3d2816] border-t-2 border-t-[#FF7100] flex items-center">
      {/* Telemetry Sidebar Header Column */}
      <div
        className={`${
          sidebarCollapsed ? 'w-auto' : 'w-[390px]'
        } px-4 shrink-0 h-full flex items-center transition-all`}
      >
        <div className="flex items-center gap-2.5 shrink-0">
          <div className="w-2 h-5 bg-[#FF7100]" />
          <div className="leading-none">
            <div className="flex items-center gap-2">
              <span className="text-[11px] font-extrabold tracking-[0.18em] uppercase text-[#FF7100]">
                {APP_PRODUCT_NAME}
              </span>
              <GalacticClockBadge />
            </div>
            <div className="text-[9px] font-mono-tabular uppercase tracking-[0.14em] text-[#a6907c] mt-0.5">
              {APP_TAGLINE}
            </div>
          </div>
        </div>
      </div>

      {/* Expedition Planner Header Row (flex-1, px-5 aligned with ExpeditionPlanner section below) */}
      <div className="flex-1 h-full px-5 flex items-center justify-between gap-4 min-w-0">
        {/* Expedition selector */}
        <div className="flex items-center gap-1.5 min-w-0">
          <button
            type="button"
            onClick={onOpenExpeditionsModal}
            className="ed-btn shrink-0 h-[30px]"
            title="Create, clone, rename, or manage expeditions"
          >
            Expeditions
          </button>
          <select
            value={activeExpeditionId}
            onChange={(event) => onSelectExpedition(Number(event.target.value))}
            title="Select active expedition flight plan"
            className="h-[30px] bg-[#140e09] border border-[#472e19] border-l-2 border-l-[#FF7100] px-2.5 text-xs font-bold uppercase tracking-wider text-[#F4ECE4] focus:border-[#FF7100] focus:outline-none cursor-pointer max-w-[180px] sm:max-w-xs md:max-w-sm truncate"
          >
            {expeditions.map((expeditionItem) => (
              <option
                key={expeditionItem.id}
                value={expeditionItem.id}
                className="bg-[#0b0907] text-[#F4ECE4]"
              >
                {expeditionItem.name} [{expeditionItem.status.toUpperCase()}]
              </option>
            ))}
          </select>
        </div>

        {/* Right Telemetry Strip */}
        <div className="flex items-center gap-2.5 text-xs shrink-0">
          {/* System Status Readout */}
          <div
            className={`h-[30px] flex items-center gap-2 px-2.5 border font-mono-tabular text-[11px] uppercase tracking-wider ${
              activeStatusMessage
                ? 'bg-[#211307] border-[#FF7100] text-[#FF7100]'
                : 'bg-[#081318] border-[#16485c] text-[#00D2FF]'
            }`}
            title={
              activeStatusMessage
                ? `Background task: ${activeStatusMessage}`
                : 'Terminal idle and ready for input'
            }
          >
            <span
              className={`w-2 h-2 ${
                activeStatusMessage ? 'bg-[#FF7100] animate-ping' : 'bg-[#00D2FF]'
              }`}
            />
            <span className="font-bold">{activeStatusMessage ? 'BUSY' : 'READY'}</span>
          </div>

          {ingest?.currentSystem && (
            <div
              title={`Current location: ${ingest.currentSystem}`}
              className="h-[30px] hidden lg:flex items-center gap-1.5 px-2.5 bg-[#120d09] border border-[#2e1f12] border-l-2 border-l-[#FF7100]"
            >
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c]">
                LOC
              </span>
              <span className="font-mono-tabular font-bold uppercase tracking-wider text-[#F4ECE4]">
                {ingest.currentSystem}
              </span>
            </div>
          )}

          {ingest?.currentShip && (
            <div
              title={`Current vessel: ${ingest.currentShip}`}
              className="h-[30px] hidden xl:flex items-center gap-1.5 px-2.5 bg-[#120d09] border border-[#2e1f12] border-l-2 border-l-[#FF7100]"
            >
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c]">
                VESSEL
              </span>
              <span className="font-bold uppercase tracking-wider text-[#F4ECE4]">
                {ingest.currentShip}
              </span>
            </div>
          )}

          <button
            type="button"
            onClick={onOpenSettingsModal}
            className="ed-btn h-[30px]"
            title="Open terminal settings, environment modes, and journal configuration"
          >
            <svg className="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
              <path
                fillRule="evenodd"
                d="M11.49 3.17c-.38-1.56-2.6-1.56-2.98 0a1.532 1.532 0 01-2.286.948c-1.372-.836-2.942.734-2.106 2.106.54.886.061 2.042-.947 2.287-1.561.379-1.561 2.6 0 2.978a1.532 1.532 0 01.947 2.287c-.836 1.372.734 2.942 2.106 2.106a1.532 1.532 0 012.287.947c.379 1.561 2.6 1.561 2.978 0a1.533 1.533 0 012.287-.947c1.372.836 2.942-.734 2.106-2.106a1.533 1.533 0 01.947-2.287c1.561-.379 1.561-2.6 0-2.978a1.532 1.532 0 01-.947-2.287c.836-1.372-.734-2.942-2.106-2.106a1.532 1.532 0 01-2.287-.947zM10 13a3 3 0 100-6 3 3 0 000 6z"
                clipRule="evenodd"
              />
            </svg>
            <span>Config</span>
          </button>
        </div>
      </div>

      {/* Animated HUD activity scanline along bottom edge of header when busy */}
      {activeStatusMessage && (
        <div className="absolute bottom-0 left-0 right-0 h-[2px] bg-[#1a1109] overflow-hidden">
          <div className="w-1/3 h-full bg-[#FF7100] ed-scan-bar" />
        </div>
      )}
    </header>
  )
}

