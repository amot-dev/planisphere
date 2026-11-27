package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type resolverSighting struct{ nocaseName, rawFID, effectiveFID, universe, firstSeen string }
type sightingTupleKey struct{ nocaseName, rawFID, universe string }
type commanderOverrideRule struct{ ruleType, sourceKey, targetKey, splitTimestamp string }

type resolverCheckpoint struct {
	id                                                 int64
	timestamp, event, nocaseName, rawFID, effectiveFID string
	universe, sourceFile, rawJSON                      string
}

type eraBoundary struct {
	timestamp        string
	exclusiveAfterID int64
	isManual         bool
}

type profileEventSegment struct {
	tuples                     []sightingTupleKey
	hasStartBound, hasEndBound bool
	startBound, endBound       eraBoundary
	fromManualMerge            bool
}

type resolvedCommanderProfile struct {
	id, enabled                         int64
	eraIndex                            int
	profileKey, displayName, fid        string
	universe, firstSeen, lastSeen       string
	isArchivedCareer, hasManualOverride bool
	segments                            []profileEventSegment
}

type resolverBaseGroup struct {
	key, universe, fid string
	hasManualOverride  bool
	sightings          []resolverSighting
	checkpoints        []resolverCheckpoint
	tuples             []sightingTupleKey
}

// ResolveCommanders deterministically resolves commander save-slot profiles,
// career wipe eras, aliases, journal_events.profile_id assignments, and
// expedition commander_profile_id event anchors.
func ResolveCommanders(transaction *sql.Tx) error {
	_, err := ResolveCommandersChanged(transaction)
	return err
}

// ResolveCommandersChanged runs ResolveCommanders and reports whether commander
// profile topology or expedition commander bindings changed.
func ResolveCommandersChanged(transaction *sql.Tx) (bool, error) {
	beforeTopology, err := snapshotProfileTopology(transaction)
	if err != nil {
		return false, err
	}
	expeditionsChanged, err := resolveCommandersCore(transaction)
	if err != nil {
		return false, err
	}
	afterTopology, err := snapshotProfileTopology(transaction)
	if err != nil {
		return false, err
	}
	return expeditionsChanged || !slices.Equal(beforeTopology, afterTopology), nil
}

func resolveCommandersCore(transaction *sql.Tx) (bool, error) {
	anchors, err := snapshotExpeditionCommanderAnchors(transaction)
	if err != nil {
		return false, err
	}
	existingEnabled, legacyEnabled, profilesWereEmpty, err := loadExistingAndLegacyEnabled(transaction)
	if err != nil {
		return false, err
	}
	overrides, err := loadCommanderOverrides(transaction)
	if err != nil {
		return false, err
	}
	sightings, checkpoints, err := loadSightingsAndCheckpoints(transaction)
	if err != nil {
		return false, err
	}
	if len(sightings) == 0 && len(checkpoints) == 0 {
		if _, deleteErr := transaction.Exec(`DELETE FROM commander_profiles`); deleteErr != nil {
			return false, deleteErr
		}
		return reconcileExpeditionCommandersFromAnchors(transaction, anchors)
	}
	bridgePreV3LiveFIDs(sightings, checkpoints)
	baseGroups := buildResolverBaseGroups(sightings, checkpoints, overrides)
	profiles, err := splitAllBaseGroups(transaction, baseGroups, overrides)
	if err != nil {
		return false, err
	}
	surviving := applyProfileMergeOverrides(profiles, overrides)
	if len(surviving) == 0 {
		if _, deleteErr := transaction.Exec(`DELETE FROM commander_profiles`); deleteErr != nil {
			return false, deleteErr
		}
		return reconcileExpeditionCommandersFromAnchors(transaction, anchors)
	}
	if err := persistProfileShellsAndEventIDs(transaction, surviving); err != nil {
		return false, err
	}
	if err := finalizeProfilesAndAliases(transaction, surviving, existingEnabled, legacyEnabled, profilesWereEmpty); err != nil {
		return false, err
	}
	return reconcileExpeditionCommandersFromAnchors(transaction, anchors)
}

