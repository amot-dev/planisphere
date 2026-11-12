import { app } from '../../../wailsjs/go/models'
import type { EddImportProgress } from '../settings/EDDiscoveryImportSection'

/**
 * HistoricalImportStepProps defines the scanned journal summary, EDDiscovery
 * database path state, live import progress, and callbacks for step three of onboarding.
 */
export interface HistoricalImportStepProps {
  localSettings: app.Settings | null
  localIngest: app.IngestStatus | null
  expectOlderJournals: boolean
  eddPath: string
  eddBusy: boolean
  eddImportedCount: number | null
  eddProgress?: EddImportProgress | null
  isBusy: boolean
  onChangeExpectOlderJournals: (expectOlder: boolean) => void
  onChangeEddPath: (nextEddPath: string) => void
  onBrowseEdd: () => void
  onResetDefaultEdd: () => void
  onRunEddImport: () => void
}

/**
 * HistoricalImportStep renders the local journal scan summary and optional
 * EDDiscovery historical archive import workflow in the setup wizard.
 */
export function HistoricalImportStep({
  localSettings,
  localIngest,
  expectOlderJournals,
  eddPath,
  eddBusy,
  eddImportedCount,
  eddProgress,
  isBusy,
  onChangeExpectOlderJournals,
  onChangeEddPath,
  onBrowseEdd,
  onResetDefaultEdd,
  onRunEddImport,
}: HistoricalImportStepProps) {
  // Format oldest journal date for Step 3
  const firstEventIso = localIngest?.firstEventAt || ''
  const firstEventUtcDisplay = firstEventIso
    ? firstEventIso.replace('T', ' ').replace('Z', ' UTC')
    : ''
  const firstEventGalacticDisplay = localIngest?.firstEventGalactic || ''

  return (
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
            onClick={() => onChangeExpectOlderJournals(false)}
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
            onClick={() => onChangeExpectOlderJournals(true)}
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

      {/* Explanation and EDDiscovery path when Yes is selected */}
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
                onChange={(event) => onChangeEddPath(event.target.value)}
                placeholder={localSettings?.defaultEdDiscoveryDbPath || 'EDDUser.sqlite'}
                className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-[11px] text-[#F4ECE4] focus:border-[#FF7100] focus:outline-none"
              />
              <button
                type="button"
                onClick={onBrowseEdd}
                disabled={isBusy}
                className="ed-btn"
              >
                Browse…
              </button>
              <button
                type="button"
                onClick={onRunEddImport}
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
                    onClick={onResetDefaultEdd}
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
              Imported {eddImportedCount.toLocaleString()} historical journal events from
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
  )
}

