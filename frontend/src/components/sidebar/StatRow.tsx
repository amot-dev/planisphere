/**
 * StatRowProps defines the label, formatted value, optional subtitle, accent
 * color, and inline action button for a sidebar telemetry row.
 */
export interface StatRowProps {
  label: string
  value: string
  sub?: string
  subClass?: string
  accent?: 'amber' | 'cyan'
  valueClass?: string
  actionLabel?: string
  onAction?: () => void
}

/**
 * StatRow renders a compact label-and-value row inside a cockpit telemetry card.
 */
export function StatRow({
  label,
  value,
  sub,
  subClass,
  accent,
  valueClass,
  actionLabel,
  onAction,
}: StatRowProps) {
  return (
    <div className="px-2.5 py-2 flex items-center justify-between gap-2">
      <div className="min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-semibold text-[#F4ECE4] truncate">{label}</span>
          {actionLabel && onAction && (
            <button
              type="button"
              onClick={onAction}
              className="text-[9px] font-mono-tabular font-bold uppercase tracking-wider px-1.5 py-0.5 bg-[#16100b] hover:bg-[#FF7100] text-[#FF7100] hover:text-[#050506] border border-[#472e19] cursor-pointer"
            >
              {actionLabel}
            </button>
          )}
        </div>
        {sub && (
          <div className={`text-[10px] font-mono-tabular ${subClass ? subClass : 'text-[#a6907c]'}`}>
            {sub}
          </div>
        )}
      </div>
      <div
        className={`font-mono-tabular text-xs font-bold shrink-0 ${
          valueClass
            ? valueClass
            : accent === 'amber'
              ? 'text-[#FF7100]'
              : accent === 'cyan'
                ? 'text-[#00D2FF]'
                : 'text-[#F4ECE4]'
        }`}
      >
        {value}
      </div>
    </div>
  )
}

