import { realization, stats } from '../../api/models'
import { formatCredits } from '../../domain/galacticTime'
import { DEFAULT_MODE_COLOR } from '../../domain/theme'
import { ExportCarrierCost } from './exportTypes'

const EXPORT_HEX_COLOR_PATTERN = /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/
export const MILLISECONDS_PER_DAY = 24 * 60 * 60 * 1000

/** Validates a CSS hex color (#RGB or #RRGGBB), returning fallback for malformed inputs. */
export function sanitizeExportHexColor(
  rawColor: string | null | undefined,
  fallback: string = DEFAULT_MODE_COLOR,
): string {
  const trimmedColor = rawColor?.trim() ?? ''
  return EXPORT_HEX_COLOR_PATTERN.test(trimmedColor) ? trimmedColor : fallback
}

/** Formats carrier credit values with a tilde prefix when estimated and greater than zero. */
export function formatEstimatedCarrierCredits(
  amount: number,
  isEstimated: boolean = amount > 0,
): string {
  return isEstimated && amount > 0 ? `~${formatCredits(amount)}` : formatCredits(amount)
}

/** Formatted net profit value paired with whether its rounded amount is negative. */
export interface FormattedNetProfit {
  text: string
  isNegative: boolean
}

/**
 * Formats credit amounts with any minus sign placed before the tilde prefix
 * and normalizes negative values rounding to zero.
 */
export function formatSignedEstimatedCredits(amount: number, isEstimated: boolean): string {
  const rounded = Math.round(amount) || 0
  if (rounded < 0) {
    const absoluteText = formatCredits(Math.abs(rounded))
    return isEstimated ? `-~${absoluteText}` : `-${absoluteText}`
  }
  const formattedText = formatCredits(rounded)
  return isEstimated ? `~${formattedText}` : formattedText
}

/**
 * Formats a net profit credit amount using carrier cost estimation metadata
 * and reports whether the rounded amount is negative.
 */
export function formatNetProfitValue(
  amount: number,
  carrierCost: ExportCarrierCost | null,
): FormattedNetProfit {
  const rounded = Math.round(amount) || 0
  const isEstimated = Boolean(
    carrierCost && carrierCost.totalCost > 0 && carrierCost.totalEstimated,
  )
  return {
    text: formatSignedEstimatedCredits(rounded, isEstimated),
    isNegative: rounded < 0,
  }
}

/** Converts a display title into a clean lowercase filename slug. */
export function slugify(rawText: string): string {
  const cleaned = rawText
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return cleaned || 'block'
}

/** Converts a camelCase identifier into a hyphenated lowercase filename slug. */
export function slugifyCamelCase(rawText: string): string {
  return slugify(rawText.replace(/([a-z0-9])([A-Z])/g, '$1-$2'))
}

/** Returns an empty zeroed Metrics object structurally matching Wails stats.Metrics. */
export function createEmptyMetrics(): stats.Metrics {
  return {
    distanceLy: 0,
    shipDistanceLy: 0,
    carrierPassengerLy: 0,
    jumps: 0,
    carrierJumps: 0,
    carrierDistanceLy: 0,
    tritiumUsed: undefined,
    jetConeBoosts: 0,
    premiumBoosts: 0,
    standardBoosts: 0,
    basicBoosts: 0,
    bodiesScanned: 0,
    bodiesMapped: 0,
    efficientMappings: 0,
    scanValueTotal: 0,
    scanValueBase: 0,
    scanValueBonus: 0,
    scanValueEfficiencyBonus: 0,
    firstDiscoveries: 0,
    firstMapped: 0,
    organicSamples: 0,
    organicValueTotal: 0,
    organicValueBase: 0,
    organicValueBonus: 0,
    firstLogged: 0,
  }
}

/** Returns an empty zeroed RealizationBreakdown object. */
export function createEmptyRealization(): realization.Breakdown {
  return {
    totalRealized: 0,
    cartoLostDeath: 0,
    organicLostDeath: 0,
    cartoStolen: 0,
    organicStolen: 0,
    carrierTariffLoss: 0,
    npcCrewLoss: 0,
    pendingValue: 0,
  }
}

