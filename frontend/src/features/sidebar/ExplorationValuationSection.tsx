import { realization, stats } from '../../api/models'
import { CartographicsValueBreakdown } from '../../components/ui/CartographicsValueBreakdown'
import { CollapsibleSectionCard } from '../../components/ui/CollapsibleSectionCard'
import { ExobiologyValueBreakdown } from '../../components/ui/ExobiologyValueBreakdown'
import { ExpandableMetricRow } from '../../components/ui/ExpandableMetricRow'
import { RealizationLossBreakdown } from '../../components/ui/RealizationLossBreakdown'
import { formatCredits, formatInt } from '../../domain/galacticTime'
import { StatRow } from './StatRow'

/**
 * ExplorationValuationSectionProps defines the discovery metrics, realized
 * profit and loss breakdown, and accordion toggle state rendered in the sidebar.
 */
export interface ExplorationValuationSectionProps {
  displayedMetrics: stats.Metrics
  displayedRealization: realization.Breakdown
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
    <CollapsibleSectionCard
      title="Science Operations"
      toggleTitle="Toggle Science Operations section"
      expanded={scienceOpsExpanded}
      onToggle={onToggleScienceOpsExpanded}
    >
      {/* Cartographics Value — Astrometric Cyan */}
      <ExpandableMetricRow
        label="Cartographics Value"
        value={formatCredits(displayedMetrics.scanValueTotal)}
        accent="cyan"
        expanded={scanExpanded}
        onToggle={onToggleScanExpanded}
      >
        <CartographicsValueBreakdown
          metrics={displayedMetrics}
          className="mt-2 pl-3.5 space-y-1 border-l-2 border-ed-cyan/50 text-[11px]"
          boldBonuses
        />
      </ExpandableMetricRow>

      <StatRow
        label="Bodies Scanned"
        value={formatInt(displayedMetrics.bodiesScanned)}
        sub={
          displayedMetrics.firstDiscoveries > 0
            ? `${formatInt(displayedMetrics.firstDiscoveries)} first discovered`
            : undefined
        }
        subClass="text-ed-cyan"
      />
      <StatRow
        label="Bodies Mapped"
        value={formatInt(displayedMetrics.bodiesMapped)}
        sub={
          displayedMetrics.firstMapped > 0
            ? `${formatInt(displayedMetrics.firstMapped)} first mapped`
            : undefined
        }
        subClass="text-ed-cyan"
      />

      {/* Exobiology Value — Vista Genomics Biological Emerald */}
      <ExpandableMetricRow
        label="Exobiology Value"
        value={formatCredits(displayedMetrics.organicValueTotal)}
        accent="emerald"
        expanded={organicExpanded}
        onToggle={onToggleOrganicExpanded}
      >
        <ExobiologyValueBreakdown
          metrics={displayedMetrics}
          className="mt-2 pl-3.5 space-y-1 border-l-2 border-ed-emerald/50 text-[11px]"
          boldBonuses
        />
      </ExpandableMetricRow>

      {/* Organic Samples — Vista Genomics Biological Emerald */}
      <StatRow
        label="Organic Samples"
        value={formatInt(displayedMetrics.organicSamples)}
        sub={
          displayedMetrics.firstLogged > 0
            ? `${formatInt(displayedMetrics.firstLogged)} first-logged species`
            : undefined
        }
        subClass="text-ed-emerald"
      />

      {/* Realized Value — Orange Realized Profit and Loss Ledger */}
      <ExpandableMetricRow
        label="Realized Value"
        value={formatCredits(displayedRealization.totalRealized)}
        accent="amber"
        expanded={realizedExpanded}
        onToggle={onToggleRealizedExpanded}
      >
        <RealizationLossBreakdown
          realization={displayedRealization}
          className="mt-2 pl-3.5 space-y-1 border-l-2 border-ed-amber/50 text-[11px]"
        />
      </ExpandableMetricRow>
    </CollapsibleSectionCard>
  )
}

