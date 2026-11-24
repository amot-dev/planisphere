import { expedition } from '../../api/models'
import { formatInt, formatLy } from '../../domain/galacticTime'

/**
 * SpanshCarrierStepViewProps defines the active Spansh route hops, fuel depot
 * balance, and jump completion handlers rendered inside the carrier router panel.
 */
export interface SpanshCarrierStepViewProps {
  route: expedition.CarrierRouteResult
  jumps: expedition.CarrierRouteJump[]
  jumpIndex: number
  destinationDisplay: string
  currentDepot: number
  loggingJump: boolean
  copiedNextJump: boolean
  onCopySystem: (systemName: string) => void
  onRefillDepot?: () => void
  onCompleteSpanshJump: () => void
}

/**
 * SpanshCarrierStepView renders step-by-step Spansh 500 Ly hop telemetry,
 * tritium depot refill controls, and the Complete Jump trigger.
 */
export function SpanshCarrierStepView({
  route,
  jumps,
  jumpIndex,
  destinationDisplay,
  currentDepot,
  loggingJump,
  copiedNextJump,
  onCopySystem,
  onRefillDepot,
  onCompleteSpanshJump,
}: SpanshCarrierStepViewProps) {
  const allJumpsRecorded = jumpIndex >= jumps.length
  const activeHopIndex = Math.min(Math.max(0, jumpIndex), jumps.length - 1)
  const currentSystem = allJumpsRecorded
    ? jumps[jumps.length - 1]?.systemName || destinationDisplay
    : activeHopIndex === 0
      ? route.origin || '???'
      : jumps[activeHopIndex - 1]?.systemName || destinationDisplay
  const nextHop = jumps[activeHopIndex]
  const isFinalJump = allJumpsRecorded || activeHopIndex === jumps.length - 1
  const hopTritiumRequired = nextHop.tritiumUsed ?? 0
  const isDepotInsufficient = !allJumpsRecorded && currentDepot < hopTritiumRequired
  const remainingJumps = allJumpsRecorded ? 0 : Math.max(0, jumps.length - activeHopIndex)
  let remainingDistanceLy = 0
  let remainingTritiumTonnes = 0
  if (!allJumpsRecorded) {
    for (let hopIndex = activeHopIndex; hopIndex < jumps.length; hopIndex++) {
      const jump = jumps[hopIndex]
      remainingDistanceLy += jump.distanceLy || 0
      remainingTritiumTonnes += jump.tritiumUsed || 0
    }
  }

  return (
    <div className="pt-2.5 flex flex-wrap items-center justify-between gap-3 sm:gap-4">
      <div className="flex flex-wrap items-center gap-3 sm:gap-6">
        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted block">
            Current System
          </span>
          <span className="font-mono-tabular text-sm font-bold text-ed-text-main">
            {currentSystem}
          </span>
        </div>

        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted block">
            Next Carrier Jump [#{activeHopIndex + 1} OF {jumps.length}]
          </span>
          <div className="flex items-center gap-2">
            <span className="font-mono-tabular text-sm font-extrabold text-ed-amber">
              {nextHop.systemName}
            </span>
            <button
              type="button"
              onClick={() => onCopySystem(nextHop.systemName)}
              title="Copy next jump system name to clipboard"
              className="px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider bg-ed-header hover:bg-ed-amber text-ed-text-main hover:text-ed-void border border-ed-border-strong cursor-pointer"
            >
              {copiedNextJump ? 'COPIED' : 'COPY'}
            </button>
            {nextHop.hasIcyRing && (
              <span className="px-1.5 py-0.5 text-[9px] font-mono-tabular font-bold uppercase bg-ed-cyan-tint text-ed-cyan border border-ed-cyan/50">
                [ICY RING]
              </span>
            )}
          </div>
        </div>

        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted block">
            Next Hop Telemetry
          </span>
          <span className="font-mono-tabular text-xs text-ed-text-main">
            {formatLy(nextHop.distanceLy, 2)} ·{' '}
            <span className="text-ed-cyan font-bold">{nextHop.tritiumUsed} t TRITIUM</span>
          </span>
        </div>

        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted block">
            Fuel Depot
          </span>
          <div className="flex items-center gap-2 mt-0.5">
            <span
              className={`font-mono-tabular text-xs font-bold ${
                isDepotInsufficient ? 'text-ed-amber' : 'text-ed-cyan'
              }`}
            >
              {formatInt(currentDepot)} / 1,000 t
            </span>
            {onRefillDepot && (
              <button
                type="button"
                onClick={onRefillDepot}
                disabled={currentDepot >= 1000}
                title="Refill fuel depot to 1,000 t from carrier storage and recalculate jump costs"
                className={`px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider border cursor-pointer transition ${
                  currentDepot >= 1000
                    ? 'bg-ed-surface text-ed-text-disabled border-ed-border-dim cursor-not-allowed'
                    : isDepotInsufficient
                      ? 'bg-ed-amber text-ed-void border-ed-amber hover:bg-ed-amber-bright font-extrabold animate-pulse'
                      : 'bg-ed-cyan-tint text-ed-cyan border-ed-cyan hover:bg-ed-cyan hover:text-ed-void'
                }`}
              >
                {currentDepot >= 1000 ? 'FULL' : 'REFILL'}
              </button>
            )}
          </div>
        </div>

        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted block">
            Remaining to Waypoint
          </span>
          <span className="font-mono-tabular text-xs text-ed-text-main">
            {remainingJumps} {remainingJumps === 1 ? 'JUMP' : 'JUMPS'} ·{' '}
            <span className="text-ed-amber font-bold">{formatLy(remainingDistanceLy, 1)}</span> ·{' '}
            <span className="text-ed-text-muted">~{formatInt(remainingTritiumTonnes)} t</span>
          </span>
        </div>
      </div>

      <div className="flex items-center gap-2">
        <button
          type="button"
          disabled={loggingJump || isDepotInsufficient}
          onClick={onCompleteSpanshJump}
          title={
            isDepotInsufficient
              ? `Refill required: Fuel depot (${formatInt(currentDepot)} t) is insufficient for this jump (${formatInt(hopTritiumRequired)} t)`
              : allJumpsRecorded
                ? `Mark Fleet Carrier arrival at ${destinationDisplay} and close the carrier router`
                : isFinalJump
                  ? `Complete final carrier jump to ${destinationDisplay} and mark waypoint arrival`
                  : undefined
          }
          className={isFinalJump ? 'ed-btn-cyan' : 'ed-btn-primary'}
        >
          {allJumpsRecorded
            ? loggingJump
              ? 'MARKING ARRIVAL...'
              : 'MARK FC STOP ARRIVAL'
            : loggingJump
              ? 'LOGGING JUMP...'
              : `COMPLETE JUMP #${activeHopIndex + 1}`}
        </button>
      </div>
    </div>
  )
}

