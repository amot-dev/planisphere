package replay

import (
	"encoding/json"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
)

type targetProfileContext struct {
	targetIDs          []int64
	targetIDSet        map[int64]bool
	satisfiedCount     int
	canEarlyExitOnFull bool
}

func (context *targetProfileContext) markSatisfied(profileID int64) bool {
	if profileID <= 0 || !context.targetIDSet[profileID] {
		return false
	}
	context.satisfiedCount++
	return context.canEarlyExitOnFull && context.satisfiedCount >= len(context.targetIDs)
}

type markerLookaheadState struct {
	hasOpenSession           bool
	hasUnclosedBeforeCurrent bool
	sourceFile               string
	filePrefix               string
	filePart                 int
	lastTimePlayed           int64
	hasTimePlayed            bool
	lookaheadStage           int
}

func loadTargetProfileContext(queryer db.Queryer, scope ProfileScope) (targetProfileContext, error) {
	var targetIDs []int64
	hasUnresolvedRows := false
	if !scope.IsAllEnabled() {
		targetIDs = scope.normalizedIDs()
	} else {
		rows, queryError := queryer.Query(`SELECT id FROM commander_profiles WHERE enabled != 0`)
		if queryError != nil {
			return targetProfileContext{}, fmt.Errorf("query enabled commander profiles: %w", queryError)
		}
		defer rows.Close()
		for rows.Next() {
			var profileID int64
			if scanError := rows.Scan(&profileID); scanError != nil {
				return targetProfileContext{}, fmt.Errorf("scan enabled commander profile: %w", scanError)
			}
			targetIDs = append(targetIDs, profileID)
		}
		if iterError := rows.Err(); iterError != nil {
			return targetProfileContext{}, iterError
		}
		var unresolvedFlag int
		if err := queryer.QueryRow(`SELECT EXISTS(SELECT 1 FROM journal_events WHERE profile_id = 0)`).Scan(&unresolvedFlag); err != nil {
			return targetProfileContext{}, fmt.Errorf("check unresolved profile rows: %w", err)
		}
		hasUnresolvedRows = unresolvedFlag != 0
	}
	idSet := make(map[int64]bool, len(targetIDs))
	for _, profileID := range targetIDs {
		idSet[profileID] = true
	}
	return targetProfileContext{targetIDs: targetIDs, targetIDSet: idSet, canEarlyExitOnFull: !hasUnresolvedRows && len(targetIDs) > 0}, nil
}

func loadCareerAnchors(queryer db.Queryer, scope ProfileScope) (map[playProfileKey]careerStatAnchor, string, []int64, error) {
	targetContext, targetError := loadTargetProfileContext(queryer, scope)
	if targetError != nil {
		return nil, "", nil, targetError
	}
	query := `SELECT timestamp, COALESCE(profile_id, 0), COALESCE(commander, ''), raw_json
		FROM journal_events WHERE event = 'Statistics'` +
		playSessionScopeClause(queryer, scope, "") + ` ORDER BY timestamp DESC, id DESC`

	rows, queryError := queryer.Query(query)
	if queryError != nil {
		return nil, "", nil, fmt.Errorf("query latest Statistics Time_Played: %w", queryError)
	}
	defer rows.Close()

	anchors := make(map[playProfileKey]careerStatAnchor)
	earliestAnchor := ""
	for rows.Next() {
		var timestamp, commander, rawJSON string
		var profileID int64
		if scanError := rows.Scan(&timestamp, &profileID, &commander, &rawJSON); scanError != nil {
			return nil, "", nil, fmt.Errorf("scan Statistics Time_Played: %w", scanError)
		}
		profileKey := makePlayProfileKey(profileID, commander)
		if anchors[profileKey].timestamp != "" {
			continue
		}
		seconds, valid := parseTimePlayed(rawJSON)
		if !valid {
			continue
		}
		anchors[profileKey] = careerStatAnchor{timestamp: timestamp, seconds: seconds}
		if earliestAnchor == "" || timestamp < earliestAnchor {
			earliestAnchor = timestamp
		}
		if targetContext.markSatisfied(profileID) {
			break
		}
	}
	if iterError := rows.Err(); iterError != nil {
		return nil, "", nil, iterError
	}

	var uncoveredIDs []int64
	if len(anchors) > 0 {
		for _, profileID := range targetContext.targetIDs {
			if anchors[playProfileKey{profileID: profileID}].timestamp == "" {
				uncoveredIDs = append(uncoveredIDs, profileID)
			}
		}
	}
	return anchors, earliestAnchor, uncoveredIDs, nil
}

