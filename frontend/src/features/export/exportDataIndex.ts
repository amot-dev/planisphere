import { expedition } from '../../api/models'
import { DEFAULT_MODE_COLOR } from '../../domain/theme'
import {
  DetailsSectionSelection,
  ExpeditionExportData,
  ExportScopeSpec,
} from './exportTypes'

/** Visited non-origin leg enriched with its drawer details and attributed Galactic Region. */
export interface VisitedLegRecord {
  rowView: expedition.RowView
  details: expedition.WaypointDetails
  regionName: string
}

/** Distinct visited Galactic Region grouping its attributed visited legs and active flight modes. */
export interface VisitedRegionSummary {
  regionName: string
  legs: VisitedLegRecord[]
  distinctModes: ActiveModeTag[]
}

/** Lightweight flight mode identifier paired with display name and cockpit color. */
export interface ActiveModeTag {
  modeId: number
  modeName: string
  modeColor: string
}

/** Pre-indexed leg, region, and flight-mode lookup structures cached per exportData instance. */
export interface ExportDataIndex {
  visitedLegs: VisitedLegRecord[]
  visitedLegByRowId: Map<number, VisitedLegRecord>
  visitedRegions: VisitedRegionSummary[]
  visitedRegionByName: Map<string, VisitedRegionSummary>
  fullDistinctModes: ActiveModeTag[]
  allRegionsDistinctModes: ActiveModeTag[]
}

const exportDataIndexCache = new WeakMap<ExpeditionExportData, ExportDataIndex>()
const EMPTY_MODE_TAGS: ActiveModeTag[] = []

/** Returns the WeakMap-cached ExportDataIndex for exportData, building it once on first access. */
export function getExportDataIndex(exportData: ExpeditionExportData): ExportDataIndex {
  const cachedIndex = exportDataIndexCache.get(exportData)
  if (cachedIndex) {
    return cachedIndex
  }
  const builtIndex = buildExportDataIndex(exportData)
  exportDataIndexCache.set(exportData, builtIndex)
  return builtIndex
}

/** Returns ordered visited non-origin legs (WP #2+) paired with their drawer telemetry and attributed Galactic Region. */
export function getVisitedLegs(exportData: ExpeditionExportData): VisitedLegRecord[] {
  return getExportDataIndex(exportData).visitedLegs
}

/** Looks up a visited non-origin leg by row ID in O(1). */
export function getVisitedLegByRowId(
  exportData: ExpeditionExportData,
  rowId: number | undefined,
): VisitedLegRecord | undefined {
  return rowId != null ? getExportDataIndex(exportData).visitedLegByRowId.get(rowId) : undefined
}

/** Groups visited legs by distinct Galactic Region in order of first arrival. */
export function getVisitedRegions(exportData: ExpeditionExportData): VisitedRegionSummary[] {
  return getExportDataIndex(exportData).visitedRegions
}

/** Looks up a visited Galactic Region summary by region name in O(1). */
export function getVisitedRegionByName(
  exportData: ExpeditionExportData,
  regionName: string | undefined,
): VisitedRegionSummary | undefined {
  return regionName != null
    ? getExportDataIndex(exportData).visitedRegionByName.get(regionName)
    : undefined
}

/** Returns the distinct active flight modes spanning the requested scope in O(1). */
export function getDistinctModesForScope(
  exportData: ExpeditionExportData,
  scope: ExportScopeSpec,
): ActiveModeTag[] {
  const dataIndex = getExportDataIndex(exportData)
  switch (scope.kind) {
    case 'baseline':
    case 'leg':
    case 'allLegs':
      return EMPTY_MODE_TAGS
    case 'full':
      return dataIndex.fullDistinctModes
    case 'region':
      return (
        (scope.regionName != null
          ? dataIndex.visitedRegionByName.get(scope.regionName)?.distinctModes
          : undefined) ?? EMPTY_MODE_TAGS
      )
    case 'allRegions':
      return dataIndex.allRegionsDistinctModes
  }
}

/** Reports whether the Combined / Separate mode control should appear for a scope and optional section. */
export function scopeSupportsModeSplit(
  exportData: ExpeditionExportData,
  scope: ExportScopeSpec,
  detailsSection?: DetailsSectionSelection,
): boolean {
  if (detailsSection === 'carrier') {
    return false
  }
  return getDistinctModesForScope(exportData, scope).length >= 2
}

