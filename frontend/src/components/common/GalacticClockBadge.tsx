import { useEffect, useState } from 'react'
import { nowGalacticTime } from '../../utils/galacticTime'

/**
 * GalacticClockBadgeProps defines optional styling overrides for the live
 * Galactic Standard Time badge.
 */
export interface GalacticClockBadgeProps {
  className?: string
}

/**
 * Isolated, self-updating Galactic Standard Time badge.
 * Updating here prevents 1Hz root re-renders of App, ExpeditionPlanner, and StatsSidebar.
 */
export function GalacticClockBadge({ className = '' }: GalacticClockBadgeProps) {
  const [time, setTime] = useState(() => nowGalacticTime())
  useEffect(() => {
    const clockTimer = setInterval(() => setTime(nowGalacticTime()), 1000)
    return () => clearInterval(clockTimer)
  }, [])
  return (
    <div
      className={`px-1.5 py-0.5 bg-[#081318] border border-[#16485c] font-mono-tabular text-[9px] font-semibold text-[#00D2FF] tracking-normal leading-tight select-none ${className}`}
      title="Galactic Standard Time (+1286 years)"
    >
      {time}
    </div>
  )
}

