package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// --- Utility: deterministic shuffle ---
func shuffle[T any](slice []T, seed int64) {
	r := rand.New(rand.NewSource(seed))
	r.Shuffle(len(slice), func(i, j int) { slice[i], slice[j] = slice[j], slice[i] })
}

// --- Utility: min/max ---
func min(a, b uint) uint {
	if a < b {
		return a
	}
	return b
}
func max(a, b uint) uint {
	if a > b {
		return a
	}
	return b
}
func pairExists(pairs [][2]uint, target [2]uint) bool {
	for _, p := range pairs {
		if (p[0] == target[0] && p[1] == target[1]) || (p[0] == target[1] && p[1] == target[0]) {
			return true
		}
	}
	return false
}

// --- POST /api/matches/generate ---
func HandleGenerateWeeklyMatches(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		Week    int `json:"week"`
		Matches []struct {
			MatchCode string `json:"match_code"`
			TeamA     string `json:"team_a"`
			TeamB     string `json:"team_b"`
		} `json:"matches"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Week <= 0 {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Matches) == 0 {
		http.Error(w, "No matches provided to publish", http.StatusBadRequest)
		return
	}

	// Step 1: Load all teams for name → ID lookup
	var teams []Team
	if err := DB.Find(&teams).Error; err != nil {
		http.Error(w, "Failed to load teams", http.StatusInternalServerError)
		return
	}
	nameToID := make(map[string]uint)
	for _, t := range teams {
		nameToID[strings.TrimSpace(t.Name)] = t.ID
	}

	// --- Step 1.5: Auto double-forfeit only previous week's unfinished matches (same season only) ---

	previousWeek := req.Week - 1
	if req.Week > 1 {
		log.Printf("🔍 Auto-forfeit check for Season %s Week %d", currentSeason, previousWeek)

		// Find all matches from previous week that are unfinished
		var oldMatches []Match
		if err := DB.
			Where("season = ?", currentSeason).
			Where("CAST(week AS INTEGER) = ?", previousWeek).
			Where("status NOT IN ?", []string{"Completed", "Forfeit", "Cancelled"}).
			Find(&oldMatches).Error; err != nil {
			log.Printf("⚠️ Could not load previous week matches: %v", err)
		}

		for _, old := range oldMatches {
			old.Status = "Forfeit"
			old.WinnerID = nil
			old.LoserID = nil

			// clear map scores (FK safe)
			_ = DB.Where("match_id = ?", old.ID).Delete(&MatchScore{}).Error

			if err := DB.Save(&old).Error; err != nil {
				log.Printf("⚠️ Failed to auto forfeit match #%d: %v", old.ID, err)
			} else {
				log.Printf("🏳️ Auto double-forfeited Week %d match #%d (%d vs %d)",
					previousWeek, old.ID, old.TeamAID, old.TeamBID)
			}
		}
	} else {
		log.Printf("🛑 Week 1 generation → skipping all auto-forfeit checks.")
	}

	// Step 2: Validate and insert each match from preview
	now := time.Now()
	systemID := int64(0)
	inserted := 0

	for _, pm := range req.Matches {
		aID, aOk := nameToID[pm.TeamA]
		bID, bOk := nameToID[pm.TeamB]
		if !aOk || !bOk {
			log.Printf("⚠️ Skipping invalid matchup: %s vs %s", pm.TeamA, pm.TeamB)
			continue
		}

		matchCode := pm.MatchCode
		if matchCode == "" {
			matchCode = fmt.Sprintf("%s-Week%d-M%03d", currentSeason, req.Week, inserted+1)
		}

		newMatch := Match{
			MatchCode:     matchCode,
			TeamAID:       aID,
			TeamBID:       bID,
			ProposedDate:  &now,
			ScheduledDate: nil,
			Status:        "Scheduled",
			ProposerID:    &systemID,
			Season:        currentSeason,
			Week:          strconv.Itoa(req.Week),
		}

		if err := DB.Create(&newMatch).Error; err != nil {
			log.Printf("❌ Failed to insert %s: %v", matchCode, err)
		} else {
			inserted++
			log.Printf("✅ Published %s → %s vs %s", matchCode, pm.TeamA, pm.TeamB)
		}
	}

	// --- Step X: Update global league week + reset challenge usage ---
	var ls LeagueSettings
	now = time.Now()

	if err := DB.First(&ls, 1).Error; err != nil {
		ls = LeagueSettings{
			ID:                   1,
			CurrentWeek:          req.Week,
			WeeklyChallengeLimit: 1,
			LastMatchGeneration:  &now,
		}
		DB.Create(&ls)
	} else {
		ls.CurrentWeek = req.Week
		ls.LastMatchGeneration = &now
		DB.Save(&ls)
	}

	// Clear cooldown for all players who left before this generation
	DB.Model(&Player{}).
		Where("last_left_team_at < ?", now).
		Update("last_left_team_at", nil)

	log.Printf("📅 Updated LeagueSettings.CurrentWeek = %d", ls.CurrentWeek)

	// Reset weekly challenges used for all teams
	if err := DB.
		Session(&gorm.Session{AllowGlobalUpdate: true}).
		Model(&Team{}).
		Update("weekly_challenges_used", 0).Error; err != nil {

		log.Printf("⚠️ Failed to reset weekly_challenges_used: %v", err)
	} else {
		log.Printf("🔄 Reset weekly_challenges_used for all teams")
	}

	respondJSON(w, map[string]any{
		"success": true,
		"week":    req.Week,
		"count":   inserted,
		"message": fmt.Sprintf("✅ Published %d matches for Week %d", inserted, req.Week),
	})
}

// --- GET /api/mod/matches/preview?week=2 ---
func HandlePreviewWeeklyMatches(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	if strings.TrimSpace(currentSeason) == "" {
		http.Error(w, "CURRENT_SEASON not configured", http.StatusInternalServerError)
		return
	}

	weekStr := r.URL.Query().Get("week")
	week, err := strconv.Atoi(weekStr)
	if err != nil || week <= 0 {
		http.Error(w, "Invalid week number", http.StatusBadRequest)
		return
	}

	// Prevent duplicate generation
	var existingCount int64
	weekPatternAny := fmt.Sprintf("%%Week%d-%%", week)
	weekPatternDashed := fmt.Sprintf("%%-Week%d-%%", week)
	DB.Model(&Match{}).
		Where("season = ?", currentSeason).
		Where("COALESCE(is_finals, false) = false").
		Where("match_code NOT ILIKE ?", "%-CHAL-%").
		Where(`(
			(NULLIF(week, '') IS NOT NULL AND CAST(NULLIF(week, '') AS INTEGER) = ?)
			OR match_code ILIKE ?
			OR match_code ILIKE ?
		)`, week, weekPatternAny, weekPatternDashed).
		Count(&existingCount)
	if existingCount > 0 {
		respondJSON(w, map[string]any{
			"success": false,
			"error":   fmt.Sprintf("Week %d already has %d matches — aborted", week, existingCount),
		})
		return
	}

	// Load ALL teams and filter to active + roster minimum
	var allTeams []Team
	if err := DB.Find(&allTeams).Error; err != nil {
		http.Error(w, "Failed to load teams", http.StatusInternalServerError)
		return
	}

	minPlayers := getEnvInt("MIN_TEAM_PLAYERS", 3)
	var teams []Team

	for _, t := range allTeams {
		if !strings.EqualFold(t.Status, "Active") {
			log.Printf("⏭️ Skipping %s (status: %s)", t.Name, t.Status)
			continue
		}

		var playerCount int64
		DB.Model(&TeamMember{}).Where("team_id = ?", t.ID).Count(&playerCount)

		if playerCount < int64(minPlayers) {
			log.Printf("🚫 Skipping %s — only %d players (need %d)", t.Name, playerCount, minPlayers)
			continue
		}

		teams = append(teams, t)
	}

	if len(teams) < 2 {
		http.Error(w, "Not enough ACTIVE teams to generate preview", http.StatusBadRequest)
		return
	}

	// Avoid rematches from last week
	type SimpleMatch struct {
		TeamAID uint
		TeamBID uint
	}
	//--------------------------------------------------
	// Build pairing constraints
	// - Prefer: pairs that have NOT happened yet this season
	// - When all pairs have happened: avoid repeating last week if possible
	//--------------------------------------------------
	activeIDs := make(map[uint]bool, len(teams))
	for _, t := range teams {
		activeIDs[t.ID] = true
	}

	// Pairs already played (or at least already scheduled) this season
	playedPairs := make(map[[2]uint]bool)
	var seasonPairs []SimpleMatch
	DB.Table("matches").
		Select("team_a_id, team_b_id").
		Where("season = ?", currentSeason).
		Where("COALESCE(is_finals, false) = false").
		Where("match_code NOT ILIKE ?", "%-CHAL-%").
		Find(&seasonPairs)
	for _, m := range seasonPairs {
		if m.TeamAID == 0 || m.TeamBID == 0 {
			continue
		}
		if !activeIDs[m.TeamAID] || !activeIDs[m.TeamBID] {
			continue
		}
		key := [2]uint{min(m.TeamAID, m.TeamBID), max(m.TeamAID, m.TeamBID)}
		playedPairs[key] = true
	}

	// Soft-block last week (to prevent back-to-back repeats when repeats are unavoidable)
	recentPairs := make(map[[2]uint]bool)
	if week > 1 {
		var lastWeek []SimpleMatch
		lastWeekPatternAny := fmt.Sprintf("%%Week%d-%%", week-1)
		lastWeekPatternDashed := fmt.Sprintf("%%-Week%d-%%", week-1)
		DB.Table("matches").
			Select("team_a_id, team_b_id").
			Where("season = ?", currentSeason).
			Where("COALESCE(is_finals, false) = false").
			Where("match_code NOT ILIKE ?", "%-CHAL-%").
			Where(`(
				(NULLIF(week, '') IS NOT NULL AND CAST(NULLIF(week, '') AS INTEGER) = ?)
				OR match_code ILIKE ?
				OR match_code ILIKE ?
			)`, week-1, lastWeekPatternAny, lastWeekPatternDashed).
			Find(&lastWeek)
		for _, m := range lastWeek {
			if m.TeamAID == 0 || m.TeamBID == 0 {
				continue
			}
			key := [2]uint{min(m.TeamAID, m.TeamBID), max(m.TeamAID, m.TeamBID)}
			recentPairs[key] = true
		}
	}

	// Build all possible pairs among active teams
	var allPairs [][2]uint
	for i := 0; i < len(teams); i++ {
		for j := i + 1; j < len(teams); j++ {
			allPairs = append(allPairs, [2]uint{teams[i].ID, teams[j].ID})
		}
	}

	// Prefer unused pairs this season
	var unusedPairs [][2]uint
	for _, pair := range allPairs {
		key := [2]uint{min(pair[0], pair[1]), max(pair[0], pair[1])}
		if !playedPairs[key] {
			unusedPairs = append(unusedPairs, key)
		}
	}

	// Deterministic seed
	seed := int64(week)*12345 + int64(len(teams))*999

	// Build a schedule that targets exactly 2 matches per team.
	// Preference order:
	//  1) pairs not yet played this season
	//  2) avoid repeating last week (if possible)
	//  3) allow repeats only when needed to fill everyone to 2
	buildSchedule := func(avoidRecent bool, disallowPlayed bool) ([][]uint, map[uint]int, bool) {
		adj := make(map[uint][]uint, len(teams))
		for _, p := range allPairs {
			a, b := p[0], p[1]
			key := [2]uint{min(a, b), max(a, b)}
			if disallowPlayed && playedPairs[key] {
				continue
			}
			if avoidRecent && recentPairs[key] {
				continue
			}
			adj[a] = append(adj[a], b)
			adj[b] = append(adj[b], a)
		}

		// Shuffle adjacency deterministically per team (stable but non-biased)
		for _, t := range teams {
			list := adj[t.ID]
			if len(list) > 1 {
				shuffle(list, seed+int64(t.ID)*777)
				adj[t.ID] = list
			}
		}

		matchCount := make(map[uint]int, len(teams))
		used := make(map[[2]uint]bool)
		matchups := make([][]uint, 0, len(teams))

		progress := true
		for progress {
			progress = false

			// Greedy by need: fill the teams with the fewest matches first
			ordered := make([]uint, 0, len(teams))
			for _, t := range teams {
				ordered = append(ordered, t.ID)
			}
			shuffle(ordered, seed+int64(len(matchups))*13)
			// bubble-ish pass to bring low counts forward (small N)
			for i := 0; i < len(ordered); i++ {
				for j := i + 1; j < len(ordered); j++ {
					if matchCount[ordered[j]] < matchCount[ordered[i]] {
						ordered[i], ordered[j] = ordered[j], ordered[i]
					}
				}
			}

			for _, a := range ordered {
				if matchCount[a] >= 2 {
					continue
				}

				bestB := uint(0)
				bestScore := 1 << 30
				for _, b := range adj[a] {
					if a == b || matchCount[b] >= 2 {
						continue
					}
					key := [2]uint{min(a, b), max(a, b)}
					if used[key] {
						continue
					}

					score := matchCount[b]
					// Prefer unplayed pairs even when repeats are allowed
					if playedPairs[key] {
						score += 10
					}

					if score < bestScore {
						bestScore = score
						bestB = b
						if score == 0 {
							break
						}
					}
				}

				if bestB != 0 {
					key := [2]uint{min(a, bestB), max(a, bestB)}
					used[key] = true
					matchups = append(matchups, []uint{a, bestB})
					matchCount[a]++
					matchCount[bestB]++
					progress = true
				}
			}
		}

		complete := true
		for _, t := range teams {
			if matchCount[t.ID] != 2 {
				complete = false
				break
			}
		}
		return matchups, matchCount, complete
	}

	// Phase 1: strict round-robin (no repeats at all)
	phase1, _, ok := buildSchedule(true, true)
	matchupsU := phase1

	// Phase 2: allow repeats if needed, but avoid last-week repeats
	if !ok {
		phase2, _, ok2 := buildSchedule(true, false)
		matchupsU = phase2
		ok = ok2
	}

	// Phase 3: last resort, allow repeats including last week
	if !ok {
		phase3, _, _ := buildSchedule(false, false)
		matchupsU = phase3
	}

	// Convert back to [2]uint format for existing code paths
	matchups := make([][2]uint, 0, len(matchupsU))
	for _, p := range matchupsU {
		if len(p) != 2 {
			continue
		}
		matchups = append(matchups, [2]uint{p[0], p[1]})
	}

	// Prepare preview output
	type Preview struct {
		MatchCode string `json:"match_code"`
		TeamA     string `json:"team_a"`
		TeamB     string `json:"team_b"`
	}
	var results []Preview
	for i, m := range matchups {
		var teamA, teamB Team
		DB.First(&teamA, m[0])
		DB.First(&teamB, m[1])
		results = append(results, Preview{
			MatchCode: fmt.Sprintf("%s-Week%d-M%03d", currentSeason, week, i+1),
			TeamA:     teamA.Name,
			TeamB:     teamB.Name,
		})
	}

	respondJSON(w, map[string]any{
		"success": true,
		"season":  currentSeason,
		"week":    week,
		"seed":    seed,
		"matches": results,
	})
}

// --- POST /api/mod/matches/clear-week ---
// Deletes all matches (and their scores) for the given week, based on match_code prefix (ignores season column).
func HandleModClearWeek(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		Week string `json:"week"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Week == "" {
		http.Error(w, "invalid request: missing week", http.StatusBadRequest)
		return
	}

	targetSeason := strings.TrimSpace(currentSeason)
	if targetSeason == "" {
		http.Error(w, "CURRENT_SEASON not configured", http.StatusInternalServerError)
		return
	}

	// ✅ Just rely on match_code pattern: e.g. 1-Week4-M###
	matchPattern := fmt.Sprintf("%s-Week%s-%%", targetSeason, req.Week)
	log.Printf("🔍 Searching matches using match_code ILIKE '%s'", matchPattern)

	var matches []Match
	if err := DB.Raw(`
		SELECT * FROM matches
		WHERE match_code ILIKE ?
	`, matchPattern).Scan(&matches).Error; err != nil {
		log.Printf("❌ DB error: %v", err)
		http.Error(w, "failed to query matches", http.StatusInternalServerError)
		return
	}

	if len(matches) == 0 {
		log.Printf("⚠️ No matches found for pattern %s", matchPattern)
		respondJSON(w, map[string]any{
			"success": false,
			"message": fmt.Sprintf("No matches found for Week %s (pattern %s)", req.Week, matchPattern),
		})
		return
	}

	var ids []uint
	for _, m := range matches {
		ids = append(ids, m.ID)
	}
	log.Printf("✅ Found %d matches for Week %s (IDs: %v)", len(ids), req.Week, ids)

	// Step 1️⃣ Delete dependent match_scores first (FK safe)
	if err := DB.Exec(`DELETE FROM match_scores WHERE match_id = ANY($1)`, pq.Array(ids)).Error; err != nil {
		log.Printf("❌ Failed to delete match_scores: %v", err)
		http.Error(w, "failed to delete match scores", http.StatusInternalServerError)
		return
	}

	// Step 2️⃣ Delete matches themselves
	result := DB.Exec(`DELETE FROM matches WHERE id = ANY($1)`, pq.Array(ids))
	if result.Error != nil {
		log.Printf("❌ Failed to delete matches: %v", result.Error)
		http.Error(w, "failed to delete matches", http.StatusInternalServerError)
		return
	}

	log.Printf("🗑️ Deleted %d matches for Week %s (IDs: %v)", result.RowsAffected, req.Week, ids)

	respondJSON(w, map[string]any{
		"success": true,
		"deleted": result.RowsAffected,
		"week":    req.Week,
		"message": fmt.Sprintf("Cleared %d matches for Week %s", result.RowsAffected, req.Week),
	})
}

