import { useState } from 'react'
import { app, type EddImportProgress } from '../../api/models'
import { ModalShell } from '../../components/ui/ModalShell'
import { APP_PRODUCT_NAME, APP_VERSION } from '../../version'
import { CommandersOnlineStep } from './CommandersOnlineStep'
import { DatabaseLocationStep } from './DatabaseLocationStep'
import { HistoricalImportStep } from './HistoricalImportStep'
import { JournalFolderStep } from './JournalFolderStep'
import { useSetupWizard } from './useSetupWizard'

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
  'Step 2 of 4 — Journal Directory',
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
  const [eddBusy, setEddBusy] = useState<boolean>(false)
  const wizard = useSetupWizard(initialSettings, initialIngest, { eddBusy, setEddBusy }, onComplete)

  return (
    <ModalShell
      title={`${APP_PRODUCT_NAME} Setup`}
      badge={`v${APP_VERSION}`}
      subtitle={STEP_TITLES[wizard.step]}
      maxWidth="max-w-lg"
      onClose={onClose}
      closeDisabled={wizard.isBusy}
      closeTitle="Close setup wizard"
      headerLeading={<div className="w-2 h-4 bg-[#FF7100]" />}
      bodyClassName="p-5 space-y-4 text-xs min-h-[310px] max-h-[70vh] overflow-y-auto"
      footer={
        <SetupWizardFooter
          step={wizard.step}
          isBusy={wizard.isBusy}
          busyMessage={wizard.busyMessage}
          onBack={wizard.handleBack}
          onSelectCompletedStep={wizard.handleSelectCompletedStep}
          onNext={wizard.handleNext}
        />
      }
    >
      {wizard.error && (
        <div className="p-2.5 border border-[#ff3b2f] bg-[#190909] text-[#ff4d3d] text-[11px]">
          {wizard.error}
        </div>
      )}

      {wizard.step === 0 && (
        <DatabaseLocationStep
          dbPath={wizard.dbPath}
          defaultDbPath={wizard.localSettings?.defaultDbPath}
          isBusy={wizard.isBusy}
          onChangeDbPath={wizard.handleChangeDbPath}
          onBrowseDb={wizard.handleBrowseDb}
          onResetDefaultDb={wizard.handleResetDefaultDb}
        />
      )}

      {wizard.step === 1 && (
        <JournalFolderStep
          journalDir={wizard.journalDir}
          localSettings={wizard.localSettings}
          isBusy={wizard.isBusy}
          onChangeJournalDir={wizard.handleChangeJournalDir}
          onBrowseJournals={wizard.handleBrowseJournals}
          onResetDefaultJournals={wizard.handleResetDefaultJournals}
        />
      )}

      {wizard.step === 2 && (
        <HistoricalImportStep
          localSettings={wizard.localSettings}
          localIngest={wizard.localIngest}
          expectOlderJournals={wizard.expectOlderJournals}
          eddPath={wizard.eddPath}
          eddBusy={eddBusy}
          eddImportedCount={wizard.eddImportedCount}
          eddProgress={eddProgress}
          isBusy={wizard.isBusy}
          onChangeExpectOlderJournals={wizard.setExpectOlderJournals}
          onChangeEddPath={wizard.handleChangeEddPath}
          onBrowseEdd={wizard.handleBrowseEdd}
          onResetDefaultEdd={wizard.handleResetDefaultEdd}
          onRunEddImport={wizard.handleRunEddImport}
        />
      )}

      {wizard.step === 3 && (
        <CommandersOnlineStep
          localSettings={wizard.localSettings}
          onlineLookups={wizard.onlineLookups}
          gecBusy={wizard.gecBusy}
          gecSyncedCount={wizard.gecSyncedCount}
          isBusy={wizard.isBusy}
          onToggleCommander={wizard.handleToggleCommander}
          onToggleOnline={wizard.handleToggleOnline}
          onSyncGecNow={wizard.handleSyncGecNow}
        />
      )}
    </ModalShell>
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
            ← BACK
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
            ? 'WORKING...'
            : step === TOTAL_STEPS - 1
              ? 'FINISH SETUP'
              : 'NEXT →'}
        </button>
      </div>
    </div>
  )
}

