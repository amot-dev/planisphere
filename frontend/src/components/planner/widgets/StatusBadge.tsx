/**
 * StatusBadgeProps defines the lifecycle state and optional arming timestamp
 * displayed in the expedition header badge.
 */
export interface StatusBadgeProps {
  status: string
  armedAt: string
}

/**
 * StatusBadge renders the high-level expedition lifecycle state (Planning / Active / Completed).
 */
export function StatusBadge({ status, armedAt }: StatusBadgeProps) {
  if (status === 'active') {
    return (
      <span className="inline-flex items-center gap-2 px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.14em] bg-[#211307] text-[#FF7100] border border-[#FF7100] border-l-3">
        <span className="w-2 h-2 bg-[#FF7100] animate-pulse" />
        {armedAt ? 'ARMED // AWAITING WAYPOINT #1' : 'ACTIVE EXPEDITION'}
      </span>
    )
  }
  if (status === 'completed') {
    return (
      <span className="inline-flex items-center gap-1.5 px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.14em] bg-[#08151c] text-[#00D2FF] border border-[#00D2FF] border-l-3">
        COMPLETED
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1.5 px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.14em] bg-[#16100b] text-[#F4ECE4] border border-[#472e19] border-l-3 border-l-[#00D2FF]">
      PLANNING PHASE
    </span>
  )
}

