// Package stats computes expedition metrics from persisted journal events.
// Everything is derived from journal_events.raw_json, so metrics can be
// recomputed at any time (with updated formulas) even after the original
// journal files are gone.
package stats

import (
	"database/sql"
	"encoding/json"
	"strings"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/journal"
	"planisphere/internal/modes"
)

// Metrics are the tracked expedition statistics.
type Metrics struct {
	DistanceLy         float64 `json:"distanceLy"`
	ShipDistanceLy     float64 `json:"shipDistanceLy"`     // subset: hyperspace FSD jumps
	CarrierPassengerLy float64 `json:"carrierPassengerLy"` // subset: travelled while aboard a carrier
	Jumps              int     `json:"jumps"`
	CarrierJumps       int     `json:"carrierJumps"`
	CarrierDistanceLy  float64 `json:"carrierDistanceLy"`
	TritiumUsed        *int    `json:"tritiumUsed"` // nil when N/A (not recorded/estimated)
	JetConeBoosts      int     `json:"jetConeBoosts"`
	PremiumBoosts      int     `json:"premiumBoosts"`
	StandardBoosts     int     `json:"standardBoosts"`
	BasicBoosts        int     `json:"basicBoosts"`
	BodiesScanned      int     `json:"bodiesScanned"`
	BodiesMapped       int     `json:"bodiesMapped"`
	ScanValueTotal     int64   `json:"scanValueTotal"`
	ScanValueBase      int64   `json:"scanValueBase"`
	ScanValueBonus     int64   `json:"scanValueBonus"`
	FirstDiscoveries   int     `json:"firstDiscoveries"`
	FirstMapped        int     `json:"firstMapped"`
	OrganicSamples     int     `json:"organicSamples"`
	OrganicValueTotal  int64   `json:"organicValueTotal"`
	OrganicValueBase   int64   `json:"organicValueBase"`
	OrganicValueBonus  int64   `json:"organicValueBonus"`
	FirstLogged        int     `json:"firstLogged"`
}

// Add returns the field-wise sum of m and o.
func (m Metrics) Add(o Metrics) Metrics {
	var trit *int
	if m.TritiumUsed != nil || o.TritiumUsed != nil {
		t := 0
		if m.TritiumUsed != nil {
			t += *m.TritiumUsed
		}
		if o.TritiumUsed != nil {
			t += *o.TritiumUsed
		}
		trit = &t
	}
	return Metrics{
		DistanceLy:         m.DistanceLy + o.DistanceLy,
		ShipDistanceLy:     m.ShipDistanceLy + o.ShipDistanceLy,
		CarrierPassengerLy: m.CarrierPassengerLy + o.CarrierPassengerLy,
		Jumps:              m.Jumps + o.Jumps,
		CarrierJumps:       m.CarrierJumps + o.CarrierJumps,
		CarrierDistanceLy:  m.CarrierDistanceLy + o.CarrierDistanceLy,
		TritiumUsed:        trit,
		JetConeBoosts:      m.JetConeBoosts + o.JetConeBoosts,
		PremiumBoosts:      m.PremiumBoosts + o.PremiumBoosts,
		StandardBoosts:     m.StandardBoosts + o.StandardBoosts,
		BasicBoosts:        m.BasicBoosts + o.BasicBoosts,
		BodiesScanned:      m.BodiesScanned + o.BodiesScanned,
		BodiesMapped:       m.BodiesMapped + o.BodiesMapped,
		ScanValueTotal:     m.ScanValueTotal + o.ScanValueTotal,
		ScanValueBase:      m.ScanValueBase + o.ScanValueBase,
		ScanValueBonus:     m.ScanValueBonus + o.ScanValueBonus,
		FirstDiscoveries:   m.FirstDiscoveries + o.FirstDiscoveries,
		FirstMapped:        m.FirstMapped + o.FirstMapped,
		OrganicSamples:     m.OrganicSamples + o.OrganicSamples,
		OrganicValueTotal:  m.OrganicValueTotal + o.OrganicValueTotal,
		OrganicValueBase:   m.OrganicValueBase + o.OrganicValueBase,
		OrganicValueBonus:  m.OrganicValueBonus + o.OrganicValueBonus,
		FirstLogged:        m.FirstLogged + o.FirstLogged,
	}
}

