import { useEffect, useState } from 'react'
import { GetStartCandidates, StartExpedition } from '../../../api/backend'
import { app, modes } from '../../../api/models'
import { ModalShell } from '../../../components/ui/ModalShell'
import { ED_AMBER_HEX } from '../../../domain/theme'

/**
 * StartExpeditionModalProps defines the expedition identifier, available flight
 * modes, initial expedition mode, and close callback for engaging a planned expedition.
 */
export interface StartExpeditionModalProps {
  expeditionId: number
  modesList?: modes.Mode[]
  initialModeId?: number
  onClose: () => void
}

function resolveDefaultModeId(modesList: modes.Mode[], initialModeId: number): number {
  if (initialModeId > 0 && modesList.some((modeItem) => modeItem.id === initialModeId)) {
    return initialModeId
  }
  const travelMode = modesList.find((modeItem) => modeItem.name.toLowerCase() === 'travel')
  return travelMode?.id ?? modesList[0]?.id ?? 0
}

/**
 * StartExpeditionModal renders the dialog for locking an expedition's start
 * from a recorded historical journal visit to Waypoint #1 and selecting the
 * starting flight mode applied to already-visited waypoints and live flight.
 */
export function StartExpeditionModal({
  expeditionId,
  modesList = [],
  initialModeId = 0,
  onClose,
}: StartExpeditionModalProps) {
  const [candidates, setCandidates] = useState<app.StartCandidates | null>(null)
  const [selectedTimestamp, setSelectedTimestamp] = useState<string>('')
  const [selectedModeId, setSelectedModeId] = useState<number>(() =>
    resolveDefaultModeId(modesList, initialModeId),
  )
  const [busy, setBusy] = useState<boolean>(false)
  const [error, setError] = useState<string>('')

  useEffect(() => {
    setSelectedModeId(resolveDefaultModeId(modesList, initialModeId))
  }, [initialModeId, modesList])

  useEffect(() => {
    GetStartCandidates(expeditionId)
      .then((candidatesResult) => {
        setCandidates(candidatesResult)
        if (candidatesResult.visits && candidatesResult.visits.length > 0) {
          setSelectedTimestamp(candidatesResult.visits[0].timestamp)
        } else {
          setSelectedTimestamp('')
        }
      })
      .catch((loadError) => setError(String(loadError)))
  }, [expeditionId])

  const selectedMode = modesList.find((modeItem) => modeItem.id === selectedModeId) ?? modesList[0]

  async function handleConfirm() {
    setError('')
    if (!selectedTimestamp) {
      setError('Select a recorded visit timestamp first.')
      return
    }
    setBusy(true)
    try {
      await StartExpedition(expeditionId, selectedTimestamp, selectedModeId)
      onClose()
    } catch (startError) {
      setError(String(startError))
    } finally {
      setBusy(false)
    }
  }

  return (
    <ModalShell title="Engage Expedition" onClose={onClose}>
      {error && (
        <div className="p-2.5 border border-ed-danger bg-ed-danger-tint text-ed-danger-text">{error}</div>
      )}

      <div className="space-y-3">
        <div className="p-3 border border-ed-border bg-ed-card space-y-2">
          <div className="font-bold uppercase tracking-wider text-ed-text-main">
            Recorded Visit to{' '}
            <span className="text-ed-cyan">{candidates?.systemName || 'Waypoint #1'}</span>
          </div>
          {candidates && candidates.visits.length > 0 ? (
            <select
              aria-label="Recorded visit to Waypoint #1"
              value={selectedTimestamp}
              onChange={(event) => setSelectedTimestamp(event.target.value)}
              className="w-full bg-ed-void border border-ed-border px-2.5 py-1.5 font-mono-tabular text-xs text-ed-text-main"
            >
              {candidates.visits.map((visit) => (
                <option key={visit.uid} value={visit.timestamp}>
                  {visit.galactic} — ({visit.event})
                </option>
              ))}
            </select>
          ) : (
            <div className="text-ed-text-muted italic">
              No historical visits to {candidates?.systemName || 'Waypoint #1'} found in journals yet.
            </div>
          )}
          <p className="text-[11px] text-ed-text-muted">
            Anchors Waypoint #1 to this recorded visit and replays subsequent journal events. Expedition telemetry and duration begin upon your first departure from Waypoint #1.
          </p>
        </div>

        {modesList.length > 0 && (
          <div className="p-3 border border-ed-border bg-ed-card space-y-2">
            <div className="font-bold uppercase tracking-wider text-ed-text-main">
              Flight Mode
            </div>
            <div className="flex items-center gap-2">
              <span
                className="w-2 h-5 shrink-0"
                style={{ backgroundColor: selectedMode?.color || ED_AMBER_HEX }}
              />
              <select
                aria-label="Starting flight mode"
                value={selectedModeId}
                onChange={(event) => setSelectedModeId(Number(event.target.value))}
                className="flex-1 bg-ed-void border border-ed-border px-2.5 py-1.5 font-mono-tabular text-xs font-bold uppercase tracking-wider text-ed-text-main"
              >
                {modesList.map((modeItem) => (
                  <option key={modeItem.id} value={modeItem.id} className="bg-ed-panel text-ed-text-main">
                    {modeItem.name}
                  </option>
                ))}
              </select>
            </div>
            <p className="text-[11px] text-ed-text-muted">
              Sets the expedition active flight mode and applies to any systems already visited after the selected start.
            </p>
          </div>
        )}
      </div>

      <div className="flex items-center justify-end gap-2 pt-2 border-t border-ed-border">
        <button
          type="button"
          disabled={busy}
          onClick={onClose}
          className="ed-btn"
        >
          CANCEL
        </button>
        <button
          type="button"
          disabled={busy || !selectedTimestamp}
          onClick={handleConfirm}
          className={`ed-btn-primary ${
            busy || !selectedTimestamp ? 'opacity-40 cursor-not-allowed hover:bg-transparent' : ''
          }`}
        >
          {busy ? 'ENGAGING...' : 'CONFIRM AND ENGAGE'}
        </button>
      </div>
    </ModalShell>
  )
}
