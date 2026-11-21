import type { ReactNode } from 'react'

/**
 * ExpandableMetricRowProps defines the summary label, formatted total value,
 * accent theme, expanded state, and child breakdown block for a metric accordion row.
 */
export interface ExpandableMetricRowProps {
  label: string
  value: string
  accent: 'amber' | 'cyan' | 'emerald'
  expanded: boolean
  onToggle?: () => void
  children: ReactNode
}

const ACCENT_TEXT_CLASSES: Record<ExpandableMetricRowProps['accent'], string> = {
  amber: 'text-[#FF7100]',
  cyan: 'text-[#00D2FF]',
  emerald: 'text-[#00E5A3]',
}

/**
 * ExpandableMetricRow renders an interactive metric summary button and
 * conditionally expands its itemized breakdown below when toggled open.
 */
export function ExpandableMetricRow({
  label,
  value,
  accent,
  expanded,
  onToggle,
  children,
}: ExpandableMetricRowProps) {
  const accentTextClass = ACCENT_TEXT_CLASSES[accent]

  return (
    <div className="p-2.5">
      <button
        type="button"
        onClick={onToggle}
        className="w-full flex items-center justify-between gap-2 text-left cursor-pointer"
      >
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="text-xs font-bold uppercase tracking-wider text-[#F4ECE4] truncate">
            {label}
          </span>
          <span className={`text-[10px] ${accentTextClass} shrink-0`}>
            {expanded ? '▾' : '▸'}
          </span>
        </div>
        <span
          className={`font-mono-tabular text-xs font-extrabold ${accentTextClass} whitespace-nowrap shrink-0`}
        >
          {value}
        </span>
      </button>
      {expanded && children}
    </div>
  )
}

