// In-universe Galactic Standard Time (+1286 years) conversion utilities.
//
// Elite Dangerous Player Journals record timestamps in real-world UTC ISO-8601
// ("2026-01-18T19:42:10Z"). Every timestamp shown in the UI is rendered in
// Galactic Standard Time ("3312-01-18 19:42:10 UTC").

export const GALACTIC_YEAR_OFFSET = 1286

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

function pad4(n: number): string {
  return String(n).padStart(4, '0')
}

/**
 * Converts a real ISO-8601 UTC timestamp ("2026-01-18T19:42:10Z") into
 * in-universe Galactic Standard Time ("3312-01-18 19:42:10 UTC").
 */
export function toGalacticTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const trimmed = iso.trim()
  const fast = /^(\d{4})-(\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})/.exec(trimmed)
  if (fast && (trimmed.endsWith('Z') || trimmed.endsWith('UTC'))) {
    const year = Number(fast[1]) + GALACTIC_YEAR_OFFSET
    return `${pad4(year)}-${fast[2]} ${fast[3]} UTC`
  }
  const d = new Date(trimmed)
  if (Number.isNaN(d.getTime())) return trimmed
  const y = d.getUTCFullYear() + GALACTIC_YEAR_OFFSET
  const m = pad2(d.getUTCMonth() + 1)
  const day = pad2(d.getUTCDate())
  const hh = pad2(d.getUTCHours())
  const mm = pad2(d.getUTCMinutes())
  const ss = pad2(d.getUTCSeconds())
  return `${pad4(y)}-${m}-${day} ${hh}:${mm}:${ss} UTC`
}

/**
 * Converts a Galactic Standard Time string ("3312-01-18 19:42:10 UTC")
 * back into a canonical real-world ISO-8601 UTC string ("2026-01-18T19:42:10Z").
 */
export function fromGalacticTime(gst: string): string {
  const s = gst
    .trim()
    .replace(/\s*UTC$/i, '')
    .replace(/Z$/i, '')
    .trim()
  const m = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2}))?)?$/.exec(s)
  if (!m) {
    throw new Error(`Invalid Galactic timestamp "${gst}" (expected 33XX-MM-DD HH:mm:ss UTC)`)
  }
  const realYear = Number(m[1]) - GALACTIC_YEAR_OFFSET
  if (realYear < 1) {
    throw new Error(`Galactic year ${m[1]} out of range`)
  }
  const month = m[2]
  const day = m[3]
  const hh = m[4] ?? '00'
  const mm = m[5] ?? '00'
  const ss = m[6] ?? '00'
  return `${pad4(realYear)}-${month}-${day}T${hh}:${mm}:${ss}Z`
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
export function formatCredits(n: number | undefined | null): string {
  const v = Math.round(n ?? 0)
  return `${v.toLocaleString('en-US')} Cr`
}

/**
 * Formats compact credit values (e.g. "95.05M Cr" or "1,420,100 Cr").
 */
export function formatLy(n: number | undefined | null, decimals = 2): string {
  const v = n ?? 0
  return `${v.toLocaleString('en-US', {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  })} Ly`
}

export function formatInt(n: number | undefined | null): string {
  return Math.round(n ?? 0).toLocaleString('en-US')
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
  const q = systemName.trim()
  if (q) {
    return `https://edastro.com/gec/search?q=${encodeURIComponent(q)}`
  }
  return `https://edastro.com/gec`
}
