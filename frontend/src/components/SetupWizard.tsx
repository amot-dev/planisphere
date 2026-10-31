import { useEffect, useState } from 'react'
import {
  BrowseDBPath,
  BrowseEDDiscoveryDB,
  BrowseJournalDir,
  CompleteSetup,
  GetIngestStatus,
  GetSettings,
  ImportEDDiscoveryHistory,
  RefreshGEC,
  SetCommanderEnabled,
  SetDatabasePath,
  SetOnlineLookups,
  SetupConfigureJournals,
} from '../../wailsjs/go/app/App'
import { app } from '../../wailsjs/go/models'
import { APP_BINARY_NAME, APP_PRODUCT_NAME, APP_VERSION } from '../version'

interface SetupWizardProps {
  settings: app.Settings | null
  ingestStatus: app.IngestStatus | null
  eddProgress?: {
    active: boolean
    stage: string
    current: number
    total: number
    percent: number
    files: number
  } | null
  onComplete: () => void
  onClose?: () => void
}

const TOTAL_STEPS = 4

export function SetupWizardModal({
  settings: initialSettings,
  ingestStatus: initialIngest,
  eddProgress,
  onComplete,
  onClose,
}: SetupWizardProps) {
  const [step, setStep] = useState<number>(0)
  const [localSettings, setLocalSettings] = useState<app.Settings | null>(initialSettings)
  const [localIngest, setLocalIngest] = useState<app.IngestStatus | null>(initialIngest)

  // Step 1 state: Database path
  const [dbPath, setDbPath] = useState<string>(
    initialSettings?.dbPath || initialSettings?.defaultDbPath || '',
  )

  // Step 2 state: Journals path
  const [journalDir, setJournalDir] = useState<string>(
    initialSettings?.journalDir || initialSettings?.defaultJournalDir || '',
  )

  // Step 3 state: Historical import (EDDiscovery)
  const [expectOlderJournals, setExpectOlderJournals] = useState<boolean>(false)
  const [eddPath, setEddPath] = useState<string>(
    initialSettings?.edDiscoveryDbPath || initialSettings?.defaultEdDiscoveryDbPath || '',
  )
  const [eddImportedCount, setEddImportedCount] = useState<number | null>(null)
  const [eddBusy, setEddBusy] = useState<boolean>(false)

  // Step 4 state: Commanders & Online Sync
  const [onlineLookups, setOnlineLookups] = useState<boolean>(
    initialSettings?.onlineLookups ?? true,
  )
  const [gecBusy, setGecBusy] = useState<boolean>(false)
  const [gecSyncedCount, setGecSyncedCount] = useState<number | null>(null)

  // Shared busy / error state
  const [busyMessage, setBusyMessage] = useState<string | null>(null)
  const [error, setError] = useState<string>('')

  useEffect(() => {
    if (initialSettings) {
      setLocalSettings(initialSettings)
      if (!dbPath) {
        setDbPath(initialSettings.dbPath || initialSettings.defaultDbPath || '')
      }
      if (!journalDir) {
        setJournalDir(initialSettings.journalDir || initialSettings.defaultJournalDir || '')
      }
      if (!eddPath) {
        setEddPath(
          initialSettings.edDiscoveryDbPath || initialSettings.defaultEdDiscoveryDbPath || '',
        )
      }
      setOnlineLookups(initialSettings.onlineLookups)
    }
  }, [initialSettings])

  useEffect(() => {
    if (initialIngest) {
      setLocalIngest(initialIngest)
    }
  }, [initialIngest])

  async function refreshWizardState() {
    const [currentSettings, currentIngestStatus] = await Promise.all([
      GetSettings(),
      GetIngestStatus(),
    ])
    setLocalSettings(currentSettings)
    setLocalIngest(currentIngestStatus)
    return { settings: currentSettings, ingestStatus: currentIngestStatus }
  }

  // Step 1 handlers
  async function handleBrowseDb() {
    setError('')
    try {
      const picked = await BrowseDBPath()
      if (picked) setDbPath(picked)
    } catch (error) {
      setError(String(error))
    }
  }

  function handleResetDefaultDb() {
    setError('')
    setDbPath(localSettings?.defaultDbPath || '')
  }

  // Step 2 handlers
  async function handleBrowseJournals() {
    setError('')
    try {
      const picked = await BrowseJournalDir()
      if (picked) setJournalDir(picked)
    } catch (error) {
      setError(String(error))
    }
  }

  function handleResetDefaultJournals() {
    setError('')
    setJournalDir(localSettings?.defaultJournalDir || '')
  }

  // Step 3 handlers
  async function handleBrowseEdd() {
    setError('')
    try {
      const picked = await BrowseEDDiscoveryDB()
      if (picked) setEddPath(picked)
    } catch (error) {
      setError(String(error))
    }
  }

  function handleResetDefaultEdd() {
    setError('')
    setEddPath(
      localSettings?.edDiscoveryDbPath || localSettings?.defaultEdDiscoveryDbPath || '',
    )
  }

  async function handleRunEddImport() {
    setError('')
    setEddBusy(true)
    try {
      const count = await ImportEDDiscoveryHistory(eddPath)
      setEddImportedCount(count)
      await refreshWizardState()
    } catch (error) {
      setError(String(error))
    } finally {
      setEddBusy(false)
    }
  }

  // Step 4 handlers
  async function handleToggleCmdr(name: string, enabled: boolean) {
    setError('')
    try {
      const next = await SetCommanderEnabled(name, enabled)
      setLocalSettings(next)
    } catch (error) {
      setError(String(error))
    }
  }

  async function handleToggleOnline(enabled: boolean) {
    setError('')
    setOnlineLookups(enabled)
    try {
      const next = await SetOnlineLookups(enabled)
      setLocalSettings(next)
    } catch (error) {
      setError(String(error))
    }
  }

  async function handleSyncGecNow() {
    setError('')
    setGecBusy(true)
    try {
      const count = await RefreshGEC()
      setGecSyncedCount(count)
      await refreshWizardState()
    } catch (error) {
      setError(String(error))
    } finally {
      setGecBusy(false)
    }
  }

  // Navigation transitions
  async function handleNext() {
    setError('')
    if (step === 0) {
      setBusyMessage('INITIALIZING DATABASE…')
      try {
        const nextSettings = await SetDatabasePath(dbPath)
        setLocalSettings(nextSettings)
        setDbPath(nextSettings.dbPath)
        if (!journalDir) {
          setJournalDir(nextSettings.journalDir || nextSettings.defaultJournalDir || '')
        }
        setStep(1)
      } catch (error) {
        setError(String(error))
      } finally {
        setBusyMessage(null)
      }
      return
    }

    if (step === 1) {
      setBusyMessage('SCANNING PLAYER JOURNALS…')
      try {
        const currentIngestStatus = await SetupConfigureJournals(journalDir)
        setLocalIngest(currentIngestStatus)
        const currentSettings = await GetSettings()
        setLocalSettings(currentSettings)
        setStep(2)
      } catch (error) {
        setError(String(error))
      } finally {
        setBusyMessage(null)
      }
      return
    }

    if (step === 2) {
      setStep(3)
      return
    }

    if (step === 3) {
      setBusyMessage('FINALIZING SETUP…')
      try {
        await CompleteSetup()
        onComplete()
      } catch (error) {
        setError(String(error))
      } finally {
        setBusyMessage(null)
      }
    }
  }

  function handleBack() {
    if (step > 0 && !busyMessage && !eddBusy) {
      setError('')
      setStep(step - 1)
    }
  }

  const isBusy = Boolean(busyMessage) || eddBusy || gecBusy

  // Format oldest journal date for Step 3
  const firstEventIso = localIngest?.firstEventAt || ''
  const firstEventUtcDisplay = firstEventIso
    ? firstEventIso.replace('T', ' ').replace('Z', ' UTC')
    : ''
  const firstEventGalacticDisplay = localIngest?.firstEventGalactic || ''

  const stepTitles = [
    'Step 1 of 4 — Database Location',
    'Step 2 of 4 — Journal Folder',
    'Step 3 of 4 — Historical Import',
    'Step 4 of 4 — Commanders & Online Sync',
  ]

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-[2px] p-4 select-none">
      {/* Compact Floating Window */}
      <div className="w-full max-w-lg border border-[#472e19] border-t-2 border-t-[#FF7100] bg-[#0b0907] shadow-[0_20px_60px_rgba(0,0,0,0.9)] flex flex-col overflow-hidden">
        {/* Window Top Header */}
        <div className="px-5 py-3.5 border-b border-[#3d2816] bg-[#140e09] flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <div className="w-2 h-4 bg-[#FF7100]" />
            <div>
              <div className="flex items-center gap-2">
                <h2 className="text-xs font-extrabold uppercase tracking-[0.18em] text-[#FF7100]">
                  {APP_PRODUCT_NAME} Setup
                </h2>
                <span className="font-mono-tabular text-[10px] text-[#00D2FF]">
                  v{APP_VERSION}
                </span>
              </div>
              <p className="text-[11px] text-[#a6907c] mt-0.5">{stepTitles[step]}</p>
            </div>
          </div>
          {onClose && (
            <button
              type="button"
              onClick={onClose}
              disabled={isBusy}
              title="Close setup wizard"
              className="px-2 py-1 text-xs text-[#a6907c] hover:text-[#F4ECE4] hover:bg-[#24180e] cursor-pointer"
            >
              ✕
            </button>
          )}
        </div>

        {/* Step Body */}
        <div className="p-5 space-y-4 text-xs min-h-[310px] max-h-[70vh] overflow-y-auto">
          {error && (
            <div className="p-2.5 border border-[#ff3b2f] bg-[#190909] text-[#ff4d3d] text-[11px]">
              {error}
            </div>
          )}

          {/* STEP 1: WELCOME & DATABASE PATH */}
          {step === 0 && (
            <div className="space-y-4">
              <div className="space-y-1">
                <div className="text-sm font-bold uppercase tracking-wider text-[#F4ECE4]">
                  Database Location
                </div>
                <p className="text-[11px] text-[#c7b3a1] leading-relaxed">
                  {APP_PRODUCT_NAME} stores your flight plans, environment timelines, star coordinates, and
                  journal history in a local SQLite database.
                </p>
              </div>

              <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-1">
                <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-[#00D2FF]">
                  Filesize
                </div>
                <p className="text-[11px] text-[#a6907c] leading-relaxed">
                  Expected size is ~10–50 MB for typical use, or ~100–300+ MB with years of
                  historical logs and the POI catalog.
                </p>
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <label className="font-bold uppercase tracking-wider text-[#FF7100]">
                    Database Path
                  </label>
                  <span className="text-[10px] font-mono-tabular text-[#a6907c]">
                    SQLite (.db)
                  </span>
                </div>
                <div className="flex gap-1.5">
                  <input
                    type="text"
                    value={dbPath}
                    onChange={(event) => setDbPath(event.target.value)}
                    placeholder={localSettings?.defaultDbPath || `${APP_BINARY_NAME}.db`}
                    className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-[11px] text-[#F4ECE4] focus:border-[#FF7100] focus:outline-none"
                  />
                  <button
                    type="button"
                    onClick={handleBrowseDb}
                    disabled={isBusy}
                    className="ed-btn"
                  >
                    Browse…
                  </button>
                </div>
                {localSettings?.defaultDbPath && dbPath !== localSettings.defaultDbPath && (
                  <div className="flex justify-end">
                    <button
                      type="button"
                      onClick={handleResetDefaultDb}
                      className="text-[11px] text-[#FF7100] hover:underline cursor-pointer"
                    >
                      Reset Default Path
                    </button>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* STEP 2: PLAYER JOURNALS */}
          {step === 1 && (
            <div className="space-y-4">
              <div className="space-y-1">
                <div className="text-sm font-bold uppercase tracking-wider text-[#F4ECE4]">
                  Journal Folder
                </div>
                <p className="text-[11px] text-[#c7b3a1] leading-relaxed">
                  Select your Elite Dangerous journal directory to track flights, scans, and exobiology.
                </p>
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <label className="font-bold uppercase tracking-wider text-[#FF7100]">
                    Journal Path
                  </label>
                  <span
                    className={
                      localSettings?.journalDirExists &&
                      journalDir === (localSettings?.journalDir || localSettings?.defaultJournalDir)
                        ? 'ed-badge ed-badge-visited'
                        : 'ed-badge ed-badge-planned'
                    }
                  >
                    {localSettings?.journalDirExists &&
                    journalDir === (localSettings?.journalDir || localSettings?.defaultJournalDir)
                      ? 'DETECTED'
                      : 'CUSTOM'}
                  </span>
                </div>
                <div className="flex gap-1.5">
                  <input
                    type="text"
                    value={journalDir}
                    onChange={(event) => setJournalDir(event.target.value)}
                    placeholder={localSettings?.defaultJournalDir || ''}
                    className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-[11px] text-[#F4ECE4] focus:border-[#FF7100] focus:outline-none"
                  />
                  <button
                    type="button"
                    onClick={handleBrowseJournals}
                    disabled={isBusy}
                    className="ed-btn"
                  >
                    Browse…
                  </button>
                </div>
                {localSettings?.defaultJournalDir &&
                  journalDir !== localSettings.defaultJournalDir && (
                    <div className="flex justify-end">
                      <button
                        type="button"
                        onClick={handleResetDefaultJournals}
                        className="text-[11px] text-[#FF7100] hover:underline cursor-pointer"
                      >
                        Reset Default Path
                      </button>
                    </div>
                  )}
              </div>

              <div className="p-3 border border-[#3d2816] bg-[#110c08] text-[11px] text-[#a6907c] leading-relaxed">
                Next will scan and index your existing journal files.
              </div>
            </div>
          )}

          {/* STEP 3: HISTORICAL IMPORT (EDDISCOVERY) */}
          {step === 2 && (
            <div className="space-y-4">
              {/* Oldest Journal Summary Banner */}
              <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-1.5">
                <div className="flex items-center justify-between">
                  <span className="text-[10px] font-bold uppercase tracking-[0.14em] text-[#00D2FF]">
                    Local Journal Scan Summary
                  </span>
                  <span className="font-mono-tabular text-[11px] text-[#a6907c]">
                    {localIngest?.filesTracked ?? 0} files ·{' '}
                    {(localIngest?.eventsStored ?? 0).toLocaleString()} events
                  </span>
                </div>
                {firstEventIso ? (
                  <div className="space-y-0.5">
                    <div className="text-[11px] text-[#a6907c]">
                      Oldest journal entry found in folder:
                    </div>
                    <div className="font-mono-tabular text-xs font-bold text-[#F4ECE4]">
                      {firstEventUtcDisplay}
                      {firstEventGalacticDisplay && (
                        <span className="text-[#FF7100] ml-2">
                          ({firstEventGalacticDisplay})
                        </span>
                      )}
                    </div>
                  </div>
                ) : (
                  <div className="text-[11px] text-[#ffb000]">
                    No local <span className="font-mono-tabular">.log</span> events were found in the
                    selected journal folder yet.
                  </div>
                )}
              </div>

              {/* Prompt: Do you expect older journal entries? */}
              <div className="space-y-2">
                <div className="font-bold uppercase tracking-wider text-[#F4ECE4]">
                  Do you have older journal history in EDDiscovery?
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    onClick={() => setExpectOlderJournals(false)}
                    className={`p-2.5 text-left border cursor-pointer transition-colors ${
                      !expectOlderJournals
                        ? 'border-[#FF7100] bg-[#1f1309] text-[#F4ECE4]'
                        : 'border-[#3d2816] bg-[#110c08] text-[#a6907c] hover:border-[#6e4726]'
                    }`}
                  >
                    <div className="font-bold uppercase tracking-wider text-[11px]">
                      No, skip import
                    </div>
                  </button>
                  <button
                    type="button"
                    onClick={() => setExpectOlderJournals(true)}
                    className={`p-2.5 text-left border cursor-pointer transition-colors ${
                      expectOlderJournals
                        ? 'border-[#FF7100] bg-[#1f1309] text-[#F4ECE4]'
                        : 'border-[#3d2816] bg-[#110c08] text-[#a6907c] hover:border-[#6e4726]'
                    }`}
                  >
                    <div className="font-bold uppercase tracking-wider text-[11px]">
                      Yes, import from EDD
                    </div>
                  </button>
                </div>
              </div>

              {/* Explanation + EDDiscovery path when Yes is selected */}
              {expectOlderJournals ? (
                <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-3">
                  <p className="text-[11px] text-[#c7b3a1] leading-relaxed">
                    Import missing historical journal events from an existing{' '}
                    <span className="font-mono-tabular text-[#F4ECE4]">EDDUser.sqlite</span> database.
                  </p>

                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between">
                      <label className="text-[11px] font-bold uppercase tracking-wider text-[#FF7100]">
                        EDDiscovery Database Path
                      </label>
                      {localSettings?.edDiscoveryDbExists && (
                        <span className="ed-badge ed-badge-visited">FILE DETECTED</span>
                      )}
                    </div>
                    <div className="flex gap-1.5">
                      <input
                        type="text"
                        value={eddPath}
                        onChange={(event) => setEddPath(event.target.value)}
                        placeholder={localSettings?.defaultEdDiscoveryDbPath || 'EDDUser.sqlite'}
                        className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-[11px] text-[#F4ECE4] focus:border-[#FF7100] focus:outline-none"
                      />
                      <button
                        type="button"
                        onClick={handleBrowseEdd}
                        disabled={isBusy}
                        className="ed-btn"
                      >
                        Browse…
                      </button>
                      <button
                        type="button"
                        onClick={handleRunEddImport}
                        disabled={isBusy || !eddPath.trim()}
                        className="ed-btn-primary"
                      >
                        {eddBusy ? 'Importing…' : 'Import'}
                      </button>
                    </div>
                    {localSettings?.defaultEdDiscoveryDbPath &&
                      eddPath !== localSettings.defaultEdDiscoveryDbPath && (
                        <div className="flex justify-end">
                          <button
                            type="button"
                            onClick={handleResetDefaultEdd}
                            className="text-[10px] text-[#FF7100] hover:underline cursor-pointer"
                          >
                            Reset Default Path
                          </button>
                        </div>
                      )}
                  </div>

                  {eddProgress?.active && (
                    <div className="space-y-1 pt-1">
                      <div className="flex items-center justify-between text-[10px] font-mono-tabular text-[#00D2FF]">
                        <span>{eddProgress.stage}</span>
                        <span>
                          {eddProgress.percent}% ({eddProgress.current.toLocaleString()} /{' '}
                          {eddProgress.total.toLocaleString()})
                        </span>
                      </div>
                      <div className="w-full h-1.5 bg-[#050506] border border-[#3d2816] overflow-hidden">
                        <div
                          className="h-full bg-[#FF7100] transition-all duration-150"
                          style={{ width: `${eddProgress.percent}%` }}
                        />
                      </div>
                    </div>
                  )}

                  {eddImportedCount !== null && !eddProgress?.active && (
                    <div className="text-[11px] font-mono-tabular text-[#22e584]">
                      ✓ Imported {eddImportedCount.toLocaleString()} historical journal events from
                      EDDiscovery.
                    </div>
                  )}
                </div>
              ) : (
                <div className="p-3 border border-[#3d2816] bg-[#110c08] text-[11px] text-[#a6907c] leading-relaxed">
                  You can always import historical journals from EDDiscovery later under{' '}
                  <span className="text-[#F4ECE4] font-semibold">Config</span>.
                </div>
              )}
            </div>
          )}

          {/* STEP 4: COMMANDERS & ONLINE SYNC */}
          {step === 3 && (
            <div className="space-y-4">
              {/* Commander Selection (always shown even if 0 or 1 CMDR) */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <label className="font-bold uppercase tracking-wider text-[#FF7100]">
                    Commander Selection
                  </label>
                  <span className="font-mono-tabular text-[10px] text-[#a6907c]">
                    {localSettings?.commanders?.length ?? 0} DISCOVERED
                  </span>
                </div>
                <p className="text-[11px] text-[#a6907c] leading-relaxed">
                  Select which Commanders to include in tracking and telemetry:
                </p>

                {localSettings?.commanders && localSettings.commanders.length > 0 ? (
                  <div className="max-h-32 overflow-y-auto space-y-1.5 pr-1">
                    {localSettings.commanders.map((commander) => (
                      <label
                        key={commander.name}
                        className="flex items-center justify-between p-2.5 border border-[#3d2816] bg-[#110c08] cursor-pointer hover:border-[#6e4726]"
                      >
                        <div className="flex items-center gap-2.5">
                          <input
                            type="checkbox"
                            checked={commander.enabled}
                            onChange={(event) => handleToggleCmdr(commander.name, event.target.checked)}
                            className="accent-[#FF7100]"
                          />
                          <div>
                            <div className="font-bold text-[#F4ECE4] tracking-wider">
                              CMDR {commander.name}
                            </div>
                            <div className="text-[10px] font-mono-tabular text-[#a6907c]">
                              {commander.eventCount.toLocaleString()} journal events
                            </div>
                          </div>
                        </div>
                        <span
                          className={
                            commander.enabled ? 'ed-badge ed-badge-active' : 'ed-badge ed-badge-planned'
                          }
                        >
                          {commander.enabled ? 'TRACKING' : 'IGNORED'}
                        </span>
                      </label>
                    ))}
                  </div>
                ) : (
                  <div className="p-3 border border-[#3d2816] bg-[#110c08] text-[11px] text-[#a6907c] italic">
                    No Commanders found in journals yet. Any Commander detected in future logs will be
                    enabled automatically.
                  </div>
                )}
              </div>

              {/* EDSM / GEC Online Sync */}
              <div className="p-3 border border-[#3d2816] bg-[#110c08] space-y-3">
                <label className="flex items-start gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={onlineLookups}
                    onChange={(event) => handleToggleOnline(event.target.checked)}
                    className="mt-0.5 accent-[#FF7100]"
                  />
                  <div className="flex-1">
                    <div className="font-bold uppercase tracking-wider text-[#F4ECE4]">
                      EDSM &amp; EDAstro GEC Online Sync
                    </div>
                    <p className="text-[11px] text-[#a6907c] mt-0.5 leading-relaxed">
                      Resolves coordinates for unvisited waypoint systems and downloads the
                      galactic POI catalog.
                    </p>
                  </div>
                </label>
                <div className="flex items-center justify-between pt-2 border-t border-[#2b1d11]">
                  <div className="text-[11px] font-mono-tabular text-[#a6907c]">
                    POI Catalog:{' '}
                    <span className="text-[#F4ECE4] font-bold">
                      {(gecSyncedCount ?? localSettings?.gecCount ?? 0).toLocaleString()}
                    </span>{' '}
                    entries
                    {localSettings?.gecFetchedAt
                      ? ` (synced ${localSettings.gecFetchedAt.slice(0, 10)})`
                      : ' (syncs automatically on finish)'}
                  </div>
                  <button
                    type="button"
                    disabled={!onlineLookups || isBusy}
                    onClick={handleSyncGecNow}
                    className="ed-btn"
                  >
                    {gecBusy ? 'Syncing…' : 'Sync Catalog Now'}
                  </button>
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Bottom Navigation Bar with Back, Dots, and Next */}
        <div className="px-5 py-3 border-t border-[#3d2816] bg-[#140e09] flex items-center justify-between">
          <div className="w-28">
            {step > 0 && (
              <button
                type="button"
                onClick={handleBack}
                disabled={isBusy}
                className="ed-btn"
              >
                ← Back
              </button>
            )}
          </div>

          {/* Dots Navigation */}
          <div className="flex items-center gap-2.5" aria-label="Setup progress">
            {Array.from({ length: TOTAL_STEPS }).map((_, stepIndex) => {
              const isCurrent = stepIndex === step
              const isCompleted = stepIndex < step
              return (
                <button
                  key={stepIndex}
                  type="button"
                  disabled={isBusy || stepIndex > step}
                  onClick={() => {
                    if (stepIndex < step && !isBusy) {
                      setError('')
                      setStep(stepIndex)
                    }
                  }}
                  title={stepTitles[stepIndex]}
                  className={`transition-all rounded-full ${
                    isCurrent
                      ? 'w-2.5 h-2.5 bg-[#FF7100] shadow-[0_0_8px_#FF7100]'
                      : isCompleted
                        ? 'w-2 h-2 bg-[#FF7100]/60 hover:bg-[#FF7100] cursor-pointer'
                        : 'w-2 h-2 bg-[#3d2816] cursor-default'
                  }`}
                />
              )
            })}
          </div>

          <div className="w-28 flex justify-end">
            <button
              type="button"
              onClick={handleNext}
              disabled={isBusy}
              className="ed-btn-primary"
            >
              {busyMessage
                ? 'Working…'
                : step === TOTAL_STEPS - 1
                  ? 'Finish Setup'
                  : 'Next →'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

