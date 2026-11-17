package app

import (
	"database/sql"
	"fmt"
	"strings"

	"planisphere/internal/db"
)

// External link setting keys and visibility modes.
const (
	settingExternalLinkInara = "external_link_inara"
	settingExternalLinkEDSM  = "external_link_edsm"
	settingExternalLinkGEC   = "external_link_gec"

	ExternalLinkOn      = "on"
	ExternalLinkOff     = "off"
	ExternalLinkIfNoGEC = "if_no_gec"
)

func normalizeBinaryExternalLinkMode(rawMode string) string {
	if strings.EqualFold(strings.TrimSpace(rawMode), ExternalLinkOff) {
		return ExternalLinkOff
	}
	return ExternalLinkOn
}

func normalizeEDSMExternalLinkMode(rawMode string) string {
	switch strings.ToLower(strings.TrimSpace(rawMode)) {
	case ExternalLinkOn:
		return ExternalLinkOn
	case ExternalLinkOff:
		return ExternalLinkOff
	default:
		return ExternalLinkIfNoGEC
	}
}

// externalLinkModes returns the configured or default visibility modes for
// Inara, EDSM, and GEC waypoint links. Caller must hold app.dbMu.RLock.
func (app *App) externalLinkModes() (inaraMode, edsmMode, gecMode string) {
	if app.db == nil {
		return ExternalLinkOn, ExternalLinkIfNoGEC, ExternalLinkOn
	}
	inaraRaw, _ := db.GetSetting(app.db, settingExternalLinkInara)
	edsmRaw, _ := db.GetSetting(app.db, settingExternalLinkEDSM)
	gecRaw, _ := db.GetSetting(app.db, settingExternalLinkGEC)
	return normalizeBinaryExternalLinkMode(inaraRaw),
		normalizeEDSMExternalLinkMode(edsmRaw),
		normalizeBinaryExternalLinkMode(gecRaw)
}

// SetExternalLinks updates the visibility modes for waypoint external star
// system links (Inara: "on"/"off", EDSM: "on"/"if_no_gec"/"off", GEC: "on"/"off").
func (app *App) SetExternalLinks(inaraMode, edsmMode, gecMode string) (Settings, error) {
	normalizedInara := normalizeBinaryExternalLinkMode(inaraMode)
	normalizedEDSM := normalizeEDSMExternalLinkMode(edsmMode)
	normalizedGEC := normalizeBinaryExternalLinkMode(gecMode)

	if saveError := app.withReadyReadLock(func() error {
		return db.WithTx(app.db, func(transaction *sql.Tx) error {
			if err := db.SetSetting(transaction, settingExternalLinkInara, normalizedInara); err != nil {
				return err
			}
			if err := db.SetSetting(transaction, settingExternalLinkEDSM, normalizedEDSM); err != nil {
				return err
			}
			return db.SetSetting(transaction, settingExternalLinkGEC, normalizedGEC)
		})
	}); saveError != nil {
		return Settings{}, fmt.Errorf("persist external link settings: %w", saveError)
	}
	return app.GetSettings()
}
