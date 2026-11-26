// In-universe Galactic Standard Time (+1286 years) conversion utilities.
//
// Elite Dangerous Player Journals record timestamps in real-world UTC ISO-8601
// ("2026-01-18T19:42:10Z"). Every timestamp shown in the UI is rendered in
// Galactic Standard Time rounded to the nearest minute ("3312-01-18 19:42 UTC").

export const GALACTIC_YEAR_OFFSET = 1286

function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

function pad4(value: number): string {
  return String(value).padStart(4, '0')
}

/**
 * Converts a real ISO-8601 UTC timestamp ("2026-01-18T19:42:10Z") into
 * in-universe Galactic Standard Time rounded to the nearest minute ("3312-01-18 19:42 UTC").
 */
export function toGalacticTime(isoTimestamp: string | null | undefined): string {
  if (!isoTimestamp) return ''
  const trimmed = isoTimestamp.trim()
  const normalizedIso = trimmed
    .replace(/^(\d{4}-\d{2}-\d{2})\s+/, '$1T')
    .replace(/\s*UTC$/i, 'Z')
  const parsedDate = new Date(normalizedIso)
  if (Number.isNaN(parsedDate.getTime())) return trimmed
  const roundedDate = new Date(Math.floor((parsedDate.getTime() + 30_000) / 60_000) * 60_000)
  const year = roundedDate.getUTCFullYear() + GALACTIC_YEAR_OFFSET
  const month = pad2(roundedDate.getUTCMonth() + 1)
  const day = pad2(roundedDate.getUTCDate())
  const hours = pad2(roundedDate.getUTCHours())
  const minutes = pad2(roundedDate.getUTCMinutes())
  return `${pad4(year)}-${month}-${day} ${hours}:${minutes} UTC`
}

/**
 * Returns the current time formatted in Galactic Standard Time.
 */
export function nowGalacticTime(): string {
  return toGalacticTime(new Date().toISOString())
}

/**
 * Formats an elapsed duration between two ISO-8601 UTC timestamps (using nowMs
 * when endTimestamp is omitted) matching the backend DurationLabel format.
 */
export function formatElapsedDuration(
  startTimestamp: string | null | undefined,
  endTimestamp?: string | null,
  nowMs: number = Date.now(),
): string {
  if (!startTimestamp) return ''
  const startMs = Date.parse(startTimestamp)
  if (Number.isNaN(startMs)) return ''
  const endMs = endTimestamp ? Date.parse(endTimestamp) : nowMs
  if (Number.isNaN(endMs)) return ''

  const totalMinutes = Math.max(0, Math.floor((endMs - startMs) / 60_000))
  const totalHours = Math.floor(totalMinutes / 60)
  const totalDays = Math.floor(totalHours / 24)
  const hours = totalHours % 24
  const minutes = totalMinutes % 60
  if (totalDays >= 365) {
    const years = Math.floor(totalDays / 365)
    const days = totalDays % 365
    if (days > 0) {
      return `${years}y ${days}d ${pad2(hours)}h ${pad2(minutes)}m`
    }
    return `${years}y ${pad2(hours)}h ${pad2(minutes)}m`
  }
  if (totalDays > 0) {
    return `${totalDays}d ${pad2(hours)}h ${pad2(minutes)}m`
  }
  return `${hours}h ${pad2(minutes)}m`
}

/**
 * Formats credit values with thousands separators and "Cr" suffix.
 */
export function formatCredits(credits: number | undefined | null): string {
  const roundedCredits = Math.round(credits ?? 0)
  return `${roundedCredits.toLocaleString('en-US')} Cr`
}

/**
 * Formats credit loss values with negative prefix and "Cr" suffix.
 */
export function formatLoss(creditLoss?: number | null): string {
  if (!creditLoss || creditLoss === 0) {
    return '0 Cr'
  }
  return `-${formatCredits(creditLoss)}`
}

/**
 * Formats distances in light-years with thousands separators, configurable decimal places, and "Ly" suffix.
 */
export function formatLy(lightYears: number | undefined | null, decimals = 2): string {
  const value = lightYears ?? 0
  return `${value.toLocaleString('en-US', {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  })} Ly`
}

/**
 * Formats rounded integer values with thousands separators.
 */
export function formatInt(value: number | undefined | null): string {
  return Math.round(value ?? 0).toLocaleString('en-US')
}

function normalizeEfficiencyStats(
  efficientMappings: number | undefined | null,
  bodiesMapped: number | undefined | null,
): { efficientCount: number; mappedCount: number; efficiencyPercent: number } | null {
  const mappedCount = Math.round(bodiesMapped ?? 0)
  if (mappedCount <= 0) return null
  const efficientCount = Math.round(efficientMappings ?? 0)
  return {
    efficientCount,
    mappedCount,
    efficiencyPercent: (efficientCount / mappedCount) * 100,
  }
}

/**
 * Formats the Cartographics Efficiency Bonus row label with efficient mapping
 * count and percentage when at least one body has been mapped.
 */
export function formatEfficiencyBonusLabel(
  efficientMappings: number | undefined | null,
  bodiesMapped: number | undefined | null,
): string {
  const efficiencyStats = normalizeEfficiencyStats(efficientMappings, bodiesMapped)
  if (!efficiencyStats) return 'Efficiency Bonus'
  return `Efficiency Bonus (${formatInt(efficiencyStats.efficientCount)} · ${Math.round(efficiencyStats.efficiencyPercent)}%)`
}

/**
 * Formats a hover tooltip detailing the DSS probe efficiency ratio when at
 * least one body has been mapped.
 */
export function formatEfficiencyBonusTooltip(
  efficientMappings: number | undefined | null,
  bodiesMapped: number | undefined | null,
): string | undefined {
  const efficiencyStats = normalizeEfficiencyStats(efficientMappings, bodiesMapped)
  if (!efficiencyStats) return undefined
  return `${formatInt(efficiencyStats.efficientCount)} of ${formatInt(efficiencyStats.mappedCount)} mapped bodies met the DSS probe efficiency target (${efficiencyStats.efficiencyPercent.toFixed(1)}%)`
}

/**
 * Builds an Inara star system search URL for the given system name.
 */
export function inaraSystemUrl(systemName: string): string {
  return `https://inara.cz/elite/starsystem/?search=${encodeURIComponent(systemName.trim())}`
}

/**
 * Builds an EDSM star system lookup URL for the given system name.
 */
export function edsmSystemUrl(systemName: string): string {
  return `https://www.edsm.net/en/system?systemName=${encodeURIComponent(systemName.trim())}`
}

/**
 * Returns a direct EDAstro GEC entry URL when provided, falling back to a GEC system search URL.
 */
export function edastroGecUrl(systemName: string, gecDirectUrl?: string): string {
  if (gecDirectUrl) return gecDirectUrl
  const query = systemName.trim()
  if (query) {
    return `https://edastro.com/gec/search?q=${encodeURIComponent(query)}`
  }
  return `https://edastro.com/gec`
}

