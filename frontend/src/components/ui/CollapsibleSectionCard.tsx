import type { ReactNode } from 'react'
import { ChevronRightIcon } from './HudIcons'

/**
 * CollapsibleSectionCardProps defines the header title, toggle handler, optional
 * right-aligned header controls, and body rows for a cockpit sidebar card.
 */
export interface CollapsibleSectionCardProps {
  title: string
  toggleTitle: string
  expanded: boolean
  onToggle?: () => void
  headerActions?: ReactNode
  children: ReactNode
}

/**
 * CollapsibleSectionCard renders a collapsible cockpit telemetry section card
 * with a rotating chevron toggle, standardized dark-bronze header, and divided rows.
 */
export function CollapsibleSectionCard({
  title,
  toggleTitle,
  expanded,
  onToggle,
  headerActions,
  children,
}: CollapsibleSectionCardProps) {
  return (
    <div className="border border-ed-border bg-ed-card-alt overflow-hidden">
      <div
        className={`px-2 py-1.5 bg-ed-header ${
          expanded ? 'border-b border-ed-border' : ''
        } text-[10px] font-extrabold uppercase tracking-[0.14em] text-ed-text-main flex items-center justify-between`}
      >
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={onToggle}
            title={toggleTitle}
            className={`h-5 w-5 flex items-center justify-center hover:bg-ed-hover cursor-pointer ${
              expanded ? 'text-ed-amber' : 'text-ed-text-muted'
            }`}
          >
            <ChevronRightIcon expanded={expanded} />
          </button>
          <span>{title}</span>
        </div>
        {headerActions}
      </div>
      {expanded && <div className="divide-y divide-ed-border-subtle">{children}</div>}
    </div>
  )
}

