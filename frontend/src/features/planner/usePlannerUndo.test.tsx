// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { app } from '../../api/models'
import { ModalShell } from '../../components/ui/ModalShell'
import { createBusyTaskTracker } from '../../hooks/useBusyTasks'

const mockUndoExpedition = vi.fn()
const mockRedoExpedition = vi.fn()

vi.mock('../../api/backend', () => ({
  UndoExpedition: (...args: unknown[]) => mockUndoExpedition(...args),
  RedoExpedition: (...args: unknown[]) => mockRedoExpedition(...args),
}))

import {
  isEditableElement,
  resolvePlannerUndoAction,
  usePlannerUndo,
  type UsePlannerUndoResult,
} from './usePlannerUndo'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

function dispatchShortcut(options: KeyboardEventInit): KeyboardEvent {
  const keyboardEvent = new KeyboardEvent('keydown', {
    bubbles: true,
    cancelable: true,
    ...options,
  })
  window.dispatchEvent(keyboardEvent)
  return keyboardEvent
}

describe('usePlannerUndo pure shortcut resolution helpers', () => {
  it('handles nested and disabled contentEditable edges, null targets, Ctrl+Shift+Z, and non-matching chords', () => {
    const editableDiv = document.createElement('div')
    editableDiv.setAttribute('contenteditable', 'true')
    const editableChild = document.createElement('span')
    editableDiv.appendChild(editableChild)
    const disabledEditableDiv = document.createElement('div')
    disabledEditableDiv.setAttribute('contenteditable', 'false')

    expect(isEditableElement(editableChild)).toBe(true)
    expect(isEditableElement(disabledEditableDiv)).toBe(false)
    expect(isEditableElement(null)).toBe(false)

    const bodyElement = document.body
    const baseEvent = { ctrlKey: false, metaKey: false, shiftKey: false, altKey: false }
    expect(
      resolvePlannerUndoAction(
        { ...baseEvent, key: 'Z', ctrlKey: true, shiftKey: true },
        false,
        4,
        bodyElement,
      ),
    ).toBe('redo')
    expect(
      resolvePlannerUndoAction(
        { ...baseEvent, key: 'y', ctrlKey: true, shiftKey: true },
        false,
        4,
        bodyElement,
      ),
    ).toBeNull()
    expect(resolvePlannerUndoAction({ ...baseEvent, key: 'z' }, false, 4, bodyElement)).toBeNull()
  })
})

