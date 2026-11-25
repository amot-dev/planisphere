import { replay } from '../../api/models'

/**
 * CommanderBadgesProps defines the save-slot classification flags rendered as
 * cockpit status badges alongside a commander display name.
 */
export interface CommanderBadgesProps {
  universe?: string
  isArchivedCareer?: boolean
  hasManualOverride?: boolean
}

const BADGE_CLASS =
  'px-1.5 py-0.5 text-[9px] font-mono-tabular font-bold uppercase tracking-wider border shrink-0'

/**
 * hasCommanderBadges reports whether at least one cockpit badge applies to the
 * given commander save-slot flags.
 */
export function hasCommanderBadges({ universe, isArchivedCareer, hasManualOverride }: CommanderBadgesProps): boolean {
  return universe === 'legacy' || universe === 'beta' || Boolean(isArchivedCareer) || Boolean(hasManualOverride)
}

/**
 * CommanderBadges renders universe, archived save-wipe era, and manual override
 * badges shared across Settings, Setup, and Expedition Scope selectors.
 */
export function CommanderBadges({ universe, isArchivedCareer, hasManualOverride }: CommanderBadgesProps) {
  if (!hasCommanderBadges({ universe, isArchivedCareer, hasManualOverride })) return null
  const isLegacy = universe === 'legacy'
  const isBeta = universe === 'beta'
  return (
    <>
      {isLegacy && <span className={`${BADGE_CLASS} bg-ed-control border-ed-border-strong text-ed-text-muted`}>LEGACY 3.8</span>}
      {isBeta && <span className={`${BADGE_CLASS} bg-ed-cyan-tint border-ed-cyan-border text-ed-cyan`}>BETA</span>}
      {isArchivedCareer && <span className={`${BADGE_CLASS} bg-ed-amber-tint border-ed-amber/70 text-ed-amber`}>PRE-WIPE</span>}
      {hasManualOverride && <span className={`${BADGE_CLASS} bg-ed-cyan-tint border-ed-cyan/60 text-ed-cyan`}>MANUAL</span>}
    </>
  )
}

/**
 * CommanderAliasSublist renders the indented per-name event breakdown whenever
 * a save-slot profile links multiple observed commander aliases.
 */
export function CommanderAliasSublist({ aliases }: { aliases?: replay.CommanderAlias[] }) {
  if (!aliases || aliases.length <= 1) return null
  return (
    <div className="mt-1.5 ml-6 pl-2.5 border-l border-ed-border space-y-0.5">
      {aliases.map((aliasItem) => (
        <div key={`${aliasItem.name}:${aliasItem.fid}:${aliasItem.startTimestamp}`} className="flex items-center justify-between gap-2 text-[10px] font-mono-tabular text-ed-text-muted">
          <div className="flex items-center gap-1.5 min-w-0">
            <span className="truncate text-ed-text-body">CMDR {aliasItem.name}</span>
            <span className="text-[9px] uppercase text-ed-text-dim shrink-0">{aliasItem.isCurrent ? '(CURRENT)' : '(HISTORIC)'}</span>
          </div>
          <span className="shrink-0">{(aliasItem.eventCount ?? 0).toLocaleString()} events</span>
        </div>
      ))}
    </div>
  )
}

/**
 * formatCommanderSubtitle formats a save-slot profile's Frontier account FID
 * and Galactic Standard Time date span into a concise metadata line.
 */
export function formatCommanderSubtitle(commander: replay.Commander): string {
  const parts: string[] = []
  if (commander.fid) parts.push(`FID ${commander.fid}`)
  const firstDate = commander.firstGalactic ? commander.firstGalactic.slice(0, 10) : ''
  const lastDate = commander.lastGalactic ? commander.lastGalactic.slice(0, 10) : ''
  if (firstDate && lastDate) parts.push(firstDate === lastDate ? firstDate : `${firstDate} - ${lastDate}`)
  else if (firstDate || lastDate) parts.push(firstDate || lastDate)
  return parts.join(' · ')
}
