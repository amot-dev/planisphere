import type { ReactNode } from 'react'

/**
 * ModalShellProps defines the header metadata, maximum width, and close handler
 * for a framed cockpit modal dialog.
 */
export interface ModalShellProps {
  title: string
  subtitle?: string
  badge?: string
  onClose: () => void
  children: ReactNode
  maxWidth?: string
}

/**
 * ModalShell renders the shared full-screen backdrop, framed header bar, and
 * scrollable content container used across cockpit configuration and lifecycle modals.
 */
export function ModalShell({
  title,
  subtitle,
  badge,
  onClose,
  children,
  maxWidth = 'max-w-xl',
}: ModalShellProps) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4">
      <div
        className={`w-full ${maxWidth} max-h-[90vh] flex flex-col border border-[#3d2816] border-t-2 border-t-[#FF7100] bg-[#0b0907] shadow-2xl overflow-hidden`}
      >
        <div className="px-5 py-3.5 border-b border-[#3d2816] bg-[#140e09] flex items-center justify-between">
          <div>
            <div className="flex items-center gap-2">
              <h3 className="text-xs font-extrabold uppercase tracking-[0.16em] text-[#FF7100]">
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
          <button
            type="button"
            onClick={onClose}
            className="px-2 py-1 text-[#a6907c] hover:text-[#F4ECE4] hover:bg-[#24180e] cursor-pointer"
          >
            ✕
          </button>
        </div>
        <div className="p-5 overflow-y-auto space-y-4 text-xs">{children}</div>
      </div>
    </div>
  )
}

