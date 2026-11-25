import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ListCommanderLoadGamesOnDate,
  MergeCommanders,
  ResetCommanderOverrides,
  SplitCommander,
  UnmergeCommanderAlias,
} from '../../api/backend'
import { app, replay } from '../../api/models'
import {
  CommanderBadges,
  formatCommanderSubtitle,
  hasCommanderBadges,
} from '../../components/ui/CommanderBadges'
import { ModalShell } from '../../components/ui/ModalShell'

/**
 * CommanderManageModalProps defines the target commander save-slot profile,
 * sibling commander profiles, settings update callback, and close handler.
 */
export interface CommanderManageModalProps {
  profile: replay.Commander
  commanders: replay.Commander[]
  onSettingsUpdated: (nextSettings: app.Settings) => void
  onClose: () => void
}

function useCommanderSplitSessions(profileKey: string, onLoadError: (message: string) => void) {
  const [selectedDate, setSelectedDate] = useState('')
  const [sessions, setSessions] = useState<replay.CommanderSession[]>([])
  const [selectedSessionTs, setSelectedSessionTs] = useState('')
  const [loadingSessions, setLoadingSessions] = useState(false)
  const requestIdRef = useRef(0)

  useEffect(() => {
    setSelectedDate('')
    setSessions([])
    setSelectedSessionTs('')
    setLoadingSessions(false)
    requestIdRef.current += 1
  }, [profileKey])

  useEffect(() => {
    const requestId = ++requestIdRef.current
    if (!selectedDate) {
      setSessions([])
      setSelectedSessionTs('')
      setLoadingSessions(false)
      return
    }
    let cancelled = false
    setLoadingSessions(true)
    ListCommanderLoadGamesOnDate(profileKey, selectedDate)
      .then((loaded) => {
        if (cancelled || requestIdRef.current !== requestId) return
        const nextSessions = loaded ?? []
        setSessions(nextSessions)
        setSelectedSessionTs(nextSessions[0]?.timestamp ?? '')
        setLoadingSessions(false)
      })
      .catch((queryError) => {
        if (cancelled || requestIdRef.current !== requestId) return
        setSessions([])
        setSelectedSessionTs('')
        setLoadingSessions(false)
        onLoadError(String(queryError))
      })
    return () => { cancelled = true }
  }, [onLoadError, profileKey, selectedDate])

  const effectiveTimestamp = sessions.length > 1 ? selectedSessionTs || sessions[0].timestamp : sessions.length === 1 ? sessions[0].timestamp : selectedDate
  return { selectedDate, setSelectedDate, sessions, selectedSessionTs, setSelectedSessionTs, loadingSessions, effectiveTimestamp }
}

