import { GetCarrierDepotInfo, GetCarrierLastLocation } from '../../../wailsjs/go/app/App'
import { app, expedition } from '../../../wailsjs/go/models'
import { calculateCarrierJumpFuel } from '../../utils/carrierRouting'

/**
 * NextCarrierStopEntry identifies the next unarrived Fleet Carrier stop row and
 * its index within the expedition plan's ordered row list.
 */
export interface NextCarrierStopEntry {
  row: expedition.Row
  index: number
}

/**
 * resolveCarrierStartSystem determines the default starting star system for a
 * Fleet Carrier route using live carrier docking journals, preceding carrier
 * stops, preceding waypoints, or the commander's current location.
 */
export async function resolveCarrierStartSystem(
  plan: expedition.PlanView | null,
  upToIndex: number,
): Promise<string> {
  if (!plan) return ''
  let startSystem = ''
  try {
    const lastKnownCarrierLocation = await GetCarrierLastLocation()
    if (lastKnownCarrierLocation && lastKnownCarrierLocation.trim()) {
      startSystem = lastKnownCarrierLocation.trim()
    }
  } catch {
    startSystem = ''
  }

  const rows = plan.rows ?? []
  const searchFrom = Math.min(upToIndex - 1, rows.length - 1)

  // Fallback: most recent preceding Fleet Carrier stop in the expedition plan
  if (!startSystem) {
    for (let i = searchFrom; i >= 0; i--) {
      const candidate = rows[i]?.row
      if (candidate?.rowType === 'waypoint' && candidate.fcStop && candidate.systemName?.trim()) {
        startSystem = candidate.systemName.trim()
        break
      }
    }
  }

  // Secondary fallback: immediately preceding waypoint in the plan
  if (!startSystem) {
    for (let i = searchFrom; i >= 0; i--) {
      const candidate = rows[i]?.row
      if (candidate?.rowType === 'waypoint' && candidate.systemName?.trim()) {
        startSystem = candidate.systemName.trim()
        break
      }
    }
  }

  if (!startSystem && plan.playerSystem?.trim()) {
    startSystem = plan.playerSystem.trim()
  }

  return startSystem
}

/**
 * resolveCarrierDefaults queries recent journal telemetry for the carrier's
 * cargo mass and tritium depot level, falling back to standard defaults.
 */
export async function resolveCarrierDefaults(): Promise<{ capacity: number; depot: number }> {
  let capacity = 5000
  let depot = 1000
  try {
    const carrierDepotSnapshot = await GetCarrierDepotInfo()
    if (carrierDepotSnapshot) {
      if (carrierDepotSnapshot.usedCapacity > 0) capacity = carrierDepotSnapshot.usedCapacity
      if (carrierDepotSnapshot.depotTritium > 0) depot = carrierDepotSnapshot.depotTritium
    }
  } catch {
    capacity = 5000
    depot = 1000
  }
  return { capacity, depot }
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
  activeCarrierRoute: app.CarrierRouteResult | null,
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
  activeCarrierRoute: app.CarrierRouteResult,
  carrierJumpIndex: number,
  newDepot: number,
  newCapacityUsed?: number,
): app.CarrierRouteResult {
  const clampedDepot = Math.max(0, Math.min(1000, newDepot))
  const maxCarrierCapacityTonnes = activeCarrierRoute.carrierType === 'squadron' ? 60000 : 25000
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

  return app.CarrierRouteResult.createFrom({
    ...activeCarrierRoute,
    depotTritium: clampedDepot,
    capacityUsed: updatedCapacity,
    jumps: recalculatedJumps,
    totalTritium: newTotalTritium,
  })
}

