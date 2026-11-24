// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it } from 'vitest'
import { createBusyTaskTracker, useBusyTasks, type UseBusyTasksResult } from './useBusyTasks'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

describe('createBusyTaskTracker', () => {
  it('preserves active task message across out-of-order completions and emits null only when empty', () => {
    const busyHistory: Array<string | null> = []
    const tracker = createBusyTaskTracker((busyMessage) => busyHistory.push(busyMessage))

    // Later task settles before earlier task.
    const endFirst = tracker.beginTask('UPDATING WAYPOINT')
    const endSecond = tracker.beginTask('UPDATING REGION')
    endSecond()
    endSecond() // Idempotent repeat call must be a no-op.
    endFirst()

    expect(busyHistory).toEqual([
      'UPDATING WAYPOINT',
      'UPDATING REGION',
      'UPDATING WAYPOINT',
      null,
    ])

    // Earlier task settles before later task.
    busyHistory.length = 0
    const endThird = tracker.beginTask('SWITCHING FLIGHT MODE')
    const endFourth = tracker.beginTask('ADDING WAYPOINT')
    endThird()
    endFourth()

    expect(busyHistory).toEqual([
      'SWITCHING FLIGHT MODE',
      'ADDING WAYPOINT',
      'ADDING WAYPOINT',
      null,
    ])
  })
})

describe('useBusyTasks', () => {
  it('coordinates initial boot status, token-scoped tasks, and setBusyAction without clobbering', () => {
    let latestResult!: UseBusyTasksResult
    function HookProbe() {
      latestResult = useBusyTasks('INITIALIZING TERMINAL')
      return null
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      act(() => {
        root.render(createElement(HookProbe))
      })
      expect(latestResult.busyAction).toBe('INITIALIZING TERMINAL')

      let endBootTask!: () => void
      act(() => {
        endBootTask = latestResult.beginBusyTask('INITIALIZING TERMINAL')
        latestResult.setBusyAction('PLOTTING SPANSH CARRIER ROUTE')
      })
      expect(latestResult.busyAction).toBe('PLOTTING SPANSH CARRIER ROUTE')

      act(() => {
        latestResult.setBusyAction(null)
      })
      expect(latestResult.busyAction).toBe('INITIALIZING TERMINAL')

      act(() => {
        endBootTask()
      })
      expect(latestResult.busyAction).toBeNull()
    } finally {
      act(() => {
        root.unmount()
      })
      container.remove()
    }
  })
})
