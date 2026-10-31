// In-universe Galactic Standard Time (+1286 years) conversion utilities.
//
// Elite Dangerous Player Journals record timestamps in real-world UTC ISO-8601
// ("2026-01-18T19:42:10Z"). Every timestamp shown in the UI is rendered in
// Galactic Standard Time ("3312-01-18 19:42:10 UTC").

export const GALACTIC_YEAR_OFFSET = 1286

function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

function pad4(value: number): string {
  return String(value).padStart(4, '0')
}

/**
 * Converts a real ISO-8601 UTC timestamp ("2026-01-18T19:42:10Z") into
 * in-universe Galactic Standard Time ("3312-01-18 19:42:10 UTC").
 */
export function toGalacticTime(isoTimestamp: string | null | undefined): string {
  if (!isoTimestamp) return ''
  const trimmed = isoTimestamp.trim()
  const fast = /^(\d{4})-(\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})/.exec(trimmed)
  if (fast && (trimmed.endsWith('Z') || trimmed.endsWith('UTC'))) {
    const year = Number(fast[1]) + GALACTIC_YEAR_OFFSET
    return `${pad4(year)}-${fast[2]} ${fast[3]} UTC`
  }
  const date = new Date(trimmed)
  if (Number.isNaN(date.getTime())) return trimmed
  const year = date.getUTCFullYear() + GALACTIC_YEAR_OFFSET
  const month = pad2(date.getUTCMonth() + 1)
  const day = pad2(date.getUTCDate())
  const hours = pad2(date.getUTCHours())
  const minutes = pad2(date.getUTCMinutes())
  const seconds = pad2(date.getUTCSeconds())
  return `${pad4(year)}-${month}-${day} ${hours}:${minutes}:${seconds} UTC`
}

/**
 * Converts a Galactic Standard Time string ("3312-01-18 19:42:10 UTC")
 * back into a canonical real-world ISO-8601 UTC string ("2026-01-18T19:42:10Z").
 */
export function fromGalacticTime(galacticTimeString: string): string {
  const cleanedString = galacticTimeString
    .trim()
    .replace(/\s*UTC$/i, '')
    .replace(/Z$/i, '')
    .trim()
  const matchResult = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2}))?)?$/.exec(cleanedString)
  if (!matchResult) {
    throw new Error(`Invalid Galactic timestamp "${galacticTimeString}" (expected 33XX-MM-DD HH:mm:ss UTC)`)
  }
  const realYear = Number(matchResult[1]) - GALACTIC_YEAR_OFFSET
  if (realYear < 1) {
    throw new Error(`Galactic year ${matchResult[1]} out of range`)
  }
  const month = matchResult[2]
  const day = matchResult[3]
  const hours = matchResult[4] ?? '00'
  const minutes = matchResult[5] ?? '00'
  const seconds = matchResult[6] ?? '00'
  return `${pad4(realYear)}-${month}-${day}T${hours}:${minutes}:${seconds}Z`
}

/**
 * Returns the current time formatted in Galactic Standard Time.
 */
export function nowGalacticTime(): string {
  return toGalacticTime(new Date().toISOString())
}

/**
 * Formats credit values with thousands separators and "Cr" suffix.
 */
export function formatCredits(credits: number | undefined | null): string {
  const roundedCredits = Math.round(credits ?? 0)
  return `${roundedCredits.toLocaleString('en-US')} Cr`
}

/**
 * Formats compact credit values (e.g. "95.05M Cr" or "1,420,100 Cr").
 */
export function formatLy(lightYears: number | undefined | null, decimals = 2): string {
  const value = lightYears ?? 0
  return `${value.toLocaleString('en-US', {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  })} Ly`
}

export function formatInt(value: number | undefined | null): string {
  return Math.round(value ?? 0).toLocaleString('en-US')
}

export function inaraSystemUrl(systemName: string): string {
  return `https://inara.cz/elite/starsystem/?search=${encodeURIComponent(systemName.trim())}`
}

export function edsmSystemUrl(systemName: string): string {
  return `https://www.edsm.net/en/system?systemName=${encodeURIComponent(systemName.trim())}`
}

export function edastroGecUrl(
  systemName: string,
  _systemAddress?: number,
  gecDirectUrl?: string,
): string {
  if (gecDirectUrl) return gecDirectUrl
  const query = systemName.trim()
  if (query) {
    return `https://edastro.com/gec/search?q=${encodeURIComponent(query)}`
  }
  return `https://edastro.com/gec`
}
