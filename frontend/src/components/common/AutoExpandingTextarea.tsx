import { memo, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

const TEXTAREA_LINE_HEIGHT = 20 // 1.25rem = 20px (leading-5)
const TEXTAREA_VERTICAL_PAD = 8 // py-[3px] (6px) + border (2px) = 8px; total 1-line height = 28px (h-7)

/**
 * AutoExpandingTextareaProps defines the controlled text value, line bounds,
 * and focus/blur callbacks for a single-to-multi-line spreadsheet cell input.
 */
export interface AutoExpandingTextareaProps {
  value: string
  placeholder: string
  onChange: (value: string) => void
  onBlur: (value: string) => void
  onFocus?: () => void
  className?: string
  maxLines?: number
}

/**
 * AutoExpandingTextarea renders a borderless spreadsheet textarea that starts
 * at a single 28px row height and expands up to maxLines on wrap or newlines.
 */
export const AutoExpandingTextarea = memo(function AutoExpandingTextarea({
  value,
  placeholder,
  onChange,
  onBlur,
  onFocus,
  className = '',
  maxLines = 3,
}: AutoExpandingTextareaProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const [, setIsFocused] = useState(false)

  const adjustHeight = useCallback(() => {
    const textareaElement = textareaRef.current
    if (!textareaElement) return

    const minHeight = TEXTAREA_LINE_HEIGHT + TEXTAREA_VERTICAL_PAD
    const maxHeight = TEXTAREA_LINE_HEIGHT * maxLines + TEXTAREA_VERTICAL_PAD

    textareaElement.style.height = 'auto'
    const borderHeight = Math.max(textareaElement.offsetHeight - textareaElement.clientHeight, 2)
    const targetHeight = Math.min(
      Math.max(textareaElement.scrollHeight + borderHeight, minHeight),
      maxHeight,
    )
    textareaElement.style.height = `${targetHeight}px`
    textareaElement.style.overflowY =
      textareaElement.scrollHeight + borderHeight > maxHeight ? 'auto' : 'hidden'
  }, [maxLines])

  // Synchronously measure and size on value change (including initial mount)
  useLayoutEffect(() => {
    adjustHeight()
  }, [value, adjustHeight])

  // Recalculate on viewport/container resize so wrapping reflows correctly
  useEffect(() => {
    const textareaElement = textareaRef.current
    if (!textareaElement) return

    let lastWidth = textareaElement.getBoundingClientRect().width

    const handleResize = () => {
      adjustHeight()
    }

    window.addEventListener('resize', handleResize)

    let observer: ResizeObserver | null = null
    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver((entries) => {
        for (const entry of entries) {
          const newWidth = entry.contentRect.width
          if (newWidth !== lastWidth) {
            lastWidth = newWidth
            adjustHeight()
          }
        }
      })
      observer.observe(textareaElement)
    }

    return () => {
      window.removeEventListener('resize', handleResize)
      observer?.disconnect()
    }
  }, [adjustHeight])

  return (
    <textarea
      ref={textareaRef}
      rows={1}
      spellCheck={false}
      autoComplete="off"
      value={value}
      placeholder={placeholder}
      onFocus={() => {
        setIsFocused(true)
        adjustHeight()
        onFocus?.()
      }}
      onChange={(event) => {
        onChange(event.target.value)
        adjustHeight()
      }}
      onBlur={(event) => {
        setIsFocused(false)
        adjustHeight()
        onBlur(event.target.value)
      }}
      onKeyDown={(event) => {
        if (event.key === 'Enter' && !event.shiftKey) {
          event.preventDefault()
          event.currentTarget.blur()
        }
      }}
      className={`w-full bg-transparent px-1.5 py-[3px] border border-transparent focus:border-[#FF7100] focus:bg-[#050506] focus:outline-none resize-none leading-5 text-xs block min-h-[28px] ${className}`}
    />
  )
})

