import { memo } from 'react'

/**
 * SquareCheckboxProps defines the visual appearance, checked state, and
 * interaction callbacks for the cockpit HUD square checkbox component.
 */
export interface SquareCheckboxProps {
  checked?: boolean | 'partial'
  onChange?: (checked: boolean) => void
  color?: string
  bgFill?: string
  size?: 'xs' | 'sm' | 'md' | 'lg'
  disabled?: boolean
  className?: string
  title?: string
  'aria-label'?: string
}

const sizeClasses = {
  xs: 'w-3 h-3',
  sm: 'w-3.5 h-3.5',
  md: 'w-4 h-4',
  lg: 'w-5 h-5',
}

/**
 * Sci-Fi HUD Cockpit Square Checkbox.
 *
 * Design:
 * - Checked (true): Outer outline border with a prominent filled inner square.
 * - Partial ('partial'): Outer outline border with a horizontally-aligned rectangle (more space above/below).
 * - Unchecked (false): Outer outline border with empty interior.
 * No checkmark icons are used.
 */
export const SquareCheckbox = memo(function SquareCheckbox({
  checked = false,
  onChange,
  color,
  bgFill = '#050506',
  size = 'md',
  disabled = false,
  className = '',
  title,
  'aria-label': ariaLabel,
}: SquareCheckboxProps) {
  let borderColor = '#3d2816'
  let fillColor = 'transparent'

  if (checked === true) {
    borderColor = color || '#FF7100'
    fillColor = color || '#FF7100'
  } else if (checked === 'partial') {
    borderColor = color || '#00D2FF'
    fillColor = color || '#00D2FF'
  }

  const isInteractive = Boolean(onChange && !disabled)

  return (
    <span
      className={`relative inline-flex shrink-0 items-center justify-center select-none ${
        disabled ? 'opacity-40 cursor-not-allowed' : isInteractive ? 'cursor-pointer' : ''
      } ${className}`}
      title={title}
    >
      {onChange && (
        <input
          type="checkbox"
          className="absolute inset-0 opacity-0 cursor-pointer w-full h-full z-10 disabled:cursor-not-allowed"
          checked={checked === true}
          disabled={disabled}
          onChange={(event) => {
            if (!disabled && onChange) {
              onChange(event.target.checked)
            }
          }}
          aria-label={ariaLabel}
        />
      )}
      <svg
        viewBox="0 0 16 16"
        className={`${sizeClasses[size]} shrink-0 pointer-events-none`}
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        {/* Outer outline border */}
        <rect
          x="1"
          y="1"
          width="14"
          height="14"
          fill={bgFill}
          stroke={borderColor}
          strokeWidth="1.5"
        />
        {/* Full square when checked */}
        {checked === true && (
          <rect x="4" y="4" width="8" height="8" fill={fillColor} />
        )}
        {/* Horizontally-aligned rectangle when partial */}
        {checked === 'partial' && (
          <rect x="4" y="6.5" width="8" height="3" fill={fillColor} />
        )}
      </svg>
    </span>
  )
})

