import { useEffect, useState } from 'react'
import {
  BrowseEDDiscoveryDB,
  BrowseJournalDir,
  CloneExpedition,
  CreateExpedition,
  CreateMode,
  DeleteExpedition,
  DeleteMode,
  GetStartCandidates,
  ImportEDDiscoveryHistory,
  RefreshGEC,
  RescanJournals,
  SetCommanderEnabled,
  SetJournalDir,
  SetOnlineLookups,
  StartExpedition,
  StartExpeditionOnArrival,
  UpdateExpedition,
  UpdateMode,
} from '../../wailsjs/go/app/App'
import { app, expedition } from '../../wailsjs/go/models'
import { fromGalacticTime, nowGalacticTime } from '../utils/galacticTime'
import { SquareCheckbox } from './SquareCheckbox'

function ModalShell({
  title,
  subtitle,
  onClose,
  children,
  maxWidth = 'max-w-xl',
}: {
  title: string
  subtitle?: string
  onClose: () => void
  children: React.ReactNode
  maxWidth?: string
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4">
      <div
        className={`w-full ${maxWidth} max-h-[90vh] flex flex-col border border-[#3d2816] border-t-2 border-t-[#FF7100] bg-[#0b0907] shadow-2xl overflow-hidden`}
      >
        <div className="px-5 py-3.5 border-b border-[#3d2816] bg-[#140e09] flex items-center justify-between">
          <div>
            <h3 className="text-xs font-extrabold uppercase tracking-[0.16em] text-[#FF7100]">
              {title}
            </h3>
            {subtitle && <p className="text-[11px] text-[#a6907c] mt-0.5">{subtitle}</p>}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="px-2 py-1 text-[#a6907c] hover:text-[#F4ECE4] hover:bg-[#24180e] cursor-pointer"
          >
            ✕
          </button>
        </div>
        <div className="p-5 overflow-y-auto space-y-4 text-xs">{children}</div>
      </div>
    </div>
  )
}

export function StartExpeditionModal({
  expeditionId,
  onClose,
  onStarted,
}: {
  expeditionId: number
  onClose: () => void
  onStarted: () => void
}) {
  const [candidates, setCandidates] = useState<app.StartCandidates | null>(null)
  const [selectedTimestamp, setSelectedTimestamp] = useState<string>('')
  const [customGalactic, setCustomGalactic] = useState<string>(() => nowGalacticTime())
  const [mode, setMode] = useState<'history' | 'custom' | 'arrival'>('history')
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
          setMode('arrival')
        }
      })
      .catch((error) => setError(String(error)))
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
      } else if (mode === 'custom') {
        const isoTimestamp = fromGalacticTime(customGalactic)
        await StartExpedition(expeditionId, isoTimestamp)
      } else {
        await StartExpeditionOnArrival(expeditionId)
      }
      onStarted()
      onClose()
    } catch (error) {
      setError(String(error))
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

        {/* Option 2: Wait for first arrival */}
        <label className="flex items-start gap-2.5 p-3 border border-[#3d2816] bg-[#110c08] cursor-pointer">
          <input
            type="radio"
            name="startMode"
            checked={mode === 'arrival'}
            onChange={() => setMode('arrival')}
            className="mt-0.5 accent-[#FF7100]"
          />
          <div className="flex-1">
            <div className="font-bold uppercase tracking-wider text-[#F4ECE4]">
              Arm &amp; Wait for First Arrival at Waypoint #1
            </div>
            <p className="text-[11px] text-[#a6907c] mt-0.5">
              The moment your ship or carrier arrives in{' '}
              <span className="text-[#F4ECE4]">{candidates?.systemName || 'Waypoint #1'}</span>, that
              timestamp locks in as the expedition start.
            </p>
          </div>
        </label>

        {/* Option 3: Custom Galactic Timestamp */}
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
              Custom Timestamp (33XX-MM-DD HH:mm:ss UTC)
            </div>
            <input
              type="text"
              value={customGalactic}
              onFocus={() => setMode('custom')}
              onChange={(event) => {
                setCustomGalactic(event.target.value)
                setMode('custom')
              }}
              placeholder="3312-01-18 19:42:10 UTC"
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
          Cancel
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={handleConfirm}
          className="ed-btn-primary"
        >
          {busy ? 'ENGAGING...' : 'Confirm & Engage'}
        </button>
      </div>
    </ModalShell>
  )
}