describe('usePlannerUndo keyboard hook lifecycle and guards', () => {
  afterEach(() => {
    vi.resetAllMocks()
    vi.useRealTimers()
  })

  function mountHookHarness(initialProps: {
    expeditionId: number
    disabled?: boolean
  }) {
    const busyMessages: Array<string | null> = []
    const busyTracker = createBusyTaskTracker((message) => busyMessages.push(message))
    let latestResult!: UsePlannerUndoResult

    function Harness({
      expeditionId,
      disabled = false,
    }: {
      expeditionId: number
      disabled?: boolean
    }) {
      latestResult = usePlannerUndo({
        expeditionId,
        disabled,
        busyTracker,
      })
      return null
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    act(() => {
      root.render(
        createElement(Harness, {
          expeditionId: initialProps.expeditionId,
          disabled: initialProps.disabled,
        }),
      )
    })

    return {
      busyMessages,
      getResult: () => latestResult,
      rerender: (nextProps: { expeditionId: number; disabled?: boolean }) => {
        act(() => {
          root.render(createElement(Harness, nextProps))
        })
      },
      unmount: () => {
        act(() => root.unmount())
        container.remove()
      },
    }
  }

  it('handles Ctrl+Z/Cmd+Z undo, auto-dismiss after 4500ms, manual dismiss, and redo shortcuts', async () => {
    vi.useFakeTimers()
    mockUndoExpedition
      .mockResolvedValueOnce(
        app.UndoActionResult.createFrom({
          applied: true,
          label: 'Delete Waypoint',
          canUndo: false,
          canRedo: true,
        }),
      )
      .mockResolvedValueOnce(
        app.UndoActionResult.createFrom({
          applied: true,
          label: '',
          canUndo: false,
          canRedo: true,
        }),
      )
    mockRedoExpedition
      .mockResolvedValueOnce(
        app.UndoActionResult.createFrom({
          applied: true,
          label: 'Delete Waypoint',
          canUndo: true,
          canRedo: false,
        }),
      )
      .mockResolvedValueOnce(
        app.UndoActionResult.createFrom({
          applied: true,
          label: 'Move Waypoint',
          canUndo: true,
          canRedo: false,
        }),
      )

    const harness = mountHookHarness({ expeditionId: 7 })
    try {
      let undoEvent!: KeyboardEvent
      await act(async () => {
        undoEvent = dispatchShortcut({ key: 'z', ctrlKey: true })
        await Promise.resolve()
        await Promise.resolve()
      })
      expect(undoEvent.defaultPrevented).toBe(true)
      expect(mockUndoExpedition).toHaveBeenCalledWith(7)
      expect(harness.busyMessages).toEqual(['UNDOING CHANGE', null])
      expect(harness.getResult().undoStatusToast).toBe('UNDONE: DELETE WAYPOINT · CTRL-Y TO REDO')

      // Timer stays active just under 4500ms and clears at 4500ms.
      act(() => {
        vi.advanceTimersByTime(4499)
      })
      expect(harness.getResult().undoStatusToast).toBe('UNDONE: DELETE WAYPOINT · CTRL-Y TO REDO')
      act(() => {
        vi.advanceTimersByTime(1)
      })
      expect(harness.getResult().undoStatusToast).toBeNull()

      // Cmd+Z with empty label falls back to CHANGE and supports manual dismiss.
      await act(async () => {
        dispatchShortcut({ key: 'z', metaKey: true })
        await Promise.resolve()
      })
      expect(harness.getResult().undoStatusToast).toBe('UNDONE: CHANGE · CTRL-Y TO REDO')
      act(() => {
        harness.getResult().dismissUndoStatusToast()
      })
      expect(harness.getResult().undoStatusToast).toBeNull()

      // Ctrl+Y triggers RedoExpedition and resets timer on subsequent Cmd+Shift+Z redo.
      await act(async () => {
        dispatchShortcut({ key: 'y', ctrlKey: true })
        await Promise.resolve()
      })
      expect(mockRedoExpedition).toHaveBeenNthCalledWith(1, 7)
      expect(harness.getResult().undoStatusToast).toBe('REDONE: DELETE WAYPOINT · CTRL-Z TO UNDO')

      act(() => {
        vi.advanceTimersByTime(3000)
      })
      await act(async () => {
        dispatchShortcut({ key: 'Z', metaKey: true, shiftKey: true })
        await Promise.resolve()
      })
      expect(mockRedoExpedition).toHaveBeenNthCalledWith(2, 7)
      expect(harness.getResult().undoStatusToast).toBe('REDONE: MOVE WAYPOINT · CTRL-Z TO UNDO')

      act(() => {
        vi.advanceTimersByTime(3000)
      })
      expect(harness.getResult().undoStatusToast).toBe('REDONE: MOVE WAYPOINT · CTRL-Z TO UNDO')
      act(() => {
        vi.advanceTimersByTime(1500)
      })
      expect(harness.getResult().undoStatusToast).toBeNull()
    } finally {
      harness.unmount()
    }
  })

  it('yields strictly to focused editable elements, open ModalShell dialogs, disabled state, invalid expedition, or Alt held', async () => {
    const harness = mountHookHarness({ expeditionId: 5 })
    const inputElement = document.createElement('input')
    const textareaElement = document.createElement('textarea')
    const selectElement = document.createElement('select')
    const editableDiv = document.createElement('div')
    editableDiv.setAttribute('contenteditable', 'true')
    editableDiv.tabIndex = 0
    document.body.append(inputElement, textareaElement, selectElement, editableDiv)

    const modalContainer = document.createElement('div')
    document.body.appendChild(modalContainer)
    const modalRoot = createRoot(modalContainer)

    try {
      for (const editableControl of [inputElement, textareaElement, selectElement, editableDiv]) {
        editableControl.focus()
        const shortcutEvent = dispatchShortcut({ key: 'z', ctrlKey: true })
        expect(shortcutEvent.defaultPrevented).toBe(false)
      }
      editableDiv.blur()

      // An open ModalShell (such as sidebar-owned CarrierConfigModal) suppresses Ctrl-Z even when disabled is false.
      act(() => {
        modalRoot.render(
          createElement(ModalShell, {
            title: 'Expedition Carrier Configuration',
            children: 'Modal body',
          }),
        )
      })
      expect(dispatchShortcut({ key: 'z', ctrlKey: true }).defaultPrevented).toBe(false)
      act(() => {
        modalRoot.unmount()
      })

      expect(dispatchShortcut({ key: 'z', ctrlKey: true, altKey: true }).defaultPrevented).toBe(false)

      harness.rerender({ expeditionId: 5, disabled: true })
      expect(dispatchShortcut({ key: 'z', ctrlKey: true }).defaultPrevented).toBe(false)

      harness.rerender({ expeditionId: 0, disabled: false })
      expect(dispatchShortcut({ key: 'z', ctrlKey: true }).defaultPrevented).toBe(false)

      expect(mockUndoExpedition).not.toHaveBeenCalled()
      expect(mockRedoExpedition).not.toHaveBeenCalled()
    } finally {
      act(() => modalRoot.unmount())
      modalContainer.remove()
      inputElement.remove()
      textareaElement.remove()
      selectElement.remove()
      editableDiv.remove()
      harness.unmount()
    }
  })

  it('serializes overlapping key repeats, clears toast on expedition switch, and ignores stale resolutions', async () => {
    let resolveFirstUndo!: (result: app.UndoActionResult) => void
    mockUndoExpedition
      .mockImplementationOnce(
        () =>
          new Promise<app.UndoActionResult>((resolve) => {
            resolveFirstUndo = resolve
          }),
      )
      .mockResolvedValueOnce(
        app.UndoActionResult.createFrom({
          applied: true,
          label: 'Edit Notes',
          canUndo: false,
          canRedo: true,
        }),
      )

    const harness = mountHookHarness({ expeditionId: 1 })
    try {
      act(() => {
        dispatchShortcut({ key: 'z', ctrlKey: true })
        // Second held key-repeat while first RPC is still in flight must be ignored.
        const repeatEvent = dispatchShortcut({ key: 'z', ctrlKey: true })
        expect(repeatEvent.defaultPrevented).toBe(true)
      })
      expect(mockUndoExpedition).toHaveBeenCalledTimes(1)
      expect(harness.busyMessages).toEqual(['UNDOING CHANGE'])

      // Switch expeditions before the first undo finishes: stale result must not trigger toast.
      harness.rerender({ expeditionId: 2 })
      await act(async () => {
        resolveFirstUndo(
          app.UndoActionResult.createFrom({
            applied: true,
            label: 'Start Carrier Route',
            canUndo: false,
            canRedo: true,
          }),
        )
        await Promise.resolve()
      })
      expect(harness.busyMessages).toEqual(['UNDOING CHANGE', null])
      expect(harness.getResult().undoStatusToast).toBeNull()

      // Undo on expedition 2 shows toast, then switching to expedition 3 clears it immediately.
      await act(async () => {
        dispatchShortcut({ key: 'z', ctrlKey: true })
        await Promise.resolve()
      })
      expect(mockUndoExpedition).toHaveBeenNthCalledWith(2, 2)
      expect(harness.getResult().undoStatusToast).toBe('UNDONE: EDIT NOTES · CTRL-Y TO REDO')

      harness.rerender({ expeditionId: 3 })
      expect(harness.getResult().undoStatusToast).toBeNull()
    } finally {
      harness.unmount()
    }
  })
})
