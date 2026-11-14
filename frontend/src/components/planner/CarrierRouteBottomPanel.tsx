import { memo, useEffect, useState } from 'react'
import { app } from '../../../wailsjs/go/models'
import { formatInt, formatLy } from '../../utils/galacticTime'

/**
 * CarrierRouteBottomPanelProps defines the active route state, hop progress,
 * manual override inputs, and action callbacks for the bottom carrier navigation drawer.
 */
export interface CarrierRouteBottomPanelProps {
  route: app.CarrierRouteResult
  jumpIndex: number
  manualMode: boolean
  hasNextFcStop?: boolean
  nextFcStopSystem?: string
  onCancelRoute?: () => void
  onToggleManualMode: () => void
  onCompleteSpanshJump: () => void
  manualDistanceInput: string
  onChangeManualDistance: (value: string) => void
  manualTritiumInput: string
  onChangeManualTritium: (value: string) => void
  onLogManualJump: () => void
  manualLegJumps: number
  manualLegDistance: number
  manualLegTritium: number
  loggingJump: boolean
  copiedNextJump: boolean
  onCopySystem: (systemName: string) => void
  onArrive: () => void
  depotTritium?: number
  onRefillDepot?: () => void
}

/**
 * CarrierRouteBottomPanel renders the active fleet carrier navigation drawer,
 * supporting both Spansh 500 Ly calculated routes and manual jump logging.
 */
export const CarrierRouteBottomPanel = memo(function CarrierRouteBottomPanel({
  route,
  jumpIndex,
  manualMode,
  hasNextFcStop,
  nextFcStopSystem,
  onCancelRoute,
  onToggleManualMode,
  onCompleteSpanshJump,
  manualDistanceInput,
  onChangeManualDistance,
  manualTritiumInput,
  onChangeManualTritium,
  onLogManualJump,
  manualLegJumps,
  manualLegDistance,
  manualLegTritium,
  loggingJump,
  copiedNextJump,
  onCopySystem,
  onArrive,
  depotTritium,
  onRefillDepot,
}: CarrierRouteBottomPanelProps) {
  const jumps = route.jumps ?? []
  const hasSpanshJumps = jumps.length > 0
  // In Spansh mode (!manualMode), the plotted route has a fixed destination (route.destination).
  // In manual mode, dynamically track nextFcStopSystem when a next FC stop is designated.
  const effectiveHasNextFcStop =
    hasNextFcStop !== undefined ? hasNextFcStop : Boolean(route.destination?.trim())
  const destinationSystem = !manualMode
    ? (route.destination ?? '').trim()
    : effectiveHasNextFcStop
      ? (nextFcStopSystem ?? route.destination ?? '').trim()
      : ''
  const destinationDisplay = destinationSystem || '???'
  const canMarkArrival = effectiveHasNextFcStop && Boolean(destinationSystem)
  const currentDepot =
    depotTritium !== undefined
      ? depotTritium
      : route.depotTritium !== undefined
        ? route.depotTritium
        : 1000

  return (
    <div className="border-t-2 border-[#00D2FF] bg-[#0b0907] px-3 sm:px-5 py-2.5 sm:py-3 shadow-2xl text-xs">
      <CarrierPlotterHeaderBar
        route={route}
        destinationDisplay={destinationDisplay}
        manualMode={manualMode}
        hasSpanshJumps={hasSpanshJumps}
        currentDepot={currentDepot}
        onToggleManualMode={onToggleManualMode}
        onCancelRoute={onCancelRoute}
      />

      {!manualMode && hasSpanshJumps ? (
        <SpanshCarrierStepView
          route={route}
          jumps={jumps}
          jumpIndex={jumpIndex}
          destinationDisplay={destinationDisplay}
          currentDepot={currentDepot}
          loggingJump={loggingJump}
          copiedNextJump={copiedNextJump}
          onCopySystem={onCopySystem}
          onRefillDepot={onRefillDepot}
          onCompleteSpanshJump={onCompleteSpanshJump}
        />
      ) : (
        <ManualCarrierLoggerView
          manualDistanceInput={manualDistanceInput}
          onChangeManualDistance={onChangeManualDistance}
          manualTritiumInput={manualTritiumInput}
          onChangeManualTritium={onChangeManualTritium}
          onLogManualJump={onLogManualJump}
          manualLegJumps={manualLegJumps}
          manualLegDistance={manualLegDistance}
          manualLegTritium={manualLegTritium}
          loggingJump={loggingJump}
          canMarkArrival={canMarkArrival}
          destinationDisplay={destinationDisplay}
          onArrive={onArrive}
        />
      )}
    </div>
  )
})

