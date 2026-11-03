import { describe, expect, it } from 'vitest'
import {
  calculateCarrierJumpFuel,
  CARRIER_BASE_MASS_TONNES,
  CARRIER_MAX_DEPOT_TONNES,
} from './carrierRouting'

describe('calculateCarrierJumpFuel', () => {
  it('corrects Spansh discrepancy: charges 91t for 494.89 Ly with 8548t used capacity and 1000t depot', () => {
    // Spansh estimates 89 tonnes by artificially assuming depot only holds 437 tonnes and truncating
    // Truncated integer division: floor(5 + 494.89 * (25000 + 8548 + 437) / 200000) = 89
    const spanshAssumed = Math.floor(5 + (494.89 * (CARRIER_BASE_MASS_TONNES + 8548 + 437)) / 200000)
    expect(spanshAssumed).toBe(89)

    // The actual game registers a full 1,000-ton depot and charges 91 tonnes:
    // Total mass = 25,000 + 8,548 + 1,000 = 34,548
    // ceil(5 + 494.89 * 34548 / 200000) = ceil(90.487...) = 91
    const inGameCost = calculateCarrierJumpFuel(494.89, 8548, CARRIER_MAX_DEPOT_TONNES)
    expect(inGameCost).toBe(91)
  })

  it('calculates 70t for 500 Ly empty carrier with 1000t depot', () => {
    expect(calculateCarrierJumpFuel(500, 0, 1000)).toBe(70)
  })

  it('accurately calculates fuel for high-capacity squadron carriers (up to 60,000t)', () => {
    // Squadron carrier with 60,000t capacity and 1,000t depot:
    // Total mass = 25,000 + 60,000 + 1,000 = 86,000t
    // 500 Ly jump: ceil(5 + 500 * 86000 / 200000) = ceil(5 + 215) = 220t
    expect(calculateCarrierJumpFuel(500, 60000, 1000)).toBe(220)

    // 494.89 Ly jump: ceil(5 + 494.89 * 86000 / 200000) = ceil(217.8027) = 218t
    expect(calculateCarrierJumpFuel(494.89, 60000, 1000)).toBe(218)
  })

  it('returns 0 for zero or negative distance', () => {
    expect(calculateCarrierJumpFuel(0, 5000, 1000)).toBe(0)
    expect(calculateCarrierJumpFuel(-20, 5000, 1000)).toBe(0)
  })

  it('clamps depot between 0 and 1000', () => {
    const costAt1000 = calculateCarrierJumpFuel(400, 5000, 1000)
    const costAtExcess = calculateCarrierJumpFuel(400, 5000, 2000)
    expect(costAtExcess).toBe(costAt1000)

    const costAt0 = calculateCarrierJumpFuel(400, 5000, 0)
    const costAtNeg = calculateCarrierJumpFuel(400, 5000, -500)
    expect(costAtNeg).toBe(costAt0)
  })
})

