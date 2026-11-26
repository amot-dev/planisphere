import { formatCredits } from '../../domain/galacticTime'
import { DEFAULT_MODE_COLOR } from '../../domain/theme'
import { ExportCarrierCost } from './exportTypes'

const EXPORT_HEX_COLOR_PATTERN = /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/

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
