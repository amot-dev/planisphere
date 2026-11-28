/**
 * DurationDisplayMode selects whether durations default to calendar elapsed
 * time or reconstructed in-game play time.
 */
export type DurationDisplayMode = 'calendar' | 'in_game'

/**
 * ResolvedDurationDisplay holds the primary plain-text duration label and the
 * optional hover tooltip showing the alternate time format.
 */
export interface ResolvedDurationDisplay {
  primary: string
  tooltip?: string
}

/**
 * normalizeDurationDisplayMode coerces a raw settings value into 'in_game' or
 * the default 'calendar' display mode.
 */
export function normalizeDurationDisplayMode(rawMode?: string | null): DurationDisplayMode {
  return rawMode?.trim().toLowerCase() === 'in_game' ? 'in_game' : 'calendar'
}

/**
 * resolvePrimaryAndTooltipDuration selects the primary duration label and hover
 * tooltip for displayMode, gracefully falling back to calendar elapsed time
 * whenever in-game play time is unavailable.
 */
export function resolvePrimaryAndTooltipDuration(
  calendarLabel: string | null | undefined,
  inGameLabel: string | null | undefined,
  displayMode: DurationDisplayMode,
): ResolvedDurationDisplay {
  const calendarText = calendarLabel?.trim() ?? ''
  const inGameText = inGameLabel?.trim() ?? ''

  if (displayMode === 'in_game' && inGameText !== '') {
    return {
      primary: inGameText,
      tooltip: calendarText !== '' ? `Calendar elapsed: ${calendarText}` : undefined,
    }
  }
  return {
    primary: calendarText,
    tooltip: inGameText !== '' ? `In-game time: ${inGameText}` : undefined,
  }
}
