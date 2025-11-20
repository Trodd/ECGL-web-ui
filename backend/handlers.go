package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"gorm.io/gorm"
)

func getEnvInt(key string, def int) int {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return n
}

// --- Require Login ---
func requireLogin(w http.ResponseWriter, r *http.Request) (*sessions.Session, bool) {
	session, _ := store.Get(r, "session")

	// Validate that user info exists
	if _, ok := session.Values["user"].(string); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, false
	}

	return session, true
}

// --- FlexibleID ---
// Safely decode JSON numbers or strings into int64 without float64 precision loss.
type FlexibleID struct {
	value *int64
}

func (f *FlexibleID) UnmarshalJSON(b []byte) error {
	// Handle null
	if string(b) == "null" {
		f.value = nil
		return nil
	}

	// Try string first
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid string ID: %w", err)
		}
		f.value = &v
		return nil
	}

	// Now try integer WITHOUT float64
	var i int64
	if err := json.Unmarshal(b, &i); err == nil {
		f.value = &i
		return nil
	}

	return fmt.Errorf("invalid FlexibleID payload: %s", string(b))
}

func (f *FlexibleID) Int64() int64 {
	if f.value == nil {
		return 0
	}
	return *f.value
}

func respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// contains checks if a uint value exists in a slice of uints.
func contains(slice []uint, val uint) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

// --- GET /api/settings ---
// Provides global configuration and state values for the frontend (roster lock, team size limits, etc.)
func GetSettings(w http.ResponseWriter, r *http.Request) {
	// Load league settings (current week + challenge limit)
	var s LeagueSettings
	if err := DB.First(&s, 1).Error; err != nil {
		// Fallback defaults if table empty or missing row
		s.CurrentWeek = 1
		s.WeeklyChallengeLimit = 1
	}

	// Safe defaults from .env
	minPlayers := getEnvInt("MIN_TEAM_PLAYERS", 3)
	maxPlayers := getEnvInt("MAX_TEAM_PLAYERS", 6)

	respondJSON(w, map[string]any{
		"roster_locked":          rosterLocked,
		"min_team_players":       minPlayers,
		"max_team_players":       maxPlayers,
		"current_week":           s.CurrentWeek,
		"weekly_challenge_limit": s.WeeklyChallengeLimit,
	})
}

func GetGlobalCurrentWeek() int {
	var s LeagueSettings

	// Load the row with ID 1 (the single global settings row)
	err := DB.First(&s, 1).Error
	if err == nil && s.CurrentWeek > 0 {
		return s.CurrentWeek
	}

	// If DB row missing or invalid, fallback to week 1
	return 1
}

// Returns BOTH captain + co-captain pings for a team
func getBothCaptainPings(teamID uint) string {
	var members []TeamMember

	// Find captain AND co-captain for that team
	DB.Where("team_id = ? AND (role = ? OR role = ?)", teamID, "Captain", "Co-Captain").
		Find(&members)

	// Safety fallback
	if len(members) == 0 {
		return "*No captains found*"
	}

	// Build ping string
	p := ""
	for _, m := range members {
		p += fmt.Sprintf("<@%d> ", m.PlayerID)
	}

	return p
}

// Returns ONLY the actual captain — if no captain exists, fallback to co-captain
func getCaptainPing(teamID uint) string {
	var captain TeamMember
	if err := DB.Where("team_id = ? AND role = ?", teamID, "Captain").First(&captain).Error; err == nil {
		return fmt.Sprintf("<@%d>", captain.PlayerID)
	}

	var co TeamMember
	if err := DB.Where("team_id = ? AND role = ?", teamID, "Co-Captain").First(&co).Error; err == nil {
		return fmt.Sprintf("<@%d>", co.PlayerID)
	}

	return "*No captain found*"
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	session, err := store.Get(r, "session")
	if err != nil {
		log.Printf("❌ Failed to get session during logout: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Expire the cookie + clear values
	session.Options.MaxAge = -1
	session.Values = map[interface{}]interface{}{}
	if err := session.Save(r, w); err != nil {
		log.Printf("❌ Failed to save session during logout: %v", err)
		// still continue to redirect even if save fails
	}

	// Redirect back to frontend
	frontend := getEnv("FRONTEND_URL", "https://gigglesquad.mooo.com")
	http.Redirect(w, r, frontend, http.StatusSeeOther)
}

// --- Players ---
func GetPlayers(w http.ResponseWriter, r *http.Request) {
	type raw struct {
		ID          int64
		Username    string
		DisplayName string
		Role        string
		Device      string
		Timezone    string
	}

	roleFilter := strings.TrimSpace(r.URL.Query().Get("role"))

	query := DB.Table("players").
		Select("id, username, display_name, role, device, timezone").
		Where("username <> ''").
		Where("display_name <> ''").
		Where("role IS NOT NULL AND role <> '' AND role <> 'Unregistered'")

	// ✅ Apply role filter if provided (case-insensitive)
	if roleFilter != "" {
		query = query.Where("LOWER(role) = LOWER(?)", roleFilter)
	}

	var rows []raw
	if err := query.Scan(&rows).Error; err != nil {
		// Always return JSON, even on error
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "failed to load registered players",
		})
		return
	}

	players := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		players = append(players, map[string]any{
			"id":           strconv.FormatInt(r.ID, 10),
			"username":     r.Username,
			"display_name": r.DisplayName,
			"role":         r.Role,
			"device":       r.Device,
			"timezone":     r.Timezone,
		})
	}

	// ✅ Always return an array, even if empty
	if players == nil {
		players = []map[string]any{}
	}

	respondJSON(w, players)
}

// --- Teams ---
func GetTeams(w http.ResponseWriter, r *http.Request) {
	var teams []Team
	if err := DB.
		Order("CASE WHEN status = 'Active' THEN 1 WHEN status = 'Inactive' THEN 2 ELSE 3 END").
		Find(&teams).Error; err != nil {
		http.Error(w, "Failed to load teams", http.StatusInternalServerError)
		return
	}
	respondJSON(w, teams)
}

func GetTeam(w http.ResponseWriter, r *http.Request) {
	// 🧱 Crash prevention: guard param parse
	params := mux.Vars(r)
	teamID, err := strconv.Atoi(params["id"])
	if err != nil || teamID <= 0 {
		http.Error(w, "invalid team id", http.StatusBadRequest)
		return
	}

	// 🧱 Crash prevention: find team safely
	var team Team
	if err := DB.First(&team, teamID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "team not found", http.StatusNotFound)
			return
		}
		log.Printf("❌ GetTeam: DB error loading team %d: %v", teamID, err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// --- Load roster (include display_name, fallback to player_history) ---
	type RosterPlayer struct {
		ID          uint   `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Rating      int    `json:"rating"`
	}

	var roster []RosterPlayer

	// 🧩 Try primary live roster
	err = DB.Table("team_members").
		Select("players.id, players.username, players.display_name, team_members.role, players.rating").
		Joins("JOIN players ON players.id = team_members.player_id").
		Where("team_members.team_id = ?", teamID).
		Scan(&roster).Error

	if err != nil {
		log.Printf("❌ GetTeam: roster query failed for team %d: %v", teamID, err)
		roster = []RosterPlayer{}
	}

	// 🧩 Fallback: use player_history if no active roster found
	if len(roster) == 0 {
		DB.Raw(`
			SELECT p.id, p.username, p.display_name, ph.role, p.rating
			FROM player_history ph
			JOIN players p ON p.id = ph.player_id
			WHERE ph.team_id = ? AND ph.season = ?
		`, teamID, currentSeason).Scan(&roster)
	}

	// --- Load match history (include numeric ID and MatchCode) ---
	type MatchRow struct {
		ID         uint       `json:"id"`
		MatchCode  string     `json:"match_code"`
		OpponentID uint       `json:"opponent_id"`
		Opponent   string     `json:"opponent"`
		Date       *time.Time `json:"date"`
		Result     string     `json:"result"`
	}

	var matches []MatchRow

	if err := DB.Raw(`
		SELECT
			m.id,
			m.match_code,
			CASE WHEN m.team_a_id = @id THEN m.team_b_id ELSE m.team_a_id END AS opponent_id,
			CASE WHEN m.team_a_id = @id THEN t2.name ELSE t1.name END AS opponent,
			m.scheduled_date AS date,
			CASE
				WHEN m.winner_id = @id THEN 'Win'
				WHEN m.loser_id = @id THEN 'Loss'
				ELSE 'Pending'
			END AS result
		FROM matches m
		JOIN teams t1 ON m.team_a_id = t1.id
		JOIN teams t2 ON m.team_b_id = t2.id
		WHERE m.team_a_id = @id OR m.team_b_id = @id
		ORDER BY m.scheduled_date DESC NULLS LAST
	`, sql.Named("id", teamID)).Scan(&matches).Error; err != nil {
		log.Printf("❌ GetTeam: matches query failed for team %d: %v", teamID, err)
		matches = []MatchRow{}
	}

	// 🧱 Crash prevention: never return nil arrays
	if roster == nil {
		roster = []RosterPlayer{}
	}
	if matches == nil {
		matches = []MatchRow{}
	}

	respondJSON(w, map[string]any{
		"id":                     team.ID,
		"name":                   team.Name,
		"status":                 team.Status,
		"roster":                 roster,
		"matches":                matches,
		"allow_challenges":       team.AllowChallenges,
		"weekly_challenges_used": team.WeeklyChallengesUsed,
	})
}

// --- Player Leaderboard ---
func GetPlayerLeaderboard(w http.ResponseWriter, r *http.Request) {
	var players []Player

	if err := DB.Order("rating DESC, wins DESC, losses ASC").Find(&players).Error; err != nil {
		http.Error(w, "failed to load player leaderboard", http.StatusInternalServerError)
		return
	}

	// Convert int64 → string
	for i := range players {
		players[i].IDStr = strconv.FormatInt(players[i].ID, 10)
	}

	respondJSON(w, players)
}

// --- Team Leaderboard ---
func GetTeamLeaderboard(w http.ResponseWriter, r *http.Request) {
	type TeamRow struct {
		ID      uint   `json:"id"`
		Name    string `json:"name"`
		Status  string `json:"status"`
		Rating  int    `json:"rating"`
		Wins    int    `json:"wins"`
		Losses  int    `json:"losses"`
		Matches int    `json:"matches"`
	}
	var rows []TeamRow

	if err := DB.Table("teams").
		Select("id, name, status, rating, wins, losses, matches").
		Order("rating DESC").
		Order("wins DESC").
		Order("losses ASC").
		Find(&rows).Error; err != nil {
		http.Error(w, "failed to load team leaderboard", http.StatusInternalServerError)
		return
	}

	respondJSON(w, rows)
}

// --- Matches (includes team names and match_code) ---
func GetMatches(w http.ResponseWriter, r *http.Request) {
	type MatchRow struct {
		ID            uint       `json:"id"`
		MatchCode     string     `json:"match_code"`
		TeamAID       uint       `json:"team_a_id"`
		TeamAName     string     `json:"team_a_name"`
		TeamBID       uint       `json:"team_b_id"`
		TeamBName     string     `json:"team_b_name"`
		ScheduledDate *time.Time `json:"scheduled_date"`
		Status        string     `json:"status"`
		WinnerID      *uint      `json:"winner_id"`
		LoserID       *uint      `json:"loser_id"`
	}

	var matches []MatchRow
	if err := DB.Raw(`
		SELECT 
			m.id,
			m.match_code,
			m.team_a_id,
			t1.name AS team_a_name,
			m.team_b_id,
			t2.name AS team_b_name,
			m.scheduled_date,
			m.status,
			m.winner_id,
			m.loser_id
		FROM matches m
		JOIN teams t1 ON m.team_a_id = t1.id
		JOIN teams t2 ON m.team_b_id = t2.id
		ORDER BY m.created_at DESC
	`).Scan(&matches).Error; err != nil {
		log.Printf("❌ GetMatches: query failed: %v", err)
		http.Error(w, "Failed to load matches", http.StatusInternalServerError)
		return
	}

	respondJSON(w, matches)
}

// --- My Team (session-based) ---
func GetMyTeam(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	discordID, _ := session.Values["discord_id"].(string)

	// DEV MODE override
	if os.Getenv("DEV_MODE") == "true" {
		if overrideID := r.URL.Query().Get("as"); overrideID != "" {
			discordID = overrideID
		}
	}

	if discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	// verify player exists
	var player Player
	if err := DB.First(&player, playerID).Error; err != nil {
		http.Error(w, "player not found", http.StatusNotFound)
		return
	}

	// find their team membership
	var membership TeamMember
	result := DB.Where("player_id = ?", playerID).
		Order("team_id DESC").
		Limit(1).
		Find(&membership)

	if result.RowsAffected == 0 {
		respondJSON(w, map[string]any{
			"team":               nil,
			"roster":             []any{},
			"matches":            []any{},
			"requests":           []any{},
			"challenge_requests": []any{},
			"myRole":             "",
		})
		return
	}

	// load team
	var team Team
	if err := DB.First(&team, membership.TeamID).Error; err != nil {
		http.Error(w, "team not found", http.StatusNotFound)
		return
	}

	// --- Current season ---
	var currentSeason string
	DB.Raw(`SELECT value FROM config WHERE key='current_season' LIMIT 1`).
		Scan(&currentSeason)

	if strings.TrimSpace(currentSeason) == "" {
		currentSeason = "0"
	}
	currentSeason = strings.TrimSpace(strings.Replace(currentSeason, "Season ", "", 1))

	// --- Load matches ---
	type MatchWithMaps struct {
		ID         uint         `json:"id"`
		MatchCode  string       `json:"match_code"`
		Opponent   string       `json:"opponent"`
		Date       *time.Time   `json:"date"`
		Result     string       `json:"result"`
		Status     string       `json:"status"`
		Season     string       `json:"season"`
		TeamAID    uint         `json:"team_a_id"`
		TeamBID    uint         `json:"team_b_id"`
		LeagueSubA *string      `json:"league_sub_a"`
		LeagueSubB *string      `json:"league_sub_b"`
		Maps       []MatchScore `json:"maps"`
	}

	var matches []MatchWithMaps

	rows, err := DB.Raw(`
        SELECT
            m.id, m.match_code,
            CASE WHEN m.team_a_id = @tid THEN t2.name ELSE t1.name END AS opponent,
            m.scheduled_date,
            CASE
                WHEN m.winner_id = @tid THEN 'Win'
                WHEN m.loser_id = @tid THEN 'Loss'
                ELSE 'Pending'
            END AS result,
            m.status,
            m.team_a_id, m.team_b_id
        FROM matches m
        JOIN teams t1 ON m.team_a_id = t1.id
        JOIN teams t2 ON m.team_b_id = t2.id
        WHERE m.team_a_id = @tid OR m.team_b_id = @tid
        ORDER BY m.scheduled_date DESC NULLS LAST
    `, sql.Named("tid", membership.TeamID)).Rows()

	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m MatchWithMaps
			if err := rows.Scan(
				&m.ID, &m.MatchCode, &m.Opponent, &m.Date,
				&m.Result, &m.Status, &m.TeamAID, &m.TeamBID,
			); err != nil {
				continue
			}

			// attach league subs from full Match
			var fullMatch Match
			if err := DB.First(&fullMatch, m.ID).Error; err == nil {
				if fullMatch.LeagueSubA != nil {
					v := strconv.FormatInt(*fullMatch.LeagueSubA, 10)
					m.LeagueSubA = &v
				}
				if fullMatch.LeagueSubB != nil {
					v := strconv.FormatInt(*fullMatch.LeagueSubB, 10)
					m.LeagueSubB = &v
				}
			}

			// extract season
			parts := strings.Split(m.MatchCode, "-")
			if len(parts) > 1 && regexp.MustCompile(`^\d+$`).MatchString(parts[0]) {
				m.Season = parts[0]
			} else {
				m.Season = "0"
			}

			DB.Where("match_id = ?", m.ID).Find(&m.Maps)
			matches = append(matches, m)
		}
	}

	// re-sort active vs past
	var active []MatchWithMaps
	var past []MatchWithMaps

	for _, m := range matches {
		seasonMatches := strings.TrimSpace(m.Season) == currentSeason

		finished := m.Status == "Finished" ||
			m.Status == "Completed" ||
			m.Status == "Cancelled" ||
			m.Status == "Forfeit"

		if seasonMatches && !finished {
			active = append(active, m)
		} else {
			past = append(past, m)
		}
	}

	matches = append(active, past...)

	// --- Load roster ---
	type RosterPlayer struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Rating      int    `json:"rating"`
	}

	var roster []RosterPlayer
	if err := DB.Table("team_members").
		Select(`
                CAST(players.id AS text) AS id,
                players.username,
                players.display_name,
                team_members.role,
                players.rating`).
		Joins("JOIN players ON players.id = team_members.player_id").
		Where("team_members.team_id = ?", team.ID).
		Scan(&roster).Error; err != nil {
		log.Printf("❌ GetMyTeam: roster query failed for team %d: %v", team.ID, err)
		roster = []RosterPlayer{}
	}

	if len(roster) == 0 {
		DB.Raw(`
                SELECT
                    CAST(p.id AS text) AS id,
                    p.username,
                    p.display_name,
                    ph.role,
                    p.rating
                FROM player_history ph
                JOIN players p ON p.id = ph.player_id
                WHERE ph.team_id = ? AND ph.season = ?
            `, team.ID, currentSeason).Scan(&roster)
	}

	if roster == nil {
		roster = []RosterPlayer{}
	}

	// --- Load Join Requests ---
	var joinRequests []map[string]any
	var challengeRequests []map[string]any
	errReq := DB.Raw(`
        SELECT r.id, r.player_id,
               p.username,
               COALESCE(p.display_name, '') AS display_name,
               r.status
        FROM team_join_requests r
        JOIN players p ON p.id = r.player_id
        WHERE r.team_id = ? AND r.status = 'pending'
        ORDER BY r.id ASC
    `, team.ID).Scan(&joinRequests).Error

	if errReq != nil || joinRequests == nil {
		joinRequests = []map[string]any{}
	}

	// --- NEW: Load Challenge Requests ---
	var challengeRows []ChallengeRequest

	DB.Where("target_team_id = ? AND status = 'Pending'", team.ID).
		Find(&challengeRows)

	// IMPORTANT — assign, do NOT redeclare
	challengeRequests = []map[string]any{}

	for _, ch := range challengeRows {
		var requesterTeam Team
		DB.First(&requesterTeam, ch.RequesterTeamID)

		challengeRequests = append(challengeRequests, map[string]any{
			"id":                  ch.ID,
			"requester_team_id":   ch.RequesterTeamID,
			"requester_team_name": requesterTeam.Name,
			"week":                ch.Week,
			"status":              ch.Status,
		})
	}

	// --- final response ---
	respondJSON(w, map[string]any{
		"team": map[string]any{
			"id":                     team.ID,
			"name":                   team.Name,
			"status":                 team.Status,
			"join_allowed":           team.JoinAllowed,
			"allow_challenges":       team.AllowChallenges,
			"weekly_challenges_used": team.WeeklyChallengesUsed,
		},
		"roster":             roster,
		"matches":            matches,
		"requests":           joinRequests,
		"challenge_requests": challengeRequests,
		"myRole":             membership.Role,
	})
}

// --- Request to Join Team ---
func handleRequestJoinTeam(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID uint `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// ✅ Always use session for requester
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	// ensure player exists
	var player Player
	if err := DB.First(&player, playerID).Error; err != nil {
		http.Error(w, "Player not found", http.StatusNotFound)
		return
	}

	// 🚫 Block banned players
	if strings.EqualFold(player.Role, "Banned") {
		http.Error(w, "Your account is banned from joining teams.", http.StatusForbidden)
		return
	}

	// ensure team exists
	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// 🚫 Prevent join if roster globally locked
	var rosterLocked bool
	if err := DB.Raw("SELECT roster_locked FROM settings WHERE id = 1").Scan(&rosterLocked).Error; err == nil && rosterLocked {
		http.Error(w, "Roster lock is active — joining teams is disabled.", http.StatusForbidden)
		return
	}

	// 🚫 Prevent joining if player already belongs to any team
	var membership TeamMember
	if err := DB.Where("player_id = ?", playerID).Take(&membership).Error; err == nil {
		http.Error(w, "You are already on a team. Leave your current team before joining another.", http.StatusForbidden)
		return
	}

	// 🚫 Prevent join requests if disabled
	if !team.JoinAllowed {
		http.Error(w, "This team is not accepting join requests right now.", http.StatusForbidden)
		return
	}

	// 🚫 Prevent duplicate pending requests to same team
	var existingReq TeamJoinRequest
	if err := DB.Where("player_id = ? AND team_id = ? AND status = ?", playerID, req.TeamID, "pending").
		First(&existingReq).Error; err == nil {
		http.Error(w, "Join request already pending", http.StatusBadRequest)
		return
	}

	// 🚫 Prevent join if team is already full
	var count int64
	DB.Model(&TeamMember{}).Where("team_id = ?", team.ID).Count(&count)
	maxPlayers := getEnvInt("MAX_TEAM_PLAYERS", 6)
	if count >= int64(maxPlayers) {
		http.Error(w, fmt.Sprintf("This team already has the maximum of %d players.", maxPlayers), http.StatusForbidden)
		return
	}

	// ✅ Create new join request
	join := TeamJoinRequest{
		PlayerID: playerID,
		TeamID:   req.TeamID,
		Status:   "pending",
	}
	if err := DB.Create(&join).Error; err != nil {
		http.Error(w, "Failed to save join request", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Join request submitted",
	})
}

// --- Create Team ---
func handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	// make sure player exists
	var player Player
	if err := DB.First(&player, playerID).Error; err != nil {
		http.Error(w, "Player not found", http.StatusNotFound)
		return
	}

	// 🚫 Prevent banned players from creating a team
	if strings.EqualFold(player.Role, "Banned") {
		http.Error(w, "Your account is banned from creating teams.", http.StatusForbidden)
		return
	}

	// 🚫 Prevent team creation when global roster lock is active
	var rosterLocked bool
	if err := DB.Raw("SELECT roster_locked FROM settings WHERE id = 1").Scan(&rosterLocked).Error; err == nil && rosterLocked {
		http.Error(w, "Roster lock is active — team creation disabled", http.StatusForbidden)
		return
	}

	// 🚫 Prevent creating a team if already on one
	var existingMembership TeamMember
	if err := DB.Where("player_id = ?", playerID).First(&existingMembership).Error; err == nil {
		http.Error(w, "You are already on a team. Leave your current team first.", http.StatusForbidden)
		return
	}

	// ✅ Create team with default ELO from .env
	team := Team{
		Name:    req.Name,
		Status:  "Active",
		Rating:  getEnvInt("DEFAULT_TEAM_RATING", 1000),
		Wins:    0,
		Losses:  0,
		Matches: 0,
	}
	if err := DB.Create(&team).Error; err != nil {
		http.Error(w, "Failed to create team", http.StatusInternalServerError)
		return
	}

	// add creator as captain
	captain := TeamMember{
		TeamID:   team.ID,
		PlayerID: playerID,
		Role:     "Captain",
	}
	if err := DB.Omit("Player").Create(&captain).Error; err != nil {
		log.Printf("❌ Failed to insert captain for player %d team %d: %v", playerID, team.ID, err)
		http.Error(w, "Failed to create membership", http.StatusInternalServerError)
		return
	}

	// log history
	DB.Create(&PlayerHistory{
		PlayerID: playerID,
		TeamID:   team.ID,
		TeamName: team.Name,
		Role:     "Captain",
		Season:   currentSeason,
	})

	// confirm
	var check TeamMember
	if err := DB.Where("player_id = ? AND team_id = ?", playerID, team.ID).First(&check).Error; err != nil {
		log.Printf("❌ Membership not found after insert: %v", err)
	} else {
		log.Printf("✅ Membership confirmed: player %d in team %d as %s", check.PlayerID, check.TeamID, check.Role)
	}

	SendDiscordLog(
		fmt.Sprintf(
			"🏗️ **Team Created:** **%s** by <@%s>",
			team.Name,
			session.Values["discord_id"].(string),
		),
	)

	respondJSON(w, map[string]any{
		"success": true,
		"team":    team,
		"message": fmt.Sprintf("Team created successfully with base rating %d", team.Rating),
	})
}

