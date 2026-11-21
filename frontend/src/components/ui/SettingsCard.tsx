import type { ReactNode } from 'react'

/**
 * SettingsCardProps defines the section heading, optional right-hand header
 * widget, spacing utility class, and inner configuration controls.
 */
export interface SettingsCardProps {
  title: string
  headerRight?: ReactNode
  spacingClassName?: string
  children: ReactNode
}

/**
 * SettingsCard renders a standardized bordered configuration card shared across
 * Terminal Configuration sections.
 */
export function SettingsCard({
  title,
  headerRight,
  spacingClassName = 'space-y-2',
  children,
}: SettingsCardProps) {
  return (
    <div className={`p-3 border border-[#3d2816] bg-[#110c08] ${spacingClassName}`}>
      {headerRight ? (
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
            {title}
          </span>
          {headerRight}
        </div>
      ) : (
        <div className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-[#FF7100]">
          {title}
        </div>
      )}
      {children}
    </div>
  )
}

