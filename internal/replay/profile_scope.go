package replay

import (
	"slices"
	"strconv"
	"strings"

	"planisphere/internal/core/db"
)

// CmdrFilterSQL is a SQL predicate on journal_events that excludes events
// belonging to any commander profile (or legacy commander entry when profile_id
// is unresolved) that the user has disabled in Settings.
const CmdrFilterSQL = `((profile_id > 0 AND NOT EXISTS (SELECT 1 FROM commander_profiles cp WHERE cp.id = journal_events.profile_id AND cp.enabled = 0)) OR (COALESCE(profile_id, 0) = 0 AND (commander IS NULL OR commander = '' OR NOT EXISTS (SELECT 1 FROM commanders c WHERE c.name = journal_events.commander COLLATE NOCASE AND c.enabled = 0))))`

// HasDisabledCommanders reports whether any commander profile or legacy
// commander row is currently disabled in Settings.
func HasDisabledCommanders(queryer db.Queryer) (bool, error) {
	var hasDisabled int
	if err := queryer.QueryRow(`SELECT EXISTS(SELECT 1 FROM commander_profiles WHERE enabled = 0) OR EXISTS(SELECT 1 FROM commanders WHERE enabled = 0)`).Scan(&hasDisabled); err != nil {
		return false, err
	}
	return hasDisabled != 0, nil
}

// ProfileScope selects which commander save-slot profiles participate in a
// replay query or projection calculation. Its zero value selects all globally
// enabled profiles (falling back to legacy commanders when profile_id is 0).
type ProfileScope struct {
	Selected   bool
	ProfileIDs []int64
}

// NewProfileScope constructs an explicit Selected profile scope from positive
// profileIDs, deduplicated and sorted in ascending order.
func NewProfileScope(profileIDs []int64) ProfileScope {
	return ProfileScope{Selected: true, ProfileIDs: normalizePositiveProfileIDs(profileIDs)}
}

