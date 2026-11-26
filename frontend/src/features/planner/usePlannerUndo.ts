import { useCallback, useEffect, useRef, useState } from 'react'
import { RedoExpedition, UndoExpedition } from '../../api/backend'
import type { BusyTaskTracker } from '../../hooks/useBusyTasks'

const UNDO_TOAST_DURATION_MS = 4500

/** PlannerUndoAction identifies the session history direction requested by a shortcut. */
export type PlannerUndoAction = 'undo' | 'redo'

/** PlannerUndoKeyEvent captures keyboard fields inspected for undo and redo shortcuts. */
export interface PlannerUndoKeyEvent {
  key: string
  ctrlKey: boolean
  metaKey: boolean
  shiftKey: boolean
  altKey: boolean
  target?: EventTarget | null
}

/** UsePlannerUndoOptions configures scope, suppression, and busy tracking. */
export interface UsePlannerUndoOptions {
  expeditionId: number
  disabled: boolean
  busyTracker: BusyTaskTracker
}

/** UsePlannerUndoResult exposes the active undo status toast and its dismiss callback. */
export interface UsePlannerUndoResult {
  undoStatusToast: string | null
  dismissUndoStatusToast: () => void
}

/**
 * isEditableElement returns true for inputs, textareas, selects, and
 * content-editable elements where native text undo must retain priority.
 */
export function isEditableElement(element: EventTarget | null): boolean {
  if (!element || !(element instanceof Element)) return false
  const tagName = element.tagName.toUpperCase()
  if (tagName === 'INPUT' || tagName === 'TEXTAREA' || tagName === 'SELECT') return true
  if ('isContentEditable' in element && Boolean((element as HTMLElement).isContentEditable)) {
    return true
  }
  const contentEditableHost = element.closest('[contenteditable]')
  if (!contentEditableHost) return false
  const contentEditableAttr = contentEditableHost.getAttribute('contenteditable')
  return contentEditableAttr !== null && contentEditableAttr.toLowerCase() !== 'false'
}

/**
 * resolvePlannerUndoAction maps a keyboard event to an undo or redo action
 * when shortcuts are enabled outside editable elements and active modals.
 */
export function resolvePlannerUndoAction(
  event: PlannerUndoKeyEvent,
  disabled: boolean,
  expeditionId: number,
  activeElement: Element | null,
): PlannerUndoAction | null {
  if (disabled || expeditionId <= 0 || event.altKey || (!event.ctrlKey && !event.metaKey)) {
    return null
  }
  const normalizedKey = event.key.toLowerCase()
  let matchedAction: PlannerUndoAction | null = null
  if (normalizedKey === 'z' && !event.shiftKey) {
    matchedAction = 'undo'
  } else if (
    (normalizedKey === 'y' && !event.shiftKey) ||
    (normalizedKey === 'z' && event.shiftKey)
  ) {
    matchedAction = 'redo'
  }
  if (!matchedAction) return null
  if (isEditableElement(activeElement) || isEditableElement(event.target ?? null)) return null
  return matchedAction
}

/**
 * usePlannerUndo registers global keyboard-only undo and redo listeners for
 * the active expedition, serializes in-flight requests, and manages the
 * ephemeral bottom-right confirmation toast.
 */
export function usePlannerUndo(options: UsePlannerUndoOptions): UsePlannerUndoResult {
  const { expeditionId } = options
  const [undoStatusToast, setUndoStatusToast] = useState<string | null>(null)
  const toastTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const inFlightRef = useRef(false)
  const mountedRef = useRef(true)
  const optionsRef = useRef(options)
  optionsRef.current = options

  const clearToastTimer = useCallback(() => {
    if (toastTimerRef.current !== null) {
      clearTimeout(toastTimerRef.current)
      toastTimerRef.current = null
    }
  }, [])

  const dismissUndoStatusToast = useCallback(() => {
    clearToastTimer()
    setUndoStatusToast(null)
  }, [clearToastTimer])

  const showUndoStatusToast = useCallback(
    (statusMessage: string) => {
      clearToastTimer()
      setUndoStatusToast(statusMessage)
      toastTimerRef.current = setTimeout(() => {
        toastTimerRef.current = null
        if (mountedRef.current) setUndoStatusToast(null)
      }, UNDO_TOAST_DURATION_MS)
    },
    [clearToastTimer],
  )

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      clearToastTimer()
    }
  }, [clearToastTimer])

  useEffect(() => {
    dismissUndoStatusToast()
  }, [dismissUndoStatusToast, expeditionId])

  useEffect(() => {
    async function executeUndoAction(action: PlannerUndoAction, requestExpeditionId: number) {
      inFlightRef.current = true
      const isRedo = action === 'redo'
      const endBusyTask = optionsRef.current.busyTracker.beginTask(
        isRedo ? 'REDOING CHANGE' : 'UNDOING CHANGE',
      )
      try {
        const actionResult = isRedo
          ? await RedoExpedition(requestExpeditionId)
          : await UndoExpedition(requestExpeditionId)
        if (!mountedRef.current || optionsRef.current.expeditionId !== requestExpeditionId) return
        if (!actionResult?.applied) return
        const actionLabel = (actionResult.label || 'Change').toUpperCase()
        showUndoStatusToast(
          isRedo
            ? `REDONE: ${actionLabel} · CTRL-Z TO UNDO`
            : `UNDONE: ${actionLabel} · CTRL-Y TO REDO`,
        )
      } catch (undoError) {
        console.error(`Failed to ${action} expedition change:`, undoError)
      } finally {
        inFlightRef.current = false
        endBusyTask()
      }
    }

    function handleKeyDown(event: KeyboardEvent) {
      const currentOptions = optionsRef.current
      const action = resolvePlannerUndoAction(
        event,
        currentOptions.disabled,
        currentOptions.expeditionId,
        document.activeElement,
      )
      if (!action) return
      if (document.querySelector('[role="dialog"][aria-modal="true"]') !== null) return
      // Prevent default even while an RPC is in flight so held repeats do not leak to WebView undo.
      event.preventDefault()
      if (inFlightRef.current) return
      void executeUndoAction(action, currentOptions.expeditionId)
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [showUndoStatusToast])

  return { undoStatusToast, dismissUndoStatusToast }
}
