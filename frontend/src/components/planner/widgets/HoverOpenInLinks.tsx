import { memo } from 'react'
import { OpenURL } from '../../../../wailsjs/go/app/App'
import { expedition } from '../../../../wailsjs/go/models'
import { edsmSystemUrl, inaraSystemUrl } from '../../../utils/galacticTime'

/**
 * HoverOpenInLinksProps defines the waypoint row view used to build external
 * Inara, EDSM, and EDAstro Galactic Exploration Catalog links.
 */
export interface HoverOpenInLinksProps {
  rowView: expedition.RowView
}

/**
 * HoverOpenInLinks renders quick-launch buttons for opening a waypoint's star
 * system on Inara, EDSM, and any matched Galactic Exploration Catalog entry.
 */
export const HoverOpenInLinks = memo(function HoverOpenInLinks({
  rowView,
}: HoverOpenInLinksProps) {
  const systemName = rowView.row.systemName?.trim()
  if (!systemName) return null
  const inara = inaraSystemUrl(systemName)
  const edsm = edsmSystemUrl(systemName)

  const linkBtnClasses =
    'h-5 px-1.5 inline-flex items-center justify-center whitespace-nowrap text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] hover:bg-[#00D2FF] text-[#00D2FF] hover:text-[#050506] border border-[#16485c] hover:border-[#00D2FF] cursor-pointer transition-colors'

  return (
    <div className="inline-flex items-center gap-1 opacity-85 group-hover:opacity-100 shrink-0 whitespace-nowrap">
      <button
        type="button"
        onClick={(event) => {
          event.stopPropagation()
          OpenURL(inara).catch(console.error)
        }}
        title={`Open ${systemName} on Inara (${inara})`}
        className={linkBtnClasses}
      >
        <span>INARA</span>
        <span aria-hidden="true">↗</span>
      </button>
      <button
        type="button"
        onClick={(event) => {
          event.stopPropagation()
          OpenURL(edsm).catch(console.error)
        }}
        title={`Open ${systemName} on EDSM (${edsm})`}
        className={linkBtnClasses}
      >
        <span>EDSM</span>
        <span aria-hidden="true">↗</span>
      </button>
      {rowView.gec?.url && (
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            OpenURL(rowView.gec!.url).catch(console.error)
          }}
          title={`EDAstro GEC POI: ${rowView.gec.name} (${rowView.gec.type})`}
          className={linkBtnClasses}
        >
          <span>GEC</span>
          <span aria-hidden="true">↗</span>
        </button>
      )}
    </div>
  )
})

