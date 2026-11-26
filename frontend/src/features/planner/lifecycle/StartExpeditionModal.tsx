import { useEffect, useRef, useState } from 'react'
import { GetStartCandidates, StartExpedition, StartExpeditionScoped } from '../../../api/backend'
import { app, modes, replay } from '../../../api/models'
import { ModalShell } from '../../../components/ui/ModalShell'
import { ED_AMBER_HEX } from '../../../domain/theme'
import { ExpeditionCommanderScopePicker, type CommanderScopeMode } from './ExpeditionCommanderScopePicker'

/**
 * StartExpeditionModalProps defines the expedition identifier, available flight
 * modes, commander profiles, initial scope, and close callback for engaging a planned expedition.
 */
export interface StartExpeditionModalProps {
  expeditionId: number
  modesList?: modes.Mode[]
  initialModeId?: number
  defaultModeId?: number
  commanders?: replay.Commander[]
  initialCommanderScope?: string
  initialCommanderProfileIds?: number[]
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

function resolveMatchingVisitTimestamp(visits: replay.Arrival[] | undefined, currentTs: string, profileIds: number[]): string {
  if (!visits || visits.length === 0 || profileIds.length === 0) return currentTs
  const selectedVisit = visits.find((visit) => visit.timestamp === currentTs)
  if ((selectedVisit?.profileId ?? 0) > 0 && !profileIds.includes(selectedVisit!.profileId!)) {
    const matchingVisit = visits.find((visit) => (visit.profileId ?? 0) > 0 && profileIds.includes(visit.profileId!))
    if (matchingVisit) return matchingVisit.timestamp
  }
  return currentTs
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
  initialCommanderScope,
  initialCommanderProfileIds,
  onClose,
}: StartExpeditionModalProps) {
  const hasMultipleCommanders = commanders.length > 1
  const hasExplicitSelected = initialCommanderScope === 'selected' && (initialCommanderProfileIds?.length ?? 0) > 0
  const [candidates, setCandidates] = useState<app.StartCandidates | null>(null)
  const [selectedTimestamp, setSelectedTimestamp] = useState<string>('')
  const [selectedModeId, setSelectedModeId] = useState<number>(() =>
    resolveDefaultModeId(modesList, initialModeId, defaultModeId),
  )
  const [scopeMode, setScopeMode] = useState<CommanderScopeMode>(() => (hasExplicitSelected ? 'selected' : 'all_enabled'))
  const [selectedProfileIds, setSelectedProfileIds] = useState<number[]>(() => (hasExplicitSelected ? [...(initialCommanderProfileIds ?? [])] : []))
  const [busy, setBusy] = useState<boolean>(false)
  const [error, setError] = useState<string>('')
  const mountedRef = useRef(true)
  const userTouchedScopeRef = useRef(false)
  const scopeModeRef = useRef<CommanderScopeMode>(scopeMode)
  scopeModeRef.current = scopeMode
  const selectedProfileIdsRef = useRef<number[]>(selectedProfileIds)
  selectedProfileIdsRef.current = selectedProfileIds

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    setSelectedModeId(resolveDefaultModeId(modesList, initialModeId, defaultModeId))
  }, [defaultModeId, initialModeId, modesList])

  function applySelectedProfileScope(nextIds: number[]) {
    scopeModeRef.current = 'selected'
    selectedProfileIdsRef.current = nextIds
    setScopeMode('selected')
    setSelectedProfileIds(nextIds)
  }

  useEffect(() => {
    let cancelled = false
    GetStartCandidates(expeditionId)
      .then((candidatesResult) => {
        if (cancelled) return
        setCandidates(candidatesResult)
        const firstVisit = candidatesResult.visits?.[0]
        if (!firstVisit) {
          setSelectedTimestamp('')
          return
        }
        if (hasMultipleCommanders && !hasExplicitSelected && !userTouchedScopeRef.current && (firstVisit.profileId ?? 0) > 0) {
          applySelectedProfileScope([firstVisit.profileId!])
          setSelectedTimestamp(firstVisit.timestamp)
          return
        }
        setSelectedTimestamp(
          hasMultipleCommanders && scopeModeRef.current === 'selected'
            ? resolveMatchingVisitTimestamp(candidatesResult.visits, firstVisit.timestamp, selectedProfileIdsRef.current)
            : firstVisit.timestamp,
        )
      })
      .catch((loadError) => {
        if (!cancelled) setError(String(loadError))
      })
    return () => {
      cancelled = true
    }
  }, [expeditionId, hasExplicitSelected, hasMultipleCommanders])

  function handleSelectVisit(nextTimestamp: string) {
    setError('')
    setSelectedTimestamp(nextTimestamp)
    if (hasMultipleCommanders && !hasExplicitSelected && !userTouchedScopeRef.current) {
      const matchedVisit = candidates?.visits.find((visit) => visit.timestamp === nextTimestamp)
      if (matchedVisit && (matchedVisit.profileId ?? 0) > 0) {
        applySelectedProfileScope([matchedVisit.profileId!])
      }
    }
  }

  function handleChangeScopeMode(nextMode: CommanderScopeMode) {
    userTouchedScopeRef.current = true
    setError('')
    scopeModeRef.current = nextMode
    setScopeMode(nextMode)
    if (nextMode === 'selected' && selectedProfileIdsRef.current.length > 0) {
      setSelectedTimestamp((currentTs) => resolveMatchingVisitTimestamp(candidates?.visits, currentTs, selectedProfileIdsRef.current))
    }
  }

  function handleChangeSelectedProfileIds(nextIds: number[]) {
    userTouchedScopeRef.current = true
    setError('')
    selectedProfileIdsRef.current = nextIds
    setSelectedProfileIds(nextIds)
    if (scopeModeRef.current === 'selected') {
      setSelectedTimestamp((currentTs) => resolveMatchingVisitTimestamp(candidates?.visits, currentTs, nextIds))
    }
  }

  const selectedMode = modesList.find((modeItem) => modeItem.id === selectedModeId) ?? modesList[0]

  async function handleConfirm() {
    if (busy) return
    setError('')
    if (!selectedTimestamp) {
      setError('Select a recorded visit timestamp first.')
      return
    }
    if (hasMultipleCommanders && scopeMode === 'selected') {
      if (selectedProfileIds.length === 0) {
        setError('Select at least one commander save slot.')
        return
      }
      const selectedVisit = candidates?.visits.find((visit) => visit.timestamp === selectedTimestamp)
      if ((selectedVisit?.profileId ?? 0) > 0 && !selectedProfileIds.includes(selectedVisit!.profileId!)) {
        setError('Selected Waypoint #1 visit does not belong to any selected commander save slot.')
        return
      }
    }
    setBusy(true)
    try {
      if (hasMultipleCommanders) {
        await StartExpeditionScoped(expeditionId, selectedTimestamp, selectedModeId, scopeMode, scopeMode === 'selected' ? selectedProfileIds : [])
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
          {candidates && candidates.visits.length > 0 ? (
            <select
              aria-label="Recorded visit to Waypoint #1"
              disabled={busy}
              value={selectedTimestamp}
              onChange={(event) => handleSelectVisit(event.target.value)}
              className="w-full bg-ed-void border border-ed-border px-2.5 py-1.5 font-mono-tabular text-xs text-ed-text-main"
            >
              {candidates.visits.map((visit) => (
                <option key={visit.uid} value={visit.timestamp}>
                  {visit.galactic} - ({visit.event}){hasMultipleCommanders && visit.commander ? ` (${visit.commander})` : ''}
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
              scopeMode={scopeMode}
              selectedProfileIds={selectedProfileIds}
              disabled={busy}
              onChangeScopeMode={handleChangeScopeMode}
              onChangeSelectedProfileIds={handleChangeSelectedProfileIds}
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
