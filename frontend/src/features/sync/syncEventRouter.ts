import type { DataChangedPayload, stats } from '../../api/models'

/**
 * RefreshAllOptions controls whether refreshAll skips speculative plan and
 * stats prefetching before core state resolves (used on database switches).
 */
export interface RefreshAllOptions {
  skipPrefetch?: boolean
}

/**
 * SyncBootLifecycleState tracks the two-stage terminal boot lifecycle so early
 * Wails events do not cancel in-flight bootstrap RPCs before main paint.
 */
export interface SyncBootLifecycleState {
  initialBootComplete: boolean
  bootCoreLoaded: boolean
  deferredPayloads: DataChangedPayload[]
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
  refreshAll: (options?: RefreshAllOptions) => void
}

function createOnceCallback<Args extends unknown[]>(
  callback: (...args: Args) => unknown,
): (...args: Args) => Promise<void> {
  let invoked = false
  return async (...args: Args) => {
    if (invoked) {
      return
    }
    invoked = true
    await callback(...args)
  }
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
    if (
      dispatchContext.bootState.bootCoreLoaded &&
      payload?.domains &&
      payload.domains.length > 0
    ) {
      dispatchContext.bootState.deferredPayloads.push(payload)
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
    dispatchContext.refreshAll({ skipPrefetch: true })
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

/**
 * flushDeferredBootEvents replays data:changed notifications captured during the
 * mid-boot window once bootstrap completes, deduplicating redundant per-domain
 * loads while preserving database-switch full refreshes.
 */
export function flushDeferredBootEvents(dispatchContext: DataChangedDispatchContext): void {
  const queuedPayloads = dispatchContext.bootState.deferredPayloads.splice(0)
  if (queuedPayloads.length === 0) {
    return
  }

  const databaseSwitchPayload = queuedPayloads.find((payload) => {
    const domainSet = new Set(payload.domains ?? [])
    return domainSet.has('settings') && domainSet.has('expeditions') && domainSet.has('modes')
  })
  if (databaseSwitchPayload) {
    routeDataChangedEvent(databaseSwitchPayload, {
      ...dispatchContext,
      bootState: { ...dispatchContext.bootState, initialBootComplete: true },
    })
    return
  }

  const deduplicatedContext: DataChangedDispatchContext = {
    ...dispatchContext,
    bootState: { ...dispatchContext.bootState, initialBootComplete: true },
    refreshExpeditionListOnly: createOnceCallback(dispatchContext.refreshExpeditionListOnly),
    loadIngestStatusOnly: createOnceCallback(dispatchContext.loadIngestStatusOnly),
    loadSettingsOnly: createOnceCallback(dispatchContext.loadSettingsOnly),
    loadModesOnly: createOnceCallback(dispatchContext.loadModesOnly),
    loadPlanOnly: createOnceCallback(dispatchContext.loadPlanOnly),
    loadStatsOnly: createOnceCallback(dispatchContext.loadStatsOnly),
  }

  for (const payload of queuedPayloads) {
    routeDataChangedEvent(payload, deduplicatedContext)
  }
}

