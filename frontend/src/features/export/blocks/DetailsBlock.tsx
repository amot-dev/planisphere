import {
  formatCredits,
  formatEfficiencyBonusLabel,
  formatInt,
  formatLoss,
  formatLy,
} from '../../../domain/galacticTime'
import {
  isDetailsMetricAvailable,
  ResolvedTelemetryContext,
  resolveTelemetryContext,
} from '../exportMetrics'
import {
  DETAILS_SECTION_METRICS,
  DETAILS_SECTIONS,
  DetailsBlockConfig,
  DetailsMetricId,
  ExpeditionExportData,
  ExportTelemetrySnapshot,
} from '../exportTypes'

/** Props for the self-contained Details export card component. */
export interface DetailsBlockProps {
  exportData: ExpeditionExportData
  config: DetailsBlockConfig
  customRangeSnapshot?: ExportTelemetrySnapshot
}

/**
 * DetailsBlock renders a single telemetry pillar using a Two-Zone Auto-Flow
 * layout: balanced scalar KPI tiles above stacked full-width breakdown cards.
 */
export function DetailsBlock({
  exportData,
  config,
  customRangeSnapshot,
}: DetailsBlockProps) {
  const context = resolveTelemetryContext(
    exportData,
    config.scope,
    config.selectedModeId,
    customRangeSnapshot,
  )
  const sectionMeta = DETAILS_SECTIONS.find((section) => section.id === config.section)!
  const selectedSet = new Set<DetailsMetricId>(config.selectedMetrics)
  const activeDescriptors = DETAILS_SECTION_METRICS[config.section].filter(
    (descriptor) =>
      selectedSet.has(descriptor.id) &&
      isDetailsMetricAvailable(descriptor, context, config.scope.kind),
  )

  const kpiDescriptors = activeDescriptors.filter((descriptor) => descriptor.zone === 'kpi')
  const cardDescriptors = activeDescriptors.filter((descriptor) => descriptor.zone === 'card')
  const colsClass =
    kpiDescriptors.length <= 1
      ? 'ed-exp-cols-1'
      : kpiDescriptors.length === 2 || kpiDescriptors.length === 4
        ? 'ed-exp-cols-2'
        : 'ed-exp-cols-3'
  const headerAccentClass =
    sectionMeta.accent === 'cyan'
      ? 'ed-exp-header-cyan'
      : sectionMeta.accent === 'emerald'
        ? 'ed-exp-header-emerald'
        : ''
  const accentStyle = context.modeBadge
    ? { borderLeftColor: context.modeBadge.modeColor, borderLeftWidth: '3px' }
    : undefined

  return (
    <div className="ed-exp-root" style={accentStyle}>
      <div className={`ed-exp-header ${headerAccentClass}`}>
        <div>
          <div className="ed-exp-eyebrow">
            {sectionMeta.label.toUpperCase()} // {context.scopeLabel}
          </div>
          <div className="ed-exp-title-row">
            <h2 className="ed-exp-title">{context.routeTitle}</h2>
            {context.modeBadge && (
              <span
                className="ed-exp-badge ed-exp-mono"
                style={{
                  borderColor: context.modeBadge.modeColor,
                  color: context.modeBadge.modeColor,
                }}
              >
                {context.modeBadge.modeName}
              </span>
            )}
            {context.inSystem && (
              <span className="ed-exp-badge ed-exp-badge-cyan ed-exp-mono">IN SYSTEM</span>
            )}
          </div>
        </div>
        <div className="ed-exp-dates ed-exp-mono">
          <div>
            {context.fromGalactic} → {context.toGalactic}
          </div>
          {context.durationLabel && (
            <div className="ed-exp-muted">ELAPSED: {context.durationLabel}</div>
          )}
        </div>
      </div>

      {activeDescriptors.length === 0 ? (
        <div className="ed-exp-empty ed-exp-mono">
          Select at least one applicable data point to preview and add this block
        </div>
      ) : (
        <>
          {kpiDescriptors.length > 0 && (
            <div className={`ed-exp-grid ${colsClass}`}>
              {kpiDescriptors.map((descriptor) => (
                <DetailsKpiTile key={descriptor.id} metricId={descriptor.id} context={context} />
              ))}
            </div>
          )}
          {cardDescriptors.length > 0 && (
            <div
              className={`ed-exp-stack ${kpiDescriptors.length === 0 ? 'ed-exp-stack-only' : ''}`}
            >
              {cardDescriptors.map((descriptor) => (
                <DetailsBreakdownCard
                  key={descriptor.id}
                  metricId={descriptor.id}
                  context={context}
                />
              ))}
            </div>
          )}
        </>
      )}

      <div className="ed-exp-footer ed-exp-mono">
        <span>PLANISPHERE // ASTROMETRIC TELEMETRY TERMINAL</span>
        <span>EXPORTED {exportData.exportedAtGalactic}</span>
      </div>
    </div>
  )
}

