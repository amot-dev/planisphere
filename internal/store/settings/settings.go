// Package settings manages persisted Planisphere user configuration and commander toggle state.
package settings

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

const (
	keyJournalDir           = "journal_dir"
	keyJournalDebounceMs    = "journal_debounce_ms"
	keyOnlineLookups        = "online_lookups"
	keyActiveExpedition     = "active_expedition_id"
	keyLastScanAt           = "last_scan_at"
	keySetupCompleted       = "setup_completed"
	keyTritiumPricePerTonne = "tritium_price_per_tonne"
	keyGECFetchedAt         = "gec_fetched_at"
	keyExternalLinkInara    = "external_link_inara"
	keyExternalLinkEDSM     = "external_link_edsm"
	keyExternalLinkGEC      = "external_link_gec"
	keyEDDHistoryRecovered  = "edd_history_recovered_v1"
	keyEDDDisconnectsDone   = "edd_history_disconnects_v7"
	keyEDDDatabasePath      = "edd_db_path"
)

// External link visibility modes.
const (
	ExternalLinkOn      = "on"
	ExternalLinkOff     = "off"
	ExternalLinkIfNoGEC = "if_no_gec"
)

// Get returns the persisted setting value for key, or "" when absent.
func Get(queryer db.Queryer, key string) (string, error) {
	var settingValue string
	queryError := queryer.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&settingValue)
	if errors.Is(queryError, sql.ErrNoRows) {
		return "", nil
	}
	return settingValue, queryError
}

// Set persists key with value, updating any existing entry.
func Set(queryer db.Queryer, key, value string) error {
	_, execError := queryer.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return execError
}

// Delete removes key from persisted settings.
func Delete(queryer db.Queryer, key string) error {
	_, execError := queryer.Exec(`DELETE FROM settings WHERE key = ?`, key)
	return execError
}

// GetInt64 reads an integer setting for key, returning defaultValue when missing or invalid.
func GetInt64(queryer db.Queryer, key string, defaultValue int64) int64 {
	if queryer == nil {
		return defaultValue
	}
	settingValue, getError := Get(queryer, key)
	trimmedValue := strings.TrimSpace(settingValue)
	if getError != nil || trimmedValue == "" {
		return defaultValue
	}
	parsedValue, parseError := strconv.ParseInt(trimmedValue, 10, 64)
	if parseError != nil {
		return defaultValue
	}
	return parsedValue
}

// NormalizeBinaryExternalLinkMode coerces Inara or GEC link visibility modes to on or off.
func NormalizeBinaryExternalLinkMode(rawMode string) string {
	if strings.EqualFold(strings.TrimSpace(rawMode), ExternalLinkOff) {
		return ExternalLinkOff
	}
	return ExternalLinkOn
}

// NormalizeEDSMExternalLinkMode coerces EDSM link visibility modes to on, off, or if_no_gec.
func NormalizeEDSMExternalLinkMode(rawMode string) string {
	switch strings.ToLower(strings.TrimSpace(rawMode)) {
	case ExternalLinkOn:
		return ExternalLinkOn
	case ExternalLinkOff:
		return ExternalLinkOff
	default:
		return ExternalLinkIfNoGEC
	}
}

// ExternalLinkModes returns the configured or default visibility modes for Inara, EDSM, and GEC links.
func ExternalLinkModes(queryer db.Queryer) (inaraMode, edsmMode, gecMode string) {
	if queryer == nil {
		return ExternalLinkOn, ExternalLinkIfNoGEC, ExternalLinkOn
	}
	inaraRaw, _ := Get(queryer, keyExternalLinkInara)
	edsmRaw, _ := Get(queryer, keyExternalLinkEDSM)
	gecRaw, _ := Get(queryer, keyExternalLinkGEC)
	return NormalizeBinaryExternalLinkMode(inaraRaw),
		NormalizeEDSMExternalLinkMode(edsmRaw),
		NormalizeBinaryExternalLinkMode(gecRaw)
}