func GetTeamJoinRequests(w http.ResponseWriter, r *http.Request) {
	teamIDStr := mux.Vars(r)["teamID"]
	teamID, err := strconv.Atoi(teamIDStr)
	if err != nil {
		http.Error(w, "Invalid team ID", http.StatusBadRequest)
		return
	}

	var requests []TeamJoinRequest
	if err := DB.Preload("Player").
		Where("team_id = ? AND status = ?", teamID, "pending").
		Find(&requests).Error; err != nil {
		http.Error(w, "Failed to load join requests", http.StatusInternalServerError)
		return
	}

	respondJSON(w, requests)
}

func HandleJoinRequestDecision(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID FlexibleID `json:"request_id"`
		Action    string     `json:"action"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var jr TeamJoinRequest
	if err := DB.First(&jr, req.RequestID.Int64()).Error; err != nil {
		http.Error(w, "Join request not found", http.StatusNotFound)
		return
	}

	// 🚫 Prevent accepting if player is already on another team
	if req.Action == "accept" {
		var existing TeamMember
		if err := DB.Where("player_id = ?", jr.PlayerID).First(&existing).Error; err == nil {
			http.Error(w, "Player already belongs to a team. Must leave before joining another.", http.StatusForbidden)
			return
		}
	}

	switch req.Action {
	case "accept":
		// ✅ Add player to team_members safely
		tm := TeamMember{
			PlayerID: int64(jr.PlayerID),
			TeamID:   jr.TeamID,
			Role:     "Member",
		}
		if err := DB.Create(&tm).Error; err != nil {
			log.Printf("❌ Failed to add player %d to team %d: %v", jr.PlayerID, jr.TeamID, err)
			http.Error(w, "Failed to add player to team", http.StatusInternalServerError)
			return
		}
		// 🧠 Fetch player + team names for readable logging
		var p Player
		DB.Select("display_name, username").First(&p, jr.PlayerID)

		var t Team
		DB.Select("name").First(&t, jr.TeamID)

		display := p.DisplayName
		if display == "" {
			display = p.Username
		}
		if display == "" {
			display = fmt.Sprintf("Player#%d", jr.PlayerID)
		}

		teamName := t.Name
		if teamName == "" {
			teamName = fmt.Sprintf("Team#%d", jr.TeamID)
		}

		// ✅ Log readable output
		log.Printf("✅ %s added to %s as Member", display, teamName)

		SendDiscordLog(
			fmt.Sprintf(
				"** <@%d> **has joined team **%s**",
				jr.PlayerID,
				teamName,
			),
		)

		// ✅ Log history (skip duplicates)
		var existing PlayerHistory
		err := DB.Where("player_id = ? AND team_id = ? AND season = ? AND role = ?",
			jr.PlayerID, jr.TeamID, currentSeason, "Member").First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var team Team
			DB.First(&team, jr.TeamID)
			DB.Create(&PlayerHistory{
				PlayerID: int64(jr.PlayerID),
				TeamID:   jr.TeamID,
				TeamName: team.Name,
				Role:     "Member",
				Season:   currentSeason,
			})
		}

		jr.Status = "accepted"

		// 🧹 Cleanup: remove all other pending join requests from this player
		DB.Where("player_id = ? AND status = ?", jr.PlayerID, "pending").
			Where("id <> ?", jr.ID).
			Delete(&TeamJoinRequest{})

	case "deny":
		jr.Status = "denied"

	default:
		http.Error(w, "Invalid action", http.StatusBadRequest)
		return
	}

	if err := DB.Save(&jr).Error; err != nil {
		http.Error(w, "Failed to update join request", http.StatusInternalServerError)
		return
	}

	// ✅ If accepted, return full MyTeam JSON
	if jr.Status == "accepted" {
		var team Team
		if err := DB.First(&team, jr.TeamID).Error; err != nil {
			http.Error(w, "team not found", http.StatusNotFound)
			return
		}

		// load roster
		var roster []struct {
			ID          int64  `json:"id"`
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			Role        string `json:"role"`
			Rating      int    `json:"rating"`
		}
		DB.Table("team_members").
			Select("players.id, players.username, players.display_name, team_members.role, players.rating").
			Joins("JOIN players ON players.id = team_members.player_id").
			Where("team_members.team_id = ?", jr.TeamID).
			Scan(&roster)

		// load matches
		var matches []struct {
			Opponent string `json:"opponent"`
			Date     string `json:"date"`
			Result   string `json:"result"`
		}
		DB.Raw(`
			SELECT 
			  CASE WHEN m.team_a_id = @id THEN t2.name ELSE t1.name END as opponent,
			  COALESCE(m.scheduled_date, m.proposed_date) as date,
			  CASE 
				WHEN m.winner_id = @id THEN 'Win'
				WHEN m.loser_id = @id THEN 'Loss'
				ELSE 'Pending'
			  END as result
			FROM matches m
			JOIN teams t1 ON m.team_a_id = t1.id
			JOIN teams t2 ON m.team_b_id = t2.id
			WHERE m.team_a_id = @id OR m.team_b_id = @id`,
			sql.Named("id", jr.TeamID),
		).Scan(&matches)

		respondJSON(w, map[string]any{
			"team": map[string]any{
				"id":     team.ID,
				"name":   team.Name,
				"status": team.Status,
			},
			"roster":   roster,
			"matches":  matches,
			"requests": []any{}, // no need to send join requests here
			"myRole":   "Member",
		})
		return
	}

	// otherwise (deny), keep old response shape
	respondJSON(w, map[string]any{
		"success": true,
		"status":  jr.Status,
	})
}

func HandleLeaveTeam(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID uint `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// ✅ Player comes from session
	session, _ := store.Get(r, "session")
	discordIDStr, ok := session.Values["discord_id"].(string)
	if !ok || discordIDStr == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	// Load team (for name)
	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// Verify membership
	var member TeamMember
	err := DB.Where("team_id = ? AND player_id = ?", req.TeamID, playerID).First(&member).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("ℹ️ Player %d is not a member of team %d", playerID, req.TeamID)
		http.Error(w, "Not a team member", http.StatusForbidden)
		return
	}

	if err != nil {
		log.Printf("❌ DB error verifying membership: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Save role before leaving
	role := member.Role

	// Remove from team
	if err := DB.Delete(&member).Error; err != nil {
		http.Error(w, "Failed to leave team", http.StatusInternalServerError)
		return
	}

	// Count remaining members
	var remaining []TeamMember
	DB.Where("team_id = ?", req.TeamID).Find(&remaining)

	// If empty, auto-disband team
	if len(remaining) == 0 {

		DB.Model(&Team{}).
			Where("id = ?", req.TeamID).
			Update("status", "Disbanded")

		log.Printf("🏴‍☠️ Team %s (#%d) auto-disbanded (last member left)", team.Name, team.ID)

		// ⭐ DISCORD LOG — Auto Disband
		SendDiscordLog(
			fmt.Sprintf(
				"🗑️ **Team Disbanded:** **%s (#%d)** — last member left",
				team.Name,
				team.ID,
			),
		)

	} else if role == "Captain" {
		// Captain left → promote next person
		var next TeamMember

		if err := DB.Where("team_id = ? AND role = ?", req.TeamID, "Co-Captain").
			First(&next).Error; err == nil {

			DB.Model(&next).Update("role", "Captain")
			log.Printf("👑 Promoted Co-Captain %d to Captain (team %d)", next.PlayerID, req.TeamID)

		} else if err := DB.Where("team_id = ?", req.TeamID).First(&next).Error; err == nil {

			DB.Model(&next).Update("role", "Captain")
			log.Printf("👑 Promoted Member %d to Captain (team %d)", next.PlayerID, req.TeamID)
		}
	}

	// ⭐ DISCORD LOG — Player Left
	SendDiscordLog(
		fmt.Sprintf(
			"🚪 <@%s> has left team **%s (#%d)**",
			discordIDStr,
			team.Name,
			team.ID,
		),
	)

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Left team successfully",
	})
}