/** Collects distinct non-empty ship hull names across ordered visited legs. */
export function collectUniqueShips(legs: ReadonlyArray<VisitedLegRecord>): string[] {
  const uniqueShips = new Set<string>()
  for (const leg of legs) {
    for (const shipName of leg.details.shipsUsed ?? []) {
      const trimmed = shipName.trim()
      if (trimmed) uniqueShips.add(trimmed)
    }
  }
  return Array.from(uniqueShips)
}

function buildExportDataIndex(exportData: ExpeditionExportData): ExportDataIndex {
  const { visitedLegs, visitedLegByRowId } = indexVisitedLegs(exportData)
  const { visitedRegions, visitedRegionByName, allRegionsDistinctModes } =
    indexVisitedRegions(visitedLegs)
  const fullDistinctModes = (exportData.modeBuckets ?? []).map((bucket) => ({
    modeId: bucket.modeId,
    modeName: bucket.modeName,
    modeColor: bucket.modeColor,
  }))
  return {
    visitedLegs,
    visitedLegByRowId,
    visitedRegions,
    visitedRegionByName,
    fullDistinctModes,
    allRegionsDistinctModes,
  }
}

function indexVisitedLegs(exportData: ExpeditionExportData): {
  visitedLegs: VisitedLegRecord[]
  visitedLegByRowId: Map<number, VisitedLegRecord>
} {
  const detailsByRowId = new Map<number, expedition.WaypointDetails>()
  for (const detailEntry of exportData.rowDetails ?? []) {
    detailsByRowId.set(detailEntry.rowId, detailEntry)
  }

  let activeManualRegion = ''
  const visitedLegs: VisitedLegRecord[] = []
  const visitedLegByRowId = new Map<number, VisitedLegRecord>()
  for (const rowView of exportData.plan.rows ?? []) {
    if (rowView.row.rowType === 'region') {
      activeManualRegion = rowView.regionName || activeManualRegion
      continue
    }
    if (rowView.number <= 1 || !rowView.row.visitedAt) {
      continue
    }
    const details = detailsByRowId.get(rowView.row.id)
    if (!details) {
      continue
    }
    const attributedRegion = rowView.detectedRegion || activeManualRegion || 'Unknown Region'
    const legRecord: VisitedLegRecord = { rowView, details, regionName: attributedRegion }
    visitedLegs.push(legRecord)
    visitedLegByRowId.set(rowView.row.id, legRecord)
  }
  return { visitedLegs, visitedLegByRowId }
}

function indexVisitedRegions(legs: VisitedLegRecord[]): {
  visitedRegions: VisitedRegionSummary[]
  visitedRegionByName: Map<string, VisitedRegionSummary>
  allRegionsDistinctModes: ActiveModeTag[]
} {
  const regionLegsByName = new Map<string, VisitedLegRecord[]>()
  for (const leg of legs) {
    const existingLegs = regionLegsByName.get(leg.regionName)
    if (existingLegs) {
      existingLegs.push(leg)
    } else {
      regionLegsByName.set(leg.regionName, [leg])
    }
  }
  const visitedRegions: VisitedRegionSummary[] = []
  const visitedRegionByName = new Map<string, VisitedRegionSummary>()
  const allRegionModesById = new Map<number, ActiveModeTag>()
  for (const [regionName, regionLegs] of regionLegsByName.entries()) {
    const distinctModes = extractDistinctModesFromLegs(regionLegs)
    const regionSummary: VisitedRegionSummary = { regionName, legs: regionLegs, distinctModes }
    visitedRegions.push(regionSummary)
    visitedRegionByName.set(regionName, regionSummary)
    for (const modeTag of distinctModes) {
      allRegionModesById.set(modeTag.modeId, modeTag)
    }
  }
  return {
    visitedRegions,
    visitedRegionByName,
    allRegionsDistinctModes: Array.from(allRegionModesById.values()),
  }
}

function extractDistinctModesFromLegs(legs: VisitedLegRecord[]): ActiveModeTag[] {
  const modesById = new Map<number, ActiveModeTag>()
  for (const leg of legs) {
    if (leg.rowView.legModeId > 0 && !modesById.has(leg.rowView.legModeId)) {
      modesById.set(leg.rowView.legModeId, {
        modeId: leg.rowView.legModeId,
        modeName: leg.rowView.legModeName || 'Travel',
        modeColor: leg.rowView.legModeColor || DEFAULT_MODE_COLOR,
      })
    }
  }
  return Array.from(modesById.values())
}