func bridgePreV3LiveFIDs(sightings []resolverSighting, checkpoints []resolverCheckpoint) {
	fidsByLiveName := make(map[string]map[string]string, len(sightings))
	recordLiveFID := func(universe, nocase, fid string) {
		if universe == "live" && nocase != "" && fid != "" {
			if fidsByLiveName[nocase] == nil {
				fidsByLiveName[nocase] = make(map[string]string, 1)
			}
			if lower := asciiLower(fid); fidsByLiveName[nocase][lower] == "" {
				fidsByLiveName[nocase][lower] = fid
			}
		}
	}
	for _, sighting := range sightings {
		recordLiveFID(sighting.universe, sighting.nocaseName, sighting.rawFID)
	}
	for _, checkpoint := range checkpoints {
		recordLiveFID(checkpoint.universe, checkpoint.nocaseName, checkpoint.rawFID)
	}
	soleLiveFID := func(nocase string) string {
		if entries := fidsByLiveName[nocase]; len(entries) == 1 {
			for _, fid := range entries {
				return fid
			}
		}
		return ""
	}
	for index := range sightings {
		if sightings[index].effectiveFID == "" && sightings[index].universe == "live" {
			sightings[index].effectiveFID = soleLiveFID(sightings[index].nocaseName)
		}
	}
	for index := range checkpoints {
		if checkpoints[index].effectiveFID == "" && checkpoints[index].universe == "live" {
			checkpoints[index].effectiveFID = soleLiveFID(checkpoints[index].nocaseName)
		}
	}
}

func buildResolverBaseGroups(sightings []resolverSighting, checkpoints []resolverCheckpoint, overrides []commanderOverrideRule) []*resolverBaseGroup {
	unmerged := make(map[string]bool)
	for _, rule := range overrides {
		if rule.ruleType == "unmerge" && rule.sourceKey != "" {
			unmerged[rule.sourceKey] = true
		}
	}
	groups := make(map[string]*resolverBaseGroup, len(sightings))
	seenTuples := make(map[string]map[sightingTupleKey]bool, len(sightings))
	ensure := func(universe, fid, nocase string) *resolverBaseGroup {
		lowerFID := asciiLower(fid)
		trimmedNocase := strings.TrimSpace(nocase)
		key, isUnmerged := universe+":name:"+trimmedNocase, false
		if lowerFID != "" && trimmedNocase != "" && unmerged[universe+":"+lowerFID+":"+trimmedNocase] {
			key, isUnmerged = "unmerged:"+universe+":"+lowerFID+":"+trimmedNocase, true
		} else if lowerFID != "" {
			key = universe + ":" + lowerFID
		}
		group := groups[key]
		if group == nil {
			group = &resolverBaseGroup{key: key, universe: universe, fid: fid, hasManualOverride: isUnmerged}
			groups[key], seenTuples[key] = group, make(map[sightingTupleKey]bool, 2)
		} else if group.fid == "" && fid != "" {
			group.fid = fid
		}
		return group
	}
	addTuple := func(group *resolverBaseGroup, tuple sightingTupleKey) {
		if !seenTuples[group.key][tuple] {
			seenTuples[group.key][tuple] = true
			group.tuples = append(group.tuples, tuple)
		}
	}
	for _, sighting := range sightings {
		group := ensure(sighting.universe, sighting.effectiveFID, sighting.nocaseName)
		group.sightings = append(group.sightings, sighting)
		addTuple(group, sightingTupleKey{sighting.nocaseName, sighting.rawFID, sighting.universe})
	}
	for _, checkpoint := range checkpoints {
		group := ensure(checkpoint.universe, checkpoint.effectiveFID, checkpoint.nocaseName)
		group.checkpoints = append(group.checkpoints, checkpoint)
		addTuple(group, sightingTupleKey{checkpoint.nocaseName, checkpoint.rawFID, checkpoint.universe})
	}
	ordered := make([]*resolverBaseGroup, 0, len(groups))
	for _, group := range groups {
		ordered = append(ordered, group)
	}
	slices.SortFunc(ordered, func(left, right *resolverBaseGroup) int { return strings.Compare(left.key, right.key) })
	return ordered
}

