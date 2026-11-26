import { useRef } from 'react'
import type { expedition } from '../../../api/models'

/**
 * ParsedWaypointImport is the plain-object-compatible subset of
 * expedition.ParsedWaypointImport rendered by the bulk-import preview.
 */
export type ParsedWaypointImport = Pick<
  expedition.ParsedWaypointImport,
  'format' | 'systems' | 'collapsedDuplicateCount' | 'skippedLineCount'
>

/**
 * WaypointImportSectionProps defines the editable import text, parsed summary,
 * file-load status, and action handlers for bulk-importing waypoints during
 * expedition creation.
 */
export interface WaypointImportSectionProps {
  rawImportText: string
  parsedImport: ParsedWaypointImport
  loadedFileNote: string
  loadingFile: boolean
  parsing?: boolean
  disabled: boolean
  onChangeText: (nextText: string, options?: { immediate?: boolean }) => void
  onLoadFile: () => void
  onClear: () => void
}

/**
 * WaypointImportSection renders the collapsible bulk-import text field, native
 * .txt/.csv loader, and live parsed waypoint summary inside the Create New
 * Expedition panel.
 */
export function WaypointImportSection({
  rawImportText,
  parsedImport,
  loadedFileNote,
  loadingFile,
  parsing = false,
  disabled,
  onChangeText,
  onLoadFile,
  onClear,
}: WaypointImportSectionProps) {
  const pastePendingRef = useRef(false)
  const hasRawText = rawImportText.trim().length > 0
  const systemCount = parsedImport.systems.length
  const firstSystem = systemCount > 0 ? parsedImport.systems[0] : ''
  const lastSystem = systemCount > 1 ? parsedImport.systems[systemCount - 1] : ''
  const showSummary = systemCount > 0
  const showInvalidWarning = !showSummary && !parsing && (hasRawText || Boolean(loadedFileNote))

  return (
    <div className="pt-2 border-t border-ed-border-subtle space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-[11px] text-ed-text-muted">
          Paste one star system per line, or load a file (such as a Spansh CSV) to populate the
          list.
        </span>
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            disabled={disabled || loadingFile}
            onClick={onLoadFile}
            className="ed-btn-cyan"
          >
            {loadingFile ? 'LOADING...' : 'LOAD TXT / CSV'}
          </button>
          {hasRawText && (
            <button
              type="button"
              disabled={disabled || loadingFile}
              onClick={onClear}
              className="ed-btn"
            >
              CLEAR
            </button>
          )}
        </div>
      </div>

      <textarea
        rows={6}
        spellCheck={false}
        disabled={disabled}
        value={rawImportText}
        onKeyDown={() => {
          pastePendingRef.current = false
        }}
        onPaste={() => {
          pastePendingRef.current = true
        }}
        onChange={(event) => {
          const immediate = pastePendingRef.current
          pastePendingRef.current = false
          onChangeText(event.target.value, immediate ? { immediate: true } : undefined)
        }}
        placeholder={'Shinrarta Dezhra\nOmega Sector VE-Q b5-15\nSkaude AA-A h294\nColonia'}
        className="w-full bg-ed-void border border-ed-border px-2.5 py-2 font-mono-tabular text-xs text-ed-text-main placeholder:text-ed-text-dim focus:border-ed-amber focus:outline-none resize-y"
      />

      {(showSummary || showInvalidWarning) && (
        <div className="space-y-1 text-[11px]">
          {showSummary ? (
            <>
              <div className="flex flex-wrap items-center gap-2 font-mono-tabular">
                <span className="font-bold text-ed-amber">
                  {systemCount} {systemCount === 1 ? 'WAYPOINT' : 'WAYPOINTS'} READY
                </span>
                {parsedImport.collapsedDuplicateCount > 0 && (
                  <span className="text-ed-text-muted">
                    ({parsedImport.collapsedDuplicateCount} consecutive{' '}
                    {parsedImport.collapsedDuplicateCount === 1 ? 'duplicate' : 'duplicates'}{' '}
                    collapsed)
                  </span>
                )}
                {loadedFileNote && <span className="text-ed-text-muted">{loadedFileNote}</span>}
              </div>
              <div className="font-mono-tabular text-[10px] text-ed-text-muted truncate">
                {systemCount === 1
                  ? `#01 ${firstSystem}`
                  : `#01 ${firstSystem} -> ${systemCount > 2 ? '... -> ' : ''}#${String(systemCount).padStart(2, '0')} ${lastSystem}`}
              </div>
            </>
          ) : (
            <div className="text-ed-danger-text">
              No valid star systems detected in the import text. Clear or edit the list before
              creating the expedition.
            </div>
          )}
        </div>
      )}
    </div>
  )
}
