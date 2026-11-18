package settings_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/store/settings"
)

func openSettingsTestDB(testingContext *testing.T) *sql.DB {
	testingContext.Helper()
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatal(openError)
	}
	testingContext.Cleanup(func() {
		_ = dbConn.Close()
	})
	return dbConn
}

func TestSettingsRoundTripsAndNormalization(testingContext *testing.T) {
	dbConn := openSettingsTestDB(testingContext)

	// Generic CRUD
	if initialValue, getError := settings.Get(dbConn, "custom_key"); getError != nil || initialValue != "" {
		testingContext.Fatalf("expected empty initial custom_key, got %q err=%v", initialValue, getError)
	}
	if setError := settings.Set(dbConn, "custom_key", "custom_value"); setError != nil {
		testingContext.Fatalf("Set custom_key: %v", setError)
	}
	if loadedValue, getError := settings.Get(dbConn, "custom_key"); getError != nil || loadedValue != "custom_value" {
		testingContext.Fatalf("expected custom_value, got %q err=%v", loadedValue, getError)
	}
	if deleteError := settings.Delete(dbConn, "custom_key"); deleteError != nil {
		testingContext.Fatalf("Delete custom_key: %v", deleteError)
	}
	if deletedValue, _ := settings.Get(dbConn, "custom_key"); deletedValue != "" {
		testingContext.Fatalf("expected empty after delete, got %q", deletedValue)
	}

	// External links defaults and normalization
	inara, edsm, gec := settings.ExternalLinkModes(dbConn)
	if inara != settings.ExternalLinkOn || edsm != settings.ExternalLinkIfNoGEC || gec != settings.ExternalLinkOn {
		testingContext.Fatalf("unexpected defaults: inara=%q edsm=%q gec=%q", inara, edsm, gec)
	}
	if saveError := settings.SetExternalLinkModes(dbConn, "invalid", "unknown", "OFF"); saveError != nil {
		testingContext.Fatalf("SetExternalLinkModes: %v", saveError)
	}
	inara, edsm, gec = settings.ExternalLinkModes(dbConn)
	if inara != settings.ExternalLinkOn || edsm != settings.ExternalLinkIfNoGEC || gec != settings.ExternalLinkOff {
		testingContext.Fatalf("unexpected normalized values: inara=%q edsm=%q gec=%q", inara, edsm, gec)
	}

	// Debounce clamping
	if defaultDebounce := settings.JournalDebounceMs(dbConn); defaultDebounce != int(reference.DefaultDebounceDelay.Milliseconds()) {
		testingContext.Fatalf("expected default debounce %d, got %d", reference.DefaultDebounceDelay.Milliseconds(), defaultDebounce)
	}
	if saveError := settings.SetJournalDebounceMs(dbConn, 1); saveError != nil {
		testingContext.Fatalf("SetJournalDebounceMs: %v", saveError)
	}
	if clampedMin := settings.JournalDebounceMs(dbConn); clampedMin != int(reference.MinDebounceDelay.Milliseconds()) {
		testingContext.Fatalf("expected clamped min %d, got %d", reference.MinDebounceDelay.Milliseconds(), clampedMin)
	}

	// Tritium price default and floor
	if defaultPrice := settings.TritiumPricePerTonne(dbConn); defaultPrice != reference.DefaultTritiumPricePerTonne {
		testingContext.Fatalf("expected default tritium price, got %d", defaultPrice)
	}
	if saveError := settings.SetTritiumPricePerTonne(dbConn, -50); saveError != nil {
		testingContext.Fatalf("SetTritiumPricePerTonne: %v", saveError)
	}
	if floorPrice := settings.TritiumPricePerTonne(dbConn); floorPrice != 0 {
		testingContext.Fatalf("expected clamped 0 tritium price, got %d", floorPrice)
	}

	// Commander toggle
	if _, execError := dbConn.Exec(`INSERT INTO commanders(name, fid, enabled) VALUES ('CmdrAlpha', 'F1', 1)`); execError != nil {
		testingContext.Fatalf("insert commander: %v", execError)
	}
	if toggleError := settings.SetCommanderEnabled(dbConn, "cmdralpha", false); toggleError != nil {
		testingContext.Fatalf("SetCommanderEnabled: %v", toggleError)
	}
	var enabledFlag int
	if scanError := dbConn.QueryRow(`SELECT enabled FROM commanders WHERE name = 'CmdrAlpha'`).Scan(&enabledFlag); scanError != nil || enabledFlag != 0 {
		testingContext.Fatalf("expected enabled=0, got %d err=%v", enabledFlag, scanError)
	}
	if missingError := settings.SetCommanderEnabled(dbConn, "CmdrUnknown", false); missingError == nil {
		testingContext.Fatal("expected error for unknown commander")
	}
}

