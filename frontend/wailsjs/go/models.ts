export namespace app {
	
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
	    minimized: boolean;
	
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
	        this.minimized = source["minimized"];
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
	export class ModeWindowInput {
	    id: number;
	    modeId: number;
	    start: string;
	    end: string;
	
	    static createFrom(source: any = {}) {
	        return new ModeWindowInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.modeId = source["modeId"];
	        this.start = source["start"];
	        this.end = source["end"];
	    }
	}
	export class ModeWindowView {
	    id: number;
	    modeId: number;
	    modeName: string;
	    color: string;
	    start: string;
	    end: string;
	    startGalactic: string;
	    endGalactic: string;
	    duration: string;
	
	    static createFrom(source: any = {}) {
	        return new ModeWindowView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.modeId = source["modeId"];
	        this.modeName = source["modeName"];
	        this.color = source["color"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.startGalactic = source["startGalactic"];
	        this.endGalactic = source["endGalactic"];
	        this.duration = source["duration"];
	    }
	}
	export class ModesState {
	    modes: modes.Mode[];
	    activeModeId: number;
	    activeSince: string;
	    activeSinceGalactic: string;
	    windows: ModeWindowView[];
	
	    static createFrom(source: any = {}) {
	        return new ModesState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.modes = this.convertValues(source["modes"], modes.Mode);
	        this.activeModeId = source["activeModeId"];
	        this.activeSince = source["activeSince"];
	        this.activeSinceGalactic = source["activeSinceGalactic"];
	        this.windows = this.convertValues(source["windows"], ModeWindowView);
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
	    options: ed.RegionOption[];
	
	    static createFrom(source: any = {}) {
	        return new RegionOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.anchorId = source["anchorId"];
	        this.anchorName = source["anchorName"];
	        this.anchorIsDefault = source["anchorIsDefault"];
	        this.suggestedRegionId = source["suggestedRegionId"];
	        this.options = this.convertValues(source["options"], ed.RegionOption);
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
	    onlineLookups: boolean;
	    dbPath: string;
	    defaultDbPath: string;
	    activeExpeditionId: number;
	    gecCount: number;
	    gecFetchedAt: string;
	    commanders: journal.Commander[];
	    edDiscoveryDbPath: string;
	    defaultEdDiscoveryDbPath: string;
	    edDiscoveryDbExists: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.setupCompleted = source["setupCompleted"];
	        this.journalDir = source["journalDir"];
	        this.defaultJournalDir = source["defaultJournalDir"];
	        this.journalDirExists = source["journalDirExists"];
	        this.onlineLookups = source["onlineLookups"];
	        this.dbPath = source["dbPath"];
	        this.defaultDbPath = source["defaultDbPath"];
	        this.activeExpeditionId = source["activeExpeditionId"];
	        this.gecCount = source["gecCount"];
	        this.gecFetchedAt = source["gecFetchedAt"];
	        this.commanders = this.convertValues(source["commanders"], journal.Commander);
	        this.edDiscoveryDbPath = source["edDiscoveryDbPath"];
	        this.defaultEdDiscoveryDbPath = source["defaultEdDiscoveryDbPath"];
	        this.edDiscoveryDbExists = source["edDiscoveryDbExists"];
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
	    baselineEnabled: boolean;
	    baselineSource: string;
	    baseline: stats.Metrics;
	    lifetime: stats.Metrics;
	
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
	        this.baselineEnabled = source["baselineEnabled"];
	        this.baselineSource = source["baselineSource"];
	        this.baseline = this.convertValues(source["baseline"], stats.Metrics);
	        this.lifetime = this.convertValues(source["lifetime"], stats.Metrics);
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
	    visits: expedition.Arrival[];
	
	    static createFrom(source: any = {}) {
	        return new StartCandidates(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.systemName = source["systemName"];
	        this.visits = this.convertValues(source["visits"], expedition.Arrival);
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

export namespace ed {
	
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

export namespace expedition {
	
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
	export class Expedition {
	    id: number;
	    name: string;
	    description: string;
	    status: string;
	    startTimestamp: string;
	    endTimestamp: string;
	    armedAt: string;
	    baselineEnabled: boolean;
	    baselineSource: string;
	    baseline: stats.Metrics;
	    adjustments: stats.Metrics;
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
	        this.armedAt = source["armedAt"];
	        this.baselineEnabled = source["baselineEnabled"];
	        this.baselineSource = source["baselineSource"];
	        this.baseline = this.convertValues(source["baseline"], stats.Metrics);
	        this.adjustments = this.convertValues(source["adjustments"], stats.Metrics);
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
	export class Transition {
	    kind: string;
	    at: string;
	    atGalactic: string;
	    from: string;
	    to: string;
	    fromColor: string;
	    toColor: string;
	
	    static createFrom(source: any = {}) {
	        return new Transition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.at = source["at"];
	        this.atGalactic = source["atGalactic"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.fromColor = source["fromColor"];
	        this.toColor = source["toColor"];
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
	    visitState: string;
	    visitedAtGalactic: string;
	    distFromPrevLy?: number;
	    distFromCurrentLy?: number;
	    transitions: Transition[];
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
	        this.visitState = source["visitState"];
	        this.visitedAtGalactic = source["visitedAtGalactic"];
	        this.distFromPrevLy = source["distFromPrevLy"];
	        this.distFromCurrentLy = source["distFromCurrentLy"];
	        this.transitions = this.convertValues(source["transitions"], Transition);
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
	    nextFcStopRowId: number;
	    waypointCount: number;
	    visitedCount: number;
	    plannedLy: number;
	    remainingLy: number;
	    pendingTransitions: Transition[];
	    activeModeId: number;
	    activeModeName: string;
	    activeModeColor: string;
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
	        this.nextFcStopRowId = source["nextFcStopRowId"];
	        this.waypointCount = source["waypointCount"];
	        this.visitedCount = source["visitedCount"];
	        this.plannedLy = source["plannedLy"];
	        this.remainingLy = source["remainingLy"];
	        this.pendingTransitions = this.convertValues(source["pendingTransitions"], Transition);
	        this.activeModeId = source["activeModeId"];
	        this.activeModeName = source["activeModeName"];
	        this.activeModeColor = source["activeModeColor"];
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
	    metrics: stats.Metrics;
	    estDistFromPrevLy?: number;
	    estDistFromCurrentLy?: number;
	    currentSystem: string;
	    missingCoords: boolean;
	
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
	        this.metrics = this.convertValues(source["metrics"], stats.Metrics);
	        this.estDistFromPrevLy = source["estDistFromPrevLy"];
	        this.estDistFromCurrentLy = source["estDistFromCurrentLy"];
	        this.currentSystem = source["currentSystem"];
	        this.missingCoords = source["missingCoords"];
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

export namespace journal {
	
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

export namespace modes {
	
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
	    modes: ModeSelection[];
	
	    static createFrom(source: any = {}) {
	        return new Filter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
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
	    scanValueTotal: number;
	    scanValueBase: number;
	    scanValueBonus: number;
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
	        this.scanValueTotal = source["scanValueTotal"];
	        this.scanValueBase = source["scanValueBase"];
	        this.scanValueBonus = source["scanValueBonus"];
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