// SetExternalLinkModes atomically validates and persists waypoint external link visibility modes.
func SetExternalLinkModes(dbConn *sql.DB, inaraMode, edsmMode, gecMode string) error {
	normalizedInara := NormalizeBinaryExternalLinkMode(inaraMode)
	normalizedEDSM := NormalizeEDSMExternalLinkMode(edsmMode)
	normalizedGEC := NormalizeBinaryExternalLinkMode(gecMode)

	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		if setError := Set(transaction, keyExternalLinkInara, normalizedInara); setError != nil {
			return setError
		}
		if setError := Set(transaction, keyExternalLinkEDSM, normalizedEDSM); setError != nil {
			return setError
		}
		return Set(transaction, keyExternalLinkGEC, normalizedGEC)
	})
}

// JournalDir returns the configured journal directory, or defaultDir when unset.
func JournalDir(queryer db.Queryer, defaultDir string) string {
	if queryer == nil {
		return defaultDir
	}
	settingValue, _ := Get(queryer, keyJournalDir)
	if strings.TrimSpace(settingValue) == "" {
		return defaultDir
	}
	return settingValue
}

// SetJournalDir stores the configured journal directory path.
func SetJournalDir(queryer db.Queryer, dir string) error {
	return Set(queryer, keyJournalDir, dir)
}

// JournalDebounceMs returns the clamped live journal watcher debounce delay in milliseconds.
func JournalDebounceMs(queryer db.Queryer) int {
	defaultDebounceMs := int(reference.DefaultDebounceDelay / time.Millisecond)
	configuredMs := GetInt64(queryer, keyJournalDebounceMs, int64(defaultDebounceMs))
	clampedDuration := reference.ClampDebounceDelay(time.Duration(configuredMs) * time.Millisecond)
	return int(clampedDuration / time.Millisecond)
}

// SetJournalDebounceMs clamps and stores the live journal watcher debounce delay in milliseconds.
func SetJournalDebounceMs(queryer db.Queryer, milliseconds int) error {
	clampedDuration := reference.ClampDebounceDelay(time.Duration(milliseconds) * time.Millisecond)
	clampedMs := strconv.Itoa(int(clampedDuration / time.Millisecond))
	return Set(queryer, keyJournalDebounceMs, clampedMs)
}

// OnlineLookups reports whether online EDSM/Spansh/GEC lookups are enabled.
func OnlineLookups(queryer db.Queryer) bool {
	if queryer == nil {
		return false
	}
	settingValue, _ := Get(queryer, keyOnlineLookups)
	return settingValue != "0"
}

// SetOnlineLookups stores whether online lookups are enabled.
func SetOnlineLookups(queryer db.Queryer, enabled bool) error {
	settingValue := "0"
	if enabled {
		settingValue = "1"
	}
	return Set(queryer, keyOnlineLookups, settingValue)
}

// ActiveExpeditionID returns the currently selected expedition ID, or 0 if unset.
func ActiveExpeditionID(queryer db.Queryer) int64 {
	return GetInt64(queryer, keyActiveExpedition, 0)
}

// SetActiveExpeditionID persists the currently active expedition ID, or clears it when <= 0.
func SetActiveExpeditionID(queryer db.Queryer, expeditionID int64) error {
	if expeditionID <= 0 {
		return Set(queryer, keyActiveExpedition, "")
	}
	return Set(queryer, keyActiveExpedition, strconv.FormatInt(expeditionID, 10))
}

// SetupCompleted reports whether onboarding setup has completed.
func SetupCompleted(queryer db.Queryer) bool {
	if queryer == nil {
		return false
	}
	settingValue, _ := Get(queryer, keySetupCompleted)
	return settingValue == "1"
}

// SetSetupCompleted stores whether initial onboarding setup is complete.
func SetSetupCompleted(queryer db.Queryer, completed bool) error {
	settingValue := "0"
	if completed {
		settingValue = "1"
	}
	return Set(queryer, keySetupCompleted, settingValue)
}

