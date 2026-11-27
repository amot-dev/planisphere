import { vi, type Mock } from 'vitest'
import type { DataChangedPayload, EddImportProgress } from '../api/models'

/**
 * WailsAppBindings mirrors the generated Wails Go App RPC surface.
 */
export type WailsAppBindings = typeof import('../../wailsjs/go/app/App')

type WailsWireFields<T> = Omit<T, 'convertValues'>

type WailsWireObject<T> = {
  [K in keyof WailsWireFields<T> as NonNullable<WailsWireFields<T>[K]> extends object
    ? never
    : K]: undefined extends WailsWireFields<T>[K]
    ? WailsWireFields<T>[K] | null
    : WailsWireFields<T>[K]
} & {
  [K in keyof WailsWireFields<T> as NonNullable<WailsWireFields<T>[K]> extends object
    ? K
    : never]?: WailsWireValue<NonNullable<WailsWireFields<T>[K]>> | null
}

type WailsWireValue<T> = T extends ReadonlyArray<infer Element>
  ? Array<WailsWireValue<Element>>
  : T extends object
    ? T | WailsWireObject<T>
    : T

type WailsMethodReturn<K extends keyof WailsAppBindings> =
  ReturnType<WailsAppBindings[K]> extends Promise<infer ResolvedValue>
    ? Promise<WailsWireValue<ResolvedValue> | null>
    : ReturnType<WailsAppBindings[K]>

type WailsOverrideFunction<K extends keyof WailsAppBindings> = (
  ...args: Parameters<WailsAppBindings[K]>
) => WailsMethodReturn<K>

/**
 * WailsAppMockOverrides defines typed per-method RPC implementations passed to
 * installWailsAppMock.
 */
export type WailsAppMockOverrides = {
  [K in keyof WailsAppBindings]?: WailsOverrideFunction<K>
}

/**
 * WailsAppMockSpies maps each configured RPC override to a typed Vitest Mock.
 */
export type WailsAppMockSpies<TOverrides extends WailsAppMockOverrides> = {
  [K in keyof TOverrides]-?: Mock<WailsOverrideFunction<K & keyof WailsAppBindings>>
}

/**
 * InstalledWailsRuntimeMock exposes typed event emitters and active listener
 * inspection for window.runtime.EventsOnMultiple subscribers.
 */
export interface InstalledWailsRuntimeMock {
  emit: (eventName: string, ...payload: unknown[]) => void
  emitDataChanged: (payload?: DataChangedPayload) => void
  emitIngestStatus: () => void
  emitEddProgress: (progress: EddImportProgress) => void
  emitSyncPending: (pending: boolean) => void
  getListenerCount: (eventName?: string) => number
}

interface WindowSnapshot {
  createdGlobalWindow: boolean
  hadGoProperty: boolean
  previousGo: Window['go']
  hadRuntimeProperty: boolean
  previousRuntime: Window['runtime']
}

interface RuntimeListenerEntry {
  callback: (...eventArgs: unknown[]) => void
  remainingCalls: number
}

let savedWindowSnapshot: WindowSnapshot | null = null

function ensureWindowWithSnapshot(): Window & typeof globalThis {
  const globalRecord = globalThis as { window?: Window & typeof globalThis }
  const createdGlobalWindow = typeof globalRecord.window === 'undefined'
  if (createdGlobalWindow) {
    globalRecord.window = {} as Window & typeof globalThis
  }
  const targetWindow = globalRecord.window as Window & typeof globalThis
  if (!savedWindowSnapshot) {
    savedWindowSnapshot = {
      createdGlobalWindow,
      hadGoProperty: Object.prototype.hasOwnProperty.call(targetWindow, 'go'),
      previousGo: targetWindow.go,
      hadRuntimeProperty: Object.prototype.hasOwnProperty.call(targetWindow, 'runtime'),
      previousRuntime: targetWindow.runtime,
    }
  }
  return targetWindow
}

/**
 * resetWailsMocks restores window.go, window.runtime, and globalThis.window to
 * their pre-test state.
 */