func resolveSessionSeedStart(queryer db.Queryer, startTimestamp string, scope ProfileScope) (string, error) {
	if startTimestamp == "" {
		return "", nil
	}
	targetContext, targetError := loadTargetProfileContext(queryer, scope)
	if targetError != nil {
		return "", targetError
	}
	query := `SELECT
		CASE WHEN COALESCE(f.first_timestamp, '') != '' AND f.first_timestamp < j.timestamp
			THEN f.first_timestamp ELSE j.timestamp END,
		COALESCE(j.profile_id, 0), COALESCE(j.commander, '')
		FROM journal_events j LEFT JOIN ingested_journals f ON f.file_name = j.source_file
		WHERE j.event = 'LoadGame' AND j.timestamp < ?` +
		playSessionScopeClause(queryer, scope, "j") + ` ORDER BY j.timestamp DESC, j.id DESC`

	rows, queryError := queryer.Query(query, startTimestamp)
	if queryError != nil {
		return "", fmt.Errorf("query preceding LoadGame seed: %w", queryError)
	}
	defer rows.Close()

	earliestSeed := startTimestamp
	seenProfiles := make(map[playProfileKey]bool)
	for rows.Next() {
		var seedTimestamp, commander string
		var profileID int64
		if scanError := rows.Scan(&seedTimestamp, &profileID, &commander); scanError != nil {
			return "", fmt.Errorf("scan preceding LoadGame seed: %w", scanError)
		}
		profileKey := makePlayProfileKey(profileID, commander)
		if seenProfiles[profileKey] {
			continue
		}
		seenProfiles[profileKey] = true
		earliestSeed = min(earliestSeed, seedTimestamp)
		if targetContext.markSatisfied(profileID) {
			break
		}
	}
	return earliestSeed, rows.Err()
}

func (state *markerLookaheadState) matchesActiveFile(sourceFile string) bool {
	if !state.hasOpenSession {
		return false
	}
	if !isDistinctJournalFile(state.sourceFile) || !isDistinctJournalFile(sourceFile) || sourceFile == state.sourceFile {
		return true
	}
	candidatePrefix, candidatePart, _ := db.ParseJournalFilenamePart(sourceFile)
	return isRolloverPart(state.filePrefix, state.filePart, 0, candidatePrefix, max(candidatePart, 1))
}

func (state *markerLookaheadState) tryAttachRollover(sourceFile string) (string, int, bool) {
	candidatePrefix, candidatePart, _ := db.ParseJournalFilenamePart(sourceFile)
	normalizedPart := max(candidatePart, 1)
	if state.hasOpenSession &&
		isDistinctJournalFile(state.sourceFile) &&
		isDistinctJournalFile(sourceFile) &&
		sourceFile != state.sourceFile &&
		isRolloverPart(state.filePrefix, state.filePart, 0, candidatePrefix, normalizedPart) {
		state.sourceFile = sourceFile
		state.filePart = normalizedPart
		return candidatePrefix, normalizedPart, true
	}
	return candidatePrefix, normalizedPart, false
}

func (state *markerLookaheadState) recordInWindowMarker(marker sessionMarkerRow) {
	switch marker.event {
	case "LoadGame":
		candidatePrefix, normalizedPart, attached := state.tryAttachRollover(marker.sourceFile)
		if attached {
			return
		}
		if state.hasOpenSession {
			state.hasUnclosedBeforeCurrent = true
		}
		state.hasOpenSession = true
		state.sourceFile = marker.sourceFile
		state.filePrefix = candidatePrefix
		state.filePart = normalizedPart
	case "Statistics":
		if marker.hasTimePlayed && state.matchesActiveFile(marker.sourceFile) {
			if state.hasTimePlayed && marker.timePlayed > state.lastTimePlayed {
				state.hasUnclosedBeforeCurrent = false
			}
			state.lastTimePlayed = marker.timePlayed
			state.hasTimePlayed = true
		}
	case "Shutdown", "Music":
		if state.matchesActiveFile(marker.sourceFile) {
			state.hasOpenSession = false
		}
	}
}

