/**
 * StatRowProps defines the label, formatted value, optional subtitle, and
 * accent color for a sidebar telemetry row.
 */
export interface StatRowProps {
  label: string
  value: string
  sub?: string
  subClass?: string
  accent?: 'amber' | 'cyan'
  valueClass?: string
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
}: StatRowProps) {
  return (
    <div className="px-2.5 py-2 flex items-center justify-between gap-2">
      <div className="min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-semibold text-[#F4ECE4] truncate">{label}</span>
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