func splitAllBaseGroups(transaction *sql.Tx, groups []*resolverBaseGroup, overrides []commanderOverrideRule) ([]*resolvedCommanderProfile, error) {
	allProfiles := make([]*resolvedCommanderProfile, 0, len(groups))
	for _, group := range groups {
		boundaries := collectCandidateBoundaries(group, overrides)
		if len(boundaries) == 0 {
			if profile := buildUnsplitGroupProfile(group); profile != nil {
				allProfiles = append(allProfiles, profile)
			}
			continue
		}
		groupProfiles, err := buildSplitGroupProfiles(transaction, group, boundaries)
		if err != nil {
			return nil, err
		}
		allProfiles = append(allProfiles, groupProfiles...)
	}
	return allProfiles, nil
}

func buildUnsplitGroupProfile(group *resolverBaseGroup) *resolvedCommanderProfile {
	if len(group.sightings) == 0 {
		return nil
	}
	firstSeen := ""
	for _, sighting := range group.sightings {
		if firstSeen == "" || (sighting.firstSeen != "" && sighting.firstSeen < firstSeen) {
			firstSeen = sighting.firstSeen
		}
	}
	return &resolvedCommanderProfile{
		profileKey: group.key + ":era:0", fid: group.fid, universe: group.universe,
		hasManualOverride: group.hasManualOverride, firstSeen: firstSeen,
		segments: []profileEventSegment{{tuples: group.tuples}},
	}
}

func buildSplitGroupProfiles(transaction *sql.Tx, group *resolverBaseGroup, boundaries []eraBoundary) ([]*resolvedCommanderProfile, error) {
	groupProfiles := make([]*resolvedCommanderProfile, 0, len(boundaries)+1)
	for sliceIdx := 0; sliceIdx <= len(boundaries); sliceIdx++ {
		segment, manualTouch := profileEventSegment{tuples: group.tuples}, group.hasManualOverride
		if sliceIdx > 0 {
			segment.hasStartBound, segment.startBound = true, boundaries[sliceIdx-1]
			manualTouch = manualTouch || boundaries[sliceIdx-1].isManual
		}
		if sliceIdx < len(boundaries) {
			segment.hasEndBound, segment.endBound = true, boundaries[sliceIdx]
			manualTouch = manualTouch || boundaries[sliceIdx].isManual
		}
		count, firstSeen, err := querySplitSegmentStats(transaction, segment)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			eraIdx := len(groupProfiles)
			groupProfiles = append(groupProfiles, &resolvedCommanderProfile{
				profileKey: fmt.Sprintf("%s:era:%d", group.key, eraIdx), fid: group.fid, universe: group.universe,
				eraIndex: eraIdx, hasManualOverride: manualTouch, firstSeen: firstSeen, segments: []profileEventSegment{segment},
			})
		}
	}
	for index := range groupProfiles {
		groupProfiles[index].isArchivedCareer = index < len(groupProfiles)-1
	}
	return groupProfiles, nil
}

func collectCandidateBoundaries(group *resolverBaseGroup, overrides []commanderOverrideRule) []eraBoundary {
	var manual []eraBoundary
	suppressStatsDrop, eraPrefix := false, group.key+":era:"
	for _, rule := range overrides {
		if rule.ruleType == "split" && rule.splitTimestamp != "" && (rule.sourceKey == group.key || strings.HasPrefix(rule.sourceKey, eraPrefix)) {
			manual = append(manual, eraBoundary{timestamp: rule.splitTimestamp, isManual: true})
		} else if rule.ruleType == "merge" && (rule.sourceKey == group.key || rule.targetKey == group.key) {
			suppressStatsDrop = true
		}
	}
	cmpBound := func(left, right eraBoundary) int {
		if left.timestamp != right.timestamp {
			return strings.Compare(left.timestamp, right.timestamp)
		}
		return int(left.exclusiveAfterID - right.exclusiveAfterID)
	}
	slices.SortFunc(manual, cmpBound)
	combined := append(detectAutoSplitBoundaries(group, manual, suppressStatsDrop), manual...)
	slices.SortFunc(combined, cmpBound)
	deduped := make([]eraBoundary, 0, len(combined))
	for _, item := range combined {
		if count := len(deduped); count > 0 && deduped[count-1].timestamp == item.timestamp {
			if item.exclusiveAfterID > deduped[count-1].exclusiveAfterID {
				deduped[count-1].exclusiveAfterID = item.exclusiveAfterID
			}
			deduped[count-1].isManual = deduped[count-1].isManual || item.isManual
		} else {
			deduped = append(deduped, item)
		}
	}
	return deduped
}