func (m *Metrics) addScan(v ed.ScanValue) {
	m.ScanValueTotal += v.Total
	m.ScanValueBase += v.Base
	m.ScanValueBonus += v.Bonus
}

func (m *Metrics) addOrganic(v ed.ScanValue) {
	m.OrganicValueTotal += v.Total
	m.OrganicValueBase += v.Base
	m.OrganicValueBonus += v.Bonus
}

// Range is the half-open time range (After, Until]. Empty After means "from
// the beginning of time"; empty Until means "up to now".
type Range struct {
	After string `json:"after"`
	Until string `json:"until"`
}

func (r Range) bounds() (string, string) {
	until := r.Until
	if until == "" {
		until = "9999-12-31T23:59:59Z"
	}
	return r.After, until
}

// ModeSelection controls which modes contribute to which metric family.
type ModeSelection struct {
	ModeID    int64 `json:"modeId"`    // modes.UnassignedID (0) = time not covered by a window
	Travel    bool  `json:"travel"`    // jumps, distance, boosts, carrier passenger travel (carrier operations ignore mode)
	Discovery bool  `json:"discovery"` // scans, mapping, values, exobiology
}

// Filter restricts metrics to events that occurred while selected modes were
// active. A nil Filter (or Enabled == false) includes everything.
type Filter struct {
	Enabled bool            `json:"enabled"`
	Modes   []ModeSelection `json:"modes"`
}

type filterSets struct {
	travel, discovery map[int64]bool
	timeline          *modes.Timeline
}

func (f *Filter) prepare(q db.Queryer) (*filterSets, error) {
	if f == nil || !f.Enabled {
		return nil, nil
	}
	tl, err := modes.LoadTimeline(q)
	if err != nil {
		return nil, err
	}
	fs := &filterSets{travel: map[int64]bool{}, discovery: map[int64]bool{}, timeline: tl}
	for _, m := range f.Modes {
		if m.Travel {
			fs.travel[m.ModeID] = true
		}
		if m.Discovery {
			fs.discovery[m.ModeID] = true
		}
	}
	return fs, nil
}

func (fs *filterSets) allow(ts string, discovery bool) bool {
	if fs == nil {
		return true
	}
	mode := fs.timeline.ModeAt(ts)
	if discovery {
		return fs.discovery[mode]
	}
	return fs.travel[mode]
}

// Engine computes Metrics from the database.
type Engine struct {
	DB      db.Queryer
	Odyssey bool
}

// NewEngine creates an engine (Odyssey mapping bonus enabled).
func NewEngine(q db.Queryer) *Engine { return &Engine{DB: q, Odyssey: true} }

type row struct {
	uid     string
	ts      string
	event   string
	sysAddr sql.NullInt64
	bodyID  sql.NullInt64
	starPos *ed.Vec3
	raw     string
}

var metricEvents = []string{"FSDJump", "CarrierJump", "Scan", "SAAScanComplete", "ScanOrganic"}