// --- Public: Get all matches grouped by season + week ---
func HandlePublicMatches(w http.ResponseWriter, r *http.Request) {
	season := r.URL.Query().Get("season")
	week := r.URL.Query().Get("week")

	type MatchRow struct {
		ID            uint       `json:"id"`
		MatchCode     string     `json:"match_code"`
		TeamAID       uint       `json:"team_a_id"`
		TeamA         string     `json:"team_a"`
		TeamBID       uint       `json:"team_b_id"`
		TeamB         string     `json:"team_b"`
		ScheduledDate *time.Time `json:"scheduled_date"`
		Status        string     `json:"status"`
		WinnerID      *uint      `json:"winner_id"`
		LoserID       *uint      `json:"loser_id"`
	}

	var rows []MatchRow
	if err := DB.Raw(`
        SELECT 
            m.id,
            m.match_code,
            m.team_a_id,
            t1.name AS team_a,
            m.team_b_id,
            t2.name AS team_b,
            m.scheduled_date,
            m.status,
            m.winner_id,
            m.loser_id
        FROM matches m
        JOIN teams t1 ON t1.id = m.team_a_id
        JOIN teams t2 ON t2.id = m.team_b_id
        
        -- 🔥 Proper season/week sorting (DESC = Newest on top)
        ORDER BY 
            CASE 
                WHEN split_part(m.match_code, '-', 1) ~ '^[0-9]+$' 
                THEN CAST(split_part(m.match_code, '-', 1) AS INTEGER)
                ELSE 0
            END DESC,
            CAST(
                split_part(
                    split_part(m.match_code, 'Week', 2),
                    '-', 1
                ) AS INTEGER
            ) DESC,
            m.match_code DESC
    `).Scan(&rows).Error; err != nil {
		log.Printf("❌ HandlePublicMatches query failed: %v", err)
		http.Error(w, "failed to fetch matches", http.StatusInternalServerError)
		return
	}

	// --- Normalize derived Season + Week ---
	type PublicMatch struct {
		ID            uint       `json:"id"`
		MatchCode     string     `json:"match_code"`
		Season        string     `json:"season"`
		Week          string     `json:"week"`
		TeamAID       uint       `json:"team_a_id"`
		TeamA         string     `json:"team_a"`
		TeamBID       uint       `json:"team_b_id"`
		TeamB         string     `json:"team_b"`
		ScheduledDate *time.Time `json:"scheduled_date"`
		Status        string     `json:"status"`
		WinnerID      *uint      `json:"winner_id"`
		LoserID       *uint      `json:"loser_id"`
	}

	var normalized []PublicMatch
	for _, m := range rows {
		seasonLabel, weekLabel := deriveSeasonAndWeek(m.MatchCode)

		if seasonLabel == "" || strings.EqualFold(seasonLabel, "null") {
			seasonLabel = "Preseason"
		}
		if weekLabel == "" {
			weekLabel = "?"
		}

		normalized = append(normalized, PublicMatch{
			ID:            m.ID,
			MatchCode:     m.MatchCode,
			Season:        fmt.Sprintf("%v", seasonLabel),
			Week:          fmt.Sprintf("%v", weekLabel),
			TeamAID:       m.TeamAID,
			TeamA:         m.TeamA,
			TeamBID:       m.TeamBID,
			TeamB:         m.TeamB,
			ScheduledDate: m.ScheduledDate,
			Status:        m.Status,
			WinnerID:      m.WinnerID,
			LoserID:       m.LoserID,
		})
	}

	// Filter by query
	var filtered []PublicMatch
	for _, m := range normalized {
		if (season == "" || strings.EqualFold(m.Season, season)) &&
			(week == "" || m.Week == week) {
			filtered = append(filtered, m)
		}
	}

	// Group by season + week
	grouped := map[string]map[string][]PublicMatch{}
	for _, m := range filtered {
		if m.Season == "" || strings.EqualFold(m.Season, "null") {
			m.Season = "Preseason"
		}
		if _, ok := grouped[m.Season]; !ok {
			grouped[m.Season] = map[string][]PublicMatch{}
		}
		grouped[m.Season][m.Week] = append(grouped[m.Season][m.Week], m)
	}

	respondJSON(w, map[string]any{
		"success": true,
		"matches": grouped,
	})
}

func getMemberRole(teamID uint, playerID int64) (string, error) {
	var member TeamMember
	err := DB.Where("team_id = ? AND player_id = ?", teamID, playerID).First(&member).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Clean, friendly log instead of full SQL spam
		log.Printf("ℹ️ Player %d is not a member of team %d", playerID, teamID)
		return "", nil // no error, just not found
	}

	if err != nil {
		// Actual DB error
		log.Printf("❌ DB error fetching member role for player %d team %d: %v", playerID, teamID, err)
		return "", err
	}

	return member.Role, nil
}

// --- Kick a team member (Captain only) ---
func HandleKickMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID   uint       `json:"team_id"`
		PlayerID FlexibleID `json:"player_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// requester
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	requesterID, _ := strconv.ParseInt(discordID, 10, 64)

	// target ID (keep exactly as it was when it worked)
	playerID := req.PlayerID.Int64() // ✅ always valid int64 now

	// role check
	role, err := getMemberRole(req.TeamID, requesterID)
	if err != nil || role != "Captain" {
		http.Error(w, "Only Captains can kick players", http.StatusForbidden)
		return
	}

	var member TeamMember
	if err := DB.Where("team_id = ? AND player_id = ?", req.TeamID, playerID).
		First(&member).Error; err != nil {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	if member.Role == "Captain" {
		http.Error(w, "Cannot kick the Captain", http.StatusForbidden)
		return
	}

	if err := DB.Delete(&member).Error; err != nil {
		http.Error(w, "Failed to remove member", http.StatusInternalServerError)
		return
	}

	// Fetch team to get its name
	var team Team
	DB.First(&team, req.TeamID)
	DB.Create(&PlayerHistory{
		PlayerID: playerID,
		TeamID:   req.TeamID,
		TeamName: team.Name,
		Role:     "Kicked (" + member.Role + ")",
		Season:   currentSeason,
	})

	// check if team empty
	var remaining []TeamMember
	DB.Where("team_id = ?", req.TeamID).Find(&remaining)
	if len(remaining) == 0 {
		DB.Delete(&Team{}, req.TeamID)

		// 🔔 Discord log for “kicked + disbanded”
		SendDiscordLog(fmt.Sprintf(
			"🦶 **Player Kicked:** <@%d> removed from **%s** — team disbanded (no members left)",
			playerID,
			team.Name,
		))

		respondJSON(w, map[string]any{
			"success": true,
			"message": "Member kicked, team disbanded",
		})
		return
	}

	// 🔔 Discord log for normal kick
	SendDiscordLog(fmt.Sprintf(
		"🦶 **Player Kicked:** <@%d> removed from **%s**",
		playerID,
		team.Name,
	))

	respondJSON(w, map[string]any{"success": true, "message": "Member kicked"})
}

// --- Promote a team member (Captain only) ---
func HandlePromoteMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID   uint       `json:"team_id"`
		PlayerID FlexibleID `json:"player_id"` // ✅ flexible
		Role     string     `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// requester
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	requesterID, _ := strconv.ParseInt(discordID, 10, 64)

	// target (keep same as last working version)
	playerID := req.PlayerID.Int64() // ✅ always valid int64 now

	if req.Role != "Captain" && req.Role != "Co-Captain" {
		http.Error(w, "Invalid role", http.StatusBadRequest)
		return
	}

	role, err := getMemberRole(req.TeamID, requesterID)
	if err != nil || role != "Captain" {
		http.Error(w, "Only Captains can promote players", http.StatusForbidden)
		return
	}

	var member TeamMember
	if err := DB.Where("team_id = ? AND player_id = ?", req.TeamID, playerID).
		First(&member).Error; err != nil {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	if req.Role == "Captain" {
		DB.Model(&TeamMember{}).
			Where("team_id = ? AND role = ?", req.TeamID, "Captain").
			Update("role", "Co-Captain")
	}

	DB.Model(&TeamMember{}).
		Where("team_id = ? AND player_id = ?", req.TeamID, playerID).
		Update("role", req.Role)

	var existing PlayerHistory
	if err := DB.Where("player_id = ? AND team_id = ? AND season = ? AND role = ?",
		playerID, req.TeamID, currentSeason, req.Role).
		First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		DB.Create(&PlayerHistory{
			PlayerID: playerID,
			TeamID:   req.TeamID,
			Role:     req.Role,
			Season:   currentSeason,
		})
	}

	// 🔔 Discord log for promotion
	SendDiscordLog(fmt.Sprintf(
		"⬆️ **Promotion:** <@%d> is now **%s** on **Team #%d**",
		playerID,
		req.Role,
		req.TeamID,
	))

	respondJSON(w, map[string]any{"success": true, "message": "Member promoted to " + req.Role})
}

// --- Captain-only: Update team active/inactive status ---
func HandleToggleTeamStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID uint   `json:"team_id"`
		Status string `json:"status"` // "Active" or "Inactive"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	role, err := getMemberRole(req.TeamID, playerID)
	if err != nil || (role != "Captain" && role != "Co-Captain") {
		http.Error(w, "Only captains can change status", http.StatusForbidden)
		return
	}

	if req.Status != "Active" && req.Status != "Inactive" {
		http.Error(w, "Invalid status", http.StatusBadRequest)
		return
	}

	if err := DB.Model(&Team{}).Where("id = ?", req.TeamID).Update("status", req.Status).Error; err != nil {
		http.Error(w, "Failed to update team status", http.StatusInternalServerError)
		return
	}

	var team Team
	DB.First(&team, req.TeamID)
	SendDiscordLog(
		fmt.Sprintf(
			"🔄 **Team Status Changed:** **%s** → **%s** by <@%s>",
			team.Name,  // team name instead of ID
			req.Status, // new status
			discordID,  // who changed it
		),
	)

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Team status updated",
	})
}

// --- Captain-only: Toggle join request allowance ---
func HandleToggleTeamJoinAllowed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID uint `json:"team_id"`
		Allow  bool `json:"allow"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// --- Validate session ---
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	// --- Check captain/co-captain role ---
	var member TeamMember
	if err := DB.Where("team_id = ? AND player_id = ?", req.TeamID, playerID).First(&member).Error; err != nil {
		http.Error(w, "Not part of the team", http.StatusForbidden)
		return
	}
	if member.Role != "Captain" && member.Role != "Co-Captain" {
		http.Error(w, "Only captains can change join permissions", http.StatusForbidden)
		return
	}

	// --- Update join permission ---
	if err := DB.Model(&Team{}).Where("id = ?", req.TeamID).
		Update("join_allowed", req.Allow).Error; err != nil {
		http.Error(w, "Failed to update join setting", http.StatusInternalServerError)
		return
	}

	var team Team
	DB.First(&team, req.TeamID)
	SendDiscordLog(
		fmt.Sprintf(
			"👥 **Join Requests %s** for **%s** (by <@%s>)",
			map[bool]string{true: "Enabled", false: "Disabled"}[req.Allow],
			team.Name,
			discordID,
		),
	)

	respondJSON(w, map[string]any{
		"success":      true,
		"join_allowed": req.Allow,
		"message": fmt.Sprintf("Join requests %s",
			map[bool]string{true: "enabled", false: "disabled"}[req.Allow]),
	})
}

func SendDiscordEmbedWithPings(content, title, description string) {
	botToken := getEnv("DISCORD_BOT_TOKEN", "")
	channelID := getEnv("DISCORD_LOG_CHANNEL_MATCHES", "")

	if botToken == "" || channelID == "" {
		log.Println("❌ Missing Discord env vars (Embed not sent)")
		return
	}

	body := map[string]any{
		"content": content, // <-- THIS IS WHERE REAL PINGS GO
		"embeds": []any{
			map[string]any{
				"title":       title,
				"description": description,
				"color":       0x3498DB,
			},
		},
	}

	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(
		"POST",
		"https://discord.com/api/v10/channels/"+channelID+"/messages",
		bytes.NewBuffer(b),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("❌ SendDiscordEmbed error:", err)
		return
	}
	resp.Body.Close()
}

// --- POST /api/match/schedule ---
// One team schedules or edits the match time; instantly marks match as Scheduled
func HandleScheduleMatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID uint   `json:"match_id"`
		TeamID  uint   `json:"team_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// --- Load match ---
	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// --- Parse date ---
	date, err := time.Parse(time.RFC3339, req.Date)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	oldDate := ""
	if match.ScheduledDate != nil {
		oldDate = match.ScheduledDate.Format(time.RFC1123)
	}

	// --- Update & confirm ---
	match.ScheduledDate = &date
	match.Status = "Scheduled"
	match.TeamAScheduleConfirmed = true
	match.TeamBScheduleConfirmed = true

	if err := DB.Save(&match).Error; err != nil {
		http.Error(w, "Failed to update match", http.StatusInternalServerError)
		return
	}

	// --- Logging (console only) ---
	if oldDate == "" {
		log.Printf("📅 Match #%d scheduled by Team %d for %s", match.ID, req.TeamID, date.Format(time.RFC1123))
	} else {
		log.Printf("✏️ Match #%d rescheduled by Team %d: %s → %s", match.ID, req.TeamID, oldDate, date.Format(time.RFC1123))
	}

	// =====================================================
	//         🔥 Build Embed for Discord Log
	// =====================================================

	// Fetch teams
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	// Actor
	session, _ := store.Get(r, "session")
	discordIDStr, _ := session.Values["discord_id"].(string)

	// --- Fetch rosters ---
	var rosterA, rosterB []TeamMember
	DB.Where("team_id = ?", match.TeamAID).Find(&rosterA)
	DB.Where("team_id = ?", match.TeamBID).Find(&rosterB)

	// --- Format pings safely ---
	formatPings := func(list []TeamMember) string {
		if len(list) == 0 {
			return "*No players found*"
		}
		p := ""
		for _, m := range list {
			p += fmt.Sprintf("<@%d> ", m.PlayerID)
		}
		return p
	}

	pingA := formatPings(rosterA)
	pingB := formatPings(rosterB)

	// Discord timestamp
	timestamp := "<t:%d:f>"
	if match.ScheduledDate != nil {
		timestamp = fmt.Sprintf("<t:%d:f>", match.ScheduledDate.Unix())
	} else {
		timestamp = "Not Set"
	}

	// --- Build embed description ---
	desc := fmt.Sprintf(
		"📌 **%s vs %s**\n"+
			"📅 **Match Time:** %s\n"+
			"🧑‍✈️ **Scheduled by:** <@%s>\n\n"+
			"🔵 **%s Roster:**\n%s\n\n"+
			"🔴 **%s Roster:**\n%s",
		teamA.Name, teamB.Name,
		timestamp,
		discordIDStr,
		teamA.Name, pingA,
		teamB.Name, pingB,
	)

	/// Combine both rosters into a ping message
	pingContent := fmt.Sprintf(
		"%s %s",
		pingA, pingB,
	)

	SendDiscordEmbedWithPings(
		pingContent,
		fmt.Sprintf("📅 Match Scheduled — %s", match.MatchCode),
		desc,
	)

	// Response
	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Match scheduled for %s", date.Format(time.RFC1123)),
	})
}

