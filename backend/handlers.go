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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var discordSession *discordgo.Session
var GlobalChallengesEnabled = true

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

func lookupName(id int64) string {
	var p Player
	if err := DB.First(&p, "id = ?", id).Error; err == nil {
		if p.DisplayName != "" {
			return p.DisplayName
		}
		return p.Username
	}
	return fmt.Sprint(id) // fallback to ID
}

func IsPlayerOnCooldown(player Player, ls LeagueSettings) bool {
	if player.LastLeftTeamAt == nil || ls.LastMatchGeneration == nil {
		return false
	}
	return player.LastLeftTeamAt.After(*ls.LastMatchGeneration)
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
	var s LeagueSettings
	if err := DB.First(&s, 1).Error; err != nil {
		s.CurrentWeek = 1
		s.WeeklyChallengeLimit = 1
		s.ChallengesEnabled = true
	}

	minPlayers := getEnvInt("MIN_TEAM_PLAYERS", 3)
	maxPlayers := getEnvInt("MAX_TEAM_PLAYERS", 6)

	respondJSON(w, map[string]any{
		"roster_locked":          rosterLocked,
		"min_team_players":       minPlayers,
		"max_team_players":       maxPlayers,
		"current_week":           s.CurrentWeek,
		"weekly_challenge_limit": s.WeeklyChallengeLimit,
		"challenges_enabled":     s.ChallengesEnabled,
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

// --- Helper: Check if a user has a Discord role ---
func userHasDiscordRole(discordIDStr string, roleID string) bool {
	guildID := getEnv("DISCORD_GUILD_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")

	if guildID == "" || botToken == "" || roleID == "" {
		return false
	}

	req, _ := http.NewRequest("GET",
		fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s",
			guildID, discordIDStr),
		nil)

	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return false
	}

	defer resp.Body.Close()

	var member struct {
		Roles []string `json:"roles"`
	}

	if json.NewDecoder(resp.Body).Decode(&member) != nil {
		return false
	}

	for _, r := range member.Roles {
		if r == roleID {
			return true
		}
	}

	return false
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

	if roleFilter != "" {
		query = query.Where("LOWER(role) = LOWER(?)", roleFilter)
	}

	var rows []raw
	if err := query.Scan(&rows).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "failed to load registered players",
		})
		return
	}

	//casterRoleID := getEnv("DISCORD_CASTER_ROLE_ID", "")
	//modRoleID := getEnv("DISCORD_LEAGUE_MOD_ROLE_ID", "")

	players := make([]map[string]any, 0, len(rows))

	for _, r := range rows {
		// Convert ID to string because Discord IDs are string-based
		idStr := strconv.FormatInt(r.ID, 10)

		// 🔥 Reuse the exact same Discord role logic as /api/me
		isCaster := false
		isMod := false

		players = append(players, map[string]any{
			"id":           idStr,
			"username":     r.Username,
			"display_name": r.DisplayName,
			"role":         r.Role,
			"device":       r.Device,
			"timezone":     r.Timezone,
			"is_caster":    isCaster,
			"is_mod":       isMod,
		})
	}

	respondJSON(w, players)
}

// --- Teams ---
func GetTeams(w http.ResponseWriter, r *http.Request) {
	var teams []Team
	if err := DB.
		Where("name NOT IN ('(No Team)', 'League Sub')").
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
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Rating      int    `json:"rating"`
		OnCooldown  bool   `json:"on_cooldown"`
	}

	var roster []RosterPlayer

	// 🧩 Primary live roster
	err = DB.Table("team_members").
		Select(`
			CAST(players.id AS TEXT) AS id,
			players.username,
			players.display_name,
			team_members.role,
			players.rating
		`).
		Joins("JOIN players ON players.id = team_members.player_id").
		Where("team_members.team_id = ?", teamID).
		Scan(&roster).Error

	if err != nil {
		log.Printf("❌ GetTeam: roster query failed for team %d: %v", teamID, err)
		roster = []RosterPlayer{}
	}

	// 🧩 Fallback: player_history
	if len(roster) == 0 {
		DB.Raw(`
			SELECT
				CAST(p.id AS TEXT) AS id,
				p.username,
				p.display_name,
				ph.role,
				p.rating
			FROM player_history ph
			JOIN players p ON p.id = ph.player_id
			WHERE ph.team_id = ? AND ph.season = ?
		`, teamID, currentSeason).Scan(&roster)
	}

	// --- Apply cooldown
	var ls LeagueSettings
	DB.First(&ls, 1)

	for i := range roster {
		var p Player
		pid, _ := strconv.ParseInt(roster[i].ID, 10, 64)
		DB.First(&p, pid)
		roster[i].OnCooldown = IsPlayerOnCooldown(p, ls)
	}
	// --- Load match history (include numeric ID and MatchCode) ---
	type MatchRow struct {
		ID         string     `json:"id"`
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

	// Add division/tier + convert ID
	for i := range players {
		players[i].IDStr = strconv.FormatInt(players[i].ID, 10)

		div, tier := GetDivisionTier(players[i].Rating)
		players[i].Division = div
		players[i].Tier = tier
	}

	respondJSON(w, players)
}

// --- Team Leaderboard ---
func GetTeamLeaderboard(w http.ResponseWriter, r *http.Request) {
	type TeamRow struct {
		ID       uint   `json:"id"`
		Name     string `json:"name"`
		Status   string `json:"status"`
		Rating   int    `json:"rating"`
		Wins     int    `json:"wins"`
		Losses   int    `json:"losses"`
		Matches  int    `json:"matches"`
		Division string `json:"division"`
		Tier     string `json:"tier"`
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

	// Add division + tier
	for i := range rows {
		div, tier := GetDivisionTier(rows[i].Rating)
		rows[i].Division = div
		rows[i].Tier = tier
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
		OnCooldown  bool   `json:"on_cooldown"`
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

	var ls LeagueSettings
	DB.First(&ls, 1)

	for i := range roster {
		var p Player
		pid, _ := strconv.ParseInt(roster[i].ID, 10, 64)
		DB.First(&p, pid)

		roster[i].OnCooldown = IsPlayerOnCooldown(p, ls)
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
			"locked":                 team.Locked,
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
	var ls LeagueSettings
	DB.First(&ls, 1)

	team := Team{
		Name:            req.Name,
		Status:          "Active",
		Rating:          getEnvInt("DEFAULT_TEAM_RATING", 1000),
		Wins:            0,
		Losses:          0,
		Matches:         0,
		AllowChallenges: ls.ChallengesEnabled, // ⬅ IMPORTANT FIX
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
		PlayerID:   playerID,
		TeamID:     team.ID,
		TeamName:   team.Name,
		Role:       "Captain",
		Season:     currentSeason,
		IsTeamJoin: true,
	})

	syncDiscordRolesForPlayer(playerID)

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
				PlayerID:   int64(jr.PlayerID),
				TeamID:     jr.TeamID,
				TeamName:   team.Name,
				Role:       "Member",
				Season:     currentSeason,
				IsTeamJoin: true,
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

	// session user
	session, _ := store.Get(r, "session")
	discordIDStr, ok := session.Values["discord_id"].(string)
	if !ok || discordIDStr == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	// load team
	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// load membership
	var member TeamMember
	if err := DB.Where("team_id = ? AND player_id = ?", req.TeamID, playerID).
		First(&member).Error; err != nil {
		http.Error(w, "Not a team member", http.StatusForbidden)
		return
	}

	wasCaptain := member.Role == "Captain"

	// remove member from team
	if err := DB.Delete(&member).Error; err != nil {
		http.Error(w, "Failed to leave team", http.StatusInternalServerError)
		return
	}

	// cooldown
	now := time.Now()
	DB.Model(&Player{}).
		Where("id = ?", playerID).
		Update("last_left_team_at", now)

	// remaining members
	var remaining []TeamMember
	DB.Where("team_id = ?", req.TeamID).Find(&remaining)

	// ------------------------------------
	// EMPTY TEAM → DISBAND
	// ------------------------------------
	if len(remaining) == 0 {
		DB.Model(&Team{}).
			Where("id = ?", req.TeamID).
			Update("status", "Disbanded")

		SendDiscordLog(fmt.Sprintf(
			"🗑️ **Team Disbanded:** **%s (#%d)** — last member left",
			team.Name, team.ID,
		))

		// sync leaving player (remove captain/co-captain roles)
		syncDiscordRolesForPlayer(playerID)

		respondJSON(w, map[string]any{
			"success": true,
			"message": "Team disbanded",
		})
		return
	}

	// ------------------------------------
	// CAPTAIN LEFT → AUTO-PROMOTE
	// ------------------------------------
	if wasCaptain {
		var next TeamMember

		// prefer Co-Captain
		if err := DB.Where("team_id = ? AND role = ?", req.TeamID, "Co-Captain").
			First(&next).Error; err == nil {

			DB.Model(&TeamMember{}).
				Where("team_id = ? AND player_id = ?", req.TeamID, next.PlayerID).
				Update("role", "Captain")

		} else if err := DB.Where("team_id = ?", req.TeamID).
			First(&next).Error; err == nil {

			DB.Model(&TeamMember{}).
				Where("team_id = ? AND player_id = ?", req.TeamID, next.PlayerID).
				Update("role", "Captain")
		}

		// sync newly promoted captain
		if next.PlayerID != 0 {
			syncDiscordRolesForPlayer(next.PlayerID)
		}
	}

	// sync leaving player LAST (removes all team roles)
	syncDiscordRolesForPlayer(playerID)

	SendDiscordLog(fmt.Sprintf(
		"🚪 <@%s> has left team **%s (#%d)**",
		discordIDStr, team.Name, team.ID,
	))

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Left team successfully",
	})
}

