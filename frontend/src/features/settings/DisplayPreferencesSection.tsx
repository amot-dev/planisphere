import { useEffect, useRef, useState } from 'react'
import { SetDurationDisplayMode } from '../../api/backend'
import { app } from '../../api/models'
import { SettingsCard } from '../../components/ui/SettingsCard'
import {
  normalizeDurationDisplayMode,
  type DurationDisplayMode,
} from '../../domain/durationDisplay'

interface DurationDisplayOption {
  value: DurationDisplayMode
  label: string
}

const DURATION_DISPLAY_OPTIONS: ReadonlyArray<DurationDisplayOption> = [
  { value: 'calendar', label: 'Calendar' },
  { value: 'in_game', label: 'In-Game' },
]

/**
 * DisplayPreferencesSectionProps defines the current settings and error callback
 * for configuring duration display defaults inside Terminal Configuration.
 */
export interface DisplayPreferencesSectionProps {
  settings: app.Settings | null
  onSetError: (errorMessage: string) => void
}

/**
 * DisplayPreferencesSection renders the segmented Calendar vs In-Game duration
 * display control inside Terminal Configuration.
 */
export function DisplayPreferencesSection({
  settings,
  onSetError,
}: DisplayPreferencesSectionProps) {
  const configuredMode = normalizeDurationDisplayMode(settings?.durationDisplayMode)
  const [selectedMode, setSelectedMode] = useState<DurationDisplayMode>(configuredMode)
  const lastCommittedModeRef = useRef<DurationDisplayMode>(configuredMode)
  const requestSequenceRef = useRef(0)

  useEffect(() => {
    lastCommittedModeRef.current = configuredMode
    setSelectedMode(configuredMode)
  }, [configuredMode])

  async function commitDisplayMode(nextMode: DurationDisplayMode) {
    onSetError('')
    setSelectedMode(nextMode)

    const currentSequence = ++requestSequenceRef.current
    try {
      await SetDurationDisplayMode(nextMode)
      if (currentSequence === requestSequenceRef.current) {
        lastCommittedModeRef.current = nextMode
      }
    } catch (saveError) {
      if (currentSequence === requestSequenceRef.current) {
        setSelectedMode(lastCommittedModeRef.current)
        onSetError(String(saveError))
      }
    }
  }

  return (
    <SettingsCard title="Display Preferences">
      <p className="text-[11px] text-ed-text-muted leading-relaxed">
        Choose whether durations default to calendar time or in-game time. Hover any duration to
        inspect the other.
      </p>
      <div className="border border-ed-border bg-ed-void">
        <div className="px-3 py-2 flex items-center justify-between gap-3">
          <span className="font-bold uppercase tracking-wider text-ed-text-main">
            Duration Display
          </span>
          <div
            role="group"
            aria-label="Duration display mode"
            className="inline-flex items-center gap-1 shrink-0"
          >
            {DURATION_DISPLAY_OPTIONS.map((optionItem) => {
              const isSelected = selectedMode === optionItem.value
              return (
                <button
                  key={optionItem.value}
                  type="button"
                  aria-pressed={isSelected}
                  onClick={() => {
                    if (!isSelected) {
                      void commitDisplayMode(optionItem.value)
                    }
                  }}
                  className={`px-2.5 py-1 text-[10px] font-mono-tabular font-bold uppercase tracking-wider border transition-colors cursor-pointer whitespace-nowrap ${
                    isSelected
                      ? 'bg-ed-amber-tint text-ed-amber border-ed-amber'
                      : 'bg-ed-surface text-ed-text-muted border-ed-border-dim hover:text-ed-text-main hover:border-ed-border-strong'
                  }`}
                >
                  {optionItem.label}
                </button>
              )
            })}
          </div>
        </div>
      </div>
    </SettingsCard>
  )
}
