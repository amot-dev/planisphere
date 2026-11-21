import { useEffect, useRef, useState } from 'react'
import {
  DEBOUNCE_STEP_MILLISECONDS,
  JOURNAL_DEBOUNCE_PRESETS,
  MAX_JOURNAL_DEBOUNCE_MILLISECONDS,
  MIN_JOURNAL_DEBOUNCE_MILLISECONDS,
  clampDebounceMilliseconds,
  formatDebounceSeconds,
} from '../../domain/journalDebounce'

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
          <div className="text-[10px] font-bold uppercase tracking-[0.12em] text-[#F4ECE4]">
            Live Watcher Debounce
          </div>
          <div className="text-[10px] text-[#a6907c] mt-0.5">
            Batches rapid journal writes such as FSS body scans before recomputing stats (0.3s to
            30.0s).
          </div>
        </div>
        <span className="px-2 py-0.5 bg-[#16100b] border border-[#3d2816] font-mono-tabular text-xs font-bold text-[#F4ECE4] shrink-0">
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
        className="w-full accent-[#FF7100] cursor-pointer"
      />

      <div className="flex flex-wrap items-center justify-between gap-1.5">
        <div className="flex flex-wrap items-center gap-1">
          {JOURNAL_DEBOUNCE_PRESETS.map((preset) => {
            const isActivePreset = draftDebounceMilliseconds === preset.milliseconds
            return (
              <button
                key={preset.milliseconds}
                type="button"
                onClick={() => applyAndCommitMilliseconds(preset.milliseconds)}
                className={`px-2 py-0.5 font-mono-tabular text-[10px] font-bold border transition-colors cursor-pointer ${
                  isActivePreset
                    ? 'bg-[#FF7100] border-[#FF7100] text-[#050506]'
                    : 'bg-[#110c08] border-[#3d2816] text-[#a6907c] hover:border-[#FF7100] hover:text-[#F4ECE4]'
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
          className="ed-btn"
        >
          RESET ({formatDebounceSeconds(normalizedDefaultMilliseconds)})
        </button>
      </div>
    </div>
  )
}

