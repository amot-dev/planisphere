import { expedition } from '../../api/models'
import {
  DetailsSectionSelection,
  ExpeditionExportData,
  ExportLegTelemetry,
  ExportRegionBucket,
  ExportScopeSpec,
} from './exportTypes'

/** Visited non-origin leg enriched with its drawer details, Go leg telemetry, and attributed Galactic Region. */
export interface VisitedLegRecord {
  rowView: expedition.RowView
  details: expedition.WaypointDetails
  regionName: string
  telemetry: ExportLegTelemetry
}

/** Distinct visited Galactic Region grouping its attributed visited legs, active flight modes, and Go bucket. */
export interface VisitedRegionSummary {
  regionName: string
  legs: VisitedLegRecord[]
  distinctModes: ActiveModeTag[]
  bucket: ExportRegionBucket
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

function buildExportDataIndex(exportData: ExpeditionExportData): ExportDataIndex {
  const { visitedLegs, visitedLegByRowId } = indexVisitedLegs(exportData)
  const { visitedRegions, visitedRegionByName, allRegionsDistinctModes } = indexVisitedRegions(
    exportData,
    visitedLegByRowId,
  )
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
  const rowViewById = new Map<number, expedition.RowView>()
  for (const rowView of exportData.plan.rows ?? []) {
    rowViewById.set(rowView.row.id, rowView)
  }
  const detailsByRowId = new Map<number, expedition.WaypointDetails>()
  for (const detailEntry of exportData.rowDetails ?? []) {
    detailsByRowId.set(detailEntry.rowId, detailEntry)
  }

  const visitedLegs: VisitedLegRecord[] = []
  const visitedLegByRowId = new Map<number, VisitedLegRecord>()
  for (const legTelemetry of exportData.legTelemetry ?? []) {
    const rowView = rowViewById.get(legTelemetry.rowId)
    const details = detailsByRowId.get(legTelemetry.rowId)
    if (!rowView || !details) continue
    const legRecord: VisitedLegRecord = {
      rowView,
      details,
      regionName: legTelemetry.regionName,
      telemetry: legTelemetry,
    }
    visitedLegs.push(legRecord)
    visitedLegByRowId.set(legTelemetry.rowId, legRecord)
  }
  return { visitedLegs, visitedLegByRowId }
}

function indexVisitedRegions(
  exportData: ExpeditionExportData,
  visitedLegByRowId: ReadonlyMap<number, VisitedLegRecord>,
): {
  visitedRegions: VisitedRegionSummary[]
  visitedRegionByName: Map<string, VisitedRegionSummary>
  allRegionsDistinctModes: ActiveModeTag[]
} {
  const visitedRegions: VisitedRegionSummary[] = []
  const visitedRegionByName = new Map<string, VisitedRegionSummary>()
  const allRegionModesById = new Map<number, ActiveModeTag>()

  for (const bucket of exportData.regionBuckets ?? []) {
    const legs: VisitedLegRecord[] = []
    for (const rowId of bucket.legRowIds ?? []) {
      const legRecord = visitedLegByRowId.get(rowId)
      if (legRecord) legs.push(legRecord)
    }
    const distinctModes: ActiveModeTag[] = (bucket.modeBuckets ?? []).map((modeBucket) => ({
      modeId: modeBucket.modeId,
      modeName: modeBucket.modeName,
      modeColor: modeBucket.modeColor,
    }))
    const regionSummary: VisitedRegionSummary = {
      regionName: bucket.regionName,
      legs,
      distinctModes,
      bucket,
    }
    visitedRegions.push(regionSummary)
    visitedRegionByName.set(bucket.regionName, regionSummary)
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