func (state *markerLookaheadState) advanceLookahead(marker sessionMarkerRow) bool {
	switch marker.event {
	case "Shutdown", "Music":
		if state.lookaheadStage == 0 && state.matchesActiveFile(marker.sourceFile) {
			state.hasOpenSession = false
			if !state.hasUnclosedBeforeCurrent {
				state.lookaheadStage = 2
				return true
			}
		}
	case "LoadGame":
		candidatePrefix, normalizedPart, attached := state.tryAttachRollover(marker.sourceFile)
		if state.lookaheadStage == 0 && attached {
			return false
		}
		// Stay at stage 1 across consecutive post-window LoadGame entries until Statistics(Time_Played) arrives.
		state.lookaheadStage = 1
		state.sourceFile = marker.sourceFile
		state.filePrefix = candidatePrefix
		state.filePart = normalizedPart
	case "Statistics":
		if state.lookaheadStage == 1 && marker.hasTimePlayed {
			state.lookaheadStage = 2
			return true
		}
	}
	return false
}

func loadSessionMarkers(queryer db.Queryer, startTimestamp, endTimestamp string, scope ProfileScope, scopeClause string) ([]sessionMarkerRow, string, string, error) {
	queryStart, seedError := resolveSessionSeedStart(queryer, startTimestamp, scope)
	if seedError != nil {
		return nil, "", "", seedError
	}
	// Unary +event keeps SQLite walking idx_journal_events_ts in timestamp order without a temp B-Tree sort.
	query := `SELECT timestamp, event, COALESCE(profile_id, 0), COALESCE(commander, ''), COALESCE(source_file, ''),
		CASE WHEN event IN ('Statistics', 'Music') THEN raw_json ELSE '' END
		FROM journal_events WHERE +event IN ('LoadGame','Statistics','Shutdown','Music')`
	var args []any
	if queryStart != "" {
		query += ` AND timestamp >= ?`
		args = append(args, queryStart)
	}
	rows, queryError := queryer.Query(query+scopeClause+` ORDER BY timestamp ASC, id ASC`, args...)
	if queryError != nil {
		return nil, "", "", fmt.Errorf("query play session markers: %w", queryError)
	}
	defer rows.Close()

	stateByProfile := make(map[playProfileKey]*markerLookaheadState)
	var loaded []sessionMarkerRow
	maxLookahead := ""
	crossedEndTimestamp := false
	pendingLookaheadCount := 0
	for rows.Next() {
		var marker sessionMarkerRow
		var rawJSON string
		if scanError := rows.Scan(&marker.timestamp, &marker.event, &marker.profileID, &marker.commander, &marker.sourceFile, &rawJSON); scanError != nil {
			return nil, "", "", fmt.Errorf("scan play session marker: %w", scanError)
		}
		parsedTime, validTimestamp := parseOptionalTime(marker.timestamp)
		if !validTimestamp {
			continue
		}
		marker.parsedTime = parsedTime
		if marker.event == "Statistics" {
			marker.timePlayed, marker.hasTimePlayed = parseTimePlayed(rawJSON)
		} else if marker.event == "Music" {
			var musicPayload struct {
				MusicTrack string `json:"MusicTrack"`
			}
			if json.Unmarshal([]byte(rawJSON), &musicPayload) != nil || !strings.EqualFold(strings.TrimSpace(musicPayload.MusicTrack), "MainMenu") {
				continue
			}
		}

		profileKey := makePlayProfileKey(marker.profileID, marker.commander)
		if endTimestamp == "" || marker.timestamp <= endTimestamp {
			state := stateByProfile[profileKey]
			if state == nil {
				state = &markerLookaheadState{}
				stateByProfile[profileKey] = state
			}
			state.recordInWindowMarker(marker)
			loaded = append(loaded, marker)
			continue
		}
		if !crossedEndTimestamp {
			crossedEndTimestamp = true
			for _, profileState := range stateByProfile {
				if profileState.hasOpenSession || profileState.hasUnclosedBeforeCurrent {
					pendingLookaheadCount++
				} else {
					profileState.lookaheadStage = 2
				}
			}
			if pendingLookaheadCount == 0 {
				break
			}
		}
		state := stateByProfile[profileKey]
		if state == nil || state.lookaheadStage >= 2 {
			continue
		}
		if state.advanceLookahead(marker) {
			pendingLookaheadCount--
		}
		maxLookahead = max(maxLookahead, marker.timestamp)
		loaded = append(loaded, marker)
		if pendingLookaheadCount == 0 {
			break
		}
	}
	boundsEnd := ""
	if endTimestamp != "" && (!crossedEndTimestamp || pendingLookaheadCount == 0) {
		hasOpenProfile := false
		for _, profileState := range stateByProfile {
			if !crossedEndTimestamp && (profileState.hasOpenSession || profileState.hasUnclosedBeforeCurrent) {
				hasOpenProfile = true
				break
			}
		}
		if !hasOpenProfile {
			boundsEnd = max(endTimestamp, maxLookahead)
		}
	}
	return loaded, queryStart, boundsEnd, rows.Err()
}

