import { useEffect, useState } from 'react'
import { formatInt, formatLy } from '../../domain/galacticTime'

/**
 * ManualCarrierLoggerViewProps defines draft inputs, leg accumulators, and
 * jump logging / arrival callbacks for manual Fleet Carrier routing.
 */
export interface ManualCarrierLoggerViewProps {
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
}

/**
 * ManualCarrierLoggerView renders the manual Fleet Carrier jump logger inputs,
 * accumulated leg counters, and Mark FC Stop Arrival action button.
 */
export function ManualCarrierLoggerView({
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
}: ManualCarrierLoggerViewProps) {
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
          <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted block">
            Manual Carrier Routing
          </span>
          <span className="font-mono-tabular text-xs text-ed-text-main">
            Logged this leg:{' '}
            <span className="text-ed-cyan font-bold">{manualLegJumps} JUMPS</span> ·{' '}
            <span className="text-ed-amber font-bold">{formatLy(manualLegDistance, 1)}</span> ·{' '}
            <span>{formatInt(manualLegTritium)} t TRITIUM</span>
          </span>
        </div>

        <div className="flex items-center gap-2">
          <div>
            <label className="block text-[10px] font-bold uppercase tracking-wider text-ed-text-muted mb-0.5">
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
              className="w-32 px-2.5 py-1 bg-ed-void border border-ed-border font-mono-tabular text-ed-text-main focus:border-ed-cyan focus:outline-none"
            />
          </div>

          <div>
            <label className="block text-[10px] font-bold uppercase tracking-wider text-ed-text-muted mb-0.5">
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
              className="w-28 px-2.5 py-1 bg-ed-void border border-ed-border font-mono-tabular text-ed-text-main focus:border-ed-cyan focus:outline-none"
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
            ? `Mark Fleet Carrier arrival at ${destinationDisplay} and close the carrier router`
            : 'No next Fleet Carrier stop designated in the expedition plan'
        }
        className="ed-btn-cyan"
      >
        MARK FC STOP ARRIVAL
      </button>
    </div>
  )
}