func (e *Engine) loadEvents(after, until string, names []string) ([]row, error) {
	ph := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	args := []any{}
	for _, n := range names {
		args = append(args, n)
	}
	args = append(args, after, until)
	rs, err := e.DB.Query(`SELECT event_uid, timestamp, event, system_address, body_id, star_x, star_y, star_z, raw_json
		FROM journal_events WHERE event IN (`+ph+`) AND timestamp > ? AND timestamp <= ? AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		var x, y, z sql.NullFloat64
		if err := rs.Scan(&r.uid, &r.ts, &r.event, &r.sysAddr, &r.bodyID, &x, &y, &z, &r.raw); err != nil {
			return nil, err
		}
		if x.Valid && y.Valid && z.Valid {
			r.starPos = &ed.Vec3{X: x.Float64, Y: y.Float64, Z: z.Float64}
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

type bodyKey struct{ sys, body int64 }

// lookupScan finds the most recent Scan of a body at or before ts.
func (e *Engine) lookupScan(k bodyKey, ts string) (*journal.ScanEvent, error) {
	var raw string
	err := e.DB.QueryRow(`SELECT raw_json FROM journal_events WHERE event = 'Scan' AND system_address = ? AND body_id = ?
		AND timestamp <= ? AND `+journal.CmdrFilterSQL+` ORDER BY timestamp DESC LIMIT 1`, k.sys, k.body, ts).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s journal.ScanEvent
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, nil
	}
	return &s, nil
}

func toBodyScan(s *journal.ScanEvent) ed.BodyScan {
	return ed.BodyScan{
		StarType: s.StarType, PlanetClass: s.PlanetClass, MassEM: s.MassEM, StellarMass: s.StellarMass,
		TerraformState: s.TerraformState, WasDiscovered: s.WasDiscovered, WasMapped: s.WasMapped,
	}
}

// Compute returns metrics for events in r, optionally filtered by mode.
func (e *Engine) Compute(r Range, f *Filter) (Metrics, error) {
	var m Metrics
	after, until := r.bounds()
	fs, err := f.prepare(e.DB)
	if err != nil {
		return m, err
	}
	events, err := e.loadEvents(after, until, metricEvents)
	if err != nil {
		return m, err
	}
	var carrier map[string]CarrierLeg
	scans := map[bodyKey]*journal.ScanEvent{}
	scanned := map[bodyKey]bool{}
	mapped := map[bodyKey]bool{}

	for _, ev := range events {
		switch ev.event {
		case "FSDJump":
			if !fs.allow(ev.ts, false) {
				continue
			}
			var j journal.FSDJumpEvent
			if json.Unmarshal([]byte(ev.raw), &j) != nil {
				continue
			}
			m.Jumps++
			m.ShipDistanceLy += j.JumpDist
			m.DistanceLy += j.JumpDist
			switch j.BoostUsed {
			case 4:
				m.JetConeBoosts++
			case 3:
				m.PremiumBoosts++
			case 2:
				m.StandardBoosts++
			case 1:
				m.BasicBoosts++
			}
		case "CarrierJump":
			if carrier == nil {
				if carrier, err = e.CarrierLegsRange(after, until); err != nil {
					return m, err
				}
			}
			leg := carrier[ev.uid]
			if fs.allow(ev.ts, false) {
				m.CarrierPassengerLy += leg.DistanceLy
				m.DistanceLy += leg.DistanceLy
			}
			m.CarrierJumps++
			m.CarrierDistanceLy += leg.DistanceLy
			if leg.Tritium > 0 {
				cur := 0
				if m.TritiumUsed != nil {
					cur = *m.TritiumUsed
				}
				next := cur + leg.Tritium
				m.TritiumUsed = &next
			}
		case "Scan":
			var s journal.ScanEvent
			if json.Unmarshal([]byte(ev.raw), &s) != nil {
				continue
			}
			if s.IsBeltCluster() {
				continue
			}
			k := bodyKey{s.SystemAddress, s.BodyID}
			sc := s
			scans[k] = &sc
			if !fs.allow(ev.ts, true) {
				continue
			}
			m.BodiesScanned++
			if s.ScanType == "NavBeaconDetail" || scanned[k] {
				continue
			}
			scanned[k] = true
			if !s.WasDiscovered {
				m.FirstDiscoveries++
			}
			m.addScan(ed.EstimateBodyValue(toBodyScan(&s), ed.MappingInfo{}, e.Odyssey))
		case "SAAScanComplete":
			var sa journal.SAAScanCompleteEvent
			if json.Unmarshal([]byte(ev.raw), &sa) != nil {
				continue
			}
			k := bodyKey{sa.SystemAddress, sa.BodyID}
			if mapped[k] || !fs.allow(ev.ts, true) {
				continue
			}
			mapped[k] = true
			m.BodiesMapped++
			scan := scans[k]
			if scan == nil {
				if scan, err = e.lookupScan(k, ev.ts); err != nil {
					return m, err
				}
				if scan != nil {
					scans[k] = scan
				}
			}
			if scan == nil {
				continue
			}
			if !scan.WasMapped {
				m.FirstMapped++
			}
			mi := ed.MappingInfo{Mapped: true, ProbesUsed: sa.ProbesUsed, EfficiencyTarget: sa.EfficiencyTarget}
			m.addScan(ed.EstimateMappingIncrement(toBodyScan(scan), mi, e.Odyssey))
		case "ScanOrganic":
			var o journal.ScanOrganicEvent
			if json.Unmarshal([]byte(ev.raw), &o) != nil || !strings.EqualFold(o.ScanType, "Analyse") {
				continue
			}
			if !fs.allow(ev.ts, true) {
				continue
			}
			m.OrganicSamples++
			base := ed.SpeciesBaseValue(o.Species, o.SpeciesLocalised, o.Genus)
			first, err := e.firstLogged(&o, scans, ev.ts)
			if err != nil {
				return m, err
			}
			if first {
				m.FirstLogged++
			}
			m.addOrganic(ed.OrganicValue(base, first))
		}
	}
	return m, nil
}

// firstLogged decides whether the first-logged (5x) bonus applies. The
// ScanOrganic WasLogged flag is authoritative when the journal provides it;
// older journals fall back to the parent body's Scan WasFootfalled == false.
func (e *Engine) firstLogged(o *journal.ScanOrganicEvent, scans map[bodyKey]*journal.ScanEvent, ts string) (bool, error) {
	if o.WasLogged != nil {
		return !*o.WasLogged, nil
	}
	k := bodyKey{o.SystemAddress, o.Body}
	scan := scans[k]
	if scan == nil {
		var err error
		if scan, err = e.lookupScan(k, ts); err != nil {
			return false, err
		}
		if scan != nil {
			scans[k] = scan
		}
	}
	if scan == nil || scan.WasFootfalled == nil {
		return false, nil
	}
	return !*scan.WasFootfalled, nil
}

// CarrierLeg is the derived distance/tritium of a single CarrierJump.
type CarrierLeg struct {
	DistanceLy float64
	Tritium    int
	FromDelta  bool // tritium measured from CarrierStats fuel delta rather than estimated
}

// CarrierLegs simulates the carrier's history up to until and returns the
// distance and tritium of every CarrierJump keyed by event UID. Distance is the
// 3D distance from the player's immediately preceding system position
// (Location, FSDJump, or CarrierJump) to the CarrierJump destination StarPos.
// Tritium is taken from the CarrierStats fuel delta when a jump is bracketed by
// two CarrierStats snapshots with no other jump in between (and the delta is
// plausible); otherwise it is estimated with ed.CarrierTritiumCost using the
// latest known fuel level and used capacity.
func (e *Engine) CarrierLegs(until string) (map[string]CarrierLeg, error) {
	return e.CarrierLegsRange("", until)
}

// CarrierLegsRange simulates carrier jumps in (after, until], seeding the
// initial position and CarrierStats snapshot from the most recent events at or
// before after when after is non-empty.
func (e *Engine) CarrierLegsRange(after, until string) (map[string]CarrierLeg, error) {
	var prevPos *ed.Vec3
	fuel, used := 1000, 0 // assumptions until a CarrierStats snapshot is seen
	var lastStats *journal.CarrierStatsEvent

	if after != "" {
		var x, y, z sql.NullFloat64
		err := e.DB.QueryRow(`SELECT star_x, star_y, star_z FROM journal_events
			WHERE event IN ('FSDJump', 'Location', 'CarrierJump') AND star_x IS NOT NULL
			AND timestamp <= ? AND `+journal.CmdrFilterSQL+`
			ORDER BY timestamp DESC, id DESC LIMIT 1`, after).Scan(&x, &y, &z)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if x.Valid && y.Valid && z.Valid {
			prevPos = &ed.Vec3{X: x.Float64, Y: y.Float64, Z: z.Float64}
		}

		var rawCS string
		err = e.DB.QueryRow(`SELECT raw_json FROM journal_events
			WHERE event = 'CarrierStats' AND timestamp <= ? AND `+journal.CmdrFilterSQL+`
			ORDER BY timestamp DESC, id DESC LIMIT 1`, after).Scan(&rawCS)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if rawCS != "" {
			var cs journal.CarrierStatsEvent
			if json.Unmarshal([]byte(rawCS), &cs) == nil {
				lastStats = &cs
				fuel, used = cs.FuelLevel, cs.UsedCapacity()
			}
		}
	}

	events, err := e.loadEvents(after, until, []string{"FSDJump", "Location", "CarrierJump", "CarrierStats"})
	if err != nil {
		return nil, err
	}
	out := map[string]CarrierLeg{}
	var pendingJump string // jump since lastStats (only if exactly one)
	jumpsSinceStats := 0

	for _, ev := range events {
		switch ev.event {
		case "FSDJump", "Location":
			if ev.starPos != nil {
				p := *ev.starPos
				prevPos = &p
			}
		case "CarrierStats":
			var cs journal.CarrierStatsEvent
			if json.Unmarshal([]byte(ev.raw), &cs) != nil {
				continue
			}
			if lastStats != nil && jumpsSinceStats == 1 && pendingJump != "" && cs.CarrierID == lastStats.CarrierID {
				delta := lastStats.FuelLevel - cs.FuelLevel
				leg := out[pendingJump]
				if delta > 0 && leg.Tritium > 0 && delta <= leg.Tritium*2+10 {
					leg.Tritium, leg.FromDelta = delta, true
					out[pendingJump] = leg
				}
			}
			c := cs
			lastStats = &c
			fuel, used = cs.FuelLevel, cs.UsedCapacity()
			jumpsSinceStats, pendingJump = 0, ""
		case "CarrierJump":
			leg := CarrierLeg{}
			if prevPos != nil && ev.starPos != nil {
				leg.DistanceLy = ed.Distance(*prevPos, *ev.starPos)
				leg.Tritium = ed.CarrierTritiumCost(leg.DistanceLy, used, fuel)
				fuel -= leg.Tritium
				if fuel < 0 {
					fuel = 0
				}
			}
			if ev.starPos != nil {
				p := *ev.starPos
				prevPos = &p
			}
			out[ev.uid] = leg
			jumpsSinceStats++
			pendingJump = ev.uid
		}
	}
	return out, nil
}

// MetricsFromStatistics maps a lifetime Statistics event onto Metrics for use
// as a "before expedition" baseline.
//
// Note: Frontier's Statistics event does not record DSS-mapped body count
// (Planets_Scanned_To_Level_2 and Planets_Scanned_To_Level_3 are pre-3.3
// Horizons DSS scan level counters and are identical in modern journals), so
// BodiesMapped is left 0 rather than duplicating BodiesScanned.
func MetricsFromStatistics(s journal.StatisticsEvent) Metrics {
	return Metrics{
		DistanceLy:        s.Exploration.TotalHyperspaceDist,
		ShipDistanceLy:    s.Exploration.TotalHyperspaceDist,
		Jumps:             int(s.Exploration.TotalHyperspaceJumps),
		BodiesScanned:     int(s.Exploration.PlanetsScannedToLevel3),
		BodiesMapped:      0,
		ScanValueTotal:    int64(s.Exploration.ExplorationProfits),
		ScanValueBase:     int64(s.Exploration.ExplorationProfits),
		OrganicSamples:    int(s.Exobiology.OrganicData),
		OrganicValueTotal: int64(s.Exobiology.OrganicDataProfits),
		OrganicValueBase:  int64(s.Exobiology.OrganicDataProfits - s.Exobiology.FirstLoggedProfits),
		OrganicValueBonus: int64(s.Exobiology.FirstLoggedProfits),
		FirstLogged:       int(s.Exobiology.FirstLogged),
		CarrierJumps:      int(s.FleetCarrierNumber("FLEETCARRIER_TOTAL_JUMPS")),
		CarrierDistanceLy: s.FleetCarrierNumber("FLEETCARRIER_DISTANCE_TRAVELLED"),
		TritiumUsed:       nil,
	}
}
