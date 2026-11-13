import { useEffect, useRef, useState } from 'react'
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
} from '../../../wailsjs/go/app/App'
import { app } from '../../../wailsjs/go/models'
import { APP_PRODUCT_NAME, APP_VERSION } from '../../version'
import type { EddImportProgress } from '../settings/EDDiscoveryImportSection'
import { CommandersOnlineStep } from './CommandersOnlineStep'
import { DatabaseLocationStep } from './DatabaseLocationStep'
import { HistoricalImportStep } from './HistoricalImportStep'
import { JournalFolderStep } from './JournalFolderStep'

/**
 * SetupWizardProps defines the initial configuration, ingestion status, live
 * EDDiscovery progress, and completion callbacks for the four-step setup wizard.
 */
export interface SetupWizardProps {
  settings: app.Settings | null
  ingestStatus: app.IngestStatus | null
  eddProgress?: EddImportProgress | null
  onComplete: () => void
  onClose?: () => void
}

const TOTAL_STEPS = 4

const STEP_TITLES = [
  'Step 1 of 4 — Database Location',
  'Step 2 of 4 — Journal Folder',
  'Step 3 of 4 — Historical Import',
  'Step 4 of 4 — Commanders and Online Sync',
]

/**
 * SetupWizardModal orchestrates the four-step onboarding flow for configuring
 * database storage, player journal folder indexing, EDDiscovery import, and online sync.
 */
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

  const userModifiedDbPathRef = useRef(false)
  const [dbPath, setDbPath] = useState<string>(
    initialSettings?.dbPath || initialSettings?.defaultDbPath || '',
  )
  const userModifiedJournalDirRef = useRef(false)
  const [journalDir, setJournalDir] = useState<string>(
    initialSettings?.journalDir || initialSettings?.defaultJournalDir || '',
  )

  const [expectOlderJournals, setExpectOlderJournals] = useState<boolean>(false)
  const userModifiedEddPathRef = useRef(false)
  const [eddPath, setEddPath] = useState<string>(
    initialSettings?.edDiscoveryDbPath || initialSettings?.defaultEdDiscoveryDbPath || '',
  )
  const [eddImportedCount, setEddImportedCount] = useState<number | null>(null)
  const [eddBusy, setEddBusy] = useState<boolean>(false)

  const userModifiedOnlineLookupsRef = useRef(false)
  const [onlineLookups, setOnlineLookups] = useState<boolean>(
    initialSettings?.onlineLookups ?? true,
  )
  const [gecBusy, setGecBusy] = useState<boolean>(false)
  const [gecSyncedCount, setGecSyncedCount] = useState<number | null>(null)

  const [busyMessage, setBusyMessage] = useState<string | null>(null)
  const [error, setError] = useState<string>('')

  useEffect(() => {
    if (initialSettings) {
      setLocalSettings(initialSettings)
      if (!userModifiedDbPathRef.current) {
        setDbPath(initialSettings.dbPath || initialSettings.defaultDbPath || '')
      }
      if (!userModifiedJournalDirRef.current) {
        setJournalDir(initialSettings.journalDir || initialSettings.defaultJournalDir || '')
      }
      if (!userModifiedEddPathRef.current) {
        setEddPath(
          initialSettings.edDiscoveryDbPath || initialSettings.defaultEdDiscoveryDbPath || '',
        )
      }
      if (!userModifiedOnlineLookupsRef.current) {
        setOnlineLookups(initialSettings.onlineLookups)
      }
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

  async function handleBrowseDb() {
    setError('')
    try {
      const picked = await BrowseDBPath()
      if (picked) {
        userModifiedDbPathRef.current = true
        setDbPath(picked)
      }
    } catch (browseError) {
      setError(String(browseError))
    }
  }

  function handleResetDefaultDb() {
    setError('')
    userModifiedDbPathRef.current = true
    setDbPath(localSettings?.defaultDbPath || '')
  }

  async function handleBrowseJournals() {
    setError('')
    try {
      const picked = await BrowseJournalDir()
      if (picked) {
        userModifiedJournalDirRef.current = true
        setJournalDir(picked)
      }
    } catch (browseError) {
      setError(String(browseError))
    }
  }

  function handleResetDefaultJournals() {
    setError('')
    userModifiedJournalDirRef.current = true
    setJournalDir(localSettings?.defaultJournalDir || '')
  }

  async function handleBrowseEdd() {
    setError('')
    try {
      const picked = await BrowseEDDiscoveryDB()
      if (picked) {
        userModifiedEddPathRef.current = true
        setEddPath(picked)
      }
    } catch (browseError) {
      setError(String(browseError))
    }
  }

  function handleResetDefaultEdd() {
    setError('')
    userModifiedEddPathRef.current = true
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
    } catch (importError) {
      setError(String(importError))
    } finally {
      setEddBusy(false)
    }
  }

  async function handleToggleCommander(name: string, enabled: boolean) {
    setError('')
    try {
      const next = await SetCommanderEnabled(name, enabled)
      setLocalSettings(next)
    } catch (toggleError) {
      setError(String(toggleError))
    }
  }

  async function handleToggleOnline(enabled: boolean) {
    userModifiedOnlineLookupsRef.current = true
    setError('')
    setOnlineLookups(enabled)
    try {
      const next = await SetOnlineLookups(enabled)
      setLocalSettings(next)
    } catch (toggleError) {
      setError(String(toggleError))
    }
  }

  async function handleSyncGecNow() {
    setError('')
    setGecBusy(true)
    try {
      const count = await RefreshGEC()
      setGecSyncedCount(count)
      await refreshWizardState()
    } catch (syncError) {
      setError(String(syncError))
    } finally {
      setGecBusy(false)
    }
  }

  async function handleNext() {
    setError('')
    if (step === 0) {
      setBusyMessage('INITIALIZING DATABASE…')
      try {
        const nextSettings = await SetDatabasePath(dbPath)
        const nextIngestStatus = await GetIngestStatus()
        setLocalSettings(nextSettings)
        setLocalIngest(nextIngestStatus)
        setDbPath(nextSettings.dbPath)
        if (!userModifiedJournalDirRef.current) {
          setJournalDir(nextSettings.journalDir || nextSettings.defaultJournalDir || '')
        }
        if (!userModifiedEddPathRef.current) {
          setEddPath(
            nextSettings.edDiscoveryDbPath || nextSettings.defaultEdDiscoveryDbPath || '',
          )
        }
        if (!userModifiedOnlineLookupsRef.current) {
          setOnlineLookups(nextSettings.onlineLookups)
        }
        setStep(1)
      } catch (stepError) {
        setError(String(stepError))
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
      } catch (stepError) {
        setError(String(stepError))
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
      } catch (stepError) {
        setError(String(stepError))
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

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-[2px] p-4 select-none">
      <div className="w-full max-w-lg border border-[#472e19] border-t-2 border-t-[#FF7100] bg-[#0b0907] shadow-[0_20px_60px_rgba(0,0,0,0.9)] flex flex-col overflow-hidden">
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
              <p className="text-[11px] text-[#a6907c] mt-0.5">{STEP_TITLES[step]}</p>
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

        <div className="p-5 space-y-4 text-xs min-h-[310px] max-h-[70vh] overflow-y-auto">
          {error && (
            <div className="p-2.5 border border-[#ff3b2f] bg-[#190909] text-[#ff4d3d] text-[11px]">
              {error}
            </div>
          )}

          {step === 0 && (
            <DatabaseLocationStep
              dbPath={dbPath}
              defaultDbPath={localSettings?.defaultDbPath}
              isBusy={isBusy}
              onChangeDbPath={(nextPath) => {
                userModifiedDbPathRef.current = true
                setDbPath(nextPath)
              }}
              onBrowseDb={handleBrowseDb}
              onResetDefaultDb={handleResetDefaultDb}
            />
          )}

          {step === 1 && (
            <JournalFolderStep
              journalDir={journalDir}
              localSettings={localSettings}
              isBusy={isBusy}
              onChangeJournalDir={(nextDir) => {
                userModifiedJournalDirRef.current = true
                setJournalDir(nextDir)
              }}
              onBrowseJournals={handleBrowseJournals}
              onResetDefaultJournals={handleResetDefaultJournals}
            />
          )}

          {step === 2 && (
            <HistoricalImportStep
              localSettings={localSettings}
              localIngest={localIngest}
              expectOlderJournals={expectOlderJournals}
              eddPath={eddPath}
              eddBusy={eddBusy}
              eddImportedCount={eddImportedCount}
              eddProgress={eddProgress}
              isBusy={isBusy}
              onChangeExpectOlderJournals={setExpectOlderJournals}
              onChangeEddPath={(nextPath) => {
                userModifiedEddPathRef.current = true
                setEddPath(nextPath)
              }}
              onBrowseEdd={handleBrowseEdd}
              onResetDefaultEdd={handleResetDefaultEdd}
              onRunEddImport={handleRunEddImport}
            />
          )}

          {step === 3 && (
            <CommandersOnlineStep
              localSettings={localSettings}
              onlineLookups={onlineLookups}
              gecBusy={gecBusy}
              gecSyncedCount={gecSyncedCount}
              isBusy={isBusy}
              onToggleCommander={handleToggleCommander}
              onToggleOnline={handleToggleOnline}
              onSyncGecNow={handleSyncGecNow}
            />
          )}
        </div>

        <SetupWizardFooter
          step={step}
          isBusy={isBusy}
          busyMessage={busyMessage}
          onBack={handleBack}
          onSelectCompletedStep={(targetStep) => {
            setError('')
            setStep(targetStep)
          }}
          onNext={handleNext}
        />
      </div>
    </div>
  )
}

function SetupWizardFooter({
  step,
  isBusy,
  busyMessage,
  onBack,
  onSelectCompletedStep,
  onNext,
}: {
  step: number
  isBusy: boolean
  busyMessage: string | null
  onBack: () => void
  onSelectCompletedStep: (targetStep: number) => void
  onNext: () => void
}) {
  return (
    <div className="px-5 py-3 border-t border-[#3d2816] bg-[#140e09] flex items-center justify-between">
      <div className="w-28">
        {step > 0 && (
          <button
            type="button"
            onClick={onBack}
            disabled={isBusy}
            className="ed-btn"
          >
            ← Back
          </button>
        )}
      </div>

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
                  onSelectCompletedStep(stepIndex)
                }
              }}
              title={STEP_TITLES[stepIndex]}
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
          onClick={onNext}
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
  )
}

