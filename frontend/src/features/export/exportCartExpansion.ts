import {
  getDistinctModesForScope,
  getVisitedLegByRowId,
  getVisitedLegs,
  getVisitedRegions,
  VisitedLegRecord,
} from './exportDataIndex'
import { slugify, slugifyCamelCase } from './exportFormatting'
import {
  AllDetailsSectionsConfig,
  CartBlockItem,
  ConfiguratorBlockConfig,
  defaultMetricsForSection,
  DETAILS_SECTIONS,
  DetailsBlockConfig,
  DetailsMetricId,
  DetailsSection,
  ExpeditionExportData,
  ExportBlockConfig,
  ExportScopeSpec,
  HIGHLIGHT_CARDS,
  WAYPOINTS_COLUMNS,
} from './exportTypes'

type ConcreteTelemetryConfig = Extract<ExportBlockConfig, { kind: 'summary' | 'details' }>

interface DetailsSectionExpansionMeta {
  section: DetailsSection
  blockLabel: string
  blockSlug: string
  defaultMetrics: ReadonlyArray<DetailsMetricId>
}

const SUMMARY_EXPANSION_META = {
  blockLabel: 'Summary',
  blockSlug: 'summary',
} as const

const DETAILS_SECTION_EXPANSION_META: ReadonlyArray<DetailsSectionExpansionMeta> =
  DETAILS_SECTIONS.map((sectionMeta) => ({
    section: sectionMeta.id,
    blockLabel: `Details: ${sectionMeta.label}`,
    blockSlug: slugifyCamelCase(sectionMeta.id),
    defaultMetrics: defaultMetricsForSection(sectionMeta.id),
  }))

const DETAILS_SECTION_EXPANSION_BY_ID: Readonly<
  Record<DetailsSection, DetailsSectionExpansionMeta>
> = Object.fromEntries(
  DETAILS_SECTION_EXPANSION_META.map((entry) => [entry.section, entry]),
) as Record<DetailsSection, DetailsSectionExpansionMeta>

/** Expands a configurator block (including All Sections, All Legs, All Regions, and Separate modes) into concrete cart items. */
export function expandConfigToCartItems(
  exportData: ExpeditionExportData,
  config: ConfiguratorBlockConfig,
  idPrefix: string,
): CartBlockItem[] {
  if (config.kind === 'waypoints') {
    const scopeText = config.waypointsScope === 'fullPlan' ? 'Full Plan' : 'Visited Only'
    return [{
      id: `${idPrefix}-waypoints`,
      title: `Waypoints (${scopeText})`,
      subtitle: `${config.selectedColumns.length} columns`,
      filenameSlug: `waypoints-${slugify(scopeText)}`,
      config: { ...config, selectedColumns: [...config.selectedColumns] },
    }]
  }
  if (config.kind === 'highlights') {
    return [{
      id: `${idPrefix}-highlights`,
      title: `Highlights (Top ${config.depth})`,
      subtitle: `${config.selectedCards.length} cards`,
      filenameSlug: `highlights-top-${config.depth}`,
      config: { ...config, selectedCards: [...config.selectedCards] },
    }]
  }
  if (config.kind === 'details' && config.section === 'all') {
    return expandAllDetailsSections(exportData, config, idPrefix)
  }
  return expandTelemetryBlockConfig(exportData, config, idPrefix)
}

/** Builds the Quick Share 3-card preset cart (Summary + Highlights Top 1 + Waypoints). */
export function createQuickShareCart(exportData: ExpeditionExportData): CartBlockItem[] {
  const items: CartBlockItem[] = [
    ...expandConfigToCartItems(
      exportData,
      { kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'combined', careerDiff: 'percentage' },
      'quick-1',
    ),
  ]
  if ((exportData.plan.visitedWaypointCount ?? 0) >= 2) {
    const selectedCards = HIGHLIGHT_CARDS.filter((card) => card.defaultChecked).map((card) => card.id)
    items.push(
      ...expandConfigToCartItems(
        exportData,
        { kind: 'highlights', depth: 1, selectedCards, lucrativeMetric: 'estimated', distanceMetric: 'total' },
        'quick-2',
      ),
    )
  }
  const selectedColumns = WAYPOINTS_COLUMNS.filter((column) => column.defaultChecked).map(
    (column) => column.id,
  )
  items.push(
    ...expandConfigToCartItems(
      exportData,
      { kind: 'waypoints', waypointsScope: 'visited', selectedColumns },
      'quick-3',
    ),
  )
  return items
}

