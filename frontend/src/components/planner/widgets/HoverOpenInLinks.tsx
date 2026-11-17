import { memo } from 'react'
import { OpenURL } from '../../../../wailsjs/go/app/App'
import { expedition } from '../../../../wailsjs/go/models'
import { edsmSystemUrl, inaraSystemUrl } from '../../../utils/galacticTime'

/**
 * BinaryExternalLinkMode represents an unconditional On or Off visibility
 * setting for external star system links such as Inara and GEC.
 */
export type BinaryExternalLinkMode = 'on' | 'off'

/**
 * EdsmExternalLinkMode represents the visibility setting for EDSM waypoint
 * links, supporting conditional display only when no GEC button is shown.
 */
export type EdsmExternalLinkMode = 'on' | 'if_no_gec' | 'off'

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

  const showInara = externalLinkInara !== 'off'
  const gecEntry = rowView.gec
  const showGec = externalLinkGec !== 'off' && Boolean(gecEntry?.url)
  const showEdsm =
    externalLinkEdsm === 'on'
      ? true
      : externalLinkEdsm === 'off'
        ? false
        : !showGec

  if (!showInara && !showEdsm && !showGec) return null

  const linkButtonClasses =
    'h-5 px-1.5 inline-flex items-center justify-center whitespace-nowrap text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] hover:bg-[#00D2FF] text-[#00D2FF] hover:text-[#050506] border border-[#16485c] hover:border-[#00D2FF] cursor-pointer transition-colors'

  const inaraUrl = showInara ? inaraSystemUrl(systemName) : ''
  const edsmUrl = showEdsm ? edsmSystemUrl(systemName) : ''

  return (
    <div className="inline-flex items-center gap-1 opacity-85 group-hover:opacity-100 shrink-0 whitespace-nowrap">
      {showInara && (
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            OpenURL(inaraUrl).catch(console.error)
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
            OpenURL(edsmUrl).catch(console.error)
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
            OpenURL(gecEntry.url).catch(console.error)
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