func loadFileBoundsAndTruncated(queryer db.Queryer, startTimestamp, endTimestamp, scopeClause string) (map[string]journalFileMeta, []*draftPlaySession, error) {
	subquery := `SELECT COALESCE(source_file, '') AS source_file, COALESCE(profile_id, 0) AS profile_id,
		COALESCE(commander, '') AS commander, MAX(timestamp) AS max_timestamp,
		MIN(CASE WHEN event NOT IN ('Music', 'Shutdown', 'Commander', 'ClearSavedGame', 'NewCommander') THEN timestamp END) AS min_gameplay_timestamp
		FROM journal_events WHERE 1=1`
	var args []any
	if startTimestamp != "" {
		subquery += ` AND timestamp >= ?`
		args = append(args, startTimestamp)
	}
	if endTimestamp != "" {
		subquery += ` AND timestamp <= ?`
		args = append(args, endTimestamp)
	}
	subquery += scopeClause + ` GROUP BY COALESCE(source_file, ''), COALESCE(profile_id, 0),
		CASE WHEN COALESCE(profile_id, 0) > 0 THEN '' ELSE LOWER(COALESCE(commander, '')) END`

	query := `SELECT grouped.source_file, grouped.profile_id, grouped.commander,
		grouped.max_timestamp, COALESCE(grouped.min_gameplay_timestamp, ''),
		COALESCE(f.first_timestamp, ''), COALESCE(f.last_timestamp, ''),
		COALESCE(f.part, 0), COALESCE(f.continued_part, 0)
		FROM (` + subquery + `) AS grouped
		LEFT JOIN ingested_journals f ON f.file_name = grouped.source_file`

	rows, queryError := queryer.Query(query, args...)
	if queryError != nil {
		return nil, nil, fmt.Errorf("query per-file event bounds: %w", queryError)
	}
	defer rows.Close()

	fileMetaByName := make(map[string]journalFileMeta)
	var candidates []*draftPlaySession
	for rows.Next() {
		var sourceFile, commander, maxTimestamp, minGameplayTimestamp, firstTimestamp, lastTimestamp string
		var profileID int64
		var part, continuedPart int
		if scanError := rows.Scan(&sourceFile, &profileID, &commander, &maxTimestamp, &minGameplayTimestamp, &firstTimestamp, &lastTimestamp, &part, &continuedPart); scanError != nil {
			return nil, nil, fmt.Errorf("scan per-file event bounds: %w", scanError)
		}
		maxTime, maxValid := parseOptionalTime(maxTimestamp)
		if !maxValid {
			continue
		}
		metaLastTime, _ := parseOptionalTime(lastTimestamp)
		observedEnd := laterTime(maxTime, metaLastTime)
		prefix, parsedPart, _ := db.ParseJournalFilenamePart(sourceFile)
		existingMeta := fileMetaByName[sourceFile]
		mergedGameplayStart := existingMeta.minGameplayTimestamp
		if minGameplayTimestamp != "" && (mergedGameplayStart == "" || minGameplayTimestamp < mergedGameplayStart) {
			mergedGameplayStart = minGameplayTimestamp
		}
		meta := journalFileMeta{
			firstTimestamp:       firstTimestamp,
			minGameplayTimestamp: mergedGameplayStart,
			prefix:               prefix,
			part:                 max(part, parsedPart, 1),
			continuedPart:        continuedPart,
			observedEnd:          laterTime(existingMeta.observedEnd, observedEnd),
		}
		fileMetaByName[sourceFile] = meta
		// Only emit a truncated candidate when the file contains at least one non-Music, non-Shutdown gameplay event.
		if isDistinctJournalFile(sourceFile) && meta.part == 1 && minGameplayTimestamp != "" {
			if gameplayStart, validStart := parseOptionalTime(minGameplayTimestamp); validStart {
				candidates = append(candidates, &draftPlaySession{
					profileKey: makePlayProfileKey(profileID, commander), profileID: profileID, commander: commander,
					start: gameplayStart, startTimestamp: minGameplayTimestamp, end: observedEnd,
					sourceFile: sourceFile, prefix: meta.prefix, lastPart: meta.part, continuedPart: meta.continuedPart,
				})
			}
		}
	}
	return fileMetaByName, candidates, rows.Err()
}

