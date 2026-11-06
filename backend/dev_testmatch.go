package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ensureTestTeam returns the team with this name, creating it as a "Test" team
// (hidden from team lists, no members, not joinable) when it doesn't exist.
func ensureTestTeam(league, name string) (Team, error) {
	name = strings.TrimSpace(name)

	var t Team
	if err := DB.Where("LOWER(name) = LOWER(?)", name).First(&t).Error; err == nil {
		return t, nil
	}

	t = Team{
		League:          normalizeLeague(league),
		Name:            name,
		Status:          "Test",
		Rating:          getEnvInt("DEFAULT_TEAM_RATING", 1000),
		AllowChallenges: false,
		JoinAllowed:     false,
	}
	err := DB.Create(&t).Error
	return t, err
}

// POST /api/dev/test-match
//
// Dev-only helper for creating a throwaway match so casting, scoring and match
// pages can be tested without touching a real fixture. Matches are coded
// "<season>-Week<week>-T###" so they're easy to spot and delete afterwards.
//
//	Body: {
//	  team_a_id, team_b_id,
//	  scheduled_date?: "YYYY-MM-DDTHH:MM" | RFC3339,
//	  confirm?: bool (default true — marks both teams schedule-confirmed),
//	  week?: int (default: the league's current week)
//	}
func HandleDevCreateTestMatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireDev(w, r); !ok {
		return
	}

	var req struct {
		TeamAID   uint   `json:"team_a_id"`
		TeamBID   uint   `json:"team_b_id"`
		TeamAName string `json:"team_a_name"`
		TeamBName string `json:"team_b_name"`
		League    string `json:"league"`
		Date      string `json:"scheduled_date"`
		Confirm   *bool  `json:"confirm"`
		Week      int    `json:"week"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var teamA, teamB Team

	nameA := strings.TrimSpace(req.TeamAName)
	nameB := strings.TrimSpace(req.TeamBName)

	if nameA != "" || nameB != "" {
		// Made-up teams: created on demand with Status "Test" so real players
		// and team lists are never touched.
		if nameA == "" || nameB == "" {
			http.Error(w, "Give both test teams a name", http.StatusBadRequest)
			return
		}
		if strings.EqualFold(nameA, nameB) {
			http.Error(w, "Test team names must differ", http.StatusBadRequest)
			return
		}

		lg := normalizeLeague(req.League)
		var err error
		if teamA, err = ensureTestTeam(lg, nameA); err != nil {
			log.Printf("❌ ensureTestTeam(%q): %v", nameA, err)
			http.Error(w, "Failed to create test team", http.StatusInternalServerError)
			return
		}
		if teamB, err = ensureTestTeam(lg, nameB); err != nil {
			log.Printf("❌ ensureTestTeam(%q): %v", nameB, err)
			http.Error(w, "Failed to create test team", http.StatusInternalServerError)
			return
		}
	} else {
		if req.TeamAID == 0 || req.TeamBID == 0 || req.TeamAID == req.TeamBID {
			http.Error(w, "Pick two different teams", http.StatusBadRequest)
			return
		}

		if err := DB.First(&teamA, req.TeamAID).Error; err != nil {
			http.Error(w, "Team A not found", http.StatusNotFound)
			return
		}
		if err := DB.First(&teamB, req.TeamBID).Error; err != nil {
			http.Error(w, "Team B not found", http.StatusNotFound)
			return
		}
	}

	lg := normalizeLeague(teamA.League)
	if normalizeLeague(teamB.League) != lg {
		http.Error(w, "Both teams must be in the same league", http.StatusBadRequest)
		return
	}

	season := seasonForLeague(lg)
	if strings.TrimSpace(season) == "" {
		season = "0"
	}

	week := req.Week
	if week <= 0 {
		week = leagueWeek(lg)
	}

	var count int64
	DB.Model(&Match{}).
		Where("league = ? AND season = ? AND week = ?", lg, season, strconv.Itoa(week)).
		Count(&count)

	now := time.Now()

	var scheduled *time.Time
	if strings.TrimSpace(req.Date) != "" {
		if t, err := time.Parse("2006-01-02T15:04", req.Date); err == nil {
			scheduled = &t
		} else if t, err := time.Parse(time.RFC3339, req.Date); err == nil {
			scheduled = &t
		}
	}
	if scheduled == nil {
		scheduled = &now
	}

	confirm := true
	if req.Confirm != nil {
		confirm = *req.Confirm
	}

	matchCode := fmt.Sprintf("%s-Week%d-T%03d", season, week, count+1)

	m := Match{
		League:                 lg,
		MatchCode:              matchCode,
		TeamAID:                teamA.ID,
		TeamBID:                teamB.ID,
		ProposedDate:           &now,
		ScheduledDate:          scheduled,
		Status:                 "Scheduled",
		Season:                 season,
		Week:                   strconv.Itoa(week),
		TeamAScheduleConfirmed: confirm,
		TeamBScheduleConfirmed: confirm,
	}

	if err := DB.Create(&m).Error; err != nil {
		log.Printf("❌ HandleDevCreateTestMatch: %v", err)
		http.Error(w, "Failed to create test match", http.StatusInternalServerError)
		return
	}

	log.Printf("🧪 [DEV] test match %s created: %s vs %s", matchCode, teamA.Name, teamB.Name)

	respondJSON(w, map[string]any{
		"success":    true,
		"match_id":   m.ID,
		"match_code": m.MatchCode,
		"league":     lg,
		"confirmed":  confirm,
		"message": fmt.Sprintf("Created test match %s: %s vs %s",
			matchCode, teamA.Name, teamB.Name),
	})
}
