import {
  formatCredits,
  formatInt,
  formatLoss,
  formatLy,
} from '../../../domain/galacticTime'
import {
  CareerDiffItem,
  formatEstimatedCarrierCredits,
  resolveTelemetryContext,
  sanitizeExportHexColor,
} from '../exportMetrics'
import {
  CareerDiffDisplay,
  ExpeditionExportData,
  SummaryBlockConfig,
} from '../exportTypes'

/** Props for the self-contained Summary export card component. */
export interface SummaryBlockProps {
  exportData: ExpeditionExportData
  config: SummaryBlockConfig
}

/**
 * SummaryBlock renders the lean fixed-layout expedition overview card with
 * optional inline Career Diff delta pills when viewing Full Expedition Combined.
 */
export function SummaryBlock({
  exportData,
  config,
}: SummaryBlockProps) {
  const context = resolveTelemetryContext(
    exportData,
    config.scope,
    config.selectedModeId,
  )
  const diffById = new Map<CareerDiffItem['id'], CareerDiffItem>()
  if (config.careerDiff !== 'hide' && context.careerDiff) {
    for (const item of context.careerDiff) {
      diffById.set(item.id, item)
    }
  }
  const showCarrierStrip =
    context.carrierCost != null &&
    (context.metrics.carrierJumps > 0 || context.carrierCost.totalCost > 0)
  const safeModeColor = context.modeBadge
    ? sanitizeExportHexColor(context.modeBadge.modeColor)
    : null
  const accentStyle = safeModeColor
    ? { borderLeftColor: safeModeColor, borderLeftWidth: '3px' }
    : undefined

  return (
    <div className="ed-exp-root" style={accentStyle}>
      <div className="ed-exp-header">
        <div>
          <div className="ed-exp-eyebrow">
            SUMMARY // {context.scopeLabel}
          </div>
          <div className="ed-exp-title-row">
            <h2 className="ed-exp-title">{context.routeTitle}</h2>
            {context.modeBadge && safeModeColor && (
              <span
                className="ed-exp-badge ed-exp-mono"
                style={{
                  borderColor: safeModeColor,
                  color: safeModeColor,
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

      <SummaryRouteStrip
        visitedWaypoints={context.visitedWaypoints}
        plannedWaypoints={context.plannedWaypoints}
        distinctRegionCount={context.distinctRegionCount}
        shipsFlown={context.shipsFlown}
        commanders={exportData.commanders}
      />

      <div className="ed-exp-grid ed-exp-cols-4">
        <div className="ed-exp-tile ed-exp-left-amber">
          <div className="ed-exp-kpi-label">Distance Travelled</div>
          <div className="ed-exp-kpi-value ed-exp-amber ed-exp-mono">
            {formatLy(context.metrics.distanceLy, 1)}
          </div>
          <div className="ed-exp-kpi-sub ed-exp-mono">
            Ship {formatLy(context.derived.shipDistanceLy, 1)} · Carrier{' '}
            {formatLy(context.metrics.carrierPassengerLy, 1)}
          </div>
          <InlineCareerDiff
            item={diffById.get('distanceLy')}
            displayMode={config.careerDiff}
            formatValue={(value) => formatLy(value, 1)}
          />
        </div>

        <div className="ed-exp-tile">
          <div className="ed-exp-kpi-label">Hyperspace Jumps</div>
          <div className="ed-exp-kpi-value ed-exp-mono">{formatInt(context.metrics.jumps)}</div>
          <div className="ed-exp-kpi-sub ed-exp-mono">
            {context.derived.avgJumpRangeLy != null
              ? `${formatLy(context.derived.avgJumpRangeLy, 1)} avg`
              : 'No piloted jumps'}
          </div>
          <InlineCareerDiff
            item={diffById.get('jumps')}
            displayMode={config.careerDiff}
            formatValue={formatInt}
          />
        </div>

        <div className="ed-exp-tile ed-exp-left-cyan">
          <div className="ed-exp-kpi-label">Estimated Exploration</div>
          <div className="ed-exp-kpi-value ed-exp-cyan ed-exp-mono">
            {formatCredits(context.derived.combinedEstimatedCredits)}
          </div>
          <div className="ed-exp-kpi-sub ed-exp-mono">
            Carto {formatCredits(context.metrics.scanValueTotal)} · Exo{' '}
            {formatCredits(context.metrics.organicValueTotal)}
          </div>
          <InlineCareerDiff
            item={diffById.get('estimatedValue')}
            displayMode={config.careerDiff}
            formatValue={formatCredits}
          />
        </div>

        <div className="ed-exp-tile ed-exp-left-emerald">
          <div className="ed-exp-kpi-label">Realized Net Value</div>
          <div className="ed-exp-kpi-value ed-exp-amber ed-exp-mono">
            {formatCredits(context.realization.totalRealized)}
          </div>
          <div className="ed-exp-kpi-sub ed-exp-mono">
            {context.realization.pendingValue > 0
              ? `Pending ${formatCredits(context.realization.pendingValue)}`
              : `Losses ${formatLoss(context.derived.totalLossesCredits)}`}
          </div>
          <InlineCareerDiff
            item={diffById.get('realizedValue')}
            displayMode={config.careerDiff}
            formatValue={formatCredits}
          />
        </div>
      </div>

      <div className="ed-exp-strip">
        <span>
          SCANNED / MAPPED:{' '}
          <strong className="ed-exp-mono ed-exp-cyan">
            {formatInt(context.metrics.bodiesScanned)} / {formatInt(context.metrics.bodiesMapped)}
          </strong>{' '}
          ({formatInt(context.metrics.firstDiscoveries)} first)
        </span>
        <span className="ed-exp-sep">|</span>
        <span>
          ORGANIC SAMPLES:{' '}
          <strong className="ed-exp-mono ed-exp-emerald">
            {formatInt(context.metrics.organicSamples)}
          </strong>{' '}
          ({formatInt(context.metrics.firstLogged)} first logged)
        </span>
      </div>

      {showCarrierStrip && context.carrierCost && (
        <div className="ed-exp-strip">
          <span>
            CARRIER JUMPS:{' '}
            <strong className="ed-exp-mono">{formatInt(context.metrics.carrierJumps)}</strong>
          </span>
          <span className="ed-exp-sep">|</span>
          <span>
            CARRIER DIST:{' '}
            <strong className="ed-exp-mono">
              {formatLy(context.metrics.carrierDistanceLy, 1)}
            </strong>
          </span>
          <span className="ed-exp-sep">|</span>
          <span>
            TRITIUM:{' '}
            <strong className="ed-exp-mono">
              {formatInt(context.metrics.tritiumUsed ?? 0)} t
            </strong>
          </span>
          <span className="ed-exp-sep">|</span>
          <span>
            CARRIER COST:{' '}
            <strong className="ed-exp-mono ed-exp-amber">
              {formatEstimatedCarrierCredits(
                context.carrierCost.totalCost,
                Boolean(context.carrierCost.totalEstimated),
              )}
            </strong>
          </span>
        </div>
      )}

      <div className="ed-exp-footer ed-exp-mono">
        <span>PLANISPHERE // ASTROMETRIC TELEMETRY TERMINAL</span>
        <span>EXPORTED {exportData.exportedAtGalactic}</span>
      </div>
    </div>
  )
}

function SummaryRouteStrip({
  visitedWaypoints,
  plannedWaypoints,
  distinctRegionCount,
  shipsFlown,
  commanders,
}: {
  visitedWaypoints: number | null
  plannedWaypoints: number | null
  distinctRegionCount: number | null
  shipsFlown: string[]
  commanders: string[]
}) {
  const segments: Array<{ key: string; label: string; value: string }> = []
  if (commanders.length > 0) {
    segments.push({ key: 'cmdr', label: 'CMDR', value: commanders.join(', ') })
  }
  if (visitedWaypoints != null && plannedWaypoints != null) {
    segments.push({
      key: 'wp',
      label: 'WAYPOINTS',
      value: `${visitedWaypoints} / ${plannedWaypoints}`,
    })
  }
  if (distinctRegionCount != null) {
    segments.push({ key: 'reg', label: 'REGIONS', value: String(distinctRegionCount) })
  }
  if (shipsFlown.length > 0) {
    segments.push({ key: 'ships', label: 'SHIPS', value: shipsFlown.join(', ') })
  }
  if (segments.length === 0) return null

  return (
    <div className="ed-exp-strip">
      {segments.map((segment, index) => (
        <span key={segment.key}>
          {index > 0 && <span className="ed-exp-sep"> | </span>}
          {segment.label}: <strong className="ed-exp-mono ed-exp-main">{segment.value}</strong>
        </span>
      ))}
    </div>
  )
}

function InlineCareerDiff({
  item,
  displayMode,
  formatValue,
}: {
  item: CareerDiffItem | undefined
  displayMode: CareerDiffDisplay
  formatValue: (value: number) => string
}) {
  if (!item || displayMode === 'hide') return null
  const parts: string[] = []
  if (displayMode === 'absolute' || displayMode === 'both') {
    parts.push(`PRE ${formatValue(item.baseline)}`)
  }
  if (displayMode === 'percentage' || displayMode === 'both') {
    const growth = item.isNewBaseline
      ? 'NEW'
      : item.growthPercent != null
        ? `+${item.growthPercent.toFixed(1)}%`
        : '—'
    parts.push(growth)
  }
  return <div className="ed-exp-diff ed-exp-mono">{parts.join(' · ')}</div>
}
