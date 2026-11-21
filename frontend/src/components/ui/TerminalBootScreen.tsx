import { nowGalacticTime } from '../../domain/galacticTime'
import { APP_PRODUCT_NAME, APP_VERSION } from '../../version'

/**
 * TerminalBootScreen renders the initial loading screen shown while the backend
 * initializes the SQLite archive and initial journal catch-up, core settings,
 * the active flight plan, and sidebar telemetry finish loading before main paint.
 */
export function TerminalBootScreen() {
  return (
    <div className="h-screen w-screen flex flex-col items-center justify-center cockpit-grid-bg text-ed-text-main select-none p-8">
      <div className="w-full max-w-md border border-ed-border border-t-2 border-t-ed-amber bg-ed-panel p-6 space-y-4">
        <div className="flex items-center justify-between">
          <span className="text-xs font-extrabold tracking-[0.2em] uppercase text-ed-amber">
            {APP_PRODUCT_NAME}
          </span>
          <span className="font-mono-tabular text-[11px] text-ed-cyan">
            PLANISPHERE TERMINAL v{APP_VERSION}
          </span>
        </div>
        <div className="space-y-1">
          <div className="text-sm font-bold uppercase tracking-wider text-ed-text-main">
            Initializing Planisphere Terminal...
          </div>
          <div className="text-xs text-ed-text-muted">
            Mounting journal archive and stellar coordinate index
          </div>
        </div>
        <div className="w-full h-1.5 bg-ed-control border border-ed-border overflow-hidden">
          <div className="w-2/5 h-full bg-ed-amber ed-scan-bar" />
        </div>
        <div className="flex items-center justify-between font-mono-tabular text-[10px] text-ed-text-muted">
          <span>STATUS: SYNCHRONIZING</span>
          <span className="text-ed-cyan">{nowGalacticTime()}</span>
        </div>
      </div>
    </div>
  )
}