// --- Public: Get all matches grouped by season + week ---
func HandlePublicMatches(w http.ResponseWriter, r *http.Request) {
	seasonFilter := r.URL.Query().Get("season")
	weekFilter := r.URL.Query().Get("week")

	type MatchRow struct {
		ID            uint
		MatchCode     string
		TeamAID       uint
		TeamA         string
		TeamBID       uint
		TeamB         string
		ScheduledDate *time.Time
		Status        string
		WinnerID      *uint
		LoserID       *uint
		IsFinals      bool
		Archived      bool
		Bracket       string
		BracketRound  int
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
			m.loser_id,
			m.is_finals,
			m.archived,
			m.bracket,
			m.bracket_round
		FROM matches m
		JOIN teams t1 ON t1.id = m.team_a_id
		JOIN teams t2 ON t2.id = m.team_b_id
		ORDER BY
			-- Season DESC
			CASE 
				WHEN split_part(m.match_code, '-', 1) ~ '^[0-9]+$' 
				THEN CAST(split_part(m.match_code, '-', 1) AS INTEGER)
				ELSE 0
			END DESC,

			-- Finals FIRST
			CASE
				WHEN m.is_finals OR m.match_code ILIKE '%-Finals-%' THEN -1
				WHEN m.match_code ILIKE '%Week%' 
					AND split_part(split_part(m.match_code, 'Week', 2), '-', 1) ~ '^[0-9]+$'
				THEN CAST(split_part(split_part(m.match_code, 'Week', 2), '-', 1) AS INTEGER)
				ELSE 999
			END ASC,

			m.id DESC
	`).Scan(&rows).Error; err != nil {
		log.Printf("❌ HandlePublicMatches query failed: %v", err)
		http.Error(w, "failed to fetch matches", http.StatusInternalServerError)
		return
	}

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
		CastActive    bool       `json:"cast_active"`
		IsFinals      bool       `json:"is_finals"`
		Archived      bool       `json:"archived"`
		Bracket       string     `json:"bracket"`
		BracketRound  int        `json:"bracket_round"`
	}

	var normalized []PublicMatch

	for _, m := range rows {
		seasonLabel, weekLabel := deriveSeasonAndWeek(m.MatchCode)

		// Detect finals
		isFinal := m.IsFinals || strings.Contains(strings.ToLower(m.MatchCode), "-finals-")

		if isFinal {
			weekLabel = "Finals"
		}

		// 🔒 HARD RULES
		// If match_code does NOT start with a number → Preseason
		parts := strings.Split(m.MatchCode, "-")
		if len(parts) == 0 || !regexp.MustCompile(`^\d+$`).MatchString(parts[0]) {
			seasonLabel = "Preseason"
		}

		if seasonLabel == "" || strings.EqualFold(seasonLabel, "null") {
			seasonLabel = "Preseason"
		}
		if weekLabel == "" {
			weekLabel = "Unknown"
		}

		var cast CastLogMulti
		DB.Where("match_id = ?", m.ID).First(&cast)

		normalized = append(normalized, PublicMatch{
			ID:            m.ID,
			MatchCode:     m.MatchCode,
			Season:        seasonLabel,
			Week:          weekLabel,
			TeamAID:       m.TeamAID,
			TeamA:         m.TeamA,
			TeamBID:       m.TeamBID,
			TeamB:         m.TeamB,
			ScheduledDate: m.ScheduledDate,
			Status:        m.Status,
			WinnerID:      m.WinnerID,
			LoserID:       m.LoserID,
			CastActive:    cast.ID != 0,
			IsFinals:      isFinal,
			Archived:      m.Archived,
			Bracket:       m.Bracket,
			BracketRound:  m.BracketRound,
		})
	}

	var filtered []PublicMatch
	for _, m := range normalized {
		if (seasonFilter == "" || strings.EqualFold(m.Season, seasonFilter)) &&
			(weekFilter == "" || m.Week == weekFilter) {
			filtered = append(filtered, m)
		}
	}

	grouped := map[string]map[string][]PublicMatch{}
	for _, m := range filtered {
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

	syncDiscordRolesForPlayer(playerID)

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
		PlayerID FlexibleID `json:"player_id"`
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

	targetID := req.PlayerID.Int64()

	// allowed roles
	validRoles := map[string]bool{
		"Captain":    true,
		"Co-Captain": true,
		"Member":     true,
	}
	if !validRoles[req.Role] {
		http.Error(w, "Invalid role", http.StatusBadRequest)
		return
	}

	// requester must be captain
	role, err := getMemberRole(req.TeamID, requesterID)
	if err != nil || role != "Captain" {
		http.Error(w, "Only Captains can promote players", http.StatusForbidden)
		return
	}

	// cannot demote self
	if requesterID == targetID && req.Role != "Captain" {
		http.Error(w, "Captain cannot demote themselves", http.StatusForbidden)
		return
	}

	// fetch target member
	var member TeamMember
	if err := DB.Where("team_id = ? AND player_id = ?", req.TeamID, targetID).
		First(&member).Error; err != nil {
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	}

	// if promoting to Captain, demote existing Captain → Co-Captain
	if req.Role == "Captain" {
		var oldCap TeamMember
		if err := DB.Where("team_id = ? AND role = ?", req.TeamID, "Captain").
			First(&oldCap).Error; err == nil {

			DB.Model(&TeamMember{}).
				Where("team_id = ? AND player_id = ?", req.TeamID, oldCap.PlayerID).
				Update("role", "Co-Captain")

			// resync old captain later
			defer syncDiscordRolesForPlayer(oldCap.PlayerID)
		}
	}

	// update target role
	DB.Model(&TeamMember{}).
		Where("team_id = ? AND player_id = ?", req.TeamID, targetID).
		Update("role", req.Role)

	// history
	var existing PlayerHistory
	if err := DB.Where(
		"player_id = ? AND team_id = ? AND season = ? AND role = ?",
		targetID, req.TeamID, currentSeason, req.Role,
	).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {

		DB.Create(&PlayerHistory{
			PlayerID: targetID,
			TeamID:   req.TeamID,
			Role:     req.Role,
			Season:   currentSeason,
		})
	}

	// 🔑 SINGLE SOURCE OF TRUTH
	syncDiscordRolesForPlayer(targetID)

	// log
	var team Team
	teamName := fmt.Sprintf("Team #%d", req.TeamID)
	if err := DB.First(&team, req.TeamID).Error; err == nil && team.Name != "" {
		teamName = team.Name
	}

	SendDiscordLog(fmt.Sprintf(
		"⬆️ **Promotion:** <@%d> is now **%s** on **%s**",
		targetID,
		req.Role,
		teamName,
	))

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Member promoted to " + req.Role,
	})
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

	// --- If team becomes inactive, force-disable challenge requests ---
	if req.Status == "Inactive" {
		DB.Model(&Team{}).
			Where("id = ?", req.TeamID).
			Updates(map[string]any{
				"allow_challenges": false,
			})
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

	// --- Load team ---
	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// 🚫 HARD BLOCK: team is locked by moderators
	if team.Locked {
		http.Error(w, "Team is locked by moderators — cannot enable join requests", http.StatusForbidden)
		return
	}

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

	// 🚀 Schedule Discord match channel lifecycle
	if match.ScheduledDate != nil {
		// Initialize Discord session for channel management
		botToken := getEnv("DISCORD_BOT_TOKEN", "")
		if botToken != "" {
			dg, err := discordgo.New("Bot " + botToken)
			if err == nil {
				go scheduleMatchChannel(dg, &match)
			} else {
				log.Printf("⚠️ Failed to create Discord session for match channel: %v", err)
			}
		}
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

	// Normalize: ALWAYS store subs in real TeamA/TeamB order
	if req.TeamID == match.TeamAID {
		// Team A submitted → subA is theirs, subB is opponent
		match.LeagueSubA = subA
		match.LeagueSubB = subB
	} else if req.TeamID == match.TeamBID {
		// Team B submitted → subA and subB must be flipped
		match.LeagueSubA = subB
		match.LeagueSubB = subA
	} else {
		http.Error(w, "Team not part of this match", http.StatusForbidden)
		return
	}

	DB.Model(&match).Updates(map[string]any{
		"league_sub_a": match.LeagueSubA,
		"league_sub_b": match.LeagueSubB,
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

	// ============================================================
	// 🧊 1) Try frozen snapshot from match_rosters
	// ============================================================
	DB.Raw(`
		SELECT player_id, display_name, username, role
		FROM match_rosters
		WHERE match_id = ? AND team_id = ?
		ORDER BY role ASC, display_name ASC
	`, match.ID, match.TeamAID).Scan(&rosterA)

	DB.Raw(`
		SELECT player_id, display_name, username, role
		FROM match_rosters
		WHERE match_id = ? AND team_id = ?
		ORDER BY role ASC, display_name ASC
	`, match.ID, match.TeamBID).Scan(&rosterB)

	// ============================================================
	// 2) Fallback: player_history (same-season)
	// ============================================================
	if len(rosterA) == 0 {
		DB.Raw(`
			SELECT p.id AS player_id, p.display_name, p.username, ph.role
			FROM player_history ph
			JOIN players p ON p.id = ph.player_id
			WHERE ph.team_id = ? AND ph.season = ?
		`, match.TeamAID, currentSeason).Scan(&rosterA)
	}

	if len(rosterB) == 0 {
		DB.Raw(`
			SELECT p.id AS player_id, p.display_name, p.username, ph.role
			FROM player_history ph
			JOIN players p ON p.id = ph.player_id
			WHERE ph.team_id = ? AND ph.season = ?
		`, match.TeamBID, currentSeason).Scan(&rosterB)
	}

	// ============================================================
	// 3) Last resort: live team_members
	// ============================================================
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

	// --- Load Cast Info ---
	var cast CastLogMulti
	castErr := DB.Where("match_id = ?", match.ID).First(&cast).Error

	var casterIDs []int64
	if castErr == nil {
		_ = json.Unmarshal(cast.Casters, &casterIDs)
	}

	// Convert IDs to strings for JSON
	casterStrs := make([]string, 0, len(casterIDs))
	for _, id := range casterIDs {
		casterStrs = append(casterStrs, strconv.FormatInt(id, 10))
	}

	cameraStr := ""
	if castErr == nil && cast.CameraID != 0 {
		cameraStr = strconv.FormatInt(cast.CameraID, 10)
	}

	castData := map[string]any{
		"active":     castErr == nil && len(casterStrs) > 0,
		"casters":    casterStrs,
		"camera":     cameraStr,
		"stream_url": cast.StreamURL,
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
			"league_sub_a":           leagueSubA,
			"league_sub_b":           leagueSubB,
		},
		"teams":      map[string]any{"a": teamA, "b": teamB},
		"map_scores": filtered,
		"roster": map[string]any{
			"a": stringifyRosterPlayers(rosterA),
			"b": stringifyRosterPlayers(rosterB),
		},
		"cast": castData,
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

func deriveSeasonAndWeek(code string) (string, string) {
	parts := strings.Split(code, "-")
	if len(parts) < 2 {
		return "Unknown", "?"
	}

	season := parts[0]

	// Finals detection (MXXX or any "Finals" token)
	if len(parts) >= 3 && strings.EqualFold(parts[1], "Finals") {
		return season, "Finals"
	}

	// Normal Week parsing
	if strings.Contains(code, "Week") {
		after := strings.SplitN(code, "Week", 2)[1]
		week := strings.Split(after, "-")[0]

		week = strings.TrimSpace(week)
		if week == "" {
			return season, "?"
		}
		return season, week
	}

	// Default fallback
	return season, "?"
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
			respondJSON(w, map[string]any{
				"id":             params["id"],
				"username":       "Unregistered Player",
				"display_name":   "Unregistered",
				"role":           "-",
				"rating":         0,
				"wins":           0,
				"losses":         0,
				"matches":        0,
				"current_team":   "",
				"history":        []any{},
				"archived_stats": []any{},
			})
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// --- Current team
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

	// 🟦 TEAM MEMBERSHIP HISTORY (joined/left/promoted)
	type HistoryRow struct {
		Season string `json:"season"`
		TeamID uint   `json:"team_id"`
		Team   string `json:"team"`
		Role   string `json:"role"`
	}

	var history []HistoryRow
	DB.Raw(`
        SELECT season, team_id, team_name AS team, role
        FROM player_history
        WHERE player_id = ?
          AND is_team_join = TRUE
        ORDER BY created_at ASC
    `, playerID).Scan(&history)

	// SEASON STATS HISTORY (archived stats)
	type ArchiveRow struct {
		Season         string `json:"season"`
		ArchiveRating  int    `json:"archive_rating"`
		ArchiveWins    int    `json:"archive_wins"`
		ArchiveLosses  int    `json:"archive_losses"`
		ArchiveMatches int    `json:"archive_matches"`
		ArchiveTeam    string `json:"archive_team"`
	}

	var archived []ArchiveRow
	DB.Raw(`
        SELECT season, archive_rating, archive_wins, archive_losses, archive_matches, archive_team
        FROM player_history
        WHERE player_id = ?
          AND is_team_join = FALSE
        ORDER BY season ASC
    `, playerID).Scan(&archived)

	// --- Final Response
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
		"archived_stats":  archived,
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

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.TeamID == 0 || req.PlayerID.Int64() == 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid payload")
		return
	}

	res := DB.
		Unscoped().
		Where("team_id = ? AND player_id = ?", req.TeamID, req.PlayerID.Int64()).
		Delete(&TeamMember{})

	if res.Error != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to remove member")
		return
	}

	if res.RowsAffected == 0 {
		modJSONErr(w, http.StatusNotFound, "player not found on that team")
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": "player removed",
	})
}

// POST /api/mod/player/ban
// body: { "player_id": "<discord id or number>", "reason": "optional" }
func ModPlayerBan(w http.ResponseWriter, r *http.Request) {
	_, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}

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
			"⛔ **Player Banned:** <@%d>%s",
			p.ID,
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
	_, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}

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
			"♻️ **Player Unbanned:** <@%d>",
			p.ID,
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
func HandleResetTeamLeaderboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	tx := DB.Begin()
	if tx.Error != nil {
		modJSONErr(w, 500, "failed to start transaction")
		return
	}

	if err := tx.Model(&Team{}).
		Session(&gorm.Session{AllowGlobalUpdate: true}).
		Updates(map[string]any{
			"rating":  800,
			"wins":    0,
			"losses":  0,
			"matches": 0,
		}).Error; err != nil {

		log.Printf("❌ Reset Team Leaderboard Failed: %v", err)
		tx.Rollback()
		modJSONErr(w, 500, "failed to reset team leaderboard")
		return
	}

	if err := tx.Commit().Error; err != nil {
		log.Printf("❌ Commit Failed (Team Reset): %v", err)
		modJSONErr(w, 500, "commit failed")
		return
	}

	LogGeneral("♻️ Team leaderboard reset")
	respondJSON(w, map[string]any{"success": true, "message": "Team leaderboard reset"})
}

func HandleResetPlayerLeaderboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	tx := DB.Begin()
	if tx.Error != nil {
		modJSONErr(w, 500, "failed to start transaction")
		return
	}

	if err := tx.Model(&Player{}).
		Session(&gorm.Session{AllowGlobalUpdate: true}).
		Updates(map[string]any{
			"rating":  800,
			"wins":    0,
			"losses":  0,
			"matches": 0,
		}).Error; err != nil {

		log.Printf("❌ Reset Player Leaderboard Failed: %v", err)
		tx.Rollback()
		modJSONErr(w, 500, "failed to reset player leaderboard")
		return
	}

	if err := tx.Commit().Error; err != nil {
		log.Printf("❌ Commit Failed (Player Reset): %v", err)
		modJSONErr(w, 500, "commit failed")
		return
	}

	LogGeneral("♻️ Player leaderboard reset")
	respondJSON(w, map[string]any{"success": true, "message": "Player leaderboard reset"})
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

	// --- load teams + players (global) ---
	var teams []Team
	var players []Player
	DB.Find(&teams)
	DB.Find(&players)

	// ------------------------------
	// 🔥 Determine CURRENT SEASON
	// ------------------------------
	curSeason := strings.TrimSpace(currentSeason)
	curSeason = strings.ToLower(curSeason)

	switch curSeason {
	case "pre", "preseason":
		curSeason = "Preseason"
	default:
		// if numeric like "1", "2", OK
		// if text "Season 1", extract number
		if strings.HasPrefix(strings.ToLower(curSeason), "season ") {
			curSeason = strings.TrimSpace(strings.TrimPrefix(curSeason, "Season"))
		}
	}

	// ------------------------------
	// 🔥 Season extractor using match_code
	// ------------------------------
	seasonFromCode := func(code string) string {
		parts := strings.Split(code, "-")
		if len(parts) == 0 {
			return "0"
		}
		// If first part is numeric → Season X
		if _, err := strconv.Atoi(parts[0]); err == nil {
			return parts[0]
		}
		// Otherwise preseason
		return "0"
	}

	// ------------------------------
	// 🔥 Load all matches, but only keep the CURRENT SEASON
	// ------------------------------
	var allMatches []Match
	DB.Find(&allMatches)

	var seasonMatches []Match
	for _, m := range allMatches {
		if seasonFromCode(m.MatchCode) == curSeason {
			seasonMatches = append(seasonMatches, m)
		}
	}

	// Attach maps only for the selected matches
	var fullMatches []MatchExport
	for _, m := range seasonMatches {
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

		f.WriteString("\n=== MATCHES (CURRENT SEASON ONLY) ===\n")
		f.WriteString("id,code,teamA,teamB,winner,status,date\n")
		for _, m := range seasonMatches {
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

	// --- Archive ALL Player Stats into player_history ---
	var playersAll []Player
	if err := DB.Find(&playersAll).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to load players for archive")
		return
	}

	for _, p := range playersAll {
		archivePlayerStats(p.ID, curSeason)
	}

	log.Printf("🗃️ Archived stats for %d players into PlayerHistory", len(playersAll))

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

func getAllTeamPings(teamID uint) string {
	var members []TeamMember
	if err := DB.Where("team_id = ?", teamID).Find(&members).Error; err != nil {
		return ""
	}

	pings := ""
	for _, m := range members {
		pings += fmt.Sprintf("<@%d> ", m.PlayerID)
	}
	return pings
}

func applyPlayerStats(teamID uint, won bool) {
	var members []TeamMember
	if err := DB.Where("team_id = ?", teamID).Find(&members).Error; err != nil {
		log.Printf("⚠️ Failed loading members for team %d: %v", teamID, err)
		return
	}

	winPts := getEnvInt("ELO_WIN_POINTS", 25)
	lossPts := getEnvInt("ELO_LOSS_POINTS", -25)

	for _, m := range members {
		var p Player
		if err := DB.First(&p, m.PlayerID).Error; err != nil {
			log.Printf("⚠️ Player %d missing: %v", m.PlayerID, err)
			continue
		}

		p.Matches++

		if won {
			p.Wins++
			p.Rating += winPts
		} else {
			p.Losses++
			p.Rating += lossPts
		}

		if p.Rating < 0 {
			p.Rating = 0
		}

		DB.Save(&p)
	}
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

	// Reload match to ensure sub IDs are fresh
	DB.First(&match, match.ID)

	// --- Generate stable score hash (scores + subs) ---
	calcHash := func(scores []MatchScore, subA *int64, subB *int64) string {
		var sb strings.Builder

		for _, m := range scores {
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

	// --- FIRST confirmation stores the hash ---
	if match.ScoreHash == "" {
		match.ScoreHash = currentHash
	} else {
		// --- SECOND confirmation must match the hash ---
		if match.ScoreHash != currentHash {

			// Overwrite scores with the NEW submission
			DB.Where("match_id = ?", match.ID).Delete(&MatchScore{})

			for _, s := range maps {
				DB.Create(&MatchScore{
					MatchID:    match.ID,
					MapNumber:  s.MapNumber,
					Gamemode:   s.Gamemode,
					TeamAScore: s.TeamAScore,
					TeamBScore: s.TeamBScore,
				})
			}

			match.TeamAScoreConfirmed = false
			match.TeamBScoreConfirmed = false
			match.ScoreHash = currentHash
			match.Status = "Pending Confirmation"
			DB.Save(&match)

			SendDiscordLog(
				fmt.Sprintf(
					"🔄 **Score Updated:** Team %d submitted a *different* score set for Match #%d.\nNew scores saved — both teams must confirm again.",
					req.TeamID, match.ID,
				),
			)

			respondJSON(w, map[string]any{
				"success": true,
				"status":  "Reset to new scores",
			})
			return
		}
	}

	// --- Mark team confirmed ---
	switch req.TeamID {
	case match.TeamAID:
		match.TeamAScoreConfirmed = true
	case match.TeamBID:
		match.TeamBScoreConfirmed = true
	default:
		http.Error(w, "Team not in match", http.StatusForbidden)
		return
	}

	DB.Save(&match)

	// --- If only one has confirmed ---
	if !(match.TeamAScoreConfirmed && match.TeamBScoreConfirmed) {

		var teamA, teamB Team
		DB.First(&teamA, match.TeamAID)
		DB.First(&teamB, match.TeamBID)

		confirmingTeam := teamA
		opposingTeam := teamB
		if req.TeamID == match.TeamBID {
			confirmingTeam = teamB
			opposingTeam = teamA
		}

		session, _ := store.Get(r, "session")
		discordIDStr, _ := session.Values["discord_id"].(string)
		submitterID, _ := strconv.ParseInt(discordIDStr, 10, 64)

		SendDiscordLog(
			fmt.Sprintf(
				"📝 **%s confirmed scores for Match %s**\n👥 Teams: %s vs %s\n👤 By: <@%d>\n⏳ Waiting on: %s captains",
				confirmingTeam.Name,
				match.MatchCode,
				teamA.Name, teamB.Name,
				submitterID,
				opposingTeam.Name,
			),
		)

		respondJSON(w, map[string]any{
			"success": true,
			"status":  "Pending Confirmation",
		})
		return
	}

	// ============================================================
	// 🏆 BOTH TEAMS CONFIRMED → FINALIZE
	// ============================================================

	// --- Determine match winner ---
	totalA, totalB := 0, 0
	for _, s := range maps {
		if s.TeamAScore > s.TeamBScore {
			totalA++
		} else if s.TeamBScore > s.TeamAScore {
			totalB++
		}
	}

	if totalA != totalB {
		if totalA > totalB {
			match.WinnerID = &match.TeamAID
			match.LoserID = &match.TeamBID
		} else {
			match.WinnerID = &match.TeamBID
			match.LoserID = &match.TeamAID
		}
	}

	match.Status = "Completed"
	DB.Save(&match)

	// 🚀 Finals auto-advance always allowed
	if match.IsFinals && match.WinnerID != nil {
		go advanceFinalsBracket(match)
	}

	// Always snapshot rosters
	snapshotMatchRosters(match.ID, match.TeamAID, match.TeamBID)

	// -------------------------------
	// 🛑 SKIP STATS IF FINALS
	// -------------------------------
	isFinals := match.IsFinals

	if !isFinals && match.WinnerID != nil {
		updateLeaderboards(*match.WinnerID, *match.LoserID)
		applyPlayerStats(*match.WinnerID, true)
		applyPlayerStats(*match.LoserID, false)
	}

	// Sub stats only if NOT finals
	if !isFinals && match.WinnerID != nil {
		var teamA, teamB Team
		DB.First(&teamA, match.TeamAID)
		DB.First(&teamB, match.TeamBID)

		applySubStats := func(subID *int64, won bool, teamID uint, teamName string) {
			if subID == nil {
				return
			}
			var p Player
			if err := DB.First(&p, *subID).Error; err != nil {
				return
			}

			p.Matches++
			if won {
				p.Wins++
				p.Rating += getEnvInt("ELO_WIN_POINTS", 25)
			} else {
				p.Losses++
				p.Rating += getEnvInt("ELO_LOSS_POINTS", -25)
			}
			DB.Save(&p)

			DB.Create(&PlayerHistory{
				PlayerID: *subID,
				TeamID:   teamID,
				TeamName: teamName,
				Role:     "League Sub",
				Season:   currentSeason,
			})
		}

		if *match.WinnerID == match.TeamAID {
			applySubStats(match.LeagueSubA, true, teamA.ID, teamA.Name)
			applySubStats(match.LeagueSubB, false, teamB.ID, teamB.Name)
		} else {
			applySubStats(match.LeagueSubA, false, teamA.ID, teamA.Name)
			applySubStats(match.LeagueSubB, true, teamB.ID, teamB.Name)
		}
	}

	// ============================================================
	// 📦 SEND FINAL EMBED (always for both regular + finals)
	// ============================================================

	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	var finalMaps []MatchScore
	DB.Where("match_id = ?", match.ID).Order("map_number ASC").Find(&finalMaps)

	mapLines := ""
	for _, m := range finalMaps {
		mapLines += fmt.Sprintf(
			"**Map %d (%s)**\n%s %d – %d %s\n\n",
			m.MapNumber, m.Gamemode,
			teamA.Name, m.TeamAScore,
			m.TeamBScore, teamB.Name,
		)
	}

	winnerName := "Tie"
	if match.WinnerID != nil {
		if *match.WinnerID == match.TeamAID {
			winnerName = teamA.Name
		} else {
			winnerName = teamB.Name
		}
	}

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

	content := fmt.Sprintf(
		"🔔 **Finalized Match Results for %s vs %s**\n%s",
		teamA.Name, teamB.Name,
		getAllTeamPings(teamA.ID)+"\n"+getAllTeamPings(teamB.ID),
	)

	desc := fmt.Sprintf(
		"**%s vs %s**\n\n📘 **Match ID**\n%s\n\n%s🧍 **League Subs**\n• %s Sub: **%s**\n• %s Sub: **%s**\n\n🏆 **Winner**\n%s",
		teamA.Name, teamB.Name,
		match.MatchCode,
		mapLines,
		teamA.Name, subAName,
		teamB.Name, subBName,
		winnerName,
	)

	SendScoreEmbedWithPings(content, "🏆 Final Match Result", desc)

	respondJSON(w, map[string]any{
		"success": true,
		"status":  "Completed",
		"message": "Match finalized.",
	})
}

// snapshotMatchRosters freezes the rosters for both teams at the time a match is finalized.
// It is SAFE to call multiple times; it will no-op if data already exists.
func snapshotMatchRosters(matchID, teamAID, teamBID uint) error {
	if matchID == 0 || (teamAID == 0 && teamBID == 0) {
		// Nothing to do
		return nil
	}

	// If we already have a snapshot, don't duplicate it
	var existing int64
	if err := DB.Model(&MatchRoster{}).
		Where("match_id = ?", matchID).
		Count(&existing).Error; err != nil {
		log.Printf("⚠️ Failed to check existing MatchRoster for match %d: %v", matchID, err)
		return err
	}
	if existing > 0 {
		// Already snapshotted
		return nil
	}

	type row struct {
		PlayerID    int64
		TeamID      uint
		DisplayName string
		Username    string
		Role        string
	}

	var rows []row

	// Pull current team_members + players for both teams
	if err := DB.Table("team_members").
		Select(`
			team_members.player_id AS player_id,
			team_members.team_id AS team_id,
			COALESCE(players.display_name, '') AS display_name,
			COALESCE(players.username, '') AS username,
			COALESCE(team_members.role, '') AS role`).
		Joins("JOIN players ON players.id = team_members.player_id").
		Where("team_members.team_id IN ?", []uint{teamAID, teamBID}).
		Scan(&rows).Error; err != nil {

		log.Printf("⚠️ Failed to load roster for snapshot (match %d): %v", matchID, err)
		return err
	}

	if len(rows) == 0 {
		// No members found — don't treat as fatal, just log
		log.Printf("⚠️ No roster rows found to snapshot for match %d", matchID)
		return nil
	}

	for _, r := range rows {
		mr := MatchRoster{
			MatchID:     matchID,
			TeamID:      r.TeamID,
			PlayerID:    r.PlayerID,
			DisplayName: r.DisplayName,
			Username:    r.Username,
			Role:        r.Role,
		}
		if err := DB.Create(&mr).Error; err != nil {
			log.Printf("⚠️ Failed to insert MatchRoster (match %d, player %d): %v", matchID, r.PlayerID, err)
			// Don't return immediately; try to insert the rest
		}
	}

	return nil
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
		Updates(map[string]any{
			"status":                 "Inactive",
			"allow_challenges":       false,
			"weekly_challenges_used": 0,
		}).Error; err != nil {

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

// POST /api/mod/team/add-player
func ModAddPlayerToTeam(w http.ResponseWriter, r *http.Request) {
	type Body struct {
		TeamID   uint   `json:"team_id"`
		PlayerID string `json:"player_id"`
		Role     string `json:"role"`
	}

	var req Body

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.TeamID == 0 || strings.TrimSpace(req.PlayerID) == "" {
		http.Error(w, "Missing team ID or player ID", http.StatusBadRequest)
		return
	}

	// Convert PlayerID to int64
	pid, err := strconv.ParseInt(req.PlayerID, 10, 64)
	if err != nil {
		http.Error(w, "Invalid player ID format", http.StatusBadRequest)
		return
	}

	// Check if player already on a team
	var existing TeamMember
	if err := DB.Where("player_id = ?", pid).First(&existing).Error; err == nil {
		http.Error(w, "Player is already on a team", http.StatusBadRequest)
		return
	}

	// Normalize role
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "Member"
	}

	// Insert new team membership
	newMember := TeamMember{
		TeamID:   req.TeamID,
		PlayerID: pid,
		Role:     role,
	}

	if err := DB.Create(&newMember).Error; err != nil {
		http.Error(w, "Failed to add player to team", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"success":   true,
		"team_id":   req.TeamID,
		"player_id": req.PlayerID,
		"role":      req.Role,
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

	// --- session ---
	session, _ := store.Get(r, "session")
	discordIDStr, ok := session.Values["discord_id"].(string)
	if !ok || strings.TrimSpace(discordIDStr) == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// --- player exists ---
	var player Player
	if err := DB.First(&player, "id = ?", discordIDStr).Error; err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// --- env ---
	casterRoleID := getEnv("DISCORD_CASTER_ROLE_ID", "")
	guildID := getEnv("DISCORD_GUILD_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")
	if casterRoleID == "" || guildID == "" || botToken == "" {
		http.Error(w, "Caster role not configured", http.StatusInternalServerError)
		return
	}

	// --- verify caster role ---
	isCaster := false
	{
		url := fmt.Sprintf(
			"https://discord.com/api/v10/guilds/%s/members/%s",
			guildID,
			discordIDStr,
		)
		req2, _ := http.NewRequest("GET", url, nil)
		req2.Header.Set("Authorization", "Bot "+botToken)

		resp, err := http.DefaultClient.Do(req2)
		if err == nil && resp != nil && resp.StatusCode == 200 {
			defer resp.Body.Close()
			var member struct {
				Roles []string `json:"roles"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&member)
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

	// --- load match ---
	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// --- status rules ---
	finalStatuses := map[string]bool{
		"Finished":       true,
		"Completed":      true,
		"Forfeit":        true,
		"Double Forfeit": true,
		"Cancelled":      true,
	}

	if finalStatuses[strings.TrimSpace(match.Status)] && !match.IsFinals {
		respondJSON(w, map[string]any{
			"success": true,
			"message": "Match already final — cast saved only.",
		})
		return
	}

	if !match.IsFinals && match.Status != "Scheduled" {
		http.Error(w, "Match is not scheduled for casting.", http.StatusForbidden)
		return
	}

	// --- caster ID ---
	casterID, err := strconv.ParseInt(discordIDStr, 10, 64)
	if err != nil || casterID == 0 {
		http.Error(w, "Invalid caster ID", http.StatusBadRequest)
		return
	}

	// -------------------------------------------------
	// ✅ SAVE CASTER (SOURCE OF TRUTH)
	// -------------------------------------------------
	casters, wasNew := upsertAddCaster(match.ID, casterID)

	// -------------------------------------------------
	// ✅ APPLY DISCORD PERMS IMMEDIATELY (IF CHANNEL EXISTS)
	// -------------------------------------------------
	addedToChannel := false
	channelID := ""

	if match.DiscordChannelID != nil &&
		strings.TrimSpace(*match.DiscordChannelID) != "" &&
		discordSession != nil {

		channelID = *match.DiscordChannelID

		// Always add MEMBER overwrite (specific caster)
		addCasterToExistingChannel(discordSession, channelID, casterID)

		if len(casters) > 0 {
			ensureCasterRoleOverwrite(discordSession, channelID)
		}

		addedToChannel = true
	}

	respondJSON(w, map[string]any{
		"success":          true,
		"saved":            wasNew,
		"added_to_channel": addedToChannel,
		"channel_id":       channelID,
		"casters_count":    len(casters),
	})
}

