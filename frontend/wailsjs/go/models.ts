export namespace app {
	
	export class IngestStatus {
	    scanning: boolean;
	    watcherActive: boolean;
	    watcherError: string;
	    lastError: string;
	    lastScanAt: string;
	    filesTracked: number;
	    eventsStored: number;
	    firstEventAt: string;
	    firstEventGalactic: string;
	    lastEventAt: string;
	    lastEventGalactic: string;
	    currentSystem: string;
	    currentShip: string;
	    closedWindowRevision: number;
	
	    static createFrom(source: any = {}) {
	        return new IngestStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scanning = source["scanning"];
	        this.watcherActive = source["watcherActive"];
	        this.watcherError = source["watcherError"];
	        this.lastError = source["lastError"];
	        this.lastScanAt = source["lastScanAt"];
	        this.filesTracked = source["filesTracked"];
	        this.eventsStored = source["eventsStored"];
	        this.firstEventAt = source["firstEventAt"];
	        this.firstEventGalactic = source["firstEventGalactic"];
	        this.lastEventAt = source["lastEventAt"];
	        this.lastEventGalactic = source["lastEventGalactic"];
	        this.currentSystem = source["currentSystem"];
	        this.currentShip = source["currentShip"];
	        this.closedWindowRevision = source["closedWindowRevision"];
	    }
	}
	export class ModesState {
	    modes: modes.Mode[];
	    defaultModeId: number;
	
	    static createFrom(source: any = {}) {
	        return new ModesState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.modes = this.convertValues(source["modes"], modes.Mode);
	        this.defaultModeId = source["defaultModeId"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RegionOptions {
	    anchorId: number;
	    anchorName: string;
	    anchorIsDefault: boolean;
	    suggestedRegionId: number;
	    options: reference.RegionOption[];
	
	    static createFrom(source: any = {}) {
	        return new RegionOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.anchorId = source["anchorId"];
	        this.anchorName = source["anchorName"];
	        this.anchorIsDefault = source["anchorIsDefault"];
	        this.suggestedRegionId = source["suggestedRegionId"];
	        this.options = this.convertValues(source["options"], reference.RegionOption);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Settings {
	    setupCompleted: boolean;
	    journalDir: string;
	    defaultJournalDir: string;
	    journalDirExists: boolean;
	    journalDebounceMs: number;
	    defaultJournalDebounceMs: number;
	    tritiumPricePerTonne: number;
	    defaultTritiumPricePerTonne: number;
	    onlineLookups: boolean;
	    externalLinkInara: string;
	    externalLinkEdsm: string;
	    externalLinkGec: string;
	    dbPath: string;
	    defaultDbPath: string;
	    activeExpeditionId: number;
	    gecCount: number;
	    gecFetchedAt: string;
	    commanders: replay.Commander[];
	    careerBaselineScope: string;
	    edDiscoveryDbPath: string;
	    defaultEdDiscoveryDbPath: string;
	    edDiscoveryDbExists: boolean;
	    latestVersion: string;
	    versionCheckFailed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.setupCompleted = source["setupCompleted"];
	        this.journalDir = source["journalDir"];
	        this.defaultJournalDir = source["defaultJournalDir"];
	        this.journalDirExists = source["journalDirExists"];
	        this.journalDebounceMs = source["journalDebounceMs"];
	        this.defaultJournalDebounceMs = source["defaultJournalDebounceMs"];
	        this.tritiumPricePerTonne = source["tritiumPricePerTonne"];
	        this.defaultTritiumPricePerTonne = source["defaultTritiumPricePerTonne"];
	        this.onlineLookups = source["onlineLookups"];
	        this.externalLinkInara = source["externalLinkInara"];
	        this.externalLinkEdsm = source["externalLinkEdsm"];
	        this.externalLinkGec = source["externalLinkGec"];
	        this.dbPath = source["dbPath"];
	        this.defaultDbPath = source["defaultDbPath"];
	        this.activeExpeditionId = source["activeExpeditionId"];
	        this.gecCount = source["gecCount"];
	        this.gecFetchedAt = source["gecFetchedAt"];
	        this.commanders = this.convertValues(source["commanders"], replay.Commander);
	        this.careerBaselineScope = source["careerBaselineScope"];
	        this.edDiscoveryDbPath = source["edDiscoveryDbPath"];
	        this.defaultEdDiscoveryDbPath = source["defaultEdDiscoveryDbPath"];
	        this.edDiscoveryDbExists = source["edDiscoveryDbExists"];
	        this.latestVersion = source["latestVersion"];
	        this.versionCheckFailed = source["versionCheckFailed"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SidebarStats {
	    expeditionId: number;
	    status: string;
	    started: boolean;
	    departed: boolean;
	    start: string;
	    end: string;
	    startGalactic: string;
	    endGalactic: string;
	    endIsNow: boolean;
	    duration: string;
	    careerStart: string;
	    careerStartGalactic: string;
	    careerDuration: string;
	    filterActive: boolean;
	    live: stats.Metrics;
	    adjustments: stats.Metrics;
	    expedition: stats.Metrics;
	    allJournals: stats.Metrics;
	    realization: realization.Breakdown;
	    allJournalsRealization: realization.Breakdown;
	    carrierCost: expedition.CarrierCost;
	
	    static createFrom(source: any = {}) {
	        return new SidebarStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.expeditionId = source["expeditionId"];
	        this.status = source["status"];
	        this.started = source["started"];
	        this.departed = source["departed"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.startGalactic = source["startGalactic"];
	        this.endGalactic = source["endGalactic"];
	        this.endIsNow = source["endIsNow"];
	        this.duration = source["duration"];
	        this.careerStart = source["careerStart"];
	        this.careerStartGalactic = source["careerStartGalactic"];
	        this.careerDuration = source["careerDuration"];
	        this.filterActive = source["filterActive"];
	        this.live = this.convertValues(source["live"], stats.Metrics);
	        this.adjustments = this.convertValues(source["adjustments"], stats.Metrics);
	        this.expedition = this.convertValues(source["expedition"], stats.Metrics);
	        this.allJournals = this.convertValues(source["allJournals"], stats.Metrics);
	        this.realization = this.convertValues(source["realization"], realization.Breakdown);
	        this.allJournalsRealization = this.convertValues(source["allJournalsRealization"], realization.Breakdown);
	        this.carrierCost = this.convertValues(source["carrierCost"], expedition.CarrierCost);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class StartCandidates {
	    systemName: string;
	    visits: replay.Arrival[];
	
	    static createFrom(source: any = {}) {
	        return new StartCandidates(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.systemName = source["systemName"];
	        this.visits = this.convertValues(source["visits"], replay.Arrival);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UndoActionResult {
	    applied: boolean;
	    label: string;
	    canUndo: boolean;
	    canRedo: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UndoActionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.applied = source["applied"];
	        this.label = source["label"];
	        this.canUndo = source["canUndo"];
	        this.canRedo = source["canRedo"];
	    }
	}
	export class WaypointImportFile {
	    fileName: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new WaypointImportFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileName = source["fileName"];
	        this.content = source["content"];
	    }
	}

}

export namespace expedition {
	
	export class CarrierRouteJump {
	    systemName: string;
	    distanceLy: number;
	    distanceToDestLy: number;
	    tritiumUsed: number;
	    fuelInTank: number;
	    hasIcyRing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CarrierRouteJump(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.systemName = source["systemName"];
	        this.distanceLy = source["distanceLy"];
	        this.distanceToDestLy = source["distanceToDestLy"];
	        this.tritiumUsed = source["tritiumUsed"];
	        this.fuelInTank = source["fuelInTank"];
	        this.hasIcyRing = source["hasIcyRing"];
	    }
	}
	export class CarrierRouteResult {
	    origin: string;
	    destination: string;
	    carrierType: string;
	    capacityUsed: number;
	    depotTritium: number;
	    manualMode: boolean;
	    error?: string;
	    totalJumps: number;
	    totalDistLy: number;
	    totalTritium: number;
	    jumps: CarrierRouteJump[];
	
	    static createFrom(source: any = {}) {
	        return new CarrierRouteResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.origin = source["origin"];
	        this.destination = source["destination"];
	        this.carrierType = source["carrierType"];
	        this.capacityUsed = source["capacityUsed"];
	        this.depotTritium = source["depotTritium"];
	        this.manualMode = source["manualMode"];
	        this.error = source["error"];
	        this.totalJumps = source["totalJumps"];
	        this.totalDistLy = source["totalDistLy"];
	        this.totalTritium = source["totalTritium"];
	        this.jumps = this.convertValues(source["jumps"], CarrierRouteJump);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ActiveCarrierRouteState {
	    expeditionId: number;
	    targetRowId: number;
	    route: CarrierRouteResult;
	    jumpIdx: number;
	    manualMode: boolean;
	    manualDistInput: string;
	    manualTritInput: string;
	    manualLegJumps: number;
	    manualLegDist: number;
	    manualLegTrit: number;
	
	    static createFrom(source: any = {}) {
	        return new ActiveCarrierRouteState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.expeditionId = source["expeditionId"];
	        this.targetRowId = source["targetRowId"];
	        this.route = this.convertValues(source["route"], CarrierRouteResult);
	        this.jumpIdx = source["jumpIdx"];
	        this.manualMode = source["manualMode"];
	        this.manualDistInput = source["manualDistInput"];
	        this.manualTritInput = source["manualTritInput"];
	        this.manualLegJumps = source["manualLegJumps"];
	        this.manualLegDist = source["manualLegDist"];
	        this.manualLegTrit = source["manualLegTrit"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CarrierCost {
	    carrierType: string;
	    weeklyUpkeep: number;
	    tritiumPrice: number;
	    tritiumCost: number;
	    jumpCost: number;
	    upkeepCost: number;
	    totalCost: number;
	    totalEstimated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CarrierCost(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.carrierType = source["carrierType"];
	        this.weeklyUpkeep = source["weeklyUpkeep"];
	        this.tritiumPrice = source["tritiumPrice"];
	        this.tritiumCost = source["tritiumCost"];
	        this.jumpCost = source["jumpCost"];
	        this.upkeepCost = source["upkeepCost"];
	        this.totalCost = source["totalCost"];
	        this.totalEstimated = source["totalEstimated"];
	    }
	}
	
	export class CarrierRoutePromptDefaults {
	    targetRowId: number;
	    origin: string;
	    destination: string;
	    carrierType: string;
	    capacityUsed: number;
	    depotTritium: number;
	
	    static createFrom(source: any = {}) {
	        return new CarrierRoutePromptDefaults(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetRowId = source["targetRowId"];
	        this.origin = source["origin"];
	        this.destination = source["destination"];
	        this.carrierType = source["carrierType"];
	        this.capacityUsed = source["capacityUsed"];
	        this.depotTritium = source["depotTritium"];
	    }
	}
	
	export class CarrierStats {
	    carrierJumps: number;
	    carrierDistanceLy: number;
	    tritiumUsed?: number;
	
	    static createFrom(source: any = {}) {
	        return new CarrierStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.carrierJumps = source["carrierJumps"];
	        this.carrierDistanceLy = source["carrierDistanceLy"];
	        this.tritiumUsed = source["tritiumUsed"];
	    }
	}
	export class DropSlotPreview {
	    targetIndex: number;
	    status: string;
	    visitedAtGalactic?: string;
	    resetsFcArrived?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DropSlotPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetIndex = source["targetIndex"];
	        this.status = source["status"];
	        this.visitedAtGalactic = source["visitedAtGalactic"];
	        this.resetsFcArrived = source["resetsFcArrived"];
	    }
	}
	export class Expedition {
	    id: number;
	    name: string;
	    description: string;
	    status: string;
	    startTimestamp: string;
	    endTimestamp: string;
	    adjustments: CarrierStats;
	    carrierType: string;
	    carrierWeeklyUpkeep: number;
	    activeModeId: number;
	    commanderScope: string;
	    commanderProfileIds: number[];
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Expedition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.status = source["status"];
	        this.startTimestamp = source["startTimestamp"];
	        this.endTimestamp = source["endTimestamp"];
	        this.adjustments = this.convertValues(source["adjustments"], CarrierStats);
	        this.carrierType = source["carrierType"];
	        this.carrierWeeklyUpkeep = source["carrierWeeklyUpkeep"];
	        this.activeModeId = source["activeModeId"];
	        this.commanderScope = source["commanderScope"];
	        this.commanderProfileIds = source["commanderProfileIds"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GECRef {
	    id: number;
	    name: string;
	    type: string;
	    url: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new GECRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.url = source["url"];
	        this.count = source["count"];
	    }
	}
	export class NonWaypointVisit {
	    timestamp: string;
	    galactic: string;
	    expeditionId?: number;
	    expeditionName?: string;
	    expeditionStatus?: string;
	
	    static createFrom(source: any = {}) {
	        return new NonWaypointVisit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.galactic = source["galactic"];
	        this.expeditionId = source["expeditionId"];
	        this.expeditionName = source["expeditionName"];
	        this.expeditionStatus = source["expeditionStatus"];
	    }
	}
	export class NonWaypointVisitSummary {
	    systemName: string;
	    totalCount: number;
	    earliestGalactic: string;
	    visits: NonWaypointVisit[];
	
	    static createFrom(source: any = {}) {
	        return new NonWaypointVisitSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.systemName = source["systemName"];
	        this.totalCount = source["totalCount"];
	        this.earliestGalactic = source["earliestGalactic"];
	        this.visits = this.convertValues(source["visits"], NonWaypointVisit);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WaypointPermissions {
	    systemLocked: boolean;
	    systemLockReason?: string;
	    allowInsertWaypoint: boolean;
	    canReorder: boolean;
	    deleteDisabledReason?: string;
	    reorderDisabledReason?: string;
	    canAddFcStop: boolean;
	    canRemoveFcStop: boolean;
	    canOverrideFcBadge: boolean;
	    overrideFcCascadeCount?: number;
	    canCascadeLegMode?: boolean;
	    hasDrawer: boolean;
	    isNextFcStop?: boolean;
	    isFcStopLocked?: boolean;
	    hasActiveCarrierRoute?: boolean;
	    isSpanshRoutedFcStop?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WaypointPermissions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.systemLocked = source["systemLocked"];
	        this.systemLockReason = source["systemLockReason"];
	        this.allowInsertWaypoint = source["allowInsertWaypoint"];
	        this.canReorder = source["canReorder"];
	        this.deleteDisabledReason = source["deleteDisabledReason"];
	        this.reorderDisabledReason = source["reorderDisabledReason"];
	        this.canAddFcStop = source["canAddFcStop"];
	        this.canRemoveFcStop = source["canRemoveFcStop"];
	        this.canOverrideFcBadge = source["canOverrideFcBadge"];
	        this.overrideFcCascadeCount = source["overrideFcCascadeCount"];
	        this.canCascadeLegMode = source["canCascadeLegMode"];
	        this.hasDrawer = source["hasDrawer"];
	        this.isNextFcStop = source["isNextFcStop"];
	        this.isFcStopLocked = source["isFcStopLocked"];
	        this.hasActiveCarrierRoute = source["hasActiveCarrierRoute"];
	        this.isSpanshRoutedFcStop = source["isSpanshRoutedFcStop"];
	    }
	}
	export class Row {
	    id: number;
	    expeditionId: number;
	    position: number;
	    rowType: string;
	    regionId: number;
	    fcStop: boolean;
	    fcArrived: boolean;
	    legModeId: number;
	    systemName: string;
	    poiName: string;
	    notes: string;
	    systemAddress: number;
	    edsmId: number;
	    hasCoords: boolean;
	    x: number;
	    y: number;
	    z: number;
	    coordsSource: string;
	    visitedAt: string;
	    departedAt: string;
	    visitEventUid: string;
	
	    static createFrom(source: any = {}) {
	        return new Row(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.expeditionId = source["expeditionId"];
	        this.position = source["position"];
	        this.rowType = source["rowType"];
	        this.regionId = source["regionId"];
	        this.fcStop = source["fcStop"];
	        this.fcArrived = source["fcArrived"];
	        this.legModeId = source["legModeId"];
	        this.systemName = source["systemName"];
	        this.poiName = source["poiName"];
	        this.notes = source["notes"];
	        this.systemAddress = source["systemAddress"];
	        this.edsmId = source["edsmId"];
	        this.hasCoords = source["hasCoords"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.z = source["z"];
	        this.coordsSource = source["coordsSource"];
	        this.visitedAt = source["visitedAt"];
	        this.departedAt = source["departedAt"];
	        this.visitEventUid = source["visitEventUid"];
	    }
	}
	export class RowView {
	    row: Row;
	    number: number;
	    regionName: string;
	    detectedRegionId: number;
	    detectedRegion: string;
	    visitCount: number;
	    visitState: string;
	    visitedAtGalactic: string;
	    departedAtGalactic?: string;
	    drawerFrom?: string;
	    drawerTo?: string;
	    distFromPrevLy?: number;
	    distFromCurrentLy?: number;
	    isNextWaypoint?: boolean;
	    legModeId: number;
	    legModeName: string;
	    legModeColor: string;
	    gec?: GECRef;
	    allowInsertWaypoint: boolean;
	    permissions?: WaypointPermissions;
	
	    static createFrom(source: any = {}) {
	        return new RowView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.row = this.convertValues(source["row"], Row);
	        this.number = source["number"];
	        this.regionName = source["regionName"];
	        this.detectedRegionId = source["detectedRegionId"];
	        this.detectedRegion = source["detectedRegion"];
	        this.visitCount = source["visitCount"];
	        this.visitState = source["visitState"];
	        this.visitedAtGalactic = source["visitedAtGalactic"];
	        this.departedAtGalactic = source["departedAtGalactic"];
	        this.drawerFrom = source["drawerFrom"];
	        this.drawerTo = source["drawerTo"];
	        this.distFromPrevLy = source["distFromPrevLy"];
	        this.distFromCurrentLy = source["distFromCurrentLy"];
	        this.isNextWaypoint = source["isNextWaypoint"];
	        this.legModeId = source["legModeId"];
	        this.legModeName = source["legModeName"];
	        this.legModeColor = source["legModeColor"];
	        this.gec = this.convertValues(source["gec"], GECRef);
	        this.allowInsertWaypoint = source["allowInsertWaypoint"];
	        this.permissions = this.convertValues(source["permissions"], WaypointPermissions);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PlanView {
	    expedition: Expedition;
	    startGalactic: string;
	    endGalactic: string;
	    rows: RowView[];
	    currentRowId: number;
	    nextWaypointRowId?: number;
	    nextFcStopRowId: number;
	    routedCarrierStopRowId?: number;
	    activeCarrierRoute?: ActiveCarrierRouteState;
	    lastVisitedIndex: number;
	    lastFcArrivedIndex: number;
	    defaultModeId: number;
	    playerSystem?: string;
	    waypointCount: number;
	    visitedWaypointCount: number;
	    plannedLy: number;
	    coveredLy: number;
	    remainingLy: number;
	    modes: modes.Mode[];
	
	    static createFrom(source: any = {}) {
	        return new PlanView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.expedition = this.convertValues(source["expedition"], Expedition);
	        this.startGalactic = source["startGalactic"];
	        this.endGalactic = source["endGalactic"];
	        this.rows = this.convertValues(source["rows"], RowView);
	        this.currentRowId = source["currentRowId"];
	        this.nextWaypointRowId = source["nextWaypointRowId"];
	        this.nextFcStopRowId = source["nextFcStopRowId"];
	        this.routedCarrierStopRowId = source["routedCarrierStopRowId"];
	        this.activeCarrierRoute = this.convertValues(source["activeCarrierRoute"], ActiveCarrierRouteState);
	        this.lastVisitedIndex = source["lastVisitedIndex"];
	        this.lastFcArrivedIndex = source["lastFcArrivedIndex"];
	        this.defaultModeId = source["defaultModeId"];
	        this.playerSystem = source["playerSystem"];
	        this.waypointCount = source["waypointCount"];
	        this.visitedWaypointCount = source["visitedWaypointCount"];
	        this.plannedLy = source["plannedLy"];
	        this.coveredLy = source["coveredLy"];
	        this.remainingLy = source["remainingLy"];
	        this.modes = this.convertValues(source["modes"], modes.Mode);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class RowPatch {
	    id: number;
	    fcStop?: boolean;
	    fcArrived?: boolean;
	    legModeId?: number;
	    systemName?: string;
	    poiName?: string;
	    notes?: string;
	    regionId?: number;
	
	    static createFrom(source: any = {}) {
	        return new RowPatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.fcStop = source["fcStop"];
	        this.fcArrived = source["fcArrived"];
	        this.legModeId = source["legModeId"];
	        this.systemName = source["systemName"];
	        this.poiName = source["poiName"];
	        this.notes = source["notes"];
	        this.regionId = source["regionId"];
	    }
	}
	
	export class WaypointStopMatch {
	    rowId: number;
	    number: number;
	    poiName: string;
	    visitedAt: string;
	    visitedAtGalactic: string;
	
	    static createFrom(source: any = {}) {
	        return new WaypointStopMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rowId = source["rowId"];
	        this.number = source["number"];
	        this.poiName = source["poiName"];
	        this.visitedAt = source["visitedAt"];
	        this.visitedAtGalactic = source["visitedAtGalactic"];
	    }
	}
	export class WaypointSearchResult {
	    expeditionId: number;
	    expeditionName: string;
	    expeditionStatus: string;
	    systemName: string;
	    poiName: string;
	    relevanceTier: number;
	    stops: WaypointStopMatch[];
	
	    static createFrom(source: any = {}) {
	        return new WaypointSearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.expeditionId = source["expeditionId"];
	        this.expeditionName = source["expeditionName"];
	        this.expeditionStatus = source["expeditionStatus"];
	        this.systemName = source["systemName"];
	        this.poiName = source["poiName"];
	        this.relevanceTier = source["relevanceTier"];
	        this.stops = this.convertValues(source["stops"], WaypointStopMatch);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SearchResponse {
	    results: WaypointSearchResult[];
	    totalMatches: number;
	    nonWaypointVisits?: NonWaypointVisitSummary;
	
	    static createFrom(source: any = {}) {
	        return new SearchResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.results = this.convertValues(source["results"], WaypointSearchResult);
	        this.totalMatches = source["totalMatches"];
	        this.nonWaypointVisits = this.convertValues(source["nonWaypointVisits"], NonWaypointVisitSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WaypointDetails {
	    rowId: number;
	    kind: string;
	    number: number;
	    from: string;
	    to: string;
	    fromGalactic: string;
	    toGalactic: string;
	    arrivedAt?: string;
	    departedAt?: string;
	    inSystem?: boolean;
	    elapsed: string;
	    prevSystem: string;
	    shipsUsed: string[];
	    metrics: stats.Metrics;
	    realization: realization.Breakdown;
	
	    static createFrom(source: any = {}) {
	        return new WaypointDetails(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rowId = source["rowId"];
	        this.kind = source["kind"];
	        this.number = source["number"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.fromGalactic = source["fromGalactic"];
	        this.toGalactic = source["toGalactic"];
	        this.arrivedAt = source["arrivedAt"];
	        this.departedAt = source["departedAt"];
	        this.inSystem = source["inSystem"];
	        this.elapsed = source["elapsed"];
	        this.prevSystem = source["prevSystem"];
	        this.shipsUsed = source["shipsUsed"];
	        this.metrics = this.convertValues(source["metrics"], stats.Metrics);
	        this.realization = this.convertValues(source["realization"], realization.Breakdown);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	

}

export namespace exporter {
	
	export class BundleFile {
	    name: string;
	    contentBase64?: string;
	    contentText?: string;
	    canonicalJsonExpeditionId?: number;
	    canonicalJsonExportedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new BundleFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.contentBase64 = source["contentBase64"];
	        this.contentText = source["contentText"];
	        this.canonicalJsonExpeditionId = source["canonicalJsonExpeditionId"];
	        this.canonicalJsonExportedAt = source["canonicalJsonExportedAt"];
	    }
	}
	export class CareerDiffItem {
	    id: string;
	    baseline: number;
	    growthPercent?: number;
	    isNewBaseline: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CareerDiffItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.baseline = source["baseline"];
	        this.growthPercent = source["growthPercent"];
	        this.isNewBaseline = source["isNewBaseline"];
	    }
	}
	export class DerivedMetrics {
	    shipDistanceLy: number;
	    totalBoosts: number;
	    avgJumpRangeLy?: number;
	    boostRatePercent?: number;
	    avgDistancePerDayLy?: number;
	    avgCarrierJumpRangeLy?: number;
	    avgTritiumPerJump?: number;
	    combinedEstimatedCredits: number;
	    dssEfficiencyPercent?: number;
	    firstDiscoveryRatePercent?: number;
	    firstLoggedRatePercent?: number;
	    avgCreditsPerSample?: number;
	    avgCombinedCreditsPerJump?: number;
	    avgCombinedCreditsPer1000Ly?: number;
	    avgCombinedCreditsPerDay?: number;
	    avgCartoCreditsPerJump?: number;
	    avgCartoCreditsPer1000Ly?: number;
	    avgExoCreditsPerJump?: number;
	    avgExoCreditsPer1000Ly?: number;
	    totalLossesCredits: number;
	    realizationRetentionPercent?: number;
	    netProfitAfterCarrierCost?: number;
	    avgRealizedCreditsPerJump?: number;
	    avgRealizedCreditsPer1000Ly?: number;
	
	    static createFrom(source: any = {}) {
	        return new DerivedMetrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shipDistanceLy = source["shipDistanceLy"];
	        this.totalBoosts = source["totalBoosts"];
	        this.avgJumpRangeLy = source["avgJumpRangeLy"];
	        this.boostRatePercent = source["boostRatePercent"];
	        this.avgDistancePerDayLy = source["avgDistancePerDayLy"];
	        this.avgCarrierJumpRangeLy = source["avgCarrierJumpRangeLy"];
	        this.avgTritiumPerJump = source["avgTritiumPerJump"];
	        this.combinedEstimatedCredits = source["combinedEstimatedCredits"];
	        this.dssEfficiencyPercent = source["dssEfficiencyPercent"];
	        this.firstDiscoveryRatePercent = source["firstDiscoveryRatePercent"];
	        this.firstLoggedRatePercent = source["firstLoggedRatePercent"];
	        this.avgCreditsPerSample = source["avgCreditsPerSample"];
	        this.avgCombinedCreditsPerJump = source["avgCombinedCreditsPerJump"];
	        this.avgCombinedCreditsPer1000Ly = source["avgCombinedCreditsPer1000Ly"];
	        this.avgCombinedCreditsPerDay = source["avgCombinedCreditsPerDay"];
	        this.avgCartoCreditsPerJump = source["avgCartoCreditsPerJump"];
	        this.avgCartoCreditsPer1000Ly = source["avgCartoCreditsPer1000Ly"];
	        this.avgExoCreditsPerJump = source["avgExoCreditsPerJump"];
	        this.avgExoCreditsPer1000Ly = source["avgExoCreditsPer1000Ly"];
	        this.totalLossesCredits = source["totalLossesCredits"];
	        this.realizationRetentionPercent = source["realizationRetentionPercent"];
	        this.netProfitAfterCarrierCost = source["netProfitAfterCarrierCost"];
	        this.avgRealizedCreditsPerJump = source["avgRealizedCreditsPerJump"];
	        this.avgRealizedCreditsPer1000Ly = source["avgRealizedCreditsPer1000Ly"];
	    }
	}
	export class HighlightRankingEntry {
	    rank: number;
	    label: string;
	    routeLabel: string;
	    primaryValue: number;
	    formattedPrimary: string;
	    secondaryLabel: string;
	    modeName?: string;
	    modeColor?: string;
	    inSystem?: boolean;
	    rowId: number;
	    waypointNumber: number;
	
	    static createFrom(source: any = {}) {
	        return new HighlightRankingEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rank = source["rank"];
	        this.label = source["label"];
	        this.routeLabel = source["routeLabel"];
	        this.primaryValue = source["primaryValue"];
	        this.formattedPrimary = source["formattedPrimary"];
	        this.secondaryLabel = source["secondaryLabel"];
	        this.modeName = source["modeName"];
	        this.modeColor = source["modeColor"];
	        this.inSystem = source["inSystem"];
	        this.rowId = source["rowId"];
	        this.waypointNumber = source["waypointNumber"];
	    }
	}
	export class HighlightLeaderboard {
	    cardId: string;
	    title: string;
	    accent: string;
	    entries: HighlightRankingEntry[];
	
	    static createFrom(source: any = {}) {
	        return new HighlightLeaderboard(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cardId = source["cardId"];
	        this.title = source["title"];
	        this.accent = source["accent"];
	        this.entries = this.convertValues(source["entries"], HighlightRankingEntry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RegionModeBucket {
	    modeId: number;
	    modeName: string;
	    modeColor: string;
	    legCount: number;
	    fromGalactic: string;
	    toGalactic: string;
	    inSystem: boolean;
	    duration: string;
	    elapsedDays?: number;
	    coveredLy: number;
	    shipsFlown: string[];
	    metrics: stats.Metrics;
	    realization: realization.Breakdown;
	    derived: DerivedMetrics;
	
	    static createFrom(source: any = {}) {
	        return new RegionModeBucket(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.modeId = source["modeId"];
	        this.modeName = source["modeName"];
	        this.modeColor = source["modeColor"];
	        this.legCount = source["legCount"];
	        this.fromGalactic = source["fromGalactic"];
	        this.toGalactic = source["toGalactic"];
	        this.inSystem = source["inSystem"];
	        this.duration = source["duration"];
	        this.elapsedDays = source["elapsedDays"];
	        this.coveredLy = source["coveredLy"];
	        this.shipsFlown = source["shipsFlown"];
	        this.metrics = this.convertValues(source["metrics"], stats.Metrics);
	        this.realization = this.convertValues(source["realization"], realization.Breakdown);
	        this.derived = this.convertValues(source["derived"], DerivedMetrics);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RegionBucket {
	    regionName: string;
	    legRowIds: number[];
	    legCount: number;
	    fromGalactic: string;
	    toGalactic: string;
	    inSystem: boolean;
	    duration: string;
	    elapsedDays?: number;
	    coveredLy: number;
	    shipsFlown: string[];
	    metrics: stats.Metrics;
	    realization: realization.Breakdown;
	    derived: DerivedMetrics;
	    modeBuckets: RegionModeBucket[];
	
	    static createFrom(source: any = {}) {
	        return new RegionBucket(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.regionName = source["regionName"];
	        this.legRowIds = source["legRowIds"];
	        this.legCount = source["legCount"];
	        this.fromGalactic = source["fromGalactic"];
	        this.toGalactic = source["toGalactic"];
	        this.inSystem = source["inSystem"];
	        this.duration = source["duration"];
	        this.elapsedDays = source["elapsedDays"];
	        this.coveredLy = source["coveredLy"];
	        this.shipsFlown = source["shipsFlown"];
	        this.metrics = this.convertValues(source["metrics"], stats.Metrics);
	        this.realization = this.convertValues(source["realization"], realization.Breakdown);
	        this.derived = this.convertValues(source["derived"], DerivedMetrics);
	        this.modeBuckets = this.convertValues(source["modeBuckets"], RegionModeBucket);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExportLegTelemetry {
	    rowId: number;
	    regionName: string;
	    elapsedDays?: number;
	    shipsFlown: string[];
	    derived: DerivedMetrics;
	
	    static createFrom(source: any = {}) {
	        return new ExportLegTelemetry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rowId = source["rowId"];
	        this.regionName = source["regionName"];
	        this.elapsedDays = source["elapsedDays"];
	        this.shipsFlown = source["shipsFlown"];
	        this.derived = this.convertValues(source["derived"], DerivedMetrics);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ModeBucket {
	    modeId: number;
	    modeName: string;
	    modeColor: string;
	    elapsedDays?: number;
	    shipsFlown: string[];
	    metrics: stats.Metrics;
	    realization: realization.Breakdown;
	    derived: DerivedMetrics;
	
	    static createFrom(source: any = {}) {
	        return new ModeBucket(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.modeId = source["modeId"];
	        this.modeName = source["modeName"];
	        this.modeColor = source["modeColor"];
	        this.elapsedDays = source["elapsedDays"];
	        this.shipsFlown = source["shipsFlown"];
	        this.metrics = this.convertValues(source["metrics"], stats.Metrics);
	        this.realization = this.convertValues(source["realization"], realization.Breakdown);
	        this.derived = this.convertValues(source["derived"], DerivedMetrics);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TelemetrySnapshot {
	    metrics: stats.Metrics;
	    realization: realization.Breakdown;
	    elapsedDays?: number;
	    shipsFlown: string[];
	    derived: DerivedMetrics;
	
	    static createFrom(source: any = {}) {
	        return new TelemetrySnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.metrics = this.convertValues(source["metrics"], stats.Metrics);
	        this.realization = this.convertValues(source["realization"], realization.Breakdown);
	        this.elapsedDays = source["elapsedDays"];
	        this.shipsFlown = source["shipsFlown"];
	        this.derived = this.convertValues(source["derived"], DerivedMetrics);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExpeditionExport {
	    schemaVersion: number;
	    exportedAt: string;
	    exportedAtGalactic: string;
	    appVersion: string;
	    commanders: string[];
	    departed: boolean;
	    effectiveStart: string;
	    effectiveStartGalactic: string;
	    effectiveEnd: string;
	    effectiveEndGalactic: string;
	    endIsNow: boolean;
	    duration: string;
	    careerStart: string;
	    careerStartGalactic: string;
	    careerDuration: string;
	    plan: expedition.PlanView;
	    expedition: TelemetrySnapshot;
	    live: stats.Metrics;
	    adjustments: stats.Metrics;
	    carrierCost: expedition.CarrierCost;
	    originBaseline?: expedition.WaypointDetails;
	    originBaselineElapsedDays?: number;
	    originBaselineDerived?: DerivedMetrics;
	    careerDiff?: CareerDiffItem[];
	    modeBuckets: ModeBucket[];
	    rowDetails: expedition.WaypointDetails[];
	    legTelemetry: ExportLegTelemetry[];
	    regionBuckets: RegionBucket[];
	    highlightsFullOrder: HighlightLeaderboard[];
	    highlightsVariants: HighlightLeaderboard[];
	
	    static createFrom(source: any = {}) {
	        return new ExpeditionExport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schemaVersion = source["schemaVersion"];
	        this.exportedAt = source["exportedAt"];
	        this.exportedAtGalactic = source["exportedAtGalactic"];
	        this.appVersion = source["appVersion"];
	        this.commanders = source["commanders"];
	        this.departed = source["departed"];
	        this.effectiveStart = source["effectiveStart"];
	        this.effectiveStartGalactic = source["effectiveStartGalactic"];
	        this.effectiveEnd = source["effectiveEnd"];
	        this.effectiveEndGalactic = source["effectiveEndGalactic"];
	        this.endIsNow = source["endIsNow"];
	        this.duration = source["duration"];
	        this.careerStart = source["careerStart"];
	        this.careerStartGalactic = source["careerStartGalactic"];
	        this.careerDuration = source["careerDuration"];
	        this.plan = this.convertValues(source["plan"], expedition.PlanView);
	        this.expedition = this.convertValues(source["expedition"], TelemetrySnapshot);
	        this.live = this.convertValues(source["live"], stats.Metrics);
	        this.adjustments = this.convertValues(source["adjustments"], stats.Metrics);
	        this.carrierCost = this.convertValues(source["carrierCost"], expedition.CarrierCost);
	        this.originBaseline = this.convertValues(source["originBaseline"], expedition.WaypointDetails);
	        this.originBaselineElapsedDays = source["originBaselineElapsedDays"];
	        this.originBaselineDerived = this.convertValues(source["originBaselineDerived"], DerivedMetrics);
	        this.careerDiff = this.convertValues(source["careerDiff"], CareerDiffItem);
	        this.modeBuckets = this.convertValues(source["modeBuckets"], ModeBucket);
	        this.rowDetails = this.convertValues(source["rowDetails"], expedition.WaypointDetails);
	        this.legTelemetry = this.convertValues(source["legTelemetry"], ExportLegTelemetry);
	        this.regionBuckets = this.convertValues(source["regionBuckets"], RegionBucket);
	        this.highlightsFullOrder = this.convertValues(source["highlightsFullOrder"], HighlightLeaderboard);
	        this.highlightsVariants = this.convertValues(source["highlightsVariants"], HighlightLeaderboard);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	

}

export namespace modes {
	
	export class ExpeditionUsage {
	    expeditionId: number;
	    expeditionName: string;
	    status: string;
	    legCount: number;
	    isActiveMode: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExpeditionUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.expeditionId = source["expeditionId"];
	        this.expeditionName = source["expeditionName"];
	        this.status = source["status"];
	        this.legCount = source["legCount"];
	        this.isActiveMode = source["isActiveMode"];
	    }
	}
	export class Mode {
	    id: number;
	    name: string;
	    description: string;
	    color: string;
	    isDefault: boolean;
	    sortOrder: number;
	
	    static createFrom(source: any = {}) {
	        return new Mode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.color = source["color"];
	        this.isDefault = source["isDefault"];
	        this.sortOrder = source["sortOrder"];
	    }
	}

}

export namespace realization {
	
	export class Breakdown {
	    totalRealized: number;
	    cartoLostDeath: number;
	    organicLostDeath: number;
	    cartoStolen: number;
	    organicStolen: number;
	    carrierTariffLoss: number;
	    npcCrewLoss: number;
	    pendingValue: number;
	
	    static createFrom(source: any = {}) {
	        return new Breakdown(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalRealized = source["totalRealized"];
	        this.cartoLostDeath = source["cartoLostDeath"];
	        this.organicLostDeath = source["organicLostDeath"];
	        this.cartoStolen = source["cartoStolen"];
	        this.organicStolen = source["organicStolen"];
	        this.carrierTariffLoss = source["carrierTariffLoss"];
	        this.npcCrewLoss = source["npcCrewLoss"];
	        this.pendingValue = source["pendingValue"];
	    }
	}

}

export namespace reference {
	
	export class RegionOption {
	    id: number;
	    name: string;
	    adjacent: boolean;
	    anchor: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RegionOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.adjacent = source["adjacent"];
	        this.anchor = source["anchor"];
	    }
	}

}

export namespace replay {
	
	export class Arrival {
	    uid: string;
	    timestamp: string;
	    galactic: string;
	    event: string;
	    systemName: string;
	    systemAddress: number;
	    profileId?: number;
	    commander?: string;
	
	    static createFrom(source: any = {}) {
	        return new Arrival(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uid = source["uid"];
	        this.timestamp = source["timestamp"];
	        this.galactic = source["galactic"];
	        this.event = source["event"];
	        this.systemName = source["systemName"];
	        this.systemAddress = source["systemAddress"];
	        this.profileId = source["profileId"];
	        this.commander = source["commander"];
	    }
	}
	export class CommanderAlias {
	    name: string;
	    fid: string;
	    universe: string;
	    startTimestamp: string;
	    endTimestamp: string;
	    eventCount: number;
	    isCurrent: boolean;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new CommanderAlias(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.fid = source["fid"];
	        this.universe = source["universe"];
	        this.startTimestamp = source["startTimestamp"];
	        this.endTimestamp = source["endTimestamp"];
	        this.eventCount = source["eventCount"];
	        this.isCurrent = source["isCurrent"];
	        this.source = source["source"];
	    }
	}
	export class Commander {
	    id: number;
	    profileKey: string;
	    name: string;
	    fid: string;
	    universe: string;
	    eraIndex: number;
	    isArchivedCareer: boolean;
	    hasManualOverride: boolean;
	    enabled: boolean;
	    firstSeen: string;
	    lastSeen: string;
	    firstGalactic: string;
	    lastGalactic: string;
	    eventCount: number;
	    aliases: CommanderAlias[];
	
	    static createFrom(source: any = {}) {
	        return new Commander(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.profileKey = source["profileKey"];
	        this.name = source["name"];
	        this.fid = source["fid"];
	        this.universe = source["universe"];
	        this.eraIndex = source["eraIndex"];
	        this.isArchivedCareer = source["isArchivedCareer"];
	        this.hasManualOverride = source["hasManualOverride"];
	        this.enabled = source["enabled"];
	        this.firstSeen = source["firstSeen"];
	        this.lastSeen = source["lastSeen"];
	        this.firstGalactic = source["firstGalactic"];
	        this.lastGalactic = source["lastGalactic"];
	        this.eventCount = source["eventCount"];
	        this.aliases = this.convertValues(source["aliases"], CommanderAlias);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class CommanderSession {
	    timestamp: string;
	    galactic: string;
	    ship: string;
	    systemName: string;
	
	    static createFrom(source: any = {}) {
	        return new CommanderSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.galactic = source["galactic"];
	        this.ship = source["ship"];
	        this.systemName = source["systemName"];
	    }
	}
	export class ProfileScope {
	    Selected: boolean;
	    ProfileIDs: number[];
	
	    static createFrom(source: any = {}) {
	        return new ProfileScope(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Selected = source["Selected"];
	        this.ProfileIDs = source["ProfileIDs"];
	    }
	}

}

export namespace stats {
	
	export class ModeSelection {
	    modeId: number;
	    travel: boolean;
	    discovery: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModeSelection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.modeId = source["modeId"];
	        this.travel = source["travel"];
	        this.discovery = source["discovery"];
	    }
	}
	export class Filter {
	    enabled: boolean;
	    expeditionId?: number;
	    modes: ModeSelection[];
	
	    static createFrom(source: any = {}) {
	        return new Filter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.expeditionId = source["expeditionId"];
	        this.modes = this.convertValues(source["modes"], ModeSelection);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Metrics {
	    distanceLy: number;
	    shipDistanceLy: number;
	    carrierPassengerLy: number;
	    jumps: number;
	    carrierJumps: number;
	    carrierDistanceLy: number;
	    tritiumUsed?: number;
	    jetConeBoosts: number;
	    premiumBoosts: number;
	    standardBoosts: number;
	    basicBoosts: number;
	    bodiesScanned: number;
	    bodiesMapped: number;
	    efficientMappings: number;
	    scanValueTotal: number;
	    scanValueBase: number;
	    scanValueBonus: number;
	    scanValueEfficiencyBonus: number;
	    firstDiscoveries: number;
	    firstMapped: number;
	    organicSamples: number;
	    organicValueTotal: number;
	    organicValueBase: number;
	    organicValueBonus: number;
	    firstLogged: number;
	
	    static createFrom(source: any = {}) {
	        return new Metrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.distanceLy = source["distanceLy"];
	        this.shipDistanceLy = source["shipDistanceLy"];
	        this.carrierPassengerLy = source["carrierPassengerLy"];
	        this.jumps = source["jumps"];
	        this.carrierJumps = source["carrierJumps"];
	        this.carrierDistanceLy = source["carrierDistanceLy"];
	        this.tritiumUsed = source["tritiumUsed"];
	        this.jetConeBoosts = source["jetConeBoosts"];
	        this.premiumBoosts = source["premiumBoosts"];
	        this.standardBoosts = source["standardBoosts"];
	        this.basicBoosts = source["basicBoosts"];
	        this.bodiesScanned = source["bodiesScanned"];
	        this.bodiesMapped = source["bodiesMapped"];
	        this.efficientMappings = source["efficientMappings"];
	        this.scanValueTotal = source["scanValueTotal"];
	        this.scanValueBase = source["scanValueBase"];
	        this.scanValueBonus = source["scanValueBonus"];
	        this.scanValueEfficiencyBonus = source["scanValueEfficiencyBonus"];
	        this.firstDiscoveries = source["firstDiscoveries"];
	        this.firstMapped = source["firstMapped"];
	        this.organicSamples = source["organicSamples"];
	        this.organicValueTotal = source["organicValueTotal"];
	        this.organicValueBase = source["organicValueBase"];
	        this.organicValueBonus = source["organicValueBonus"];
	        this.firstLogged = source["firstLogged"];
	    }
	}

}