/** Sums two Metrics objects field-by-field. */
export function addMetrics(left: stats.Metrics, right: stats.Metrics): stats.Metrics {
  const hasTritium = left.tritiumUsed != null || right.tritiumUsed != null
  return {
    distanceLy: left.distanceLy + right.distanceLy,
    shipDistanceLy: left.shipDistanceLy + right.shipDistanceLy,
    carrierPassengerLy: left.carrierPassengerLy + right.carrierPassengerLy,
    jumps: left.jumps + right.jumps,
    carrierJumps: left.carrierJumps + right.carrierJumps,
    carrierDistanceLy: left.carrierDistanceLy + right.carrierDistanceLy,
    tritiumUsed: hasTritium ? (left.tritiumUsed ?? 0) + (right.tritiumUsed ?? 0) : undefined,
    jetConeBoosts: left.jetConeBoosts + right.jetConeBoosts,
    premiumBoosts: left.premiumBoosts + right.premiumBoosts,
    standardBoosts: left.standardBoosts + right.standardBoosts,
    basicBoosts: left.basicBoosts + right.basicBoosts,
    bodiesScanned: left.bodiesScanned + right.bodiesScanned,
    bodiesMapped: left.bodiesMapped + right.bodiesMapped,
    efficientMappings: left.efficientMappings + right.efficientMappings,
    scanValueTotal: left.scanValueTotal + right.scanValueTotal,
    scanValueBase: left.scanValueBase + right.scanValueBase,
    scanValueBonus: left.scanValueBonus + right.scanValueBonus,
    scanValueEfficiencyBonus: left.scanValueEfficiencyBonus + right.scanValueEfficiencyBonus,
    firstDiscoveries: left.firstDiscoveries + right.firstDiscoveries,
    firstMapped: left.firstMapped + right.firstMapped,
    organicSamples: left.organicSamples + right.organicSamples,
    organicValueTotal: left.organicValueTotal + right.organicValueTotal,
    organicValueBase: left.organicValueBase + right.organicValueBase,
    organicValueBonus: left.organicValueBonus + right.organicValueBonus,
    firstLogged: left.firstLogged + right.firstLogged,
  }
}

/** Sums two RealizationBreakdown objects field-by-field. */
export function addRealization(
  left: realization.Breakdown,
  right: realization.Breakdown,
): realization.Breakdown {
  return {
    totalRealized: left.totalRealized + right.totalRealized,
    cartoLostDeath: left.cartoLostDeath + right.cartoLostDeath,
    organicLostDeath: left.organicLostDeath + right.organicLostDeath,
    cartoStolen: left.cartoStolen + right.cartoStolen,
    organicStolen: left.organicStolen + right.organicStolen,
    carrierTariffLoss: left.carrierTariffLoss + right.carrierTariffLoss,
    npcCrewLoss: left.npcCrewLoss + right.npcCrewLoss,
    pendingValue: left.pendingValue + right.pendingValue,
  }
}

/** Computes fractional elapsed days between two ISO timestamps when positive, or null otherwise. */
export function computeElapsedDays(
  fromIso: string | undefined,
  toIso: string | undefined,
  nowMs: number,
): number | null {
  if (!fromIso) return null
  const startMs = Date.parse(fromIso)
  if (Number.isNaN(startMs)) return null
  const endMs = toIso ? Date.parse(toIso) : nowMs
  if (Number.isNaN(endMs) || endMs <= startMs) return null
  return (endMs - startMs) / MILLISECONDS_PER_DAY
}

/** Divides numerator by denominator when denominator is finite and strictly positive. */
export function safeDivide(numerator: number, denominator: number): number | null {
  return Number.isFinite(denominator) && denominator > 0 ? numerator / denominator : null
}

/** Computes (part / whole) * 100 when whole is finite and strictly positive. */
export function safePercent(part: number, whole: number): number | null {
  return Number.isFinite(whole) && whole > 0 ? (part / whole) * 100 : null
}

/** Computes value per 1,000 light-years when distanceLy is finite and strictly positive. */
export function safePerThousand(value: number, distanceLy: number): number | null {
  return Number.isFinite(distanceLy) && distanceLy > 0 ? value / (distanceLy / 1000) : null
}
