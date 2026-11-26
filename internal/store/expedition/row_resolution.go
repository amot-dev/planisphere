package expedition

import (
	"database/sql"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

// SetRowCoords stores resolved coordinates if the row still has systemName.
func SetRowCoords(queryer db.Queryer, rowID int64, systemName string, systemAddress, edsmID int64, coords *reference.Vec3, source string) error {
	var xCoord, yCoord, zCoord, addressValue, edsmIDValue any
	if coords != nil {
		xCoord, yCoord, zCoord = coords.X, coords.Y, coords.Z
	}
	if systemAddress != 0 {
		addressValue = systemAddress
	}
	if edsmID != 0 {
		edsmIDValue = edsmID
	}
	_, execError := queryer.Exec(`UPDATE expedition_rows SET system_address = ?, edsm_id = ?, x = ?, y = ?, z = ?, coords_source = ?
		WHERE id = ? AND system_name = ?`, addressValue, edsmIDValue, xCoord, yCoord, zCoord, source, rowID, systemName)
	return execError
}

// MissingPOIRow identifies a waypoint row whose poi_name is still blank.
type MissingPOIRow struct {
	ID            int64
	SystemName    string
	SystemAddress int64
}

// ListMissingPOIRows returns all named waypoint rows without a POI label.
func ListMissingPOIRows(queryer db.Queryer) ([]MissingPOIRow, error) {
	databaseRows, queryError := queryer.Query(
		`SELECT id, system_name, COALESCE(system_address, 0) FROM expedition_rows WHERE row_type = 'waypoint' AND (poi_name IS NULL OR TRIM(poi_name) = '') AND system_name != ''`,
	)
	if queryError != nil {
		return nil, queryError
	}
	defer databaseRows.Close()
	var missingRows []MissingPOIRow
	for databaseRows.Next() {
		var rowItem MissingPOIRow
		if scanError := databaseRows.Scan(&rowItem.ID, &rowItem.SystemName, &rowItem.SystemAddress); scanError != nil {
			return nil, scanError
		}
		missingRows = append(missingRows, rowItem)
	}
	return missingRows, databaseRows.Err()
}

// SetRowPOIIfBlank stores poiName on rowID only when system_name still matches and poi_name is empty.
func SetRowPOIIfBlank(queryer db.Queryer, rowID int64, systemName, poiName string) error {
	_, execError := queryer.Exec(
		`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND system_name = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`,
		poiName,
		rowID,
		systemName,
	)
	return execError
}

// ResolvedWaypointUpdate carries resolved coordinates and optional POI metadata for a waypoint row.
type ResolvedWaypointUpdate struct {
	RowID         int64
	SystemName    string
	SystemAddress int64
	EDSMID        int64
	Pos           *reference.Vec3
	Source        string
	POIName       string
}

// ApplyResolvedWaypointsTx persists a batch of locally resolved waypoint coordinates
// and optional blank-POI backfills inside a single transaction.
func ApplyResolvedWaypointsTx(dbConn *sql.DB, updates []ResolvedWaypointUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		statement, prepareError := transaction.Prepare(`UPDATE expedition_rows
			SET system_address = ?, edsm_id = ?, x = ?, y = ?, z = ?, coords_source = ?,
			    poi_name = CASE WHEN ? != '' AND (poi_name IS NULL OR TRIM(poi_name) = '') THEN ? ELSE poi_name END
			WHERE id = ? AND system_name = ?`)
		if prepareError != nil {
			return fmt.Errorf("prepare resolved waypoint update: %w", prepareError)
		}
		defer statement.Close()

		for _, update := range updates {
			var xCoord, yCoord, zCoord, addressValue, edsmIDValue any
			if update.Pos != nil {
				xCoord, yCoord, zCoord = update.Pos.X, update.Pos.Y, update.Pos.Z
			}
			if update.SystemAddress != 0 {
				addressValue = update.SystemAddress
			}
			if update.EDSMID != 0 {
				edsmIDValue = update.EDSMID
			}
			trimmedPOI := strings.TrimSpace(update.POIName)
			if _, execError := statement.Exec(
				addressValue, edsmIDValue, xCoord, yCoord, zCoord, update.Source,
				trimmedPOI, trimmedPOI, update.RowID, update.SystemName,
			); execError != nil {
				return fmt.Errorf("apply resolved waypoint %d: %w", update.RowID, execError)
			}
		}
		return nil
	})
}

// RowPOIUpdate pairs a waypoint row ID and expected system name with a resolved POI label to backfill when blank.
type RowPOIUpdate struct {
	RowID      int64
	SystemName string
	POIName    string
}

// SetMissingRowPOIsTx populates blank poi_name columns across updates inside a single transaction.
func SetMissingRowPOIsTx(dbConn *sql.DB, updates []RowPOIUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		statement, prepareError := transaction.Prepare(
			`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND system_name = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`,
		)
		if prepareError != nil {
			return fmt.Errorf("prepare missing poi update: %w", prepareError)
		}
		defer statement.Close()

		for _, update := range updates {
			trimmedPOI := strings.TrimSpace(update.POIName)
			if trimmedPOI == "" {
				continue
			}
			if _, execError := statement.Exec(trimmedPOI, update.RowID, update.SystemName); execError != nil {
				return fmt.Errorf("update poi name for row %d: %w", update.RowID, execError)
			}
		}
		return nil
	})
}
