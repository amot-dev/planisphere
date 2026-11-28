import { useState } from 'react'
import { app, type EddImportProgress } from '../../api/models'
import { ModalShell } from '../../components/ui/ModalShell'
import { CommandersSection } from './CommandersSection'
import { DatabaseArchiveSection } from './DatabaseArchiveSection'
import { DisplayPreferencesSection } from './DisplayPreferencesSection'
import { EDDiscoveryImportSection } from './EDDiscoveryImportSection'
import { ExternalLinksSection } from './ExternalLinksSection'
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
}

/**
 * SettingsModal renders the Terminal Configuration dialog organizing journal
 * paths, archive telemetry, flight modes, commanders, EDDiscovery import,
 * online sync, and external links.
 */
export function SettingsModal({
  settings,
  ingestStatus,
  modesState,
  eddProgress,
  onClose,
}: SettingsModalProps) {
  const [error, setError] = useState('')
  const [flightModesBusy, setFlightModesBusy] = useState(false)
  const isBusy =
    flightModesBusy || Boolean(eddProgress?.active) || Boolean(ingestStatus?.scanning)

  return (
    <ModalShell
      title="Terminal Configuration"
      onClose={onClose}
      closeDisabled={isBusy}
      maxWidth="max-w-2xl"
    >
      {error && (
        <div className="p-2.5 border border-ed-danger bg-ed-danger-tint text-ed-danger-text">{error}</div>
      )}

      <JournalDirectorySection
        settings={settings}
        onSetError={setError}
      />

      <DatabaseArchiveSection
        settings={settings}
        ingestStatus={ingestStatus}
        onSetError={setError}
      />

      <FlightModesSection
        modesState={modesState}
        onSetError={setError}
        onBusyChange={setFlightModesBusy}
      />

      <FleetCarrierSection
        settings={settings}
        onSetError={setError}
      />

      <CommandersSection
        settings={settings}
        onSetError={setError}
      />

      <EDDiscoveryImportSection
        settings={settings}
        eddProgress={eddProgress}
        onSetError={setError}
      />

      <OnlineServicesSection
        settings={settings}
        onSetError={setError}
      />

      <DisplayPreferencesSection
        settings={settings}
        onSetError={setError}
      />

      <ExternalLinksSection
        settings={settings}
        onSetError={setError}
      />
    </ModalShell>
  )
}

