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
        <div className="text-sm font-bold uppercase tracking-wider text-ed-text-main">
          Database Location
        </div>
        <p className="text-[11px] text-ed-text-body leading-relaxed">
          {APP_PRODUCT_NAME} stores your expeditions, flight mode timelines, star coordinates, and
          journal history in a local SQLite database.
        </p>
      </div>

      <div className="p-3 border border-ed-border bg-ed-card space-y-1">
        <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-cyan">
          Filesize
        </div>
        <p className="text-[11px] text-ed-text-muted leading-relaxed">
          Expected size is ~10–50 MB for typical use, or ~100–300+ MB with years of
          historical logs and the POI catalog.
        </p>
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <label className="font-bold uppercase tracking-wider text-ed-amber">
            Database Path
          </label>
          <span className="text-[10px] font-mono-tabular text-ed-text-muted">
            SQLite (.db)
          </span>
        </div>
        <div className="flex gap-1.5">
          <input
            type="text"
            value={dbPath}
            onChange={(event) => onChangeDbPath(event.target.value)}
            placeholder={defaultDbPath || `${APP_BINARY_NAME}.db`}
            className="flex-1 bg-ed-void border border-ed-border px-2.5 py-1.5 font-mono-tabular text-[11px] text-ed-text-main focus:border-ed-amber focus:outline-none"
          />
          <button
            type="button"
            onClick={onBrowseDb}
            disabled={isBusy}
            className="ed-btn"
          >
            BROWSE...
          </button>
        </div>
        {defaultDbPath && dbPath !== defaultDbPath && (
          <div className="flex justify-end">
            <button
              type="button"
              onClick={onResetDefaultDb}
              className="text-[11px] text-ed-amber hover:underline cursor-pointer"
            >
              Reset to Default
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

