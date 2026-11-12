import { stats } from '../../../wailsjs/go/models'
import { formatCredits, formatInt, formatLoss } from '../../utils/galacticTime'
import { StatRow } from './StatRow'

/**
 * ExplorationValuationSectionProps defines the discovery metrics, realized
 * profit and loss breakdown, and accordion toggle state rendered in the sidebar.
 */
export interface ExplorationValuationSectionProps {
  displayedMetrics: stats.Metrics
  displayedRealization: stats.RealizationBreakdown
  scanExpanded: boolean
  organicExpanded: boolean
  realizedExpanded: boolean
  onToggleScanExpanded: () => void
  onToggleOrganicExpanded: () => void
  onToggleRealizedExpanded: () => void
}

/**
 * ExplorationValuationSection renders scanned/mapped body counts, Cartographics
 * Value, Organic Samples, Exobiology Value, and the Realized Value ledger.
 */
export function ExplorationValuationSection({
  displayedMetrics,
  displayedRealization,
  scanExpanded,
  organicExpanded,
  realizedExpanded,
  onToggleScanExpanded,
  onToggleOrganicExpanded,
  onToggleRealizedExpanded,
}: ExplorationValuationSectionProps) {
  return (
    <div className="border border-[#3d2816] bg-[#0e0a07] divide-y divide-[#24180e]">
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
          <div className="mt-2 pl-3.5 space-y-1 border-l-2 border-[#FF7100]/50 text-[11px]">
            <div className="flex items-center justify-between gap-2 text-[#a6907c]">
              <span className="truncate">Cartographics Lost (Death)</span>
              <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                {formatLoss(displayedRealization.cartoLostDeath)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 text-[#a6907c]">
              <span className="truncate">Exobiology Lost (Death)</span>
              <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                {formatLoss(displayedRealization.organicLostDeath)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 text-[#a6907c]">
              <span className="truncate">Cartographics Stolen</span>
              <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                {formatLoss(displayedRealization.cartoStolen)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 text-[#a6907c]">
              <span className="truncate">Exobiology Stolen</span>
              <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                {formatLoss(displayedRealization.organicStolen)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 text-[#a6907c]">
              <span className="truncate">Carrier Tariffs</span>
              <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                {formatLoss(displayedRealization.carrierTariffLoss)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 text-[#a6907c]">
              <span className="truncate">NPC Crew Payouts</span>
              <span className="font-mono-tabular text-[#FF7100] whitespace-nowrap shrink-0">
                {formatLoss(displayedRealization.npcCrewLoss)}
              </span>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