// / --- POST /api/match/submit-score ---
// One team enters or edits scores. Resets confirmations until both re-confirm.
func HandleSubmitScore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID      uint        `json:"match_id"`
		TeamID       uint        `json:"team_id"`
		LeagueSubA   *FlexibleID `json:"league_sub_a"`
		LeagueSubB   *FlexibleID `json:"league_sub_b"`
		CoinFlipCall string      `json:"coin_flip_call"`
		Maps         []struct {
			MapNumber  int    `json:"map_number"`
			Gamemode   string `json:"gamemode"`
			TeamAScore int    `json:"team_a_score"`
			TeamBScore int    `json:"team_b_score"`
		} `json:"maps"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// --- Convert FlexibleID → *int64 for the Match struct ---
	var subA, subB *int64
	if req.LeagueSubA != nil {
		v := req.LeagueSubA.Int64()
		subA = &v
	}
	if req.LeagueSubB != nil {
		v := req.LeagueSubB.Int64()
		subB = &v
	}

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// Load team names safely
	var teamA, teamB Team

	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	// ⭐ Perform Coin Flip (HEADS/TAILS)
	call := strings.ToUpper(strings.TrimSpace(req.CoinFlipCall))

	if call == "HEADS" || call == "TAILS" {

		// Random flip result
		sides := []string{"HEADS", "TAILS"}
		result := sides[rand.Intn(2)]

		// Determine winner (team that made the correct call)
		winner := ""
		if result == call {
			// calling team wins
			if req.TeamID == match.TeamAID {
				winner = "A"
			} else {
				winner = "B"
			}
		} else {
			// non-calling team wins
			if req.TeamID == match.TeamAID {
				winner = "B"
			} else {
				winner = "A"
			}
		}

		// Save winner to DB
		match.CoinFlip = winner
		DB.Model(&match).Update("coin_flip", winner)

		// Build readable winner
		winnerName := ""
		if winner == "A" {
			winnerName = teamA.Name
		} else {
			winnerName = teamB.Name
		}

		// Log to Discord
		LogGeneral(fmt.Sprintf(
			"🎲 **Coin Flip Performed**\n"+
				"Caller: **%s** (%s)\n"+
				"Call: **%s**\n"+
				"Result: **%s**\n"+
				"Winner: **%s**",
			func() string {
				if req.TeamID == match.TeamAID {
					return teamA.Name
				}
				return teamB.Name
			}(),
			map[bool]string{req.TeamID == match.TeamAID: "Team A", req.TeamID == match.TeamBID: "Team B"}[true],
			call,
			result,
			winnerName,
		))
	}

	isTeamA := req.TeamID == match.TeamAID
	isTeamB := req.TeamID == match.TeamBID
	if !isTeamA && !isTeamB {
		http.Error(w, "You are not part of this match", http.StatusForbidden)
		return
	}

	// 🚫 Cannot reuse same league sub for both sides
	if subA != nil && subB != nil && *subA == *subB {
		http.Error(w, "The same League Sub cannot be used for both teams.", http.StatusBadRequest)
		return
	}

	// Save league subs
	match.LeagueSubA = subA
	match.LeagueSubB = subB

	DB.Model(&match).Updates(map[string]any{
		"league_sub_a": subA,
		"league_sub_b": subB,
	})

	// Get existing score-set
	var existing []MatchScore
	DB.Where("match_id = ?", req.MatchID).Find(&existing)

	// Build new canonicalized score set
	var newScores []MatchScore
	for i, m := range req.Maps {
		mapNum := m.MapNumber
		if mapNum <= 0 {
			mapNum = i + 1
		}

		mode := strings.TrimSpace(m.Gamemode)
		if mode == "" {
			mode = "Unknown"
		}

		newScores = append(newScores, MatchScore{
			MatchID:    req.MatchID,
			MapNumber:  mapNum,
			Gamemode:   mode,
			TeamAScore: m.TeamAScore,
			TeamBScore: m.TeamBScore,
		})
	}

	// Detect changes
	changed := false

	if len(existing) != len(newScores) {
		changed = true
	} else {
		for _, n := range newScores {
			found := false
			for _, e := range existing {
				if e.MapNumber == n.MapNumber {
					found = true
					if e.TeamAScore != n.TeamAScore || e.TeamBScore != n.TeamBScore {
						changed = true
					}
					break
				}
			}
			if !found {
				changed = true
			}
		}
	}

	// Apply changes if needed
	if changed {
		DB.Where("match_id = ?", req.MatchID).Delete(&MatchScore{})

		for _, s := range newScores {
			if err := DB.Create(&s).Error; err != nil {
				log.Printf("❌ Failed to insert map %d score: %v", s.MapNumber, err)
			}
		}

		match.TeamAScoreConfirmed = false
		match.TeamBScoreConfirmed = false
		match.Status = "Pending Confirmation"

		if err := DB.Save(&match).Error; err != nil {
			log.Printf("❌ Failed to update match after score change: %v", err)
		}

		log.Printf("📝 Team %d submitted NEW scores for match #%d", req.TeamID, match.ID)

	} else {
		// No changes — preserve confirmations
		if err := DB.Save(&match).Error; err != nil {
			log.Printf("❌ Failed to save unchanged match: %v", err)
		}
		log.Printf("🔁 Team %d re-submitted SAME scores for match #%d", req.TeamID, match.ID)
	}

	respondJSON(w, map[string]any{
		"success": true,
		"changed": changed,
		"message": "Scores saved. Both teams must confirm to finalize.",
	})
}

func stringifyRosterPlayers(r []MatchRosterPlayer) []map[string]any {
	out := make([]map[string]any, 0, len(r))
	for _, p := range r {
		out = append(out, map[string]any{
			"player_id":    strconv.FormatInt(p.PlayerID, 10),
			"display_name": p.DisplayName,
			"username":     p.Username,
			"role":         p.Role,
		})
	}
	return out
}

type MatchRosterPlayer struct {
	PlayerID    int64  `json:"player_id"`
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
	Role        string `json:"role"`
}

// --- Get match with unified map_scores (legacy + JSONB) ---
func HandleGetMatch(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	matchID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid match ID", http.StatusBadRequest)
		return
	}

	var match Match
	if err := DB.First(&match, matchID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Match not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// --- Attempt to read JSONB field ---
	var jsonMaps []map[string]any
	var rawJSON sql.NullString
	if err := DB.Raw("SELECT map_scores FROM matches WHERE id = ?", match.ID).Scan(&rawJSON).Error; err == nil {
		if rawJSON.Valid && strings.TrimSpace(rawJSON.String) != "" {
			if err := json.Unmarshal([]byte(rawJSON.String), &jsonMaps); err != nil {
				log.Printf("⚠️ Failed to parse JSONB map_scores for match %d: %v", match.ID, err)
			}
		}
	}

	// --- Fallback: legacy table rows ---
	if len(jsonMaps) == 0 {
		var legacyMaps []MatchScore
		if err := DB.Where("match_id = ?", match.ID).Find(&legacyMaps).Error; err == nil {
			for _, m := range legacyMaps {
				jsonMaps = append(jsonMaps, map[string]any{
					"map":          m.MapNumber,
					"mode":         m.Gamemode,
					"team_a_score": m.TeamAScore,
					"team_b_score": m.TeamBScore,
				})
			}
		}
	}

	// --- Filter out 0–0 maps ---
	filtered := make([]map[string]any, 0)
	for _, m := range jsonMaps {
		a := int(toFloat(m["team_a_score"]))
		b := int(toFloat(m["team_b_score"]))
		if a == 0 && b == 0 {
			continue
		}
		filtered = append(filtered, m)
	}

	// --- Load teams ---
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	var rosterA, rosterB []MatchRosterPlayer

	// Try player_history first
	DB.Raw(`
		SELECT p.id AS player_id, p.display_name, p.username, ph.role
		FROM player_history ph
		JOIN players p ON p.id = ph.player_id
		WHERE ph.team_id = ? AND ph.season = ?
	`, match.TeamAID, currentSeason).Scan(&rosterA)

	DB.Raw(`
		SELECT p.id AS player_id, p.display_name, p.username, ph.role
		FROM player_history ph
		JOIN players p ON p.id = ph.player_id
		WHERE ph.team_id = ? AND ph.season = ?
	`, match.TeamBID, currentSeason).Scan(&rosterB)

	// 🧩 Fallback to live team_members if empty
	if len(rosterA) == 0 {
		DB.Raw(`
			SELECT p.id AS player_id, p.display_name, p.username, tm.role
			FROM team_members tm
			JOIN players p ON p.id = tm.player_id
			WHERE tm.team_id = ?
		`, match.TeamAID).Scan(&rosterA)
	}

	if len(rosterB) == 0 {
		DB.Raw(`
			SELECT p.id AS player_id, p.display_name, p.username, tm.role
			FROM team_members tm
			JOIN players p ON p.id = tm.player_id
			WHERE tm.team_id = ?
		`, match.TeamBID).Scan(&rosterB)
	}

	// Safely stringify league subs
	var leagueSubA string
	var leagueSubB string

	if match.LeagueSubA != nil {
		leagueSubA = strconv.FormatInt(*match.LeagueSubA, 10)
	}

	if match.LeagueSubB != nil {
		leagueSubB = strconv.FormatInt(*match.LeagueSubB, 10)
	}

	// Final Response
	respondJSON(w, map[string]any{
		"match": map[string]any{
			"id":                     match.ID,
			"match_code":             match.MatchCode,
			"team_a_id":              match.TeamAID,
			"team_b_id":              match.TeamBID,
			"scheduled_date":         match.ScheduledDate,
			"proposed_date":          match.ProposedDate,
			"status":                 match.Status,
			"winner_id":              match.WinnerID,
			"loser_id":               match.LoserID,
			"team_a_score_confirmed": match.TeamAScoreConfirmed,
			"team_b_score_confirmed": match.TeamBScoreConfirmed,
			"league_sub_a":           leagueSubA, // <-- FIXED
			"league_sub_b":           leagueSubB, // <-- FIXED
		},
		"teams":      map[string]any{"a": teamA, "b": teamB},
		"map_scores": filtered,
		"roster": map[string]any{
			"a": stringifyRosterPlayers(rosterA),
			"b": stringifyRosterPlayers(rosterB),
		},
	})

}

// helper to safely coerce numbers
func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	default:
		return 0
	}
}

// --- Get all matches for a team (with map scores) ---
func HandleGetTeamMatches(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	teamID, err := strconv.Atoi(vars["teamID"])
	if err != nil {
		http.Error(w, "Invalid team ID", http.StatusBadRequest)
		return
	}

	// fetch matches where team is A or B
	var matches []Match
	if err := DB.
		Where("team_a_id = ? OR team_b_id = ?", teamID, teamID).
		Find(&matches).Error; err != nil {
		http.Error(w, "Failed to load matches", http.StatusInternalServerError)
		return
	}

	// build response with maps
	type MatchWithMaps struct {
		Match Match        `json:"match"`
		Maps  []MatchScore `json:"maps"`
	}

	var result []MatchWithMaps
	for _, m := range matches {
		var maps []MatchScore
		DB.Where("match_id = ?", m.ID).Find(&maps)
		result = append(result, MatchWithMaps{
			Match: m,
			Maps:  maps,
		})
	}

	respondJSON(w, map[string]any{
		"team_id": teamID,
		"matches": result,
	})
}

func deriveSeasonAndWeek(matchCode string) (string, string) {
	// Example: "1-Week1-M004" → Season=1, Week=1
	// Example: "Week1-M004" → Season="Preseason", Week=1
	parts := strings.Split(matchCode, "-")

	if len(parts) >= 3 && strings.Contains(parts[1], "Week") {
		// Case: 1-Week1-M004
		season := parts[0]
		week := strings.TrimPrefix(parts[1], "Week")
		return season, week
	} else if len(parts) >= 2 && strings.HasPrefix(parts[0], "Week") {
		// Case: Week1-M004
		week := strings.TrimPrefix(parts[0], "Week")
		return "Preseason", week
	}

	return "Preseason", "?"
}

// --- Get all matches for a player (with historical team data) ---
func HandleGetPlayerMatches(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	playerID, err := strconv.ParseInt(vars["playerID"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid player ID", http.StatusBadRequest)
		return
	}

	// fetch historical memberships
	var history []PlayerHistory
	if err := DB.Where("player_id = ?", playerID).Find(&history).Error; err != nil {
		http.Error(w, "Failed to load history", http.StatusInternalServerError)
		return
	}
	if len(history) == 0 {
		respondJSON(w, map[string]any{
			"player_id": playerID,
			"matches":   []any{},
		})
		return
	}

	// collect all team IDs player has ever been on
	var teamIDs []uint
	teamRoles := make(map[uint]string)
	for _, h := range history {
		teamIDs = append(teamIDs, h.TeamID)
		teamRoles[h.TeamID] = h.Role
	}

	// fetch matches involving those teams
	var matches []Match
	if err := DB.
		Where("team_a_id IN ? OR team_b_id IN ?", teamIDs, teamIDs).
		Find(&matches).Error; err != nil {
		http.Error(w, "Failed to load matches", http.StatusInternalServerError)
		return
	}

	// build response with maps + team names from history
	type MatchWithDetails struct {
		Match      Match        `json:"match"`
		Maps       []MatchScore `json:"maps"`
		MyTeamName string       `json:"my_team_name"`
		MyRole     string       `json:"my_role"`
		OppTeam    string       `json:"opponent_team"`
		Season     string       `json:"season"`
	}

	var result []MatchWithDetails
	for _, m := range matches {
		var maps []MatchScore
		DB.Where("match_id = ?", m.ID).Find(&maps)

		// determine which team the player was on
		var myTeamID, oppTeamID uint
		if contains(teamIDs, m.TeamAID) {
			myTeamID, oppTeamID = m.TeamAID, m.TeamBID
		} else {
			myTeamID, oppTeamID = m.TeamBID, m.TeamAID
		}

		// load teams
		var myTeam, oppTeam Team
		DB.First(&myTeam, myTeamID)
		DB.First(&oppTeam, oppTeamID)

		// pick season + role from history
		season, role := "", ""
		for _, h := range history {
			if h.TeamID == myTeamID {
				season, role = h.Season, h.Role
				break
			}
		}

		result = append(result, MatchWithDetails{
			Match:      m,
			Maps:       maps,
			MyTeamName: myTeam.Name,
			MyRole:     role,
			OppTeam:    oppTeam.Name,
			Season:     season,
		})
	}

	respondJSON(w, map[string]any{
		"player_id": playerID,
		"matches":   result,
	})
}

// --- Get single player details ---
func GetPlayerDetail(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	playerID, err := strconv.ParseInt(params["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid player ID", http.StatusBadRequest)
		return
	}

	var player Player
	if err := DB.First(&player, playerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Return default placeholder for unregistered users
			respondJSON(w, map[string]any{
				"id":           params["id"],
				"username":     "Unregistered Player",
				"display_name": "Unregistered",
				"role":         "-",
				"rating":       0,
				"wins":         0,
				"losses":       0,
				"matches":      0,
				"current_team": "",
				"history":      []any{},
			})
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// --- Get current team (if any)
	var team struct {
		ID   uint
		Name string
	}
	DB.Raw(`
		SELECT t.id, t.name
		FROM teams t
		JOIN team_members tm ON tm.team_id = t.id
		WHERE tm.player_id = ?
		LIMIT 1
	`, playerID).Scan(&team)

	// --- Get player history (with team names)
	type HistoryRow struct {
		Season string `json:"season"`
		TeamID uint   `json:"team_id"`
		Team   string `json:"team"`
	}

	var history []HistoryRow
	DB.Raw(`
		SELECT ph.season, ph.team_id, ph.team_name AS team
		FROM player_histories ph
		WHERE ph.player_id = ?
		ORDER BY ph.season ASC
	`, playerID).Scan(&history)

	respondJSON(w, map[string]any{
		"id":              strconv.FormatInt(player.ID, 10),
		"username":        player.Username,
		"display_name":    player.DisplayName,
		"role":            player.Role,
		"timezone":        player.Timezone,
		"rating":          player.Rating,
		"wins":            player.Wins,
		"losses":          player.Losses,
		"matches":         player.Matches,
		"current_team":    team.Name,
		"current_team_id": team.ID,
		"history":         history,
	})
}

// --- Require League Mod via Discord Role ---
func requireLeagueMod(w http.ResponseWriter, r *http.Request) (string, bool) {
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", false
	}

	guildID := os.Getenv("DISCORD_GUILD_ID")
	modRoleID := os.Getenv("DISCORD_LEAGUE_MOD_ROLE_ID")
	botToken := os.Getenv("DISCORD_BOT_TOKEN")

	if guildID == "" || modRoleID == "" || botToken == "" {
		http.Error(w, "Server not configured for Discord role check", http.StatusInternalServerError)
		return "", false
	}

	// --- Call Discord API ---
	req, _ := http.NewRequest("GET",
		fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, discordID),
		nil)
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("❌ Discord API request failed: %v", err)
		http.Error(w, "Failed to reach Discord API", http.StatusInternalServerError)
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Discord returned %d: %s", resp.StatusCode, string(body))
		http.Error(w, "Failed to verify Discord role", http.StatusForbidden)
		return "", false
	}

	var member struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&member); err != nil {
		log.Printf("❌ Discord response parse error: %v", err)
		http.Error(w, "Failed to parse Discord response", http.StatusInternalServerError)
		return "", false
	}

	//log.Printf("👤 Discord member: %s (%s)", member.User.Username, member.User.ID)
	//log.Printf("🎭 Roles returned: %+v", member.Roles)
	//log.Printf("🔎 Expecting League Mod role ID: %s", modRoleID)

	for _, role := range member.Roles {
		if role == modRoleID {
			//log.Printf("✅ League Mod verified for %s", discordID)
			return discordID, true
		}
	}

	log.Printf("🚫 User %s missing League Mod role %s", discordID, modRoleID)
	http.Error(w, "Forbidden: missing League Mod role", http.StatusForbidden)
	return "", false
}

// ========= MOD HELPERS (safe utilities) =========

func modJSONErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

/*func getUint(body any) (uint, bool) {
	switch v := body.(type) {
	case float64:
		if v < 0 {
			return 0, false
		}
		return uint(v), true
	case int:
		if v < 0 {
			return 0, false
		}
		return uint(v), true
	case int64:
		if v < 0 {
			return 0, false
		}
		return uint(v), true
	case string:
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, false
		}
		return uint(n), true
	default:
		return 0, false
	}
}*/

// ========= MATCH MOD: Reset / Forfeit / DoubleForfeit / Delete =========

// POST /api/mod/match/reset
// body: { "match_id": <id> }
func ModMatchReset(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
	var req struct {
		MatchID uint `json:"match_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid match_id")
		return
	}

	var m Match
	if err := DB.First(&m, req.MatchID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "match not found")
		return
	}

	// clear scores + result; keep scheduled/proposed date as-is
	if err := DB.Where("match_id = ?", m.ID).Delete(&MatchScore{}).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed clearing scores")
		return
	}
	m.WinnerID = nil
	m.LoserID = nil
	m.Status = "Scheduled" // or "Pending" if you prefer
	if err := DB.Save(&m).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to reset match")
		return
	}

	respondJSON(w, map[string]any{"success": true, "message": "match reset", "match_id": m.ID})
}

