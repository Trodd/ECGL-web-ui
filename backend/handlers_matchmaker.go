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
)

// --- Utility: shuffle slice ---
func shuffle[T any](slice []T) {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(slice), func(i, j int) { slice[i], slice[j] = slice[j], slice[i] })
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
	// 🔒 Require League Mod access first
	if _, ok := requireLeagueMod(w, r); !ok {
		return // stops unauthorized users
	}

	type Req struct {
		Week int `json:"week"`
	}
	var req Req
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Week <= 0 {
		http.Error(w, "Invalid week number", http.StatusBadRequest)
		return
	}

	// Step 1: Load active teams
	var teams []Team
	if err := DB.Where("status = ?", "Active").Find(&teams).Error; err != nil {
		http.Error(w, "Failed to load teams", http.StatusInternalServerError)
		return
	}
	if len(teams) < 2 {
		http.Error(w, "Not enough teams to generate matchups", http.StatusBadRequest)
		return
	}

	// Step 2: Load previous week's matches (avoid immediate rematches)
	type SimpleMatch struct {
		TeamAID uint
		TeamBID uint
	}
	var previous []SimpleMatch
	DB.Table("matches").
		Where("match_code LIKE ?", "Week"+strconv.Itoa(req.Week-1)+"%").
		Find(&previous)

	recentPairs := make(map[[2]uint]bool)
	for _, m := range previous {
		key := [2]uint{min(m.TeamAID, m.TeamBID), max(m.TeamAID, m.TeamBID)}
		recentPairs[key] = true
	}

	// Step 3: Create all possible team pairs excluding recent rematches
	var allPairs [][2]uint
	for i := 0; i < len(teams); i++ {
		for j := i + 1; j < len(teams); j++ {
			key := [2]uint{teams[i].ID, teams[j].ID}
			if !recentPairs[[2]uint{min(key[0], key[1]), max(key[0], key[1])}] {
				allPairs = append(allPairs, key)
			}
		}
	}
	shuffle(allPairs)

	// Step 4: Assign up to 2 matches per team
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

	// Step 5: Fallback fill for under-matched teams
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

	// Step 6: Insert new matches into DB
	now := time.Now()
	for i, m := range matchups {
		matchCode := fmt.Sprintf("%s-Week%d-M%03d", currentSeason, req.Week, i+1)
		systemID := int64(0)
		newMatch := Match{
			MatchCode:     matchCode,
			TeamAID:       m[0],
			TeamBID:       m[1],
			ProposedDate:  &now,
			ScheduledDate: nil,
			Status:        "Scheduled",
			ProposerID:    &systemID,
		}

		if err := DB.Create(&newMatch).Error; err != nil {
			log.Printf("❌ Failed to insert match %s: %v", matchCode, err)
		} else {
			log.Printf("✅ Created %s → TeamA %d vs TeamB %d", matchCode, m[0], m[1])
		}
	}

	// Step 7: Respond with summary
	summary := make([]map[string]any, 0, len(matchups))
	for _, m := range matchups {
		summary = append(summary, map[string]any{
			"team_a": m[0],
			"team_b": m[1],
		})
	}

	respondJSON(w, map[string]any{
		"success": true,
		"season":  currentSeason,
		"week":    req.Week,
		"matches": summary,
	})
}

// --- GET /api/mod/matches/preview?week=2 ---
// Generates matchups preview without saving to DB
func HandlePreviewWeeklyMatches(w http.ResponseWriter, r *http.Request) {
	// 🔒 Require League Mod access first
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	// Parse week number
	weekStr := r.URL.Query().Get("week")
	week, err := strconv.Atoi(weekStr)
	if err != nil || week <= 0 {
		http.Error(w, "Invalid week number", http.StatusBadRequest)
		return
	}

	// --- Step 0: Prevent duplicate week generation ---
	var existingCount int64
	DB.Model(&Match{}).
		Where("match_code LIKE ?", fmt.Sprintf("%%-Week%d-%%", week)).
		Count(&existingCount)
	if existingCount > 0 {
		respondJSON(w, map[string]any{
			"success": false,
			"error":   fmt.Sprintf("Week %d already has %d matches — generation aborted", week, existingCount),
		})
		return
	}

	// Step 1: Load active teams
	var teams []Team
	if err := DB.Where("status = ?", "Active").Find(&teams).Error; err != nil {
		http.Error(w, "Failed to load teams", http.StatusInternalServerError)
		return
	}

	if len(teams) < 2 {
		http.Error(w, "Not enough teams to generate preview", http.StatusBadRequest)
		return
	}

	// Step 2: Load previous week's matches (avoid rematches)
	type SimpleMatch struct {
		TeamAID uint
		TeamBID uint
	}
	var previous []SimpleMatch
	DB.Table("matches").
		Where("match_code LIKE ?", "Week"+strconv.Itoa(week-1)+"%").
		Find(&previous)

	recentPairs := make(map[[2]uint]bool)
	for _, m := range previous {
		key := [2]uint{min(m.TeamAID, m.TeamBID), max(m.TeamAID, m.TeamBID)}
		recentPairs[key] = true
	}

	// Step 3: Build potential pairs excluding recent rematches
	var allPairs [][2]uint
	for i := 0; i < len(teams); i++ {
		for j := i + 1; j < len(teams); j++ {
			key := [2]uint{teams[i].ID, teams[j].ID}
			if !recentPairs[[2]uint{min(key[0], key[1]), max(key[0], key[1])}] {
				allPairs = append(allPairs, key)
			}
		}
	}
	shuffle(allPairs)

	// Step 4: Assign up to 2 matches per team (same logic as live)
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

	// Step 5: Fallback fill for under-matched teams
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

	// Step 6: Prepare readable summary (no DB writes)
	type Preview struct {
		MatchCode string `json:"match_code"`
		TeamA     string `json:"team_a"`
		TeamB     string `json:"team_b"`
	}
	var results []Preview
	for i, m := range matchups {
		// Fetch team names
		var teamA, teamB Team
		DB.First(&teamA, m[0])
		DB.First(&teamB, m[1])

		results = append(results, Preview{
			MatchCode: fmt.Sprintf("%s-Week%d-M%03d", currentSeason, week, i+1),
			TeamA:     teamA.Name,
			TeamB:     teamB.Name,
		})
	}

	// Step 7: Send preview (no DB insert)
	respondJSON(w, map[string]any{
		"success": true,
		"season":  currentSeason,
		"week":    week,
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
