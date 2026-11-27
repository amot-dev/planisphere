import { describe, expect, it } from 'vitest'
import { realization, stats } from '../api/models'
import {
  buildCartographicsBreakdownRows,
  buildExobiologyBreakdownRows,
  buildRealizationLossRows,
} from './telemetryBreakdowns'

describe('telemetryBreakdowns', () => {
  it('builds cartographics, exobiology, and realization loss breakdown rows with mapped-body tooltips', () => {
    const metrics = stats.Metrics.createFrom({
      bodiesMapped: 34,
      efficientMappings: 30,
      scanValueTotal: 42_000_000,
      scanValueBase: 19_000_000,
      scanValueBonus: 15_500_000,
      scanValueEfficiencyBonus: 7_500_000,
      organicValueTotal: 130_000_000,
      organicValueBase: 26_000_000,
      organicValueBonus: 104_000_000,
    })
    const breakdown = realization.Breakdown.createFrom({
      totalRealized: 155_000_000,
      cartoLostDeath: 1_000_000,
      organicLostDeath: 2_000_000,
      carrierTariffLoss: 2_000_000,
      pendingValue: 12_000_000,
    })

    const cartoRows = buildCartographicsBreakdownRows(metrics)
    expect(cartoRows).toHaveLength(3)
    expect(cartoRows[2].tooltip).toContain('30 of 34 mapped bodies')
    expect(buildExobiologyBreakdownRows(metrics)).toHaveLength(2)
    expect(buildRealizationLossRows(breakdown)).toHaveLength(6)
  })
})