// Inserts/loads CastLogMulti for match and appends casterID if missing.
// Returns current caster list and whether it was newly added.
func upsertAddCaster(matchID uint, casterID int64) ([]int64, bool) {
	var multi CastLogMulti
	_ = DB.Where("match_id = ?", matchID).First(&multi).Error // ok if not found

	var casterIDs []int64
	if len(multi.Casters) > 0 {
		_ = json.Unmarshal(multi.Casters, &casterIDs)
	}
	// de-dupe
	for _, id := range casterIDs {
		if id == casterID {
			return casterIDs, false
		}
	}
	casterIDs = append(casterIDs, casterID)

	b, _ := json.Marshal(casterIDs)

	// create if missing
	if multi.ID == 0 {
		multi.MatchID = matchID
		multi.Casters = b
		_ = DB.Create(&multi).Error
	} else {
		_ = DB.Model(&multi).Update("casters", b).Error
	}

	return casterIDs, true
}

// Adds a member permission overwrite to an existing channel via Discord HTTP API.
// Crash-safe: returns error instead of panicking.
func addMemberOverwriteHTTP(channelID string, userID int64, botToken string) error {
	if channelID == "" || userID == 0 || botToken == "" {
		return fmt.Errorf("missing channelID/userID/botToken")
	}

	// Same perms you use in createMatchChannel for players:
	allow := discordgo.PermissionViewChannel |
		discordgo.PermissionSendMessages |
		discordgo.PermissionReadMessageHistory

	deny := int64(0)

	payload := map[string]any{
		"type":  1, // member
		"allow": fmt.Sprint(allow),
		"deny":  fmt.Sprint(deny),
	}

	b, _ := json.Marshal(payload)

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/permissions/%d", channelID, userID)
	req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(b))
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("⚠️ addMemberOverwriteHTTP error: %v", err)
		return err
	}
	defer resp.Body.Close()

	// Discord returns 204 No Content on success
	if resp.StatusCode != 204 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("⚠️ addMemberOverwriteHTTP failed: %s %s", resp.Status, string(bodyBytes))
		return fmt.Errorf("discord api status %d", resp.StatusCode)
	}

	return nil
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

	// 🔥 Ensure randomness
	rand.Seed(time.Now().UnixNano())
	sides := []string{"HEADS", "TAILS"}
	result := sides[rand.Intn(2)]

	// Determine winner
	var winner string

	if result == call {
		// The caller wins
		if req.TeamID == match.TeamAID {
			winner = "A"
		} else {
			winner = "B"
		}
	} else {
		// The other team wins
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

	// Load both teams
	var requester, target Team
	if err := DB.First(&requester, req.RequesterTeamID).Error; err != nil {
		http.Error(w, "Requester team not found", http.StatusNotFound)
		return
	}
	if err := DB.First(&target, req.TargetTeamID).Error; err != nil {
		http.Error(w, "Target team not found", http.StatusNotFound)
		return
	}

	// 🔥 Global challenge enable check
	var settings LeagueSettings
	if err := DB.First(&settings, 1).Error; err != nil {
		settings.ChallengesEnabled = true
	}
	if !settings.ChallengesEnabled {
		http.Error(w, "Challenge matches are disabled league-wide", http.StatusForbidden)
		return
	}

	// ❌ Cannot challenge yourself
	if requester.ID == target.ID {
		http.Error(w, "Cannot challenge your own team", http.StatusBadRequest)
		return
	}

	// ❌ Reject if requester team is inactive or disbanded
	if requester.Status != "Active" {
		http.Error(w, "Your team must be active to issue challenges.", http.StatusForbidden)
		return
	}

	// ❌ Reject if target team is inactive or disbanded
	if target.Status != "Active" {
		http.Error(w, "That team is not active and cannot receive challenges.", http.StatusForbidden)
		return
	}

	// ❌ Reject if target has challenge toggle off
	if !target.AllowChallenges {
		http.Error(w, "Target team does not allow challenges", http.StatusForbidden)
		return
	}

	// 🔥 Weekly challenge limit
	if requester.WeeklyChallengesUsed >= settings.WeeklyChallengeLimit {
		http.Error(w, "Weekly challenge limit reached", http.StatusForbidden)
		return
	}

	// Prevent duplicate pending challenges
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

	// 🔒 GLOBAL OVERRIDE — block all toggles if global challenges disabled
	var settings LeagueSettings
	DB.First(&settings)

	if !settings.ChallengesEnabled {
		// Force team challenge toggle OFF
		DB.Model(&team).Update("allow_challenges", false)

		respondJSON(w, map[string]any{
			"success": false,
			"allow":   false,
			"message": "Challenge matches are currently globally disabled by league moderators.",
		})
		return
	}

	// 🔒 TEAM-LEVEL LOCK — inactive teams cannot toggle
	if team.Status != "Active" {
		DB.Model(&team).Update("allow_challenges", false)

		respondJSON(w, map[string]any{
			"success": false,
			"allow":   false,
			"message": "Inactive teams cannot enable challenge requests.",
		})
		return
	}

	// ✅ Apply toggle normally when allowed
	DB.Model(&team).Update("allow_challenges", req.Allow)

	LogGeneral(fmt.Sprintf(
		"🔧 **Challenge Setting Updated**\n"+
			"Team **%s** has **%s** challenge requests.",
		team.Name,
		map[bool]string{true: "ENABLED", false: "DISABLED"}[req.Allow],
	))

	respondJSON(w, map[string]any{
		"success": true,
		"allow":   req.Allow,
		"message": "Team challenge setting updated.",
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

func HandleModSyncRoles(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	rolePlayer := os.Getenv("DISCORD_PLAYER_ROLE_ID")
	roleSub := os.Getenv("DISCORD_LEAGUE_SUB_ROLE_ID")
	roleCaptain := os.Getenv("DISCORD_CAPTAIN_ROLE_ID")
	roleCoCaptain := os.Getenv("DISCORD_CO_CAPTAIN_ROLE_ID")

	guildID := os.Getenv("DISCORD_GUILD_ID")
	botToken := os.Getenv("DISCORD_BOT_TOKEN")

	if guildID == "" || botToken == "" {
		http.Error(w, "Missing Discord bot env vars", http.StatusInternalServerError)
		return
	}

	// Load players
	var players []Player
	DB.Find(&players)

	// Load team roles
	var members []TeamMember
	DB.Find(&members)

	teamRole := map[int64]string{}
	for _, m := range members {
		teamRole[m.PlayerID] = m.Role
	}

	for _, p := range players {
		pid := strconv.FormatInt(p.ID, 10)

		// Queue worker: fetch member → compute → update
		queueRoleJob(func() {
			// 1. Fetch existing roles
			url := "https://discord.com/api/v10/guilds/" + guildID + "/members/" + pid
			req, _ := http.NewRequest("GET", url, nil)
			req.Header.Set("Authorization", "Bot "+botToken)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Println("❌ Failed fetching Discord member:", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 404 {
				log.Println("⚠️ Member", pid, "not in guild — skipping")
				return
			}

			var member struct {
				Roles []string `json:"roles"`
			}
			json.NewDecoder(resp.Body).Decode(&member)

			have := map[string]bool{}
			for _, r := range member.Roles {
				have[r] = true
			}

			// 2. Determine which roles they SHOULD have
			need := map[string]bool{}

			if p.Role == "Player" {
				need[rolePlayer] = true
			}
			if p.Role == "League Sub" {
				need[roleSub] = true
			}

			switch teamRole[p.ID] {
			case "Captain":
				need[roleCaptain] = true
			case "Co-Captain":
				need[roleCoCaptain] = true
			}

			// 3. Compute roles to add/remove
			for role := range need {
				if !have[role] {
					r := role
					queueRoleJob(func() {
						DiscordAddRole(pid, r)
					})
				}
			}

			for role := range have {
				if role == rolePlayer || role == roleSub || role == roleCaptain || role == roleCoCaptain {
					if !need[role] {
						r := role
						queueRoleJob(func() {
							DiscordRemoveRole(pid, r)
						})
					}
				}
			}
		})
	}

	// SendDiscordLog("🔄 **Discord Roles Synced** — All roles recalculated safely.")

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Role sync started — may take ~1 minute depending on player count",
	})
}

func LoadLeagueSettings() {
	var s LeagueSettings
	if err := DB.First(&s).Error; err != nil {
		// create default row
		s.ChallengesEnabled = true
		DB.Create(&s)
	}
	GlobalChallengesEnabled = s.ChallengesEnabled
}

func HandleModSetChallenges(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	GlobalChallengesEnabled = req.Enabled

	// Save to DB
	DB.Model(&LeagueSettings{}).Where("id = 1").Update("challenges_enabled", req.Enabled)

	if !req.Enabled {
		// 🔥 force-disable all team challenge settings
		DB.Model(&Team{}).Update("allow_challenges", false)
	}

	if req.Enabled {
		SendDiscordLog("⚔️ **Global Challenges Enabled** — Teams may toggle their challenge settings again.")
	} else {
		SendDiscordLog("🛑 **Global Challenges Disabled** — All team challenge toggles have been turned off.")
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Global challenge settings updated.",
	})
}

func HandleEnableGlobalChallenges(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	DB.Model(&LeagueSettings{}).Where("id = 1").Update("challenges_enabled", true)

	LogGeneral("⚔️ **Global Challenges Enabled** — teams may toggle challenges again.")
	respondJSON(w, map[string]any{"success": true})
}

func HandleDisableGlobalChallenges(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	// Disable global toggle value
	DB.Model(&LeagueSettings{}).Where("id = 1").Update("challenges_enabled", false)

	// Force ALL teams' challenge toggle OFF (GORM-safe)
	DB.Model(&Team{}).Where("1 = 1").Update("allow_challenges", false)

	LogGeneral("🛑 **Global Challenges Disabled** — Challenge requests cleared for ALL teams.")

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Global challenges disabled; all teams reset to not accepting challenges.",
	})
}