export function SettingsModal({
  settings,
  ingestStatus,
  modesState,
  eddProgress,
  onClose,
  onRefresh,
  onOpenSetupWizard,
}: {
  settings: app.Settings | null
  ingestStatus: app.IngestStatus | null
  modesState: app.ModesState | null
  eddProgress?: {
    active: boolean
    stage: string
    current: number
    total: number
    percent: number
    files: number
  } | null
  onClose: () => void
  onRefresh: () => void
  onOpenSetupWizard?: () => void
}) {
  const [dir, setDir] = useState(settings?.journalDir ?? '')
  const [online, setOnline] = useState(settings?.onlineLookups ?? true)
  const [gecBusy, setGecBusy] = useState(false)
  const [eddPath, setEddPath] = useState(settings?.edDiscoveryDbPath ?? '')
  const [eddBusy, setEddBusy] = useState(false)
  const [eddStatus, setEddStatus] = useState('')
  const [error, setError] = useState('')

  // Modes management state within Settings
  const [newModeName, setNewModeName] = useState('')
  const [newModeDesc, setNewModeDesc] = useState('')
  const [newModeColor, setNewModeColor] = useState('#FF7100')

  async function handleBrowse() {
    try {
      const picked = await BrowseJournalDir()
      if (picked) setDir(picked)
    } catch (error) {
      setError(String(error))
    }
  }

  async function handleSaveDir() {
    setError('')
    try {
      await SetJournalDir(dir)
      onRefresh()
    } catch (error) {
      setError(String(error))
    }
  }

  async function handleResetDefaultDir() {
    setError('')
    try {
      await SetJournalDir('')
      setDir(settings?.defaultJournalDir ?? '')
      onRefresh()
    } catch (error) {
      setError(String(error))
    }
  }

  async function handleToggleOnline(next: boolean) {
    setOnline(next)
    await SetOnlineLookups(next)
    onRefresh()
  }

  async function handleRefreshGec() {
    setGecBusy(true)
    setError('')
    try {
      await RefreshGEC()
      onRefresh()
    } catch (error) {
      setError(String(error))
    } finally {
      setGecBusy(false)
    }
  }

  async function handleToggleCommander(name: string, enabled: boolean) {
    setError('')
    try {
      await SetCommanderEnabled(name, enabled)
      onRefresh()
    } catch (error) {
      setError(String(error))
    }
  }

  async function handleBrowseEdd() {
    try {
      const picked = await BrowseEDDiscoveryDB()
      if (picked) setEddPath(picked)
    } catch (error) {
      setError(String(error))
    }
  }

  async function handleImportEdd() {
    setEddBusy(true)
    setError('')
    setEddStatus('')
    try {
      const count = await ImportEDDiscoveryHistory(eddPath)
      setEddStatus(
        `Imported ${count.toLocaleString()} historical journal events into local database.`,
      )
      onRefresh()
    } catch (error) {
      setError(String(error))
    } finally {
      setEddBusy(false)
    }
  }

  async function handleCreateMode() {
    setError('')
    try {
      await CreateMode(newModeName, newModeDesc, newModeColor)
      setNewModeName('')
      setNewModeDesc('')
      onRefresh()
    } catch (error) {
      setError(String(error))
    }
  }

  return (
    <ModalShell
      title="Terminal Configuration"
      onClose={onClose}
      maxWidth="max-w-2xl"
    >
      {error && (
        <div className="p-2.5 border border-[#ff3b2f] bg-[#190909] text-[#ff4d3d]">{error}</div>
      )}

      {/* Journal Directory */}
      <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
            Player Journal Directory
          </span>
          <span
            className={`text-[10px] font-mono-tabular font-bold uppercase px-2 py-0.5 border ${
              settings?.journalDirExists
                ? 'bg-[#08151c] border-[#00D2FF] text-[#00D2FF]'
                : 'bg-[#211307] border-[#FF7100] text-[#FF7100]'
            }`}
          >
            {settings?.journalDirExists ? 'FOUND' : 'NOT FOUND'}
          </span>
        </div>

        <div className="flex items-center gap-2">
          <input
            type="text"
            value={dir}
            onChange={(event) => setDir(event.target.value)}
            className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-xs text-[#F4ECE4]"
          />
          <button
            type="button"
            onClick={handleBrowse}
            className="ed-btn"
          >
            Browse…
          </button>
          <button
            type="button"
            onClick={handleSaveDir}
            className="ed-btn-primary"
          >
            Apply
          </button>
          <button
            type="button"
            onClick={handleResetDefaultDir}
            title="Reset journal directory to the game default"
            className="ed-btn-cyan"
          >
            Reset to Default
          </button>
        </div>
      </div>

      {/* Database Storage Location & Live Watcher */}
      <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#00D2FF]">
            Database &amp; Event Archive
          </span>
          <button
            type="button"
            onClick={() => RescanJournals().then(onRefresh)}
            className="ed-btn-cyan"
          >
            ↻ Rescan Journals
          </button>
        </div>
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-2 font-mono-tabular text-xs">
          <div className="p-2 bg-[#050506] border border-[#3d2816]">
            <div className="text-[10px] uppercase text-[#a6907c]">Tracked Files</div>
            <div className="text-sm font-bold text-[#F4ECE4]">
              {ingestStatus?.filesTracked ?? 0}
            </div>
          </div>
          <div className="p-2 bg-[#050506] border border-[#3d2816]">
            <div className="text-[10px] uppercase text-[#a6907c]">Stored Events</div>
            <div className="text-sm font-bold text-[#FF7100]">
              {(ingestStatus?.eventsStored ?? 0).toLocaleString()}
            </div>
          </div>
          <div className="p-2 bg-[#050506] border border-[#3d2816]">
            <div className="text-[10px] uppercase text-[#a6907c]">Live Watcher</div>
            <div className="text-sm font-bold text-[#00D2FF]">
              {ingestStatus?.watcherActive ? 'ACTIVE' : 'STANDBY'}
            </div>
          </div>
        </div>
        <div className="text-[11px] text-[#a6907c] font-mono-tabular flex items-center justify-between">
          <span>Database Path:</span>
          <span className="text-[#F4ECE4] font-bold">{settings?.dbPath}</span>
        </div>
      </div>

      {/* Environment Modes Management */}
      <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2.5">
        <div className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
          Environment Modes
        </div>
        <div className="divide-y divide-[#24180e] border border-[#3d2816] bg-[#050506]">
          {(modesState?.modes ?? []).map((modeItem) => (
            <div key={modeItem.id} className="p-2 flex items-center justify-between gap-3">
              <div className="flex items-center gap-2.5 min-w-0">
                <input
                  type="color"
                  value={modeItem.color}
                  onChange={(event) =>
                    UpdateMode(modeItem.id, modeItem.name, modeItem.description, event.target.value).then(onRefresh)
                  }
                  className="w-5 h-5 bg-transparent border border-[#3d2816] cursor-pointer shrink-0"
                />
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-bold uppercase tracking-wider text-[#F4ECE4]">
                      {modeItem.name}
                    </span>
                    {modeItem.isDefault && (
                      <span className="text-[9px] font-mono-tabular uppercase px-1.5 py-0.2 bg-[#16100b] border border-[#3d2816] text-[#a6907c]">
                        DEFAULT
                      </span>
                    )}
                  </div>
                  {modeItem.description && (
                    <div className="text-[10px] text-[#a6907c] truncate">{modeItem.description}</div>
                  )}
                </div>
              </div>
              {!modeItem.isDefault && (
                <button
                  type="button"
                  onClick={() => DeleteMode(modeItem.id).then(onRefresh)}
                  className="ed-btn-danger"
                >
                  Delete
                </button>
              )}
            </div>
          ))}
        </div>

        {/* Add Custom Mode */}
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2 pt-1">
          <input
            type="text"
            value={newModeName}
            onChange={(event) => setNewModeName(event.target.value)}
            placeholder="New mode name..."
            className="bg-[#050506] border border-[#3d2816] px-2.5 py-1 text-[#F4ECE4]"
          />
          <input
            type="text"
            value={newModeDesc}
            onChange={(event) => setNewModeDesc(event.target.value)}
            placeholder="Optional description..."
            className="bg-[#050506] border border-[#3d2816] px-2.5 py-1 text-[#F4ECE4]"
          />
          <div className="flex items-center gap-2">
            <input
              type="color"
              value={newModeColor}
              onChange={(event) => setNewModeColor(event.target.value)}
              className="w-7 h-7 bg-transparent border border-[#3d2816] cursor-pointer"
            />
            <button
              type="button"
              onClick={handleCreateMode}
              className="flex-1 ed-btn-primary"
            >
              Add Mode
            </button>
          </div>
        </div>
      </div>

      {/* Commanders */}
      <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
            Commanders
          </span>
          <span className="text-[10px] text-[#a6907c]">
            Select commanders to track
          </span>
        </div>
        {(settings?.commanders ?? []).length === 0 ? (
          <div className="text-[#6e5b4b] font-mono-tabular uppercase py-1 text-[10px]">
            No commanders detected yet.
          </div>
        ) : (
          <div className="divide-y divide-[#24180e] border border-[#3d2816] bg-[#050506]">
            {(settings?.commanders ?? []).map((commanderItem) => (
              <label
                key={commanderItem.name}
                className="px-3 py-2 flex items-center justify-between gap-3 cursor-pointer hover:bg-[#16100b]"
              >
                <div className="flex items-center gap-2.5 min-w-0">
                  <SquareCheckbox
                    checked={commanderItem.enabled}
                    onChange={(checked) => handleToggleCommander(commanderItem.name, checked)}
                  />
                  <div className="min-w-0">
                    <span className="font-bold uppercase tracking-wider text-[#F4ECE4]">
                      CMDR {commanderItem.name}
                    </span>
                  </div>
                </div>
                <span className="font-mono-tabular text-xs font-bold text-[#00D2FF] shrink-0">
                  {commanderItem.eventCount.toLocaleString()} events
                </span>
              </label>
            ))}
          </div>
        )}
      </div>

      {/* EDDiscovery Recovery */}
      <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#00D2FF]">
            Historical Journal Import (EDDiscovery)
          </span>
        </div>
        <p className="text-[11px] text-[#a6907c]">
          Import past journal history from an existing EDDiscovery database.
        </p>
        <div className="flex items-center gap-2">
          <input
            type="text"
            value={eddPath}
            onChange={(event) => setEddPath(event.target.value)}
            placeholder="Path to EDDUser.sqlite..."
            className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-xs text-[#F4ECE4]"
          />
          <button
            type="button"
            onClick={handleBrowseEdd}
            className="ed-btn"
          >
            Browse…
          </button>
          <button
            type="button"
            disabled={eddBusy || Boolean(eddProgress?.active)}
            onClick={handleImportEdd}
            className="ed-btn-cyan"
          >
            {eddBusy || eddProgress?.active ? 'Importing…' : 'Import'}
          </button>
        </div>
        {(eddBusy || eddProgress?.active) && (
          <div className="space-y-1.5 pt-1">
            <div className="flex items-center justify-between text-[11px] font-mono-tabular">
              <span className="text-[#00D2FF]">
                {eddProgress?.stage || 'Reading database…'}
              </span>
              <span className="text-[#F4ECE4] font-bold">
                {eddProgress && eddProgress.total > 0
                  ? `${eddProgress.percent}% (${eddProgress.current.toLocaleString()} / ${eddProgress.total.toLocaleString()} events)`
                  : 'Preparing…'}
              </span>
            </div>
            <div className="w-full h-2 bg-[#050506] border border-[#3d2816] overflow-hidden">
              <div
                className="h-full bg-[#FF7100] transition-all duration-150"
                style={{ width: `${eddProgress?.total ? Math.max(2, eddProgress.percent) : 15}%` }}
              />
            </div>
          </div>
        )}
        {eddStatus && <div className="text-[11px] text-[#00D2FF] font-mono-tabular">{eddStatus}</div>}
      </div>

      {/* Online Services */}
      <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2.5">
        <label className="flex items-center justify-between cursor-pointer">
          <div>
            <div className="font-bold uppercase tracking-wider text-[#F4ECE4]">
              Enable Online EDSM Coordinates &amp; EDAstro GEC Sync
            </div>
            <div className="text-[11px] text-[#a6907c]">
              Fetches coordinates for unvisited systems and downloads catalog POIs
            </div>
          </div>
          <SquareCheckbox
            checked={online}
            onChange={(checked) => handleToggleOnline(checked)}
          />
        </label>

        <div className="flex items-center justify-between pt-2 border-t border-[#2e1f12]">
          <div className="text-[11px] text-[#a6907c]">
            Cached POIs:{' '}
            <span className="font-mono-tabular text-[#F4ECE4] font-bold">
              {settings?.gecCount ?? 0}
            </span>
          </div>
          <button
            type="button"
            disabled={gecBusy}
            onClick={handleRefreshGec}
            className="ed-btn"
          >
            {gecBusy ? 'Syncing…' : 'Sync GEC Catalog'}
          </button>
        </div>
      </div>

      {onOpenSetupWizard && (
        <div className="flex items-center justify-between pt-2 border-t border-[#3d2816]">
          <span className="text-[11px] text-[#a6907c]">
            Need to change your SQLite database file path or re-run initial onboarding?
          </span>
          <button
            type="button"
            onClick={onOpenSetupWizard}
            className="ed-btn"
          >
            Run Setup Wizard…
          </button>
        </div>
      )}
    </ModalShell>
  )
}

export function ExpeditionManageModal({
  expeditions,
  activeId,
  onSelect,
  onClose,
  onRefresh,
}: {
  expeditions: expedition.Expedition[]
  activeId: number
  onSelect: (id: number) => void
  onClose: () => void
  onRefresh: () => void
}) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editName, setEditName] = useState('')
  const [editDescription, setEditDescription] = useState('')

  async function handleCreate() {
    if (!name.trim()) return
    const created = await CreateExpedition(name.trim(), description.trim())
    setName('')
    setDescription('')
    onSelect(created.id)
    onClose()
  }

  async function handleSaveEdit(id: number) {
    await UpdateExpedition(id, editName, editDescription)
    setEditingId(null)
    onRefresh()
  }

  async function handleClone(id: number) {
    const cloned = await CloneExpedition(id)
    onSelect(cloned.id)
    onClose()
  }

  return (
    <ModalShell
      title="Expedition Flight Plans"
      subtitle="Create a new expedition or switch between saved expedition plans"
      onClose={onClose}
    >
      <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-2">
        <div className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
          + Create New Expedition Plan
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          <input
            type="text"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Expedition Name (e.g. Colonia & Sagittarius A*)"
            className="bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 text-[#F4ECE4]"
          />
          <input
            type="text"
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            placeholder="Optional description..."
            className="bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 text-[#F4ECE4]"
          />
        </div>
        <div className="flex justify-end">
          <button
            type="button"
            onClick={handleCreate}
            className="ed-btn-primary"
          >
            Create Expedition
          </button>
        </div>
      </div>

      <div className="divide-y divide-[#24180e] border border-[#3d2816] bg-[#110c08]">
        {expeditions.map((expeditionItem) => (
          <div key={expeditionItem.id} className="p-2.5 flex items-center justify-between gap-2">
            {editingId === expeditionItem.id ? (
              <div className="flex-1 flex items-center gap-2">
                <input
                  type="text"
                  value={editName}
                  onChange={(event) => setEditName(event.target.value)}
                  className="bg-[#050506] border border-[#3d2816] px-2 py-1 text-[#F4ECE4]"
                />
                <input
                  type="text"
                  value={editDescription}
                  onChange={(event) => setEditDescription(event.target.value)}
                  className="flex-1 bg-[#050506] border border-[#3d2816] px-2 py-1 text-[#F4ECE4]"
                />
                <button
                  type="button"
                  onClick={() => handleSaveEdit(expeditionItem.id)}
                  className="ed-btn-cyan"
                >
                  Save
                </button>
              </div>
            ) : (
              <>
                <button
                  type="button"
                  onClick={() => {
                    onSelect(expeditionItem.id)
                    onClose()
                  }}
                  className="flex-1 text-left cursor-pointer"
                >
                  <div className="flex items-center gap-2">
                    <span
                      className={`font-bold uppercase tracking-wider ${
                        expeditionItem.id === activeId ? 'text-[#FF7100]' : 'text-[#F4ECE4]'
                      }`}
                    >
                      {expeditionItem.name}
                    </span>
                    <span className="text-[9px] font-mono-tabular uppercase px-1.5 py-0.5 bg-[#16100b] border border-[#3d2816] text-[#a6907c]">
                      {expeditionItem.status}
                    </span>
                  </div>
                  {expeditionItem.description && <div className="text-[11px] text-[#a6907c]">{expeditionItem.description}</div>}
                </button>
                <div className="flex items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => handleClone(expeditionItem.id)}
                    title="Clone this expedition into a new expedition in the Planning phase"
                    className="ed-btn-cyan"
                  >
                    Clone
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setEditingId(expeditionItem.id)
                      setEditName(expeditionItem.name)
                      setEditDescription(expeditionItem.description)
                    }}
                    className="ed-btn"
                  >
                    Rename
                  </button>
                  {expeditions.length > 1 && (
                    <button
                      type="button"
                      onClick={() => DeleteExpedition(expeditionItem.id).then(onRefresh)}
                      className="ed-btn-danger"
                    >
                      Delete
                    </button>
                  )}
                </div>
              </>
            )}
          </div>
        ))}
      </div>
    </ModalShell>
  )
}
