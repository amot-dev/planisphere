// @vitest-environment happy-dom
import { act } from 'react'
import { describe, expect, it } from 'vitest'
import { renderHook } from '../test/render'
import { createBusyTaskTracker, useBusyTasks } from './useBusyTasks'

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
    const hook = renderHook(() => useBusyTasks('INITIALIZING TERMINAL'))

    expect(hook.getResult().busyAction).toBe('INITIALIZING TERMINAL')

    let endBootTask!: () => void
    act(() => {
      endBootTask = hook.getResult().beginBusyTask('INITIALIZING TERMINAL')
      hook.getResult().setBusyAction('PLOTTING SPANSH CARRIER ROUTE')
    })
    expect(hook.getResult().busyAction).toBe('PLOTTING SPANSH CARRIER ROUTE')

    act(() => {
      hook.getResult().setBusyAction(null)
    })
    expect(hook.getResult().busyAction).toBe('INITIALIZING TERMINAL')

    act(() => {
      endBootTask()
    })
    expect(hook.getResult().busyAction).toBeNull()
  })
})