func HandleGetLeagueSettings(w http.ResponseWriter, r *http.Request) {
	var s LeagueSettings

	if err := DB.First(&s).Error; err != nil {
		// fallback default
		respondJSON(w, map[string]any{
			"challenges_enabled": true,
		})
		return
	}

	respondJSON(w, map[string]any{
		"challenges_enabled": s.ChallengesEnabled,
	})
}

func HandleSetCast(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID   uint     `json:"match_id"`
		Casters   []string `json:"casters"`
		CameraID  string   `json:"camera_id"`
		StreamURL string   `json:"stream_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.MatchID == 0 || len(req.Casters) == 0 || strings.TrimSpace(req.CameraID) == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Convert casters → int64 slice
	casterIDs := make([]int64, 0, len(req.Casters))
	for _, idStr := range req.Casters {
		idStr = strings.TrimSpace(idStr)
		if idStr == "" {
			continue
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid caster ID", http.StatusBadRequest)
			return
		}
		casterIDs = append(casterIDs, id)
	}

	if len(casterIDs) == 0 {
		http.Error(w, "No valid caster IDs", http.StatusBadRequest)
		return
	}

	// Convert camera ID
	camID, err := strconv.ParseInt(strings.TrimSpace(req.CameraID), 10, 64)
	if err != nil || camID == 0 {
		http.Error(w, "Invalid camera ID", http.StatusBadRequest)
		return
	}

	// 🔍 Load previous casters (for diff)
	var previousCasters []int64
	{
		var prev CastLogMulti
		if err := DB.Where("match_id = ?", req.MatchID).First(&prev).Error; err == nil {
			if len(prev.Casters) > 0 {
				_ = json.Unmarshal(prev.Casters, &previousCasters)
			}
		}
	}

	// Marshal casters JSONB
	castersJSON, err := json.Marshal(casterIDs)
	if err != nil {
		http.Error(w, "Failed to encode casters", http.StatusInternalServerError)
		return
	}

	// Remove legacy single-cast record
	DB.Where("match_id = ?", req.MatchID).Delete(&CastLog{})

	// Upsert CastLogMulti
	var existing CastLogMulti
	dbErr := DB.Where("match_id = ?", req.MatchID).First(&existing).Error

	if errors.Is(dbErr, gorm.ErrRecordNotFound) {
		// INSERT NEW
		if err := DB.Create(&CastLogMulti{
			MatchID:   req.MatchID,
			Casters:   castersJSON,
			CameraID:  camID,
			StreamURL: strings.TrimSpace(req.StreamURL), // ⭐ SAVE STREAM URL
			CreatedAt: time.Now(),
		}).Error; err != nil {
			http.Error(w, "Failed to save cast", http.StatusInternalServerError)
			return
		}

	} else if dbErr == nil {
		// UPDATE EXISTING
		existing.MatchID = req.MatchID
		existing.Casters = castersJSON
		existing.CameraID = camID
		existing.StreamURL = strings.TrimSpace(req.StreamURL) // ⭐ SAVE STREAM URL

		if err := DB.Save(&existing).Error; err != nil {
			http.Error(w, "Failed to update cast", http.StatusInternalServerError)
			return
		}

	} else {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err == nil &&
		match.DiscordChannelID != nil &&
		strings.TrimSpace(*match.DiscordChannelID) != "" {

		// Build lookup of previous casters
		prev := map[int64]bool{}
		for _, id := range previousCasters {
			prev[id] = true
		}

		// Find newly added casters
		newCasters := []int64{}
		for _, id := range casterIDs {
			if !prev[id] {
				newCasters = append(newCasters, id)
			}
		}

		if match.DiscordChannelID != nil &&
			strings.TrimSpace(*match.DiscordChannelID) != "" {

			channelID := *match.DiscordChannelID
			botToken := os.Getenv("DISCORD_BOT_TOKEN")

			mentions := []string{}
			for _, id := range casterIDs {
				mentions = append(mentions, fmt.Sprintf("<@%d>", id))
			}

			msg := fmt.Sprintf(
				"🎥 **THIS MATCH IS BEING CASTED**\n\n"+
					"Casters: %s\n\n"+
					"⛔ **DO NOT START THE MATCH** until the **casters** give the green light.\n"+
					"🎙️ Please coordinate stream setup here.",
				strings.Join(mentions, " "),
			)

			if err := sendChannelMessageHTTP(channelID, msg, botToken); err != nil {
				log.Printf("❌ Failed to send cast message: %v", err)
			} else {
				log.Printf("✅ Cast message sent to channel %s", channelID)
			}
		}
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Cast saved.",
	})
}

func HandleGetCast(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	matchID, err := strconv.Atoi(idStr)
	if err != nil || matchID <= 0 {
		http.Error(w, "Invalid match ID", http.StatusBadRequest)
		return
	}

	// Priority: Multi-caster log
	var multi CastLogMulti
	if err := DB.Where("match_id = ?", matchID).First(&multi).Error; err == nil {
		var casterIDs []int64
		_ = json.Unmarshal(multi.Casters, &casterIDs)

		// Convert to strings for JSON
		casterStrs := make([]string, 0, len(casterIDs))
		for _, id := range casterIDs {
			casterStrs = append(casterStrs, strconv.FormatInt(id, 10))
		}

		cameraStr := ""
		if multi.CameraID != 0 {
			cameraStr = strconv.FormatInt(multi.CameraID, 10)
		}

		reconcileCastPermissions(uint(matchID))

		respondJSON(w, map[string]any{
			"match_id":   matchID,
			"casters":    casterStrs,
			"camera":     cameraStr,
			"stream_url": multi.StreamURL,
		})
		return
	}

	// Legacy fallback
	var legacy CastLog
	if err := DB.Where("match_id = ?", matchID).First(&legacy).Error; err == nil {
		casterStr := strconv.FormatInt(legacy.CasterID, 10)
		cameraStr := ""
		if legacy.CameraID != 0 {
			cameraStr = strconv.FormatInt(legacy.CameraID, 10)
		}

		respondJSON(w, map[string]any{
			"match_id": matchID,
			"casters":  []string{casterStr},
			"camera":   cameraStr,
		})
		return
	}

	// ✅ Safe empty object (React-friendly)
	respondJSON(w, map[string]any{
		"match_id": matchID,
		"casters":  []string{},
		"camera":   "",
	})
}

func reconcileCastPermissions(matchID uint) {
	var match Match
	if err := DB.First(&match, matchID).Error; err != nil {
		return
	}

	if match.DiscordChannelID == nil ||
		strings.TrimSpace(*match.DiscordChannelID) == "" {
		return
	}

	channelID := *match.DiscordChannelID
	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	casterRoleID := os.Getenv("DISCORD_CASTER_ROLE_ID")

	var cast CastLogMulti
	if err := DB.Where("match_id = ?", matchID).First(&cast).Error; err != nil {
		return
	}

	var casterIDs []int64
	_ = json.Unmarshal(cast.Casters, &casterIDs)
	if len(casterIDs) == 0 {
		return
	}

	// ✅ ROLE overwrite (HTTP, no session needed)
	if casterRoleID != "" {
		_ = addRoleOverwriteHTTP(channelID, casterRoleID, botToken)
	}

	// ✅ MEMBER overwrites (HTTP, no session needed)
	for _, cid := range casterIDs {
		_ = addMemberOverwriteHTTP(channelID, cid, botToken)
	}
}

func addRoleOverwriteHTTP(channelID, roleID, botToken string) error {
	if channelID == "" || roleID == "" || botToken == "" {
		return fmt.Errorf("missing params")
	}

	allow := discordgo.PermissionViewChannel |
		discordgo.PermissionSendMessages |
		discordgo.PermissionReadMessageHistory

	payload := map[string]any{
		"type":  0, // role
		"allow": fmt.Sprint(allow),
		"deny":  "0",
	}

	b, _ := json.Marshal(payload)

	url := fmt.Sprintf(
		"https://discord.com/api/v10/channels/%s/permissions/%s",
		channelID,
		roleID,
	)

	req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(b))
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("role overwrite failed %s %s", resp.Status, body)
	}

	return nil
}

func HandleDeleteCast(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID uint `json:"match_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.MatchID == 0 {
		http.Error(w, "Missing match ID", http.StatusBadRequest)
		return
	}

	DB.Where("match_id = ?", req.MatchID).Delete(&CastLogMulti{})
	DB.Where("match_id = ?", req.MatchID).Delete(&CastLog{})

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Cast removed.",
	})
}

// POST /api/mod/team/adjust-stats
func HandleModAdjustTeamStats(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID  uint `json:"team_id"`
		Rating  int  `json:"rating"`
		Wins    int  `json:"wins"`
		Losses  int  `json:"losses"`
		Matches int  `json:"matches"` // NEW
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

	team.Rating = req.Rating
	team.Wins = req.Wins
	team.Losses = req.Losses
	team.Matches = req.Matches

	if err := DB.Save(&team).Error; err != nil {
		http.Error(w, "Failed to save", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"team_id": team.ID,
	})
}

