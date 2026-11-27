import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { vi } from 'vitest'
import { app, expedition, modes, realization, replay, stats } from '../api/models'
import {
  WaypointRowItem,
  type WaypointRowHandlers,
  type WaypointRowItemProps,
} from '../features/planner/table/WaypointRowItem'
import type {
  DataChangedDispatchContext,
  SyncBootLifecycleState,
} from '../features/sync/syncEventRouter'

/**
 * MakeRowViewOptions configures synthetic expedition.RowView test fixtures.
 */
export interface MakeRowViewOptions {
  row?: Partial<expedition.Row>
  number?: number
  visitState?: string
  visitedAtGalactic?: string
  departedAtGalactic?: string
  distFromPrevLy?: number
  distFromCurrentLy?: number
  isNextWaypoint?: boolean
  legModeId?: number
  legModeName?: string
  legModeColor?: string
  regionName?: string
  detectedRegion?: string
  visitCount?: number
  drawerFrom?: string
  drawerTo?: string
  allowInsertWaypoint?: boolean
  permissions?: Partial<expedition.WaypointPermissions>
}

/**
 * MakePlanViewOptions configures synthetic expedition.PlanView test fixtures.
 */
export type MakePlanViewOptions = Omit<
  Partial<expedition.PlanView>,
  'expedition' | 'rows' | 'modes' | 'activeCarrierRoute' | 'convertValues'
> & {
  expedition?: Partial<expedition.Expedition>
  rows?: Array<expedition.RowView | MakeRowViewOptions>
  modes?: Array<modes.Mode | Partial<modes.Mode>>
  activeCarrierRoute?:
    | expedition.ActiveCarrierRouteState
    | Partial<expedition.ActiveCarrierRouteState>
}

/**
 * MakeWaypointDetailsOptions configures synthetic expedition.WaypointDetails
 * test fixtures.
 */
export type MakeWaypointDetailsOptions = Omit<
  Partial<expedition.WaypointDetails>,
  'metrics' | 'realization' | 'convertValues'
> & {
  metrics?: Partial<stats.Metrics>
  realization?: Partial<realization.Breakdown>
}

/**
 * MakeSidebarStatsOptions configures synthetic app.SidebarStats test fixtures.
 */
export type MakeSidebarStatsOptions = Omit<
  Partial<app.SidebarStats>,
  | 'expedition'
  | 'allJournals'
  | 'carrierCost'
  | 'expeditionRealization'
  | 'allRealization'
  | 'convertValues'
> & {
  expedition?: Partial<stats.Metrics>
  allJournals?: Partial<stats.Metrics>
  carrierCost?: Partial<expedition.CarrierCost>
  expeditionRealization?: Partial<realization.Breakdown>
  allRealization?: Partial<realization.Breakdown>
}

/**
 * MakeCommanderOptions configures synthetic replay.Commander test fixtures.
 */
export type MakeCommanderOptions = Omit<
  Partial<replay.Commander>,
  'aliases' | 'convertValues'
> & {
  aliases?: Array<replay.CommanderAlias | Partial<replay.CommanderAlias>>
}

/**
 * MakeSettingsOptions configures synthetic app.Settings test fixtures.
 */
export type MakeSettingsOptions = Omit<
  Partial<app.Settings>,
  'commanders' | 'convertValues'
> & {
  commanders?: Array<replay.Commander | MakeCommanderOptions>
}

/**
 * makeMode builds a hydrated modes.Mode instance with deterministic defaults.
 */
export function makeMode(overrides: Partial<modes.Mode> = {}): modes.Mode {
  return modes.Mode.createFrom({
    id: 1,
    name: 'Travel',
    description: '',
    color: '#FF7100',
    isDefault: true,
    sortOrder: 0,
    ...overrides,
  })
}

/**
 * makeRowView builds a hydrated expedition.RowView with realistic visit and
 * permission defaults.
 */
