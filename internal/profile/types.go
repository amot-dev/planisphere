// Package profile implements a standalone live-data performance profiler that
// executes backend and frontend operations against an isolated temporary
// snapshot of the user's SQLite database without modifying production code.
package profile

import "time"

// Options configures a live-data profiling run.
type Options struct {
	DBPath          string
	FilterPattern   string
	Iterations      int
	IncludeFrontend bool
	SavePath        string
	BaselinePath    string
}

// RepresentativeWindow identifies a real time range selected from the archive
// for multi-window scaling probes (SingleLeg, Expedition, and AllTime).
type RepresentativeWindow struct {
	Label          string `json:"label"`
	AfterTimestamp string `json:"afterTimestamp"`
	UntilTimestamp string `json:"untilTimestamp"`
	MetricEvents   int    `json:"metricEvents"`
	RealEvents     int    `json:"realEvents"`
}

// DatasetSummary describes the scale and shape of the snapshotted database.
type DatasetSummary struct {
	SourceDBPath            string                 `json:"sourceDbPath"`
	SnapshotDBPath          string                 `json:"snapshotDbPath"`
	DatabaseSizeBytes       int64                  `json:"databaseSizeBytes"`
	FilesTracked            int                    `json:"filesTracked"`
	TotalEvents             int                    `json:"totalEvents"`
	MetricEvents            int                    `json:"metricEvents"`
	RealizationEvents       int                    `json:"realizationEvents"`
	RawJSONBytesTotal       int64                  `json:"rawJsonBytesTotal"`
	CommanderCount          int                    `json:"commanderCount"`
	ExpeditionCount         int                    `json:"expeditionCount"`
	GECPOICount             int                    `json:"gecPoiCount"`
	PrimaryExpeditionID     int64                  `json:"primaryExpeditionId"`
	PrimaryExpeditionName   string                 `json:"primaryExpeditionName"`
	PrimaryExpeditionStatus string                 `json:"primaryExpeditionStatus"`
	PrimaryRowCount         int                    `json:"primaryRowCount"`
	PrimaryWaypointCount    int                    `json:"primaryWaypointCount"`
	PrimaryVisitedCount     int                    `json:"primaryVisitedCount"`
	OriginRowID             int64                  `json:"originRowId"`
	SampleVisitedLegRowID   int64                  `json:"sampleVisitedLegRowId"`
	SampleVisitedLegNumber  int                    `json:"sampleVisitedLegNumber"`
	Windows                 []RepresentativeWindow `json:"windows"`
}

// SpanNode represents a node in the hierarchical execution tree of a probe.
type SpanNode struct {
	Name             string        `json:"name"`
	Location         string        `json:"location,omitempty"`
	Category         string        `json:"category,omitempty"` // "root", "sql", "cpu", "subspan", "render"
	Calls            int           `json:"calls"`
	ItemsProcessed   int           `json:"itemsProcessed"`
	ItemUnit         string        `json:"itemUnit,omitempty"`
	TotalDuration    time.Duration `json:"totalDuration"`
	SelfDuration     time.Duration `json:"selfDuration"`
	SQLDuration      time.Duration `json:"sqlDuration"`
	GoCPUDuration    time.Duration `json:"goCpuDuration"`
	SQLQueries       int           `json:"sqlQueries"`
	RowsScanned      int           `json:"rowsScanned"`
	AllocBytes       uint64        `json:"allocBytes"`
	Mallocs          uint64        `json:"mallocs"`
	QueryPlans       []string      `json:"queryPlans,omitempty"`
	HasFullScan      bool          `json:"hasFullScan,omitempty"`
	HasTempBTree     bool          `json:"hasTempBTree,omitempty"`
	HasNPlusOne      bool          `json:"hasNPlusOne,omitempty"`
	Children         []*SpanNode   `json:"children,omitempty"`
}

// ProbeRun captures the median and best metrics of either the Cold or Warm pass.
type ProbeRun struct {
	Mode               string        `json:"mode"` // "Cold" or "Warm"
	Iterations         int           `json:"iterations"`
	MedianDuration     time.Duration `json:"medianDuration"`
	MinDuration        time.Duration `json:"minDuration"`
	MaxDuration        time.Duration `json:"maxDuration"`
	NormalizedNsPerItem float64      `json:"normalizedNsPerItem"`
	ItemsPerSecond     float64       `json:"itemsPerSecond"`
	AllocBytes         uint64        `json:"allocBytes"`
	BytesPerItem       float64       `json:"bytesPerItem"`
	Mallocs            uint64        `json:"mallocs"`
	AllocsPerItem      float64       `json:"allocsPerItem"`
	SQLQueries         int           `json:"sqlQueries"`
	SQLDuration        time.Duration `json:"sqlDuration"`
	GoCPUDuration      time.Duration `json:"goCpuDuration"`
	ResultHash         string        `json:"resultHash"`
	ResultSummary      string        `json:"resultSummary"`
	RootSpan           *SpanNode     `json:"rootSpan"`
}

