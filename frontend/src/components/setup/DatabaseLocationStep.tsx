import { APP_BINARY_NAME, APP_PRODUCT_NAME } from '../../version'

/**
 * DatabaseLocationStepProps defines the SQLite database path input state and
 * browse/reset callbacks for the first step of the onboarding wizard.
 */
export interface DatabaseLocationStepProps {
  dbPath: string
  defaultDbPath?: string
  isBusy: boolean
  onChangeDbPath: (nextDbPath: string) => void
  onBrowseDb: () => void
  onResetDefaultDb: () => void
}

/**
 * DatabaseLocationStep renders the SQLite database file path selector and
 * expected storage footprint guidance in the setup wizard.
 */
export function DatabaseLocationStep({
  dbPath,
  defaultDbPath,
  isBusy,
  onChangeDbPath,
  onBrowseDb,
  onResetDefaultDb,
}: DatabaseLocationStepProps) {
  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <div className="text-sm font-bold uppercase tracking-wider text-[#F4ECE4]">
          Database Location
        </div>
        <p className="text-[11px] text-[#c7b3a1] leading-relaxed">
          {APP_PRODUCT_NAME} stores your flight plans, flight mode timelines, star coordinates, and
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
            onChange={(event) => onChangeDbPath(event.target.value)}
            placeholder={defaultDbPath || `${APP_BINARY_NAME}.db`}
            className="flex-1 bg-[#050506] border border-[#3d2816] px-2.5 py-1.5 font-mono-tabular text-[11px] text-[#F4ECE4] focus:border-[#FF7100] focus:outline-none"
          />
          <button
            type="button"
            onClick={onBrowseDb}
            disabled={isBusy}
            className="ed-btn"
          >
            Browse…
          </button>
        </div>
        {defaultDbPath && dbPath !== defaultDbPath && (
          <div className="flex justify-end">
            <button
              type="button"
              onClick={onResetDefaultDb}
              className="text-[11px] text-[#FF7100] hover:underline cursor-pointer"
            >
              Reset Default Path
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

