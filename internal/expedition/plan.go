package expedition

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/journal"
	"planisphere/internal/modes"
	"planisphere/internal/stats"
)

// Row visit states.
const (
	VisitVisited = "visited"
	VisitCurrent = "current"
	VisitFuture  = "future"
)

// Transition is a ship or mode switch that happened during a leg.
type Transition struct {
	Kind       string `json:"kind"` // "mode" | "ship"
	At         string `json:"at"`
	AtGalactic string `json:"atGalactic"`
	From       string `json:"from"`
	To         string `json:"to"`
	FromColor  string `json:"fromColor"`
	ToColor    string `json:"toColor"`
}

// GECRef links a system to an EDAstro Galactic Exploration Catalog entry.
type GECRef struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	Count int    `json:"count"` // number of GEC POIs in the system
}

// RowView is a row enriched for display.
type RowView struct {
	Row               Row          `json:"row"`
	Number            int          `json:"number"` // waypoint # (0 for region headers)
	RegionName        string       `json:"regionName"`
	DetectedRegionID  int          `json:"detectedRegionId"` // region containing the coordinates
	DetectedRegion    string       `json:"detectedRegion"`
	VisitState        string       `json:"visitState"`
	VisitedAtGalactic string       `json:"visitedAtGalactic"`
	DistFromPrevLy    *float64     `json:"distFromPrevLy,omitempty"`
	DistFromCurrentLy *float64     `json:"distFromCurrentLy,omitempty"`
	Transitions       []Transition `json:"transitions"` // ships flown during the leg arriving at this waypoint
	LegModeID         int64        `json:"legModeId"`
	LegModeName       string       `json:"legModeName"`
	LegModeColor      string       `json:"legModeColor"`
	GEC               *GECRef      `json:"gec,omitempty"`
}

// PlanView is the full planner sheet.
type PlanView struct {
	Expedition         Expedition   `json:"expedition"`
	StartGalactic      string       `json:"startGalactic"`
	EndGalactic        string       `json:"endGalactic"`
	Rows               []RowView    `json:"rows"`
	CurrentRowID       int64        `json:"currentRowId"`
	NextFCStopRowID    int64        `json:"nextFcStopRowId"`
	WaypointCount      int          `json:"waypointCount"`
	VisitedCount       int          `json:"visitedCount"`
	PlannedLy          float64      `json:"plannedLy"`
	RemainingLy        float64      `json:"remainingLy"`
	PendingTransitions []Transition `json:"pendingTransitions"` // ships flown since the current waypoint
	ActiveModeID       int64        `json:"activeModeId"`
	ActiveModeName     string       `json:"activeModeName"`
	ActiveModeColor    string       `json:"activeModeColor"`
	Modes              []modes.Mode `json:"modes"`
}

// GECLookup resolves GEC entries for a system.
type GECLookup func(systemName string, systemAddress int64) *GECRef

// ShipChange is one ship switch.
type shipState struct {
	at   string
	id   int64
	name string
}