/** Resolves human-readable title and filename slug metadata for a scope specification in O(1). */
export function describeScope(
  exportData: ExpeditionExportData,
  scope: ExportScopeSpec,
): { label: string; slug: string } {
  switch (scope.kind) {
    case 'full':
      return { label: 'Full Expedition', slug: 'full-expedition' }
    case 'baseline':
      return { label: 'Pre-Expedition Baseline', slug: 'pre-expedition-baseline' }
    case 'leg': {
      const leg = getVisitedLegByRowId(exportData, scope.rowId)
      return leg
        ? { label: `Leg #${leg.details.number - 1} to #${leg.details.number}`, slug: `wp${leg.details.number}` }
        : { label: 'Selected Leg', slug: 'selected-leg' }
    }
    case 'region': {
      const label = scope.regionName || 'Selected Region'
      return { label, slug: slugify(label) }
    }
    case 'allLegs':
      return { label: 'All Legs', slug: 'all-legs' }
    case 'allRegions':
      return { label: 'All Galactic Regions', slug: 'all-galactic-regions' }
  }
}

function expandAllDetailsSections(
  exportData: ExpeditionExportData,
  config: AllDetailsSectionsConfig,
  idPrefix: string,
): CartBlockItem[] {
  const allowCarrier = includeCarrierSection(exportData, config)
  const applicableSections = DETAILS_SECTION_EXPANSION_META.filter(
    (entry) => entry.section !== 'carrier' || allowCarrier,
  )
  if (config.scope.kind === 'allLegs') {
    return expandAllLegsDetailsSections(exportData, applicableSections, idPrefix)
  }
  const perSectionItems = applicableSections.map((entry) =>
    expandTelemetryBlockConfig(
      exportData,
      { ...config, section: entry.section, selectedMetrics: [...entry.defaultMetrics] },
      `${idPrefix}-${entry.section}`,
    ),
  )
  // Transpose into entity-outer and section-inner order so all applicable sections
  // for each region or flight mode stay grouped together before the next slice.
  const sliceCount = perSectionItems[0]?.length ?? 0
  const orderedItems: CartBlockItem[] = []
  for (let sliceIndex = 0; sliceIndex < sliceCount; sliceIndex++) {
    for (const sectionItems of perSectionItems) {
      if (sectionItems[sliceIndex]) {
        orderedItems.push(sectionItems[sliceIndex])
      }
    }
  }
  return orderedItems
}

function expandAllLegsDetailsSections(
  exportData: ExpeditionExportData,
  applicableSections: ReadonlyArray<DetailsSectionExpansionMeta>,
  idPrefix: string,
): CartBlockItem[] {
  const items: CartBlockItem[] = []
  // Iterate entity-outer (visited leg) and section-inner so each leg's sections stay grouped.
  for (const [legIndex, leg] of getVisitedLegs(exportData).entries()) {
    for (const sectionEntry of applicableSections) {
      const legConfig: DetailsBlockConfig = {
        kind: 'details',
        section: sectionEntry.section,
        scope: { kind: 'leg', rowId: leg.rowView.row.id },
        modeGrouping: 'combined',
        selectedMetrics: [...sectionEntry.defaultMetrics],
      }
      items.push(
        buildLegCartItem(
          leg,
          legIndex,
          legConfig,
          `${idPrefix}-${sectionEntry.section}`,
          sectionEntry.blockLabel,
          sectionEntry.blockSlug,
        ),
      )
    }
  }
  return items
}

function includeCarrierSection(exportData: ExpeditionExportData, config: AllDetailsSectionsConfig): boolean {
  if (config.scope.kind !== 'full') return false
  if (config.modeGrouping === 'separate' && getDistinctModesForScope(exportData, config.scope).length >= 2) {
    return false
  }
  return (exportData.expedition.metrics.carrierJumps ?? 0) > 0 || (exportData.carrierCost?.totalCost ?? 0) > 0
}

function buildLegCartItem(
  leg: VisitedLegRecord,
  legIndex: number,
  config: ConcreteTelemetryConfig,
  idPrefix: string,
  blockLabel: string,
  blockSlug: string,
): CartBlockItem {
  return {
    id: `${idPrefix}-leg-${leg.rowView.row.id}-${legIndex}`,
    title: `${blockLabel} — Leg #${leg.details.number - 1} to #${leg.details.number}`,
    subtitle: `${leg.details.prevSystem || 'Origin'} to ${leg.rowView.row.systemName}`,
    filenameSlug: `wp${leg.details.number}-${blockSlug}`,
    config: cloneConcreteTelemetryConfig(config, { kind: 'leg', rowId: leg.rowView.row.id }, undefined, 'hide'),
  }
}

