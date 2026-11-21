import { GetCarrierDepotInfo, GetCarrierLastLocation } from '../../api/backend'
import { expedition } from '../../api/models'

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

  // Fallback: most recent preceding Fleet Carrier stop in the expedition plan.
  if (!startSystem) {
    for (let i = searchFrom; i >= 0; i--) {
      const candidate = rows[i]?.row
      if (candidate?.rowType === 'waypoint' && candidate.fcStop && candidate.systemName?.trim()) {
        startSystem = candidate.systemName.trim()
        break
      }
    }
  }

  // Secondary fallback: immediately preceding waypoint in the plan.
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