// POST /api/mod/match/forfeit
// body: { "match_id": <id>, "winner_team_id": <teamID> }
func ModMatchForfeit(w http.ResponseWriter, r *http.Request) {
	modDiscordID, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}
	actorDiscordID := modDiscordID

	var req struct {
		MatchID      uint `json:"match_id"`
		WinnerTeamID uint `json:"winner_team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 || req.WinnerTeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid payload")
		return
	}

	var m Match
	if err := DB.First(&m, req.MatchID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "match not found")
		return
	}

	// Winner must be one of the teams
	if req.WinnerTeamID != m.TeamAID && req.WinnerTeamID != m.TeamBID {
		modJSONErr(w, http.StatusBadRequest, "winner_team_id not part of this match")
		return
	}

	// Determine loser
	var loser uint
	if req.WinnerTeamID == m.TeamAID {
		loser = m.TeamBID
	} else {
		loser = m.TeamAID
	}

	// Assign winner/loser + finalize match
	m.WinnerID = &req.WinnerTeamID
	m.LoserID = &loser
	m.Status = "Completed"

	// Clear map scores
	DB.Where("match_id = ?", m.ID).Delete(&MatchScore{})

	if err := DB.Save(&m).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to set forfeit")
		return
	}

	// Snapshot both teams
	snapshotTeamRoster(m.TeamAID, currentSeason)
	snapshotTeamRoster(m.TeamBID, currentSeason)

	// Fetch team names
	var teamA, teamB Team
	DB.First(&teamA, m.TeamAID)
	DB.First(&teamB, m.TeamBID)

	winnerTeam := teamA
	loserTeam := teamB
	if req.WinnerTeamID == teamB.ID {
		winnerTeam, loserTeam = teamB, teamA
	}

	// ⭐ MOD LOG → score log channel
	LogScore(fmt.Sprintf(
		"🏳️ **Match Forfeited by Mod:** %s\nWinner: **%s**\nLoser: **%s**\nForced by <@%s>",
		m.MatchCode,
		winnerTeam.Name,
		loserTeam.Name,
		actorDiscordID,
	))

	respondJSON(w, map[string]any{
		"success":    true,
		"message":    "match forfeited",
		"match_id":   m.ID,
		"winner":     req.WinnerTeamID,
		"loser":      loser,
		"season_log": currentSeason,
	})
}

// POST /api/mod/match/double-forfeit
// body: { "match_id": <id> }
func ModMatchDoubleForfeit(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
	var req struct {
		MatchID uint `json:"match_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid match_id")
		return
	}

	var m Match
	if err := DB.First(&m, req.MatchID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "match not found")
		return
	}

	// 🧹 Clear out any map scores
	_ = DB.Where("match_id = ?", m.ID).Delete(&MatchScore{}).Error

	m.WinnerID = nil
	m.LoserID = nil
	m.Status = "Completed" // double forfeit = still finalized

	if err := DB.Save(&m).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to set double forfeit")
		return
	}

	// 🧩 Snapshot both rosters for historical record
	snapshotTeamRoster(m.TeamAID, currentSeason)
	snapshotTeamRoster(m.TeamBID, currentSeason)

	log.Printf("🏳️‍⚖️ Mod applied double forfeit on match #%d between teams %d and %d", m.ID, m.TeamAID, m.TeamBID)

	respondJSON(w, map[string]any{
		"success":    true,
		"message":    "double forfeit applied",
		"match_id":   m.ID,
		"team_a_id":  m.TeamAID,
		"team_b_id":  m.TeamBID,
		"season_log": currentSeason,
	})
}

// DELETE /api/mod/match
// body: { "match_id": <id> }
func ModMatchDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
	var req struct {
		MatchID uint `json:"match_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid match_id")
		return
	}
	var m Match
	if err := DB.First(&m, req.MatchID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "match not found")
		return
	}
	if err := DB.Where("match_id = ?", m.ID).Delete(&MatchScore{}).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to delete scores")
		return
	}
	if err := DB.Delete(&m).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to delete match")
		return
	}
	respondJSON(w, map[string]any{"success": true, "message": "match deleted", "match_id": req.MatchID})
}

// ========= TEAM MOD: Adjust Rating / Disband =========

// POST /api/mod/team/adjust-rating
// body: { "team_id": <id>, "delta": <int> }
func ModTeamAdjustRating(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
	var req struct {
		TeamID uint `json:"team_id"`
		Delta  int  `json:"delta"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	var t Team
	if err := DB.First(&t, req.TeamID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "team not found")
		return
	}
	t.Rating += req.Delta
	if err := DB.Save(&t).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to adjust rating")
		return
	}
	respondJSON(w, map[string]any{"success": true, "team_id": t.ID, "new_rating": t.Rating})
}

// POST /api/mod/team/disband
// body: { "team_id": <id> }
func ModTeamDisband(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		TeamID uint `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid team_id")
		return
	}

	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "team not found")
		return
	}

	// 🔹 Remove all memberships
	if err := DB.Where("team_id = ?", req.TeamID).Delete(&TeamMember{}).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to remove members")
		return
	}

	// 🔹 Mark all their matches as cancelled (instead of deleting)
	_ = DB.Model(&Match{}).
		Where("team_a_id = ? OR team_b_id = ?", req.TeamID, req.TeamID).
		Update("status", "Cancelled").Error

	// 🔹 Mark team as Disbanded instead of deleting it
	if err := DB.Model(&Team{}).
		Where("id = ?", req.TeamID).
		Update("status", "Disbanded").Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to mark team disbanded")
		return
	}

	log.Printf("🏴‍☠️ Team %d (%s) marked as Disbanded by mod", team.ID, team.Name)

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Team %s marked as Disbanded", team.Name),
		"team_id": req.TeamID,
	})
}

// ========= PLAYER MOD: Kick / Ban / Unban =========

// POST /api/mod/player/kick
// body: { "team_id": <id>, "player_id": "<discord id or number>" }
func ModPlayerKick(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
	var req struct {
		TeamID   uint       `json:"team_id"`
		PlayerID FlexibleID `json:"player_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 || req.PlayerID.Int64() == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid payload")
		return
	}

	// mirror your captain kick, but privileged
	if err := DB.Where("team_id = ? AND player_id = ?", req.TeamID, req.PlayerID.Int64()).
		Delete(&TeamMember{}).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to remove member")
		return
	}

	respondJSON(w, map[string]any{"success": true, "message": "player removed"})
}

// POST /api/mod/player/ban
// body: { "player_id": "<discord id or number>", "reason": "optional" }
func ModPlayerBan(w http.ResponseWriter, r *http.Request) {
	modDiscordID, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}
	actorDiscordID := modDiscordID

	var req struct {
		PlayerID FlexibleID `json:"player_id"`
		Reason   string     `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PlayerID.Int64() == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid payload")
		return
	}

	var p Player
	if err := DB.First(&p, req.PlayerID.Int64()).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "player not found")
		return
	}

	if p.Role != "Banned" {
		p.Role = "Banned"
		if err := DB.Save(&p).Error; err != nil {
			modJSONErr(w, http.StatusInternalServerError, "failed to ban")
			return
		}
	}

	// Remove from any team
	DB.Where("player_id = ?", p.ID).Delete(&TeamMember{})

	// ⭐ MOD LOG — Ban
	LogGeneral(
		fmt.Sprintf(
			"⛔ **Player Banned:** <@%d> by <@%s>%s",
			p.ID,
			actorDiscordID,
			func() string {
				if req.Reason != "" {
					return "\n**Reason:** " + req.Reason
				}
				return ""
			}(),
		),
	)

	respondJSON(w, map[string]any{
		"success":   true,
		"message":   "player banned",
		"player_id": p.ID,
	})
}

// POST /api/mod/player/unban
// body: { "player_id": "<discord id or number>" }
func ModPlayerUnban(w http.ResponseWriter, r *http.Request) {
	modDiscordID, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}
	actorDiscordID := modDiscordID

	var req struct {
		PlayerID FlexibleID `json:"player_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PlayerID.Int64() == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid payload")
		return
	}

	var p Player
	if err := DB.First(&p, req.PlayerID.Int64()).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "player not found")
		return
	}

	if p.Role == "Banned" {
		p.Role = "Player"
		if err := DB.Save(&p).Error; err != nil {
			modJSONErr(w, http.StatusInternalServerError, "failed to unban")
			return
		}
	}

	// ⭐ MOD LOG — Unban
	LogGeneral(
		fmt.Sprintf(
			"♻️ **Player Unbanned:** <@%d> by <@%s>",
			p.ID,
			actorDiscordID,
		),
	)

	respondJSON(w, map[string]any{
		"success":   true,
		"message":   "player unbanned",
		"player_id": p.ID,
	})
}

// ========= DATA MOD: Reset Leaderboard (optional) =========

// POST /api/mod/leaderboard/reset
// body: { "scope": "teams|players|all" }
func ModLeaderboardReset(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
	var req struct {
		Scope string `json:"scope"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	scope := req.Scope
	if scope == "" {
		scope = "all"
	}

	tx := DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if scope == "teams" || scope == "all" {
		if err := tx.Model(&Team{}).Updates(map[string]any{
			"rating":  1000,
			"wins":    0,
			"losses":  0,
			"matches": 0,
		}).Error; err != nil {
			tx.Rollback()
			modJSONErr(w, http.StatusInternalServerError, "failed reset teams")
			return
		}
	}
	if scope == "players" || scope == "all" {
		if err := tx.Model(&Player{}).Updates(map[string]any{
			"rating":  1000,
			"wins":    0,
			"losses":  0,
			"matches": 0,
		}).Error; err != nil {
			tx.Rollback()
			modJSONErr(w, http.StatusInternalServerError, "failed reset players")
			return
		}
	}

	if err := tx.Commit().Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "commit failed")
		return
	}
	respondJSON(w, map[string]any{"success": true, "message": "leaderboard reset", "scope": scope})
}

// POST /api/mod/match/edit-score
// body: { "match_id": <id>, "maps": [ { "map_number": 1, "gamemode": "Payload", "team_a_score": 5, "team_b_score": 4 } ] }
func ModMatchEditScore(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		MatchID uint `json:"match_id"`
		Maps    []struct {
			MapNumber  int    `json:"map_number"`
			Gamemode   string `json:"gamemode"`
			TeamAScore int    `json:"team_a_score"`
			TeamBScore int    `json:"team_b_score"`
		} `json:"maps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid payload")
		return
	}

	var m Match
	if err := DB.First(&m, req.MatchID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "match not found")
		return
	}

	// 🧹 Clear existing map scores
	DB.Where("match_id = ?", m.ID).Delete(&MatchScore{})

	// 📝 Insert new map data
	for _, mapData := range req.Maps {
		DB.Create(&MatchScore{
			MatchID:    m.ID,
			MapNumber:  mapData.MapNumber,
			Gamemode:   strings.TrimSpace(mapData.Gamemode),
			TeamAScore: mapData.TeamAScore,
			TeamBScore: mapData.TeamBScore,
		})
	}

	// 🏁 Mark match completed
	m.Status = "Completed"
	if err := DB.Save(&m).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to save updated match")
		return
	}

	// 🧩 Snapshot both teams' rosters for historical record
	snapshotTeamRoster(m.TeamAID, currentSeason)
	snapshotTeamRoster(m.TeamBID, currentSeason)

	log.Printf("✏️ Mod edited and finalized scores for match #%d (Teams: %d vs %d)", m.ID, m.TeamAID, m.TeamBID)

	respondJSON(w, map[string]any{
		"success":    true,
		"message":    "scores updated and rosters snapshotted",
		"match_id":   m.ID,
		"team_a_id":  m.TeamAID,
		"team_b_id":  m.TeamBID,
		"season_log": currentSeason,
	})
}