func detectAutoSplitBoundaries(group *resolverBaseGroup, manual []eraBoundary, suppressStatsDrop bool) []eraBoundary {
	eraEarliest := ""
	for _, sighting := range group.sightings {
		if eraEarliest == "" || (sighting.firstSeen != "" && sighting.firstSeen < eraEarliest) {
			eraEarliest = sighting.firstSeen
		}
	}
	var boundaries []eraBoundary
	awaitingPostClear, reachedStatsThreshold := false, false
	lastAnchorIdx, manualCursor := -1, 0
	for index, checkpoint := range group.checkpoints {
		for manualCursor < len(manual) && manual[manualCursor].timestamp <= checkpoint.timestamp {
			awaitingPostClear, eraEarliest, reachedStatsThreshold, lastAnchorIdx = false, "", false, index-1
			manualCursor++
		}
		if eraEarliest == "" {
			eraEarliest = checkpoint.timestamp
		}
		switch checkpoint.event {
		case "ClearSavedGame":
			boundaries = append(boundaries, eraBoundary{timestamp: checkpoint.timestamp, exclusiveAfterID: checkpoint.id})
			awaitingPostClear, eraEarliest, reachedStatsThreshold, lastAnchorIdx = true, "", false, index
		case "NewCommander":
			if !awaitingPostClear && eraEarliest != "" && eraEarliest < checkpoint.timestamp {
				boundaries = append(boundaries, eraBoundary{timestamp: checkpoint.timestamp})
				reachedStatsThreshold = false
			}
			awaitingPostClear, eraEarliest, lastAnchorIdx = false, checkpoint.timestamp, index
		case "Commander", "LoadGame":
			if awaitingPostClear {
				awaitingPostClear, eraEarliest = false, checkpoint.timestamp
			}
		case "Statistics":
			awaitingPostClear = false
			if splitTS, didSplit := evaluateStatsDropBoundary(group, index, lastAnchorIdx, suppressStatsDrop, &reachedStatsThreshold); didSplit {
				boundaries = append(boundaries, eraBoundary{timestamp: splitTS})
				eraEarliest = splitTS
			}
			lastAnchorIdx = index
		}
	}
	return boundaries
}

func evaluateStatsDropBoundary(group *resolverBaseGroup, statsIdx, lastAnchorIdx int, suppressStatsDrop bool, reachedThreshold *bool) (string, bool) {
	var payload struct {
		Exploration *struct {
			SystemsVisited       float64 `json:"Systems_Visited"`
			TotalHyperspaceJumps float64 `json:"Total_Hyperspace_Jumps"`
		} `json:"Exploration"`
	}
	if json.Unmarshal([]byte(group.checkpoints[statsIdx].rawJSON), &payload) != nil || payload.Exploration == nil {
		return "", false
	}
	visited, jumps := payload.Exploration.SystemsVisited, payload.Exploration.TotalHyperspaceJumps
	if *reachedThreshold && visited < 5 && jumps < 5 {
		*reachedThreshold = false
		if suppressStatsDrop {
			group.hasManualOverride = true
			return "", false
		}
		return findPrecedingSessionTimestamp(group.checkpoints, lastAnchorIdx, statsIdx), true
	}
	if visited >= 25 || jumps >= 25 {
		*reachedThreshold = true
	}
	return "", false
}

