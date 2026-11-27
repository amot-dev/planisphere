import { afterEach, vi } from 'vitest'
import { restoreAllDomMocks } from './domMocks'
import { unmountAllTestRoots } from './render'
import { resetWailsMocks } from './wailsMock'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

function configureTestLocalStorage(): Storage | null {
  if (typeof document === 'undefined' || typeof window === 'undefined') {
    const nodeDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
    if (nodeDescriptor && typeof nodeDescriptor.get === 'function') {
      Object.defineProperty(globalThis, 'localStorage', {
        value: undefined,
        configurable: true,
        writable: true,
      })
    }
    return null
  }
  const existingDescriptor = Object.getOwnPropertyDescriptor(window, 'localStorage')
  if (existingDescriptor && 'value' in existingDescriptor && existingDescriptor.value) {
    return existingDescriptor.value as Storage
  }
  const entries = new Map<string, string>()
  const memoryStorage: Storage = {
    get length() {
      return entries.size
    },
    clear() {
      entries.clear()
    },
    getItem(key: string) {
      return entries.get(String(key)) ?? null
    },
    key(index: number) {
      return Array.from(entries.keys())[index] ?? null
    },
    removeItem(key: string) {
      entries.delete(String(key))
    },
    setItem(key: string, value: string) {
      entries.set(String(key), String(value))
    },
  }
  Object.defineProperty(window, 'localStorage', {
    value: memoryStorage,
    configurable: true,
    enumerable: true,
    writable: true,
  })
  return memoryStorage
}

const domLocalStorage = configureTestLocalStorage()

afterEach(() => {
  unmountAllTestRoots()
  resetWailsMocks()
  restoreAllDomMocks()
  vi.useRealTimers()
  if (domLocalStorage !== null && typeof document !== 'undefined' && typeof window !== 'undefined') {
    window.localStorage.clear()
  }
})
