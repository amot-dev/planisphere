import type { ReactNode } from 'react'

/**
 * ModalShellProps defines the header metadata, accent color, maximum width,
 * close handler, and optional footer for a framed cockpit modal dialog.
 */
export interface ModalShellProps {
  title: string
  subtitle?: string
  badge?: string
  accent?: 'amber' | 'cyan'
  onClose?: () => void
  closeDisabled?: boolean
  closeTitle?: string
  headerLeading?: ReactNode
  bodyClassName?: string
  footer?: ReactNode
  children: ReactNode
  maxWidth?: string
}

/**
 * ModalShell renders the shared full-screen backdrop, framed header bar,
 * scrollable content container, and optional footer bar for cockpit modals.
 */
export function ModalShell({
  title,
  subtitle,
  badge,
  accent = 'amber',
  onClose,
  closeDisabled = false,
  closeTitle,
  headerLeading,
  bodyClassName = 'p-5 overflow-y-auto space-y-4 text-xs',
  footer,
  children,
  maxWidth = 'max-w-xl',
}: ModalShellProps) {
  const isCyanAccent = accent === 'cyan'
  const topBorderClass = isCyanAccent ? 'border-t-[#00D2FF]' : 'border-t-[#FF7100]'
  const titleTextClass = isCyanAccent ? 'text-[#00D2FF]' : 'text-[#FF7100]'

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4">
      <div
        className={`w-full ${maxWidth} max-h-[90vh] flex flex-col border border-[#3d2816] border-t-2 ${topBorderClass} bg-[#0b0907] shadow-2xl overflow-hidden`}
      >
        <div className="px-5 py-3.5 border-b border-[#3d2816] bg-[#140e09] flex items-center justify-between">
          <div className={headerLeading ? 'flex items-center gap-2.5' : undefined}>
            {headerLeading}
            <div>
              <div className="flex items-center gap-2">
                <h3 className={`text-xs font-extrabold uppercase tracking-[0.16em] ${titleTextClass}`}>
                  {title}
                </h3>
                {badge && (
                  <span className="px-1.5 py-0.5 bg-[#08151c] border border-[#00D2FF]/50 font-mono-tabular text-[10px] font-bold text-[#00D2FF]">
                    {badge}
                  </span>
                )}
              </div>
              {subtitle && <p className="text-[11px] text-[#a6907c] mt-0.5">{subtitle}</p>}
            </div>
          </div>
          {onClose && (
            <button
              type="button"
              disabled={closeDisabled}
              title={closeTitle}
              onClick={onClose}
              className="px-2 py-1 text-[#a6907c] hover:text-[#F4ECE4] hover:bg-[#24180e] cursor-pointer"
            >
              <span>✕</span>
            </button>
          )}
        </div>
        <div className={bodyClassName}>{children}</div>
        {footer}
      </div>
    </div>
  )
}

