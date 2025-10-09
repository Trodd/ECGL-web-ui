package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"
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