function expandTelemetryBlockConfig(
  exportData: ExpeditionExportData,
  config: ConcreteTelemetryConfig,
  idPrefix: string,
): CartBlockItem[] {
  const { blockLabel, blockSlug } =
    config.kind === 'summary'
      ? SUMMARY_EXPANSION_META
      : DETAILS_SECTION_EXPANSION_BY_ID[config.section]

  if (config.scope.kind === 'allLegs') {
    return getVisitedLegs(exportData).map((leg, index) =>
      buildLegCartItem(leg, index, config, idPrefix, blockLabel, blockSlug),
    )
  }
  if (config.scope.kind === 'allRegions') {
    return expandAllRegionsItems(exportData, config, idPrefix, blockLabel, blockSlug)
  }
  return expandSingleScopeItems(exportData, config, idPrefix, blockLabel, blockSlug)
}

function expandAllRegionsItems(
  exportData: ExpeditionExportData,
  config: ConcreteTelemetryConfig,
  idPrefix: string,
  blockLabel: string,
  blockSlug: string,
): CartBlockItem[] {
  const items: CartBlockItem[] = []
  for (const region of getVisitedRegions(exportData)) {
    const regionSlug = slugify(region.regionName)
    const regionScope: ExportScopeSpec = { kind: 'region', regionName: region.regionName }
    const splitModes =
      config.modeGrouping === 'separate' &&
      (config.kind !== 'details' || config.section !== 'carrier') &&
      region.distinctModes.length >= 2
    if (splitModes) {
      for (const modeTag of region.distinctModes) {
        const modeLegs = region.legs.filter((leg) => leg.rowView.legModeId === modeTag.modeId).length
        items.push({
          id: `${idPrefix}-region-${regionSlug}-mode-${modeTag.modeId}`,
          title: `${blockLabel} — ${region.regionName} (${modeTag.modeName})`,
          subtitle: `${modeLegs} legs`,
          filenameSlug: `${regionSlug}-${slugify(modeTag.modeName)}-${blockSlug}`,
          config: cloneConcreteTelemetryConfig(config, regionScope, modeTag.modeId, 'hide'),
        })
      }
      continue
    }
    items.push({
      id: `${idPrefix}-region-${regionSlug}`,
      title: `${blockLabel} — ${region.regionName}`,
      subtitle: `${region.legs.length} legs`,
      filenameSlug: `${regionSlug}-${blockSlug}`,
      config: cloneConcreteTelemetryConfig(config, regionScope, undefined, 'hide'),
    })
  }
  return items
}

function expandSingleScopeItems(
  exportData: ExpeditionExportData,
  config: ConcreteTelemetryConfig,
  idPrefix: string,
  blockLabel: string,
  blockSlug: string,
): CartBlockItem[] {
  const availableModes = getDistinctModesForScope(exportData, config.scope)
  const shouldSplit =
    config.modeGrouping === 'separate' &&
    (config.kind !== 'details' || config.section !== 'carrier') &&
    availableModes.length >= 2
  const { label: scopeText, slug: scopeSlug } = describeScope(exportData, config.scope)

  if (shouldSplit) {
    return availableModes.map((modeTag) => ({
      id: `${idPrefix}-mode-${modeTag.modeId}`,
      title: `${blockLabel} — ${scopeText} (${modeTag.modeName})`,
      subtitle: modeTag.modeName,
      filenameSlug: `${scopeSlug}-${slugify(modeTag.modeName)}-${blockSlug}`,
      config: cloneConcreteTelemetryConfig(config, { ...config.scope }, modeTag.modeId, 'hide'),
    }))
  }
  const baseCareerDiff = config.kind === 'summary' ? config.careerDiff : undefined
  return [{
    id: `${idPrefix}-single`,
    title: `${blockLabel} — ${scopeText}`,
    subtitle: config.scope.kind === 'full' ? 'Combined' : scopeText,
    filenameSlug: `${scopeSlug}-${blockSlug}`,
    config: cloneConcreteTelemetryConfig(
      config,
      { ...config.scope },
      undefined,
      config.scope.kind === 'full' ? baseCareerDiff : 'hide',
    ),
  }]
}

function cloneConcreteTelemetryConfig(
  config: ConcreteTelemetryConfig,
  concreteScope: ExportScopeSpec,
  selectedModeId: number | undefined,
  careerDiffOverride: 'hide' | 'absolute' | 'percentage' | 'both' | undefined,
): ConcreteTelemetryConfig {
  const modeGrouping = selectedModeId ? 'separate' : 'combined'
  if (config.kind === 'summary') {
    return {
      kind: 'summary',
      scope: concreteScope,
      modeGrouping,
      selectedModeId,
      careerDiff: careerDiffOverride ?? config.careerDiff,
    }
  }
  return {
    kind: 'details',
    section: config.section,
    scope: concreteScope,
    modeGrouping,
    selectedModeId,
    selectedMetrics: [...config.selectedMetrics],
  }
}
