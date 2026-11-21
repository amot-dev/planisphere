/**
 * StatusBadgeProps defines the lifecycle state displayed in the expedition header badge.
 */
export interface StatusBadgeProps {
  status: string
}

/**
 * StatusBadge renders the high-level expedition lifecycle state (Planning / Active / Completed).
 */
export function StatusBadge({ status }: StatusBadgeProps) {
  if (status === 'active') {
    return (
      <span className="inline-flex items-center gap-2 px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.14em] bg-ed-amber-tint text-ed-amber border border-ed-amber border-l-3">
        <span className="w-2 h-2 bg-ed-amber animate-pulse" />
        ACTIVE EXPEDITION
      </span>
    )
  }
  if (status === 'completed') {
    return (
      <span className="inline-flex items-center gap-1.5 px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.14em] bg-ed-cyan-tint text-ed-cyan border border-ed-cyan border-l-3">
        COMPLETED
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1.5 px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.14em] bg-ed-control text-ed-text-main border border-ed-border-strong border-l-3 border-l-ed-cyan">
      PLANNING PHASE
    </span>
  )
}