// ProbeResult holds the Cold and Warm runs for a single benchmark probe.
type ProbeResult struct {
	Name            string   `json:"name"`
	Category        string   `json:"category"`
	Description     string   `json:"description"`
	ItemsProcessed  int      `json:"itemsProcessed"`
	ItemUnit        string   `json:"itemUnit"`
	SecondaryCount  int      `json:"secondaryCount,omitempty"`
	SecondaryUnit   string   `json:"secondaryUnit,omitempty"`
	Cold            ProbeRun `json:"cold"`
	Warm            ProbeRun `json:"warm"`
	HasWarm         bool     `json:"hasWarm"`
	ColdWarmMatch   bool     `json:"coldWarmMatch"`
	WarmSpeedupPct  float64  `json:"warmSpeedupPct"`
}

// ScalingPoint is one window observation used in linear scaling decomposition.
type ScalingPoint struct {
	WindowLabel string        `json:"windowLabel"`
	Items       int           `json:"items"`
	ItemUnit    string        `json:"itemUnit"`
	ColdTime    time.Duration `json:"coldTime"`
	WarmTime    time.Duration `json:"warmTime"`
	NsPerItem   float64       `json:"nsPerItem"`
}

// ScalingModel decomposes a windowed function into Fixed Setup Overhead (T0)
// and Marginal Per-Item Cost (m) across SingleLeg, Expedition, and AllTime windows.
type ScalingModel struct {
	FunctionName        string         `json:"functionName"`
	ItemUnit            string         `json:"itemUnit"`
	FixedSetupOverhead  time.Duration  `json:"fixedSetupOverhead"`
	MarginalNsPerItem   float64        `json:"marginalNsPerItem"`
	MarginalItemsPerSec float64        `json:"marginalItemsPerSec"`
	Points              []ScalingPoint `json:"points"`
}

// HotspotEntry highlights a specific leaf bottleneck or SQL anomaly.
type HotspotEntry struct {
	ProbeName      string        `json:"probeName"`
	RunMode        string        `json:"runMode"`
	SpanName       string        `json:"spanName"`
	Location       string        `json:"location"`
	SelfDuration   time.Duration `json:"selfDuration"`
	TotalDuration  time.Duration `json:"totalDuration"`
	SQLQueries     int           `json:"sqlQueries"`
	RowsScanned    int           `json:"rowsScanned"`
	AllocBytes     uint64        `json:"allocBytes"`
	Finding        string        `json:"finding"`
}

// Report is the complete output of a live-data profiling session.
type Report struct {
	GeneratedAt    string         `json:"generatedAt"`
	Dataset        DatasetSummary `json:"dataset"`
	Probes         []ProbeResult  `json:"probes"`
	ScalingModels  []ScalingModel `json:"scalingModels,omitempty"`
	TopBySelfTime  []HotspotEntry `json:"topBySelfTime,omitempty"`
	TopByMemory    []HotspotEntry `json:"topByMemory,omitempty"`
	SQLAnomalies   []HotspotEntry `json:"sqlAnomalies,omitempty"`
	WarmRemnants   []HotspotEntry `json:"warmRemnants,omitempty"`
}

// DiffEntry compares a single probe between a baseline report and a current report.
type DiffEntry struct {
	ProbeName         string        `json:"probeName"`
	HasWarm           bool          `json:"hasWarm"`
	ColdBaseline      time.Duration `json:"coldBaseline"`
	ColdCurrent       time.Duration `json:"coldCurrent"`
	ColdDeltaPct      float64       `json:"coldDeltaPct"`
	WarmBaseline      time.Duration `json:"warmBaseline"`
	WarmCurrent       time.Duration `json:"warmCurrent"`
	WarmDeltaPct      float64       `json:"warmDeltaPct"`
	AllocBaseline     uint64        `json:"allocBaseline"`
	AllocCurrent      uint64        `json:"allocCurrent"`
	AllocDeltaPct     float64       `json:"allocDeltaPct"`
	ResultHashMatched bool          `json:"resultHashMatched"`
}

// DiffReport summarizes all probe changes against a saved baseline.
type DiffReport struct {
	BaselineGeneratedAt string      `json:"baselineGeneratedAt"`
	Entries             []DiffEntry `json:"entries"`
	AnyResultMismatch   bool        `json:"anyResultMismatch"`
}

