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

	// --- Step 1.5: Auto double-forfeit unfinished matches for involved teams ---
	var teamNames []string
	for _, m := range req.Matches {
		teamNames = append(teamNames, m.TeamA, m.TeamB)
	}

	// Find all matching teams
	var involvedTeams []Team
	if err := DB.Where("name IN ?", teamNames).Find(&involvedTeams).Error; err != nil {
		log.Printf("❌ Failed to load teams for auto-forfeit cleanup: %v", err)
	} else {
		for _, team := range involvedTeams {
			var oldMatches []Match
			// any active match not already completed/forfeit/cancelled
			if err := DB.
				Where("(team_a_id = ? OR team_b_id = ?) AND status NOT IN ?", team.ID, team.ID,
					[]string{"Completed", "Forfeit", "Cancelled"}).
				Find(&oldMatches).Error; err != nil {
				log.Printf("⚠️ Could not load old matches for team %s: %v", team.Name, err)
				continue
			}

			for _, old := range oldMatches {
				// Double-forfeit: both teams lose
				old.Status = "Forfeit"
				old.WinnerID = nil
				old.LoserID = nil

				// Clear any map scores
				_ = DB.Where("match_id = ?", old.ID).Delete(&MatchScore{}).Error

				if err := DB.Save(&old).Error; err != nil {
					log.Printf("⚠️ Failed to mark match #%d as forfeit: %v", old.ID, err)
				} else {
					log.Printf("🏳️ Auto double-forfeited unfinished match #%d: %d vs %d",
						old.ID, old.TeamAID, old.TeamBID)
				}
			}
		}
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

	weekStr := r.URL.Query().Get("week")
	week, err := strconv.Atoi(weekStr)
	if err != nil || week <= 0 {
		http.Error(w, "Invalid week number", http.StatusBadRequest)
		return
	}

	// Prevent duplicate generation
	var existingCount int64
	DB.Model(&Match{}).
		Where("match_code LIKE ?", fmt.Sprintf("%%-Week%d-%%", week)).
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
	// Avoid rematches within the last 3 weeks
	var previous []SimpleMatch
	DB.Table("matches").
		Where("season = ? AND CAST(week AS INTEGER) >= ?", currentSeason, week-3).
		Find(&previous)
	recentPairs := make(map[[2]uint]bool)
	for _, m := range previous {
		key := [2]uint{min(m.TeamAID, m.TeamBID), max(m.TeamAID, m.TeamBID)}
		recentPairs[key] = true
	}

	// Build all possible pairs (only active teams)
	var allPairs [][2]uint
	for i := 0; i < len(teams); i++ {
		for j := i + 1; j < len(teams); j++ {
			key := [2]uint{teams[i].ID, teams[j].ID}
			if !recentPairs[[2]uint{min(key[0], key[1]), max(key[0], key[1])}] {
				allPairs = append(allPairs, key)
			}
		}
	}

	// Deterministic shuffle
	seed := int64(week)*12345 + int64(len(teams))*999
	shuffle(allPairs, seed)

	// Assign matches (each team max 2)
	matchCount := make(map[uint]int)
	var matchups [][2]uint
	for _, pair := range allPairs {
		a, b := pair[0], pair[1]
		if matchCount[a] < 2 && matchCount[b] < 2 {
			matchups = append(matchups, [2]uint{a, b})
			matchCount[a]++
			matchCount[b]++
		}
	}

	// Fill under-matched teams
	for _, tA := range teams {
		if matchCount[tA.ID] >= 2 {
			continue
		}
		for _, tB := range teams {
			if tA.ID == tB.ID || matchCount[tB.ID] >= 2 {
				continue
			}
			key := [2]uint{min(tA.ID, tB.ID), max(tA.ID, tB.ID)}
			if !pairExists(matchups, key) {
				matchups = append(matchups, key)
				matchCount[tA.ID]++
				matchCount[tB.ID]++
				break
			}
		}
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