export function makeRowView(options: MakeRowViewOptions = {}): expedition.RowView {
  const visitState = options.visitState ?? 'future'
  const number = options.number ?? 2
  const isVisited = visitState === 'visited'
  const isOrigin = number === 1 && isVisited
  return expedition.RowView.createFrom({
    row: {
      id: 10,
      expeditionId: 1,
      rowType: 'waypoint',
      systemName: 'Sol',
      ...options.row,
    },
    number,
    visitState,
    visitedAtGalactic: options.visitedAtGalactic ?? '',
    departedAtGalactic: options.departedAtGalactic,
    distFromPrevLy: options.distFromPrevLy,
    distFromCurrentLy: options.distFromCurrentLy,
    isNextWaypoint: options.isNextWaypoint,
    legModeId: options.legModeId ?? 0,
    legModeName: options.legModeName ?? '',
    legModeColor: options.legModeColor ?? '',
    regionName: options.regionName,
    detectedRegion: options.detectedRegion ?? '',
    visitCount: options.visitCount ?? 0,
    drawerFrom: options.drawerFrom,
    drawerTo: options.drawerTo,
    allowInsertWaypoint: options.allowInsertWaypoint ?? true,
    permissions: {
      systemLocked: isVisited || isOrigin,
      allowInsertWaypoint: options.allowInsertWaypoint ?? true,
      canReorder: !isOrigin,
      deleteDisabledReason: isOrigin ? 'Origin waypoint cannot be deleted' : undefined,
      reorderDisabledReason: isOrigin ? 'Origin waypoint cannot be reordered' : undefined,
      canAddFcStop: true,
      canRemoveFcStop: true,
      canOverrideFcBadge: true,
      hasDrawer: visitState !== 'future',
      ...options.permissions,
    },
  })
}

/**
 * makeOriginRowView builds a visited Waypoint #1 origin RowView fixture.
 */
export function makeOriginRowView(options: MakeRowViewOptions = {}): expedition.RowView {
  return makeRowView({
    ...options,
    number: options.number ?? 1,
    visitState: options.visitState ?? 'visited',
    row: {
      id: 1,
      expeditionId: 1,
      rowType: 'waypoint',
      systemName: 'Sol',
      ...options.row,
    },
  })
}

/**
 * makePlanView builds a hydrated expedition.PlanView instance.
 */
export function makePlanView(options: MakePlanViewOptions = {}): expedition.PlanView {
  const hydratedRows = options.rows?.map((entry) =>
    entry instanceof expedition.RowView ? entry : expedition.RowView.createFrom(entry),
  )
  const hydratedModes = options.modes?.map((entry) =>
    entry instanceof modes.Mode ? entry : makeMode(entry),
  )
  return expedition.PlanView.createFrom({
    ...options,
    expedition: {
      id: 1,
      name: 'Test Expedition',
      description: '',
      status: 'active',
      ...options.expedition,
    },
    rows: hydratedRows ?? [],
    modes: hydratedModes ?? [],
  })
}

/**
 * makeMetrics builds a hydrated stats.Metrics instance with zeroed counters.
 */
export function makeMetrics(overrides: Partial<stats.Metrics> = {}): stats.Metrics {
  return stats.Metrics.createFrom({
    distanceLy: 0,
    shipDistanceLy: 0,
    carrierPassengerLy: 0,
    jumps: 0,
    carrierJumps: 0,
    carrierDistanceLy: 0,
    jetConeBoosts: 0,
    premiumBoosts: 0,
    standardBoosts: 0,
    basicBoosts: 0,
    bodiesScanned: 0,
    bodiesMapped: 0,
    efficientMappings: 0,
    scanValueTotal: 0,
    scanValueBase: 0,
    scanValueBonus: 0,
    scanValueEfficiencyBonus: 0,
    firstDiscoveries: 0,
    firstMapped: 0,
    organicSamples: 0,
    organicValueTotal: 0,
    organicValueBase: 0,
    organicValueBonus: 0,
    firstLogged: 0,
    ...overrides,
  })
}

/**
 * makeRealization builds a hydrated realization.Breakdown instance with zeroed
 * settlement and loss counters.
 */
export function makeRealization(
  overrides: Partial<realization.Breakdown> = {},
): realization.Breakdown {
  return realization.Breakdown.createFrom({
    totalRealized: 0,
    cartoLostDeath: 0,
    organicLostDeath: 0,
    cartoStolen: 0,
    organicStolen: 0,
    carrierTariffLoss: 0,
    npcCrewLoss: 0,
    pendingValue: 0,
    ...overrides,
  })
}

/**
 * makeWaypointDetails builds a hydrated expedition.WaypointDetails instance.
 */