// POST /api/mod/season/archive
// body: { "format": "json|csv", "reset_after": true|false }
func ModSeasonArchive(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		Format     string `json:"format"`
		ResetAfter bool   `json:"reset_after"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Format == "" {
		req.Format = "json"
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	os.MkdirAll("archives", 0755)

	// --- include match + map scores for a full snapshot ---
	type MatchExport struct {
		Match Match        `json:"match"`
		Maps  []MatchScore `json:"maps"`
	}

	type ArchiveData struct {
		Teams   []Team        `json:"teams"`
		Players []Player      `json:"players"`
		Matches []MatchExport `json:"matches"`
	}

	var teams []Team
	var players []Player
	var matches []Match

	DB.Find(&teams)
	DB.Find(&players)
	DB.Find(&matches)

	// attach maps to each match
	var fullMatches []MatchExport
	for _, m := range matches {
		var maps []MatchScore
		DB.Where("match_id = ?", m.ID).Find(&maps)
		fullMatches = append(fullMatches, MatchExport{Match: m, Maps: maps})
	}

	data := ArchiveData{
		Teams:   teams,
		Players: players,
		Matches: fullMatches,
	}

	// --- write to file ---
	var filePath string
	if req.Format == "csv" {
		filePath = fmt.Sprintf("archives/season_%s.csv", timestamp)
		f, err := os.Create(filePath)
		if err != nil {
			modJSONErr(w, http.StatusInternalServerError, "failed to create file")
			return
		}
		defer f.Close()

		// Simple CSV export for Teams & Players
		f.WriteString("=== TEAMS ===\n")
		f.WriteString("id,name,status,rating,wins,losses,matches\n")
		for _, t := range teams {
			fmt.Fprintf(f, "%d,%s,%s,%d,%d,%d,%d\n",
				t.ID, t.Name, t.Status, t.Rating, t.Wins, t.Losses, t.Matches)
		}

		f.WriteString("\n=== PLAYERS ===\n")
		f.WriteString("id,username,role,rating,wins,losses,matches\n")
		for _, p := range players {
			fmt.Fprintf(f, "%d,%s,%s,%d,%d,%d,%d\n",
				p.ID, p.Username, p.Role, p.Rating, p.Wins, p.Losses, p.Matches)
		}

		f.WriteString("\n=== MATCHES ===\n")
		f.WriteString("id,code,teamA,teamB,winner,status,date\n")
		for _, m := range matches {
			fmt.Fprintf(f, "%d,%s,%d,%d,%v,%s,%v\n",
				m.ID, m.MatchCode, m.TeamAID, m.TeamBID, m.WinnerID, m.Status, m.ScheduledDate)
		}

	} else {
		filePath = fmt.Sprintf("archives/season_%s.json", timestamp)
		f, err := os.Create(filePath)
		if err != nil {
			modJSONErr(w, http.StatusInternalServerError, "failed to create file")
			return
		}
		defer f.Close()
		_ = json.NewEncoder(f).Encode(data)
	}

	log.Printf("📦 Season archived to %s (%s)", filePath, req.Format)

	// --- Optionally reset leaderboard ---
	if req.ResetAfter {
		if err := resetAllLeagueStats(); err != nil {
			modJSONErr(w, http.StatusInternalServerError, "archive saved but reset failed")
			return
		}
		log.Printf("♻️ League reset after archive")
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": "season archived successfully",
		"file":    filePath,
		"reset":   req.ResetAfter,
	})
}

// internal helper
func resetAllLeagueStats() error {
	tx := DB.Begin()
	if err := tx.Model(&Player{}).Updates(map[string]any{
		"rating":  1000,
		"wins":    0,
		"losses":  0,
		"matches": 0,
	}).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Model(&Team{}).Updates(map[string]any{
		"rating":  1000,
		"wins":    0,
		"losses":  0,
		"matches": 0,
	}).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Model(&Match{}).Delete(&Match{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	return nil
}

// --- POST /api/mod/match/schedule ---
// Allows moderators to override and force schedule any match
func ModForceSchedule(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		MatchID uint   `json:"match_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		http.Error(w, "Invalid match_id", http.StatusBadRequest)
		return
	}

	parsed, _ := time.Parse(time.RFC3339, req.Date)
	if err := DB.Model(&Match{}).Where("id = ?", req.MatchID).
		Updates(map[string]any{
			"scheduled_date": parsed,
			"status":         "Scheduled",
		}).Error; err != nil {
		http.Error(w, "Failed to update match", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success":  true,
		"match_id": req.MatchID,
		"message":  "match force-scheduled by mod",
	})
}

// --- POST /api/mod/match/delete ---
// Allows League Mods to permanently delete a match.
func HandleModDeleteMatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		MatchID uint `json:"match_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		http.Error(w, "invalid match_id", http.StatusBadRequest)
		return
	}

	// Check if match exists
	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}

	// Delete the match and any associated data (scores, results, etc.)
	if err := DB.Delete(&match).Error; err != nil {
		log.Printf("❌ Failed to delete match %d: %v", req.MatchID, err)
		http.Error(w, "failed to delete match", http.StatusInternalServerError)
		return
	}

	log.Printf("🗑️ Mod deleted match %d (%s)", match.ID, match.MatchCode)

	respondJSON(w, map[string]any{
		"success":  true,
		"match_id": match.ID,
		"message":  "match deleted",
	})
}

func HandleConfirmSchedule(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLogin(w, r); !ok {
		return
	}

	var req struct {
		MatchID uint `json:"match_id"`
		TeamID  uint `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 || req.TeamID == 0 {
		http.Error(w, "invalid data", http.StatusBadRequest)
		return
	}

	// Get confirming user
	session, _ := store.Get(r, "session")
	actorDiscordID := session.Values["discord_id"].(string)

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}

	// Mark schedule confirmation
	if match.TeamAID == req.TeamID {
		match.TeamAScheduleConfirmed = true
	} else if match.TeamBID == req.TeamID {
		match.TeamBScheduleConfirmed = true
	} else {
		http.Error(w, "team not part of match", http.StatusForbidden)
		return
	}

	// Load teams
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	// ================================
	// 📌 Detect if this is a reschedule
	// ================================
	isReschedule := false
	if match.Status == "Scheduled" {
		isReschedule = true
	}

	// If both teams confirmed → finalize
	if match.TeamAScheduleConfirmed && match.TeamBScheduleConfirmed {

		// Update match
		now := time.Now()
		match.Status = "Scheduled"
		match.ScheduleConfirmedAt = &now
		DB.Save(&match)

		// ================================
		// 🔍 Load team rosters
		// ================================
		var teamAMembers, teamBMembers []TeamMember
		DB.Where("team_id = ?", match.TeamAID).Find(&teamAMembers)
		DB.Where("team_id = ?", match.TeamBID).Find(&teamBMembers)

		// ================================
		// 🧠 Format roster pings (clean)
		// ================================
		formatPings := func(list []TeamMember) string {
			if len(list) == 0 {
				return "*No players found*"
			}
			if len(list) > 15 {
				// Show first 10, hide the rest
				p := ""
				for i := 0; i < 10; i++ {
					p += fmt.Sprintf("<@%d> ", list[i].PlayerID)
				}
				return fmt.Sprintf("%s\n…and **%d more**", p, len(list)-10)
			}
			// Normal ping list
			p := ""
			for _, m := range list {
				p += fmt.Sprintf("<@%d> ", m.PlayerID)
			}
			return p
		}

		pingA := formatPings(teamAMembers)
		pingB := formatPings(teamBMembers)

		// ================================
		// 📅 Include scheduled date/time
		// ================================
		scheduledDate := "Not Set"
		if match.ScheduledDate != nil {
			// Discord timestamp style
			scheduledDate = fmt.Sprintf("<t:%d:f>", match.ScheduledDate.Unix())
		}

		// ================================
		// 📝 Build log message
		// ================================
		var logMsg string

		if isReschedule {
			// 🔁 Reschedule log
			logMsg = fmt.Sprintf(
				"🔁 **Match Rescheduled:** %s\n"+
					"Teams: **%s** vs **%s**\n"+
					"Rescheduled by <@%s>\n"+
					"📅 New Date: %s\n\n"+
					"🔵 **Team %s Players:**\n%s\n\n"+
					"🔴 **Team %s Players:**\n%s",
				match.MatchCode,
				teamA.Name, teamB.Name,
				actorDiscordID,
				scheduledDate,
				teamA.Name, pingA,
				teamB.Name, pingB,
			)
		} else {
			// 🆕 Initial schedule log
			logMsg = fmt.Sprintf(
				"📅 **Match Scheduled:** %s\n"+
					"Teams: **%s** vs **%s**\n"+
					"Confirmed by <@%s>\n"+
					"📅 Match Date: %s\n\n"+
					"🔵 **Team %s Players:**\n%s\n\n"+
					"🔴 **Team %s Players:**\n%s",
				match.MatchCode,
				teamA.Name, teamB.Name,
				actorDiscordID,
				scheduledDate,
				teamA.Name, pingA,
				teamB.Name, pingB,
			)
		}

		// ================================
		// 📤 Send schedule log to MATCHES channel
		// ================================
		LogMatch(logMsg)
	}

	respondJSON(w, map[string]any{
		"success":  true,
		"status":   match.Status,
		"match_id": match.ID,
	})
}

func SendScoreEmbedWithPings(content, title, description string) {
	botToken := getEnv("DISCORD_BOT_TOKEN", "")
	channelID := getEnv("DISCORD_LOG_CHANNEL_SCORES", "") // ⬅ CORRECT CHANNEL

	if botToken == "" || channelID == "" {
		log.Println("❌ Missing Discord score log env vars")
		return
	}

	body := map[string]any{
		"content": content, // pings go here
		"embeds": []any{
			map[string]any{
				"title":       title,
				"description": description,
				"color":       0x2ECC71, // green highlight
			},
		},
	}

	b, _ := json.Marshal(body)

	req, _ := http.NewRequest(
		"POST",
		"https://discord.com/api/v10/channels/"+channelID+"/messages",
		bytes.NewBuffer(b),
	)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("❌ SendScoreEmbedWithPings error:", err)
		return
	}

	resp.Body.Close()
}

// --- POST /api/match/confirm-score ---
func HandleConfirmScore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID uint `json:"match_id"`
		TeamID  uint `json:"team_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// --- Load match ---
	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// --- Load map scores (ordered for stable hashing) ---
	var maps []MatchScore
	DB.Where("match_id = ?", match.ID).Order("map_number ASC").Find(&maps)

	if len(maps) == 0 {
		http.Error(w, "No map scores submitted yet", http.StatusBadRequest)
		return
	}

	// 🔄 Reload match to ensure we have the latest LeagueSubA/B (from submit-score)
	DB.First(&match, match.ID)

	// --- Convert score list + subs to a comparable string ---
	calcHash := func(scores []MatchScore, subA *int64, subB *int64) string {
		var sb strings.Builder

		for _, m := range scores {
			// Include map_number as well just to be extra stable
			sb.WriteString(fmt.Sprintf("M%d:%d-%d|", m.MapNumber, m.TeamAScore, m.TeamBScore))
		}

		var aVal, bVal int64
		if subA != nil {
			aVal = *subA
		}
		if subB != nil {
			bVal = *subB
		}

		sb.WriteString(fmt.Sprintf("A:%d|B:%d", aVal, bVal))
		return sb.String()
	}

	currentHash := calcHash(maps, match.LeagueSubA, match.LeagueSubB)

	// --- If this is the *first* confirmation, store the hash ---
	if match.ScoreHash == "" {
		match.ScoreHash = currentHash
	} else {
		// --- SECOND TEAM MUST CONFIRM THE SAME HASH ---
		if match.ScoreHash != currentHash {

			// ⚠️ Opponent entered different scores or subs → overwrite existing ones
			DB.Where("match_id = ?", match.ID).Delete(&MatchScore{})

			// Recreate scores using the NEW submission (what's currently in DB)
			for _, s := range maps {
				if err := DB.Create(&MatchScore{
					MatchID:    match.ID,
					MapNumber:  s.MapNumber,
					Gamemode:   s.Gamemode,
					TeamAScore: s.TeamAScore,
					TeamBScore: s.TeamBScore,
				}).Error; err != nil {
					log.Printf("❌ Failed to recreate map %d score: %v", s.MapNumber, err)
				}
			}

			// Reset confirmations so BOTH must confirm again
			match.TeamAScoreConfirmed = false
			match.TeamBScoreConfirmed = false

			// Reset stored hash to the new state
			match.ScoreHash = currentHash
			match.Status = "Pending Confirmation"
			DB.Save(&match)

			// Notify teams in Discord
			SendDiscordLog(
				fmt.Sprintf(
					"🔄 **Score Updated:** Team %d submitted a *different* score set for Match #%d.\n"+
						"New scores have been saved. Both teams must confirm again.",
					req.TeamID, match.ID,
				),
			)

			respondJSON(w, map[string]any{
				"success": true,
				"status":  "Reset to new scores",
				"message": "Opponent entered different scores or league subs. New scores saved — both teams must confirm again.",
			})
			return
		}
	}

	// --- Mark team as confirmed ---
	switch req.TeamID {
	case match.TeamAID:
		match.TeamAScoreConfirmed = true
	case match.TeamBID:
		match.TeamBScoreConfirmed = true
	default:
		http.Error(w, "Team not part of match", http.StatusForbidden)
		return
	}

	DB.Save(&match)

	// --- One team confirmed (but not both yet) ---
	if !(match.TeamAScoreConfirmed && match.TeamBScoreConfirmed) {

		// Fetch teams
		var teamA, teamB Team
		DB.First(&teamA, match.TeamAID)
		DB.First(&teamB, match.TeamBID)

		// Determine submitter + opponent
		confirmingTeam := teamA
		opposingTeam := teamB
		if req.TeamID == match.TeamBID {
			confirmingTeam = teamB
			opposingTeam = teamA
		}

		// Get submitting Discord user ID
		session, _ := store.Get(r, "session")
		discordIDStr, _ := session.Values["discord_id"].(string)
		submitterID, _ := strconv.ParseInt(discordIDStr, 10, 64)
		submitterPing := fmt.Sprintf("<@%d>", submitterID)

		// Opponent captain + co-captain pings
		opposingCaptainPings := getBothCaptainPings(opposingTeam.ID)

		// Log
		SendDiscordLog(
			fmt.Sprintf(
				"📝 **%s submitted score confirmation for Match %s**\n"+
					"👥 **Teams:** %s vs %s\n"+
					"👤 **By:** %s\n"+
					"⏳ **Waiting on:** %s captains: %s",
				confirmingTeam.Name,  // the team confirming
				match.MatchCode,      // match ID
				teamA.Name,           // team A name
				teamB.Name,           // team B name
				submitterPing,        // the user who clicked confirm
				opposingTeam.Name,    // team that still needs to confirm
				opposingCaptainPings, // captain + co-captain pings
			),
		)

		respondJSON(w, map[string]any{
			"success": true,
			"status":  "Pending Confirmation",
			"message": "Waiting for opponent confirmation.",
		})
		return
	}

	// ==============================================================
	// 🏆 BOTH TEAMS CONFIRMED → FINALIZE MATCH
	// ==============================================================

	// Count map wins
	totalA, totalB := 0, 0
	for _, s := range maps {
		if s.TeamAScore > s.TeamBScore {
			totalA++
		} else if s.TeamBScore > s.TeamAScore {
			totalB++
		}
	}

	// Determine winner
	if totalA != totalB {
		var winnerID, loserID uint
		if totalA > totalB {
			winnerID = match.TeamAID
			loserID = match.TeamBID
		} else {
			winnerID = match.TeamBID
			loserID = match.TeamAID
		}
		match.WinnerID = &winnerID
		match.LoserID = &loserID
	}

	match.Status = "Completed"
	DB.Save(&match)

	// Leaderboard update for teams
	if match.WinnerID != nil {
		updateLeaderboards(*match.WinnerID, *match.LoserID)
	}

	// Load team records
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	// 🧍 APPLY LEAGUE SUB STATS
	applySubStats := func(subID *int64, won bool, teamID uint, teamName string) {
		if subID == nil {
			return
		}

		var p Player
		if err := DB.First(&p, *subID).Error; err != nil {
			return
		}

		// Update stats
		p.Matches++
		if won {
			p.Wins++
			p.Rating += getEnvInt("ELO_WIN_POINTS", 25)
		} else {
			p.Losses++
			p.Rating += getEnvInt("ELO_LOSS_POINTS", -25)
		}
		DB.Save(&p)

		// Add to PlayerHistory with correct team
		DB.Create(&PlayerHistory{
			PlayerID: *subID,
			TeamID:   teamID,
			TeamName: teamName,
			Role:     "League Sub",
			Season:   currentSeason,
		})
	}

	if match.WinnerID != nil {
		if *match.WinnerID == match.TeamAID {
			applySubStats(match.LeagueSubA, true, teamA.ID, teamA.Name)
			applySubStats(match.LeagueSubB, false, teamB.ID, teamB.Name)
		} else {
			applySubStats(match.LeagueSubA, false, teamA.ID, teamA.Name)
			applySubStats(match.LeagueSubB, true, teamB.ID, teamB.Name)
		}
	}

	// 📖 Load final map list
	var finalMaps []MatchScore
	DB.Where("match_id = ?", match.ID).Order("map_number ASC").Find(&finalMaps)

	mapLines := ""
	for _, m := range finalMaps {
		mapLines += fmt.Sprintf(
			"**Map %d (%s)**\n%s %d – %d %s\n\n",
			m.MapNumber,
			m.Gamemode,
			teamA.Name, m.TeamAScore,
			m.TeamBScore, teamB.Name,
		)
	}

	// Determine winner's name
	winnerName := "Tie"
	if match.WinnerID != nil {
		if *match.WinnerID == match.TeamAID {
			winnerName = teamA.Name
		} else {
			winnerName = teamB.Name
		}
	}

	// 🧍 League Sub Display
	subAName := "None"
	subBName := "None"

	if match.LeagueSubA != nil {
		var p Player
		DB.First(&p, *match.LeagueSubA)
		subAName = p.DisplayName
	}

	if match.LeagueSubB != nil {
		var p Player
		DB.First(&p, *match.LeagueSubB)
		subBName = p.DisplayName
	}

	// 🔔 Pings outside embed
	content := fmt.Sprintf(
		"🔔 **Finalized Match Results for %s vs %s**\n%s",
		teamA.Name,
		teamB.Name,
		getBothCaptainPings(teamA.ID)+getBothCaptainPings(teamB.ID),
	)

	// 📦 Final Embed
	desc := fmt.Sprintf(
		"**%s vs %s**\n\n"+
			"📘 **Match ID**\n%s\n\n"+
			"%s"+
			"🧍 **League Subs**\n"+
			"• %s Sub: **%s**\n"+
			"• %s Sub: **%s**\n\n"+
			"🏆 **Winner**\n%s",
		teamA.Name,
		teamB.Name,
		match.MatchCode,
		mapLines,
		teamA.Name, subAName,
		teamB.Name, subBName,
		winnerName,
	)

	// Send embed
	SendScoreEmbedWithPings(
		content,
		"🏆 Final Match Result",
		desc,
	)

	respondJSON(w, map[string]any{
		"success": true,
		"status":  "Completed",
		"message": "Match finalized.",
	})
}

// updateLeaderboards updates both team + player leaderboards using ELO from env.
func updateLeaderboards(winnerID, loserID uint) {
	// Read from .env or fallback
	eloWinPoints := getEnvInt("ELO_WIN_POINTS", 25)
	eloLossPoints := getEnvInt("ELO_LOSS_POINTS", -25)
	defaultPlayerRating := getEnvInt("DEFAULT_PLAYER_RATING", 800)

	var winner, loser Team
	if err := DB.First(&winner, winnerID).Error; err == nil {
		winner.Wins++
		winner.Matches++
		winner.Rating += eloWinPoints
		DB.Save(&winner)
	} else {
		log.Printf("⚠️ Could not find winner team %d", winnerID)
	}

	if err := DB.First(&loser, loserID).Error; err == nil {
		loser.Losses++
		loser.Matches++
		loser.Rating += eloLossPoints
		DB.Save(&loser)
	} else {
		log.Printf("⚠️ Could not find loser team %d", loserID)
	}

	// --- Player stats ---
	var winners, losers []TeamMember
	DB.Where("team_id = ?", winnerID).Find(&winners)
	DB.Where("team_id = ?", loserID).Find(&losers)

	for _, w := range winners {
		DB.Model(&Player{}).Where("id = ?", w.PlayerID).
			Updates(map[string]any{
				"wins":    gorm.Expr("wins + 1"),
				"matches": gorm.Expr("matches + 1"),
				"rating":  gorm.Expr("COALESCE(rating, ?) + ?", defaultPlayerRating, eloWinPoints),
			})
	}

	for _, l := range losers {
		DB.Model(&Player{}).Where("id = ?", l.PlayerID).
			Updates(map[string]any{
				"losses":  gorm.Expr("losses + 1"),
				"matches": gorm.Expr("matches + 1"),
				"rating":  gorm.Expr("COALESCE(rating, ?) + ?", defaultPlayerRating, eloLossPoints),
			})
	}

	log.Printf("📊 Leaderboards updated (ELO %+d / %+d): winner=%d loser=%d",
		eloWinPoints, eloLossPoints, winnerID, loserID)
}

// snapshotTeamRoster saves all current members of a team into player_history for the current season (if not already recorded).
func snapshotTeamRoster(teamID uint, season string) {
	var members []struct {
		PlayerID int64
		Role     string
	}
	DB.Table("team_members").Select("player_id, role").Where("team_id = ?", teamID).Scan(&members)

	var team Team
	DB.First(&team, teamID)

	for _, m := range members {
		// Only insert if not already logged for this team+season+player
		var existing PlayerHistory
		if err := DB.Where("player_id = ? AND team_id = ? AND season = ?", m.PlayerID, teamID, season).
			First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {

			DB.Create(&PlayerHistory{
				PlayerID: m.PlayerID,
				TeamID:   teamID,
				TeamName: team.Name,
				Role:     m.Role,
				Season:   season,
			})
		}
	}
}

// --- POST /api/mod/team/set-inactive ---
// Sets a single team to Inactive (League Mod only)
func HandleModSetTeamInactive(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		TeamID uint `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := DB.Model(&Team{}).
		Where("id = ?", req.TeamID).
		Update("status", "Inactive").Error; err != nil {
		http.Error(w, "failed to update team", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Set team #%d inactive", req.TeamID),
	})
}