// TritiumPricePerTonne returns the configured Tritium price per tonne, or DefaultTritiumPricePerTonne.
func TritiumPricePerTonne(queryer db.Queryer) int64 {
	configuredPrice := GetInt64(queryer, keyTritiumPricePerTonne, reference.DefaultTritiumPricePerTonne)
	if configuredPrice < 0 {
		return reference.DefaultTritiumPricePerTonne
	}
	return configuredPrice
}

// SetTritiumPricePerTonne stores the Tritium price per tonne (clamped at 0 minimum).
func SetTritiumPricePerTonne(queryer db.Queryer, pricePerTonne int64) error {
	if pricePerTonne < 0 {
		pricePerTonne = 0
	}
	return Set(queryer, keyTritiumPricePerTonne, strconv.FormatInt(pricePerTonne, 10))
}

// LastScanAt returns the ISO timestamp of the most recent journal directory scan.
func LastScanAt(queryer db.Queryer) string {
	if queryer == nil {
		return ""
	}
	settingValue, _ := Get(queryer, keyLastScanAt)
	return settingValue
}

// SetLastScanAt stores the ISO timestamp of the latest journal directory scan.
func SetLastScanAt(queryer db.Queryer, isoTimestamp string) error {
	return Set(queryer, keyLastScanAt, isoTimestamp)
}

// GECFetchedAt returns the ISO timestamp when the GEC/GMP catalog was last refreshed.
func GECFetchedAt(queryer db.Queryer) string {
	if queryer == nil {
		return ""
	}
	settingValue, _ := Get(queryer, keyGECFetchedAt)
	return settingValue
}

// SetGECFetchedAt stores the ISO timestamp of the latest GEC/GMP catalog refresh.
func SetGECFetchedAt(queryer db.Queryer, isoTimestamp string) error {
	return Set(queryer, keyGECFetchedAt, isoTimestamp)
}

// EDDRecoveryState returns EDDiscovery history recovery completion flags and saved database path.
func EDDRecoveryState(queryer db.Queryer) (recovered bool, disconnectsDone bool, eddPath string) {
	if queryer == nil {
		return false, false, ""
	}
	recoveredValue, _ := Get(queryer, keyEDDHistoryRecovered)
	disconnectsValue, _ := Get(queryer, keyEDDDisconnectsDone)
	eddPath, _ = Get(queryer, keyEDDDatabasePath)
	return recoveredValue == "1", disconnectsValue == "1", eddPath
}

// MarkEDDRecovered records completion of full EDDiscovery history recovery and its source path.
func MarkEDDRecovered(queryer db.Queryer, eddPath string) error {
	if setError := Set(queryer, keyEDDHistoryRecovered, "1"); setError != nil {
		return setError
	}
	if setError := Set(queryer, keyEDDDatabasePath, eddPath); setError != nil {
		return setError
	}
	return Set(queryer, keyEDDDisconnectsDone, "1")
}

// MarkEDDDisconnectsRecovered records completion of EDDiscovery disconnect-recovery pass.
func MarkEDDDisconnectsRecovered(queryer db.Queryer) error {
	return Set(queryer, keyEDDDisconnectsDone, "1")
}

// SetCommanderEnabled enables or disables a discovered commander in statistics and plans.
func SetCommanderEnabled(queryer db.Queryer, name string, enabled bool) error {
	enabledValue := 0
	if enabled {
		enabledValue = 1
	}
	result, execError := queryer.Exec(`UPDATE commanders SET enabled = ? WHERE name = ? COLLATE NOCASE`, enabledValue, name)
	if execError != nil {
		return fmt.Errorf("update commander %q enabled state: %w", name, execError)
	}
	rowsAffected, rowsError := result.RowsAffected()
	if rowsError != nil {
		return fmt.Errorf("check commander update rows affected: %w", rowsError)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("commander %q not found", name)
	}
	return nil
}