func findPrecedingSessionTimestamp(checkpoints []resolverCheckpoint, afterIdx int, statsIdx int) string {
	sessionIdx := -1
	for scan := statsIdx - 1; scan > afterIdx; scan-- {
		if checkpoints[scan].event == "Commander" || checkpoints[scan].event == "LoadGame" {
			sessionIdx = scan
			break
		}
	}
	if sessionIdx < 0 {
		return checkpoints[statsIdx].timestamp
	}
	if sessionIdx-1 > afterIdx {
		previous, current := checkpoints[sessionIdx-1], checkpoints[sessionIdx]
		if (previous.event == "Commander" || previous.event == "LoadGame") && previous.event != current.event &&
			(previous.sourceFile == current.sourceFile || previous.timestamp == current.timestamp) {
			sessionIdx--
		}
	}
	return checkpoints[sessionIdx].timestamp
}

func applyProfileMergeOverrides(profiles []*resolvedCommanderProfile, overrides []commanderOverrideRule) []*resolvedCommanderProfile {
	byKey := make(map[string]*resolvedCommanderProfile, len(profiles))
	for _, profile := range profiles {
		byKey[profile.profileKey] = profile
	}
	links := buildNormalizedMergeLinks(byKey, overrides)
	if len(links) == 0 {
		return profiles
	}
	folded := make(map[string]bool, len(links))
	for _, sourceProfile := range profiles {
		currentKey, visited := sourceProfile.profileKey, map[string]bool{sourceProfile.profileKey: true}
		for links[currentKey] != "" && !visited[links[currentKey]] && byKey[links[currentKey]] != nil {
			currentKey = links[currentKey]
			visited[currentKey] = true
		}
		if currentKey != sourceProfile.profileKey {
			foldMergedProfile(byKey[currentKey], sourceProfile)
			folded[sourceProfile.profileKey] = true
		}
	}
	surviving := make([]*resolvedCommanderProfile, 0, len(profiles)-len(folded))
	for _, profile := range profiles {
		if !folded[profile.profileKey] {
			surviving = append(surviving, profile)
		}
	}
	return surviving
}

func buildNormalizedMergeLinks(byKey map[string]*resolvedCommanderProfile, overrides []commanderOverrideRule) map[string]string {
	links := make(map[string]string)
	reachesKey := func(startKey, goalKey string) bool {
		cursor, visited := startKey, map[string]bool{startKey: true}
		for links[cursor] != "" {
			nextKey := links[cursor]
			if nextKey == goalKey {
				return true
			}
			if visited[nextKey] {
				return false
			}
			visited[nextKey] = true
			cursor = nextKey
		}
		return false
	}
	for _, rule := range overrides {
		if rule.ruleType != "merge" || rule.sourceKey == "" || rule.targetKey == "" {
			continue
		}
		sourceKey, targetKey := rule.sourceKey, rule.targetKey
		if byKey[sourceKey] == nil && byKey[sourceKey+":era:0"] != nil {
			sourceKey += ":era:0"
		}
		if byKey[targetKey] == nil && byKey[targetKey+":era:0"] != nil {
			targetKey += ":era:0"
		}
		if byKey[sourceKey] == nil || byKey[targetKey] == nil || sourceKey == targetKey || reachesKey(targetKey, sourceKey) {
			continue
		}
		links[sourceKey] = targetKey
	}
	return links
}

func foldMergedProfile(target, source *resolvedCommanderProfile) {
	target.hasManualOverride = true
	target.isArchivedCareer = target.isArchivedCareer && source.isArchivedCareer
	if target.fid == "" {
		target.fid = source.fid
	}
	if target.firstSeen == "" || (source.firstSeen != "" && source.firstSeen < target.firstSeen) {
		target.firstSeen = source.firstSeen
	}
	for _, segment := range source.segments {
		segment.fromManualMerge = true
		target.segments = append(target.segments, segment)
	}
}
