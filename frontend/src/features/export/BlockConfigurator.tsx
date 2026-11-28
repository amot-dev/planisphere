import { memo, useMemo } from 'react'
import { ChecklistItem, ChecklistSelector } from './ChecklistSelector'
import {
  getDistinctModesForScope, getVisitedLegs, getVisitedRegions,
  scopeSupportsModeSplit, VisitedLegRecord, VisitedRegionSummary,
} from './exportMetrics'
import {
  CareerDiffDisplay, ConfiguratorBlockConfig, DETAILS_SECTION_METRICS, DETAILS_SECTIONS,
  DetailsConfiguratorConfig, DetailsMetricId, DetailsSectionSelection, ExpeditionExportData,
  ExportBlockKind, ExportScopeKind, HIGHLIGHT_CARDS, HighlightDepth, HighlightsBlockConfig,
  SummaryBlockConfig, WAYPOINTS_COLUMNS, WaypointsBlockConfig,
} from './exportTypes'

/** Props for the left-column sticky Export Studio block configurator. */
export interface BlockConfiguratorProps {
  exportData: ExpeditionExportData
  config: ConfiguratorBlockConfig
  availableDetailsMetricIds: ReadonlySet<DetailsMetricId> | null
  addCount: number
  canAddToCart: boolean
  disabled?: boolean
  onSelectBlockKind: (nextKind: ExportBlockKind) => void
  onChangeConfig: (nextConfig: ConfiguratorBlockConfig) => void
  onAddToCart: () => void
}

const BLOCK_KIND_OPTIONS: ReadonlyArray<{ id: ExportBlockKind; label: string }> = [
  { id: 'summary', label: 'Summary' },
  { id: 'details', label: 'Details' },
  { id: 'waypoints', label: 'Waypoints' },
  { id: 'highlights', label: 'Highlights' },
]

const CAREER_DIFF_OPTIONS: ReadonlyArray<{ id: CareerDiffDisplay; label: string }> = [
  { id: 'hide', label: 'Hide' },
  { id: 'absolute', label: 'Absolute' },
  { id: 'percentage', label: 'Percent' },
  { id: 'both', label: 'Both' },
]

const UNAVAILABLE_METRIC_TOOLTIP =
  'Not available for the currently selected scope, mode split, sub-24h elapsed duration, or sub-1h in-game duration'

/**
 * BlockConfigurator renders sticky block-type, scope, conditional mode grouping,
 * and per-item checkbox controls with Select All and Select None actions.
 */
export const BlockConfigurator = memo(function BlockConfigurator(props: BlockConfiguratorProps) {
  const { exportData, config, availableDetailsMetricIds, addCount, canAddToCart, disabled = false, onSelectBlockKind, onChangeConfig, onAddToCart } = props
  const hasVisitedLegs = (exportData.plan.visitedWaypointCount ?? 0) >= 2
  const addDisabled = disabled || !canAddToCart

  return (
    <div className="space-y-3">
      <BlockKindSelector selectedKind={config.kind} hasVisitedLegs={hasVisitedLegs} disabled={disabled} onSelectBlockKind={onSelectBlockKind} />
      {config.kind === 'summary' && (
        <SummaryControls exportData={exportData} config={config} hasVisitedLegs={hasVisitedLegs} disabled={disabled} onChange={onChangeConfig} />
      )}
      {config.kind === 'details' && (
        <DetailsControls exportData={exportData} config={config} availableDetailsMetricIds={availableDetailsMetricIds} hasVisitedLegs={hasVisitedLegs} disabled={disabled} onChange={onChangeConfig} />
      )}
      {config.kind === 'waypoints' && <WaypointsControls config={config} disabled={disabled} onChange={onChangeConfig} />}
      {config.kind === 'highlights' && <HighlightsControls config={config} disabled={disabled} onChange={onChangeConfig} />}
      <button
        type="button"
        disabled={addDisabled}
        title={!disabled && !canAddToCart ? 'Select at least one applicable item before adding to cart' : undefined}
        onClick={onAddToCart}
        className={`w-full py-2 text-xs font-extrabold uppercase tracking-[0.14em] border transition ${
          addDisabled ? 'bg-ed-surface border-ed-border-dim text-ed-text-disabled cursor-not-allowed' : 'bg-ed-cyan-tint hover:bg-ed-cyan text-ed-cyan hover:text-ed-void border-ed-cyan cursor-pointer'
        }`}
      >
        {addCount > 1 ? `+ ADD ${addCount} BLOCKS TO CART` : '+ ADD TO CART'}
      </button>
    </div>
  )
})

