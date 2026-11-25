package projections

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

var (
	recoveredJumpsJournalEvents = journal.NewEventSet(journal.EventLocation)
	recoveredJumpsAppEvents     = events.NewAppSet(events.AppCommanderFilterChanged, events.AppDatabaseSwitched)
)

// RecoveredJump represents a hyperspace or Fleet Carrier passenger jump completed
// server-side during a client disconnect or offline session, reconstructed from
// the subsequent Location sequence.
type RecoveredJump = replay.RecoveredJump

type recoveredJumpsCachePayload struct {
	all            []RecoveredJump
	enabledMatcher replay.EnabledProfileMatcher
}

// RecoveredJumps caches crash-recovered hyperspace jumps and offline or
// disconnected Fleet Carrier passenger jumps across all profiles in the archive.
type RecoveredJumps struct {
	database *sql.DB
	cache    *projection.Cache[recoveredJumpsCachePayload]
}

// NewRecoveredJumps creates a RecoveredJumps projection and registers it on bus.
func NewRecoveredJumps(dbConn *sql.DB, bus *projection.Bus) *RecoveredJumps {
	triggers := projection.Triggers{Journal: recoveredJumpsJournalEvents, App: recoveredJumpsAppEvents}
	return &RecoveredJumps{
		database: dbConn,
		cache: projection.NewCache[recoveredJumpsCachePayload](bus, "RecoveredJumps", triggers, func() (recoveredJumpsCachePayload, error) {
			if dbConn == nil {
				return recoveredJumpsCachePayload{}, nil
			}
			matcher, err := replay.LoadProfileMatcher(dbConn, replay.ProfileScope{})
			if err != nil {
				return recoveredJumpsCachePayload{}, err
			}
			allJumps, err := replay.ComputeRecoveredJumpsAll(dbConn)
			if err != nil {
				return recoveredJumpsCachePayload{}, err
			}
			return recoveredJumpsCachePayload{all: allJumps, enabledMatcher: matcher}, nil
		}),
	}
}

// Triggers reports the journal and app events that invalidate RecoveredJumps.
func (jumps *RecoveredJumps) Triggers() projection.Triggers { return jumps.cache.Triggers() }

// All returns cached crash-recovered jumps across globally enabled profiles.
func (jumps *RecoveredJumps) All() ([]RecoveredJump, error) {
	return jumps.AllScoped(replay.ProfileScope{})
}

// AllScoped returns cached crash-recovered jumps filtered to scope.
func (jumps *RecoveredJumps) AllScoped(scope replay.ProfileScope) ([]RecoveredJump, error) {
	payload, err := jumps.cache.Get()
	if err != nil || len(payload.all) == 0 {
		return nil, err
	}
	return replay.FilterRecoveredJumpsByMatcher(payload.all, resolveCachedMatcher(payload.enabledMatcher, scope)), nil
}

// InRange returns cached crash-recovered jumps falling inside (afterTimestamp, untilTimestamp].
func (jumps *RecoveredJumps) InRange(afterTimestamp, untilTimestamp string) ([]RecoveredJump, error) {
	return jumps.InRangeScoped(afterTimestamp, untilTimestamp, replay.ProfileScope{})
}

// InRangeScoped returns cached crash-recovered jumps inside (afterTimestamp, untilTimestamp] matching scope.
func (jumps *RecoveredJumps) InRangeScoped(afterTimestamp, untilTimestamp string, scope replay.ProfileScope) ([]RecoveredJump, error) {
	payload, err := jumps.cache.Get()
	if err != nil || len(payload.all) == 0 {
		return nil, err
	}
	inWindow := replay.FilterRecoveredJumpsInRange(payload.all, afterTimestamp, untilTimestamp)
	if len(inWindow) == 0 {
		return nil, nil
	}
	return replay.FilterRecoveredJumpsByMatcher(inWindow, resolveCachedMatcher(payload.enabledMatcher, scope)), nil
}

func resolveCachedMatcher(cachedEnabled replay.EnabledProfileMatcher, scope replay.ProfileScope) replay.EnabledProfileMatcher {
	if scope.IsAllEnabled() {
		return cachedEnabled
	}
	return scope.Matcher()
}
