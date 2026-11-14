import { stats } from '../../../wailsjs/go/models'
import { formatCredits, formatInt } from '../../utils/galacticTime'
import { RealizationLossBreakdown } from '../common/RealizationLossBreakdown'
import { StatRow } from './StatRow'

/**
 * ExplorationValuationSectionProps defines the discovery metrics, realized
 * profit and loss breakdown, and accordion toggle state rendered in the sidebar.
 */
export interface ExplorationValuationSectionProps {
  displayedMetrics: stats.Metrics
  displayedRealization: stats.RealizationBreakdown
  scienceOpsExpanded?: boolean
  scanExpanded: boolean
  organicExpanded: boolean
  realizedExpanded: boolean
  onToggleScienceOpsExpanded?: () => void
  onToggleScanExpanded: () => void
  onToggleOrganicExpanded: () => void
  onToggleRealizedExpanded: () => void
}

/**
 * ExplorationValuationSection renders the Science Operations card containing
 * Cartographics Value, scanned/mapped body counts, Exobiology Value, Organic
 * Samples, and the Realized Value ledger.
 */
export function ExplorationValuationSection({
  displayedMetrics,
  displayedRealization,
  scienceOpsExpanded = true,
  scanExpanded,
  organicExpanded,
  realizedExpanded,
  onToggleScienceOpsExpanded,
  onToggleScanExpanded,
  onToggleOrganicExpanded,
  onToggleRealizedExpanded,
}: ExplorationValuationSectionProps) {
  return (
    <div className="border border-[#3d2816] bg-[#0e0a07] overflow-hidden">
      <div
        className={`px-2 py-1.5 bg-[#140e09] ${
          scienceOpsExpanded ? 'border-b border-[#3d2816]' : ''
        } text-[10px] font-extrabold uppercase tracking-[0.14em] text-[#F4ECE4] flex items-center justify-between`}
      >
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={onToggleScienceOpsExpanded}
            title="Toggle Science Operations section"
            className={`h-5 w-5 flex items-center justify-center hover:bg-[#24180e] cursor-pointer ${
              scienceOpsExpanded ? 'text-[#FF7100]' : 'text-[#a6907c]'
            }`}
          >
            <svg
              className={`w-3.5 h-3.5 transition-transform ${
                scienceOpsExpanded ? 'rotate-90' : ''
              }`}
              viewBox="0 0 20 20"
              fill="currentColor"
            >
              <path
                fillRule="evenodd"
                d="M7.21 14.77a.75.75 0 01.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.25 4.5a.75.75 0 01-1.06-.02z"
                clipRule="evenodd"
              />
            </svg>
          </button>
          <span>Science Operations</span>
        </div>
      </div>
      {scienceOpsExpanded && (
        <div className="divide-y divide-[#24180e]">
          {/* Cartographics Value — Astrometric Cyan */}
          <div className="p-2.5">
            <button
              type="button"
              onClick={onToggleScanExpanded}
              className="w-full flex items-center justify-between gap-2 text-left cursor-pointer"
            >
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="text-xs font-bold uppercase tracking-wider text-[#F4ECE4] truncate">
                  Cartographics Value
                </span>
                <span className="text-[10px] text-[#00D2FF] shrink-0">{scanExpanded ? '▾' : '▸'}</span>
              </div>
              <span className="font-mono-tabular text-xs font-extrabold text-[#00D2FF] whitespace-nowrap shrink-0">
                {formatCredits(displayedMetrics.scanValueTotal)}
              </span>
            </button>
            {scanExpanded && (
              <div className="mt-2 pl-3.5 space-y-1 border-l-2 border-[#00D2FF]/50 text-[11px]">
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">Base Scans and Mapping</span>
                  <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
                    {formatCredits(displayedMetrics.scanValueBase)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">First Discovered / Mapped</span>
                  <span className="font-mono-tabular text-[#00D2FF] font-bold whitespace-nowrap shrink-0">
                    +{formatCredits(displayedMetrics.scanValueBonus)}
                  </span>
                </div>
              </div>
            )}
          </div>

          <StatRow
            label="Bodies Scanned"
            value={formatInt(displayedMetrics.bodiesScanned)}
            sub={
              displayedMetrics.firstDiscoveries > 0
                ? `${formatInt(displayedMetrics.firstDiscoveries)} undiscovered`
                : undefined
            }
            subClass="text-[#00D2FF]"
          />
          <StatRow
            label="Bodies Mapped"
            value={formatInt(displayedMetrics.bodiesMapped)}
            sub={
              displayedMetrics.firstMapped > 0
                ? `${formatInt(displayedMetrics.firstMapped)} first mapped`
                : undefined
            }
            subClass="text-[#00D2FF]"
          />

          {/* Exobiology Value — Vista Genomics Biological Emerald */}
          <div className="p-2.5">
            <button
              type="button"
              onClick={onToggleOrganicExpanded}
              className="w-full flex items-center justify-between gap-2 text-left cursor-pointer"
            >
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="text-xs font-bold uppercase tracking-wider text-[#F4ECE4] truncate">
                  Exobiology Value
                </span>
                <span className="text-[10px] text-[#00E5A3] shrink-0">{organicExpanded ? '▾' : '▸'}</span>
              </div>
              <span className="font-mono-tabular text-xs font-extrabold text-[#00E5A3] whitespace-nowrap shrink-0">
                {formatCredits(displayedMetrics.organicValueTotal)}
              </span>
            </button>
            {organicExpanded && (
              <div className="mt-2 pl-3.5 space-y-1 border-l-2 border-[#00E5A3]/50 text-[11px]">
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">Base Organic Value</span>
                  <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
                    {formatCredits(displayedMetrics.organicValueBase)}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2 text-[#a6907c]">
                  <span className="truncate">First Logged Bonus</span>
                  <span className="font-mono-tabular text-[#00E5A3] font-bold whitespace-nowrap shrink-0">
                    +{formatCredits(displayedMetrics.organicValueBonus)}
                  </span>
                </div>
              </div>
            )}
          </div>

          {/* Organic Samples — Vista Genomics Biological Emerald */}
          <StatRow
            label="Organic Samples"
            value={formatInt(displayedMetrics.organicSamples)}
            sub={
              displayedMetrics.firstLogged > 0
                ? `${formatInt(displayedMetrics.firstLogged)} first-logged species`
                : undefined
            }
            subClass="text-[#00E5A3]"
          />

          {/* Realized Value — Orange Realized Profit and Loss Ledger */}
          <div className="p-2.5">
            <button
              type="button"
              onClick={onToggleRealizedExpanded}
              className="w-full flex items-center justify-between gap-2 text-left cursor-pointer"
            >
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="text-xs font-bold uppercase tracking-wider text-[#F4ECE4] truncate">
                  Realized Value
                </span>
                <span className="text-[10px] text-[#FF7100] shrink-0">{realizedExpanded ? '▾' : '▸'}</span>
              </div>
              <span className="font-mono-tabular text-xs font-extrabold text-[#FF7100] whitespace-nowrap shrink-0">
                {formatCredits(displayedRealization.totalRealized)}
              </span>
            </button>
            {realizedExpanded && (
              <RealizationLossBreakdown
                realization={displayedRealization}
                className="mt-2 pl-3.5 space-y-1 border-l-2 border-[#FF7100]/50 text-[11px]"
              />
            )}
          </div>
        </div>
      )}
    </div>
  )
}