// --- POST /api/mod/teams/set-all-inactive ---
// Sets all Active teams to Inactive (League Mod only)
func HandleModSetAllTeamsInactive(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	result := DB.Model(&Team{}).
		Where("status = ?", "Active").
		Update("status", "Inactive")
	if result.Error != nil {
		http.Error(w, "failed to update teams", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Set %d teams inactive", result.RowsAffected),
	})
}

// --- POST /api/mod/team/delete ---
// Permanently deletes a team (League Mod only)
func HandleModDeleteTeam(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		TeamID uint `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Step 1️⃣ – Ensure team exists
	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// Step 2️⃣ – Delete memberships and join requests
	DB.Where("team_id = ?", req.TeamID).Delete(&TeamMember{})
	DB.Where("team_id = ?", req.TeamID).Delete(&TeamJoinRequest{})

	// Step 3️⃣ – Delete matches referencing this team (safe cleanup)
	DB.Where("team_a_id = ? OR team_b_id = ?", req.TeamID, req.TeamID).Delete(&Match{})

	// Step 4️⃣ – Delete the team itself
	if err := DB.Delete(&Team{}, req.TeamID).Error; err != nil {
		http.Error(w, "Failed to delete team", http.StatusInternalServerError)
		return
	}

	log.Printf("🗑️ League Mod deleted team #%d (%s)", team.ID, team.Name)

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Deleted team #%d (%s)", team.ID, team.Name),
	})
}

// --- POST /api/mod/team/rename ---
// Allows League Mods to rename a team safely
func HandleModRenameTeam(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		TeamID  uint   `json:"team_id"`
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 || strings.TrimSpace(req.NewName) == "" {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Prevent duplicate names
	var existing Team
	if err := DB.Where("LOWER(name) = LOWER(?)", req.NewName).First(&existing).Error; err == nil {
		http.Error(w, "A team with that name already exists.", http.StatusConflict)
		return
	}

	// Rename
	if err := DB.Model(&Team{}).Where("id = ?", req.TeamID).Update("name", req.NewName).Error; err != nil {
		http.Error(w, "Failed to rename team", http.StatusInternalServerError)
		return
	}

	log.Printf("✏️ League Mod renamed team #%d to '%s'", req.TeamID, req.NewName)

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Renamed team #%d to '%s'", req.TeamID, req.NewName),
	})
}

// Rename a team (League Mod only)
func ModTeamRename(w http.ResponseWriter, r *http.Request) {
	modIDStr, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}

	// Convert the string ID (Discord ID) to int64 safely
	modID, _ := strconv.ParseInt(modIDStr, 10, 64)

	var req struct {
		TeamID  uint   `json:"team_id"`
		NewName string `json:"new_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 || req.NewName == "" {
		modJSONErr(w, http.StatusBadRequest, "invalid payload")
		return
	}

	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "team not found")
		return
	}

	// Check for duplicate names
	var exists int64
	DB.Model(&Team{}).Where("LOWER(name) = LOWER(?) AND id <> ?", req.NewName, req.TeamID).Count(&exists)
	if exists > 0 {
		modJSONErr(w, http.StatusConflict, "team name already exists")
		return
	}

	// ✅ Save old name before changing
	oldName := team.Name
	team.Name = req.NewName

	if err := DB.Save(&team).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to rename team")
		return
	}

	// 🧾 Log rename history
	_ = DB.Exec(`
		INSERT INTO team_history (team_id, old_name, new_name, changed_by)
		VALUES (?, ?, ?, ?)
	`, team.ID, oldName, team.Name, modID)

	log.Printf("🧰 [MOD] Renamed team #%d: '%s' → '%s' (by mod %d)", team.ID, oldName, team.Name, modID)

	respondJSON(w, map[string]any{
		"success": true,
		"message": "team renamed successfully",
		"team":    team,
	})
}

// Captain rename team (limited to their own team)
func CaptainRenameTeam(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	var req struct {
		TeamID  uint   `json:"team_id"`
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 || req.NewName == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// 🔒 Verify this player is the captain of the given team
	var member TeamMember
	if err := DB.Where("team_id = ? AND player_id = ?", req.TeamID, playerID).First(&member).Error; err != nil {
		http.Error(w, "not part of this team", http.StatusForbidden)
		return
	}
	if strings.ToLower(member.Role) != "captain" {
		http.Error(w, "only captains can rename the team", http.StatusForbidden)
		return
	}

	// ✅ Verify team exists
	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		http.Error(w, "team not found", http.StatusNotFound)
		return
	}

	// 🚫 Prevent duplicate names
	var exists int64
	DB.Model(&Team{}).Where("LOWER(name) = LOWER(?) AND id <> ?", req.NewName, req.TeamID).Count(&exists)
	if exists > 0 {
		http.Error(w, "team name already taken", http.StatusConflict)
		return
	}

	// ✅ Save old name before changing
	oldName := team.Name
	team.Name = req.NewName

	if err := DB.Save(&team).Error; err != nil {
		http.Error(w, "failed to rename team", http.StatusInternalServerError)
		return
	}

	// 🧾 Log rename history
	_ = DB.Exec(`
		INSERT INTO team_history (team_id, old_name, new_name, changed_by)
		VALUES (?, ?, ?, ?)
	`, team.ID, oldName, team.Name, playerID)

	log.Printf("🧢 Captain renamed team #%d: '%s' → '%s' (player %d)", team.ID, oldName, team.Name, playerID)

	SendDiscordLog(
		fmt.Sprintf(
			"✏️ **Team Renamed:** **%s** → **%s** (Team #%d, Captain <@%d>)",
			oldName,
			team.Name,
			team.ID,
			playerID,
		),
	)

	respondJSON(w, map[string]any{"success": true, "team": team})
}

// Get all team rename history (League Mod only)
func ModGetTeamHistory(w http.ResponseWriter, r *http.Request) {

	// Optional filter: ?team_id=###
	teamIDParam := r.URL.Query().Get("team_id")
	var teamID int
	if teamIDParam != "" {
		if val, err := strconv.Atoi(teamIDParam); err == nil {
			teamID = val
		}
	}

	type HistoryRow struct {
		ID        uint      `json:"id"`
		TeamID    uint      `json:"team_id"`
		OldName   string    `json:"old_name"`
		NewName   string    `json:"new_name"`
		ChangedBy int64     `json:"changed_by"`
		ChangedAt time.Time `json:"changed_at"`
		Changer   string    `json:"changer"` // username if available
	}

	query := `
		SELECT 
			th.id, th.team_id, th.old_name, th.new_name, th.changed_by, th.changed_at,
			COALESCE(p.display_name, p.username, 'Unknown') AS changer
		FROM team_history th
		LEFT JOIN players p ON p.id = th.changed_by
	`
	if teamID > 0 {
		query += " WHERE th.team_id = ?"
	}

	query += " ORDER BY th.changed_at DESC LIMIT 100"

	var rows []HistoryRow
	var err error
	if teamID > 0 {
		err = DB.Raw(query, teamID).Scan(&rows).Error
	} else {
		err = DB.Raw(query).Scan(&rows).Error
	}

	if err != nil {
		log.Printf("❌ ModGetTeamHistory: query failed: %v", err)
		http.Error(w, "failed to fetch history", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"count":   len(rows),
		"history": rows,
	})
}

// / Lock all rosters
func ModRosterLockAll(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
	if err := DB.Exec("UPDATE settings SET roster_locked = TRUE WHERE id = 1").Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to enable roster lock")
		return
	}
	DB.Exec("UPDATE teams SET join_allowed = FALSE")
	respondJSON(w, map[string]any{"success": true, "locked": true})
}

// Unlock all rosters
func ModRosterUnlockAll(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
	if err := DB.Exec("UPDATE settings SET roster_locked = FALSE WHERE id = 1").Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to disable roster lock")
		return
	}
	respondJSON(w, map[string]any{"success": true, "locked": false})
}

// Get global roster lock status
func GetRosterLockStatus(w http.ResponseWriter, r *http.Request) {
	var locked bool
	if err := DB.Raw("SELECT roster_locked FROM settings WHERE id = 1").Scan(&locked).Error; err != nil {
		http.Error(w, "failed to fetch status", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]any{"locked": locked})
}

// --- GET /api/mod/team/history ---
func ModTeamHistory(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	teamID := strings.TrimSpace(r.URL.Query().Get("team_id"))

	var rows []struct {
		ID        uint      `json:"id"`
		TeamID    uint      `json:"team_id"`
		OldName   string    `json:"old_name"`
		NewName   string    `json:"new_name"`
		ChangedBy string    `json:"changed_by"`
		Changer   string    `json:"changer"`
		ChangedAt time.Time `json:"changed_at"`
	}

	q := DB.Table("team_rename_logs").
		Select("team_rename_logs.id, team_rename_logs.team_id, team_rename_logs.old_name, team_rename_logs.new_name, team_rename_logs.changed_by, players.display_name AS changer, team_rename_logs.changed_at").
		Joins("LEFT JOIN players ON players.id = team_rename_logs.changed_by")

	if teamID != "" {
		q = q.Where("team_rename_logs.team_id = ?", teamID)
	}
	if search != "" {
		searchLike := "%" + search + "%"
		q = q.Where(`
			LOWER(team_rename_logs.old_name) LIKE LOWER(?) OR
			LOWER(team_rename_logs.new_name) LIKE LOWER(?) OR
			LOWER(players.display_name) LIKE LOWER(?)`,
			searchLike, searchLike, searchLike)
	}

	if err := q.Order("team_rename_logs.changed_at DESC").Scan(&rows).Error; err != nil {
		log.Printf("❌ ModTeamHistory failed: %v", err)
		http.Error(w, "Failed to fetch rename logs", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"history": rows,
	})
}