// POST /api/mod/player/adjust-stats
func HandleModAdjustPlayerStats(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlayerID string `json:"player_id"`
		Rating   int    `json:"rating"`
		Wins     int    `json:"wins"`
		Losses   int    `json:"losses"`
		Matches  int    `json:"matches"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.PlayerID == "" {
		http.Error(w, "Player ID required", http.StatusBadRequest)
		return
	}

	playerID, err := strconv.ParseInt(req.PlayerID, 10, 64)
	if err != nil {
		http.Error(w, "Invalid player ID format", http.StatusBadRequest)
		return
	}

	var p Player
	if err := DB.First(&p, "id = ?", playerID).Error; err != nil {
		http.Error(w, "Player not found", http.StatusNotFound)
		return
	}

	p.Rating = req.Rating
	p.Wins = req.Wins
	p.Losses = req.Losses
	p.Matches = req.Matches

	if err := DB.Save(&p).Error; err != nil {
		http.Error(w, "Failed to update player stats", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"success":   true,
		"player_id": p.ID,
	})
}

// GET /api/mod/player/stats?id=123
func HandleModGetPlayerStats(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing player ID", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid player ID", http.StatusBadRequest)
		return
	}

	var p Player
	if err := DB.First(&p, "id = ?", id).Error; err != nil {
		http.Error(w, "Player not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"id":      p.ID,
		"rating":  p.Rating,
		"wins":    p.Wins,
		"losses":  p.Losses,
		"matches": p.Matches,
	})
}

// GET /api/mod/team/stats?id=123
func HandleModGetTeamStats(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing team ID", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid team ID", http.StatusBadRequest)
		return
	}

	var t Team
	if err := DB.First(&t, id).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"id":      t.ID,
		"rating":  t.Rating,
		"wins":    t.Wins,
		"losses":  t.Losses,
		"matches": t.Matches,
	})
}

// GET /api/mod/team/members?id=123
func HandleModGetTeamMembers(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing team ID", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid team ID", http.StatusBadRequest)
		return
	}

	type Member struct {
		PlayerID    string `json:"player_id"`
		DisplayName string `json:"display_name"`
		Username    string `json:"username"`
		Role        string `json:"role"`
	}

	members := []Member{}

	err = DB.Raw(`
        SELECT 
            CAST(p.id AS TEXT) AS player_id,
            p.display_name,
            p.username,
            tm.role
        FROM team_members tm
        JOIN players p ON p.id = tm.player_id
        WHERE tm.team_id = ?
        ORDER BY 
            CASE WHEN tm.role = 'Captain' THEN 1
                 WHEN tm.role = 'Co-Captain' THEN 2
                 ELSE 3 END,
            p.display_name ASC
    `, id).Scan(&members).Error

	if err != nil {
		http.Error(w, "Failed to load team members", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(members)
}

// POST /api/mod/team/set-role
func HandleModSetTeamRole(w http.ResponseWriter, r *http.Request) {
	type Body struct {
		TeamID   uint   `json:"team_id"`
		PlayerID string `json:"player_id"`
		Role     string `json:"role"`
	}

	var req Body

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.TeamID == 0 || strings.TrimSpace(req.PlayerID) == "" {
		http.Error(w, "Missing team_id or player_id", http.StatusBadRequest)
		return
	}

	// Validate role
	validRoles := map[string]bool{
		"Captain":    true,
		"Co-Captain": true,
		"Member":     true,
	}

	if !validRoles[req.Role] {
		http.Error(w, "Invalid role", http.StatusBadRequest)
		return
	}

	// Convert player ID to int64 safely
	pid, err := strconv.ParseInt(req.PlayerID, 10, 64)
	if err != nil {
		http.Error(w, "Invalid player ID format", http.StatusBadRequest)
		return
	}

	// Confirm membership
	var member TeamMember
	if err := DB.Where("team_id = ? AND player_id = ?", req.TeamID, pid).First(&member).Error; err != nil {
		http.Error(w, "Player is not on this team", http.StatusNotFound)
		return
	}

	// Update role
	if err := DB.Model(&member).Update("role", req.Role).Error; err != nil {
		http.Error(w, "Failed to update role", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"success":   true,
		"team_id":   req.TeamID,
		"player_id": req.PlayerID,
		"new_role":  req.Role,
	})
}

// POST /api/mod/team/promote-captain
func HandleModPromoteToCaptain(w http.ResponseWriter, r *http.Request) {
	type Body struct {
		TeamID   uint   `json:"team_id"`
		PlayerID string `json:"player_id"`
	}

	var req Body

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.TeamID == 0 || strings.TrimSpace(req.PlayerID) == "" {
		http.Error(w, "Missing team_id or player_id", http.StatusBadRequest)
		return
	}

	// Convert to int64 safely
	pid, err := strconv.ParseInt(req.PlayerID, 10, 64)
	if err != nil {
		http.Error(w, "Invalid player ID format", http.StatusBadRequest)
		return
	}

	// Load all team members
	var members []TeamMember
	if err := DB.Where("team_id = ?", req.TeamID).Find(&members).Error; err != nil {
		http.Error(w, "Failed to load team members", http.StatusInternalServerError)
		return
	}

	if len(members) == 0 {
		http.Error(w, "Team has no members", http.StatusBadRequest)
		return
	}

	var newCaptain *TeamMember
	var oldCaptain *TeamMember

	for i := range members {
		if members[i].PlayerID == pid {
			newCaptain = &members[i]
		}
		if members[i].Role == "Captain" {
			oldCaptain = &members[i]
		}
	}

	if newCaptain == nil {
		http.Error(w, "Player not found on this team", http.StatusNotFound)
		return
	}

	// If the selected player is already Captain
	if newCaptain.Role == "Captain" {
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"message": "Player is already Captain",
		})
		return
	}

	// Demote old captain → Co-Captain
	if oldCaptain != nil {
		DB.Model(oldCaptain).Update("role", "Co-Captain")
	}

	// Promote target → Captain
	if err := DB.Model(newCaptain).Update("role", "Captain").Error; err != nil {
		http.Error(w, "Failed to promote to Captain", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"team_id":     req.TeamID,
		"captain_id":  req.PlayerID,
		"old_captain": oldCaptain.PlayerID,
		"message":     "Player promoted to Captain",
	})
}

// POST /api/mod/match/reset-schedule
func ModResetMatchSchedule(w http.ResponseWriter, r *http.Request) {
	type Body struct {
		MatchID uint `json:"match_id"`
	}

	var req Body

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.MatchID == 0 {
		http.Error(w, "Missing match_id", http.StatusBadRequest)
		return
	}

	// Load match
	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// Reset schedule fields
	updates := map[string]any{
		"proposed_date": nil,
		"scheduled_by":  nil,
		"proposer_id":   nil,
		"status":        "Pending",
	}

	if err := DB.Model(&match).Updates(updates).Error; err != nil {
		http.Error(w, "Failed to reset schedule", http.StatusInternalServerError)
		return
	}

	// Fetch team names for logging
	var teamA, teamB Team
	teamAName := fmt.Sprintf("Team #%d", match.TeamAID)
	teamBName := fmt.Sprintf("Team #%d", match.TeamBID)

	if err := DB.First(&teamA, match.TeamAID).Error; err == nil && teamA.Name != "" {
		teamAName = teamA.Name
	}
	if err := DB.First(&teamB, match.TeamBID).Error; err == nil && teamB.Name != "" {
		teamBName = teamB.Name
	}

	// Discord log
	SendDiscordLog(fmt.Sprintf(
		"🔄 **Schedule Reset:** Match **%s** (%s vs %s) has been reset to **Pending**.",
		match.MatchCode,
		teamAName,
		teamBName,
	))

	respondJSON(w, map[string]any{
		"success":  true,
		"message":  "Match schedule reset",
		"match_id": req.MatchID,
	})
}

// POST /api/mod/team/lock
func HandleModToggleTeamLock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID uint `json:"team_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.TeamID == 0 {
		http.Error(w, "Missing team_id", http.StatusBadRequest)
		return
	}

	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// 🔄 Toggle the locked state
	newLockState := !team.Locked

	updateData := map[string]any{
		"locked": newLockState,
	}

	// 🚫 If locking → automatically disable join requests
	if newLockState {
		updateData["join_allowed"] = false
	}

	// 🔥 Apply updates
	if err := DB.Model(&Team{}).Where("id = ?", team.ID).Updates(updateData).Error; err != nil {
		http.Error(w, "Failed to update lock state", http.StatusInternalServerError)
		return
	}

	stateText := "UNLOCKED"
	if newLockState {
		stateText = "LOCKED"
	}

	json.NewEncoder(w).Encode(map[string]any{
		"success":      true,
		"team_id":      team.ID,
		"locked":       newLockState,
		"join_allowed": updateData["join_allowed"], // always false when locked
		"message":      fmt.Sprintf("Team is now %s", stateText),
	})
}

// --- Finals Bracket DTOs ---

type finalsBracketMatch struct {
	TeamA   string `json:"team_a"`
	TeamB   string `json:"team_b"`
	TeamAID uint   `json:"team_a_id"`
	TeamBID uint   `json:"team_b_id"`
	Winner  string `json:"winner"`
}

type finalsBracketResponse struct {
	Winners       [][]finalsBracketMatch `json:"winners"`
	Losers        [][]finalsBracketMatch `json:"losers"`
	GrandFinal    *finalsBracketMatch    `json:"grand_final"`
	ResetPossible bool                   `json:"reset_possible"`
}

// GET /api/finals/teams
func HandleGetFinalsTeams(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	season := strings.TrimSpace(currentSeason)
	if season == "" {
		season = "0"
	}

	var finals []FinalsTeam
	if err := DB.
		Where("season = ?", season).
		Where("NOT EXISTS (SELECT 1 FROM matches m WHERE m.season = finals_teams.season AND m.is_finals = true AND m.archived = true)").
		Where("team_id != 0").
		Order("seed ASC").
		Find(&finals).Error; err != nil {

		log.Printf("❌ HandleGetFinalsTeams: DB error: %v", err)
		http.Error(w, "Failed to load finals teams", http.StatusInternalServerError)
		return
	}

	if len(finals) == 0 {
		respondJSON(w, []any{})
		return
	}

	// Load team names safely
	teamIDs := make([]uint, 0, len(finals))
	for _, ft := range finals {
		if ft.TeamID != 0 {
			teamIDs = append(teamIDs, ft.TeamID)
		}
	}

	teamNames := map[uint]string{}
	if len(teamIDs) > 0 {
		var teams []Team
		if err := DB.Where("id IN ?", teamIDs).Find(&teams).Error; err != nil {
			log.Printf("⚠️ HandleGetFinalsTeams: team lookup failed: %v", err)
		} else {
			for _, t := range teams {
				teamNames[t.ID] = t.Name
			}
		}
	}

	out := make([]map[string]any, 0, len(finals))
	for _, ft := range finals {
		out = append(out, map[string]any{
			"team_id": ft.TeamID,
			"name":    teamNames[ft.TeamID],
			"seed":    ft.Seed,
		})
	}

	respondJSON(w, out)
}

// GET /api/finals/bracket
func HandleGetFinalsBracket(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	season := strings.TrimSpace(currentSeason)
	if season == "" {
		season = "0"
	}

	// -----------------------------------
	// Load all active finals matches
	// -----------------------------------
	var matches []Match
	if err := DB.
		Where("season = ? AND is_finals = true AND archived = false", season).
		Order("bracket ASC, bracket_round ASC, bracket_slot ASC").
		Find(&matches).Error; err != nil {

		http.Error(w, "Failed to load finals bracket", http.StatusInternalServerError)
		return
	}

	if len(matches) == 0 {
		respondJSON(w, map[string]any{
			"matches":        []any{},
			"winners":        [][]any{},
			"losers":         [][]any{},
			"grand_finals":   []any{},
			"reset_possible": false,
		})
		return
	}

	// -----------------------------------
	// Collect team IDs for name lookup
	// -----------------------------------
	teamIDs := map[uint]bool{}
	for _, m := range matches {
		if m.TeamAID != 0 {
			teamIDs[m.TeamAID] = true
		}
		if m.TeamBID != 0 {
			teamIDs[m.TeamBID] = true
		}
		if m.WinnerID != nil && *m.WinnerID != 0 {
			teamIDs[*m.WinnerID] = true
		}
	}

	ids := make([]uint, 0, len(teamIDs))
	for id := range teamIDs {
		ids = append(ids, id)
	}

	teamNames := map[uint]string{}
	if len(ids) > 0 {
		var teams []Team
		if err := DB.Where("id IN ?", ids).Find(&teams).Error; err == nil {
			for _, t := range teams {
				teamNames[t.ID] = t.Name
			}
		}
	}

	nameFor := func(id uint) string {
		if id == 0 {
			return "TBD"
		}
		if n, ok := teamNames[id]; ok {
			return n
		}
		return fmt.Sprintf("Team #%d", id)
	}

	// -----------------------------------
	// Build lookup table for NEXT MATCH
	// -----------------------------------
	matchByKey := map[string]uint{}

	key := func(bracket string, round, slot int) string {
		return fmt.Sprintf("%s:%d:%d", strings.ToLower(bracket), round, slot)
	}

	for _, m := range matches {
		matchByKey[key(m.Bracket, m.BracketRound, m.BracketSlot)] = m.ID
	}

	// -----------------------------------
	// DTO
	// -----------------------------------
	type dto struct {
		ID           uint   `json:"id"`
		MatchCode    string `json:"match_code"`
		Bracket      string `json:"bracket"`
		BracketRound int    `json:"bracket_round"`
		BracketSlot  int    `json:"bracket_slot"`
		Archived     bool   `json:"archived"`

		TeamA   string `json:"team_a"`
		TeamB   string `json:"team_b"`
		TeamAID uint   `json:"team_a_id"`
		TeamBID uint   `json:"team_b_id"`

		WinnerID        *uint  `json:"winner_id"`
		Winner          string `json:"winner"`
		WinnerToMatchID *uint  `json:"winner_to_match_id"`
		LoserToMatchID  *uint  `json:"loser_to_match_id"`
		NextMatchID     *uint  `json:"next_match_id"`
	}

	out := make([]dto, 0, len(matches))
	winners := map[int][]dto{}
	losers := map[int][]dto{}
	grandFinals := []dto{}

	// -----------------------------------
	// Build DTOs WITH next_match_id
	// -----------------------------------
	for _, m := range matches {
		winnerName := "TBD"
		if m.WinnerID != nil && *m.WinnerID != 0 {
			winnerName = nameFor(*m.WinnerID)
		}

		var nextID *uint
		br := strings.ToLower(m.Bracket)

		switch br {
		case "winners", "wb", "upper":
			r := m.BracketRound + 1
			s := (m.BracketSlot + 1) / 2
			if id, ok := matchByKey[key("winners", r, s)]; ok {
				nextID = &id
			}

		case "losers", "lb", "lower":
			r := m.BracketRound + 1
			s := (m.BracketSlot + 1) / 2
			if id, ok := matchByKey[key("losers", r, s)]; ok {
				nextID = &id
			}
		}

		item := dto{
			ID:           m.ID,
			MatchCode:    m.MatchCode,
			Bracket:      br,
			Archived:     m.Archived,
			BracketRound: m.BracketRound,
			BracketSlot:  m.BracketSlot,

			TeamA:   nameFor(m.TeamAID),
			TeamB:   nameFor(m.TeamBID),
			TeamAID: m.TeamAID,
			TeamBID: m.TeamBID,

			WinnerID:        m.WinnerID,
			Winner:          winnerName,
			WinnerToMatchID: m.WinnerToMatchID,
			LoserToMatchID:  m.LoserToMatchID,
			NextMatchID:     nextID,
		}

		out = append(out, item)

		switch br {
		case "winners", "wb", "upper":
			winners[m.BracketRound] = append(winners[m.BracketRound], item)
		case "losers", "lb", "lower":
			losers[m.BracketRound] = append(losers[m.BracketRound], item)
		case "grand_final", "gf", "grandfinal":
			grandFinals = append(grandFinals, item)
		}
	}

	// -----------------------------------
	// Sort rounds
	// -----------------------------------
	sortKeys := func(m map[int][]dto) []int {
		keys := make([]int, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		return keys
	}

	winnerRounds := [][]dto{}
	for _, k := range sortKeys(winners) {
		winnerRounds = append(winnerRounds, winners[k])
	}

	loserRounds := [][]dto{}
	for _, k := range sortKeys(losers) {
		loserRounds = append(loserRounds, losers[k])
	}

	resetPossible := len(grandFinals) == 1 && grandFinals[0].WinnerID != nil

	// -----------------------------------
	// Respond
	// -----------------------------------
	respondJSON(w, map[string]any{
		"matches":        out,
		"winners":        winnerRounds,
		"losers":         loserRounds,
		"grand_finals":   grandFinals,
		"reset_possible": resetPossible,
	})
}

// POST /api/mod/finals/add-team
func HandleModFinalsAddTeam(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		TeamID uint `json:"team_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "Team not found")
		return
	}

	season := strings.TrimSpace(currentSeason)
	if season == "" {
		season = "0"
	}

	// Prevent duplicates
	var existing FinalsTeam
	if err := DB.Where("season = ? AND team_id = ?", season, req.TeamID).First(&existing).Error; err == nil {
		modJSONErr(w, http.StatusBadRequest, "Team already in finals")
		return
	}

	// Get current highest seed
	var maxSeed int64
	DB.Model(&FinalsTeam{}).
		Where("season = ?", season).
		Select("COALESCE(MAX(seed),0)").Scan(&maxSeed)

	ft := FinalsTeam{
		Season: season,
		TeamID: req.TeamID,
		Seed:   int(maxSeed) + 1,
	}

	if err := DB.Create(&ft).Error; err != nil {
		log.Printf("❌ Finals add team: %v", err)
		modJSONErr(w, http.StatusInternalServerError, "Failed to add team to finals")
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"team": map[string]any{
			"team_id": ft.TeamID,
			"name":    team.Name,
			"seed":    ft.Seed,
		},
	})
}

// POST /api/mod/finals/remove-team
func HandleModFinalsRemoveTeam(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		TeamID uint `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "Invalid request")
		return
	}

	season := strings.TrimSpace(currentSeason)
	if season == "" {
		season = "0"
	}

	// Delete the record
	if err := DB.Where("season = ? AND team_id = ?", season, req.TeamID).
		Delete(&FinalsTeam{}).Error; err != nil {

		log.Printf("❌ Finals remove team: %v", err)
		modJSONErr(w, http.StatusInternalServerError, "Failed to remove team")
		return
	}

	// Reseed remaining teams
	var remaining []FinalsTeam
	if err := DB.Where("season = ?", season).Order("seed ASC").Find(&remaining).Error; err == nil {

		for i := range remaining {
			DB.Model(&FinalsTeam{}).
				Where("id = ?", remaining[i].ID).
				Update("seed", i+1)
		}
	}

	respondJSON(w, map[string]any{
		"success": true,
		"removed": req.TeamID,
	})
}

