import { nowGalacticTime } from '../../utils/galacticTime'
import { APP_PRODUCT_NAME, APP_VERSION } from '../../version'

/**
 * TerminalBootScreen renders the initial loading screen shown while the backend
 * mounts the SQLite archive and stellar coordinate index.
 */
export function TerminalBootScreen() {
  return (
    <div className="h-screen w-screen flex flex-col items-center justify-center cockpit-grid-bg text-[#F4ECE4] select-none p-8">
      <div className="w-full max-w-md border border-[#3d2816] border-t-2 border-t-[#FF7100] bg-[#0b0907] p-6 space-y-4">
        <div className="flex items-center justify-between">
          <span className="text-xs font-extrabold tracking-[0.2em] uppercase text-[#FF7100]">
            {APP_PRODUCT_NAME}
          </span>
          <span className="font-mono-tabular text-[11px] text-[#00D2FF]">
            TERMINAL v{APP_VERSION}
          </span>
        </div>
        <div className="space-y-1">
          <div className="text-sm font-bold uppercase tracking-wider text-[#F4ECE4]">
            Initializing Exploration Terminal…
          </div>
          <div className="text-xs text-[#a6907c]">
            Mounting journal archive and stellar coordinate index
          </div>
        </div>
        <div className="w-full h-1.5 bg-[#16100b] border border-[#3d2816] overflow-hidden">
          <div className="w-2/5 h-full bg-[#FF7100] ed-scan-bar" />
        </div>
        <div className="flex items-center justify-between font-mono-tabular text-[10px] text-[#a6907c]">
          <span>STATUS: SYNCHRONIZING</span>
          <span className="text-[#00D2FF]">{nowGalacticTime()}</span>
        </div>
      </div>
    </div>
  )
}

