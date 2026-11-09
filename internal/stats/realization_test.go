package stats_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/db"
	"planisphere/internal/journal"
	"planisphere/internal/stats"
)

func setupRealizationDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func insertEvent(t *testing.T, conn *sql.DB, timestamp, event, rawJSON string) {
	t.Helper()
	uid := journal.ComputeUID(timestamp, event, rawJSON)
	_, err := conn.Exec(`INSERT INTO journal_events(event_uid, timestamp, event, raw_json, ingested_at)
		VALUES (?, ?, ?, ?, ?)`, uid, timestamp, event, rawJSON, timestamp)
	if err != nil {
		t.Fatalf("insert %s event: %v", event, err)
	}
}

func TestRealizationOrganicSoldWithBonus(t *testing.T) {
	conn := setupRealizationDB(t)

	// Scan Stratum Tectonicas (WasLogged: false -> 5x theoretical value: 19010800 base + 76043200 bonus = 95054000)
	insertEvent(t, conn, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":111,"Body":1}`)

	// Sold at Vista Genomics with full bonus
	insertEvent(t, conn, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":76043200}]}`)

	engine := stats.NewEngine(conn)
	res, err := engine.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if err != nil {
		t.Fatalf("ComputeRealization failed: %v", err)
	}

	if res.TotalRealized != 95054000 {
		t.Errorf("expected TotalRealized 95,054,000, got %d", res.TotalRealized)
	}
	if res.OrganicStolen != 0 {
		t.Errorf("expected OrganicStolen 0, got %d", res.OrganicStolen)
	}
	if res.OrganicLostDeath != 0 {
		t.Errorf("expected OrganicLostDeath 0, got %d", res.OrganicLostDeath)
	}
}

func TestRealizationOrganicStolen(t *testing.T) {
	conn := setupRealizationDB(t)

	// Scan Stratum Tectonicas expecting first-logged bonus (76043200 bonus)
	insertEvent(t, conn, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":111,"Body":1}`)

	// Sold at Vista Genomics but someone else turned it in first: Bonus = 0
	insertEvent(t, conn, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)

	engine := stats.NewEngine(conn)
	res, err := engine.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if err != nil {
		t.Fatalf("ComputeRealization failed: %v", err)
	}

	if res.TotalRealized != 19010800 {
		t.Errorf("expected TotalRealized 19,010,800, got %d", res.TotalRealized)
	}
	if res.OrganicStolen != 76043200 {
		t.Errorf("expected OrganicStolen 76,043,200, got %d", res.OrganicStolen)
	}
	if res.OrganicLostDeath != 0 {
		t.Errorf("expected OrganicLostDeath 0, got %d", res.OrganicLostDeath)
	}
}

func TestRealizationDeathLosses(t *testing.T) {
	conn := setupRealizationDB(t)

	// Organic scan on foot
	insertEvent(t, conn, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":111,"Body":1}`)

	// Player dies on foot
	insertEvent(t, conn, "2026-03-01T11:00:00Z", "Died",
		`{"timestamp":"2026-03-01T11:00:00Z","event":"Died","KillerShip":"suit_class2","KillerName":"Guard"}`)

	engine := stats.NewEngine(conn)
	res, err := engine.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T10:30:00Z"}, nil)
	if err != nil {
		t.Fatalf("ComputeRealization failed: %v", err)
	}

	if res.OrganicLostDeath != 95054000 {
		t.Errorf("expected OrganicLostDeath 95,054,000, got %d", res.OrganicLostDeath)
	}
	if res.TotalRealized != 0 {
		t.Errorf("expected TotalRealized 0, got %d", res.TotalRealized)
	}
}

func TestRealizationNpcCrewCut(t *testing.T) {
	conn := setupRealizationDB(t)

	// Scan
	insertEvent(t, conn, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":111,"Body":1}`)

	// Sale for 19,010,800 credits
	insertEvent(t, conn, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)

	// Concurrent NPC crew cut: 1,901,080 (10%)
	insertEvent(t, conn, "2026-03-01T12:00:00Z", "NpcCrewPaidWage",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"NpcCrewPaidWage","NpcCrewId":99,"NpcCrewName":"Piloteer","Amount":1901080}`)

	engine := stats.NewEngine(conn)
	res, err := engine.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if err != nil {
		t.Fatalf("ComputeRealization failed: %v", err)
	}

	if res.TotalRealized != 19010800 {
		t.Errorf("expected TotalRealized 19,010,800, got %d", res.TotalRealized)
	}
	if res.NpcCrewLoss != 1901080 {
		t.Errorf("expected NpcCrewLoss 1,901,080, got %d", res.NpcCrewLoss)
	}
}

func TestRealizationCartoLostDeathAndSold(t *testing.T) {
	conn := setupRealizationDB(t)

	// Scan in Sol A 1 (Earth-like or terraformable)
	insertEvent(t, conn, "2026-03-01T10:00:00Z", "Scan",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Sol","SystemAddress":1234,"BodyID":1,"BodyName":"Sol A 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)

	// Ship destroyed
	insertEvent(t, conn, "2026-03-01T11:00:00Z", "Died",
		`{"timestamp":"2026-03-01T11:00:00Z","event":"Died","KillerShip":"anaconda","KillerName":"Pirate"}`)

	engine := stats.NewEngine(conn)
	res, err := engine.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T10:30:00Z"}, nil)
	if err != nil {
		t.Fatalf("ComputeRealization failed: %v", err)
	}

	if res.CartoLostDeath <= 0 {
		t.Errorf("expected CartoLostDeath > 0, got %d", res.CartoLostDeath)
	}
	if res.TotalRealized != 0 {
		t.Errorf("expected TotalRealized 0, got %d", res.TotalRealized)
	}
}
