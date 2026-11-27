import { useEffect, useMemo, useRef, useState } from 'react'
import { GetStartCandidates, StartExpedition, StartExpeditionScoped } from '../../../api/backend'
import { app, modes, replay } from '../../../api/models'
import { ModalShell } from '../../../components/ui/ModalShell'
import { ED_AMBER_HEX } from '../../../domain/theme'
import { ExpeditionCommanderScopePicker, resolveSelectedCommanderProfileId } from './ExpeditionCommanderScopePicker'

/**
 * StartExpeditionModalProps defines the expedition identifier, available flight
 * modes, commander profiles, initial commander profile ID, and close callback for engaging a planned expedition.
 */
export interface StartExpeditionModalProps {
  expeditionId: number
  modesList?: modes.Mode[]
  initialModeId?: number
  defaultModeId?: number
  commanders?: replay.Commander[]
  initialCommanderProfileId?: number
  onClose: () => void
}

function resolveDefaultModeId(
  modesList: modes.Mode[],
  initialModeId: number,
  defaultModeId: number,
): number {
  if (initialModeId > 0 && modesList.some((modeItem) => modeItem.id === initialModeId)) {
    return initialModeId
  }
  if (defaultModeId > 0 && modesList.some((modeItem) => modeItem.id === defaultModeId)) {
    return defaultModeId
  }
  const defaultMode = modesList.find((modeItem) => modeItem.isDefault)
  return defaultMode?.id ?? modesList[0]?.id ?? 0
}

