import { memo } from 'react'
import { openExternalUrl } from '../../../../api/backend'
import { expedition } from '../../../../api/models'
import { resolveExternalLinkVisibility } from '../../../../domain/externalLinks'
import { edsmSystemUrl, inaraSystemUrl } from '../../../../domain/galacticTime'

/**
 * HoverOpenInLinksProps defines the waypoint row view and configured visibility
 * modes used to render external Inara, EDSM, and EDAstro GEC links.
 */
export interface HoverOpenInLinksProps {
  rowView: expedition.RowView
  externalLinkInara?: string
  externalLinkEdsm?: string
  externalLinkGec?: string
}

/**
 * HoverOpenInLinks renders quick-launch buttons for opening a waypoint's star
 * system on Inara, EDSM, and any matched Galactic Exploration Catalog entry.
 */
export const HoverOpenInLinks = memo(function HoverOpenInLinks({
  rowView,
  externalLinkInara = 'on',
  externalLinkEdsm = 'if_no_gec',
  externalLinkGec = 'on',
}: HoverOpenInLinksProps) {
  const systemName = rowView.row.systemName?.trim()
  if (!systemName) return null

  const gecEntry = rowView.gec
  const { showInara, showEdsm, showGec } = resolveExternalLinkVisibility(Boolean(gecEntry?.url), {
    inara: externalLinkInara,
    edsm: externalLinkEdsm,
    gec: externalLinkGec,
  })

  if (!showInara && !showEdsm && !showGec) return null

  const linkButtonClasses =
    'h-5 px-1.5 inline-flex items-center justify-center whitespace-nowrap text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-ed-cyan-tint hover:bg-ed-cyan text-ed-cyan hover:text-ed-void border border-ed-cyan-border hover:border-ed-cyan cursor-pointer transition-colors'

  const inaraUrl = showInara ? inaraSystemUrl(systemName) : ''
  const edsmUrl = showEdsm ? edsmSystemUrl(systemName) : ''

  return (
    <div className="inline-flex items-center gap-1 opacity-85 group-hover:opacity-100 shrink-0 whitespace-nowrap">
      {showInara && (
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            openExternalUrl(inaraUrl)
          }}
          title={`Open ${systemName} on Inara (${inaraUrl})`}
          className={linkButtonClasses}
        >
          <span>INARA</span>
        </button>
      )}
      {showEdsm && (
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            openExternalUrl(edsmUrl)
          }}
          title={`Open ${systemName} on EDSM (${edsmUrl})`}
          className={linkButtonClasses}
        >
          <span>EDSM</span>
        </button>
      )}
      {showGec && gecEntry && (
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            openExternalUrl(gecEntry.url)
          }}
          title={`EDAstro GEC POI: ${gecEntry.name} (${gecEntry.type})`}
          className={linkButtonClasses}
        >
          <span>GEC</span>
        </button>
      )}
    </div>
  )
})

