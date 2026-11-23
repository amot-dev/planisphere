export namespace app {
	
	export class CarrierCostBreakdown {
	    carrierType: string;
	    weeklyUpkeep: number;
	    tritiumPrice: number;
	    tritiumCost: number;
	    jumpCost: number;
	    upkeepCost: number;
	    totalCost: number;
	    totalEstimated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CarrierCostBreakdown(source);
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
	    }
	}
	export class ModesState {
	    modes: modes.Mode[];
	
	    static createFrom(source: any = {}) {
	        return new ModesState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	    carrierCost: CarrierCostBreakdown;
	
	    static createFrom(source: any = {}) {
	        return new SidebarStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.expeditionId = source["expeditionId"];
	        this.status = source["status"];
	        this.started = source["started"];
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
	        this.carrierCost = this.convertValues(source["carrierCost"], CarrierCostBreakdown);
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
	    distFromPrevLy?: number;
	    distFromCurrentLy?: number;
	    isNextWaypoint?: boolean;
	    legModeId: number;
	    legModeName: string;
	    legModeColor: string;
	    gec?: GECRef;
	
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
	        this.distFromPrevLy = source["distFromPrevLy"];
	        this.distFromCurrentLy = source["distFromCurrentLy"];
	        this.isNextWaypoint = source["isNextWaypoint"];
	        this.legModeId = source["legModeId"];
	        this.legModeName = source["legModeName"];
	        this.legModeColor = source["legModeColor"];
	        this.gec = this.convertValues(source["gec"], GECRef);
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
	
	export class WaypointDetails {
	    rowId: number;
	    kind: string;
	    number: number;
	    from: string;
	    to: string;
	    fromGalactic: string;
	    toGalactic: string;
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
	    }
	}
	export class CarrierDepotInfo {
	    usedCapacity: number;
	    depotTritium: number;
	    cargo: number;
	
	    static createFrom(source: any = {}) {
	        return new CarrierDepotInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.usedCapacity = source["usedCapacity"];
	        this.depotTritium = source["depotTritium"];
	        this.cargo = source["cargo"];
	    }
	}
	export class Commander {
	    name: string;
	    fid: string;
	    enabled: boolean;
	    firstSeen: string;
	    lastSeen: string;
	    firstGalactic: string;
	    lastGalactic: string;
	    eventCount: number;
	
	    static createFrom(source: any = {}) {
	        return new Commander(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.fid = source["fid"];
	        this.enabled = source["enabled"];
	        this.firstSeen = source["firstSeen"];
	        this.lastSeen = source["lastSeen"];
	        this.firstGalactic = source["firstGalactic"];
	        this.lastGalactic = source["lastGalactic"];
	        this.eventCount = source["eventCount"];
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