function DetailsKpiTile({
  metricId,
  context,
}: {
  metricId: DetailsMetricId
  context: ResolvedTelemetryContext
}) {
  const tileData = resolveKpiTileContent(metricId, context)
  if (!tileData) return null
  return (
    <div className="ed-exp-tile">
      <div className="ed-exp-kpi-label">{tileData.label}</div>
      <div className={`ed-exp-kpi-value ed-exp-mono ${tileData.accentClass ?? ''}`}>
        {tileData.value}
      </div>
      {tileData.sub && <div className="ed-exp-kpi-sub ed-exp-mono">{tileData.sub}</div>}
    </div>
  )
}

function resolveKpiTileContent(
  metricId: DetailsMetricId,
  context: ResolvedTelemetryContext,
): { label: string; value: string; sub?: string; accentClass?: string } | null {
  const { metrics, realization: real, derived, carrierCost } = context
  switch (metricId) {
    case 'jumps':
      return { label: 'Hyperspace Jumps', value: formatInt(metrics.jumps) }
    case 'shipsFlown':
      return {
        label: 'Ships Flown',
        value: String(context.shipsFlown.length),
        sub: context.shipsFlown.join(', ') || 'None',
      }
    case 'distinctRegions':
      return {
        label: 'Distinct Galactic Regions',
        value: formatInt(context.distinctRegionCount ?? 0),
      }
    case 'avgJumpRange':
      return { label: 'Avg Jump Range', value: formatOptLy(derived.avgJumpRangeLy) }
    case 'boostRate':
      return { label: 'Boost Rate', value: formatOptPercent(derived.boostRatePercent) }
    case 'avgDistancePerDay':
      return { label: 'Avg Distance per Day', value: formatOptLy(derived.avgDistancePerDayLy) }
    case 'carrierJumps':
      return { label: 'Carrier Jumps', value: formatInt(metrics.carrierJumps) }
    case 'carrierDistance':
      return { label: 'Carrier Distance', value: formatLy(metrics.carrierDistanceLy, 1) }
    case 'avgCarrierJumpRange':
      return { label: 'Avg Carrier Jump Range', value: formatOptLy(derived.avgCarrierJumpRangeLy) }
    case 'carrierTritium':
      return { label: 'Carrier Tritium Used', value: `${formatInt(metrics.tritiumUsed ?? 0)} t` }
    case 'avgTritiumPerJump':
      return {
        label: 'Avg Tritium / Jump',
        value: derived.avgTritiumPerJump != null ? `${derived.avgTritiumPerJump.toFixed(1)} t` : '—',
      }
    case 'discoveryCountsSummary':
      return {
        label: 'Discovery Counts',
        value: `${formatInt(metrics.bodiesScanned)} bodies · ${formatInt(metrics.organicSamples)} samples`,
        sub: `${formatInt(metrics.firstDiscoveries)} first disc · ${formatInt(metrics.firstLogged)} first logged`,
        accentClass: 'ed-exp-cyan',
      }
    case 'realizedPendingSnapshot':
      return {
        label: 'Realized / Pending',
        value: formatCredits(real.totalRealized),
        sub: `Pending: ${formatCredits(real.pendingValue)}`,
        accentClass: 'ed-exp-amber',
      }
    case 'avgCombinedCreditsPerJump':
      return {
        label: 'Avg Credits / Jump',
        value: formatOptCredits(derived.avgCombinedCreditsPerJump),
        accentClass: 'ed-exp-cyan',
      }
    case 'avgCombinedCreditsPer1000Ly':
      return {
        label: 'Avg Credits / 1,000 Ly',
        value: formatOptCredits(derived.avgCombinedCreditsPer1000Ly),
        accentClass: 'ed-exp-cyan',
      }
    case 'avgCombinedCreditsPerDay':
      return {
        label: 'Avg Credits / Day',
        value: formatOptCredits(derived.avgCombinedCreditsPerDay),
        accentClass: 'ed-exp-cyan',
      }
    case 'cartoTotalValue':
      return {
        label: 'Cartographics Value',
        value: formatCredits(metrics.scanValueTotal),
        accentClass: 'ed-exp-cyan',
      }
    case 'bodiesScanned':
      return {
        label: 'Bodies Scanned',
        value: formatInt(metrics.bodiesScanned),
        sub: `${formatInt(metrics.firstDiscoveries)} first discovered`,
        accentClass: 'ed-exp-cyan',
      }
    case 'bodiesMapped':
      return {
        label: 'Bodies Mapped',
        value: formatInt(metrics.bodiesMapped),
        sub: `${formatInt(metrics.firstMapped)} first mapped`,
        accentClass: 'ed-exp-cyan',
      }
    case 'dssEfficiencyRate':
      return {
        label: 'DSS Probe Efficiency',
        value: formatOptPercent(derived.dssEfficiencyPercent),
        sub: `${formatInt(metrics.efficientMappings)} efficient`,
        accentClass: 'ed-exp-cyan',
      }
    case 'firstDiscoveryRate':
      return {
        label: 'First Discovery Rate',
        value: formatOptPercent(derived.firstDiscoveryRatePercent),
        accentClass: 'ed-exp-cyan',
      }
    case 'avgCartoCreditsPerJump':
      return {
        label: 'Avg Carto Cr / Jump',
        value: formatOptCredits(derived.avgCartoCreditsPerJump),
        accentClass: 'ed-exp-cyan',
      }
    case 'avgCartoCreditsPer1000Ly':
      return {
        label: 'Avg Carto Cr / 1,000 Ly',
        value: formatOptCredits(derived.avgCartoCreditsPer1000Ly),
        accentClass: 'ed-exp-cyan',
      }
    case 'exoTotalValue':
      return {
        label: 'Exobiology Value',
        value: formatCredits(metrics.organicValueTotal),
        accentClass: 'ed-exp-emerald',
      }
    case 'organicSamples':
      return {
        label: 'Organic Samples',
        value: formatInt(metrics.organicSamples),
        sub: `${formatInt(metrics.firstLogged)} first logged`,
        accentClass: 'ed-exp-emerald',
      }
    case 'firstLoggedRate':
      return {
        label: 'First Logged Rate',
        value: formatOptPercent(derived.firstLoggedRatePercent),
        accentClass: 'ed-exp-emerald',
      }
    case 'avgCreditsPerSample':
      return {
        label: 'Avg Credits / Sample',
        value: formatOptCredits(derived.avgCreditsPerSample),
        accentClass: 'ed-exp-emerald',
      }
    case 'avgExoCreditsPerJump':
      return {
        label: 'Avg Exo Cr / Jump',
        value: formatOptCredits(derived.avgExoCreditsPerJump),
        accentClass: 'ed-exp-emerald',
      }
    case 'avgExoCreditsPer1000Ly':
      return {
        label: 'Avg Exo Cr / 1,000 Ly',
        value: formatOptCredits(derived.avgExoCreditsPer1000Ly),
        accentClass: 'ed-exp-emerald',
      }
    case 'realizedTotal':
      return {
        label: 'Total Realized Net',
        value: formatCredits(real.totalRealized),
        accentClass: 'ed-exp-amber',
      }
    case 'pendingValue':
      return { label: 'Pending (In Transit)', value: formatCredits(real.pendingValue) }
    case 'estimatedValueTotal':
      return {
        label: 'Combined Estimated',
        value: formatCredits(derived.combinedEstimatedCredits),
      }
    case 'realizationRetentionRate':
      return {
        label: 'Realization Retention',
        value: formatOptPercent(derived.realizationRetentionPercent),
        accentClass: 'ed-exp-amber',
      }
    case 'netProfitAfterCarrierCost':
      return {
        label: 'Net Profit After Carrier',
        value: formatOptCredits(derived.netProfitAfterCarrierCost),
        sub: carrierCost ? `Carrier cost: ${formatCredits(carrierCost.totalCost)}` : undefined,
        accentClass: 'ed-exp-amber',
      }
    case 'avgRealizedCreditsPerJump':
      return {
        label: 'Avg Realized Cr / Jump',
        value: formatOptCredits(derived.avgRealizedCreditsPerJump),
        accentClass: 'ed-exp-amber',
      }
    case 'avgRealizedCreditsPer1000Ly':
      return {
        label: 'Avg Realized Cr / 1,000 Ly',
        value: formatOptCredits(derived.avgRealizedCreditsPer1000Ly),
        accentClass: 'ed-exp-amber',
      }
    default:
      return null
  }
}