function BlockKindSelector(props: {
  selectedKind: ExportBlockKind
  hasVisitedLegs: boolean
  disabled: boolean
  onSelectBlockKind: (nextKind: ExportBlockKind) => void
}) {
  const { selectedKind, hasVisitedLegs, disabled, onSelectBlockKind } = props
  return (
    <div>
      <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1.5">Block Type</div>
      <div className="grid grid-cols-4 gap-1">
        {BLOCK_KIND_OPTIONS.map((option) => {
          const lockedByMissingLegs = option.id === 'highlights' && !hasVisitedLegs
          const isButtonDisabled = disabled || lockedByMissingLegs
          const buttonStyle = isButtonDisabled
            ? 'bg-ed-void border-ed-border-subtle text-ed-text-disabled cursor-not-allowed'
            : selectedKind === option.id
              ? 'bg-ed-amber-tint border-ed-amber text-ed-amber cursor-pointer'
              : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main hover:bg-ed-hover cursor-pointer'
          return (
            <button
              key={option.id}
              type="button"
              disabled={isButtonDisabled}
              title={lockedByMissingLegs ? 'Visit Waypoint #2 to unlock leg and highlight exports' : undefined}
              onClick={() => onSelectBlockKind(option.id)}
              className={`flex items-center justify-center px-1 py-1.5 text-[10px] font-extrabold uppercase tracking-wide border transition ${buttonStyle}`}
            >
              {option.label}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function ScopeAndModeControls(props: {
  exportData: ExpeditionExportData
  config: SummaryBlockConfig | DetailsConfiguratorConfig
  hasVisitedLegs: boolean
  lockedFullOnly: boolean
  disabled: boolean
  detailsSection?: DetailsSectionSelection
  onUpdateScope: (nextScope: SummaryBlockConfig['scope']) => void
  onUpdateModeGrouping: (nextGrouping: 'combined' | 'separate') => void
}) {
  const { exportData, config, hasVisitedLegs, lockedFullOnly, disabled, detailsSection, onUpdateScope, onUpdateModeGrouping } = props
  const visitedLegs = getVisitedLegs(exportData)
  const visitedRegions = getVisitedRegions(exportData)
  const canSplitModes = scopeSupportsModeSplit(exportData, config.scope, detailsSection)
  const activeModesCount = getDistinctModesForScope(exportData, config.scope).length

  return (
    <div className="space-y-2.5">
      <ScopeSelectControls scope={config.scope} visitedLegs={visitedLegs} visitedRegions={visitedRegions} hasVisitedLegs={hasVisitedLegs} lockedFullOnly={lockedFullOnly} disabled={disabled} onUpdateScope={onUpdateScope} />
      {canSplitModes && (
        <div>
          <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">Flight Modes ({activeModesCount} Active)</div>
          <div className="grid grid-cols-2 gap-1">
            {(['combined', 'separate'] as const).map((grouping) => (
              <button
                key={grouping}
                type="button"
                disabled={disabled}
                onClick={() => onUpdateModeGrouping(grouping)}
                className={`py-1.5 text-[10px] font-extrabold uppercase tracking-wider border ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'} ${
                  config.modeGrouping === grouping ? 'bg-ed-cyan-tint border-ed-cyan text-ed-cyan' : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main'
                }`}
              >
                {grouping === 'combined' ? 'Combined' : `Separate (${activeModesCount})`}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function ScopeSelectControls(props: {
  scope: SummaryBlockConfig['scope']; visitedLegs: ReadonlyArray<VisitedLegRecord>; visitedRegions: ReadonlyArray<VisitedRegionSummary>
  hasVisitedLegs: boolean; lockedFullOnly: boolean; disabled: boolean; onUpdateScope: (nextScope: SummaryBlockConfig['scope']) => void
}) {
  const { scope, visitedLegs, visitedRegions, hasVisitedLegs, lockedFullOnly, disabled, onUpdateScope } = props
  function handleScopeKindChange(nextKind: ExportScopeKind) {
    if (nextKind === 'leg') return onUpdateScope({ kind: 'leg', rowId: visitedLegs[0]?.rowView.row.id })
    if (nextKind === 'region') return onUpdateScope({ kind: 'region', regionName: visitedRegions[0]?.regionName })
    onUpdateScope({ kind: nextKind })
  }
  return (
    <>
      <div>
        <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">Scope</div>
        <select value={lockedFullOnly ? 'full' : scope.kind} disabled={disabled || lockedFullOnly} onChange={(event) => handleScopeKindChange(event.target.value as ExportScopeKind)} className="w-full bg-ed-void border border-ed-border-strong border-l-2 border-l-ed-amber px-2.5 py-1.5 text-xs font-bold text-ed-text-main focus:border-ed-amber focus:outline-none disabled:opacity-50">
          <option value="full">Full Expedition</option>
          <option value="baseline">Pre-Expedition Baseline</option>
          <option value="leg" disabled={!hasVisitedLegs}>Specific Leg</option>
          <option value="allLegs" disabled={!hasVisitedLegs}>All Legs (1 per leg)</option>
          <option value="region" disabled={visitedRegions.length === 0}>Specific Galactic Region</option>
          <option value="allRegions" disabled={visitedRegions.length === 0}>All Galactic Regions (1 per region)</option>
        </select>
      </div>
      {!lockedFullOnly && scope.kind === 'leg' && visitedLegs.length > 0 && (
        <select value={scope.rowId ?? visitedLegs[0].rowView.row.id} disabled={disabled} onChange={(event) => onUpdateScope({ kind: 'leg', rowId: Number(event.target.value) })} className="w-full bg-ed-void border border-ed-border px-2.5 py-1.5 text-xs font-mono-tabular text-ed-text-main disabled:opacity-50">
          {visitedLegs.map((leg) => <option key={leg.rowView.row.id} value={leg.rowView.row.id}>{`#${leg.details.number - 1} -> #${leg.details.number}: ${leg.details.prevSystem || 'Origin'} -> ${leg.rowView.row.systemName}`}</option>)}
        </select>
      )}
      {!lockedFullOnly && scope.kind === 'region' && visitedRegions.length > 0 && (
        <select value={scope.regionName ?? visitedRegions[0].regionName} disabled={disabled} onChange={(event) => onUpdateScope({ kind: 'region', regionName: event.target.value })} className="w-full bg-ed-void border border-ed-border px-2.5 py-1.5 text-xs text-ed-text-main disabled:opacity-50">
          {visitedRegions.map((region) => <option key={region.regionName} value={region.regionName}>{`${region.regionName} (${region.legs.length} legs)`}</option>)}
        </select>
      )}
    </>
  )
}

function SummaryControls(props: {
  exportData: ExpeditionExportData
  config: SummaryBlockConfig
  hasVisitedLegs: boolean
  disabled: boolean
  onChange: (nextConfig: SummaryBlockConfig) => void
}) {
  const { exportData, config, hasVisitedLegs, disabled, onChange } = props
  const showCareerDiff = config.scope.kind === 'full' && config.modeGrouping === 'combined'
  return (
    <div className="space-y-3">
      <ScopeAndModeControls
        exportData={exportData}
        config={config}
        hasVisitedLegs={hasVisitedLegs}
        lockedFullOnly={false}
        disabled={disabled}
        onUpdateScope={(nextScope) => onChange({ ...config, scope: nextScope, modeGrouping: scopeSupportsModeSplit(exportData, nextScope) ? config.modeGrouping : 'combined' })}
        onUpdateModeGrouping={(nextGrouping) => onChange({ ...config, modeGrouping: nextGrouping })}
      />
      {showCareerDiff && (
        <div>
          <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">Career Diff</div>
          <div className="grid grid-cols-4 gap-1">
            {CAREER_DIFF_OPTIONS.map((option) => (
              <button
                key={option.id}
                type="button"
                disabled={disabled}
                onClick={() => onChange({ ...config, careerDiff: option.id })}
                className={`py-1 text-[10px] font-bold uppercase tracking-wider border ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'} ${
                  config.careerDiff === option.id ? 'bg-ed-amber-tint border-ed-amber text-ed-amber' : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main'
                }`}
              >
                {option.label}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function buildNextDetailsSectionConfig(
  exportData: ExpeditionExportData,
  config: DetailsConfiguratorConfig,
  nextSection: DetailsSectionSelection,
): DetailsConfiguratorConfig {
  const nextScope = nextSection === 'carrier' ? { kind: 'full' as const } : config.scope
  const nextModeGrouping = scopeSupportsModeSplit(exportData, nextScope, nextSection) ? config.modeGrouping : 'combined'
  if (nextSection === 'all') return { kind: 'details', section: 'all', scope: nextScope, modeGrouping: nextModeGrouping }
  const selectedMetrics = DETAILS_SECTION_METRICS[nextSection].filter((metric) => metric.defaultChecked).map((metric) => metric.id)
  return { ...config, section: nextSection, scope: nextScope, modeGrouping: nextModeGrouping, selectedMetrics }
}

function DetailsControls(props: {
  exportData: ExpeditionExportData
  config: DetailsConfiguratorConfig
  availableDetailsMetricIds: ReadonlySet<DetailsMetricId> | null
  hasVisitedLegs: boolean
  disabled: boolean
  onChange: (nextConfig: DetailsConfiguratorConfig) => void
}) {
  const { exportData, config, availableDetailsMetricIds, hasVisitedLegs, disabled, onChange } = props
  const metricChecklistItems = useMemo<ReadonlyArray<ChecklistItem<DetailsMetricId>>>(() => {
    if (config.section === 'all') return []
    return DETAILS_SECTION_METRICS[config.section].map((descriptor) => ({
      id: descriptor.id,
      label: descriptor.label,
      available: availableDetailsMetricIds ? availableDetailsMetricIds.has(descriptor.id) : true,
      disabledTooltip: UNAVAILABLE_METRIC_TOOLTIP,
    }))
  }, [availableDetailsMetricIds, config.section])

  return (
    <div className="space-y-3">
      <ScopeAndModeControls
        exportData={exportData}
        config={config}
        hasVisitedLegs={hasVisitedLegs}
        lockedFullOnly={config.section === 'carrier'}
        disabled={disabled}
        detailsSection={config.section}
        onUpdateScope={(nextScope) => onChange({ ...config, scope: nextScope, modeGrouping: scopeSupportsModeSplit(exportData, nextScope, config.section) ? config.modeGrouping : 'combined' })}
        onUpdateModeGrouping={(nextGrouping) => onChange({ ...config, modeGrouping: nextGrouping })}
      />
      <div>
        <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">Section</div>
        <select value={config.section} disabled={disabled} onChange={(event) => onChange(buildNextDetailsSectionConfig(exportData, config, event.target.value as DetailsSectionSelection))} className="w-full bg-ed-void border border-ed-border-strong border-l-2 border-l-ed-cyan px-2.5 py-1.5 text-xs font-bold text-ed-text-main focus:border-ed-cyan focus:outline-none disabled:opacity-50">
          <option value="all">All (1 per section)</option>
          {DETAILS_SECTIONS.map((section) => <option key={section.id} value={section.id}>{section.label}</option>)}
        </select>
      </div>
      {config.section === 'all' ? (
        <div className="bg-ed-void border border-ed-border-subtle px-2.5 py-2 text-[11px] text-ed-text-muted">Adds one block for each applicable section using its default data points.</div>
      ) : (
        <ChecklistSelector label="Data Points" items={metricChecklistItems} selectedIds={config.selectedMetrics} disabled={disabled} onChangeSelectedIds={(selectedMetrics) => onChange({ ...config, selectedMetrics })} />
      )}
    </div>
  )
}

function WaypointsControls(props: { config: WaypointsBlockConfig; disabled: boolean; onChange: (nextConfig: WaypointsBlockConfig) => void }) {
  const { config, disabled, onChange } = props
  return (
    <div className="space-y-3">
      <div>
        <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">Route Rows</div>
        <div className="grid grid-cols-2 gap-1">
          {(['visited', 'fullPlan'] as const).map((waypointsScope) => (
            <button
              key={waypointsScope}
              type="button"
              disabled={disabled}
              onClick={() => onChange({ ...config, waypointsScope })}
              className={`py-1.5 text-[10px] font-extrabold uppercase tracking-wider border ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'} ${
                config.waypointsScope === waypointsScope ? 'bg-ed-amber-tint border-ed-amber text-ed-amber' : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main'
              }`}
            >
              {waypointsScope === 'visited' ? 'Visited Only' : 'Full Plan'}
            </button>
          ))}
        </div>
      </div>
      <ChecklistSelector label="Columns" items={WAYPOINTS_COLUMNS} selectedIds={config.selectedColumns} disabled={disabled} onChangeSelectedIds={(selectedColumns) => onChange({ ...config, selectedColumns })} />
    </div>
  )
}

const LUCRATIVE_METRIC_OPTIONS: ReadonlyArray<{ id: 'estimated' | 'realized'; label: string }> = [
  { id: 'estimated', label: 'Estimated' },
  { id: 'realized', label: 'Realized' },
]

const DISTANCE_METRIC_OPTIONS: ReadonlyArray<{ id: 'total' | 'ship'; label: string }> = [
  { id: 'total', label: 'Total Ly' },
  { id: 'ship', label: 'Ship FSD' },
]

const DURATION_METRIC_OPTIONS: ReadonlyArray<{ id: 'elapsed' | 'inGame'; label: string }> = [
  { id: 'elapsed', label: 'Calendar' },
  { id: 'inGame', label: 'In-Game' },
]

function HighlightMetricToggle<TValue extends string>(props: {
  label: string
  value: TValue
  options: ReadonlyArray<{ id: TValue; label: string }>
  disabled: boolean
  disabledTooltip?: string
  onSelect: (nextValue: TValue) => void
}) {
  const { label, value, options, disabled, disabledTooltip, onSelect } = props
  return (
    <div title={disabled ? disabledTooltip : undefined}>
      <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">{label}</div>
      <div className="grid grid-cols-2 gap-1">
        {options.map((option) => (
          <button
            key={option.id}
            type="button"
            disabled={disabled}
            onClick={() => onSelect(option.id)}
            className={`py-1 text-[10px] font-extrabold uppercase tracking-wider border ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'} ${
              value === option.id
                ? 'bg-ed-cyan-tint border-ed-cyan text-ed-cyan'
                : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main'
            }`}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  )
}

function HighlightsControls(props: { config: HighlightsBlockConfig; disabled: boolean; onChange: (nextConfig: HighlightsBlockConfig) => void }) {
  const { config, disabled, onChange } = props
  const hasLucrativeCard =
    config.selectedCards.includes('mostLucrative') || config.selectedCards.includes('highestEarningPace')
  const hasDistanceCard =
    config.selectedCards.includes('longestDistance') ||
    config.selectedCards.includes('shortestDistance') ||
    config.selectedCards.includes('fastestTransitPace')
  const hasDurationCard = config.selectedCards.includes('longestDuration')

  return (
    <div className="space-y-3">
      <div>
        <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">Leaderboard Depth</div>
        <div className="grid grid-cols-3 gap-1">
          {([1, 3, 5] as HighlightDepth[]).map((depth) => (
            <button
              key={depth}
              type="button"
              disabled={disabled}
              onClick={() => onChange({ ...config, depth })}
              className={`py-1.5 text-[10px] font-extrabold uppercase tracking-wider border ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'} ${
                config.depth === depth ? 'bg-ed-cyan-tint border-ed-cyan text-ed-cyan' : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main'
              }`}
            >
              {`Top ${depth}`}
            </button>
          ))}
        </div>
      </div>
      <div className="space-y-2">
        <HighlightMetricToggle
          label="Lucrative Metric"
          value={config.lucrativeMetric}
          options={LUCRATIVE_METRIC_OPTIONS}
          disabled={disabled || !hasLucrativeCard}
          disabledTooltip="Select Most Lucrative Leg or Highest Earning Pace to customize"
          onSelect={(lucrativeMetric) => onChange({ ...config, lucrativeMetric })}
        />
        <HighlightMetricToggle
          label="Distance Metric"
          value={config.distanceMetric}
          options={DISTANCE_METRIC_OPTIONS}
          disabled={disabled || !hasDistanceCard}
          disabledTooltip="Select a distance or transit pace card to customize"
          onSelect={(distanceMetric) => onChange({ ...config, distanceMetric })}
        />
        <HighlightMetricToggle
          label="Duration Metric"
          value={config.durationMetric}
          options={DURATION_METRIC_OPTIONS}
          disabled={disabled || !hasDurationCard}
          disabledTooltip="Select Longest Leg (Duration) to customize"
          onSelect={(durationMetric) => onChange({ ...config, durationMetric })}
        />
      </div>
      <ChecklistSelector label="Trophy Cards" items={HIGHLIGHT_CARDS} selectedIds={config.selectedCards} disabled={disabled} maxHeightClassName="max-h-44" onChangeSelectedIds={(selectedCards) => onChange({ ...config, selectedCards })} />
    </div>
  )
}
