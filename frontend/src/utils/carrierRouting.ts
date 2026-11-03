/**
 * Carrier constants matching Elite Dangerous in-game mass mechanics.
 */
export const CARRIER_BASE_MASS_TONNES = 25000
export const CARRIER_FUEL_DIVISOR = 200000
export const CARRIER_FIXED_JUMP_TONNES = 5
export const CARRIER_MAX_DEPOT_TONNES = 1000
export const CARRIER_MAX_PERSONAL_CAPACITY_TONNES = 25000
export const CARRIER_MAX_SQUADRON_CAPACITY_TONNES = 60000

/**
 * Calculate Fleet Carrier tritium consumption for a jump using standard in-game formula:
 * round(5 + (distance * (usedCapacity + depotTritium + 25000)) / 200000)
 *
 * Elite Dangerous rounds fuel cost to the nearest whole number (round).
 * Supports both personal carriers (up to 25,000 t) and squadron carriers (up to 60,000 t).
 */
export function calculateCarrierJumpFuel(
  distanceLy: number,
  usedCapacity: number,
  depotTritium: number,
  maxCapacity: number = CARRIER_MAX_SQUADRON_CAPACITY_TONNES
): number {
  if (distanceLy <= 0) return 0
  const clampedCap = Math.max(0, Math.min(maxCapacity, usedCapacity || 0))
  const clampedDepot = Math.max(0, Math.min(CARRIER_MAX_DEPOT_TONNES, depotTritium || 0))
  const totalMass = CARRIER_BASE_MASS_TONNES + clampedCap + clampedDepot
  return Math.round(CARRIER_FIXED_JUMP_TONNES + (distanceLy * totalMass) / CARRIER_FUEL_DIVISOR)
}

