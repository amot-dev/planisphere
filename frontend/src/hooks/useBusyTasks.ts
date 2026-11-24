import { useCallback, useMemo, useRef, useState } from 'react'

/**
 * BusyTaskTracker coordinates concurrent in-flight operations using monotonic
 * tokens so overlapping tasks never prematurely clear the busy indicator.
 */
export interface BusyTaskTracker {
  beginTask: (message: string) => () => void
}

/**
 * UseBusyTasksResult exposes the current busy banner message alongside both
 * token-scoped task tracking and direct setter compatibility.
 */
export interface UseBusyTasksResult {
  busyAction: string | null
  beginBusyTask: (message: string) => () => void
  setBusyAction: (busyMessage: string | null) => void
}

/**
 * createBusyTaskTracker tracks concurrent tasks in insertion order and emits
 * the latest still-active task message when an individual token finishes, or
 * null only after every active token has completed.
 */
export function createBusyTaskTracker(
  onBusyChange: (busyMessage: string | null) => void,
): BusyTaskTracker {
  const activeTasks = new Map<number, string>()
  let nextTokenId = 0

  return {
    beginTask(message: string): () => void {
      const tokenId = ++nextTokenId
      activeTasks.set(tokenId, message)
      onBusyChange(message)

      return () => {
        if (!activeTasks.delete(tokenId)) {
          return
        }
        let latestActiveMessage: string | null = null
        for (const activeMessage of activeTasks.values()) {
          latestActiveMessage = activeMessage
        }
        onBusyChange(latestActiveMessage)
      }
    },
  }
}

/**
 * useBusyTasks manages top-level busy status state with token-scoped task
 * tracking while preserving setBusyAction compatibility for child components.
 */
export function useBusyTasks(initialBootMessage: string | null = null): UseBusyTasksResult {
  const [busyAction, setBusyActionState] = useState<string | null>(initialBootMessage)
  const latestTrackedMessageRef = useRef<string | null>(null)
  const directTaskEndRef = useRef<(() => void) | null>(null)

  const tracker = useMemo(
    () =>
      createBusyTaskTracker((nextMessage) => {
        latestTrackedMessageRef.current = nextMessage
        setBusyActionState(nextMessage)
      }),
    [],
  )

  const beginBusyTask = useCallback((message: string) => tracker.beginTask(message), [tracker])

  const setBusyAction = useCallback(
    (busyMessage: string | null) => {
      if (busyMessage === null) {
        const endDirectTask = directTaskEndRef.current
        directTaskEndRef.current = null
        if (endDirectTask) {
          endDirectTask()
        } else {
          setBusyActionState(latestTrackedMessageRef.current)
        }
        return
      }
      const previousEndDirectTask = directTaskEndRef.current
      directTaskEndRef.current = tracker.beginTask(busyMessage)
      previousEndDirectTask?.()
    },
    [tracker],
  )

  return { busyAction, beginBusyTask, setBusyAction }
}
