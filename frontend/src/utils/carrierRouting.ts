import { expedition } from '../api/models'

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
 * NextCarrierStopEntry identifies the next unarrived Fleet Carrier stop row and
 * its index within the expedition plan's ordered row list.
 */
export interface NextCarrierStopEntry {
  row: expedition.Row
  index: number
}

/**
 * normalizeDepotTritium clamps an optional depot fuel reading into [0, 1000],
 * defaulting missing readings to a full 1000 t depot tank.
 */
export function normalizeDepotTritium(depotTritium: number | undefined): number {
  return depotTritium !== undefined && depotTritium >= 0
    ? depotTritium
    : CARRIER_MAX_DEPOT_TONNES
}

/**
 * computeDepotRefill computes the updated fuel depot and cargo storage mass
 * after transferring tritium from carrier storage into the depot up to 1000 t,
 * returning null if the depot is already full.
 */
export function computeDepotRefill(
  currentDepot: number,
  currentCapacityUsed: number,
): { nextDepot: number; nextCapacity: number } | null {
  const neededTonnes = CARRIER_MAX_DEPOT_TONNES - currentDepot
  if (neededTonnes <= 0) return null
  const transferTonnes =
    currentCapacityUsed > 0 ? Math.min(neededTonnes, currentCapacityUsed) : neededTonnes
  const nextDepot = currentDepot + transferTonnes
  const nextCapacity =
    currentCapacityUsed > 0
      ? Math.max(0, currentCapacityUsed - transferTonnes)
      : currentCapacityUsed
  return { nextDepot, nextCapacity }
}

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
  maxCapacity: number = CARRIER_MAX_SQUADRON_CAPACITY_TONNES,
): number {
  if (distanceLy <= 0) return 0
  const clampedCapacity = Math.max(0, Math.min(maxCapacity, usedCapacity || 0))
  const clampedDepot = Math.max(0, Math.min(CARRIER_MAX_DEPOT_TONNES, depotTritium || 0))
  const totalMass = CARRIER_BASE_MASS_TONNES + clampedCapacity + clampedDepot
  return Math.round(CARRIER_FIXED_JUMP_TONNES + (distanceLy * totalMass) / CARRIER_FUEL_DIVISOR)
}

/**
 * findNextCarrierStopEntry locates the next unarrived Fleet Carrier stop row
 * in the active expedition plan.
 */
export function findNextCarrierStopEntry(
  plan: expedition.PlanView | null,
): NextCarrierStopEntry | null {
  if (!plan?.rows) return null
  if (plan.nextFcStopRowId > 0) {
    const matchedRowIndex = plan.rows.findIndex(
      (rowView) => rowView?.row?.id === plan.nextFcStopRowId,
    )
    if (
      matchedRowIndex >= 0 &&
      plan.rows[matchedRowIndex].row.fcStop &&
      !plan.rows[matchedRowIndex].row.fcArrived
    ) {
      return { row: plan.rows[matchedRowIndex].row, index: matchedRowIndex }
    }
  }
  const fallbackRowIndex = plan.rows.findIndex(
    (rowView) =>
      rowView?.row?.rowType === 'waypoint' &&
      rowView.row.fcStop &&
      !rowView.row.fcArrived,
  )
  if (fallbackRowIndex >= 0) {
    return { row: plan.rows[fallbackRowIndex].row, index: fallbackRowIndex }
  }
  return null
}

/**
 * computeRoutedCarrierStopIndex returns the row index locked by an active
 * Spansh or manual carrier route, or -1 when no carrier route is active.
 */
export function computeRoutedCarrierStopIndex(
  activeCarrierRoute: expedition.CarrierRouteResult | null,
  activeCarrierTargetRowId: number,
  isManualCarrierRoute: boolean,
  nextCarrierStopEntry: NextCarrierStopEntry | null,
  rows: expedition.RowView[] | undefined,
): number {
  if (!activeCarrierRoute || !rows) return -1
  if (isManualCarrierRoute) {
    return nextCarrierStopEntry ? nextCarrierStopEntry.index : -1
  }
  if (activeCarrierTargetRowId > 0) {
    const targetRowIndex = rows.findIndex(
      (rowView) => rowView?.row?.id === activeCarrierTargetRowId,
    )
    if (targetRowIndex >= 0) {
      return targetRowIndex
    }
  }
  return nextCarrierStopEntry ? nextCarrierStopEntry.index : -1
}

/**
 * recalculateCarrierRouteDepotFuel updates the carrier depot level and optional
 * cargo mass, recalculating tritium burn for all remaining jumps in the route.
 */
export function recalculateCarrierRouteDepotFuel(
  activeCarrierRoute: expedition.CarrierRouteResult,
  carrierJumpIndex: number,
  newDepot: number,
  newCapacityUsed?: number,
): expedition.CarrierRouteResult {
  const clampedDepot = Math.max(0, Math.min(CARRIER_MAX_DEPOT_TONNES, newDepot))
  const maxCarrierCapacityTonnes =
    activeCarrierRoute.carrierType === 'squadron'
      ? CARRIER_MAX_SQUADRON_CAPACITY_TONNES
      : CARRIER_MAX_PERSONAL_CAPACITY_TONNES
  const updatedCapacity =
    newCapacityUsed !== undefined
      ? Math.max(0, Math.min(maxCarrierCapacityTonnes, newCapacityUsed))
      : (activeCarrierRoute.capacityUsed || 0)

  const currentJumps = activeCarrierRoute.jumps || []
  const recalculatedJumps = currentJumps.map((jump, hopIndex) => {
    if (hopIndex < carrierJumpIndex) {
      return jump
    }
    const recalculatedCost = calculateCarrierJumpFuel(
      jump.distanceLy,
      updatedCapacity,
      clampedDepot,
      maxCarrierCapacityTonnes,
    )
    return {
      ...jump,
      tritiumUsed: recalculatedCost,
      fuelInTank: clampedDepot,
    }
  })

  const newTotalTritium = recalculatedJumps.reduce(
    (sum, carrierJump) => sum + (carrierJump.tritiumUsed || 0),
    0,
  )

  return expedition.CarrierRouteResult.createFrom({
    ...activeCarrierRoute,
    depotTritium: clampedDepot,
    capacityUsed: updatedCapacity,
    jumps: recalculatedJumps,
    totalTritium: newTotalTritium,
  })
}
