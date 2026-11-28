package projections

import (
	"database/sql"
	"time"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

var (
	playSessionsJournalEvents = journal.AllEvents
	playSessionsAppEvents     = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// PlaySession is one reconstructed contiguous play session for a commander profile.
type PlaySession = replay.PlaySession

type careerPlayDuration struct {
	duration time.Duration
	found    bool
}

// PlaySessions caches reconstructed play sessions across bounded expedition
// windows and career playtime totals across scoped profiles.
type PlaySessions struct {
	database *sql.DB
	windows  *projection.KeyedCache[string, []PlaySession]
	career   *projection.KeyedCache[string, careerPlayDuration]
}

// NewPlaySessions creates a PlaySessions projection and registers it on bus.
func NewPlaySessions(dbConn *sql.DB, bus *projection.Bus) *PlaySessions {
	triggers := projection.Triggers{
		Journal: playSessionsJournalEvents,
		App:     playSessionsAppEvents,
	}
	windows := projection.NewKeyedCache[string, []PlaySession](bus, "PlaySessions", triggers)
	windows.SetInvalidationFilter(func(observed projection.Triggers, windowKey string) bool {
		if !observed.App.IsEmpty() {
			return true
		}
		return !projection.WindowEndsBefore(windowKey, observed.EarliestTimestamp)
	})
	career := projection.NewKeyedCache[string, careerPlayDuration](bus, "PlaySessionsCareer", triggers)
	return &PlaySessions{
		database: dbConn,
		windows:  windows,
		career:   career,
	}
}

// Triggers reports the journal and app events that invalidate PlaySessions.
func (sessions *PlaySessions) Triggers() projection.Triggers {
	return sessions.windows.Triggers()
}

// Sessions returns cached reconstructed play sessions overlapping
// [startTimestamp, endTimestamp] across globally enabled profiles.
func (sessions *PlaySessions) Sessions(startTimestamp, endTimestamp string) ([]PlaySession, error) {
	return sessions.SessionsScoped(startTimestamp, endTimestamp, replay.ProfileScope{})
}

// SessionsScoped returns cached reconstructed play sessions overlapping
// [startTimestamp, endTimestamp] for scope.
func (sessions *PlaySessions) SessionsScoped(startTimestamp, endTimestamp string, scope replay.ProfileScope) ([]PlaySession, error) {
	if sessions.database == nil {
		return nil, nil
	}
	windowKey := startTimestamp + "|" + endTimestamp + scope.CacheKeySuffix()
	return sessions.windows.GetOrLoad(windowKey, func() ([]PlaySession, error) {
		return replay.QueryPlaySessionsScoped(sessions.database, startTimestamp, endTimestamp, scope)
	})
}

// CareerDurationScoped returns the cached cumulative career play duration for scope.
func (sessions *PlaySessions) CareerDurationScoped(scope replay.ProfileScope) (time.Duration, bool, error) {
	if sessions.database == nil {
		return 0, false, nil
	}
	cacheKey := "career" + scope.CacheKeySuffix()
	cached, loadError := sessions.career.GetOrLoad(cacheKey, func() (careerPlayDuration, error) {
		duration, found, queryError := replay.QueryCareerPlayDurationScoped(sessions.database, scope)
		if queryError != nil {
			return careerPlayDuration{}, queryError
		}
		return careerPlayDuration{duration: duration, found: found}, nil
	})
	if loadError != nil {
		return 0, false, loadError
	}
	return cached.duration, cached.found, nil
}