func normalizePositiveProfileIDs(profileIDs []int64) []int64 {
	cleaned := make([]int64, 0, len(profileIDs))
	for _, profileID := range profileIDs {
		if profileID > 0 {
			cleaned = append(cleaned, profileID)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	slices.Sort(cleaned)
	return slices.Compact(cleaned)
}

func (scope ProfileScope) normalizedIDs() []int64 {
	for index, profileID := range scope.ProfileIDs {
		if profileID <= 0 || (index > 0 && profileID <= scope.ProfileIDs[index-1]) {
			return normalizePositiveProfileIDs(scope.ProfileIDs)
		}
	}
	return scope.ProfileIDs
}

// IsAllEnabled reports whether scope represents all globally enabled profiles.
func (scope ProfileScope) IsAllEnabled() bool {
	return !scope.Selected && len(scope.normalizedIDs()) == 0
}

// ContainsProfileID reports whether an explicit scope includes profileID.
func (scope ProfileScope) ContainsProfileID(profileID int64) bool {
	if profileID <= 0 {
		return false
	}
	_, found := slices.BinarySearch(scope.normalizedIDs(), profileID)
	return found
}

// CacheKeySuffix returns "" for AllEnabled scopes or "|prof:..." for explicit
// profile selections, preserving WindowEndsBefore's second-pipe timestamp parse.
func (scope ProfileScope) CacheKeySuffix() string {
	if scope.IsAllEnabled() {
		return ""
	}
	ids := scope.normalizedIDs()
	if len(ids) == 0 {
		return "|prof:none"
	}
	return "|prof:" + formatInt64CSV(ids)
}

// SQLPredicate returns a standalone SQL boolean expression filtering
// journal_events (or tableAlias when non-empty) to events matching scope.
// Explicit profile IDs use unary +profile_id so timestamp and body indexes
// remain the primary scan access path without temporary B-tree sorts.
func (scope ProfileScope) SQLPredicate(tableAlias string) string {
	if scope.IsAllEnabled() {
		if tableAlias == "" {
			return CmdrFilterSQL
		}
		return commanderFilterForTableAlias(tableAlias)
	}
	columnExpr := "+profile_id"
	if tableAlias != "" {
		columnExpr = "+" + tableAlias + ".profile_id"
	}
	ids := scope.normalizedIDs()
	if len(ids) == 0 {
		return columnExpr + " IN (-1)"
	}
	return columnExpr + " IN (" + formatInt64CSV(ids) + ")"
}

func commanderFilterForTableAlias(alias string) string {
	return `((` + alias + `.profile_id > 0 AND NOT EXISTS (SELECT 1 FROM commander_profiles cp WHERE cp.id = ` + alias + `.profile_id AND cp.enabled = 0)) OR (COALESCE(` + alias + `.profile_id, 0) = 0 AND (` + alias + `.commander IS NULL OR ` + alias + `.commander = '' OR NOT EXISTS (SELECT 1 FROM commanders c WHERE c.name = ` + alias + `.commander COLLATE NOCASE AND c.enabled = 0))))`
}

func formatInt64CSV(values []int64) string {
	if len(values) == 1 {
		return strconv.FormatInt(values[0], 10)
	}
	var builder strings.Builder
	for index, value := range values {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatInt(value, 10))
	}
	return builder.String()
}

// SqliteNocaseKey folds ASCII uppercase bytes to lowercase to match SQLite's
// built-in COLLATE NOCASE behavior byte-for-byte.
func SqliteNocaseKey(value string) string { return sqliteNocaseKey(value) }

// EnabledProfileMatcher evaluates ProfileScope membership in memory against
// preloaded profile_id and legacy commander attributes.
type EnabledProfileMatcher struct {
	matchesAll, explicitSelection bool
	selectedProfileIDs            []int64
	disabledProfileIDs            map[int64]bool
	disabledCommanders            map[string]bool
}

// Matcher returns an in-memory EnabledProfileMatcher for an explicit scope
// without querying SQLite.
func (scope ProfileScope) Matcher() EnabledProfileMatcher {
	return EnabledProfileMatcher{explicitSelection: true, selectedProfileIDs: scope.normalizedIDs()}
}

// LoadProfileMatcher loads the in-memory actor filter for scope.
func LoadProfileMatcher(queryer db.Queryer, scope ProfileScope) (EnabledProfileMatcher, error) {
	if !scope.IsAllEnabled() {
		return scope.Matcher(), nil
	}
	if isNilQueryer(queryer) {
		return EnabledProfileMatcher{matchesAll: true}, nil
	}
	hasDisabled, err := HasDisabledCommanders(queryer)
	if err != nil || !hasDisabled {
		return EnabledProfileMatcher{matchesAll: err == nil}, err
	}
	disabledProfiles, profileErr := queryDisabledProfileIDs(queryer)
	if profileErr != nil {
		return EnabledProfileMatcher{}, profileErr
	}
	disabledCmdrs, cmdrErr := queryDisabledCommanderKeys(queryer)
	if cmdrErr != nil {
		return EnabledProfileMatcher{}, cmdrErr
	}
	return EnabledProfileMatcher{disabledProfileIDs: disabledProfiles, disabledCommanders: disabledCmdrs}, nil
}

// MatchesAll reports whether every actor matches without filtering.
func (matcher EnabledProfileMatcher) MatchesAll() bool { return matcher.matchesAll }

// Matches reports whether an event with profileID and commander belongs to scope.
func (matcher EnabledProfileMatcher) Matches(profileID int64, commander string) bool {
	if matcher.matchesAll {
		return true
	}
	if matcher.explicitSelection {
		_, found := slices.BinarySearch(matcher.selectedProfileIDs, profileID)
		return profileID > 0 && found
	}
	if profileID > 0 {
		return !matcher.disabledProfileIDs[profileID]
	}
	return commander == "" || !matcher.disabledCommanders[sqliteNocaseKey(commander)]
}

func queryDisabledProfileIDs(queryer db.Queryer) (map[int64]bool, error) {
	rows, err := queryer.Query(`SELECT id FROM commander_profiles WHERE enabled = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	disabled := make(map[int64]bool)
	for rows.Next() {
		var profileID int64
		if scanErr := rows.Scan(&profileID); scanErr != nil {
			return nil, scanErr
		}
		disabled[profileID] = true
	}
	return disabled, rows.Err()
}

func queryDisabledCommanderKeys(queryer db.Queryer) (map[string]bool, error) {
	rows, err := queryer.Query(`SELECT name FROM commanders WHERE enabled = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	disabled := make(map[string]bool)
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			return nil, scanErr
		}
		disabled[sqliteNocaseKey(name)] = true
	}
	return disabled, rows.Err()
}
