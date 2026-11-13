import { memo, useCallback, useEffect, useLayoutEffect, useRef } from 'react'

const TEXTAREA_LINE_HEIGHT = 20 // 1.25rem = 20px (leading-5)
const TEXTAREA_VERTICAL_PAD = 8 // py-[3px] (6px) + border (2px) = 8px; total 1-line height = 28px (h-7)
const SINGLE_LINE_TEXT_THRESHOLD = 15

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

  const adjustHeight = useCallback(() => {
    const textareaElement = textareaRef.current
    if (!textareaElement) return

    const minHeight = TEXTAREA_LINE_HEIGHT + TEXTAREA_VERTICAL_PAD
    const currentText = textareaElement.value
    if (currentText.length <= SINGLE_LINE_TEXT_THRESHOLD && !currentText.includes('\n')) {
      const targetSingleLineHeight = `${minHeight}px`
      if (textareaElement.style.height !== targetSingleLineHeight) {
        textareaElement.style.height = targetSingleLineHeight
      }
      if (textareaElement.style.overflowY !== 'hidden') {
        textareaElement.style.overflowY = 'hidden'
      }
      return
    }

    const maxHeight = TEXTAREA_LINE_HEIGHT * maxLines + TEXTAREA_VERTICAL_PAD
    textareaElement.style.height = 'auto'
    const borderHeight = Math.max(textareaElement.offsetHeight - textareaElement.clientHeight, 2)
    const scrollAndBorderHeight = textareaElement.scrollHeight + borderHeight
    const targetHeight = Math.min(Math.max(scrollAndBorderHeight, minHeight), maxHeight)
    textareaElement.style.height = `${targetHeight}px`
    textareaElement.style.overflowY = scrollAndBorderHeight > maxHeight ? 'auto' : 'hidden'
  }, [maxLines])

  // Synchronously measure and size on value change (including initial mount)
  useLayoutEffect(() => {
    adjustHeight()
  }, [value, adjustHeight])

  // Recalculate on container width changes using ResizeObserver, falling back to window resize
  useEffect(() => {
    const textareaElement = textareaRef.current
    if (!textareaElement) return

    if (typeof ResizeObserver !== 'undefined') {
      let lastWidth = textareaElement.getBoundingClientRect().width
      const observer = new ResizeObserver((entries) => {
        for (const entry of entries) {
          const newWidth = entry.contentRect.width
          if (newWidth !== lastWidth) {
            lastWidth = newWidth
            adjustHeight()
          }
        }
      })
      observer.observe(textareaElement)
      return () => observer.disconnect()
    }

    const handleResize = () => {
      adjustHeight()
    }
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [adjustHeight])

  return (
    <textarea
      ref={textareaRef}
      rows={1}
      spellCheck={false}
      autoComplete="off"
      value={value}
      placeholder={placeholder}
      onFocus={onFocus}
      onChange={(event) => onChange(event.target.value)}
      onBlur={(event) => onBlur(event.target.value)}
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
