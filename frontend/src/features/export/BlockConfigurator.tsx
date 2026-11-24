import { SquareCheckbox } from '../../components/ui/SquareCheckbox'
import {
  getDistinctModesForScope,
  getVisitedLegs,
  getVisitedRegions,
  scopeSupportsModeSplit,
} from './exportMetrics'
import {
  CareerDiffDisplay,
  ConfiguratorBlockConfig,
  DETAILS_SECTION_METRICS,
  DETAILS_SECTIONS,
  DetailsConfiguratorConfig,
  DetailsMetricId,
  DetailsSectionSelection,
  ExpeditionExportData,
  ExportBlockKind,
  ExportScopeKind,
  HIGHLIGHT_CARDS,
  HighlightCardId,
  HighlightDepth,
  HighlightsBlockConfig,
  SummaryBlockConfig,
  WAYPOINTS_COLUMNS,
  WaypointsBlockConfig,
  WaypointsColumnId,
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

/**
 * BlockConfigurator renders sticky block-type, scope, conditional mode grouping,
 * and per-item checkbox controls with Select All / Select None actions.
 */
export function BlockConfigurator({
  exportData,
  config,
  availableDetailsMetricIds,
  addCount,
  canAddToCart,
  disabled = false,
  onSelectBlockKind,
  onChangeConfig,
  onAddToCart,
}: BlockConfiguratorProps) {
  const hasVisitedLegs = (exportData.plan.visitedWaypointCount ?? 0) >= 2

  return (
    <div className="space-y-3">
      <div>
        <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1.5">
          Block Type
        </div>
        <div className="grid grid-cols-4 gap-1">
          {BLOCK_KIND_OPTIONS.map((option) => {
            const lockedByMissingLegs = option.id === 'highlights' && !hasVisitedLegs
            const isDisabled = disabled || lockedByMissingLegs
            const isSelected = config.kind === option.id
            return (
              <button
                key={option.id}
                type="button"
                disabled={isDisabled}
                title={
                  lockedByMissingLegs
                    ? 'Visit Waypoint #2 to unlock leg and highlight exports'
                    : undefined
                }
                onClick={() => onSelectBlockKind(option.id)}
                className={`flex items-center justify-center px-1 py-1.5 text-[10px] font-extrabold uppercase tracking-wide border transition ${
                  isDisabled
                    ? 'bg-ed-void border-ed-border-subtle text-ed-text-disabled cursor-not-allowed'
                    : isSelected
                      ? 'bg-ed-amber-tint border-ed-amber text-ed-amber cursor-pointer'
                      : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main hover:bg-ed-hover cursor-pointer'
                }`}
              >
                {option.label}
              </button>
            )
          })}
        </div>
      </div>

      {config.kind === 'summary' && (
        <SummaryControls
          exportData={exportData}
          config={config}
          hasVisitedLegs={hasVisitedLegs}
          disabled={disabled}
          onChange={onChangeConfig}
        />
      )}

      {config.kind === 'details' && (
        <DetailsControls
          exportData={exportData}
          config={config}
          availableDetailsMetricIds={availableDetailsMetricIds}
          hasVisitedLegs={hasVisitedLegs}
          disabled={disabled}
          onChange={onChangeConfig}
        />
      )}

      {config.kind === 'waypoints' && (
        <WaypointsControls config={config} disabled={disabled} onChange={onChangeConfig} />
      )}

      {config.kind === 'highlights' && (
        <HighlightsControls config={config} disabled={disabled} onChange={onChangeConfig} />
      )}

      <button
        type="button"
        disabled={disabled || !canAddToCart}
        title={
          !disabled && !canAddToCart
            ? 'Select at least one applicable item before adding to cart'
            : undefined
        }
        onClick={onAddToCart}
        className={`w-full py-2 text-xs font-extrabold uppercase tracking-[0.14em] border transition ${
          disabled || !canAddToCart
            ? 'bg-ed-surface border-ed-border-dim text-ed-text-disabled cursor-not-allowed'
            : 'bg-ed-cyan-tint hover:bg-ed-cyan text-ed-cyan hover:text-ed-void border-ed-cyan cursor-pointer'
        }`}
      >
        {addCount > 1 ? `+ ADD ${addCount} BLOCKS TO CART` : '+ ADD TO CART'}
      </button>
    </div>
  )
}

function ScopeAndModeControls({
  exportData,
  config,
  hasVisitedLegs,
  lockedFullOnly,
  disabled,
  detailsSection,
  onUpdateScope,
  onUpdateModeGrouping,
}: {
  exportData: ExpeditionExportData
  config: SummaryBlockConfig | DetailsConfiguratorConfig
  hasVisitedLegs: boolean
  lockedFullOnly: boolean
  disabled: boolean
  detailsSection?: DetailsSectionSelection
  onUpdateScope: (nextScope: SummaryBlockConfig['scope']) => void
  onUpdateModeGrouping: (nextGrouping: 'combined' | 'separate') => void
}) {
  const visitedLegs = getVisitedLegs(exportData)
  const visitedRegions = getVisitedRegions(exportData)
  const canSplitModes = scopeSupportsModeSplit(exportData, config.scope, detailsSection)
  const activeModesCount = getDistinctModesForScope(exportData, config.scope).length

  function handleScopeKindChange(nextKind: ExportScopeKind) {
    if (nextKind === 'leg') {
      onUpdateScope({ kind: 'leg', rowId: visitedLegs[0]?.rowView.row.id })
      return
    }
    if (nextKind === 'region') {
      onUpdateScope({ kind: 'region', regionName: visitedRegions[0]?.regionName })
      return
    }
    onUpdateScope({ kind: nextKind })
  }

  return (
    <div className="space-y-2.5">
      <div>
        <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">
          Scope
        </div>
        <select
          value={lockedFullOnly ? 'full' : config.scope.kind}
          disabled={disabled || lockedFullOnly}
          onChange={(event) => handleScopeKindChange(event.target.value as ExportScopeKind)}
          className="w-full bg-ed-void border border-ed-border-strong border-l-2 border-l-ed-amber px-2.5 py-1.5 text-xs font-bold text-ed-text-main focus:border-ed-amber focus:outline-none disabled:opacity-50"
        >
          <option value="full">Full Expedition</option>
          <option value="baseline">Pre-Expedition Baseline</option>
          <option value="leg" disabled={!hasVisitedLegs}>
            Specific Leg
          </option>
          <option value="allLegs" disabled={!hasVisitedLegs}>
            All Legs (1 per leg)
          </option>
          <option value="region" disabled={visitedRegions.length === 0}>
            Specific Galactic Region
          </option>
          <option value="allRegions" disabled={visitedRegions.length === 0}>
            All Galactic Regions (1 per region)
          </option>
        </select>
      </div>

      {!lockedFullOnly && config.scope.kind === 'leg' && visitedLegs.length > 0 && (
        <select
          value={config.scope.rowId ?? visitedLegs[0].rowView.row.id}
          disabled={disabled}
          onChange={(event) =>
            onUpdateScope({ kind: 'leg', rowId: Number(event.target.value) })
          }
          className="w-full bg-ed-void border border-ed-border px-2.5 py-1.5 text-xs font-mono-tabular text-ed-text-main disabled:opacity-50"
        >
          {visitedLegs.map((leg) => (
            <option key={leg.rowView.row.id} value={leg.rowView.row.id}>
              {`#${leg.details.number - 1} -> #${leg.details.number}: ${leg.details.prevSystem || 'Origin'} -> ${leg.rowView.row.systemName}`}
            </option>
          ))}
        </select>
      )}

      {!lockedFullOnly && config.scope.kind === 'region' && visitedRegions.length > 0 && (
        <select
          value={config.scope.regionName ?? visitedRegions[0].regionName}
          disabled={disabled}
          onChange={(event) =>
            onUpdateScope({ kind: 'region', regionName: event.target.value })
          }
          className="w-full bg-ed-void border border-ed-border px-2.5 py-1.5 text-xs text-ed-text-main disabled:opacity-50"
        >
          {visitedRegions.map((region) => (
            <option key={region.regionName} value={region.regionName}>
              {`${region.regionName} (${region.legs.length} legs)`}
            </option>
          ))}
        </select>
      )}

      {canSplitModes && (
        <div>
          <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">
            Flight Modes ({activeModesCount} Active)
          </div>
          <div className="grid grid-cols-2 gap-1">
            {(['combined', 'separate'] as const).map((grouping) => (
              <button
                key={grouping}
                type="button"
                disabled={disabled}
                onClick={() => onUpdateModeGrouping(grouping)}
                className={`py-1.5 text-[10px] font-extrabold uppercase tracking-wider border ${
                  disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'
                } ${
                  config.modeGrouping === grouping
                    ? 'bg-ed-cyan-tint border-ed-cyan text-ed-cyan'
                    : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main'
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

function SummaryControls({
  exportData,
  config,
  hasVisitedLegs,
  disabled,
  onChange,
}: {
  exportData: ExpeditionExportData
  config: SummaryBlockConfig
  hasVisitedLegs: boolean
  disabled: boolean
  onChange: (next: SummaryBlockConfig) => void
}) {
  const showCareerDiff = config.scope.kind === 'full' && config.modeGrouping === 'combined'

  return (
    <div className="space-y-3">
      <ScopeAndModeControls
        exportData={exportData}
        config={config}
        hasVisitedLegs={hasVisitedLegs}
        lockedFullOnly={false}
        disabled={disabled}
        onUpdateScope={(nextScope) =>
          onChange({
            ...config,
            scope: nextScope,
            modeGrouping: scopeSupportsModeSplit(exportData, nextScope)
              ? config.modeGrouping
              : 'combined',
          })
        }
        onUpdateModeGrouping={(nextGrouping) => onChange({ ...config, modeGrouping: nextGrouping })}
      />

      {showCareerDiff && (
        <div>
          <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">
            Career Diff
          </div>
          <div className="grid grid-cols-4 gap-1">
            {CAREER_DIFF_OPTIONS.map((option) => (
              <button
                key={option.id}
                type="button"
                disabled={disabled}
                onClick={() => onChange({ ...config, careerDiff: option.id })}
                className={`py-1 text-[10px] font-bold uppercase tracking-wider border ${
                  disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'
                } ${
                  config.careerDiff === option.id
                    ? 'bg-ed-amber-tint border-ed-amber text-ed-amber'
                    : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main'
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

function DetailsControls({
  exportData,
  config,
  availableDetailsMetricIds,
  hasVisitedLegs,
  disabled,
  onChange,
}: {
  exportData: ExpeditionExportData
  config: DetailsConfiguratorConfig
  availableDetailsMetricIds: ReadonlySet<DetailsMetricId> | null
  hasVisitedLegs: boolean
  disabled: boolean
  onChange: (next: DetailsConfiguratorConfig) => void
}) {
  const descriptors = config.section === 'all' ? [] : DETAILS_SECTION_METRICS[config.section]
  const selectedSet = new Set<DetailsMetricId>(
    config.section === 'all' ? [] : config.selectedMetrics,
  )
  const isCarrierSection = config.section === 'carrier'

  function handleSectionChange(nextSection: DetailsSectionSelection) {
    const nextScope = nextSection === 'carrier' ? { kind: 'full' as const } : config.scope
    const nextModeGrouping = scopeSupportsModeSplit(exportData, nextScope, nextSection)
      ? config.modeGrouping
      : 'combined'
    if (nextSection === 'all') {
      onChange({
        kind: 'details',
        section: 'all',
        scope: nextScope,
        modeGrouping: nextModeGrouping,
      })
      return
    }
    const nextMetrics = DETAILS_SECTION_METRICS[nextSection]
      .filter((metric) => metric.defaultChecked)
      .map((metric) => metric.id)
    onChange({
      ...config,
      section: nextSection,
      scope: nextScope,
      modeGrouping: nextModeGrouping,
      selectedMetrics: nextMetrics,
    })
  }

  function toggleMetric(metricId: DetailsMetricId) {
    if (config.section === 'all') return
    const nextSelected = selectedSet.has(metricId)
      ? config.selectedMetrics.filter((id) => id !== metricId)
      : [...config.selectedMetrics, metricId]
    onChange({ ...config, selectedMetrics: nextSelected })
  }

  return (
    <div className="space-y-3">
      <ScopeAndModeControls
        exportData={exportData}
        config={config}
        hasVisitedLegs={hasVisitedLegs}
        lockedFullOnly={isCarrierSection}
        disabled={disabled}
        detailsSection={config.section}
        onUpdateScope={(nextScope) => {
          const nextModeGrouping = scopeSupportsModeSplit(exportData, nextScope, config.section)
            ? config.modeGrouping
            : 'combined'
          onChange(
            config.section === 'all'
              ? { ...config, scope: nextScope, modeGrouping: nextModeGrouping }
              : { ...config, scope: nextScope, modeGrouping: nextModeGrouping },
          )
        }}
        onUpdateModeGrouping={(nextGrouping) =>
          onChange(
            config.section === 'all'
              ? { ...config, modeGrouping: nextGrouping }
              : { ...config, modeGrouping: nextGrouping },
          )
        }
      />

      <div>
        <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">
          Section
        </div>
        <select
          value={config.section}
          disabled={disabled}
          onChange={(event) => handleSectionChange(event.target.value as DetailsSectionSelection)}
          className="w-full bg-ed-void border border-ed-border-strong border-l-2 border-l-ed-cyan px-2.5 py-1.5 text-xs font-bold text-ed-text-main focus:border-ed-cyan focus:outline-none disabled:opacity-50"
        >
          <option value="all">All (1 per section)</option>
          {DETAILS_SECTIONS.map((section) => (
            <option key={section.id} value={section.id}>
              {section.label}
            </option>
          ))}
        </select>
      </div>

      {config.section === 'all' ? (
        <div className="bg-ed-void border border-ed-border-subtle px-2.5 py-2 text-[11px] text-ed-text-muted">
          Adds one block for each applicable section using its default data points.
        </div>
      ) : (
        <div>
          <div className="flex items-center justify-between mb-1.5">
            <span className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted">
              Data Points
            </span>
            <div className="flex items-center gap-2 text-[10px] font-mono-tabular">
              <button
                type="button"
                disabled={disabled}
                onClick={() =>
                  onChange({ ...config, selectedMetrics: descriptors.map((item) => item.id) })
                }
                className={`text-ed-amber ${disabled ? 'opacity-40 cursor-not-allowed' : 'hover:underline cursor-pointer'}`}
              >
                [All]
              </button>
              <button
                type="button"
                disabled={disabled}
                onClick={() => onChange({ ...config, selectedMetrics: [] })}
                className={`text-ed-text-muted ${disabled ? 'opacity-40 cursor-not-allowed' : 'hover:text-ed-text-main hover:underline cursor-pointer'}`}
              >
                [None]
              </button>
            </div>
          </div>
          <div className="space-y-1 max-h-48 overflow-y-auto pr-1">
            {descriptors.map((descriptor) => {
              const available = availableDetailsMetricIds
                ? availableDetailsMetricIds.has(descriptor.id)
                : true
              const checked = selectedSet.has(descriptor.id)
              const isMetricDisabled = disabled || !available
              return (
                <button
                  key={descriptor.id}
                  type="button"
                  disabled={isMetricDisabled}
                  title={
                    !available
                      ? 'Not available for the currently selected scope, mode split, or sub-24h duration'
                      : undefined
                  }
                  onClick={() => toggleMetric(descriptor.id)}
                  className={`w-full flex items-center gap-2 px-2 py-1 border text-left text-[11px] ${
                    isMetricDisabled
                      ? 'bg-ed-void border-ed-border-subtle text-ed-text-disabled opacity-50 cursor-not-allowed'
                      : checked
                        ? 'bg-ed-control border-ed-border-strong text-ed-text-main cursor-pointer'
                        : 'bg-ed-void border-ed-border-subtle text-ed-text-muted hover:text-ed-text-main cursor-pointer'
                  }`}
                >
                  <SquareCheckbox checked={checked && available} size="xs" />
                  <span className="truncate">{descriptor.label}</span>
                </button>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}

function WaypointsControls({
  config,
  disabled,
  onChange,
}: {
  config: WaypointsBlockConfig
  disabled: boolean
  onChange: (next: WaypointsBlockConfig) => void
}) {
  const selectedSet = new Set<WaypointsColumnId>(config.selectedColumns)

  function toggleColumn(columnId: WaypointsColumnId) {
    const nextColumns = selectedSet.has(columnId)
      ? config.selectedColumns.filter((id) => id !== columnId)
      : [...config.selectedColumns, columnId]
    onChange({ ...config, selectedColumns: nextColumns })
  }

  return (
    <div className="space-y-3">
      <div>
        <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">
          Route Rows
        </div>
        <div className="grid grid-cols-2 gap-1">
          {(['visited', 'fullPlan'] as const).map((scope) => (
            <button
              key={scope}
              type="button"
              disabled={disabled}
              onClick={() => onChange({ ...config, waypointsScope: scope })}
              className={`py-1.5 text-[10px] font-extrabold uppercase tracking-wider border ${
                disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'
              } ${
                config.waypointsScope === scope
                  ? 'bg-ed-amber-tint border-ed-amber text-ed-amber'
                  : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main'
              }`}
            >
              {scope === 'visited' ? 'Visited Only' : 'Full Plan'}
            </button>
          ))}
        </div>
      </div>

      <div>
        <div className="flex items-center justify-between mb-1.5">
          <span className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted">
            Columns
          </span>
          <div className="flex items-center gap-2 text-[10px] font-mono-tabular">
            <button
              type="button"
              disabled={disabled}
              onClick={() =>
                onChange({
                  ...config,
                  selectedColumns: WAYPOINTS_COLUMNS.map((column) => column.id),
                })
              }
              className={`text-ed-amber ${disabled ? 'opacity-40 cursor-not-allowed' : 'hover:underline cursor-pointer'}`}
            >
              [All]
            </button>
            <button
              type="button"
              disabled={disabled}
              onClick={() => onChange({ ...config, selectedColumns: [] })}
              className={`text-ed-text-muted ${disabled ? 'opacity-40 cursor-not-allowed' : 'hover:text-ed-text-main hover:underline cursor-pointer'}`}
            >
              [None]
            </button>
          </div>
        </div>
        <div className="space-y-1 max-h-48 overflow-y-auto pr-1">
          {WAYPOINTS_COLUMNS.map((column) => {
            const checked = selectedSet.has(column.id)
            return (
              <button
                key={column.id}
                type="button"
                disabled={disabled}
                onClick={() => toggleColumn(column.id)}
                className={`w-full flex items-center gap-2 px-2 py-1 border text-left text-[11px] ${
                  disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'
                } ${
                  checked
                    ? 'bg-ed-control border-ed-border-strong text-ed-text-main'
                    : 'bg-ed-void border-ed-border-subtle text-ed-text-muted hover:text-ed-text-main'
                }`}
              >
                <SquareCheckbox checked={checked} size="xs" />
                <span className="truncate">{column.label}</span>
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function HighlightsControls({
  config,
  disabled,
  onChange,
}: {
  config: HighlightsBlockConfig
  disabled: boolean
  onChange: (next: HighlightsBlockConfig) => void
}) {
  const selectedSet = new Set<HighlightCardId>(config.selectedCards)

  function toggleCard(cardId: HighlightCardId) {
    const nextCards = selectedSet.has(cardId)
      ? config.selectedCards.filter((id) => id !== cardId)
      : [...config.selectedCards, cardId]
    onChange({ ...config, selectedCards: nextCards })
  }

  return (
    <div className="space-y-3">
      <div>
        <div className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted mb-1">
          Leaderboard Depth
        </div>
        <div className="grid grid-cols-3 gap-1">
          {([1, 3, 5] as HighlightDepth[]).map((depth) => (
            <button
              key={depth}
              type="button"
              disabled={disabled}
              onClick={() => onChange({ ...config, depth })}
              className={`py-1.5 text-[10px] font-extrabold uppercase tracking-wider border ${
                disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'
              } ${
                config.depth === depth
                  ? 'bg-ed-cyan-tint border-ed-cyan text-ed-cyan'
                  : 'bg-ed-control border-ed-border text-ed-text-muted hover:text-ed-text-main'
              }`}
            >
              {`Top ${depth}`}
            </button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-2 gap-1.5">
        <button
          type="button"
          disabled={disabled}
          onClick={() =>
            onChange({
              ...config,
              lucrativeMetric: config.lucrativeMetric === 'estimated' ? 'realized' : 'estimated',
            })
          }
          className={`px-2 py-1 border border-ed-border bg-ed-control text-[10px] font-mono-tabular text-ed-text-muted ${
            disabled ? 'opacity-50 cursor-not-allowed' : 'hover:text-ed-text-main cursor-pointer'
          }`}
        >
          Value: {config.lucrativeMetric === 'estimated' ? 'Estimated' : 'Realized'}
        </button>
        <button
          type="button"
          disabled={disabled}
          onClick={() =>
            onChange({
              ...config,
              distanceMetric: config.distanceMetric === 'total' ? 'ship' : 'total',
            })
          }
          className={`px-2 py-1 border border-ed-border bg-ed-control text-[10px] font-mono-tabular text-ed-text-muted ${
            disabled ? 'opacity-50 cursor-not-allowed' : 'hover:text-ed-text-main cursor-pointer'
          }`}
        >
          Dist: {config.distanceMetric === 'total' ? 'Total Ly' : 'Ship FSD'}
        </button>
      </div>

      <div>
        <div className="flex items-center justify-between mb-1.5">
          <span className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted">
            Trophy Cards
          </span>
          <div className="flex items-center gap-2 text-[10px] font-mono-tabular">
            <button
              type="button"
              disabled={disabled}
              onClick={() =>
                onChange({ ...config, selectedCards: HIGHLIGHT_CARDS.map((card) => card.id) })
              }
              className={`text-ed-amber ${disabled ? 'opacity-40 cursor-not-allowed' : 'hover:underline cursor-pointer'}`}
            >
              [All]
            </button>
            <button
              type="button"
              disabled={disabled}
              onClick={() => onChange({ ...config, selectedCards: [] })}
              className={`text-ed-text-muted ${disabled ? 'opacity-40 cursor-not-allowed' : 'hover:text-ed-text-main hover:underline cursor-pointer'}`}
            >
              [None]
            </button>
          </div>
        </div>
        <div className="space-y-1 max-h-44 overflow-y-auto pr-1">
          {HIGHLIGHT_CARDS.map((card) => {
            const checked = selectedSet.has(card.id)
            return (
              <button
                key={card.id}
                type="button"
                disabled={disabled}
                onClick={() => toggleCard(card.id)}
                className={`w-full flex items-center gap-2 px-2 py-1 border text-left text-[11px] ${
                  disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'
                } ${
                  checked
                    ? 'bg-ed-control border-ed-border-strong text-ed-text-main'
                    : 'bg-ed-void border-ed-border-subtle text-ed-text-muted hover:text-ed-text-main'
                }`}
              >
                <SquareCheckbox checked={checked} size="xs" />
                <span className="truncate">{card.label}</span>
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}