// --- League Mod: Add player to a team manually (with auto role adjustment) ---
func ModAddPlayerToTeam(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		PlayerID int64  `json:"player_id"`
		TeamID   uint   `json:"team_id"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		modJSONErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.PlayerID == 0 || req.TeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "missing player_id or team_id")
		return
	}

	// Normalize role
	role := strings.Title(strings.ToLower(req.Role))
	if role == "" {
		role = "Member"
	}
	if role != "Member" && role != "Co-Captain" && role != "Captain" {
		modJSONErr(w, http.StatusBadRequest, "invalid role")
		return
	}

	// ✅ Check if player already belongs to a team
	var existing TeamMember
	if err := DB.Where("player_id = ?", req.PlayerID).First(&existing).Error; err == nil {
		modJSONErr(w, http.StatusConflict, "player already on a team")
		return
	}

	// ✅ Auto-demote existing captains/co-captains if adding a Captain
	if role == "Captain" {
		// Step 1: Demote any current Captain → Co-Captain
		DB.Model(&TeamMember{}).
			Where("team_id = ? AND role = ?", req.TeamID, "Captain").
			Update("role", "Co-Captain")

		// Step 2: Demote any existing Co-Captain → Member
		DB.Model(&TeamMember{}).
			Where("team_id = ? AND role = ?", req.TeamID, "Co-Captain").
			Update("role", "Member")
	}

	// ✅ Create new membership
	member := TeamMember{
		PlayerID: req.PlayerID,
		TeamID:   req.TeamID,
		Role:     role,
	}
	log.Printf("🧩 ADD DEBUG: team_id=%v player_id=%v role=%v", req.TeamID, req.PlayerID, role)

	if err := DB.Create(&member).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to add player to team")
		return
	}

	log.Println("✅ DB Create succeeded for player:", req.PlayerID)

	// ✅ Log team history
	var team Team
	DB.First(&team, req.TeamID)
	DB.Create(&PlayerHistory{
		PlayerID: req.PlayerID,
		TeamID:   team.ID,
		TeamName: team.Name,
		Role:     role,
		Season:   currentSeason,
	})

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Player %d added to %s as %s (roles adjusted)", req.PlayerID, team.Name, role),
	})
}

// --- League Mod: Set one team active ---
func ModSetTeamActive(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
	var req struct {
		TeamID uint `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid team_id")
		return
	}
	if err := DB.Model(&Team{}).Where("id = ?", req.TeamID).Update("status", "Active").Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to set team active")
		return
	}
	respondJSON(w, map[string]any{"success": true})
}

// --- League Mod: Set ALL teams active ---
func ModSetAllTeamsActive(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	// GORM requires an explicit WHERE for global updates
	if err := DB.Model(&Team{}).
		Where("1 = 1").
		Update("status", "Active").Error; err != nil {

		modJSONErr(w, http.StatusInternalServerError, "failed to set all teams active")
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": "All teams set to Active",
	})
}

// POST /api/match/cast
// Creates a private caster channel
func HandleRequestCast(w http.ResponseWriter, r *http.Request) {
	type Body struct {
		MatchID uint `json:"match_id"`
	}
	var req Body

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	session, _ := store.Get(r, "session")
	discordIDStr, ok := session.Values["discord_id"].(string)
	if !ok || discordIDStr == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var player Player
	if err := DB.First(&player, "id = ?", discordIDStr).Error; err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	casterRoleID := getEnv("DISCORD_CASTER_ROLE_ID", "")
	guildID := getEnv("DISCORD_GUILD_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")

	if casterRoleID == "" || guildID == "" || botToken == "" {
		http.Error(w, "Caster role not configured", http.StatusInternalServerError)
		return
	}

	// Check guild roles
	isCaster := false
	{
		url := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, discordIDStr)
		req2, _ := http.NewRequest("GET", url, nil)
		req2.Header.Set("Authorization", "Bot "+botToken)
		resp, err := http.DefaultClient.Do(req2)
		if err == nil && resp.StatusCode == 200 {
			var member struct {
				Roles []string `json:"roles"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&member)
			resp.Body.Close()

			for _, r := range member.Roles {
				if r == casterRoleID {
					isCaster = true
					break
				}
			}
		}
	}

	if !isCaster {
		http.Error(w, "You are not a caster", http.StatusForbidden)
		return
	}

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	finalStatuses := map[string]bool{
		"Finished":       true,
		"Completed":      true,
		"Forfeit":        true,
		"Double Forfeit": true,
		"Cancelled":      true,
	}

	if finalStatuses[strings.TrimSpace(match.Status)] {
		http.Error(w, "This match has already been finalized and cannot be casted.", http.StatusForbidden)
		return
	}

	if match.Status != "Scheduled" {
		http.Error(w, "Match is not scheduled", http.StatusForbidden)
		return
	}

	var teamA Team
	var teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	var rosterA []TeamMember
	var rosterB []TeamMember
	DB.Where("team_id = ?", match.TeamAID).Find(&rosterA)
	DB.Where("team_id = ?", match.TeamBID).Find(&rosterB)

	categoryID := getEnv("DISCORD_CAST_CATEGORY_ID", "")
	if categoryID == "" {
		http.Error(w, "Cast category not configured", http.StatusInternalServerError)
		return
	}

	const (
		PermViewChannel        = 1 << 10 // 1024
		PermSendMessages       = 1 << 11 // 2048
		PermReadMessageHistory = 1 << 16 // 65536
	)

	overwrites := []map[string]any{
		{
			"id":   guildID,
			"type": 0,
			"deny": PermViewChannel, // int, not string
		},
		{
			"id":    casterRoleID,
			"type":  0,
			"allow": PermViewChannel | PermSendMessages | PermReadMessageHistory,
		},
	}

	modRoleID := getEnv("DISCORD_LEAGUE_MOD_ROLE_ID", "")
	if modRoleID != "" {
		overwrites = append(overwrites, map[string]any{
			"id":    modRoleID,
			"type":  0,
			"allow": PermViewChannel | PermReadMessageHistory,
		})
	}

	for _, tm := range append(rosterA, rosterB...) {
		overwrites = append(overwrites, map[string]any{
			"id":    fmt.Sprint(tm.PlayerID),
			"type":  1,
			"allow": PermViewChannel | PermReadMessageHistory,
		})
	}

	body := map[string]any{
		"name":                  fmt.Sprintf("cast-%s", match.MatchCode),
		"type":                  0,
		"parent_id":             categoryID,
		"permission_overwrites": overwrites,
	}

	jsonBody, _ := json.Marshal(body)

	req3, _ := http.NewRequest("POST",
		fmt.Sprintf("https://discord.com/api/v10/guilds/%s/channels", guildID),
		strings.NewReader(string(jsonBody)))

	req3.Header.Set("Authorization", "Bot "+botToken)
	req3.Header.Set("Content-Type", "application/json")

	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		http.Error(w, "Failed to create Discord channel", http.StatusInternalServerError)
		return
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != 201 {
		bodyBytes, _ := io.ReadAll(resp3.Body)
		log.Println("Discord API error:", resp3.Status)
		log.Println("Response:", string(bodyBytes))
		http.Error(w, "Discord API error", http.StatusInternalServerError)
		return
	}

	var created struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp3.Body).Decode(&created)

	// ==========================================================
	// SEND MESSAGE INSIDE THE CAST CHANNEL
	// ==========================================================
	msgBody := map[string]any{
		"content": fmt.Sprintf(
			"📣 **This match is being casted!**\n\n"+
				"**%s vs %s**\n\n"+
				"🔗 **Post all Taxi Links here**\n\n"+
				"wait for casters greenlight before starting the match.\n\n%s",
			teamA.Name,
			teamB.Name,
			buildMentionList(rosterA, rosterB),
		),
	}

	msgJSON, _ := json.Marshal(msgBody)

	msgReq, _ := http.NewRequest("POST",
		fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", created.ID),
		strings.NewReader(string(msgJSON)),
	)

	msgReq.Header.Set("Authorization", "Bot "+botToken)
	msgReq.Header.Set("Content-Type", "application/json")

	msgResp, err := http.DefaultClient.Do(msgReq)
	if err != nil {
		log.Println("❌ Failed to send cast message:", err)
	} else {
		defer msgResp.Body.Close()
		if msgResp.StatusCode >= 300 {
			bodyBytes, _ := io.ReadAll(msgResp.Body)
			log.Println("❌ Discord message error:", msgResp.Status)
			log.Println("Response:", string(bodyBytes))
		}
	}

	respondJSON(w, map[string]any{
		"success":    true,
		"channel_id": created.ID,
	})
}

func buildMentionList(a []TeamMember, b []TeamMember) string {
	text := ""
	for _, tm := range append(a, b...) {
		text += fmt.Sprintf("<@%d> ", tm.PlayerID)
	}
	return text
}

func HandleConfirmCoinFlip(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID      uint   `json:"match_id"`
		TeamID       uint   `json:"team_id"`
		CoinFlipCall string `json:"coin_flip_call"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Load match
	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// Load teams
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	call := strings.ToUpper(strings.TrimSpace(req.CoinFlipCall))
	if call != "HEADS" && call != "TAILS" {
		http.Error(w, "Invalid coin flip call", http.StatusBadRequest)
		return
	}

	// Random flip result
	sides := []string{"HEADS", "TAILS"}
	result := sides[rand.Intn(2)]

	// Determine winner
	var winner string
	if result == call {
		if req.TeamID == match.TeamAID {
			winner = "A"
		} else {
			winner = "B"
		}
	} else {
		if req.TeamID == match.TeamAID {
			winner = "B"
		} else {
			winner = "A"
		}
	}

	// Save winner
	match.CoinFlip = winner
	DB.Model(&match).Update("coin_flip", winner)

	// Winner readable
	winnerName := teamA.Name
	if winner == "B" {
		winnerName = teamB.Name
	}

	// ==============================
	// ⭐ Load roster + build mentions
	// ==============================
	var rosterA []TeamMember
	var rosterB []TeamMember
	DB.Where("team_id = ?", match.TeamAID).Find(&rosterA)
	DB.Where("team_id = ?", match.TeamBID).Find(&rosterB)

	mentionsA := ""
	for _, p := range rosterA {
		mentionsA += fmt.Sprintf("<@%d> ", p.PlayerID)
	}

	mentionsB := ""
	for _, p := range rosterB {
		mentionsB += fmt.Sprintf("<@%d> ", p.PlayerID)
	}

	// Caller readable
	callerName := teamA.Name
	if req.TeamID == match.TeamBID {
		callerName = teamB.Name
	}

	// Discord Log Message
	LogGeneral(fmt.Sprintf(
		"🎲 **Coin Flip Performed**\n"+
			"📌 **Match:** %s (#%d)\n"+
			"🙋 **Caller:** %s \n"+
			"🪙 **Call:** %s\n"+
			"🎰 **Result:** %s\n"+
			"🏆 **Flip Winner:** %s\n\n"+
			"**%s Team:** %s\n"+
			"**%s Team:** %s",
		match.MatchCode, match.ID, // match info
		callerName,               // who called
		call, result, winnerName, // flip outcome
		teamA.Name, mentionsA, // team A pings
		teamB.Name, mentionsB, // team B pings
	))

	respondJSON(w, map[string]any{
		"success": true,
		"result":  result,
		"winner":  winnerName,
	})
}

func HandleChallengeRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequesterTeamID uint `json:"requester_team_id"`
		TargetTeamID    uint `json:"target_team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.RequesterTeamID == 0 || req.TargetTeamID == 0 {
		http.Error(w, "Missing team IDs", http.StatusBadRequest)
		return
	}

	// Load teams
	var requester, target Team
	if err := DB.First(&requester, req.RequesterTeamID).Error; err != nil {
		http.Error(w, "Requester team not found", http.StatusNotFound)
		return
	}
	if err := DB.First(&target, req.TargetTeamID).Error; err != nil {
		http.Error(w, "Target team not found", http.StatusNotFound)
		return
	}

	// Cannot challenge self
	if requester.ID == target.ID {
		http.Error(w, "Cannot challenge your own team", http.StatusBadRequest)
		return
	}

	// Check if target allows challenges
	if !target.AllowChallenges {
		http.Error(w, "Target team does not allow challenges", http.StatusForbidden)
		return
	}

	// Load global league settings (weekly challenge limit)
	var settings LeagueSettings
	if err := DB.First(&settings, 1).Error; err != nil {
		settings.WeeklyChallengeLimit = 1 // safe fallback
	}

	// Weekly challenge limit check (CORRECT FIELD)
	if requester.WeeklyChallengesUsed >= settings.WeeklyChallengeLimit {
		http.Error(w, "Weekly challenge limit reached", http.StatusForbidden)
		return
	}

	// Prevent duplicate pending
	var exists int64
	DB.Model(&ChallengeRequest{}).
		Where("requester_team_id = ? AND target_team_id = ? AND status = 'Pending'",
			requester.ID, target.ID).
		Count(&exists)
	if exists > 0 {
		http.Error(w, "Challenge already pending", http.StatusConflict)
		return
	}

	// Create challenge
	challenge := ChallengeRequest{
		RequesterTeamID: requester.ID,
		TargetTeamID:    target.ID,
		Week:            GetGlobalCurrentWeek(),
		Status:          "Pending",
	}
	DB.Create(&challenge)

	// Notify target captains
	var captains []TeamMember
	DB.Where("team_id = ? AND role IN ?", target.ID, []string{"Captain", "Co-Captain"}).Find(&captains)

	mentions := ""
	for _, c := range captains {
		mentions += fmt.Sprintf("<@%d> ", c.PlayerID)
	}

	LogGeneral(fmt.Sprintf(
		"⚠️ **Challenge Match Requested!**\n"+
			"**%s** has challenged **%s**.\n"+
			"📣 Captains: %s\n"+
			"Check **My Team → Requests** to accept or deny.",
		requester.Name,
		target.Name,
		mentions,
	))

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Challenge request sent.",
	})
}

func HandleChallengeRespond(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChallengeID uint `json:"challenge_id"`
		Accept      bool `json:"accept"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var ch ChallengeRequest
	if err := DB.First(&ch, req.ChallengeID).Error; err != nil {
		http.Error(w, "Challenge not found", http.StatusNotFound)
		return
	}

	if ch.Status != "Pending" {
		http.Error(w, "Challenge already resolved", http.StatusBadRequest)
		return
	}

	// Load teams
	var requester, target Team
	DB.First(&requester, ch.RequesterTeamID)
	DB.First(&target, ch.TargetTeamID)

	if req.Accept {
		// Accept the challenge
		ch.Status = "Accepted"
		DB.Save(&ch)

		// Add a new match into weekly flow
		CreateWeeklyChallengeMatch(requester.ID, target.ID, ch.Week)

		// Increment requester weekly challenge count
		requester.WeeklyChallengesUsed++
		DB.Save(&requester)

		LogGeneral(fmt.Sprintf(
			"⚔️ **Challenge Accepted!**\n"+
				"**%s** vs **%s** has been added to Week %d matchups.",
			requester.Name,
			target.Name,
			ch.Week,
		))

	} else {
		ch.Status = "Denied"
		DB.Save(&ch)

		LogGeneral(fmt.Sprintf(
			"❌ **Challenge Denied**\n"+
				"%s denied challenge from %s.",
			target.Name,
			requester.Name,
		))
	}

	respondJSON(w, map[string]any{
		"success": true,
		"status":  ch.Status,
	})
}

func HandleToggleChallenges(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID uint `json:"team_id"`
		Allow  bool `json:"allow"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// ✅ FIX — update allow_challenges (NOT join_allowed)
	team.AllowChallenges = req.Allow
	DB.Save(&team)

	LogGeneral(fmt.Sprintf(
		"🔧 **Challenge Setting Updated**\n"+
			"Team **%s** has **%s** challenge requests.",
		team.Name,
		map[bool]string{true: "ENABLED", false: "DISABLED"}[req.Allow],
	))

	respondJSON(w, map[string]any{
		"success": true,
		"allow":   req.Allow,
	})
}

func CreateWeeklyChallengeMatch(teamAID, teamBID uint, week int) error {
	// --- Normalize season ---
	season := strings.TrimSpace(currentSeason)
	if season == "" || !regexp.MustCompile(`^\d+$`).MatchString(season) {
		season = "0" // fallback = preseason
	}

	// --- Normalize week ---
	if week <= 0 {
		week = 1
	}
	weekStr := strconv.Itoa(week)

	now := time.Now()
	systemID := int64(0)

	// --- ALWAYS VALID FORMAT ---
	matchCode := fmt.Sprintf(
		"%s-Week%s-CHAL-%03dvs%03d",
		season, weekStr, teamAID, teamBID,
	)

	match := Match{
		TeamAID:      teamAID,
		TeamBID:      teamBID,
		Season:       season,
		Week:         weekStr,
		Status:       "Scheduled",
		ProposedDate: &now,
		ProposerID:   &systemID,
		MatchCode:    matchCode,
	}

	if err := DB.Create(&match).Error; err != nil {
		log.Printf("❌ Failed to create challenge match: %v", err)
		return err
	}

	log.Printf("⚔️ Challenge match created: %s (%d vs %d)", match.MatchCode, teamAID, teamBID)
	return nil
}
