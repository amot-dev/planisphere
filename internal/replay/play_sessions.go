package replay

import (
	"cmp"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

// PlaySession represents one reconstructed contiguous in-game play session for a commander profile.
type PlaySession struct {
	ProfileID int64
	Start     time.Time
	End       time.Time
}

type playProfileKey struct {
	profileID    int64
	commanderKey string
}

type sessionMarkerRow struct {
	profileID     int64
	timePlayed    int64
	timestamp     string
	event         string
	commander     string
	sourceFile    string
	parsedTime    time.Time
	hasTimePlayed bool
}

type journalFileMeta struct {
	firstTimestamp       string
	minGameplayTimestamp string
	prefix               string
	part                 int
	continuedPart        int
	observedEnd          time.Time
}

type draftPlaySession struct {
	profileKey     playProfileKey
	profileID      int64
	timePlayed     int64
	commander      string
	startTimestamp string
	sourceFile     string
	prefix         string
	start          time.Time
	end            time.Time
	closed         bool
	hasTimePlayed  bool
	sharedFile     bool
	lastPart       int
	continuedPart  int
}

type careerStatAnchor struct {
	timestamp string
	seconds   int64
}

func makePlayProfileKey(profileID int64, commander string) playProfileKey {
	if profileID > 0 {
		return playProfileKey{profileID: profileID}
	}
	return playProfileKey{commanderKey: sqliteNocaseKey(strings.TrimSpace(commander))}
}

func isDistinctJournalFile(fileName string) bool {
	return fileName != "" && !strings.EqualFold(fileName, "EDDiscovery")
}

func laterTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func parseOptionalTime(rawTimestamp string) (time.Time, bool) {
	if rawTimestamp == "" {
		return time.Time{}, false
	}
	parsedTime, parseError := reference.ParseISO(rawTimestamp)
	return parsedTime, parseError == nil
}

func parseTimePlayed(rawJSON string) (int64, bool) {
	var payload struct {
		Exploration struct {
			TimePlayed *float64 `json:"Time_Played"`
		} `json:"Exploration"`
	}
	if json.Unmarshal([]byte(rawJSON), &payload) != nil ||
		payload.Exploration.TimePlayed == nil ||
		*payload.Exploration.TimePlayed < 0 {
		return 0, false
	}
	return int64(math.Round(*payload.Exploration.TimePlayed)), true
}

func isRolloverPart(currentPrefix string, currentPart, currentContinuedPart int, candidatePrefix string, candidatePart int) bool {
	if candidatePart <= 1 {
		return false
	}
	if currentPrefix != "" && candidatePrefix == currentPrefix && candidatePart > currentPart {
		return true
	}
	return currentContinuedPart > 0 && candidatePart == currentContinuedPart
}

// QueryPlaySessionsScoped reconstructs contiguous play sessions per commander
// profile overlapping [startTimestamp, endTimestamp] for scope.
func QueryPlaySessionsScoped(queryer db.Queryer, startTimestamp, endTimestamp string, scope ProfileScope) ([]PlaySession, error) {
	sessionsByProfile, queryError := queryPlaySessionsByProfile(queryer, startTimestamp, endTimestamp, scope)
	if queryError != nil {
		return nil, queryError
	}
	var resolved []PlaySession
	for _, profileSessions := range sessionsByProfile {
		resolved = append(resolved, profileSessions...)
	}
	slices.SortFunc(resolved, func(left, right PlaySession) int {
		return cmp.Or(
			left.Start.Compare(right.Start),
			left.End.Compare(right.End),
			cmp.Compare(left.ProfileID, right.ProfileID),
		)
	})
	return resolved, nil
}

func playSessionScopeClause(queryer db.Queryer, scope ProfileScope, tableAlias string) string {
	if !scope.IsAllEnabled() {
		return " AND " + scope.SQLPredicate(tableAlias)
	}
	hasDisabled, checkError := HasDisabledCommanders(queryer)
	if checkError != nil || hasDisabled {
		return " AND " + scope.SQLPredicate(tableAlias)
	}
	return ""
}

func queryPlaySessionsByProfile(
	queryer db.Queryer,
	startTimestamp, endTimestamp string,
	scope ProfileScope,
) (map[playProfileKey][]PlaySession, error) {
	scopeClause := playSessionScopeClause(queryer, scope, "")
	markers, queryStart, boundsEnd, markerError := loadSessionMarkers(queryer, startTimestamp, endTimestamp, scope, scopeClause)
	if markerError != nil {
		return nil, markerError
	}
	fileMetaByName, truncatedCandidates, boundsError := loadFileBoundsAndTruncated(queryer, queryStart, boundsEnd, scopeClause)
	if boundsError != nil {
		return nil, boundsError
	}

	markersByProfile := make(map[playProfileKey][]sessionMarkerRow)
	for _, marker := range markers {
		profileKey := makePlayProfileKey(marker.profileID, marker.commander)
		markersByProfile[profileKey] = append(markersByProfile[profileKey], marker)
	}

	draftsByProfile := make(map[playProfileKey][]*draftPlaySession, len(markersByProfile))
	coveredFiles := make(map[string]bool)
	for profileKey, profileMarkers := range markersByProfile {
		profileDrafts, buildError := buildProfileDraftSessions(queryer, profileKey, profileMarkers, fileMetaByName, coveredFiles)
		if buildError != nil {
			return nil, buildError
		}
		draftsByProfile[profileKey] = profileDrafts
	}
	for _, candidate := range truncatedCandidates {
		if !coveredFiles[candidate.sourceFile] {
			draftsByProfile[candidate.profileKey] = append(draftsByProfile[candidate.profileKey], candidate)
		}
	}

	for _, drafts := range draftsByProfile {
		slices.SortFunc(drafts, func(left, right *draftPlaySession) int {
			return left.start.Compare(right.start)
		})
	}
	if intervalError := populateIntervalObservedEnds(queryer, draftsByProfile, scopeClause); intervalError != nil {
		return nil, intervalError
	}

	windowStart, hasStart := parseOptionalTime(startTimestamp)
	windowEnd, hasEnd := parseOptionalTime(endTimestamp)
	resolvedByProfile := make(map[playProfileKey][]PlaySession, len(draftsByProfile))
	for profileKey, drafts := range draftsByProfile {
		resolveProfileSessionEnds(drafts)
		for _, draft := range drafts {
			endTime := laterTime(draft.start, draft.end)
			if hasStart && endTime.Before(windowStart) {
				continue
			}
			if hasEnd && draft.start.After(windowEnd) {
				continue
			}
			resolvedByProfile[profileKey] = append(resolvedByProfile[profileKey], PlaySession{
				ProfileID: draft.profileID,
				Start:     draft.start,
				End:       endTime,
			})
		}
	}
	return resolvedByProfile, nil
}

// SumPlayDuration clips sessions against [fromTimestamp, toTimestamp] (empty
// bounds are unbounded) and returns the summed play duration and overlap flag.
func SumPlayDuration(sessions []PlaySession, fromTimestamp, toTimestamp string) (time.Duration, bool) {
	fromTime, hasFrom := parseOptionalTime(fromTimestamp)
	toTime, hasTo := parseOptionalTime(toTimestamp)
	if len(sessions) == 0 ||
		(fromTimestamp != "" && !hasFrom) ||
		(toTimestamp != "" && !hasTo) ||
		(hasFrom && hasTo && toTime.Before(fromTime)) {
		return 0, false
	}
	var totalDuration time.Duration
	hasOverlap := false
	isPointInterval := hasFrom && hasTo && fromTime.Equal(toTime)
	for _, session := range sessions {
		clipStart := laterTime(session.Start, fromTime)
		clipEnd := session.End
		if hasTo && clipEnd.After(toTime) {
			clipEnd = toTime
		}
		if clipEnd.After(clipStart) {
			totalDuration += clipEnd.Sub(clipStart)
			hasOverlap = true
		} else if isPointInterval && !session.Start.After(fromTime) && !session.End.Before(toTime) {
			hasOverlap = true
		}
	}
	return totalDuration, hasOverlap
}

// QueryCareerPlayDurationScoped computes cumulative career play duration across
// scoped profiles using Frontier's latest Statistics.Exploration.Time_Played
// plus post-Statistics active play time, falling back to session replay.
func QueryCareerPlayDurationScoped(queryer db.Queryer, scope ProfileScope) (time.Duration, bool, error) {
	if !scope.IsAllEnabled() && len(scope.normalizedIDs()) == 0 {
		return 0, false, nil
	}
	anchors, earliestAnchor, uncoveredIDs, anchorError := loadCareerAnchors(queryer, scope)
	if anchorError != nil {
		return 0, false, anchorError
	}
	if len(anchors) == 0 {
		sessions, sessionsError := QueryPlaySessionsScoped(queryer, "", "", scope)
		if sessionsError != nil {
			return 0, false, sessionsError
		}
		duration, found := SumPlayDuration(sessions, "", "")
		return duration, found, nil
	}

	tailSessionsByProfile, tailError := queryPlaySessionsByProfile(queryer, earliestAnchor, "", scope)
	if tailError != nil {
		return 0, false, tailError
	}
	var totalDuration time.Duration
	for profileKey, anchor := range anchors {
		tailDuration, _ := SumPlayDuration(tailSessionsByProfile[profileKey], anchor.timestamp, "")
		totalDuration += time.Duration(anchor.seconds)*time.Second + tailDuration
	}
	if len(uncoveredIDs) == 0 {
		return totalDuration, true, nil
	}
	fallbackSessions, fallbackError := QueryPlaySessionsScoped(queryer, "", "", NewProfileScope(uncoveredIDs))
	if fallbackError != nil {
		return 0, false, fallbackError
	}
	fallbackDuration, _ := SumPlayDuration(fallbackSessions, "", "")
	return totalDuration + fallbackDuration, true, nil
}

func buildProfileDraftSessions(
	queryer db.Queryer,
	profileKey playProfileKey,
	markers []sessionMarkerRow,
	fileMetaByName map[string]journalFileMeta,
	coveredFiles map[string]bool,
) ([]*draftPlaySession, error) {
	var drafts []*draftPlaySession
	var current *draftPlaySession

	openDraft := func(marker sessionMarkerRow, start time.Time, startTimestamp string) *draftPlaySession {
		meta := fileMetaByName[marker.sourceFile]
		coveredFiles[marker.sourceFile] = true
		return &draftPlaySession{
			profileKey:     profileKey,
			profileID:      marker.profileID,
			commander:      marker.commander,
			start:          start,
			startTimestamp: startTimestamp,
			end:            marker.parsedTime,
			sourceFile:     marker.sourceFile,
			prefix:         meta.prefix,
			lastPart:       max(meta.part, 1),
			continuedPart:  meta.continuedPart,
		}
	}

	attachRollover := func(fileName string) bool {
		if current == nil || current.closed || !isDistinctJournalFile(fileName) || fileName == current.sourceFile {
			return false
		}
		meta := fileMetaByName[fileName]
		if !isRolloverPart(current.prefix, current.lastPart, current.continuedPart, meta.prefix, meta.part) {
			return false
		}
		current.sourceFile = fileName
		current.lastPart = meta.part
		current.continuedPart = meta.continuedPart
		coveredFiles[fileName] = true
		return true
	}

	belongsToCurrentSession := func(fileName string) bool {
		if current == nil || current.closed {
			return false
		}
		if !isDistinctJournalFile(current.sourceFile) || !isDistinctJournalFile(fileName) || fileName == current.sourceFile {
			return true
		}
		return attachRollover(fileName)
	}

	for _, marker := range markers {
		switch marker.event {
		case "LoadGame":
			if attachRollover(marker.sourceFile) {
				current.end = laterTime(current.end, marker.parsedTime)
				continue
			}
			if current != nil {
				current.sharedFile = isDistinctJournalFile(current.sourceFile) && current.sourceFile == marker.sourceFile
				drafts = append(drafts, current)
			}
			current = openDraft(marker, marker.parsedTime, marker.timestamp)
		case "Statistics":
			if belongsToCurrentSession(marker.sourceFile) {
				current.end = laterTime(current.end, marker.parsedTime)
				coveredFiles[marker.sourceFile] = true
				if marker.hasTimePlayed && !current.hasTimePlayed {
					current.timePlayed = marker.timePlayed
					current.hasTimePlayed = true
				}
			}
		case "Shutdown", "Music":
			if current != nil && !current.closed {
				if belongsToCurrentSession(marker.sourceFile) {
					current.end = laterTime(current.start, marker.parsedTime)
					current.closed = true
					coveredFiles[marker.sourceFile] = true
					continue
				}
				// A Shutdown or Music:MainMenu from a different distinct journal file cannot close
				// a prior unclosed session; flush the prior session as unclosed first.
				drafts = append(drafts, current)
				current = nil
			}
			if current == nil && isDistinctJournalFile(marker.sourceFile) {
				meta := fileMetaByName[marker.sourceFile]
				if meta.minGameplayTimestamp != "" && meta.minGameplayTimestamp < marker.timestamp {
					if gameplayStart, valid := parseOptionalTime(meta.minGameplayTimestamp); valid {
						current = openDraft(marker, gameplayStart, meta.minGameplayTimestamp)
						current.end = laterTime(current.start, marker.parsedTime)
						current.closed = true
					}
				}
			}
		}
	}
	if current != nil {
		drafts = append(drafts, current)
	}
	if err := extendRolloverObservedEnds(queryer, drafts, fileMetaByName, coveredFiles); err != nil {
		return nil, err
	}
	return drafts, nil
}

func extendRolloverObservedEnds(
	queryer db.Queryer,
	drafts []*draftPlaySession,
	fileMetaByName map[string]journalFileMeta,
	coveredFiles map[string]bool,
) error {
	for index, draft := range drafts {
		if draft.closed || draft.sharedFile || !isDistinctJournalFile(draft.sourceFile) {
			continue
		}
		fileName := draft.sourceFile
		meta := fileMetaByName[fileName]
		for {
			coveredFiles[fileName] = true
			draft.end = laterTime(draft.end, meta.observedEnd)
			if draft.continuedPart <= draft.lastPart || draft.prefix == "" {
				break
			}
			nextFile := fmt.Sprintf("%s.%02d.log", draft.prefix, draft.continuedPart)
			nextMeta, exists := fileMetaByName[nextFile]
			if !exists {
				var lastTimestamp string
				scanError := queryer.QueryRow(
					`SELECT COALESCE(first_timestamp, ''), COALESCE(last_timestamp, ''), continued_part FROM ingested_journals WHERE file_name = ?`,
					nextFile,
				).Scan(&nextMeta.firstTimestamp, &lastTimestamp, &nextMeta.continuedPart)
				if scanError != nil {
					if errors.Is(scanError, sql.ErrNoRows) {
						break
					}
					return fmt.Errorf("query rollover journal metadata for %s: %w", nextFile, scanError)
				}
				nextMeta.observedEnd, _ = parseOptionalTime(lastTimestamp)
			}
			if index+1 < len(drafts) && nextMeta.firstTimestamp != "" && nextMeta.firstTimestamp >= drafts[index+1].startTimestamp {
				break
			}
			fileName = nextFile
			meta = nextMeta
			draft.lastPart = draft.continuedPart
			draft.continuedPart = nextMeta.continuedPart
		}
		if index+1 < len(drafts) && draft.end.After(drafts[index+1].start) {
			draft.end = drafts[index+1].start
		}
	}
	return nil
}

func resolveProfileSessionEnds(drafts []*draftPlaySession) {
	for index := 0; index < len(drafts); {
		draft := drafts[index]
		nextAnchor := -1
		if draft.hasTimePlayed {
			for lookahead := index + 1; lookahead < len(drafts); lookahead++ {
				candidate := drafts[lookahead]
				if candidate.hasTimePlayed && candidate.timePlayed != draft.timePlayed {
					if candidate.timePlayed > draft.timePlayed {
						nextAnchor = lookahead
					}
					break
				}
			}
		}
		if nextAnchor < 0 {
			index++
			continue
		}
		hasUnclosed := false
		var sumObserved time.Duration
		for subIndex := index; subIndex < nextAnchor; subIndex++ {
			if !drafts[subIndex].closed {
				hasUnclosed = true
			}
			sumObserved += max(drafts[subIndex].end.Sub(drafts[subIndex].start), 0)
		}
		if !hasUnclosed {
			index = nextAnchor
			continue
		}
		slack := time.Duration(drafts[nextAnchor].timePlayed-draft.timePlayed)*time.Second - sumObserved
		for subIndex := index; subIndex < nextAnchor; subIndex++ {
			subSession := drafts[subIndex]
			if subSession.closed {
				continue
			}
			baseEnd := subSession.end
			nextStart := drafts[subIndex+1].start
			if baseEnd.After(nextStart) {
				baseEnd = nextStart
			}
			grant := min(max(slack, 0), nextStart.Sub(baseEnd))
			subSession.end = baseEnd.Add(grant)
			slack -= grant
		}
		index = nextAnchor
	}
}
