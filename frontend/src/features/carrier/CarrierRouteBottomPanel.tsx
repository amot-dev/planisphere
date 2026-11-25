import { memo } from 'react'
import { expedition } from '../../api/models'
import { formatInt } from '../../domain/galacticTime'
import { ManualCarrierLoggerView } from './ManualCarrierLoggerView'
import { SpanshCarrierStepView } from './SpanshCarrierStepView'

/**
 * CarrierRouteBottomPanelProps defines the active route state, hop progress,
 * manual override inputs, and action callbacks for the bottom carrier navigation drawer.
 */
export interface CarrierRouteBottomPanelProps {
  route: expedition.CarrierRouteResult
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
    <div className="border-t-2 border-ed-cyan bg-ed-panel px-3 sm:px-5 py-2.5 sm:py-3 shadow-2xl text-xs">
      <CarrierRouterHeaderBar
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

function CarrierRouterHeaderBar({
  route,
  destinationDisplay,
  manualMode,
  hasSpanshJumps,
  currentDepot,
  onToggleManualMode,
  onCancelRoute,
}: {
  route: expedition.CarrierRouteResult
  destinationDisplay: string
  manualMode: boolean
  hasSpanshJumps: boolean
  currentDepot: number
  onToggleManualMode: () => void
  onCancelRoute?: () => void
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 pb-2.5 border-b border-ed-border-dim">
      <div className="flex flex-wrap items-center gap-2 sm:gap-2.5">
        <span className="px-2 py-0.5 text-[10px] font-extrabold uppercase tracking-[0.14em] bg-ed-cyan-tint text-ed-cyan border border-ed-cyan">
          {route.carrierType === 'squadron'
            ? 'SQUADRON CARRIER ROUTER'
            : 'PERSONAL CARRIER ROUTER'}
        </span>
        <span className="font-mono-tabular font-bold text-ed-text-main">
          {route.origin || '???'} <span className="relative -top-0.5 text-ed-text-muted">→</span>{' '}
          <span className="text-ed-amber">{destinationDisplay}</span>
        </span>
        {!manualMode && (
          <>
            <span className="text-[11px] font-mono-tabular text-ed-text-muted">
              [{formatInt(route.capacityUsed)} t USED CAP]
            </span>
            <span className="text-[11px] font-mono-tabular text-ed-cyan">
              [{formatInt(currentDepot)} / 1,000 t DEPOT]
            </span>
          </>
        )}
        {route.error && (
          <span className="text-[11px] text-ed-amber bg-ed-amber-tint border border-ed-amber px-2 py-0.5 font-mono-tabular">
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
            SWITCH TO MANUAL MODE
          </button>
        )}
        {onCancelRoute && (
          <button
            type="button"
            onClick={onCancelRoute}
            title="Close the carrier router. Existing jumps remain logged in your expedition statistics."
            className="ed-btn"
          >
            CLOSE
          </button>
        )}
      </div>
    </div>
  )
}

