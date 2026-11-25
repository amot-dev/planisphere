import { useEffect, useState } from 'react'
import { SetCareerBaselineScope, SetCommanderEnabled } from '../../api/backend'
import { app, replay } from '../../api/models'
import {
  CommanderAliasSublist,
  CommanderBadges,
  formatCommanderSubtitle,
} from '../../components/ui/CommanderBadges'
import { SettingsCard } from '../../components/ui/SettingsCard'
import { SquareCheckbox } from '../../components/ui/SquareCheckbox'
import { CommanderManageModal } from './CommanderManageModal'

type CareerBaselineScopeMode = 'expedition' | 'all_enabled'

/**
 * CommandersSectionProps defines the discovered commanders list and callbacks
 * for toggling commander inclusion and managing save-slot identity rules.
 */
export interface CommandersSectionProps {
  settings: app.Settings | null
  onSetError: (errorMessage: string) => void
}

function CareerBaselineScopeControl({
  selectedScope,
  disabled,
  onSelectScope,
}: {
  selectedScope: CareerBaselineScopeMode
  disabled: boolean
  onSelectScope: (nextScope: CareerBaselineScopeMode) => void
}) {
  return (
    <div className="p-2.5 border border-ed-border bg-ed-void flex flex-wrap items-center justify-between gap-2">
      <div className="min-w-0">
        <div className="font-bold uppercase tracking-wider text-ed-text-main">Career and Baseline Scope</div>
        <p className="text-[11px] text-ed-text-muted">Controls Sidebar All-Time, Origin Baseline, and Export Career Diff totals.</p>
      </div>
      <div role="group" aria-label="Career and Baseline Scope" className="inline-flex gap-1 shrink-0">
        {([
          { value: 'expedition', label: 'Expedition Profile' },
          { value: 'all_enabled', label: 'All Enabled' },
        ] as const).map((optionItem) => {
          const isSelected = selectedScope === optionItem.value
          return (
            <button
              key={optionItem.value}
              type="button"
              disabled={disabled}
              aria-pressed={isSelected}
              onClick={() => { if (!isSelected) onSelectScope(optionItem.value) }}
              className={`px-2.5 py-1 text-[10px] font-mono-tabular font-bold uppercase tracking-wider border transition-colors cursor-pointer whitespace-nowrap disabled:opacity-40 disabled:cursor-not-allowed ${
                isSelected ? 'bg-ed-amber-tint text-ed-amber border-ed-amber' : 'bg-ed-surface text-ed-text-muted border-ed-border-dim hover:text-ed-text-main hover:border-ed-border-strong'
              }`}
            >
              {optionItem.label}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function CommanderProfileRow({
  commanderItem,
  busy,
  onToggleEnabled,
  onManage,
}: {
  commanderItem: replay.Commander
  busy: boolean
  onToggleEnabled: (checked: boolean) => void
  onManage: () => void
}) {
  const subtitle = formatCommanderSubtitle(commanderItem)
  return (
    <div className="px-3 py-2 hover:bg-ed-control">
      <div className="flex items-center justify-between gap-3">
        <label className={`flex items-center gap-2.5 min-w-0 flex-1 ${busy ? 'cursor-not-allowed' : 'cursor-pointer'}`}>
          <SquareCheckbox checked={commanderItem.enabled} disabled={busy} onChange={onToggleEnabled} />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="font-bold uppercase tracking-wider text-ed-text-main">CMDR {commanderItem.name}</span>
              <CommanderBadges universe={commanderItem.universe} isArchivedCareer={commanderItem.isArchivedCareer} hasManualOverride={commanderItem.hasManualOverride} />
            </div>
            {subtitle && <div className="text-[10px] font-mono-tabular text-ed-text-muted">{subtitle}</div>}
          </div>
        </label>
        <div className="flex items-center gap-2.5 shrink-0">
          <span className="font-mono-tabular text-xs font-bold text-ed-text-main">{(commanderItem.eventCount ?? 0).toLocaleString()} events</span>
          <button type="button" disabled={busy} onClick={onManage} className="ed-btn">MANAGE</button>
        </div>
      </div>
      <CommanderAliasSublist aliases={commanderItem.aliases} />
    </div>
  )
}

/**
 * CommandersSection renders discovered Elite Dangerous save-slot career profiles,
 * historic alias counts, the career baseline scope toggle, and profile management.
 */
export function CommandersSection({ settings, onSetError }: CommandersSectionProps) {
  const [localSettings, setLocalSettings] = useState<app.Settings | null>(settings)
  const [busy, setBusy] = useState(false)
  const [managingKey, setManagingKey] = useState<string | null>(null)

  useEffect(() => { setLocalSettings(settings) }, [settings])

  const commanders = localSettings?.commanders ?? []
  const baselineScope: CareerBaselineScopeMode = localSettings?.careerBaselineScope === 'all_enabled' ? 'all_enabled' : 'expedition'
  const managedProfile = managingKey ? (commanders.find((item) => (item.profileKey || item.name) === managingKey) ?? null) : null

  useEffect(() => {
    if (managingKey !== null && !managedProfile) {
      setManagingKey(null)
    }
  }, [managedProfile, managingKey])

  async function runSettingsAction(action: () => Promise<app.Settings>) {
    if (busy) return
    onSetError('')
    setBusy(true)
    try {
      const nextSettings = await action()
      if (nextSettings) setLocalSettings(nextSettings)
    } catch (actionError) {
      onSetError(String(actionError))
    } finally {
      setBusy(false)
    }
  }

  return (
    <SettingsCard title="Commanders" headerRight={<span className="text-[10px] text-ed-text-muted">Select save slots to track</span>}>
      {commanders.length > 1 && (
        <CareerBaselineScopeControl selectedScope={baselineScope} disabled={busy} onSelectScope={(nextScope) => void runSettingsAction(() => SetCareerBaselineScope(nextScope))} />
      )}
      {commanders.length === 0 ? (
        <div className="text-ed-text-dim font-mono-tabular uppercase py-1 text-[10px]">No commanders detected yet.</div>
      ) : (
        <div className="divide-y divide-ed-border-subtle border border-ed-border bg-ed-void">
          {commanders.map((commanderItem) => {
            const identifier = commanderItem.profileKey || commanderItem.name
            return (
              <CommanderProfileRow
                key={identifier} commanderItem={commanderItem} busy={busy}
                onToggleEnabled={(checked) => void runSettingsAction(() => SetCommanderEnabled(identifier, checked))}
                onManage={() => setManagingKey(identifier)}
              />
            )
          })}
        </div>
      )}
      {managedProfile && (
        <CommanderManageModal
          profile={managedProfile} commanders={commanders}
          onSettingsUpdated={(nextSettings) => { if (nextSettings) setLocalSettings(nextSettings) }}
          onClose={() => setManagingKey(null)}
        />
      )}
    </SettingsCard>
  )
}