// POST /api/mod/match/add
func HandleModAddMatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		TeamA string `json:"team_a"`
		TeamB string `json:"team_b"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.TeamA == "" || req.TeamB == "" || strings.EqualFold(req.TeamA, req.TeamB) {
		http.Error(w, "invalid team selection", http.StatusBadRequest)
		return
	}

	var aTeam, bTeam Team
	if err := DB.Where("LOWER(name)=LOWER(?)", req.TeamA).First(&aTeam).Error; err != nil {
		http.Error(w, "Team A not found", http.StatusBadRequest)
		return
	}
	if err := DB.Where("LOWER(name)=LOWER(?)", req.TeamB).First(&bTeam).Error; err != nil {
		http.Error(w, "Team B not found", http.StatusBadRequest)
		return
	}

	// Find current active week (highest numeric week)
	var lastMatch Match
	DB.Where("season = ?", currentSeason).Order("CAST(week AS INTEGER) DESC").First(&lastMatch)
	currentWeek := lastMatch.Week
	if currentWeek == "" {
		currentWeek = "1"
	}

	var count int64
	DB.Model(&Match{}).
		Where("season = ? AND week = ?", currentSeason, currentWeek).
		Count(&count)

	matchCode := fmt.Sprintf("%s-Week%s-M%03d", currentSeason, currentWeek, count+1)
	now := time.Now()

	newMatch := Match{
		MatchCode:    matchCode,
		TeamAID:      aTeam.ID,
		TeamBID:      bTeam.ID,
		ProposedDate: &now,
		Status:       "Scheduled",
		Season:       currentSeason,
		Week:         currentWeek,
	}

	if err := DB.Create(&newMatch).Error; err != nil {
		http.Error(w, "failed to create match", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success":    true,
		"match_code": matchCode,
		"match_id":   newMatch.ID,
		"week":       currentWeek,
		"message":    fmt.Sprintf("Added %s: %s vs %s", matchCode, aTeam.Name, bTeam.Name),
	})
}

// --- Moderator: Set map scores manually ---
func HandleModSetMaps(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		MatchID int              `json:"match_id"`
		Maps    []map[string]any `json:"maps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.MatchID == 0 || len(req.Maps) == 0 {
		http.Error(w, "Missing match_id or maps", http.StatusBadRequest)
		return
	}

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// Normalize and filter out empty maps
	filtered := make([]map[string]any, 0)
	for _, m := range req.Maps {
		a := int(toFloat(m["team_a_score"]))
		b := int(toFloat(m["team_b_score"]))

		// ✅ Safe mode extraction
		var mode string
		if raw, ok := m["mode"]; ok && raw != nil {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				mode = strings.TrimSpace(s)
			} else {
				mode = "Unknown"
			}
		} else {
			mode = "Unknown"
		}

		if a == 0 && b == 0 {
			continue
		}

		filtered = append(filtered, map[string]any{
			"map":          int(toFloat(m["map"])),
			"mode":         mode,
			"team_a_score": a,
			"team_b_score": b,
		})
	}

	// Serialize JSONB
	jsonBytes, err := json.Marshal(filtered)
	if err != nil {
		http.Error(w, "Failed to encode map scores", http.StatusInternalServerError)
		return
	}

	// Save to match
	if err := DB.Model(&match).Update("map_scores", datatypes.JSON(jsonBytes)).Error; err != nil {
		http.Error(w, "Failed to update match scores", http.StatusInternalServerError)
		return
	}

	// Update overall winner + status
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	totalA := 0
	totalB := 0
	for _, m := range filtered {
		if int(toFloat(m["team_a_score"])) > int(toFloat(m["team_b_score"])) {
			totalA++
		} else if int(toFloat(m["team_b_score"])) > int(toFloat(m["team_a_score"])) {
			totalB++
		}
	}

	if totalA > totalB {
		match.WinnerID = &teamA.ID
		match.LoserID = &teamB.ID
	} else if totalB > totalA {
		match.WinnerID = &teamB.ID
		match.LoserID = &teamA.ID
	}
	match.Status = "Finished"

	DB.Save(&match)

	respondJSON(w, map[string]any{
		"ok":         true,
		"match_id":   match.ID,
		"map_scores": filtered,
		"winner":     match.WinnerID,
	})
}