// POST /api/mod/finals/generate
func HandleModFinalsGenerate(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	season := strings.TrimSpace(currentSeason)
	if season == "" {
		season = "0"
	}

	var finalsTeams []FinalsTeam
	if err := DB.Where("season = ?", season).Order("seed ASC").Find(&finalsTeams).Error; err != nil {
		http.Error(w, "Failed to load finals teams", http.StatusInternalServerError)
		return
	}
	if len(finalsTeams) < 2 {
		http.Error(w, "Not enough finals teams", http.StatusBadRequest)
		return
	}

	matches, err := GenerateDoubleElimBracket(finalsTeams, season)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := DB.
		Where("season = ? AND is_finals = true AND archived = false", season).
		Delete(&Match{}).Error; err != nil {

		http.Error(w, "Failed clearing old finals bracket", http.StatusInternalServerError)
		return
	}

	for _, m := range matches {
		if err := DB.Create(&m).Error; err != nil {
			http.Error(w, "Failed saving finals match", http.StatusInternalServerError)
			return
		}
	}

	// reload
	var created []Match
	DB.Where("season = ? AND is_finals = true AND archived = false", season).Find(&created)

	byCode := map[string]uint{}
	for _, m := range created {
		byCode[m.MatchCode] = m.ID
	}

	bracketSize := 1
	for bracketSize < len(finalsTeams) {
		bracketSize *= 2
	}
	if bracketSize > 16 {
		bracketSize = 16
	}

	k := 0
	for (1 << k) < bracketSize {
		k++
	}
	lbRounds := 2*k - 2

	code := func(br string, r, s int) string {
		return fmt.Sprintf("%s-Finals-%s-R%dS%d", season, br, r, s)
	}

	lbMatchCount := func(r int) int {
		exp := 2 + (r-1)/2
		if exp > k {
			exp = k
		}
		n := bracketSize / (1 << exp)
		if n < 1 {
			n = 1
		}
		return n
	}

	patch := func(from string, winTo, loseTo *string) {
		id, ok := byCode[from]
		if !ok {
			return
		}

		var wID *uint
		var lID *uint

		if winTo != nil {
			if x, ok := byCode[*winTo]; ok {
				tmp := x
				wID = &tmp
			}
		}
		if loseTo != nil {
			if x, ok := byCode[*loseTo]; ok {
				tmp := x
				lID = &tmp
			}
		}

		DB.Model(&Match{}).Where("id = ?", id).Updates(map[string]any{
			"winner_to_match_id": wID,
			"loser_to_match_id":  lID,
		})
	}

	for r := 1; r <= k; r++ {
		mc := bracketSize / (1 << r)
		for s := 1; s <= mc; s++ {
			from := code("winners", r, s)

			var winTo *string
			if r < k {
				n := code("winners", r+1, (s+1)/2)
				winTo = &n
			} else {
				n := code("grand_final", 1, 1)
				winTo = &n
			}

			var loseTo *string
			if r == 1 {
				n := code("losers", 1, (s+1)/2)
				loseTo = &n
			} else {
				n := code("losers", 2*(r-1), s)
				loseTo = &n
			}

			patch(from, winTo, loseTo)
		}
	}

	for r := 1; r <= lbRounds; r++ {
		mc := lbMatchCount(r)
		for s := 1; s <= mc; s++ {
			from := code("losers", r, s)

			var winTo *string
			if r < lbRounds {
				ns := s
				if r%2 == 0 {
					ns = (s + 1) / 2
				}
				n := code("losers", r+1, ns)
				winTo = &n
			} else {
				n := code("grand_final", 1, 1)
				winTo = &n
			}

			patch(from, winTo, nil)
		}
	}

	{
		a := code("grand_final", 1, 1)
		b := code("grand_final", 2, 1)
		patch(a, nil, &b)
	}
	{
		a := code("grand_final", 2, 1)
		patch(a, nil, nil)
	}

	ResolveFinalsByes(season)

	respondJSON(w, map[string]string{
		"status":  "ok",
		"message": "Finals bracket generated",
	})
}

func ResolveFinalsByes(season string) {
	changed := true
	for changed {
		changed = false

		var ms []Match
		DB.Where("season = ? AND is_finals = true AND archived = false", season).
			Find(&ms)

		for _, m := range ms {
			if m.WinnerID != nil && *m.WinnerID != 0 {
				continue
			}
			// exactly one team present
			if (m.TeamAID != 0 && m.TeamBID == 0) || (m.TeamAID == 0 && m.TeamBID != 0) {
				w := m.TeamAID
				l := m.TeamBID
				if w == 0 {
					w = m.TeamBID
					l = m.TeamAID
				}

				m.WinnerID = &w
				if l != 0 {
					m.LoserID = &l
				}
				m.Status = "Completed"

				if err := DB.Save(&m).Error; err == nil {
					advanceFinalsBracket(m)
					changed = true
				}
			}
		}
	}
}