function filterVisitsForCommander(visits: replay.Arrival[] | undefined, hasMultipleCommanders: boolean, profileId: number): replay.Arrival[] {
  const allVisits = visits ?? []
  if (!hasMultipleCommanders || profileId <= 0) return allVisits
  return allVisits.filter((visit) => (visit.profileId ?? 0) <= 0 || visit.profileId === profileId)
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
  defaultModeId = 0,
  commanders = [],
  initialCommanderProfileId,
  onClose,
}: StartExpeditionModalProps) {
  const hasMultipleCommanders = commanders.length > 1
  const [candidates, setCandidates] = useState<app.StartCandidates | null>(null)
  const [selectedTimestamp, setSelectedTimestamp] = useState<string>('')
  const [selectedModeId, setSelectedModeId] = useState<number>(() =>
    resolveDefaultModeId(modesList, initialModeId, defaultModeId),
  )
  const [selectedProfileId, setSelectedProfileId] = useState<number>(() =>
    resolveSelectedCommanderProfileId(commanders, initialCommanderProfileId),
  )
  const [busy, setBusy] = useState<boolean>(false)
  const [error, setError] = useState<string>('')
  const mountedRef = useRef(true)
  const prevExpeditionIdRef = useRef<number>(expeditionId)
  const userTouchedProfileRef = useRef<boolean>(false)
  const hasMultipleCommandersRef = useRef<boolean>(hasMultipleCommanders)
  hasMultipleCommandersRef.current = hasMultipleCommanders
  const candidatesRef = useRef<app.StartCandidates | null>(candidates)
  candidatesRef.current = candidates
  const selectedProfileIdRef = useRef<number>(selectedProfileId)
  selectedProfileIdRef.current = selectedProfileId

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    setSelectedModeId(resolveDefaultModeId(modesList, initialModeId, defaultModeId))
  }, [defaultModeId, initialModeId, modesList])

  useEffect(() => {
    const expeditionChanged = prevExpeditionIdRef.current !== expeditionId
    if (expeditionChanged) {
      prevExpeditionIdRef.current = expeditionId
      userTouchedProfileRef.current = false
      candidatesRef.current = null
      setCandidates(null)
      setSelectedTimestamp('')
      setError('')
    }
    const currentProfileId = selectedProfileIdRef.current
    const currentProfileValid =
      currentProfileId > 0 && commanders.some((commander) => commander.id === currentProfileId)
    if (expeditionChanged || !userTouchedProfileRef.current || !currentProfileValid) {
      if (!currentProfileValid) {
        userTouchedProfileRef.current = false
      }
      const nextProfileId = resolveSelectedCommanderProfileId(commanders, initialCommanderProfileId)
      selectedProfileIdRef.current = nextProfileId
      setSelectedProfileId(nextProfileId)
      if (!expeditionChanged && candidatesRef.current !== null) {
        const nextVisits = filterVisitsForCommander(
          candidatesRef.current.visits,
          commanders.length > 1,
          nextProfileId,
        )
        setSelectedTimestamp((currentTs) =>
          nextProfileId !== currentProfileId
            ? (nextVisits[0]?.timestamp ?? '')
            : nextVisits.some((visit) => visit.timestamp === currentTs)
              ? currentTs
              : (nextVisits[0]?.timestamp ?? ''),
        )
      }
    }
  }, [commanders, expeditionId, initialCommanderProfileId])

  useEffect(() => {
    let cancelled = false
    GetStartCandidates(expeditionId)
      .then((candidatesResult) => {
        if (cancelled) return
        candidatesRef.current = candidatesResult
        setCandidates(candidatesResult)
        const matchingVisits = filterVisitsForCommander(
          candidatesResult.visits,
          hasMultipleCommandersRef.current,
          selectedProfileIdRef.current,
        )
        setSelectedTimestamp(matchingVisits[0]?.timestamp ?? '')
      })
      .catch((loadError) => {
        if (!cancelled) setError(String(loadError))
      })
    return () => {
      cancelled = true
    }
  }, [expeditionId])

  const visibleVisits = useMemo(
    () => filterVisitsForCommander(candidates?.visits, hasMultipleCommanders, selectedProfileId),
    [candidates?.visits, hasMultipleCommanders, selectedProfileId],
  )

  function handleChangeProfileId(nextProfileId: number) {
    userTouchedProfileRef.current = true
    setError('')
    selectedProfileIdRef.current = nextProfileId
    setSelectedProfileId(nextProfileId)
    const nextVisits = filterVisitsForCommander(candidates?.visits, hasMultipleCommanders, nextProfileId)
    setSelectedTimestamp((currentTs) =>
      nextVisits.some((visit) => visit.timestamp === currentTs) ? currentTs : (nextVisits[0]?.timestamp ?? ''),
    )
  }

  const selectedMode = modesList.find((modeItem) => modeItem.id === selectedModeId) ?? modesList[0]
  const selectedCommander = commanders.find((commander) => commander.id === selectedProfileId)

  async function handleConfirm() {
    if (busy) return
    setError('')
    if (!selectedTimestamp) {
      setError('Select a recorded visit timestamp first.')
      return
    }
    if (hasMultipleCommanders && selectedProfileId <= 0) {
      setError('Select a commander save slot.')
      return
    }
    setBusy(true)
    try {
      if (hasMultipleCommanders) {
        await StartExpeditionScoped(expeditionId, selectedTimestamp, selectedModeId, selectedProfileId)
      } else {
        await StartExpedition(expeditionId, selectedTimestamp, selectedModeId)
      }
      onClose()
    } catch (startError) {
      if (mountedRef.current) setError(String(startError))
    } finally {
      if (mountedRef.current) setBusy(false)
    }
  }

  return (
    <ModalShell title="Engage Expedition" onClose={onClose} closeDisabled={busy}>
      {error && (
        <div className="p-2.5 border border-ed-danger bg-ed-danger-tint text-ed-danger-text">{error}</div>
      )}

      <div className="space-y-3">
        <div className="p-3 border border-ed-border bg-ed-card space-y-2">
          <div className="font-bold uppercase tracking-wider text-ed-text-main">
            Recorded Visit to{' '}
            <span className="text-ed-cyan">{candidates?.systemName || 'Waypoint #1'}</span>
          </div>
          {visibleVisits.length > 0 ? (
            <select
              aria-label="Recorded visit to Waypoint #1"
              disabled={busy}
              value={selectedTimestamp}
              onChange={(event) => {
                setError('')
                setSelectedTimestamp(event.target.value)
              }}
              className="w-full bg-ed-void border border-ed-border px-2.5 py-1.5 font-mono-tabular text-xs text-ed-text-main"
            >
              {visibleVisits.map((visit) => (
                <option key={visit.uid} value={visit.timestamp}>
                  {visit.galactic} - ({visit.event}){hasMultipleCommanders && visit.commander ? ` (${visit.commander})` : ''}
                </option>
              ))}
            </select>
          ) : (
            <div className="text-ed-text-muted italic">
              {hasMultipleCommanders && selectedCommander
                ? `No historical visits to ${candidates?.systemName || 'Waypoint #1'} found for CMDR ${selectedCommander.name} yet.`
                : `No historical visits to ${candidates?.systemName || 'Waypoint #1'} found in journals yet.`}
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
                disabled={busy}
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

        {hasMultipleCommanders && (
          <div className="p-3 border border-ed-border bg-ed-card space-y-2">
            <ExpeditionCommanderScopePicker
              commanders={commanders}
              selectedProfileId={selectedProfileId}
              disabled={busy}
              onChangeProfileId={handleChangeProfileId}
            />
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
