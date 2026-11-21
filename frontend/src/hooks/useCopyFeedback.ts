import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * UseCopyFeedbackResult exposes the active copied key indicator and a
 * clipboard copy trigger with automatic fallback and timeout reset.
 */
export interface UseCopyFeedbackResult<KeyType extends string | number> {
  copiedKey: KeyType | null
  copyWithFeedback: (key: KeyType, rawText: string) => void
}

/**
 * useCopyFeedback copies text to the system clipboard (falling back to a
 * temporary textarea selection when Clipboard API is unavailable) and sets
 * copiedKey for resetDelayMilliseconds.
 */
export function useCopyFeedback<KeyType extends string | number = string>(
  resetDelayMilliseconds = 1500,
): UseCopyFeedbackResult<KeyType> {
  const [copiedKey, setCopiedKey] = useState<KeyType | null>(null)
  const resetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (resetTimerRef.current) {
        clearTimeout(resetTimerRef.current)
      }
    }
  }, [])

  const copyWithFeedback = useCallback(
    (key: KeyType, rawText: string) => {
      if (!rawText) return
      if (navigator?.clipboard?.writeText) {
        navigator.clipboard.writeText(rawText).catch(console.error)
      } else if (typeof document !== 'undefined') {
        const textareaElement = document.createElement('textarea')
        textareaElement.value = rawText
        document.body.appendChild(textareaElement)
        textareaElement.select()
        document.execCommand('copy')
        document.body.removeChild(textareaElement)
      }
      if (resetTimerRef.current) {
        clearTimeout(resetTimerRef.current)
      }
      setCopiedKey(key)
      resetTimerRef.current = setTimeout(() => {
        setCopiedKey((currentKey) => (currentKey === key ? null : currentKey))
        resetTimerRef.current = null
      }, resetDelayMilliseconds)
    },
    [resetDelayMilliseconds],
  )

  return { copiedKey, copyWithFeedback }
}

