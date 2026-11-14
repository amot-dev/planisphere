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
 * Converts a Galactic Standard Time string ("3312-01-18 19:42 UTC")
 * back into a canonical real-world ISO-8601 UTC string ("2026-01-18T19:42:00Z").
 */
export function fromGalacticTime(galacticTimeString: string): string {
  const cleanedString = galacticTimeString
    .trim()
    .replace(/\s*UTC$/i, '')
    .replace(/Z$/i, '')
    .trim()
  const matchResult = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2}))?)?$/.exec(cleanedString)
  if (!matchResult) {
    throw new Error(`Invalid Galactic timestamp "${galacticTimeString}" (expected 33XX-MM-DD HH:mm UTC)`)
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
 * Formats credit loss values with negative prefix and "Cr" suffix.
 */
export function formatLoss(creditLoss?: number | null): string {
  if (!creditLoss || creditLoss === 0) {
    return '0 Cr'
  }
  return `-${formatCredits(creditLoss)}`
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

export function edastroGecUrl(systemName: string, gecDirectUrl?: string): string {
  if (gecDirectUrl) return gecDirectUrl
  const query = systemName.trim()
  if (query) {
    return `https://edastro.com/gec/search?q=${encodeURIComponent(query)}`
  }
  return `https://edastro.com/gec`
}