// POST /api/mod/finals/reset
func HandleModFinalsReset(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	season := normalizeSeason(currentSeason)
	if season == "" {
		season = "Preseason"
	}

	// Crash prevention: do everything in a transaction
	tx := DB.Begin()
	if tx.Error != nil {
		log.Printf("❌ FinalsReset: failed to start tx: %v", tx.Error)
		http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer func() {
		// crash guard: if panic, rollback
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	// Check if finals are archived for this season
	var archivedCount int64
	if err := tx.Model(&Match{}).
		Where("season = ? AND is_finals = true AND archived = true", season).
		Count(&archivedCount).Error; err != nil {

		tx.Rollback()
		log.Printf("❌ FinalsReset: archive check failed: %v", err)
		http.Error(w, "failed to validate finals state", http.StatusInternalServerError)
		return
	}

	// Always clear finals teams for that season (safe in both modes)
	if err := tx.Where("season = ?", season).Delete(&FinalsTeam{}).Error; err != nil {
		tx.Rollback()
		log.Printf("❌ FinalsReset: failed to delete finals teams: %v", err)
		http.Error(w, "Failed to reset finals teams", http.StatusInternalServerError)
		return
	}

	// ✅ If archived, do SOFT RESET: clear bracket layout only, keep matches + scores
	if archivedCount > 0 {
		if err := tx.Model(&Match{}).
			Where("season = ? AND is_finals = true", season).
			Updates(map[string]any{
				"bracket":       "",
				"bracket_round": 0,
				"bracket_slot":  0,
			}).Error; err != nil {

			tx.Rollback()
			log.Printf("❌ FinalsReset: failed to clear bracket view: %v", err)
			http.Error(w, "Failed to clear bracket view", http.StatusInternalServerError)
			return
		}

		if err := tx.Commit().Error; err != nil {
			log.Printf("❌ FinalsReset: commit failed: %v", err)
			http.Error(w, "Commit failed", http.StatusInternalServerError)
			return
		}

		LogGeneral(fmt.Sprintf("🧹 Finals soft-reset (archived bracket cleared) for %s", season))

		respondJSON(w, map[string]any{
			"success": true,
			"mode":    "soft",
			"season":  season,
			"message": "Archived finals preserved. Bracket view cleared and seeds reset.",
		})
		return
	}

	// ❗ If NOT archived, do HARD RESET (your original behavior): delete finals matches + scores
	var matches []Match
	if err := tx.Where("season = ? AND is_finals = true", season).Find(&matches).Error; err != nil {
		// not fatal, but log
		log.Printf("⚠️ FinalsReset: failed to load finals matches: %v", err)
	} else if len(matches) > 0 {
		ids := make([]uint, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.ID)
		}

		if err := tx.Where("match_id IN ?", ids).Delete(&MatchScore{}).Error; err != nil {
			log.Printf("⚠️ FinalsReset: failed to delete finals map scores: %v", err)
		}
		if err := tx.Where("id IN ?", ids).Delete(&Match{}).Error; err != nil {
			log.Printf("⚠️ FinalsReset: failed to delete finals matches: %v", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		log.Printf("❌ FinalsReset: commit failed: %v", err)
		http.Error(w, "Commit failed", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"mode":    "hard",
		"season":  season,
		"message": "Finals reset (not archived). Matches and scores removed.",
	})
}

// POST /api/mod/finals/update-match
func HandleModFinalsUpdateMatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		MatchID uint   `json:"match_id"`
		Bracket string `json:"bracket"`
		Round   int    `json:"round"`
		Slot    int    `json:"slot"`
		Winner  uint   `json:"winner"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Winner == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var match Match

	// Load by ID OR bracket metadata
	if req.MatchID != 0 {
		if err := DB.First(&match, req.MatchID).Error; err != nil {
			http.Error(w, "Match not found", http.StatusNotFound)
			return
		}
	} else {
		season := strings.TrimSpace(currentSeason)
		if season == "" {
			season = "0"
		}

		if err := DB.Where(
			"season = ? AND is_finals = ? AND bracket = ? AND bracket_round = ? AND bracket_slot = ?",
			season, true, req.Bracket, req.Round, req.Slot,
		).First(&match).Error; err != nil {
			http.Error(w, "Finals match not found", http.StatusNotFound)
			return
		}
	}

	// Validate winner belongs to match
	if req.Winner != match.TeamAID && req.Winner != match.TeamBID {
		http.Error(w, "Winner not in this match", http.StatusBadRequest)
		return
	}

	// Apply winner + loser
	match.WinnerID = &req.Winner
	if req.Winner == match.TeamAID {
		match.LoserID = &match.TeamBID
	} else {
		match.LoserID = &match.TeamAID
	}

	match.Status = "Completed"

	// Save
	if err := DB.Save(&match).Error; err != nil {
		http.Error(w, "Failed to save match", http.StatusInternalServerError)
		return
	}

	advanceFinalsBracket(match)

	respondJSON(w, map[string]any{
		"success": true,
		"updated": match.ID,
	})
}

func insertIntoMatch(matchID uint, teamID uint) {
	if matchID == 0 || teamID == 0 {
		return
	}

	var next Match
	if err := DB.First(&next, matchID).Error; err != nil {
		log.Printf("⚠️ Finals advance: target match %d missing: %v", matchID, err)
		return
	}

	// idempotent: already inserted
	if next.TeamAID == teamID || next.TeamBID == teamID {
		return
	}

	if next.TeamAID == 0 {
		next.TeamAID = teamID
	} else if next.TeamBID == 0 {
		next.TeamBID = teamID
	} else {
		log.Printf("⚠️ Finals advance: target match %d full", matchID)
		return
	}

	// if we changed participants, clear completion (safe)
	next.WinnerID = nil
	next.LoserID = nil
	next.Status = "Scheduled"

	if err := DB.Save(&next).Error; err != nil {
		log.Printf("❌ Finals advance: save target match %d failed: %v", matchID, err)
	}
}

func advanceFinalsBracket(m Match) {
	if m.WinnerID == nil || *m.WinnerID == 0 {
		return
	}

	// compute loser safely if missing
	var loser uint
	if m.LoserID != nil && *m.LoserID != 0 {
		loser = *m.LoserID
	} else {
		if *m.WinnerID == m.TeamAID {
			loser = m.TeamBID
		} else {
			loser = m.TeamAID
		}
	}

	// winner route
	if m.WinnerToMatchID != nil && *m.WinnerToMatchID != 0 {
		insertIntoMatch(*m.WinnerToMatchID, *m.WinnerID)
	}

	// loser route
	if loser != 0 && m.LoserToMatchID != nil && *m.LoserToMatchID != 0 {
		insertIntoMatch(*m.LoserToMatchID, loser)
	}
}

// POST /api/mod/finals/clear-bracket-view
func HandleModFinalsClearBracketView(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	season := strings.TrimSpace(currentSeason)
	if season == "" {
		season = "0"
	}

	// Find ONLY active (non-archived) finals matches
	var finals []Match
	if err := DB.
		Where("season = ? AND is_finals = ? AND archived = false", season, true).
		Find(&finals).Error; err != nil {

		log.Printf("❌ FinalsClearView: failed to load finals: %v", err)
		http.Error(w, "Failed to load finals matches", http.StatusInternalServerError)
		return
	}

	if len(finals) == 0 {
		respondJSON(w, map[string]any{
			"success": true,
			"message": "No finals bracket to clear.",
		})
		return
	}

	// Only clear BRACKET metadata (do NOT unset is_finals)
	for _, m := range finals {
		m.Bracket = ""
		m.BracketRound = 0
		m.BracketSlot = 0

		if err := DB.Save(&m).Error; err != nil {
			log.Printf("❌ FinalsClearView: failed to update match %d: %v", m.ID, err)
		}
	}

	log.Printf("🧹 Cleared bracket assignments but kept finals matches in history (season %s)", season)

	respondJSON(w, map[string]any{
		"success": true,
		"cleared": len(finals),
		"message": "Finals bracket view cleared — matches remain intact.",
	})
}

func HandleModToggleFinalsVisible(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		Visible bool `json:"visible"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var s LeagueSettings
	DB.First(&s)

	s.FinalsVisible = req.Visible
	DB.Save(&s)

	respondJSON(w, map[string]any{
		"success": true,
		"visible": s.FinalsVisible,
	})
}

func HandleGetFinalsVisible(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var s LeagueSettings
	if err := DB.First(&s).Error; err != nil {
		respondJSON(w, map[string]any{"visible": false})
		return
	}

	respondJSON(w, map[string]any{"visible": s.FinalsVisible})
}

// POST /api/mod/finals/update-seeds
func HandleModFinalsSetSeeds(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		Seeds []struct {
			TeamID uint `json:"team_id"`
			Seed   int  `json:"seed"`
		} `json:"seeds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		modJSONErr(w, http.StatusBadRequest, "Invalid seed list")
		return
	}

	season := strings.TrimSpace(currentSeason)
	if season == "" {
		season = "0"
	}

	// Validate & apply
	for _, entry := range req.Seeds {
		DB.Model(&FinalsTeam{}).
			Where("season = ? AND team_id = ?", season, entry.TeamID).
			Update("seed", entry.Seed)
	}

	respondJSON(w, map[string]any{
		"success": true,
		"updated": len(req.Seeds),
	})
}

func GenerateDoubleElimBracket(finalsTeams []FinalsTeam, season string) ([]Match, error) {
	sort.Slice(finalsTeams, func(i, j int) bool { return finalsTeams[i].Seed < finalsTeams[j].Seed })

	teamCount := len(finalsTeams)
	if teamCount < 2 {
		return nil, fmt.Errorf("need at least 2 teams for finals")
	}

	// normalize to power of two up to 16
	bracketSize := 1
	for bracketSize < teamCount {
		bracketSize *= 2
	}
	if bracketSize > 16 {
		bracketSize = 16
	}

	// fill BYEs with TeamID=0
	for len(finalsTeams) < bracketSize {
		finalsTeams = append(finalsTeams, FinalsTeam{TeamID: 0, Seed: 999})
	}

	// how many WB rounds
	k := 0
	for (1 << k) < bracketSize {
		k++
	}

	now := time.Now()
	create := func(aID, bID uint, bracket string, round, slot int) Match {
		return Match{
			Season:       season,
			MatchCode:    fmt.Sprintf("%s-Finals-%s-R%dS%d", season, bracket, round, slot),
			TeamAID:      aID,
			TeamBID:      bID,
			IsFinals:     true,
			Bracket:      bracket,
			BracketRound: round,
			BracketSlot:  slot,
			Status:       "Scheduled",
			ProposedDate: &now,
		}
	}

	// --- seeding pairs: 1 vs N, 2 vs N-1, ...
	seedToTeam := make([]uint, bracketSize+1) // 1-indexed
	for i := 0; i < bracketSize; i++ {
		seedToTeam[i+1] = finalsTeams[i].TeamID
	}

	// create WB matches in memory
	// wb[round][slot] = index in matches slice
	wb := make([][]int, k+1) // 1..k
	for r := 1; r <= k; r++ {
		matchCount := bracketSize / (1 << r)
		wb[r] = make([]int, matchCount+1) // 1..matchCount
	}

	// create LB matches
	lbRounds := 2*k - 2
	lb := make([][]int, lbRounds+1) // 1..lbRounds
	lbMatchCount := func(round int) int {
		// exp = min(k, 2 + floor((round-1)/2))
		exp := 2 + (round-1)/2
		if exp > k {
			exp = k
		}
		cnt := bracketSize / (1 << exp)
		if cnt < 1 {
			cnt = 1
		}
		return cnt
	}
	for r := 1; r <= lbRounds; r++ {
		mc := lbMatchCount(r)
		lb[r] = make([]int, mc+1)
	}

	var matches []Match

	// ============== BUILD WB R1 with seed pairs
	// Pair slots: (1 vs N), (2 vs N-1), ...
	// match slot i uses seed i and seed (N-i+1)
	wbR1Count := bracketSize / 2
	for s := 1; s <= wbR1Count; s++ {
		aSeed := s
		bSeed := bracketSize - s + 1
		aID := seedToTeam[aSeed]
		bID := seedToTeam[bSeed]

		m := create(aID, bID, "winners", 1, s)
		matches = append(matches, m)
		wb[1][s] = len(matches) - 1
	}

	// ============== BUILD remaining WB rounds as placeholders
	for r := 2; r <= k; r++ {
		mc := bracketSize / (1 << r)
		for s := 1; s <= mc; s++ {
			matches = append(matches, create(0, 0, "winners", r, s))
			wb[r][s] = len(matches) - 1
		}
	}

	// ============== BUILD LB rounds as placeholders
	for r := 1; r <= lbRounds; r++ {
		mc := lbMatchCount(r)
		for s := 1; s <= mc; s++ {
			matches = append(matches, create(0, 0, "losers", r, s))
			lb[r][s] = len(matches) - 1
		}
	}

	// ============== BUILD GF1 + GF2 (reset)
	matches = append(matches, create(0, 0, "grand_final", 1, 1))
	gf1Idx := len(matches) - 1

	matches = append(matches, create(0, 0, "grand_final", 2, 1))
	gf2Idx := len(matches) - 1

	// ----------------------------------------------------
	// WIRING (graph)
	// ----------------------------------------------------

	// WB winner -> next WB
	for r := 1; r < k; r++ {
		mc := bracketSize / (1 << r)
		for s := 1; s <= mc; s++ {
			nextSlot := (s + 1) / 2
			nextIdx := wb[r+1][nextSlot]
			targetID := &matches[nextIdx].ID // not set yet, we will patch after DB insert
			_ = targetID
		}
	}

	// LB winner -> next LB (pattern)
	// odd rounds -> next round same slot
	// even rounds -> next round slot=(s+1)/2
	for r := 1; r < lbRounds; r++ {
		mc := lbMatchCount(r)
		for s := 1; s <= mc; s++ {
			var nextSlot int
			if r%2 == 1 {
				nextSlot = s
			} else {
				nextSlot = (s + 1) / 2
			}
			nextIdx := lb[r+1][nextSlot]
			_ = nextIdx
		}
	}

	// We'll patch IDs AFTER DB insert, so store routes by indices now.
	type route struct {
		fromIdx int
		winTo   *int
		loseTo  *int
	}
	var routes []route

	// --- WB routes (winner to WB, loser to LB)
	// Winner: WB r,s -> WB r+1,(s+1)/2 (except WB final -> GF1)
	// Loser:
	//   WB R1 losers -> LB R1 slot=(s+1)/2
	//   WB Rr (r>=2) losers -> LB R(2*(r-1)) slot=s
	for r := 1; r <= k; r++ {
		mc := bracketSize / (1 << r)
		for s := 1; s <= mc; s++ {
			from := wb[r][s]

			// winner destination
			var winTo *int
			if r < k {
				ns := (s + 1) / 2
				nidx := wb[r+1][ns]
				winTo = &nidx
			} else {
				winTo = &gf1Idx
			}

			// loser destination
			var loseTo *int
			if r == 1 {
				ls := (s + 1) / 2
				lidx := lb[1][ls]
				loseTo = &lidx
			} else {
				lr := 2 * (r - 1)
				// clamp in case (should exist)
				if lr < 1 {
					lr = 1
				}
				if lr > lbRounds {
					lr = lbRounds
				}
				// slot aligns with WB slot in that round
				lidx := lb[lr][s]
				loseTo = &lidx
			}

			routes = append(routes, route{fromIdx: from, winTo: winTo, loseTo: loseTo})
		}
	}

	// --- LB routes (winner to next LB; LB final winner -> GF1)
	for r := 1; r <= lbRounds; r++ {
		mc := lbMatchCount(r)
		for s := 1; s <= mc; s++ {
			from := lb[r][s]

			var winTo *int
			if r < lbRounds {
				var ns int
				if r%2 == 1 {
					ns = s
				} else {
					ns = (s + 1) / 2
				}
				nidx := lb[r+1][ns]
				winTo = &nidx
			} else {
				// LB champion -> GF1
				winTo = &gf1Idx
			}

			// loser in LB is eliminated (no route)
			routes = append(routes, route{fromIdx: from, winTo: winTo, loseTo: nil})
		}
	}

	// GF1 winner -> champion (no route)
	// GF1 loser -> GF2 (optional reset; you can enforce later)
	// We'll set winner_to nil, loser_to -> gf2
	{
		winTo := (*int)(nil)
		loseTo := &gf2Idx
		routes = append(routes, route{fromIdx: gf1Idx, winTo: winTo, loseTo: loseTo})
	}
	// GF2 no routes
	routes = append(routes, route{fromIdx: gf2Idx, winTo: nil, loseTo: nil})

	// We return matches with routes stored by indices; IDs get assigned after DB insert.
	// We'll temporarily store the route indices inside unused fields? No.
	// Instead, we return matches + a separate patcher is hard.
	//
	// ✅ EASIEST: patch using MatchCode after insert.
	// So ensure match codes are stable and unique (they are), then patch by MatchCode lookup in generate handler.

	return matches, nil
}

func HandleGenerateFinals(w http.ResponseWriter, r *http.Request) {
	season := strings.TrimSpace(currentSeason)
	if season == "" {
		season = "0"
	}

	// Load finals teams
	var teams []FinalsTeam
	if err := DB.Where("season = ?", season).Order("seed ASC").Find(&teams).Error; err != nil {
		http.Error(w, "Failed to load finals teams", http.StatusInternalServerError)
		return
	}

	if len(teams) < 2 {
		http.Error(w, "Not enough finals teams", http.StatusBadRequest)
		return
	}

	matches, err := GenerateDoubleElimBracket(teams, season)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Clear previous finals
	DB.Where("season = ? AND is_finals = true AND archived = false", season).
		Delete(&Match{})

	// Save new matches
	for _, m := range matches {
		DB.Create(&m)
	}

	respondJSON(w, map[string]string{
		"status":  "ok",
		"message": "Finals bracket generated",
	})
}

// POST /api/match/ping-sub
func HandleMatchPingSub(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID uint `json:"match_id"`
		TeamID  uint `json:"team_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 || req.TeamID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Load match
	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// Ensure team belongs to match
	if req.TeamID != match.TeamAID && req.TeamID != match.TeamBID {
		http.Error(w, "Team not part of this match", http.StatusForbidden)
		return
	}

	// Load team
	var team Team
	DB.First(&team, req.TeamID)

	// Load approved league subs for that team
	var subs []Player
	DB.Raw(`
		SELECT id, display_name, username
		FROM players
		WHERE LOWER(role) = 'league sub'
	`).Scan(&subs)

	// Build ping list
	pings := ""
	for _, s := range subs {
		pings += fmt.Sprintf("<@%d> ", s.ID)
	}
	if pings == "" {
		pings = "_No registered league subs for this team._"
	}

	// Match timestamp
	ts := "Unknown time"
	if match.ProposedDate != nil {
		ts = fmt.Sprintf("<t:%d:F>", match.ProposedDate.Unix())
	}

	// Build message
	msg := fmt.Sprintf(
		"📣 **Sub Request Needed!**\n\n"+
			"**Match:** %s\n"+
			"**Teams:** %s vs %s\n"+
			"**Team Requesting Sub:** %s\n"+
			"**Match Time:** %s\n\n"+
			"🔔 **Pinging League Subs:**\n%s",
		match.MatchCode,
		getTeamNameSafe(match.TeamAID),
		getTeamNameSafe(match.TeamBID),
		team.Name,
		ts,
		pings,
	)

	// Load channel ID from ENV
	subChannel := getEnv("SUB_PING_CHANNEL_ID", "")
	if subChannel == "" {
		log.Println("⚠️ Missing SUB_PING_CHANNEL_ID in .env")
	} else {
		SendDiscordToChannel(subChannel, msg)
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Sub ping sent",
	})
}

func getTeamNameSafe(teamID uint) string {
	var t Team
	if err := DB.First(&t, teamID).Error; err != nil {
		return fmt.Sprintf("Team #%d", teamID)
	}
	return t.Name
}

func SendDiscordToChannel(channelID string, msg string) {
	if channelID == "" {
		log.Println("⚠️ SUB_PING_CHANNEL_ID missing")
		return
	}

	botToken := getEnv("DISCORD_BOT_TOKEN", "")
	if botToken == "" {
		log.Println("❌ Missing DISCORD_BOT_TOKEN")
		return
	}

	payload := map[string]string{
		"content": msg,
	}

	jsonBody, _ := json.Marshal(payload)

	req, _ := http.NewRequest(
		"POST",
		fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID),
		bytes.NewBuffer(jsonBody),
	)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("❌ Failed to send Discord message: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Discord API error (%d): %s", resp.StatusCode, string(body))
	}
}

func HandleGetSeasonCalendar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	out := map[string]any{
		"season_start": getEnv("SEASON_START", ""),
		"season_end":   getEnv("SEASON_END", ""),
		"break_start":  getEnv("BREAK_START", ""),
		"break_end":    getEnv("BREAK_END", ""),
		"finals_start": getEnv("FINALS_START", ""),
		"finals_end":   getEnv("FINALS_END", ""),
	}

	respondJSON(w, out)
}