export function makeWaypointDetails(
  options: MakeWaypointDetailsOptions = {},
): expedition.WaypointDetails {
  return expedition.WaypointDetails.createFrom({
    rowId: 10,
    kind: 'leg',
    number: 2,
    ...options,
    metrics: makeMetrics(options.metrics),
    realization: options.realization ? makeRealization(options.realization) : undefined,
  })
}

/**
 * makeSidebarStats builds a hydrated app.SidebarStats instance.
 */
export function makeSidebarStats(options: MakeSidebarStatsOptions = {}): app.SidebarStats {
  return app.SidebarStats.createFrom({
    expeditionId: 1,
    status: 'active',
    ...options,
  })
}

/**
 * makeCommander builds a hydrated replay.Commander save-slot profile fixture.
 */
export function makeCommander(options: MakeCommanderOptions = {}): replay.Commander {
  return replay.Commander.createFrom({
    id: 1,
    profileKey: 'live:f100:era:0',
    name: 'Jameson',
    fid: 'F100',
    universe: 'live',
    eraIndex: 0,
    isArchivedCareer: false,
    hasManualOverride: false,
    enabled: true,
    eventCount: 100,
    aliases: [],
    ...options,
  })
}

/**
 * makeSettings builds a hydrated app.Settings instance.
 */
export function makeSettings(options: MakeSettingsOptions = {}): app.Settings {
  return app.Settings.createFrom({
    setupCompleted: true,
    ...options,
  })
}

/**
 * makeWaypointRowHandlers creates no-op WaypointRowHandlers merged with any
 * test-specific callback spies.
 */
export function makeWaypointRowHandlers(
  overrides: Partial<WaypointRowHandlers> = {},
): WaypointRowHandlers {
  return {
    onToggleExpand: () => {},
    onFieldBlur: () => {},
    onCopySystem: () => {},
    onToggleFcStop: () => {},
    onOpenCarrierRoutePrompt: () => {},
    onToggleInsertMenu: () => {},
    onInsert: () => {},
    onDelete: () => {},
    onDragStart: () => {},
    onDragEnd: () => {},
    ...overrides,
  }
}

/**
 * renderWaypointRowHtml renders a single WaypointRowItem inside a valid table
 * wrapper and returns its static HTML string.
 */
export function renderWaypointRowHtml(
  props: Partial<WaypointRowItemProps> & { rowView: expedition.RowView },
): string {
  const resolvedProps: WaypointRowItemProps = {
    index: 1,
    isPlanning: false,
    isLocked: false,
    isBeingDragged: false,
    isExpanded: false,
    isSystemCopied: false,
    insertMenuOpen: false,
    lastVisitedIndex: 0,
    rowStyle: {},
    dropProps: {},
    handlers: makeWaypointRowHandlers(),
    ...props,
  }
  return renderToString(
    createElement(
      'table',
      null,
      createElement('tbody', null, createElement(WaypointRowItem, resolvedProps)),
    ),
  )
}

/**
 * createSyncDispatchHarness builds spy-backed DataChangedDispatchContext and
 * EDDiscovery progress mocks shared across sync router and pause tests.
 */
export function createSyncDispatchHarness(
  bootOverrides: Partial<SyncBootLifecycleState> = {},
  expeditionId = 4,
) {
  const filter = stats.Filter.createFrom({ enabled: false, modes: [] })
  const refreshExpeditionListOnly = vi.fn()
  const loadIngestStatusOnly = vi.fn(async () => null)
  const loadSettingsOnly = vi.fn(async () => null)
  const loadModesOnly = vi.fn(async () => null)
  const loadPlanOnly = vi.fn(async () => {})
  const loadStatsOnly = vi.fn(async () => {})
  const refreshAll = vi.fn()
  const applyEddProgress = vi.fn()

  const dispatchContext: DataChangedDispatchContext = {
    bootState: {
      initialBootComplete: true,
      bootCoreLoaded: true,
      deferredPayloads: [],
      ...bootOverrides,
    },
    currentActiveExpeditionId: expeditionId,
    currentFilter: filter,
    refreshExpeditionListOnly,
    loadIngestStatusOnly,
    loadSettingsOnly,
    loadModesOnly,
    loadPlanOnly,
    loadStatsOnly,
    refreshAll,
  }

  return {
    filter,
    dispatchContext,
    refreshExpeditionListOnly,
    loadIngestStatusOnly,
    loadSettingsOnly,
    loadModesOnly,
    loadPlanOnly,
    loadStatsOnly,
    refreshAll,
    applyEddProgress,
  }
}
