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
} from '../../api/backend'
import { app } from '../../api/models'

/**
 * useSetupWizard encapsulates multi-step onboarding state, path drafts,
 * backend RPC persistence, and step transitions for the four-step setup wizard.
 */
export function useSetupWizard(
  initialSettings: app.Settings | null,
  initialIngest: app.IngestStatus | null,
  eddBusyState: { eddBusy: boolean; setEddBusy: (busy: boolean) => void },
  onComplete: () => void,
) {
  const { eddBusy, setEddBusy } = eddBusyState
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

  const userModifiedOnlineLookupsRef = useRef(false)
  const [onlineLookups, setOnlineLookups] = useState<boolean>(
    initialSettings?.onlineLookups ?? true,
  )
  const [gecBusy, setGecBusy] = useState<boolean>(false)
  const [gecSyncedCount, setGecSyncedCount] = useState<number | null>(null)

  const [busyMessage, setBusyMessage] = useState<string | null>(null)
  const [error, setError] = useState<string>('')

  useEffect(() => {
    if (!initialSettings) return
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

  async function runWizardAction(
    action: () => Promise<void>,
    options?: { busyText?: string; setBusyFlag?: (busy: boolean) => void },
  ) {
    setError('')
    if (options?.busyText) {
      setBusyMessage(options.busyText)
    }
    options?.setBusyFlag?.(true)
    try {
      await action()
    } catch (actionError) {
      setError(String(actionError))
    } finally {
      if (options?.busyText) {
        setBusyMessage(null)
      }
      options?.setBusyFlag?.(false)
    }
  }

  function browseWizardPath(
    browse: () => Promise<string>,
    modifiedRef: { current: boolean },
    setPath: (nextPath: string) => void,
  ) {
    return runWizardAction(async () => {
      const picked = await browse()
      if (picked) {
        modifiedRef.current = true
        setPath(picked)
      }
    })
  }

  function handleChangeDbPath(nextPath: string) {
    userModifiedDbPathRef.current = true
    setDbPath(nextPath)
  }

  function handleChangeJournalDir(nextDir: string) {
    userModifiedJournalDirRef.current = true
    setJournalDir(nextDir)
  }

  function handleChangeEddPath(nextPath: string) {
    userModifiedEddPathRef.current = true
    setEddPath(nextPath)
  }

  function handleResetDefaultDb() {
    setError('')
    userModifiedDbPathRef.current = true
    setDbPath(localSettings?.defaultDbPath || '')
  }

  function handleResetDefaultJournals() {
    setError('')
    userModifiedJournalDirRef.current = true
    setJournalDir(localSettings?.defaultJournalDir || '')
  }

  function handleResetDefaultEdd() {
    setError('')
    userModifiedEddPathRef.current = true
    setEddPath(
      localSettings?.edDiscoveryDbPath || localSettings?.defaultEdDiscoveryDbPath || '',
    )
  }

  function handleRunEddImport() {
    return runWizardAction(
      async () => {
        const count = await ImportEDDiscoveryHistory(eddPath)
        setEddImportedCount(count)
        await refreshWizardState()
      },
      { setBusyFlag: setEddBusy },
    )
  }

  function handleToggleCommander(name: string, enabled: boolean) {
    return runWizardAction(async () => {
      const next = await SetCommanderEnabled(name, enabled)
      setLocalSettings(next)
    })
  }

  function handleToggleOnline(enabled: boolean) {
    userModifiedOnlineLookupsRef.current = true
    setOnlineLookups(enabled)
    return runWizardAction(async () => {
      const next = await SetOnlineLookups(enabled)
      setLocalSettings(next)
    })
  }

  function handleSyncGecNow() {
    return runWizardAction(
      async () => {
        const count = await RefreshGEC()
        setGecSyncedCount(count)
        await refreshWizardState()
      },
      { setBusyFlag: setGecBusy },
    )
  }

  function handleNext() {
    if (step === 0) {
      return runWizardAction(
        async () => {
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
        },
        { busyText: 'INITIALIZING DATABASE...' },
      )
    }

    if (step === 1) {
      return runWizardAction(
        async () => {
          const currentIngestStatus = await SetupConfigureJournals(journalDir)
          setLocalIngest(currentIngestStatus)
          const currentSettings = await GetSettings()
          setLocalSettings(currentSettings)
          setStep(2)
        },
        { busyText: 'SCANNING PLAYER JOURNALS...' },
      )
    }

    if (step === 2) {
      setError('')
      setStep(3)
      return
    }

    if (step === 3) {
      return runWizardAction(
        async () => {
          await CompleteSetup()
          onComplete()
        },
        { busyText: 'FINALIZING SETUP...' },
      )
    }
  }

  function handleBack() {
    if (step > 0 && !busyMessage && !eddBusy) {
      setError('')
      setStep(step - 1)
    }
  }

  function handleSelectCompletedStep(targetStep: number) {
    setError('')
    setStep(targetStep)
  }

  return {
    step,
    localSettings,
    localIngest,
    dbPath,
    journalDir,
    expectOlderJournals,
    setExpectOlderJournals,
    eddPath,
    eddImportedCount,
    onlineLookups,
    gecBusy,
    gecSyncedCount,
    busyMessage,
    error,
    isBusy: Boolean(busyMessage) || eddBusy || gecBusy,
    handleChangeDbPath,
    handleBrowseDb: () => browseWizardPath(BrowseDBPath, userModifiedDbPathRef, setDbPath),
    handleResetDefaultDb,
    handleChangeJournalDir,
    handleBrowseJournals: () =>
      browseWizardPath(BrowseJournalDir, userModifiedJournalDirRef, setJournalDir),
    handleResetDefaultJournals,
    handleChangeEddPath,
    handleBrowseEdd: () =>
      browseWizardPath(BrowseEDDiscoveryDB, userModifiedEddPathRef, setEddPath),
    handleResetDefaultEdd,
    handleRunEddImport,
    handleToggleCommander,
    handleToggleOnline,
    handleSyncGecNow,
    handleNext,
    handleBack,
    handleSelectCompletedStep,
  }
}

