package replay

import (
	"strings"

	"planisphere/internal/core/db"
)

// NormalizeSystemKey canonicalizes a star system name for case-insensitive
// and whitespace-normalized cache and map lookups.
func NormalizeSystemKey(systemName string) string {
	return strings.ToLower(strings.Join(strings.Fields(systemName), " "))
}

// QuerySystemVisitCounts queries commander visit counts for missingNormalizedNames
// against queryer, adding any matching crash-recovered jumps from recovered.
func QuerySystemVisitCounts(
	queryer db.Queryer,
	missingNormalizedNames []string,
	recovered []RecoveredJump,
) (map[string]int, error) {
	if len(missingNormalizedNames) == 0 {
		return map[string]int{}, nil
	}
	placeholders := make([]string, len(missingNormalizedNames))
	queryArgs := make([]any, len(missingNormalizedNames))
	for index, name := range missingNormalizedNames {
		placeholders[index] = "?"
		queryArgs[index] = name
	}
	query := `SELECT system_name, COUNT(*), COALESCE(SUM(CASE WHEN event IN ('FSDJump', 'CarrierJump') THEN 1 ELSE 0 END), 0)
		FROM journal_events
		WHERE system_name COLLATE NOCASE IN (` + strings.Join(placeholders, ",") + `)
		  AND (event IN ('FSDJump', 'Location', 'CarrierJump') OR star_x IS NOT NULL)
		  AND ` + CmdrFilterSQL + `
		GROUP BY system_name COLLATE NOCASE`
	databaseRows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return nil, queryError
	}
	defer databaseRows.Close()

	totalBySystem := make(map[string]int, len(missingNormalizedNames))
	jumpsBySystem := make(map[string]int, len(missingNormalizedNames))
	for databaseRows.Next() {
		var matchedSystem string
		var totalEvents, jumpEvents int
		if scanError := databaseRows.Scan(&matchedSystem, &totalEvents, &jumpEvents); scanError != nil {
			return nil, scanError
		}
		normalizedName := NormalizeSystemKey(matchedSystem)
		totalBySystem[normalizedName] += totalEvents
		jumpsBySystem[normalizedName] += jumpEvents
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		return nil, iterationError
	}

	for _, recoveredJump := range recovered {
		normalizedSystem := NormalizeSystemKey(recoveredJump.SystemName)
		if totalBySystem[normalizedSystem] > 0 {
			jumpsBySystem[normalizedSystem]++
		}
	}

	resolvedCounts := make(map[string]int, len(missingNormalizedNames))
	for _, normalizedName := range missingNormalizedNames {
		if totalBySystem[normalizedName] > 0 {
			visitCount := jumpsBySystem[normalizedName]
			if visitCount == 0 {
				visitCount = 1
			}
			resolvedCounts[normalizedName] = visitCount
		} else {
			resolvedCounts[normalizedName] = 0
		}
	}
	return resolvedCounts, nil
}
