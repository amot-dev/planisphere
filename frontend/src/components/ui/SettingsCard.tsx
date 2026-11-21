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
    <div className={`p-3 border border-ed-border bg-ed-card ${spacingClassName}`}>
      {headerRight ? (
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-ed-amber">
            {title}
          </span>
          {headerRight}
        </div>
      ) : (
        <div className="text-[11px] font-extrabold uppercase tracking-[0.14em] text-ed-amber">
          {title}
        </div>
      )}
      {children}
    </div>
  )
}