function LinkedNamesSection({ profile, busy, onSeparateAlias }: { profile: replay.Commander; busy: boolean; onSeparateAlias: (name: string) => void }) {
  const aliases = profile.aliases && profile.aliases.length > 0
    ? profile.aliases
    : [replay.CommanderAlias.createFrom({ name: profile.name, fid: profile.fid, universe: profile.universe, eventCount: profile.eventCount, isCurrent: true, source: 'auto' })]
  return (
    <div className="p-3 border border-ed-border bg-ed-card space-y-2">
      <div className="font-bold uppercase tracking-wider text-ed-text-main">Names Linked to This Save Slot</div>
      <div className="divide-y divide-ed-border-subtle border border-ed-border bg-ed-void">
        {aliases.map((aliasItem) => {
          const isDetachable = (!aliasItem.isCurrent && aliases.length > 1) || aliasItem.source === 'manual'
          return (
            <div key={`${aliasItem.name}:${aliasItem.fid}:${aliasItem.startTimestamp}`} className="px-2.5 py-1.5 flex items-center justify-between gap-2">
              <div className="flex items-center gap-2 min-w-0">
                <span className="font-bold uppercase tracking-wider text-ed-text-main truncate">CMDR {aliasItem.name}</span>
                <span className="text-[9px] font-mono-tabular uppercase text-ed-text-dim shrink-0">{aliasItem.isCurrent ? '(CURRENT)' : '(HISTORIC)'}</span>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                <span className="font-mono-tabular text-[11px] text-ed-text-muted">{(aliasItem.eventCount ?? 0).toLocaleString()} events</span>
                {isDetachable && <button type="button" disabled={busy} onClick={() => onSeparateAlias(aliasItem.name)} className="ed-btn">SEPARATE</button>}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function SplitCareerSection({
  busy, selectedDate, sessions, selectedSessionTs, loadingSessions, onChangeDate, onChangeSessionTs, onConfirmSplit,
}: {
  busy: boolean; selectedDate: string; sessions: replay.CommanderSession[]; selectedSessionTs: string; loadingSessions: boolean
  onChangeDate: (nextDate: string) => void; onChangeSessionTs: (nextTs: string) => void; onConfirmSplit: () => void
}) {
  return (
    <div className="p-3 border border-ed-border bg-ed-card space-y-2">
      <div className="font-bold uppercase tracking-wider text-ed-text-main">Split Career at Save Wipe Date</div>
      <p className="text-[11px] text-ed-text-muted">Splits earlier journal history into an archived PRE-WIPE career profile starting at the selected date or LoadGame session.</p>
      <div className="flex items-center gap-2">
        <input type="date" aria-label="Save wipe date" disabled={busy} value={selectedDate} onChange={(event) => onChangeDate(event.target.value)} className="flex-1 bg-ed-void border border-ed-border px-2.5 py-1.5 font-mono-tabular text-xs text-ed-text-main" />
        <button type="button" disabled={busy || loadingSessions || !selectedDate} onClick={onConfirmSplit} className="ed-btn-primary">SPLIT AT DATE</button>
      </div>
      {sessions.length > 1 && (
        <select aria-label="LoadGame session on selected date" disabled={busy || loadingSessions} value={selectedSessionTs} onChange={(event) => onChangeSessionTs(event.target.value)} className="w-full bg-ed-void border border-ed-border px-2.5 py-1.5 font-mono-tabular text-xs text-ed-text-main">
          {sessions.map((sessionItem) => (
            <option key={sessionItem.timestamp} value={sessionItem.timestamp}>
              {sessionItem.galactic} - {sessionItem.ship}{sessionItem.systemName ? ` (${sessionItem.systemName})` : ''}
            </option>
          ))}
        </select>
      )}
    </div>
  )
}

/**
 * CommanderManageModal renders manual save-slot override controls for merging
 * character renames, separating aliases, splitting save wipes, and resetting rules.
 */
export function CommanderManageModal({ profile, commanders, onSettingsUpdated, onClose }: CommanderManageModalProps) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const mountedRef = useRef(true)
  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  const otherProfiles = useMemo(
    () => commanders.filter((candidate) => candidate.profileKey !== profile.profileKey),
    [commanders, profile.profileKey],
  )
  const [targetKey, setTargetKey] = useState<string>(() => otherProfiles[0]?.profileKey ?? '')
  useEffect(() => {
    if (!otherProfiles.some((candidate) => candidate.profileKey === targetKey)) setTargetKey(otherProfiles[0]?.profileKey ?? '')
  }, [otherProfiles, targetKey])

  const splitState = useCommanderSplitSessions(profile.profileKey, setError)

  async function runOverrideMutation(
    mutation: () => Promise<app.Settings>,
    options?: { closeAfterSuccess?: boolean; resetSplitDate?: boolean },
  ) {
    if (busy) return
    setError('')
    setBusy(true)
    try {
      onSettingsUpdated(await mutation())
      if (options?.resetSplitDate && mountedRef.current) {
        splitState.setSelectedDate('')
      }
      if (options?.closeAfterSuccess) onClose()
    } catch (mutationError) {
      if (mountedRef.current) setError(String(mutationError))
    } finally {
      if (mountedRef.current) setBusy(false)
    }
  }

  return (
    <ModalShell title={`Manage CMDR ${profile.name}`} subtitle={formatCommanderSubtitle(profile) || 'Save-slot identity and career era overrides'} onClose={onClose} closeDisabled={busy}>
      {error && <div className="p-2.5 border border-ed-danger bg-ed-danger-tint text-ed-danger-text">{error}</div>}
      {hasCommanderBadges(profile) && (
        <div className="flex items-center gap-1.5">
          <CommanderBadges universe={profile.universe} isArchivedCareer={profile.isArchivedCareer} hasManualOverride={profile.hasManualOverride} />
        </div>
      )}
      <LinkedNamesSection profile={profile} busy={busy} onSeparateAlias={(aliasName) => void runOverrideMutation(() => UnmergeCommanderAlias(aliasName, profile.profileKey))} />
      {otherProfiles.length > 0 && (
        <div className="p-3 border border-ed-border bg-ed-card space-y-2">
          <div className="font-bold uppercase tracking-wider text-ed-text-main">Merge as Character Rename</div>
          <p className="text-[11px] text-ed-text-muted">Folds this profile into another save slot as part of the same commander career.</p>
          <div className="flex items-center gap-2">
            <select aria-label="Target save-slot profile" disabled={busy} value={targetKey} onChange={(event) => setTargetKey(event.target.value)} className="flex-1 bg-ed-void border border-ed-border px-2.5 py-1.5 font-mono-tabular text-xs text-ed-text-main">
              {otherProfiles.map((candidate) => (
                <option key={candidate.profileKey} value={candidate.profileKey}>
                  CMDR {candidate.name}{candidate.fid ? ` (${candidate.fid})` : ''}{candidate.isArchivedCareer ? ' [PRE-WIPE]' : ''}
                </option>
              ))}
            </select>
            <button type="button" disabled={busy || !targetKey} onClick={() => void runOverrideMutation(() => MergeCommanders(profile.profileKey, targetKey), { closeAfterSuccess: true })} className="ed-btn-cyan">MERGE INTO SELECTED</button>
          </div>
        </div>
      )}
      <SplitCareerSection
        busy={busy} selectedDate={splitState.selectedDate} sessions={splitState.sessions} selectedSessionTs={splitState.selectedSessionTs} loadingSessions={splitState.loadingSessions}
        onChangeDate={(nextDate) => { setError(''); splitState.setSelectedDate(nextDate) }} onChangeSessionTs={splitState.setSelectedSessionTs}
        onConfirmSplit={() => void runOverrideMutation(() => SplitCommander(profile.profileKey, splitState.effectiveTimestamp), { resetSplitDate: true })}
      />
      <div className="flex items-center justify-between pt-2 border-t border-ed-border">
        <button type="button" disabled={!profile.hasManualOverride || busy} onClick={() => void runOverrideMutation(() => ResetCommanderOverrides(profile.profileKey))} className="ed-btn">RESET TO AUTO-DETECTED</button>
        <button type="button" disabled={busy} onClick={onClose} className="ed-btn">CLOSE</button>
      </div>
    </ModalShell>
  )
}