function CarrierPlotterHeaderBar({
  route,
  destinationDisplay,
  manualMode,
  hasSpanshJumps,
  currentDepot,
  onToggleManualMode,
  onCancelRoute,
}: {
  route: app.CarrierRouteResult
  destinationDisplay: string
  manualMode: boolean
  hasSpanshJumps: boolean
  currentDepot: number
  onToggleManualMode: () => void
  onCancelRoute?: () => void
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 pb-2.5 border-b border-[#2e1f12]">
      <div className="flex flex-wrap items-center gap-2 sm:gap-2.5">
        <span className="px-2 py-0.5 text-[10px] font-extrabold uppercase tracking-[0.14em] bg-[#08151c] text-[#00D2FF] border border-[#00D2FF]">
          {route.carrierType === 'squadron'
            ? 'SQUADRON CARRIER PLOTTER'
            : 'PERSONAL CARRIER PLOTTER'}
        </span>
        <span className="font-mono-tabular font-bold text-[#F4ECE4]">
          {route.origin || '???'} <span className="text-[#a6907c]">→</span>{' '}
          <span className="text-[#FF7100]">{destinationDisplay}</span>
        </span>
        {!manualMode && (
          <>
            <span className="text-[11px] font-mono-tabular text-[#a6907c]">
              [{formatInt(route.capacityUsed)} t USED CAP]
            </span>
            <span className="text-[11px] font-mono-tabular text-[#00D2FF]">
              [{formatInt(currentDepot)} / 1,000 t DEPOT]
            </span>
          </>
        )}
        {route.error && (
          <span className="text-[11px] text-[#FF7100] bg-[#211307] border border-[#FF7100] px-2 py-0.5 font-mono-tabular">
            {route.error}
          </span>
        )}
      </div>

      <div className="flex items-center gap-2">
        {/* Once in manual mode, always in manual mode: in manual mode the user can go
            wherever they want. After even just one jump, the Spansh route is completely
            invalid. Do not provide a button or path to revert to Spansh mode. */}
        {!manualMode && hasSpanshJumps && (
          <button
            type="button"
            onClick={onToggleManualMode}
            className="ed-btn"
          >
            Switch to Manual Mode
          </button>
        )}
        {onCancelRoute && (
          <button
            type="button"
            onClick={onCancelRoute}
            title="Close the carrier plotter. Existing jumps remain logged in your expedition statistics."
            className="ed-btn"
          >
            Close
          </button>
        )}
      </div>
    </div>
  )
}

function SpanshCarrierStepView({
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
}: {
  route: app.CarrierRouteResult
  jumps: app.CarrierRouteJump[]
  jumpIndex: number
  destinationDisplay: string
  currentDepot: number
  loggingJump: boolean
  copiedNextJump: boolean
  onCopySystem: (systemName: string) => void
  onRefillDepot?: () => void
  onCompleteSpanshJump: () => void
}) {
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
              ? `Refill required: Tritium depot (${formatInt(currentDepot)} t) is insufficient for this jump (${formatInt(hopTritiumRequired)} t)`
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

function ManualCarrierLoggerView({
  manualDistanceInput,
  onChangeManualDistance,
  manualTritiumInput,
  onChangeManualTritium,
  onLogManualJump,
  manualLegJumps,
  manualLegDistance,
  manualLegTritium,
  loggingJump,
  canMarkArrival,
  destinationDisplay,
  onArrive,
}: {
  manualDistanceInput: string
  onChangeManualDistance: (value: string) => void
  manualTritiumInput: string
  onChangeManualTritium: (value: string) => void
  onLogManualJump: () => void
  manualLegJumps: number
  manualLegDistance: number
  manualLegTritium: number
  loggingJump: boolean
  canMarkArrival: boolean
  destinationDisplay: string
  onArrive: () => void
}) {
  const [distanceDraft, setDistanceDraft] = useState(manualDistanceInput)
  const [tritiumDraft, setTritiumDraft] = useState(manualTritiumInput)

  useEffect(() => {
    setDistanceDraft(manualDistanceInput)
  }, [manualDistanceInput, manualLegJumps])

  useEffect(() => {
    setTritiumDraft(manualTritiumInput)
  }, [manualTritiumInput, manualLegJumps])

  return (
    <div className="pt-2.5 flex flex-wrap items-center justify-between gap-4">
      <div className="flex flex-wrap items-center gap-4">
        <div>
          <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
            Manual Carrier Routing
          </span>
          <span className="font-mono-tabular text-xs text-[#F4ECE4]">
            Logged this leg:{' '}
            <span className="text-[#00D2FF] font-bold">{manualLegJumps} JUMPS</span> ·{' '}
            <span className="text-[#FF7100] font-bold">{formatLy(manualLegDistance, 1)}</span> ·{' '}
            <span>{formatInt(manualLegTritium)} t TRITIUM</span>
          </span>
        </div>

        <div className="flex items-center gap-2">
          <div>
            <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-0.5">
              Jump Distance (Ly)
            </label>
            <input
              type="number"
              step="0.01"
              min="0"
              max="500"
              value={distanceDraft}
              onChange={(event) => {
                const nextDistance = event.target.value
                setDistanceDraft(nextDistance)
                onChangeManualDistance(nextDistance)
              }}
              placeholder="e.g. 498.4"
              className="w-32 px-2.5 py-1 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
            />
          </div>

          <div>
            <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-0.5">
              Tritium Used (t)
            </label>
            <input
              type="number"
              step="1"
              min="0"
              value={tritiumDraft}
              onChange={(event) => {
                const nextTritium = event.target.value
                setTritiumDraft(nextTritium)
                onChangeManualTritium(nextTritium)
              }}
              placeholder="e.g. 68"
              className="w-28 px-2.5 py-1 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
            />
          </div>

          <div className="pt-4">
            <button
              type="button"
              disabled={loggingJump}
              onClick={onLogManualJump}
              className="ed-btn-primary"
            >
              {loggingJump ? 'LOGGING...' : '+ LOG JUMP'}
            </button>
          </div>
        </div>
      </div>

      <button
        type="button"
        disabled={!canMarkArrival}
        onClick={onArrive}
        title={
          canMarkArrival
            ? `Mark Fleet Carrier arrival at ${destinationDisplay} and close the carrier plotter`
            : 'No next Fleet Carrier stop designated in the expedition plan'
        }
        className="ed-btn-cyan"
      >
        Mark FC Stop Arrival
      </button>
    </div>
  )
}

