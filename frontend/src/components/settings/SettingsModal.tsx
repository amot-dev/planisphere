import { useState } from 'react'
import { app } from '../../../wailsjs/go/models'
import { ModalShell } from '../common/ModalShell'
import { CommandersSection } from './CommandersSection'
import { DatabaseArchiveSection } from './DatabaseArchiveSection'
import { EDDiscoveryImportSection, type EddImportProgress } from './EDDiscoveryImportSection'
import { FleetCarrierSection } from './FleetCarrierSection'
import { FlightModesSection } from './FlightModesSection'
import { JournalDirectorySection } from './JournalDirectorySection'
import { OnlineServicesSection } from './OnlineServicesSection'

/**
 * SettingsModalProps defines the terminal settings, ingestion status, flight
 * modes, EDDiscovery progress, and lifecycle callbacks for Terminal Configuration.
 */
export interface SettingsModalProps {
  settings: app.Settings | null
  ingestStatus: app.IngestStatus | null
  modesState: app.ModesState | null
  eddProgress?: EddImportProgress | null
  onClose: () => void
  onRefresh: () => void
  onOpenSetupWizard?: () => void
}

/**
 * SettingsModal renders the Terminal Configuration dialog organizing journal
 * paths, archive telemetry, flight modes, commanders, EDDiscovery import, and online sync.
 */
export function SettingsModal({
  settings,
  ingestStatus,
  modesState,
  eddProgress,
  onClose,
  onRefresh,
  onOpenSetupWizard,
}: SettingsModalProps) {
  const [error, setError] = useState('')

  return (
    <ModalShell
      title="Terminal Configuration"
      onClose={onClose}
      maxWidth="max-w-2xl"
    >
      {error && (
        <div className="p-2.5 border border-[#ff3b2f] bg-[#190909] text-[#ff4d3d]">{error}</div>
      )}

      <JournalDirectorySection
        settings={settings}
        onRefresh={onRefresh}
        onSetError={setError}
      />

      <DatabaseArchiveSection
        settings={settings}
        ingestStatus={ingestStatus}
        onRefresh={onRefresh}
        onSetError={setError}
      />

      <FlightModesSection
        modesState={modesState}
        onRefresh={onRefresh}
        onSetError={setError}
      />

      <FleetCarrierSection
        settings={settings}
        onRefresh={onRefresh}
        onSetError={setError}
      />

      <CommandersSection
        settings={settings}
        onRefresh={onRefresh}
        onSetError={setError}
      />

      <EDDiscoveryImportSection
        settings={settings}
        eddProgress={eddProgress}
        onRefresh={onRefresh}
        onSetError={setError}
      />

      <OnlineServicesSection
        settings={settings}
        onRefresh={onRefresh}
        onSetError={setError}
      />

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

