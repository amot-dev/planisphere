import { useCallback, useState } from 'react'

/**
 * Manages a boolean UI preference persisted in localStorage, returning the
 * current boolean state and a stable toggle callback.
 */
export function useLocalStorageBoolean(
  storageKey: string,
  defaultValue: boolean,
): [boolean, () => void] {
  const [enabled, setEnabled] = useState<boolean>(() => {
    try {
      const savedValue = localStorage.getItem(storageKey)
      return savedValue !== null ? savedValue === 'true' : defaultValue
    } catch {
      return defaultValue
    }
  })

  const toggle = useCallback(() => {
    setEnabled((previousValue) => {
      const nextValue = !previousValue
      try {
        localStorage.setItem(storageKey, String(nextValue))
      } catch {
        // Ignore storage errors in restricted or server-rendered environments.
      }
      return nextValue
    })
  }, [storageKey])

  return [enabled, toggle]
}