func ModRemoveCooldown(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		PlayerID string `json:"player_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(strings.TrimSpace(req.PlayerID), 10, 64)
	if err != nil || id == 0 {
		http.Error(w, "Invalid player ID", http.StatusBadRequest)
		return
	}

	// Remove cooldown
	if err := DB.Model(&Player{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_left_team_at": nil,
		}).Error; err != nil {

		http.Error(w, "Failed to remove cooldown", http.StatusInternalServerError)
		return
	}

	LogGeneral(fmt.Sprintf("🧹 Cooldown cleared for <@%d>", id))

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Cooldown cleared",
	})
}

func HandleArchiveAllPlayers(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		Season string `json:"season"`
	}

	_ = json.NewDecoder(r.Body).Decode(&req)

	// Normalize Season
	season := strings.TrimSpace(req.Season)
	if season == "" {
		season = strings.TrimSpace(currentSeason)
	}

	season = normalizeSeason(season)

	// 🔥 Load players minimal numeric fields only
	type Row struct {
		ID      int64
		Rating  int64
		Wins    int64
		Losses  int64
		Matches int64
	}

	var rows []Row
	if err := DB.Raw(`
        SELECT id, rating, wins, losses, matches
        FROM players
        ORDER BY id
    `).Scan(&rows).Error; err != nil {
		log.Println("❌ Failed raw load:", err)
		http.Error(w, "Failed to load players", 500)
		return
	}

	log.Printf("🔥 Loaded %d players by RAW SQL\n", len(rows))

	// --- Archive each one ---
	for _, rrow := range rows {
		archivePlayerStats(rrow.ID, season)
	}

	LogGeneral(fmt.Sprintf("📦 Archived %d players (Season %s)", len(rows), season))

	respondJSON(w, map[string]any{
		"success":          true,
		"archived_players": len(rows),
		"season":           season,
	})
}

func normalizeSeason(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))

	if s == "" {
		return "Preseason"
	}

	if s == "pre" || s == "preseason" {
		return "Preseason"
	}

	s = strings.TrimPrefix(s, "season ")

	// If still not numeric, return as formatted text
	if _, err := strconv.Atoi(s); err != nil {
		return strings.Title(s)
	}

	return s
}

func archivePlayerStats(playerID int64, season string) {
	// Load minimal player stats
	type P struct {
		Rating  int64
		Wins    int64
		Losses  int64
		Matches int64
		Role    string
	}

	var p P
	DB.Raw(`
        SELECT rating, wins, losses, matches, role
        FROM players
        WHERE id = ?
    `, playerID).Scan(&p)

	// Normalize Player Role
	role := strings.TrimSpace(p.Role)
	role = strings.Title(strings.ToLower(role)) // e.g. "player", "league sub"

	if role != "Player" && role != "League Sub" {
		role = "Player"
	}

	// Load team info
	type TM struct {
		TeamID   uint
		TeamName string
	}
	var tm TM

	DB.Raw(`
		SELECT t.id AS team_id, t.name AS team_name
		FROM team_members tm
		LEFT JOIN teams t ON t.id = tm.team_id
		WHERE tm.player_id = ?
		LIMIT 1
	`, playerID).Scan(&tm)

	var archiveTeamID uint = 0

	if tm.TeamID != 0 {
		archiveTeamID = tm.TeamID
	}

	snapshot := PlayerHistory{
		PlayerID:       playerID,
		TeamID:         archiveTeamID,
		TeamName:       tm.TeamName,
		Role:           role,
		Season:         season,
		ArchiveRating:  int(p.Rating),
		ArchiveWins:    int(p.Wins),
		ArchiveLosses:  int(p.Losses),
		ArchiveMatches: int(p.Matches),
		ArchiveTeam:    tm.TeamName,
		IsTeamJoin:     false,
	}

	DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "player_id"}, {Name: "season"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"team_id",
			"team_name",
			"role",
			"archive_rating",
			"archive_wins",
			"archive_losses",
			"archive_matches",
			"archive_team",
		}),
	}).Create(&snapshot)
}

func GetPlayerComputedRating(playerID int64) int {
	type WL struct {
		Wins   int
		Losses int
	}
	var wl WL

	DB.Raw(`
        SELECT 
            (SELECT COUNT(*) FROM matches WHERE winner_id = team_id AND 
                (team_a_id = ? OR team_b_id = ?)) AS wins,
            (SELECT COUNT(*) FROM matches WHERE loser_id = team_id AND 
                (team_a_id = ? OR team_b_id = ?)) AS losses
    `, playerID, playerID, playerID, playerID).Scan(&wl)

	return 800 + (wl.Wins * 25) + (wl.Losses * -25)
}

func HandleResetTeamChallenges(w http.ResponseWriter, r *http.Request) {
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

	// Ensure team exists
	var t Team
	if err := DB.First(&t, req.TeamID).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// Reset challenges
	if err := DB.Model(&t).Update("weekly_challenges_used", 0).Error; err != nil {
		log.Printf("⚠️ Failed to reset challenges for team %d: %v", req.TeamID, err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	LogGeneral(fmt.Sprintf("🔄 Reset weekly challenges for team %s (%d)", t.Name, req.TeamID))

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Team challenge matches reset successfully",
	})
}

func HandleArchiveTeamStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	// LOAD CURRENT SEASON FROM .env
	season := os.Getenv("CURRENT_SEASON")
	season = strings.TrimSpace(season)

	if season == "" {
		season = "0"
	}

	var teams []Team
	if err := DB.Find(&teams).Error; err != nil {
		modJSONErr(w, 500, "failed to load teams")
		return
	}

	count := 0
	for _, t := range teams {
		if t.Status == "Disbanded" {
			continue
		}

		archive := TeamArchive{
			TeamID:  t.ID,
			Name:    t.Name,
			Season:  season,
			Rating:  t.Rating,
			Wins:    t.Wins,
			Losses:  t.Losses,
			Matches: t.Matches,
		}

		if err := DB.Create(&archive).Error; err != nil {
			log.Printf("⚠️ Failed archiving team %d: %v", t.ID, err)
			continue
		}

		count++
	}

	LogGeneral(fmt.Sprintf("📦 Archived stats for %d teams (Season %s)", count, season))

	respondJSON(w, map[string]any{
		"success":  true,
		"archived": count,
		"season":   season,
	})
}

func HandleGetTeamArchive(w http.ResponseWriter, r *http.Request) {
	teamIDStr := r.URL.Query().Get("id")
	teamID, err := strconv.Atoi(teamIDStr)
	if err != nil || teamID <= 0 {
		http.Error(w, "Invalid team ID", http.StatusBadRequest)
		return
	}

	var rows []TeamArchive
	if err := DB.Where("team_id = ?", teamID).Order("created_at DESC").Find(&rows).Error; err != nil {
		http.Error(w, "Failed to load team archive", http.StatusInternalServerError)
		return
	}

	respondJSON(w, rows)
}

func GetDivisionTier(rating int) (string, string) {
	divisions := []struct {
		Name  string
		Tiers []int
	}{
		{"Grandmaster", []int{2200, 2100, 2000, 1900}},
		{"Diamond", []int{1800, 1700, 1600, 1500}},
		{"Platinum", []int{1400, 1300, 1200, 1100}},
		{"Gold", []int{1000, 950, 900, 850}},
		{"Silver", []int{800, 750, 700, 650}},
		{"Bronze", []int{600, 550, 500, 0}},
	}

	for _, div := range divisions {
		for idx, min := range div.Tiers {
			if rating >= min {
				// tiers I–IV (1–4)
				tier := []string{"IV", "III", "II", "I"}[idx]
				return div.Name, tier
			}
		}
	}

	return "Unranked", ""
}

func HandleCheckDiscordMembership(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	discordIDStr, ok := session.Values["discord_id"].(string)

	if !ok || discordIDStr == "" {
		respondJSON(w, map[string]any{"in_guild": false})
		return
	}

	guildID := getEnv("DISCORD_GUILD_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")

	if guildID == "" || botToken == "" {
		respondJSON(w, map[string]any{"in_guild": false})
		return
	}

	url := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, discordIDStr)

	req2, _ := http.NewRequest("GET", url, nil)
	req2.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		respondJSON(w, map[string]any{"in_guild": false})
		return
	}

	respondJSON(w, map[string]any{
		"in_guild": resp.StatusCode == 200,
	})
}

func HandleGetDiscordServerInfo(w http.ResponseWriter, r *http.Request) {
	guildID := getEnv("DISCORD_GUILD_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")
	invite := getEnv("DISCORD_INVITE_URL", "")

	if guildID == "" || botToken == "" {
		http.Error(w, "Discord settings missing", http.StatusInternalServerError)
		return
	}

	// Fetch guild info
	url := fmt.Sprintf("https://discord.com/api/v10/guilds/%s?with_counts=true", guildID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "Discord API error", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Println("Discord error:", string(body))
		http.Error(w, "Discord API returned error", http.StatusInternalServerError)
		return
	}

	var guild struct {
		Name   string `json:"name"`
		Icon   string `json:"icon"`
		Banner string `json:"banner"`

		ApproximateMemberCount   int `json:"approximate_member_count"`
		ApproximatePresenceCount int `json:"approximate_presence_count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&guild); err != nil {
		http.Error(w, "Failed to decode guild data", http.StatusInternalServerError)
		return
	}

	// Build URLs
	iconURL := ""
	bannerURL := ""

	if guild.Icon != "" {
		iconURL = fmt.Sprintf("https://cdn.discordapp.com/icons/%s/%s.png?size=256", guildID, guild.Icon)
	}
	if guild.Banner != "" {
		bannerURL = fmt.Sprintf("https://cdn.discordapp.com/banners/%s/%s.png?size=512", guildID, guild.Banner)
	}

	// Final JSON for frontend
	respondJSON(w, map[string]any{
		"name":    guild.Name,
		"icon":    iconURL,
		"banner":  bannerURL,
		"invite":  invite,
		"members": guild.ApproximateMemberCount,
		"online":  guild.ApproximatePresenceCount,
	})
}

func BuildFinalsSnapshot(season string) (map[string]any, error) {
	var matches []Match

	if err := DB.
		Where("season = ? AND is_finals = true", season).
		Order("bracket, bracket_round, bracket_slot").
		Find(&matches).Error; err != nil {
		return nil, err
	}

	// Defensive: empty finals guard
	if len(matches) == 0 {
		return nil, errors.New("no finals matches found")
	}

	return map[string]any{
		"season":      season,
		"archived_at": time.Now(),
		"matches":     matches,
	}, nil
}

// POST /api/mod/finals/archive
func HandleArchiveFinals(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	season := normalizeSeason(currentSeason)

	tx := DB.Begin()
	if tx.Error != nil {
		http.Error(w, "failed to start transaction", 500)
		return
	}

	// Prevent double-archive
	var count int64
	tx.Model(&FinalsArchive{}).
		Where("season = ?", season).
		Count(&count)

	if count > 0 {
		tx.Rollback()
		http.Error(w, "finals already archived for this season", http.StatusConflict)
		return
	}

	// Build snapshot
	snapshot, err := BuildFinalsSnapshot(season)
	if err != nil {
		tx.Rollback()
		http.Error(w, err.Error(), 400)
		return
	}

	data, _ := json.Marshal(snapshot)

	if err := tx.Create(&FinalsArchive{
		Season:   season,
		Snapshot: datatypes.JSON(data),
	}).Error; err != nil {
		tx.Rollback()
		http.Error(w, "failed to archive finals", 500)
		return
	}

	// 🔒 Mark finals matches archived
	if err := tx.Model(&Match{}).
		Where("season = ? AND is_finals = true", season).
		Update("archived", true).Error; err != nil {
		tx.Rollback()
		http.Error(w, "failed to mark finals archived", 500)
		return
	}

	// 🧹 THIS IS THE KEY STEP
	if err := ClearLiveFinalsState(tx, season); err != nil {
		tx.Rollback()
		http.Error(w, "failed to clear live finals state", 500)
		return
	}

	if err := tx.Commit().Error; err != nil {
		http.Error(w, "commit failed", 500)
		return
	}

	LogGeneral(fmt.Sprintf("🏁 Finals archived & cleared for %s", season))

	respondJSON(w, map[string]any{
		"success": true,
		"season":  season,
	})
}

// GET /api/finals/archive?season=2
func HandleGetFinalsArchive(w http.ResponseWriter, r *http.Request) {
	season := strings.TrimSpace(r.URL.Query().Get("season"))
	if season == "" {
		http.Error(w, "missing season", http.StatusBadRequest)
		return
	}

	var archive FinalsArchive
	if err := DB.Where("season = ?", season).First(&archive).Error; err != nil {
		http.Error(w, "archive not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(archive.Snapshot)
}

func ClearLiveFinalsState(tx *gorm.DB, season string) error {
	// Remove finals teams (clears finals tab team list)
	if err := tx.Where("season = ?", season).
		Delete(&FinalsTeam{}).Error; err != nil {
		return err
	}

	// Clear bracket layout ONLY (keep matches)
	if err := tx.Model(&Match{}).
		Where("season = ? AND is_finals = true", season).
		Updates(map[string]any{
			"bracket":       "",
			"bracket_round": 0,
			"bracket_slot":  0,
		}).Error; err != nil {
		return err
	}

	return nil
}

func buildDesiredDiscordRoles(
	p Player,
	teamMember *TeamMember,
) []string {

	roles := []string{}

	// League sub (only if applicable)
	if p.Role == "sub" {
		roles = append(roles, os.Getenv("DISCORD_LEAGUE_SUB_ROLE_ID"))
	}

	// Team-based roles
	if teamMember != nil {
		switch strings.ToLower(teamMember.Role) {
		case "captain":
			roles = append(roles, os.Getenv("DISCORD_CAPTAIN_ROLE_ID"))
		case "co-captain":
			roles = append(roles, os.Getenv("DISCORD_CO_CAPTAIN_ROLE_ID"))
		}
	}

	return roles
}

func syncDiscordRolesForPlayer(playerID int64) {
	guildID := os.Getenv("DISCORD_GUILD_ID")
	botToken := os.Getenv("DISCORD_BOT_TOKEN")

	if guildID == "" || botToken == "" {
		return
	}

	var p Player
	if err := DB.First(&p, playerID).Error; err != nil {
		return
	}

	var tm TeamMember
	var tmPtr *TeamMember = nil

	err := DB.
		Where("player_id = ?", playerID).
		Order("team_id DESC").
		Limit(1).
		Find(&tm).Error

	if err == nil && tm.TeamID != 0 {
		tmPtr = &tm
	}

	desired := buildDesiredDiscordRoles(p, tmPtr)

	syncMemberRoles(
		guildID,
		playerID,
		desired,
		botToken,
	)
}

func isManagedRole(roleID string) bool {
	switch roleID {
	case os.Getenv("DISCORD_LEAGUE_SUB_ROLE_ID"),
		os.Getenv("DISCORD_CAPTAIN_ROLE_ID"),
		os.Getenv("DISCORD_CO_CAPTAIN_ROLE_ID"):
		return true
	default:
		return false
	}
}

func SyncAllDiscordRoles() {
	var players []Player
	DB.Find(&players)

	for _, p := range players {
		syncDiscordRolesForPlayer(p.ID)
	}
}

func fetchMemberRoles(
	guildID string,
	userID int64,
	botToken string,
) ([]string, error) {

	url := fmt.Sprintf(
		"https://discord.com/api/v10/guilds/%s/members/%d",
		guildID,
		userID,
	)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch member failed %s %s", resp.Status, body)
	}

	var data struct {
		Roles []string `json:"roles"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return data.Roles, nil
}

func addGuildRoleHTTP(guildID string, userID int64, roleID, botToken string) error {
	url := fmt.Sprintf(
		"https://discord.com/api/v10/guilds/%s/members/%d/roles/%s",
		guildID,
		userID,
		roleID,
	)

	req, _ := http.NewRequest("PUT", url, nil)
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("add role failed %s %s", resp.Status, body)
	}

	return nil
}

func removeGuildRoleHTTP(guildID string, userID int64, roleID, botToken string) error {
	url := fmt.Sprintf(
		"https://discord.com/api/v10/guilds/%s/members/%d/roles/%s",
		guildID,
		userID,
		roleID,
	)

	req, _ := http.NewRequest("DELETE", url, nil)
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remove role failed %s %s", resp.Status, body)
	}

	return nil
}

func syncMemberRoles(
	guildID string,
	playerID int64,
	desiredRoles []string,
	botToken string,
) {
	currentRoles, err := fetchMemberRoles(guildID, playerID, botToken)
	if err != nil {
		log.Printf("❌ role fetch failed for %d: %v", playerID, err)
		return
	}

	current := map[string]bool{}
	for _, r := range currentRoles {
		current[r] = true
	}

	desired := map[string]bool{}
	for _, r := range desiredRoles {
		if r != "" {
			desired[r] = true
		}
	}

	// ➕ ADD missing roles
	for roleID := range desired {
		if !current[roleID] {
			if err := addGuildRoleHTTP(guildID, playerID, roleID, botToken); err != nil {
				log.Printf("❌ add role %s to %d failed: %v", roleID, playerID, err)
			} else {
				log.Printf("✅ added role %s → %d", roleID, playerID)
			}
		}
	}

	// ➖ REMOVE managed roles that should not exist
	for roleID := range current {
		if isManagedRole(roleID) && !desired[roleID] {
			if err := removeGuildRoleHTTP(guildID, playerID, roleID, botToken); err != nil {
				log.Printf("❌ remove role %s from %d failed: %v", roleID, playerID, err)
			} else {
				log.Printf("🧹 removed role %s ← %d", roleID, playerID)
			}
		}
	}
}