func populateIntervalObservedEnds(queryer db.Queryer, draftsByProfile map[playProfileKey][]*draftPlaySession, scopeClause string) error {
	earliestNeedStart, latestNeedEnd, hasUnboundedNeed := "", "", false
	needingDrafts := make(map[*draftPlaySession]bool)
	for _, drafts := range draftsByProfile {
		for index, draft := range drafts {
			if draft.closed || (!draft.sharedFile && isDistinctJournalFile(draft.sourceFile)) {
				continue
			}
			needingDrafts[draft] = true
			if earliestNeedStart == "" || draft.startTimestamp < earliestNeedStart {
				earliestNeedStart = draft.startTimestamp
			}
			if index+1 < len(drafts) {
				if nextStart := drafts[index+1].startTimestamp; latestNeedEnd == "" || nextStart > latestNeedEnd {
					latestNeedEnd = nextStart
				}
			} else {
				hasUnboundedNeed = true
			}
		}
	}
	if len(needingDrafts) == 0 {
		return nil
	}
	query := `SELECT timestamp, COALESCE(profile_id, 0), COALESCE(commander, '') FROM journal_events WHERE timestamp >= ?`
	args := []any{earliestNeedStart}
	if !hasUnboundedNeed && latestNeedEnd != "" {
		query += ` AND timestamp < ?`
		args = append(args, latestNeedEnd)
	}
	rows, queryError := queryer.Query(query+scopeClause+` ORDER BY timestamp ASC, id ASC`, args...)
	if queryError != nil {
		return fmt.Errorf("query unclosed session observed bounds: %w", queryError)
	}
	defer rows.Close()
	cursorByProfile := make(map[playProfileKey]int, len(draftsByProfile))
	maxTimestampByDraft := make(map[*draftPlaySession]string, len(needingDrafts))
	for rows.Next() {
		var eventTimestamp, commander string
		var profileID int64
		if scanError := rows.Scan(&eventTimestamp, &profileID, &commander); scanError != nil {
			return fmt.Errorf("scan unclosed session observed bound: %w", scanError)
		}
		profileKey := makePlayProfileKey(profileID, commander)
		drafts := draftsByProfile[profileKey]
		if len(drafts) == 0 {
			continue
		}
		cursor := cursorByProfile[profileKey]
		for cursor+1 < len(drafts) && eventTimestamp >= drafts[cursor+1].startTimestamp {
			cursor++
		}
		cursorByProfile[profileKey] = cursor
		if draft := drafts[cursor]; needingDrafts[draft] && eventTimestamp >= draft.startTimestamp {
			maxTimestampByDraft[draft] = eventTimestamp
		}
	}
	if iterError := rows.Err(); iterError != nil {
		return iterError
	}
	for draft, maxTimestamp := range maxTimestampByDraft {
		if parsedMax, valid := parseOptionalTime(maxTimestamp); valid {
			draft.end = laterTime(draft.end, parsedMax)
		}
	}
	return nil
}