function DetailsBreakdownCard({
  metricId,
  context,
}: {
  metricId: DetailsMetricId
  context: ResolvedTelemetryContext
}) {
  const { metrics, realization: real, carrierCost, derived } = context
  const renderRows = (rows: Array<{ label: string; value: string; accentClass?: string }>) =>
    rows.map((row) => (
      <div key={row.label} className="ed-exp-bd-row">
        <span className="ed-exp-muted">{row.label}</span>
        <span className={`ed-exp-mono ${row.accentClass ?? 'ed-exp-main'}`}>{row.value}</span>
      </div>
    ))

  switch (metricId) {
    case 'distanceBreakdown':
      return (
        <div className="ed-exp-bd-card ed-exp-left-amber">
          <div className="ed-exp-bd-head">
            <span>Distance Travelled</span>
            <span className="ed-exp-mono ed-exp-amber">{formatLy(metrics.distanceLy)}</span>
          </div>
          {renderRows([
            { label: 'By Ship (FSD)', value: formatLy(derived.shipDistanceLy) },
            { label: 'By Carrier (Passenger)', value: formatLy(metrics.carrierPassengerLy) },
          ])}
        </div>
      )
    case 'fsdBoosts':
      return (
        <div className="ed-exp-bd-card ed-exp-left-amber">
          <div className="ed-exp-bd-head">
            <span>FSD Boosts</span>
            <span className="ed-exp-mono ed-exp-amber">{formatInt(derived.totalBoosts)} total</span>
          </div>
          {renderRows([
            { label: 'Jet Cone Boosts', value: formatInt(metrics.jetConeBoosts) },
            { label: 'Premium Injections', value: formatInt(metrics.premiumBoosts) },
            { label: 'Standard Injections', value: formatInt(metrics.standardBoosts) },
            { label: 'Basic Injections', value: formatInt(metrics.basicBoosts) },
          ])}
        </div>
      )
    case 'carrierCosts':
      if (!carrierCost) return null
      return (
        <div className="ed-exp-bd-card ed-exp-left-amber">
          <div className="ed-exp-bd-head">
            <span>Carrier Cost Breakdown ({carrierCost.carrierType})</span>
            <span className="ed-exp-mono ed-exp-amber">{formatCredits(carrierCost.totalCost)}</span>
          </div>
          {renderRows([
            { label: 'Tritium Cost', value: formatCredits(carrierCost.tritiumCost) },
            { label: 'Jump Maintenance', value: formatCredits(carrierCost.jumpCost) },
            { label: 'Prorated Weekly Upkeep', value: formatCredits(carrierCost.upkeepCost) },
          ])}
        </div>
      )
    case 'combinedEstimatedValue':
      return (
        <div className="ed-exp-bd-card ed-exp-left-cyan">
          <div className="ed-exp-bd-head">
            <span>Combined Estimated Exploration Value</span>
            <span className="ed-exp-mono ed-exp-cyan">
              {formatCredits(derived.combinedEstimatedCredits)}
            </span>
          </div>
          {renderRows([
            {
              label: 'Universal Cartographics',
              value: formatCredits(metrics.scanValueTotal),
              accentClass: 'ed-exp-cyan',
            },
            {
              label: 'Vista Genomics (Exobiology)',
              value: formatCredits(metrics.organicValueTotal),
              accentClass: 'ed-exp-emerald',
            },
          ])}
        </div>
      )
    case 'cartoValueCard':
    case 'cartoValueBreakdown':
      return (
        <div className="ed-exp-bd-card ed-exp-left-cyan">
          <div className="ed-exp-bd-head">
            <span>Cartographics Valuation</span>
            <span className="ed-exp-mono ed-exp-cyan">{formatCredits(metrics.scanValueTotal)}</span>
          </div>
          {renderRows([
            { label: 'Base Scans and Mapping', value: formatCredits(metrics.scanValueBase) },
            {
              label: 'First Discovered / Mapped',
              value: `+${formatCredits(metrics.scanValueBonus)}`,
              accentClass: 'ed-exp-cyan',
            },
            {
              label: formatEfficiencyBonusLabel(metrics.efficientMappings, metrics.bodiesMapped),
              value: `+${formatCredits(metrics.scanValueEfficiencyBonus)}`,
              accentClass: 'ed-exp-cyan',
            },
          ])}
        </div>
      )
    case 'exoValueCard':
    case 'exoValueBreakdown':
      return (
        <div className="ed-exp-bd-card ed-exp-left-emerald">
          <div className="ed-exp-bd-head">
            <span>Exobiology Valuation</span>
            <span className="ed-exp-mono ed-exp-emerald">
              {formatCredits(metrics.organicValueTotal)}
            </span>
          </div>
          {renderRows([
            { label: 'Base Organic Value', value: formatCredits(metrics.organicValueBase) },
            {
              label: 'First Logged Bonus',
              value: `+${formatCredits(metrics.organicValueBonus)}`,
              accentClass: 'ed-exp-emerald',
            },
          ])}
        </div>
      )
    case 'lossBreakdown':
      return (
        <div className="ed-exp-bd-card ed-exp-left-danger">
          <div className="ed-exp-bd-head">
            <span>Realization Losses and Deductions</span>
            <span className="ed-exp-mono ed-exp-danger">
              {formatLoss(derived.totalLossesCredits)}
            </span>
          </div>
          {renderRows([
            { label: 'Cartographics Lost (Death)', value: formatLoss(real.cartoLostDeath) },
            { label: 'Exobiology Lost (Death)', value: formatLoss(real.organicLostDeath) },
            { label: 'Cartographics Stolen', value: formatLoss(real.cartoStolen) },
            { label: 'Exobiology Stolen', value: formatLoss(real.organicStolen) },
            { label: 'Carrier Tariffs', value: formatLoss(real.carrierTariffLoss) },
            { label: 'NPC Crew Payouts', value: formatLoss(real.npcCrewLoss) },
          ])}
        </div>
      )
    default:
      return null
  }
}

function formatOptLy(value: number | null): string {
  return value != null ? formatLy(value, 1) : '—'
}

function formatOptPercent(value: number | null): string {
  return value != null ? `${value.toFixed(1)}%` : '—'
}

function formatOptCredits(value: number | null): string {
  return value != null ? formatCredits(value) : '—'
}
