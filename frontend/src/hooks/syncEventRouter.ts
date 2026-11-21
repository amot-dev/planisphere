import type { DataChangedPayload } from '../api/models'
import type { stats } from '../api/models'

/**
 * SyncBootLifecycleState tracks the two-stage terminal boot lifecycle so early
 * Wails events do not cancel in-flight bootstrap RPCs before main paint.
 */
export interface SyncBootLifecycleState {
  initialBootComplete: boolean
  bootCoreLoaded: boolean
  pendingBootRefresh: boolean
}

/**
 * DataChangedDispatchContext supplies current synchronization state and loader
 * callbacks when routing a backend data:changed event.
 */
export interface DataChangedDispatchContext {
  bootState: SyncBootLifecycleState
  currentActiveExpeditionId: number
  currentFilter: stats.Filter
  refreshExpeditionListOnly: () => void
  loadIngestStatusOnly: () => Promise<unknown>
  loadSettingsOnly: () => Promise<unknown>
  loadModesOnly: () => Promise<unknown>
  loadPlanOnly: (expeditionId: number) => Promise<void>
  loadStatsOnly: (expeditionId: number, currentFilter: stats.Filter) => Promise<void>
  refreshAll: () => void
}

/**
 * routeDataChangedEvent dispatches a Wails data:changed notification strictly
 * from its typed domains array without interrupting an in-progress initial boot.
 */
export function routeDataChangedEvent(
  payload: DataChangedPayload | undefined,
  dispatchContext: DataChangedDispatchContext,
): void {
  if (!dispatchContext.bootState.initialBootComplete) {
    if (dispatchContext.bootState.bootCoreLoaded) {
      dispatchContext.bootState.pendingBootRefresh = true
    }
    return
  }

  const domains = payload?.domains
  if (!domains || domains.length === 0) {
    return
  }

  const currentActive = dispatchContext.currentActiveExpeditionId
  const eventExpeditionId = payload?.expeditionId
  const domainSet = new Set(domains)

  if (domainSet.has('settings') && domainSet.has('expeditions') && domainSet.has('modes')) {
    dispatchContext.refreshAll()
    return
  }

  const isTargetActive =
    !eventExpeditionId || (currentActive > 0 && eventExpeditionId === currentActive)

  if (domainSet.has('ingest')) {
    void dispatchContext.loadIngestStatusOnly()
  }
  if (domainSet.has('settings')) {
    void dispatchContext.loadSettingsOnly()
  }
  if (domainSet.has('modes')) {
    void dispatchContext.loadModesOnly()
  }
  if (
    domainSet.has('expeditions') ||
    Boolean(eventExpeditionId && currentActive > 0 && !isTargetActive)
  ) {
    dispatchContext.refreshExpeditionListOnly()
  }
  if (domainSet.has('plan') && isTargetActive && currentActive > 0) {
    void dispatchContext.loadPlanOnly(currentActive)
  }
  if (domainSet.has('stats') && currentActive > 0 && (isTargetActive || domainSet.has('modes'))) {
    void dispatchContext.loadStatsOnly(currentActive, dispatchContext.currentFilter)
  }
}

