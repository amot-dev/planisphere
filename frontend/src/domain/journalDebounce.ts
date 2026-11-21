export const MIN_JOURNAL_DEBOUNCE_MILLISECONDS = 300
export const DEFAULT_JOURNAL_DEBOUNCE_MILLISECONDS = 2000
export const MAX_JOURNAL_DEBOUNCE_MILLISECONDS = 30000
export const DEBOUNCE_STEP_MILLISECONDS = 100

/**
 * JOURNAL_DEBOUNCE_PRESETS defines quick-select duration buttons for the live
 * journal file watcher debounce control.
 */
export const JOURNAL_DEBOUNCE_PRESETS: ReadonlyArray<{ label: string; milliseconds: number }> = [
  { label: '0.3s', milliseconds: 300 },
  { label: '2s', milliseconds: 2000 },
  { label: '5s', milliseconds: 5000 },
  { label: '15s', milliseconds: 15000 },
  { label: '30s', milliseconds: 30000 },
]

/**
 * clampDebounceMilliseconds clamps a live journal watcher debounce duration in
 * milliseconds into [300, 30000].
 */
export function clampDebounceMilliseconds(milliseconds: number): number {
  if (!Number.isFinite(milliseconds)) {
    return DEFAULT_JOURNAL_DEBOUNCE_MILLISECONDS
  }
  const roundedMilliseconds = Math.round(milliseconds)
  if (roundedMilliseconds < MIN_JOURNAL_DEBOUNCE_MILLISECONDS) {
    return MIN_JOURNAL_DEBOUNCE_MILLISECONDS
  }
  if (roundedMilliseconds > MAX_JOURNAL_DEBOUNCE_MILLISECONDS) {
    return MAX_JOURNAL_DEBOUNCE_MILLISECONDS
  }
  return roundedMilliseconds
}

/**
 * formatDebounceSeconds formats a millisecond debounce duration as tabular seconds
 * (for example, "2.0s").
 */
export function formatDebounceSeconds(milliseconds: number): string {
  return `${(clampDebounceMilliseconds(milliseconds) / 1000).toFixed(1)}s`
}

