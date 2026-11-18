package edsm

import (
	"database/sql"
	"errors"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

func key(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// CanonicalSystemName resolves the canonical capitalization of name from
// journal_events or system_cache, returning trimmed name when unknown.
func CanonicalSystemName(queryer db.Queryer, name string) string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" || queryer == nil {
		return trimmedName
	}
	var canonicalName string
	if queryError := queryer.QueryRow(
		`SELECT system_name FROM journal_events WHERE system_name = ? COLLATE NOCASE LIMIT 1`,
		trimmedName,
	).Scan(&canonicalName); queryError == nil && canonicalName != "" {
		return canonicalName
	}
	if queryError := queryer.QueryRow(
		`SELECT system_name FROM system_cache WHERE name_key = ? LIMIT 1`,
		key(trimmedName),
	).Scan(&canonicalName); queryError == nil && canonicalName != "" {
		return canonicalName
	}
	return trimmedName
}

// FromJournal finds the system's coordinates in the local archive.
func FromJournal(queryer db.Queryer, name string) (*Info, error) {
	var addressVal sql.NullInt64
	var starX, starY, starZ float64
	var canonical string
	queryError := queryer.QueryRow(`SELECT system_name, system_address, star_x, star_y, star_z FROM journal_events
		WHERE system_name = ? COLLATE NOCASE AND star_x IS NOT NULL ORDER BY timestamp DESC LIMIT 1`, name).
		Scan(&canonical, &addressVal, &starX, &starY, &starZ)
	if errors.Is(queryError, sql.ErrNoRows) {
		return nil, nil
	}
	if queryError != nil {
		return nil, queryError
	}
	return &Info{Name: canonical, SystemAddress: addressVal.Int64, Pos: &reference.Vec3{X: starX, Y: starY, Z: starZ}, Source: "journal"}, nil
}

func (resolver *Resolver) fromCache(name string) (*Info, error) {
	var systemInfo Info
	var addressVal, edsmVal sql.NullInt64
	var xVal, yVal, zVal sql.NullFloat64
	queryError := resolver.DB.QueryRow(`SELECT system_name, system_address, edsm_id, x, y, z, source, fetched_at FROM system_cache WHERE name_key = ?`, key(name)).
		Scan(&systemInfo.Name, &addressVal, &edsmVal, &xVal, &yVal, &zVal, &systemInfo.Source, new(string))
	if errors.Is(queryError, sql.ErrNoRows) {
		return nil, nil
	}
	if queryError != nil {
		return nil, queryError
	}
	systemInfo.SystemAddress, systemInfo.EDSMID = addressVal.Int64, edsmVal.Int64
	if xVal.Valid {
		systemInfo.Pos = &reference.Vec3{X: xVal.Float64, Y: yVal.Float64, Z: zVal.Float64}
	}
	return &systemInfo, nil
}

func (resolver *Resolver) store(name string, systemInfo *Info) {
	var xCoord, yCoord, zCoord, nullableSystemAddress, nullableEDSMID any
	if systemInfo.Pos != nil {
		xCoord, yCoord, zCoord = systemInfo.Pos.X, systemInfo.Pos.Y, systemInfo.Pos.Z
	}
	if systemInfo.SystemAddress != 0 {
		nullableSystemAddress = systemInfo.SystemAddress
	}
	if systemInfo.EDSMID != 0 {
		nullableEDSMID = systemInfo.EDSMID
	}
	_, _ = resolver.DB.Exec(`INSERT INTO system_cache(name_key, system_name, system_address, edsm_id, x, y, z, source, fetched_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(name_key) DO UPDATE SET system_name=excluded.system_name,
		system_address=excluded.system_address, edsm_id=excluded.edsm_id, x=excluded.x, y=excluded.y, z=excluded.z,
		source=excluded.source, fetched_at=excluded.fetched_at`,
		key(name), systemInfo.Name, nullableSystemAddress, nullableEDSMID, xCoord, yCoord, zCoord, systemInfo.Source, reference.NowISO())
}

