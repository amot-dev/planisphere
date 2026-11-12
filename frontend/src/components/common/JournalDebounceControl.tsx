import { useEffect, useRef, useState } from 'react'

export const MIN_JOURNAL_DEBOUNCE_MILLISECONDS = 300
export const DEFAULT_JOURNAL_DEBOUNCE_MILLISECONDS = 2000
export const MAX_JOURNAL_DEBOUNCE_MILLISECONDS = 30000
const DEBOUNCE_STEP_MILLISECONDS = 100

const DEBOUNCE_PRESETS: ReadonlyArray<{ label: string; milliseconds: number }> = [
  { label: '0.3s', milliseconds: 300 },
  { label: '2s', milliseconds: 2000 },
  { label: '5s', milliseconds: 5000 },
  { label: '15s', milliseconds: 15000 },
  { label: '30s', milliseconds: 30000 },
]

/** Clamps a live journal watcher debounce duration in milliseconds to [300, 30000]. */
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

/** Formats a millisecond debounce duration as tabular seconds (for example, "2.0s"). */
export function formatDebounceSeconds(milliseconds: number): string {
  return `${(clampDebounceMilliseconds(milliseconds) / 1000).toFixed(1)}s`
}

/** Props for configuring the live journal file-watcher debounce window. */
export interface JournalDebounceControlProps {
  configuredDebounceMilliseconds: number
  defaultDebounceMilliseconds: number
  onCommitDebounceMilliseconds: (milliseconds: number) => Promise<void> | void
}

/**
 * Interactive slider and preset selector for the live journal watcher debounce window.
 * Slider drags update local state without IPC traffic and commit only on release or preset selection.
 */
export function JournalDebounceControl({
  configuredDebounceMilliseconds,
  defaultDebounceMilliseconds,
  onCommitDebounceMilliseconds,
}: JournalDebounceControlProps) {
  const normalizedConfiguredMilliseconds = clampDebounceMilliseconds(configuredDebounceMilliseconds)
  const normalizedDefaultMilliseconds = clampDebounceMilliseconds(defaultDebounceMilliseconds)
  const [draftDebounceMilliseconds, setDraftDebounceMilliseconds] = useState(
    normalizedConfiguredMilliseconds,
  )
  const lastCommittedMillisecondsRef = useRef(normalizedConfiguredMilliseconds)

  useEffect(() => {
    setDraftDebounceMilliseconds(normalizedConfiguredMilliseconds)
    lastCommittedMillisecondsRef.current = normalizedConfiguredMilliseconds
  }, [normalizedConfiguredMilliseconds])

  function applyAndCommitMilliseconds(targetMilliseconds: number) {
    const clampedTargetMilliseconds = clampDebounceMilliseconds(targetMilliseconds)
    setDraftDebounceMilliseconds(clampedTargetMilliseconds)
    if (clampedTargetMilliseconds !== lastCommittedMillisecondsRef.current) {
      lastCommittedMillisecondsRef.current = clampedTargetMilliseconds
      void onCommitDebounceMilliseconds(clampedTargetMilliseconds)
    }
  }

  return (
    <div className="p-2.5 bg-[#050506] border border-[#3d2816] space-y-2">
      <div className="flex items-center justify-between gap-2">
        <div>
          <div className="text-[10px] font-bold uppercase tracking-[0.12em] text-[#00D2FF]">
            Live Watcher Debounce
          </div>
          <div className="text-[10px] text-[#a6907c] mt-0.5">
            Batches rapid journal writes such as FSS body scans before recomputing stats (0.3s to
            30.0s).
          </div>
        </div>
        <span className="px-2 py-0.5 bg-[#08151c] border border-[#00D2FF]/60 font-mono-tabular text-xs font-bold text-[#00D2FF] shrink-0">
          {formatDebounceSeconds(draftDebounceMilliseconds)}
        </span>
      </div>

      <input
        type="range"
        aria-label="Live Watcher Debounce"
        min={MIN_JOURNAL_DEBOUNCE_MILLISECONDS}
        max={MAX_JOURNAL_DEBOUNCE_MILLISECONDS}
        step={DEBOUNCE_STEP_MILLISECONDS}
        value={draftDebounceMilliseconds}
        onChange={(event) => setDraftDebounceMilliseconds(Number(event.currentTarget.value))}
        onPointerUp={(event) => applyAndCommitMilliseconds(Number(event.currentTarget.value))}
        onKeyUp={(event) => applyAndCommitMilliseconds(Number(event.currentTarget.value))}
        onBlur={(event) => applyAndCommitMilliseconds(Number(event.currentTarget.value))}
        className="w-full accent-[#00D2FF] cursor-pointer"
      />

      <div className="flex flex-wrap items-center justify-between gap-1.5">
        <div className="flex flex-wrap items-center gap-1">
          {DEBOUNCE_PRESETS.map((preset) => {
            const isActivePreset = draftDebounceMilliseconds === preset.milliseconds
            return (
              <button
                key={preset.milliseconds}
                type="button"
                onClick={() => applyAndCommitMilliseconds(preset.milliseconds)}
                className={`px-2 py-0.5 font-mono-tabular text-[10px] font-bold border transition-colors cursor-pointer ${
                  isActivePreset
                    ? 'bg-[#00D2FF] border-[#00D2FF] text-[#050506]'
                    : 'bg-[#110c08] border-[#3d2816] text-[#a6907c] hover:border-[#00D2FF] hover:text-[#F4ECE4]'
                }`}
              >
                {preset.label}
              </button>
            )
          })}
        </div>
        <button
          type="button"
          disabled={draftDebounceMilliseconds === normalizedDefaultMilliseconds}
          onClick={() => applyAndCommitMilliseconds(normalizedDefaultMilliseconds)}
          className="ed-btn-cyan"
        >
          Reset ({formatDebounceSeconds(normalizedDefaultMilliseconds)})
        </button>
      </div>
    </div>
  )
}