func loadShipTimeline(q db.Queryer) ([]shipState, error) {
	rs, err := q.Query(`SELECT timestamp, event, raw_json FROM journal_events WHERE event IN ('LoadGame','ShipyardSwap') AND ` + journal.CmdrFilterSQL + ` ORDER BY timestamp, id`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []shipState
	for rs.Next() {
		var ts, ev, raw string
		if err := rs.Scan(&ts, &ev, &raw); err != nil {
			return nil, err
		}
		var s shipState
		s.at = ts
		switch ev {
		case "LoadGame":
			var lg journal.LoadGameEvent
			if json.Unmarshal([]byte(raw), &lg) != nil || lg.Ship == "" || !isShip(lg.Ship) {
				continue
			}
			s.id, s.name = lg.ShipID, ed.ShipDisplayName(lg.Ship, lg.ShipLocalised)
		case "ShipyardSwap":
			var sw journal.ShipyardSwapEvent
			if json.Unmarshal([]byte(raw), &sw) != nil || sw.ShipType == "" {
				continue
			}
			s.id, s.name = sw.ShipID, ed.ShipDisplayName(sw.ShipType, sw.ShipTypeLocalised)
		}
		out = append(out, s)
	}
	return out, rs.Err()
}

// isShip filters out on-foot suits, SRVs and fighters reported by LoadGame.
func isShip(internal string) bool {
	l := strings.ToLower(internal)
	return !strings.Contains(l, "suit") && !strings.Contains(l, "buggy") && !strings.Contains(l, "srv") &&
		!strings.Contains(l, "fighter")
}

func shipAt(tl []shipState, ts string) *shipState {
	i := sort.Search(len(tl), func(i int) bool { return tl[i].at > ts }) - 1
	if i < 0 {
		return nil
	}
	return &tl[i]
}

func loadJumpTimestamps(q db.Queryer, after string) ([]string, error) {
	rs, err := q.Query(`SELECT timestamp FROM journal_events WHERE event = 'FSDJump' AND timestamp >= ? AND `+journal.CmdrFilterSQL+` ORDER BY timestamp, id`, after)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []string
	for rs.Next() {
		var ts string
		if err := rs.Scan(&ts); err != nil {
			return nil, err
		}
		out = append(out, ts)
	}
	return out, rs.Err()
}

func shipsUsedOnLeg(tl []shipState, jumps []string, after, until string) []Transition {
	var out []Transition
	seen := map[string]bool{}
	startIdx := sort.Search(len(jumps), func(i int) bool { return jumps[i] > after })
	for i := startIdx; i < len(jumps); i++ {
		ts := jumps[i]
		if ts > until {
			break
		}
		s := shipAt(tl, ts)
		if s == nil || s.name == "" || seen[s.name] {
			continue
		}
		seen[s.name] = true
		out = append(out, Transition{
			Kind:       "ship",
			At:         ts,
			AtGalactic: ed.ToGalactic(ts),
			To:         s.name,
		})
	}
	return out
}


// BuildPlan assembles the planner view for an expedition.
func BuildPlan(q db.Queryer, expID int64, gec GECLookup) (PlanView, error) {
	e, err := Get(q, expID)
	if err != nil {
		return PlanView{}, err
	}
	rows, err := ListRows(q, expID)
	if err != nil {
		return PlanView{}, err
	}
	pv := PlanView{Expedition: e, StartGalactic: ed.ToGalactic(e.StartTimestamp), EndGalactic: ed.ToGalactic(e.EndTimestamp)}

	// Current waypoint = visited waypoint with the latest visit time.
	var current *Row
	for i := range rows {
		r := &rows[i]
		if r.IsWaypoint() && r.VisitedAt != "" && (current == nil || r.VisitedAt > current.VisitedAt) {
			current = r
		}
	}
	if current != nil {
		pv.CurrentRowID = current.ID
	}

	var shipTL []shipState
	var jumpTS []string
	modeTL, err := modes.LoadTimeline(q)
	if err != nil {
		return pv, err
	}
	ms, err := modes.List(q)
	if err != nil {
		return pv, err
	}
	pv.Modes = ms
	modeByID := map[int64]modes.Mode{}
	for _, m := range ms {
		modeByID[m.ID] = m
	}
	defaultModeID := modes.DefaultFallbackModeID(q)
	resolveMode := func(id int64) (int64, string, string) {
		if m, ok := modeByID[id]; ok {
			return m.ID, m.Name, m.Color
		}
		if m, ok := modeByID[defaultModeID]; ok {
			return m.ID, m.Name, m.Color
		}
		if len(ms) > 0 {
			return ms[0].ID, ms[0].Name, ms[0].Color
		}
		return 0, "Travel", "#FF7100"
	}

	if e.StartTimestamp != "" {
		if shipTL, err = loadShipTimeline(q); err != nil {
			return pv, err
		}
		if jumpTS, err = loadJumpTimestamps(q, e.StartTimestamp); err != nil {
			return pv, err
		}
	}

	// Visited waypoints sorted by visit time for leg transitions.
	prevVisit := map[int64]string{}
	var visited []Row
	for _, r := range rows {
		if r.IsWaypoint() && r.VisitedAt != "" {
			visited = append(visited, r)
		}
	}
	sort.SliceStable(visited, func(i, j int) bool { return visited[i].VisitedAt < visited[j].VisitedAt })
	for i := 1; i < len(visited); i++ {
		prevVisit[visited[i].ID] = visited[i-1].VisitedAt
	}

	num := 0
	var prevWP *Row
	var curPos *ed.Vec3
	if current != nil && current.HasCoords {
		p, _ := current.Coords()
		curPos = &p
	}
	for i := range rows {
		r := rows[i]
		v := RowView{Row: r, Transitions: []Transition{}}
		if r.RowType == RowRegion {
			if reg, ok := ed.RegionByID(r.RegionID); ok {
				v.RegionName = reg.Name
			}
			pv.Rows = append(pv.Rows, v)
			continue
		}
		num++
		pv.WaypointCount++
		v.Number = num
		if r.FCStop && !r.FCArrived && pv.NextFCStopRowID == 0 {
			pv.NextFCStopRowID = r.ID
		}
		if r.HasCoords {
			if reg, ok := ed.RegionAt(r.X, r.Y, r.Z); ok {
				v.DetectedRegionID, v.DetectedRegion = reg.ID, reg.Name
			}
		}
		switch {
		case current != nil && r.ID == current.ID:
			v.VisitState = VisitCurrent
		case r.VisitedAt != "":
			v.VisitState = VisitVisited
		default:
			v.VisitState = VisitFuture
		}
		if r.VisitedAt != "" {
			pv.VisitedCount++
			v.VisitedAtGalactic = ed.ToGalactic(r.VisitedAt)
			if after, ok := prevVisit[r.ID]; ok && shipTL != nil {
				v.Transitions = shipsUsedOnLeg(shipTL, jumpTS, after, r.VisitedAt)
				mID := r.LegModeID
				if mID == 0 && modeTL != nil {
					mID = modeTL.ModeAt(r.VisitedAt)
				}
				v.LegModeID, v.LegModeName, v.LegModeColor = resolveMode(mID)
			}
		} else if num > 1 && r.LegModeID > 0 {
			v.LegModeID, v.LegModeName, v.LegModeColor = resolveMode(r.LegModeID)
		}
		if prevWP != nil && prevWP.HasCoords && r.HasCoords {
			a, _ := prevWP.Coords()
			b, _ := r.Coords()
			d := ed.Distance(a, b)
			v.DistFromPrevLy = &d
			pv.PlannedLy += d
			if r.VisitedAt == "" {
				pv.RemainingLy += d
			}
		}
		if r.VisitedAt == "" && curPos != nil && r.HasCoords {
			b, _ := r.Coords()
			d := ed.Distance(*curPos, b)
			v.DistFromCurrentLy = &d
		}
		if gec != nil && r.SystemName != "" {
			v.GEC = gec(r.SystemName, r.SystemAddress)
		}
		rr := r
		prevWP = &rr
		pv.Rows = append(pv.Rows, v)
	}
	if pv.Rows == nil {
		pv.Rows = []RowView{}
	}
	pv.PendingTransitions = []Transition{}
	if current != nil && shipTL != nil {
		until := e.EndTimestamp
		if until == "" {
			until = ed.NowISO()
		}
		pv.PendingTransitions = shipsUsedOnLeg(shipTL, jumpTS, current.VisitedAt, until)
		mID := modeTL.ModeAt(until)
		pv.ActiveModeID, pv.ActiveModeName, pv.ActiveModeColor = resolveMode(mID)
	} else {
		pv.ActiveModeID, pv.ActiveModeName, pv.ActiveModeColor = resolveMode(modeTL.ModeAt(ed.NowISO()))
	}
	return pv, nil
}

func addOneSecond(iso string) string {
	t, err := ed.ParseISO(iso)
	if err != nil {
		return iso
	}
	return t.Add(time.Second).UTC().Format("2006-01-02T15:04:05Z")
}

// SetLegMode updates the single mode assigned to the leg arriving at rowID.
// When the waypoint has been visited (after a previous visited waypoint), it
// also updates the corresponding time window (prevVisit, rowVisit] in mode_windows
// so filtered expedition and all-time stats immediately reflect the change.
func SetLegMode(tx *sql.Tx, rowID int64, modeID int64) (int64, error) {
	r, err := GetRow(tx, rowID)
	if err != nil {
		return 0, err
	}
	if _, err := UpdateRow(tx, RowPatch{ID: rowID, LegModeID: &modeID}); err != nil {
		return 0, err
	}
	if r.VisitedAt != "" {
		rows, err := ListRows(tx, r.ExpeditionID)
		if err != nil {
			return r.ExpeditionID, err
		}
		prevTS := ""
		for _, cand := range rows {
			if cand.IsWaypoint() && cand.VisitedAt != "" && cand.VisitedAt < r.VisitedAt && cand.VisitedAt > prevTS {
				prevTS = cand.VisitedAt
			}
		}
		if prevTS != "" {
			startISO := addOneSecond(prevTS)
			endISO := addOneSecond(r.VisitedAt)
			if endISO > startISO {
				if err := modes.SetWindow(tx, 0, modeID, startISO, endISO); err != nil {
					return r.ExpeditionID, err
				}
			}
		}
	}
	return r.ExpeditionID, nil
}

// SetActiveLegMode sets the mode for the current in-progress leg (from the
// latest visited waypoint's timestamp forward).
func SetActiveLegMode(tx *sql.Tx, expID int64, modeID int64) error {
	rows, err := ListRows(tx, expID)
	if err != nil {
		return err
	}
	latestTS := ""
	for _, r := range rows {
		if r.IsWaypoint() && r.VisitedAt != "" && r.VisitedAt > latestTS {
			latestTS = r.VisitedAt
		}
	}
	if latestTS != "" {
		return modes.SetWindow(tx, 0, modeID, addOneSecond(latestTS), "")
	}
	return modes.Switch(tx, modeID, "")
}

// WaypointDetails is the content of a waypoint's expandable drawer.
type WaypointDetails struct {
	RowID        int64         `json:"rowId"`
	Kind         string        `json:"kind"` // "origin" (waypoint #1), "leg" (visited N>1), "future"
	Number       int           `json:"number"`
	From         string        `json:"from"`
	To           string        `json:"to"`
	FromGalactic string        `json:"fromGalactic"`
	ToGalactic   string        `json:"toGalactic"`
	Elapsed      string        `json:"elapsed"`
	PrevSystem   string        `json:"prevSystem"`
	Metrics      stats.Metrics `json:"metrics"`

	EstDistFromPrevLy    *float64 `json:"estDistFromPrevLy,omitempty"`
	EstDistFromCurrentLy *float64 `json:"estDistFromCurrentLy,omitempty"`
	CurrentSystem        string   `json:"currentSystem"`
	MissingCoords        bool     `json:"missingCoords"`
}

// Details computes the drawer content for a waypoint row.
func Details(conn *sql.DB, rowID int64) (WaypointDetails, error) {
	r, err := GetRow(conn, rowID)
	if err != nil {
		return WaypointDetails{}, err
	}
	pv, err := BuildPlan(conn, r.ExpeditionID, nil)
	if err != nil {
		return WaypointDetails{}, err
	}
	var self *RowView
	var prev *RowView
	var current *RowView
	for i := range pv.Rows {
		v := &pv.Rows[i]
		if v.Row.RowType != RowWaypoint {
			continue
		}
		if v.Row.ID == pv.CurrentRowID {
			current = v
		}
		if v.Row.ID == rowID {
			self = v
		} else if self == nil {
			prev = v
		}
	}
	if self == nil {
		return WaypointDetails{}, nil
	}
	d := WaypointDetails{RowID: rowID, Number: self.Number}
	if prev != nil {
		d.PrevSystem = prev.Row.SystemName
	}
	engine := stats.NewEngine(conn)

	if self.Row.VisitedAt == "" {
		d.Kind = "future"
		d.EstDistFromPrevLy = self.DistFromPrevLy
		d.EstDistFromCurrentLy = self.DistFromCurrentLy
		if current != nil {
			d.CurrentSystem = current.Row.SystemName
		}
		d.MissingCoords = !self.Row.HasCoords || (prev != nil && !prev.Row.HasCoords)
		return d, nil
	}

	if self.Number == 1 {
		d.Kind = "origin"
		d.To, d.ToGalactic = self.Row.VisitedAt, ed.ToGalactic(self.Row.VisitedAt)
		var first sql.NullString
		_ = conn.QueryRow(`SELECT MIN(timestamp) FROM journal_events WHERE `+journal.CmdrFilterSQL).Scan(&first)
		d.From, d.FromGalactic = first.String, ed.ToGalactic(first.String)
		d.Metrics, err = engine.Compute(stats.Range{After: "", Until: self.Row.VisitedAt}, nil)
	} else {
		d.Kind = "leg"
		// The leg starts at the visit of the waypoint visited immediately before this one.
		after := ""
		for i := range pv.Rows {
			v := pv.Rows[i]
			if v.Row.RowType == RowWaypoint && v.Row.VisitedAt != "" && v.Row.VisitedAt < self.Row.VisitedAt && v.Row.VisitedAt > after {
				after = v.Row.VisitedAt
				d.PrevSystem = v.Row.SystemName
			}
		}
		if after == "" {
			after = pv.Expedition.StartTimestamp
		}
		d.From, d.FromGalactic = after, ed.ToGalactic(after)
		d.To, d.ToGalactic = self.Row.VisitedAt, ed.ToGalactic(self.Row.VisitedAt)
		d.Metrics, err = engine.Compute(stats.Range{After: after, Until: self.Row.VisitedAt}, nil)
	}
	if err != nil {
		return d, err
	}
	if a, err1 := ed.ParseISO(d.From); err1 == nil {
		if b, err2 := ed.ParseISO(d.To); err2 == nil {
			d.Elapsed = ed.DurationLabel(b.Sub(a))
		}
	}
	return d, nil
}

// Elapsed returns a human label between two ISO timestamps (now when to is empty).
// Uses rounded days and years without hours/minutes.
func Elapsed(from, to string) string {
	a, err := ed.ParseISO(from)
	if err != nil {
		return ""
	}
	b := time.Now().UTC()
	if to != "" {
		if t, err := ed.ParseISO(to); err == nil {
			b = t
		}
	}
	return ed.DurationDaysLabel(b.Sub(a))
}
