package gec_test

import (
	"strings"
	"testing"

	"planisphere/internal/db"
	"planisphere/internal/gec"
)

func TestGECandGMPCombinedImport(t *testing.T) {
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatalf("open db: %v", openError)
	}
	defer dbConn.Close()

	catalogInstance := gec.NewCatalog(dbConn)

	// Sample JSON with 1 GEC entry and 1 GMP entry having the SAME ID 729
	rawJSON := `[
		{
			"id": 729,
			"source": "GEC",
			"type": "Tourist Destination",
			"name": "Breathing Water",
			"galMapSearch": "Bleae Thua ZY-Z d13-0",
			"poiUrl": "https://edastro.com/gec/view/729",
			"coordinates": [10.0, 20.0, 30.0],
			"descriptionMardown": "Long description markdown to be dropped",
			"descriptionHtml": "<p>Long description html to be dropped</p>"
		},
		{
			"id": 729,
			"source": "GMP",
			"type": "planetaryNebula",
			"name": "Cupid&#39;s Arrow",
			"galMapSearch": "S171 34",
			"galMapUrl": "https://www.edsm.net/en/system/id/332599/name/S171+34",
			"coordinates": [-2985.96875, 601.75, -1723.96875]
		}
	]`

	count, importError := catalogInstance.Import([]byte(rawJSON))
	if importError != nil {
		t.Fatalf("import failed: %v", importError)
	}
	if count != 2 {
		t.Fatalf("expected 2 imported entries, got %d", count)
	}
	if reloadError := catalogInstance.Reload(); reloadError != nil {
		t.Fatalf("reload failed: %v", reloadError)
	}

	// Verify description fields are excluded from cached raw_json
	var cachedRawJSON string
	if queryError := dbConn.QueryRow(`SELECT raw_json FROM gec_pois_cache WHERE id = 729`).Scan(&cachedRawJSON); queryError != nil {
		t.Fatalf("query cached raw_json: %v", queryError)
	}
	if strings.Contains(cachedRawJSON, "descriptionMardown") || strings.Contains(cachedRawJSON, "descriptionHtml") {
		t.Errorf("expected descriptions to be omitted from raw_json, got: %s", cachedRawJSON)
	}

	// Lookup GEC
	gecMatch := catalogInstance.Lookup("Bleae Thua ZY-Z d13-0", 0)
	if len(gecMatch) == 0 {
		t.Fatalf("expected GEC match for Bleae Thua ZY-Z d13-0")
	}
	if gecMatch[0].Name != "Breathing Water" || gecMatch[0].Source != "GEC" {
		t.Errorf("unexpected GEC entry: %+v", gecMatch[0])
	}

	// Lookup GMP (e.g. S171 34 -> Cupid's Arrow)
	gmpMatch := catalogInstance.Lookup("S171 34", 0)
	if len(gmpMatch) == 0 {
		t.Fatalf("expected GMP match for S171 34")
	}
	if gmpMatch[0].Name != "Cupid's Arrow" || gmpMatch[0].Source != "GMP" {
		t.Errorf("unexpected GMP entry: %+v", gmpMatch[0])
	}
}
