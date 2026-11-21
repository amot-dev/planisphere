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
  const activeHopIndex = Math.min(Math.max(0, jumpIndex), jumps.length - 1)
  const currentSystem =
    activeHopIndex === 0
      ? route.origin || '???'
      : jumps[activeHopIndex - 1]?.systemName || destinationDisplay
  const nextHop = jumps[activeHopIndex]
  const isFinalJump = activeHopIndex === jumps.length - 1
  const hopTritiumRequired = nextHop.tritiumUsed ?? 0
  const isDepotInsufficient = currentDepot < hopTritiumRequired
  const remainingJumps = Math.max(0, jumps.length - activeHopIndex)
  let remainingDistanceLy = 0
  let remainingTritiumTonnes = 0
  for (let hopIndex = activeHopIndex; hopIndex < jumps.length; hopIndex++) {
    const jump = jumps[hopIndex]
    remainingDistanceLy += jump.distanceLy || 0
    remainingTritiumTonnes += jump.tritiumUsed || 0
  }

  return (
    <div className="pt-2.5 flex flex-wrap items-center justify-between gap-3 sm:gap-4">
      <div className="flex flex-wrap items-center gap-3 sm:gap-6">
        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
            Current System
          </span>
          <span className="font-mono-tabular text-sm font-bold text-[#F4ECE4]">
            {currentSystem}
          </span>
        </div>

        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
            Next Carrier Jump [#{activeHopIndex + 1} OF {jumps.length}]
          </span>
          <div className="flex items-center gap-2">
            <span className="font-mono-tabular text-sm font-extrabold text-[#FF7100]">
              {nextHop.systemName}
            </span>
            <button
              type="button"
              onClick={() => onCopySystem(nextHop.systemName)}
              title="Copy next jump system name to clipboard"
              className="px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider bg-[#140e09] hover:bg-[#FF7100] text-[#F4ECE4] hover:text-[#050506] border border-[#472e19] cursor-pointer"
            >
              {copiedNextJump ? 'COPIED' : 'COPY'}
            </button>
            {nextHop.hasIcyRing && (
              <span className="px-1.5 py-0.5 text-[9px] font-mono-tabular font-bold uppercase bg-[#08151c] text-[#00D2FF] border border-[#00D2FF]/50">
                [ICY RING]
              </span>
            )}
          </div>
        </div>

        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
            Next Hop Telemetry
          </span>
          <span className="font-mono-tabular text-xs text-[#F4ECE4]">
            {formatLy(nextHop.distanceLy, 2)} ·{' '}
            <span className="text-[#00D2FF] font-bold">{nextHop.tritiumUsed} t TRITIUM</span>
          </span>
        </div>

        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
            Fuel Depot
          </span>
          <div className="flex items-center gap-2 mt-0.5">
            <span
              className={`font-mono-tabular text-xs font-bold ${
                isDepotInsufficient ? 'text-[#FF7100]' : 'text-[#00D2FF]'
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
                    ? 'bg-[#120d09] text-[#554030] border-[#2e1f12] cursor-not-allowed'
                    : isDepotInsufficient
                      ? 'bg-[#FF7100] text-[#050506] border-[#FF7100] hover:bg-[#ff9436] font-extrabold animate-pulse'
                      : 'bg-[#08151c] text-[#00D2FF] border-[#00D2FF] hover:bg-[#00D2FF] hover:text-[#050506]'
                }`}
              >
                {currentDepot >= 1000 ? 'FULL' : 'REFILL'}
              </button>
            )}
          </div>
        </div>

        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
            Remaining to Waypoint
          </span>
          <span className="font-mono-tabular text-xs text-[#F4ECE4]">
            {remainingJumps} {remainingJumps === 1 ? 'JUMP' : 'JUMPS'} ·{' '}
            <span className="text-[#FF7100] font-bold">{formatLy(remainingDistanceLy, 1)}</span> ·{' '}
            <span className="text-[#a6907c]">~{formatInt(remainingTritiumTonnes)} t</span>
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
              : isFinalJump
                ? `Complete final carrier jump to ${destinationDisplay} and mark waypoint arrival`
                : undefined
          }
          className={isFinalJump ? 'ed-btn-cyan' : 'ed-btn-primary'}
        >
          {loggingJump ? 'LOGGING JUMP...' : `COMPLETE JUMP #${activeHopIndex + 1}`}
        </button>
      </div>
    </div>
  )
}

