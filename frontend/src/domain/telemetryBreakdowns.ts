import { realization as realizationModel, stats } from '../api/models'
import {
  formatCredits,
  formatEfficiencyBonusLabel,
  formatEfficiencyBonusTooltip,
  formatLoss,
} from './galacticTime'

/** Single formatted valuation row in a Cartographics or Exobiology breakdown list. */
export interface TelemetryBreakdownRow<TTone extends string = string> {
  label: string
  value: string
  tone: TTone
  tooltip?: string
}

/** Single formatted deduction row in a Realization loss breakdown list. */
export interface RealizationLossRow {
  label: string
  value: string
}

const LOSS_ROW_DEFINITIONS: ReadonlyArray<{
  label: string
  selectLoss: (breakdown?: realizationModel.Breakdown | null) => number | null | undefined
}> = [
  { label: 'Cartographics Lost (Death)', selectLoss: (breakdown) => breakdown?.cartoLostDeath },
  { label: 'Exobiology Lost (Death)', selectLoss: (breakdown) => breakdown?.organicLostDeath },
  { label: 'Cartographics Stolen', selectLoss: (breakdown) => breakdown?.cartoStolen },
  { label: 'Exobiology Stolen', selectLoss: (breakdown) => breakdown?.organicStolen },
  { label: 'Carrier Tariffs', selectLoss: (breakdown) => breakdown?.carrierTariffLoss },
  { label: 'NPC Crew Payouts', selectLoss: (breakdown) => breakdown?.npcCrewLoss },
]

/**
 * Builds the three standardized Universal Cartographics valuation breakdown rows
 * shared across sidebar panels, waypoint drawers, PNG export cards, and HTML reports.
 */
export function buildCartographicsBreakdownRows(
  metrics: stats.Metrics,
): TelemetryBreakdownRow<'main' | 'cyan'>[] {
  return [
    {
      label: 'Base Scans and Mapping',
      value: formatCredits(metrics.scanValueBase),
      tone: 'main',
    },
    {
      label: 'First Discovered / Mapped',
      value: `+${formatCredits(metrics.scanValueBonus)}`,
      tone: 'cyan',
    },
    {
      label: formatEfficiencyBonusLabel(metrics.efficientMappings, metrics.bodiesMapped),
      value: `+${formatCredits(metrics.scanValueEfficiencyBonus)}`,
      tone: 'cyan',
      tooltip: formatEfficiencyBonusTooltip(metrics.efficientMappings, metrics.bodiesMapped),
    },
  ]
}

/**
 * Builds the two standardized Vista Genomics exobiology valuation breakdown rows
 * shared across sidebar panels, waypoint drawers, PNG export cards, and HTML reports.
 */
export function buildExobiologyBreakdownRows(
  metrics: stats.Metrics,
): TelemetryBreakdownRow<'main' | 'emerald'>[] {
  return [
    {
      label: 'Base Organic Value',
      value: formatCredits(metrics.organicValueBase),
      tone: 'main',
    },
    {
      label: 'First Logged Bonus',
      value: `+${formatCredits(metrics.organicValueBonus)}`,
      tone: 'emerald',
    },
  ]
}

/**
 * Builds the six standardized realization loss and tariff deduction rows
 * shared across sidebar panels, waypoint drawers, PNG export cards, and HTML reports.
 */
export function buildRealizationLossRows(
  realizationBreakdown?: realizationModel.Breakdown | null,
): RealizationLossRow[] {
  return LOSS_ROW_DEFINITIONS.map((definition) => ({
    label: definition.label,
    value: formatLoss(definition.selectLoss(realizationBreakdown)),
  }))
}
