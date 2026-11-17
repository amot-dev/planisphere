import { useEffect, useState } from 'react'
import { GetStartCandidates, StartExpedition } from '../../../wailsjs/go/app/App'
import { app } from '../../../wailsjs/go/models'
import { fromGalacticTime, nowGalacticTime } from '../../utils/galacticTime'
import { ModalShell } from '../common/ModalShell'

/**
 * StartExpeditionModalProps defines the expedition identifier and lifecycle
 * callbacks for engaging a planned expedition.
 */
export interface StartExpeditionModalProps {
  expeditionId: number
  onClose: () => void
  onStarted: () => void
}

/**
 * StartExpeditionModal renders the dialog for locking an expedition's start
 * timestamp from historical journal visits or a custom Galactic Standard Time timestamp.
 */
export function StartExpeditionModal({
  expeditionId,
  onClose,
  onStarted,
}: StartExpeditionModalProps) {
  const [candidates, setCandidates] = useState<app.StartCandidates | null>(null)
  const [selectedTimestamp, setSelectedTimestamp] = useState<string>('')
  const [customGalactic, setCustomGalactic] = useState<string>(() => nowGalacticTime())
  const [mode, setMode] = useState<'history' | 'custom'>('history')
  const [busy, setBusy] = useState<boolean>(false)
  const [error, setError] = useState<string>('')

  useEffect(() => {
    GetStartCandidates(expeditionId)
      .then((candidatesResult) => {
        setCandidates(candidatesResult)
        if (candidatesResult.visits && candidatesResult.visits.length > 0) {
          setSelectedTimestamp(candidatesResult.visits[0].timestamp)
          setMode('history')
        } else {
          setMode('custom')
        }
      })
      .catch((loadError) => setError(String(loadError)))
  }, [expeditionId])

  async function handleConfirm() {
    setError('')
    setBusy(true)
    try {
      if (mode === 'history') {
        if (!selectedTimestamp) {
          setError('Select a recorded visit timestamp first.')
          return
        }
        await StartExpedition(expeditionId, selectedTimestamp)
      } else {
        const isoTimestamp = fromGalacticTime(customGalactic)
        await StartExpedition(expeditionId, isoTimestamp)
      }
      onStarted()
      onClose()
    } catch (startError) {
      setError(String(startError))
    } finally {
      setBusy(false)
    }
  }

  return (
    <ModalShell
      title="Engage Expedition — Lock Waypoint #1 Arrival"
      subtitle={
        candidates?.systemName
          ? `Historical journal visits to Waypoint #1: ${candidates.systemName}`
          : 'Configure expedition start timestamp'
      }
      onClose={onClose}
    >
      {error && (
        <div className="p-2.5 border border-[#ff3b2f] bg-[#190909] text-[#ff4d3d]">{error}</div>
      )}

      <div className="space-y-3">
        {/* Option 1: Historical visits dropdown */}
        <label className="flex items-start gap-2.5 p-3 border border-[#3d2816] bg-[#110c08] cursor-pointer">
          <input
            type="radio"
            name="startMode"
            checked={mode === 'history'}
            onChange={() => setMode('history')}
            className="mt-0.5 accent-[#FF7100]"
          />
          <div className="flex-1 space-y-2">
            <div className="font-bold uppercase tracking-wider text-[#F4ECE4]">
              Select Recorded Visit to{' '}
              <span className="text-[#00D2FF]">{candidates?.systemName || 'Waypoint #1'}</span>
            </div>
            {candidates && candidates.visits.length > 0 ? (
              <select
                value={selectedTimestamp}
                onChange={(event) => {
                  setSelectedTimestamp(event.target.value)
                  setMode('history')
                }}
                className="w-full bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-xs text-[#F4ECE4]"
              >
                {candidates.visits.map((visit) => (
                  <option key={visit.uid} value={visit.timestamp}>
                    {visit.galactic} — ({visit.event})
                  </option>
                ))}
              </select>
            ) : (
              <div className="text-[#a6907c] italic">
                No historical visits to {candidates?.systemName || 'Waypoint #1'} found in journals yet.
              </div>
            )}
            <p className="text-[11px] text-[#a6907c]">
              Starts the expedition from this visit and replays subsequent journal events.
            </p>
          </div>
        </label>

        {/* Option 2: Custom Galactic Timestamp */}
        <label className="flex items-start gap-2.5 p-3 border border-[#3d2816] bg-[#110c08] cursor-pointer">
          <input
            type="radio"
            name="startMode"
            checked={mode === 'custom'}
            onChange={() => setMode('custom')}
            className="mt-0.5 accent-[#FF7100]"
          />
          <div className="flex-1 space-y-1.5">
            <div className="font-bold uppercase tracking-wider text-[#F4ECE4]">
              Custom Timestamp (33XX-MM-DD HH:mm UTC)
            </div>
            <input
              type="text"
              value={customGalactic}
              onFocus={() => setMode('custom')}
              onChange={(event) => {
                setCustomGalactic(event.target.value)
                setMode('custom')
              }}
              placeholder="3312-01-18 19:42 UTC"
              className="w-full bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-xs text-[#F4ECE4]"
            />
          </div>
        </label>
      </div>

      <div className="flex items-center justify-end gap-2 pt-2 border-t border-[#3d2816]">
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
          disabled={busy}
          onClick={handleConfirm}
          className="ed-btn-primary"
        >
          {busy ? 'ENGAGING...' : 'CONFIRM AND ENGAGE'}
        </button>
      </div>
    </ModalShell>
  )
}