export function resetWailsMocks(): void {
  if (!savedWindowSnapshot) {
    return
  }
  const snapshot = savedWindowSnapshot
  savedWindowSnapshot = null
  const globalRecord = globalThis as { window?: Window & typeof globalThis }
  if (snapshot.createdGlobalWindow) {
    delete globalRecord.window
    return
  }
  const targetWindow = globalRecord.window
  if (!targetWindow) {
    return
  }
  if (snapshot.hadGoProperty) {
    targetWindow.go = snapshot.previousGo
  } else {
    delete targetWindow.go
  }
  if (snapshot.hadRuntimeProperty) {
    targetWindow.runtime = snapshot.previousRuntime
  } else {
    delete targetWindow.runtime
  }
}

/**
 * installWailsAppMock installs a strict Proxy on window.go.app.App backed by
 * Vitest spies for provided overrides and throwing on unexpected RPC calls.
 */
export function installWailsAppMock<TOverrides extends WailsAppMockOverrides>(
  overrides: TOverrides = {} as TOverrides,
): { spies: WailsAppMockSpies<TOverrides> } {
  const targetWindow = ensureWindowWithSnapshot()
  const spiesRecord: Record<string, Mock> = {}

  for (const methodName of Object.keys(overrides) as Array<keyof TOverrides & string>) {
    const configuredHandler = overrides[methodName]
    if (typeof configuredHandler === 'function') {
      spiesRecord[methodName] = vi.fn(configuredHandler as (...args: never[]) => unknown)
    }
  }

  const appProxy = new Proxy(spiesRecord, {
    get(target, propertyKey) {
      if (typeof propertyKey === 'symbol' || propertyKey === 'then' || propertyKey === 'toJSON') {
        return undefined
      }
      if (Object.prototype.hasOwnProperty.call(target, propertyKey)) {
        return target[propertyKey]
      }
      return (..._unexpectedArgs: unknown[]): never => {
        throw new Error(`Unmocked Wails RPC call: app.App.${String(propertyKey)}`)
      }
    },
  })

  targetWindow.go = {
    app: {
      App: appProxy as unknown as NonNullable<NonNullable<Window['go']>['app']>['App'],
    },
  }

  return {
    spies: spiesRecord as unknown as WailsAppMockSpies<TOverrides>,
  }
}

/**
 * installWailsRuntimeMock installs window.runtime.EventsOnMultiple and returns
 * typed emitters and active listener counters.
 */
export function installWailsRuntimeMock(): InstalledWailsRuntimeMock {
  const targetWindow = ensureWindowWithSnapshot()
  const listenersByEvent = new Map<string, Set<RuntimeListenerEntry>>()

  targetWindow.runtime = {
    EventsOnMultiple(eventName, callback, maxCallbacks) {
      if (maxCallbacks === 0) {
        return () => {}
      }
      const listenersForEvent = listenersByEvent.get(eventName) ?? new Set<RuntimeListenerEntry>()
      const listenerEntry: RuntimeListenerEntry = {
        callback,
        remainingCalls: maxCallbacks < 0 ? -1 : maxCallbacks,
      }
      listenersForEvent.add(listenerEntry)
      listenersByEvent.set(eventName, listenersForEvent)
      return () => {
        listenersForEvent.delete(listenerEntry)
        if (listenersForEvent.size === 0) {
          listenersByEvent.delete(eventName)
        }
      }
    },
  }

  const emit = (eventName: string, ...payload: unknown[]): void => {
    const listenersForEvent = listenersByEvent.get(eventName)
    if (!listenersForEvent) {
      return
    }
    for (const listenerEntry of Array.from(listenersForEvent)) {
      listenerEntry.callback(...payload)
      if (listenerEntry.remainingCalls > 0) {
        listenerEntry.remainingCalls -= 1
        if (listenerEntry.remainingCalls === 0) {
          listenersForEvent.delete(listenerEntry)
        }
      }
    }
    if (listenersForEvent.size === 0) {
      listenersByEvent.delete(eventName)
    }
  }

  return {
    emit,
    emitDataChanged: (payload?: DataChangedPayload) => emit('data:changed', payload),
    emitIngestStatus: () => emit('ingest:status'),
    emitEddProgress: (progress: EddImportProgress) => emit('edd:progress', progress),
    emitSyncPending: (pending: boolean) => emit('sync:pending', pending),
    getListenerCount: (eventName?: string) => {
      if (eventName !== undefined) {
        return listenersByEvent.get(eventName)?.size ?? 0
      }
      let totalListeners = 0
      for (const listenerSet of listenersByEvent.values()) {
        totalListeners += listenerSet.size
      }
      return totalListeners
    },
  }
}
