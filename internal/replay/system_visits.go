package replay

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

// SystemVisit represents a commander visit to a star system reconstructed from
// persisted journal events and crash-recovered jumps.
type SystemVisit struct {
	UID        string `json:"uid"`
	Timestamp  string `json:"timestamp"`
	Galactic   string `json:"galactic"`
	SystemName string `json:"systemName"`
	ProfileID  int64  `json:"profileId,omitempty"`
	Commander  string `json:"commander,omitempty"`
}

// QuerySystemVisits returns visits to exactSystemName across globally enabled
// commander profiles, including matching crash-recovered jumps from recovered.
func QuerySystemVisits(queryer db.Queryer, exactSystemName string, recovered []RecoveredJump) ([]SystemVisit, error) {
	return QuerySystemVisitsScoped(queryer, exactSystemName, recovered, ProfileScope{})
}

// QuerySystemVisitsScoped returns visits to exactSystemName matching scope,
// mirroring QuerySystemVisitCountsScoped semantics: when jump arrivals exist
// (FSDJump, CarrierJump, or recovered jumps), only those jump arrivals are
// returned newest-first; otherwise the single earliest Location or
// coordinate-bearing event is returned.
func QuerySystemVisitsScoped(queryer db.Queryer, exactSystemName string, recovered []RecoveredJump, scope ProfileScope) ([]SystemVisit, error) {
	trimmedSystemName := strings.TrimSpace(exactSystemName)
	if trimmedSystemName == "" || isNilQueryer(queryer) {
		return nil, nil
	}
	query := `SELECT event_uid, timestamp, event, COALESCE(system_name, ''), COALESCE(profile_id, 0), COALESCE(commander, '')
		FROM journal_events
		WHERE system_name = ? COLLATE NOCASE
		  AND (event IN ('FSDJump', 'Location', 'CarrierJump') OR star_x IS NOT NULL)
		  AND ` + scope.SQLPredicate("") + `
		  AND ` + nonMulticrewSQLFilter + `
		ORDER BY timestamp DESC, id DESC`
	databaseRows, queryError := queryer.Query(query, trimmedSystemName)
	if queryError != nil {
		return nil, fmt.Errorf("query system visits for %q: %w", trimmedSystemName, queryError)
	}
	defer databaseRows.Close()

	var jumpVisits []SystemVisit
	seenJumpUIDs := make(map[string]bool)
	var earliestFallback SystemVisit
	hasFallback := false
	for databaseRows.Next() {
		var visitRecord SystemVisit
		var eventName string
		if scanError := databaseRows.Scan(
			&visitRecord.UID, &visitRecord.Timestamp, &eventName,
			&visitRecord.SystemName, &visitRecord.ProfileID, &visitRecord.Commander,
		); scanError != nil {
			return nil, fmt.Errorf("scan system visit row: %w", scanError)
		}
		if eventName == "FSDJump" || eventName == "CarrierJump" {
			visitRecord.Galactic = reference.ToGalactic(visitRecord.Timestamp)
			jumpVisits = append(jumpVisits, visitRecord)
			if visitRecord.UID != "" {
				seenJumpUIDs[visitRecord.UID] = true
			}
			continue
		}
		// Rows arrive in timestamp DESC, id DESC order, so the final non-jump
		// row encountered is the earliest fallback sighting.
		earliestFallback, hasFallback = visitRecord, true
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		return nil, fmt.Errorf("iterate system visits for %q: %w", trimmedSystemName, iterationError)
	}

	targetSystemKey := NormalizeSystemKey(trimmedSystemName)
	explicitScope, scopeMatcher := !scope.IsAllEnabled(), scope.Matcher()
	for _, recoveredJump := range recovered {
		if NormalizeSystemKey(recoveredJump.SystemName) != targetSystemKey {
			continue
		}
		if explicitScope && !scopeMatcher.Matches(recoveredJump.ProfileID, recoveredJump.Commander) {
			continue
		}
		if recoveredJump.LocationEventUID != "" {
			if seenJumpUIDs[recoveredJump.LocationEventUID] {
				continue
			}
			seenJumpUIDs[recoveredJump.LocationEventUID] = true
		}
		jumpVisits = append(jumpVisits, SystemVisit{
			UID:        recoveredJump.LocationEventUID,
			Timestamp:  recoveredJump.Timestamp,
			Galactic:   reference.ToGalactic(recoveredJump.Timestamp),
			SystemName: recoveredJump.SystemName,
			ProfileID:  recoveredJump.ProfileID,
			Commander:  recoveredJump.Commander,
		})
	}

	if len(jumpVisits) > 0 {
		slices.SortFunc(jumpVisits, func(left, right SystemVisit) int {
			return cmp.Or(cmp.Compare(right.Timestamp, left.Timestamp), cmp.Compare(right.UID, left.UID))
		})
		return jumpVisits, nil
	}
	if hasFallback {
		earliestFallback.Galactic = reference.ToGalactic(earliestFallback.Timestamp)
		return []SystemVisit{earliestFallback}, nil
	}
	return nil, nil
}
