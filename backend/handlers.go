package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
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

func isRosterLocked() bool {
	var dbLocked bool
	if err := DB.Raw("SELECT roster_locked FROM settings WHERE id = 1").Scan(&dbLocked).Error; err == nil && dbLocked {
		return true
	}
	rl := os.Getenv("ROSTER_LOCK")
	if rl != "" {
		if t, err := time.Parse("2006-01-02", rl); err == nil {
			nowLocal := time.Now().Local()
			lockTime := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
			if nowLocal.After(lockTime) || nowLocal.Format("2006-01-02") == rl {
				return true
			}
		}
	}
	return false
}

// clampRating enforces floor (0) and ceiling (MAX_RATING env, default 1300) on a rating value.
func clampRating(rating int) int {
	min := getEnvInt("MIN_RATING", 0)
	max := getEnvInt("MAX_RATING", 1300)
	if rating < min {
		return min
	}
	if rating > max {
		return max
	}
	return rating
}

// --- Require Login ---
func requireLogin(w http.ResponseWriter, r *http.Request) (*sessions.Session, bool) {
	session, _ := store.Get(r, "session")

	// Validate that user info exists
	if _, ok := session.Values["user"].(string); !ok {
		if _, ok2 := session.Values["discord_id"].(string); !ok2 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return nil, false
		}
	}

	// DEV MODE impersonation override — inject into session values for downstream use
	if os.Getenv("DEV_MODE") == "true" {
		if overrideID := r.URL.Query().Get("as"); overrideID != "" {
			session.Values["discord_id"] = overrideID
		}
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
	// Avoid caching API responses; stale JSON can cause UI to show old logo URLs after refresh.
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
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
	arenaModeEnabled := os.Getenv("ARENA_MODE_ENABLED") == "true"

	respondJSON(w, map[string]any{
		"roster_locked":          isRosterLocked(),
		"min_team_players":       minPlayers,
		"max_team_players":       maxPlayers,
		"current_week":           s.CurrentWeek,
		"weekly_challenge_limit": s.WeeklyChallengeLimit,
		"challenges_enabled":     s.ChallengesEnabled,
		"arena_mode_enabled":     arenaModeEnabled,
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
		Avatar      string
		Role        string
		Device      string
		Timezone    string
	}

	roleFilter := strings.TrimSpace(r.URL.Query().Get("role"))

	query := DB.Table("players").
		Select("id, username, display_name, avatar, role, device, timezone").
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
			"avatar":       r.Avatar,
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
		Avatar      string `json:"avatar"`
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
			players.avatar,
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
				p.avatar,
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
		"logo_url":               team.LogoURL,
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
	placementMatches := getEnvInt("PLACEMENT_MATCHES", 3)

	type TeamRow struct {
		ID          uint   `json:"id"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		Rating      int    `json:"rating"`
		Wins        int    `json:"wins"`
		Losses      int    `json:"losses"`
		Matches     int    `json:"matches"`
		Division    string `json:"division"`
		Tier        string `json:"tier"`
		InPlacement bool   `json:"in_placement"`
	}

	var rows []TeamRow

	// Show all teams including disbanded
	if err := DB.Table("teams").
		Select("id, name, status, rating, wins, losses, matches").
		Order("rating DESC").
		Order("wins DESC").
		Order("losses ASC").
		Find(&rows).Error; err != nil {

		http.Error(w, "failed to load team leaderboard", http.StatusInternalServerError)
		return
	}

	// Add division + tier + placement status
	for i := range rows {
		if rows[i].Matches < placementMatches {
			rows[i].InPlacement = true
			rows[i].Division = "Placement"
			rows[i].Tier = ""
		} else {
			div, tier := GetDivisionTier(rows[i].Rating)
			rows[i].Division = div
			rows[i].Tier = tier
		}
	}

	respondJSON(w, rows)
}

// --- Get Available Seasons ---
func GetLeaderboardSeasons(w http.ResponseWriter, r *http.Request) {
	currentSeason := os.Getenv("CURRENT_SEASON")
	if currentSeason == "" {
		currentSeason = "1"
	}

	// Get distinct seasons from team_archives
	var teamSeasons []string
	DB.Table("team_archives").Distinct().Pluck("season", &teamSeasons)

	// Also get seasons from player_stats_archive
	var playerSeasons []string
	DB.Table("player_stats_archive").Distinct().Pluck("season", &playerSeasons)

	// Combine with current season and deduplicate
	seasonMap := make(map[string]bool)
	seasonMap[currentSeason] = true
	for _, s := range teamSeasons {
		seasonMap[s] = true
	}
	for _, s := range playerSeasons {
		seasonMap[s] = true
	}

	// Convert to slice and sort descending (newest first)
	seasons := make([]string, 0, len(seasonMap))
	for s := range seasonMap {
		seasons = append(seasons, s)
	}
	// Sort in reverse order (higher numbers first)
	for i := 0; i < len(seasons)-1; i++ {
		for j := i + 1; j < len(seasons); j++ {
			if seasons[j] > seasons[i] {
				seasons[i], seasons[j] = seasons[j], seasons[i]
			}
		}
	}

	respondJSON(w, map[string]any{
		"current_season": currentSeason,
		"seasons":        seasons,
	})
}

// --- Historical Team Leaderboard by Season ---
func GetTeamLeaderboardBySeason(w http.ResponseWriter, r *http.Request) {
	season := r.URL.Query().Get("season")
	currentSeason := os.Getenv("CURRENT_SEASON")
	if currentSeason == "" {
		currentSeason = "1"
	}

	// If requesting current season or no season specified, return live data
	if season == "" || season == currentSeason {
		GetTeamLeaderboard(w, r)
		return
	}

	// Get archived data for the requested season
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

	var archives []TeamArchive
	if err := DB.Where("season = ?", season).
		Order("rating DESC").
		Order("wins DESC").
		Order("losses ASC").
		Find(&archives).Error; err != nil {
		http.Error(w, "failed to load archived team leaderboard", http.StatusInternalServerError)
		return
	}

	rows := make([]TeamRow, 0, len(archives))
	for _, a := range archives {
		// Get current team status
		var team Team
		DB.First(&team, "id = ?", a.TeamID)

		div, tier := GetDivisionTier(a.Rating)
		rows = append(rows, TeamRow{
			ID:       a.TeamID,
			Name:     a.Name,
			Status:   team.Status,
			Rating:   a.Rating,
			Wins:     a.Wins,
			Losses:   a.Losses,
			Matches:  a.Matches,
			Division: div,
			Tier:     tier,
		})
	}

	respondJSON(w, rows)
}

// --- Historical Player Leaderboard by Season ---
func GetPlayerLeaderboardBySeason(w http.ResponseWriter, r *http.Request) {
	season := r.URL.Query().Get("season")
	currentSeason := os.Getenv("CURRENT_SEASON")
	if currentSeason == "" {
		currentSeason = "1"
	}

	// If requesting current season or no season specified, return live data
	if season == "" || season == currentSeason {
		GetPlayerLeaderboard(w, r)
		return
	}

	// Get archived player data from player_stats_archive table
	type PlayerRow struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Rating      int    `json:"rating"`
		Wins        int    `json:"wins"`
		Losses      int    `json:"losses"`
		Matches     int    `json:"matches"`
		Division    string `json:"division"`
		Tier        string `json:"tier"`
	}

	// Query archived player stats joined with player info
	type ArchivedPlayer struct {
		PlayerID       int64
		ArchiveRating  int
		ArchiveWins    int
		ArchiveLosses  int
		ArchiveMatches int
	}

	var archives []ArchivedPlayer
	if err := DB.Table("player_stats_archive").
		Select("player_id, archive_rating, archive_wins, archive_losses, archive_matches").
		Where("season = ?", season).
		Order("archive_rating DESC").
		Order("archive_wins DESC").
		Order("archive_losses ASC").
		Find(&archives).Error; err != nil {
		http.Error(w, "failed to load archived player leaderboard", http.StatusInternalServerError)
		return
	}

	// Look up player usernames
	rows := make([]PlayerRow, 0, len(archives))
	for _, a := range archives {
		var player Player
		DB.First(&player, "id = ?", a.PlayerID)

		div, tier := GetDivisionTier(a.ArchiveRating)
		rows = append(rows, PlayerRow{
			ID:          strconv.FormatInt(a.PlayerID, 10),
			Username:    player.Username,
			DisplayName: player.DisplayName,
			Rating:      a.ArchiveRating,
			Wins:        a.ArchiveWins,
			Losses:      a.ArchiveLosses,
			Matches:     a.ArchiveMatches,
			Division:    div,
			Tier:        tier,
		})
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
		ID                     uint         `json:"id"`
		MatchCode              string       `json:"match_code"`
		Opponent               string       `json:"opponent"`
		Date                   *time.Time   `json:"date"`
		Result                 string       `json:"result"`
		Status                 string       `json:"status"`
		Season                 string       `json:"season"`
		TeamAID                uint         `json:"team_a_id"`
		TeamBID                uint         `json:"team_b_id"`
		TeamAScheduleConfirmed bool         `json:"team_a_schedule_confirmed"`
		TeamBScheduleConfirmed bool         `json:"team_b_schedule_confirmed"`
		LeagueSubA             *string      `json:"league_sub_a"`
		LeagueSubB             *string      `json:"league_sub_b"`
		Maps                   []MatchScore `json:"maps"`
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
            m.team_a_id, m.team_b_id,
            m.team_a_schedule_confirmed,
            m.team_b_schedule_confirmed
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
				&m.TeamAScheduleConfirmed, &m.TeamBScheduleConfirmed,
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
			m.Status == "Forfeit" ||
			m.Status == "Forfeited"

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
		Avatar      string `json:"avatar"`
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
                players.avatar,
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
                    p.avatar,
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
               COALESCE(p.avatar, '') AS avatar,
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
			"logo_url":               team.LogoURL,
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
	if isRosterLocked() {
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

	// 🔔 Notify team captains about the join request
	notifyTeamCaptains(req.TeamID, "join_request",
		"New Join Request",
		fmt.Sprintf("%s wants to join your team", player.DisplayName),
		"/myteam",
	)

	// 📣 Discord log to general channel with captain mentions
	go func() {
		var captains []TeamMember
		DB.Where("team_id = ? AND (role = 'Captain' OR role = 'Co-Captain')", req.TeamID).Find(&captains)
		captainMentions := ""
		for _, c := range captains {
			captainMentions += fmt.Sprintf(" <@%d>", c.PlayerID)
		}
		SendDiscordLog(fmt.Sprintf("<@%d> has requested to join **%s**%s", playerID, team.Name, captainMentions))
	}()

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
	if isRosterLocked() {
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

// --- Captain/Co-Captain: Upload or replace a team logo ---
// Expects multipart/form-data with fields:
// - team_id: uint
// - logo: file
// Responds: { success: true, logo_url: "/api/team/logo/<teamID>/<version>", logo_version: "<version>" }
func HandleUploadTeamLogo(w http.ResponseWriter, r *http.Request) {
	// --- Validate session ---
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	// Limit size (8MB)
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "Invalid multipart form", http.StatusBadRequest)
		return
	}

	teamIDStr := strings.TrimSpace(r.FormValue("team_id"))
	if teamIDStr == "" {
		http.Error(w, "Missing team_id", http.StatusBadRequest)
		return
	}
	teamID64, err := strconv.ParseUint(teamIDStr, 10, 64)
	if err != nil || teamID64 == 0 {
		http.Error(w, "Invalid team_id", http.StatusBadRequest)
		return
	}
	teamID := uint(teamID64)

	role, err := getMemberRole(teamID, playerID)
	if err != nil || (role != "Captain" && role != "Co-Captain") {
		http.Error(w, "Only captains can upload a team logo", http.StatusForbidden)
		return
	}

	file, header, err := r.FormFile("logo")
	if err != nil {
		http.Error(w, "Missing logo file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Basic extension allowlist
	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		// ok
	default:
		http.Error(w, "Unsupported file type (use png/jpg/jpeg/webp/gif)", http.StatusBadRequest)
		return
	}
	if ext == ".jpeg" {
		ext = ".jpg"
	}

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		// fallback based on extension
		switch ext {
		case ".png":
			contentType = "image/png"
		case ".gif":
			contentType = "image/gif"
		case ".webp":
			contentType = "image/webp"
		default:
			contentType = "image/jpeg"
		}
	}

	// Ensure team exists
	var team Team
	if err := DB.First(&team, teamID).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// Read bytes into memory (MaxBytesReader already caps this)
	data, err := io.ReadAll(file)
	if err != nil || len(data) == 0 {
		http.Error(w, "Failed to read logo", http.StatusBadRequest)
		return
	}

	// Store in DB (upsert)
	logoRow := TeamLogo{
		TeamID:      teamID,
		ContentType: contentType,
		Data:        data,
		UpdatedAt:   time.Now(),
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "team_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"content_type", "data", "updated_at"}),
	}).Create(&logoRow).Error; err != nil {
		log.Printf("❌ Failed to store team logo in DB: %v", err)
		http.Error(w, "Failed to save logo", http.StatusInternalServerError)
		return
	}

	// Point team.logo_url to a versioned API endpoint to defeat aggressive caches.
	version := fmt.Sprintf("%d", logoRow.UpdatedAt.UnixNano())
	logoURL := fmt.Sprintf("/api/team/logo/%d/%s", teamID, version)
	if err := DB.Model(&Team{}).Where("id = ?", teamID).Update("logo_url", logoURL).Error; err != nil {
		log.Printf("❌ Failed to persist logo_url: %v", err)
		http.Error(w, "Failed to save logo", http.StatusInternalServerError)
		return
	}

	/*SendDiscordLog(
		fmt.Sprintf(
			"🖼️ **Team Logo Updated:** **%s** (Team #%d) by <@%d>",
			team.Name,
			team.ID,
			playerID,
		),
	)*/

	respondJSON(w, map[string]any{
		"success":      true,
		"logo_url":     logoURL,
		"logo_version": version,
	})
}

// --- Public: Serve a team logo from DB ---
// GET /api/team/logo/{teamID} and /api/team/logo/{teamID}/{version}
func HandleGetTeamLogo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	teamIDStr := strings.TrimSpace(vars["teamID"])
	_ = vars["version"] // optional; used only for cache busting
	teamID64, err := strconv.ParseUint(teamIDStr, 10, 64)
	if err != nil || teamID64 == 0 {
		http.Error(w, "Invalid team id", http.StatusBadRequest)
		return
	}
	teamID := uint(teamID64)

	var logo TeamLogo
	if err := DB.First(&logo, "team_id = ?", teamID).Error; err == nil && len(logo.Data) > 0 {
		// Strong caching here causes "logo won't change" issues (browser/CDN).
		// Use ETag + no-store so changes show immediately after upload.
		etag := fmt.Sprintf("\"%d\"", logo.UpdatedAt.UnixNano())
		if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" && inm == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("Content-Type", logo.ContentType)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.Header().Set("Surrogate-Control", "no-store")
		w.Header().Set("CDN-Cache-Control", "no-store")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(logo.Data)
		return
	}

	// No logo uploaded — generate a placeholder SVG with team initials
	var team Team
	if err := DB.First(&team, teamID).Error; err != nil {
		http.Error(w, "team not found", http.StatusNotFound)
		return
	}

	abbr := getTeamAbbreviation(team.Name)
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200" viewBox="0 0 200 200">
  <rect width="200" height="200" rx="16" fill="#374151"/>
  <text x="100" y="120" font-family="Arial, sans-serif" font-size="%d" font-weight="bold" fill="#D1D5DB" text-anchor="middle">%s</text>
</svg>`, getLogoSVGSize(abbr), abbr)

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(svg))
}

func getTeamAbbreviation(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "?"
	}
	words := strings.Fields(name)
	if len(words) == 1 {
		if len(words[0]) >= 2 {
			return strings.ToUpper(words[0][:2])
		}
		return strings.ToUpper(words[0])
	}
	abbr := ""
	for _, w := range words {
		if len(w) > 0 {
			abbr += strings.ToUpper(string(w[0]))
		}
	}
	if len(abbr) > 4 {
		abbr = abbr[:4]
	}
	return abbr
}

func getLogoSVGSize(abbr string) int {
	switch {
	case len(abbr) <= 2:
		return 72
	case len(abbr) == 3:
		return 56
	default:
		return 44
	}
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

func SendDiscordEmbedWithPings(title, description, buttonLabel, buttonURL string, mentionUserIDs []string) {
	botToken := getEnv("DISCORD_BOT_TOKEN", "")
	channelID := getEnv("DISCORD_LOG_CHANNEL_MATCHES", "")

	if botToken == "" || channelID == "" {
		log.Println("❌ Missing Discord env vars (Embed not sent)")
		return
	}

	allowedMentions := map[string]any{}
	body := map[string]any{
		"embeds": []any{
			map[string]any{
				"title":       title,
				"description": description,
				"color":       0x3498DB,
			},
		},
	}

	if len(mentionUserIDs) > 0 {
		mentionContent := ""
		for _, id := range mentionUserIDs {
			mentionContent += fmt.Sprintf("<@%s> ", id)
		}
		mentionContent = strings.TrimSpace(mentionContent)
		body["content"] = mentionContent
		allowedMentions["users"] = mentionUserIDs
		body["allowed_mentions"] = allowedMentions
	}

	if buttonLabel != "" && buttonURL != "" {
		body["components"] = []any{
			map[string]any{
				"type": 1,
				"components": []any{
					map[string]any{
						"type":  2,
						"style": 5,
						"label": buttonLabel,
						"url":   buttonURL,
					},
				},
			},
		}
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
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("❌ SendDiscordEmbed failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
		return
	}
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
	oldScheduled := match.ScheduledDate
	match.ScheduledDate = &date

	isTeamA := req.TeamID == match.TeamAID
	dateChanged := oldScheduled == nil || !oldScheduled.Equal(date)

	if isTeamA {
		match.TeamAScheduleConfirmed = true
		if dateChanged {
			match.TeamBScheduleConfirmed = false
		}
	} else {
		match.TeamBScheduleConfirmed = true
		if dateChanged {
			match.TeamAScheduleConfirmed = false
		}
	}

	if match.TeamAScheduleConfirmed && match.TeamBScheduleConfirmed {
		now2 := time.Now()
		match.ScheduleConfirmedAt = &now2
		match.Status = "Scheduled"
	} else {
		match.Status = "Pending Schedule Confirmation"
	}

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

	// Fetch teams
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	requestingTeam := teamA.Name
	if req.TeamID == match.TeamBID {
		requestingTeam = teamB.Name
	}

	getCaptainPings := func(teamID uint) string {
		var captains []TeamMember
		DB.Where("team_id = ? AND role IN ?", teamID, []string{"Captain", "Co-Captain"}).Find(&captains)
		if len(captains) == 0 {
			return "(no captains found)"
		}
		p := ""
		for _, m := range captains {
			p += fmt.Sprintf("<@%d> ", m.PlayerID)
		}
		return strings.TrimSpace(p)
	}

	captainA := getCaptainPings(match.TeamAID)
	captainB := getCaptainPings(match.TeamBID)

	frontendURL := getEnv("FRONTEND_URL", "https://gigglesquad.mooo.com")
	var mentionUserIDs []string
	addIDs := func(teamID uint) {
		var captains []TeamMember
		DB.Where("team_id = ? AND role IN ?", teamID, []string{"Captain", "Co-Captain"}).Find(&captains)
		for _, m := range captains {
			mentionUserIDs = append(mentionUserIDs, fmt.Sprintf("%d", m.PlayerID))
		}
	}
	addIDs(match.TeamAID)
	addIDs(match.TeamBID)

	SendDiscordEmbedToGeneral(
		fmt.Sprintf("📌 Match Time Request — %s", match.MatchCode),
		fmt.Sprintf(
			"🕐 **Proposed Match Time:** <t:%d:F> (<t:%d:R>)\n\n"+
				"Requested by: **%s**\n"+
				"Teams: **%s** vs **%s**\n"+
				"Status: Waiting on opponent confirmation.\n\n"+
				"🔵 Team A Captains:\n%s\n\n🔴 Team B Captains:\n%s",
			date.Unix(),
			date.Unix(),
			requestingTeam,
			teamA.Name,
			teamB.Name,
			captainA,
			captainB,
		),
		"View Match",
		fmt.Sprintf("%s/match/%d", frontendURL, match.ID),
		mentionUserIDs,
	)

	// 🔔 Notify the OTHER team's captains about the schedule proposal
	otherTeamID := match.TeamAID
	if req.TeamID == match.TeamAID {
		otherTeamID = match.TeamBID
	}
	notifyTeamCaptains(otherTeamID, "schedule_proposed",
		"Match Time Proposed",
		fmt.Sprintf("%s proposed a time for your match", requestingTeam),
		fmt.Sprintf("/match/%d", match.ID),
	)

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

	if !match.TeamAScheduleConfirmed || !match.TeamBScheduleConfirmed {
		http.Error(w, "Match schedule must be confirmed by both teams before submitting scores", http.StatusBadRequest)
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
	switch req.TeamID {
	case match.TeamAID:
		// Team A submitted → subA is theirs, subB is opponent
		match.LeagueSubA = subA
		match.LeagueSubB = subB
	case match.TeamBID:
		// Team B submitted → subA and subB must be flipped
		match.LeagueSubA = subB
		match.LeagueSubB = subA
	default:
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

	// 🔔 Notify the OTHER team's captains about score submission
	if changed {
		otherTeamID := match.TeamAID
		submittingTeam := teamB.Name
		if req.TeamID == match.TeamAID {
			otherTeamID = match.TeamBID
			submittingTeam = teamA.Name
		}
		notifyTeamCaptains(otherTeamID, "score_submitted",
			"Scores Submitted",
			fmt.Sprintf("%s submitted scores — please review and confirm", submittingTeam),
			fmt.Sprintf("/match/%d", match.ID),
		)
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
	Avatar      string `json:"avatar"`
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

	// ------------------------------------------------------------
	// Map scores (JSONB → legacy fallback)
	// ------------------------------------------------------------
	var mapScores []map[string]any
	var rawJSON sql.NullString

	if err := DB.Raw(`SELECT map_scores FROM matches WHERE id = ?`, match.ID).
		Scan(&rawJSON).Error; err == nil && rawJSON.Valid && strings.TrimSpace(rawJSON.String) != "" {
		_ = json.Unmarshal([]byte(rawJSON.String), &mapScores)
	}

	if len(mapScores) == 0 {
		var legacyMaps []MatchScore
		DB.Where("match_id = ?", match.ID).Find(&legacyMaps)
		for _, m := range legacyMaps {
			if m.TeamAScore == 0 && m.TeamBScore == 0 {
				continue
			}
			mapScores = append(mapScores, map[string]any{
				"map":          m.MapNumber,
				"mode":         m.Gamemode,
				"team_a_score": m.TeamAScore,
				"team_b_score": m.TeamBScore,
			})
		}
	}

	// ------------------------------------------------------------
	// Teams
	// ------------------------------------------------------------
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	// ------------------------------------------------------------
	// Rosters (THIS IS THE FIX)
	// ------------------------------------------------------------
	var rosterA, rosterB []MatchRosterPlayer
	frozen := isRosterFrozen(match)

	if frozen {
		// 🧊 Snapshot roster
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

	} else {
		// 🟢 LIVE roster (NO timestamps, NO DISTINCT ON)
		DB.Raw(`
			SELECT
				p.id AS player_id,
				p.display_name,
				p.username,
				p.avatar,
				tm.role
			FROM team_members tm
			JOIN players p ON p.id = tm.player_id
			WHERE tm.team_id = ?
		`, match.TeamAID).Scan(&rosterA)

		DB.Raw(`
			SELECT
				p.id AS player_id,
				p.display_name,
				p.username,
				p.avatar,
				tm.role
			FROM team_members tm
			JOIN players p ON p.id = tm.player_id
			WHERE tm.team_id = ?
		`, match.TeamBID).Scan(&rosterB)
	}

	// ------------------------------------------------------------
	// Cast
	// ------------------------------------------------------------
	var cast CastLogMulti
	castErr := DB.Where("match_id = ?", match.ID).First(&cast).Error

	var casterIDs []int64
	if castErr == nil {
		_ = json.Unmarshal(cast.Casters, &casterIDs)
	}

	casters := make([]string, 0, len(casterIDs))
	for _, id := range casterIDs {
		casters = append(casters, strconv.FormatInt(id, 10))
	}

	camera := ""
	if castErr == nil && cast.CameraID != 0 {
		camera = strconv.FormatInt(cast.CameraID, 10)
	}

	// ------------------------------------------------------------
	// Response
	// ------------------------------------------------------------
	respondJSON(w, map[string]any{
		"match":      match,
		"teams":      map[string]any{"a": teamA, "b": teamB},
		"map_scores": mapScores,
		"roster": map[string]any{
			"a": stringifyRosterPlayers(rosterA),
			"b": stringifyRosterPlayers(rosterB),
		},
		"cast": map[string]any{
			"active":     castErr == nil && len(casters) > 0,
			"casters":    casters,
			"camera":     camera,
			"stream_url": cast.StreamURL,
		},
	})
}

// GET /api/overlay/match/{id}
// Lightweight endpoint for live stream overlays — returns team names, absolute logo URLs, and scores.
func HandleOverlayMatch(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	matchID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid match ID", http.StatusBadRequest)
		return
	}

	var match Match
	if err := DB.First(&match, matchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	baseURL := strings.TrimRight(getEnv("FRONTEND_URL", "https://gigglesquad.mooo.com"), "/")

	logoA := teamA.LogoURL
	if logoA == "" {
		logoA = fmt.Sprintf("%s/api/team/logo/%d", baseURL, teamA.ID)
	} else if !strings.HasPrefix(logoA, "http") {
		logoA = baseURL + logoA
	}

	logoB := teamB.LogoURL
	if logoB == "" {
		logoB = fmt.Sprintf("%s/api/team/logo/%d", baseURL, teamB.ID)
	} else if !strings.HasPrefix(logoB, "http") {
		logoB = baseURL + logoB
	}

	respondJSON(w, map[string]any{
		"match_code": match.MatchCode,
		"status":     match.Status,
		"team_a": map[string]any{
			"id":       teamA.ID,
			"name":     teamA.Name,
			"logo_url": logoA,
			"score":    match.TeamAScore,
		},
		"team_b": map[string]any{
			"id":       teamB.ID,
			"name":     teamB.Name,
			"logo_url": logoB,
			"score":    match.TeamBScore,
		},
	})
}

func isRosterFrozen(match Match) bool {
	status := strings.TrimSpace(strings.ToLower(match.Status))

	switch status {
	case "completed", "finished", "forfeit", "forfeited", "double forfeit":
		return true
	default:
		return false
	}
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

func FreezeMatchRoster(matchID uint) error {
	var match Match
	if err := DB.First(&match, matchID).Error; err != nil {
		return err
	}

	// Prevent double-freeze
	var count int64
	DB.Model(&MatchRoster{}).
		Where("match_id = ?", matchID).
		Count(&count)

	if count > 0 {
		return nil
	}

	// Snapshot Team A
	DB.Exec(`
		INSERT INTO match_rosters (match_id, team_id, player_id, display_name, username, role)
		SELECT ?, tm.team_id, p.id, p.display_name, p.username, tm.role
		FROM team_members tm
		JOIN players p ON p.id = tm.player_id
		WHERE tm.team_id = ?
	`, matchID, match.TeamAID)

	// Snapshot Team B
	DB.Exec(`
		INSERT INTO match_rosters (match_id, team_id, player_id, display_name, username, role)
		SELECT ?, tm.team_id, p.id, p.display_name, p.username, tm.role
		FROM team_members tm
		JOIN players p ON p.id = tm.player_id
		WHERE tm.team_id = ?
	`, matchID, match.TeamBID)

	return nil
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
		SELECT season, archive_rating, archive_wins, archive_losses, archive_matches, NULL AS archive_team
		FROM player_stats_archive
		WHERE player_id = ?
		ORDER BY season ASC
	`, playerID).Scan(&archived)

	// --- Final Response
	respondJSON(w, map[string]any{
		"id":              strconv.FormatInt(player.ID, 10),
		"username":        player.Username,
		"display_name":    player.DisplayName,
		"avatar":          player.Avatar,
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
func modActionLabel(r *http.Request) string {
	path := r.URL.Path
	method := r.Method

	switch {
	// Matches
	case method == http.MethodPost && path == "/api/mod/match/reset":
		return "Match Reset"
	case method == http.MethodPost && path == "/api/mod/match/reset-schedule":
		return "Reset Match Schedule"
	case method == http.MethodPost && path == "/api/mod/match/forfeit":
		return "Match Forfeit"
	case method == http.MethodPost && path == "/api/mod/match/double-forfeit":
		return "Match Double Forfeit"
	case method == http.MethodDelete && path == "/api/mod/match":
		return "Delete Match"
	case method == http.MethodPost && path == "/api/mod/match/add":
		return "Add Match"
	case method == http.MethodPost && path == "/api/mod/match/set-maps":
		return "Set Match Maps"
	case method == http.MethodPost && path == "/api/mod/match/delete":
		return "Delete Match (POST)"
	case method == http.MethodPost && path == "/api/mod/match/schedule":
		return "Force Schedule Match"
	case method == http.MethodPost && path == "/api/mod/match/edit-score":
		return "Edit Match Score"
	case method == http.MethodPost && path == "/api/mod/matches/generate":
		return "Generate Weekly Matches"
	case method == http.MethodGet && path == "/api/mod/matches/preview":
		return "Preview Weekly Matches"
	case method == http.MethodPost && path == "/api/mod/matches/clear-week":
		return "Clear Week Matches"

	// Teams
	case method == http.MethodPost && path == "/api/mod/team/set-active":
		return "Set Team Active"
	case method == http.MethodPost && path == "/api/mod/teams/set-all-active":
		return "Set All Teams Active"
	case method == http.MethodPost && path == "/api/mod/team/set-inactive":
		return "Set Team Inactive"
	case method == http.MethodPost && path == "/api/mod/teams/set-all-inactive":
		return "Set All Teams Inactive"
	case method == http.MethodPost && path == "/api/mod/team/rename":
		return "Rename Team"
	case method == http.MethodPost && path == "/api/mod/team/adjust-rating":
		return "Adjust Team Rating"
	case method == http.MethodPost && path == "/api/mod/team/disband":
		return "Disband Team"
	case method == http.MethodPost && path == "/api/mod/team/delete":
		return "Delete Team"
	case method == http.MethodGet && path == "/api/mod/team/history":
		return "View Team Rename History"
	case method == http.MethodPost && path == "/api/mod/team/add-player":
		return "Add Player To Team"
	case method == http.MethodPost && path == "/api/mod/team/adjust-stats":
		return "Adjust Team Stats"
	case method == http.MethodGet && path == "/api/mod/team/stats":
		return "Get Team Stats"
	case method == http.MethodGet && path == "/api/mod/team/members":
		return "Get Team Members"
	case method == http.MethodPost && path == "/api/mod/team/set-role":
		return "Set Team Member Role"
	case method == http.MethodPost && path == "/api/mod/team/promote-captain":
		return "Promote To Captain"
	case method == http.MethodPost && path == "/api/mod/team/lock":
		return "Lock/Unlock Team"

	// Players
	case method == http.MethodPost && path == "/api/mod/player/kick":
		return "Kick Player"
	case method == http.MethodPost && path == "/api/mod/player/ban":
		return "Ban Player"
	case method == http.MethodPost && path == "/api/mod/player/unban":
		return "Unban Player"
	case method == http.MethodPost && path == "/api/mod/player/adjust-stats":
		return "Adjust Player Stats"
	case method == http.MethodGet && path == "/api/mod/player/stats":
		return "Get Player Stats"
	case method == http.MethodPost && path == "/api/mod/player/remove-cooldown":
		return "Remove Player Cooldown"
	case method == http.MethodPost && path == "/api/mod/player/archive-all":
		return "Archive All Players"

	// League settings / tools
	case method == http.MethodPost && path == "/api/mod/leaderboard/reset":
		return "Reset Team Leaderboard"
	case method == http.MethodPost && path == "/api/mod/reset_player_leaderboard":
		return "Reset Player Leaderboard"
	case method == http.MethodPost && path == "/api/mod/season/archive":
		return "Archive Season"
	case method == http.MethodPost && path == "/api/mod/roster/lock-all":
		return "Lock All Rosters"
	case method == http.MethodPost && path == "/api/mod/roster/unlock-all":
		return "Unlock All Rosters"
	case method == http.MethodGet && path == "/api/mod/roster/status":
		return "Get Roster Lock Status"
	case method == http.MethodPost && path == "/api/mod/sync-roles":
		return "Sync Discord Roles"
	case method == http.MethodPost && path == "/api/mod/challenges/enable":
		return "Enable Global Challenges"
	case method == http.MethodPost && path == "/api/mod/challenges/disable":
		return "Disable Global Challenges"

	// Finals
	case method == http.MethodPost && path == "/api/mod/finals/archive":
		return "Archive Finals"
	case method == http.MethodPost && path == "/api/mod/finals/add-team":
		return "Finals: Add Team"
	case method == http.MethodPost && path == "/api/mod/finals/remove-team":
		return "Finals: Remove Team"
	case method == http.MethodPost && path == "/api/mod/finals/generate":
		return "Finals: Generate Bracket"
	case method == http.MethodPost && path == "/api/mod/finals/reset":
		return "Finals: Reset"
	case method == http.MethodPost && path == "/api/mod/finals/update-match":
		return "Finals: Update Match"
	case method == http.MethodPost && path == "/api/mod/finals/clear-bracket-view":
		return "Finals: Clear Bracket View"
	case method == http.MethodPost && path == "/api/mod/finals/set-seeds":
		return "Finals: Update Seeds"
	}

	if strings.HasPrefix(path, "/api/mod/") {
		return "Mod Action: " + strings.TrimPrefix(path, "/api/mod/")
	}
	return "Mod Action"
}

func modTargetSummary(r *http.Request) string {
	add := func(parts []string, s string) []string {
		s = strings.TrimSpace(s)
		if s == "" {
			return parts
		}
		return append(parts, s)
	}

	// Collect common targets from query string first (GET endpoints)
	parts := []string{}
	q := r.URL.Query()
	path := strings.TrimSpace(r.URL.Path)

	// Some older endpoints use ?id= for team/player.
	idParam := strings.TrimSpace(q.Get("id"))

	teamIDStr := strings.TrimSpace(q.Get("team_id"))
	playerIDStr := strings.TrimSpace(q.Get("player_id"))
	matchIDStr := strings.TrimSpace(q.Get("match_id"))
	seasonStr := strings.TrimSpace(q.Get("season"))
	weekStr := strings.TrimSpace(q.Get("week"))

	if idParam != "" {
		// Heuristic mapping of ?id=
		if teamIDStr == "" && (strings.Contains(path, "/team/") || strings.Contains(path, "/teams/")) {
			teamIDStr = idParam
		} else if playerIDStr == "" && strings.Contains(path, "/player/") {
			playerIDStr = idParam
		} else {
			parts = add(parts, "id="+idParam)
		}
	}

	// season/week are appended later after body/query normalization

	// For JSON-body endpoints, peek body and restore it for the handler.
	if r.Body == nil {
		return strings.Join(parts, " ")
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		// Best effort: restore empty body and return what we have.
		r.Body = io.NopCloser(bytes.NewReader(nil))
		return strings.Join(parts, " ")
	}
	// Restore body for downstream handler.
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		return strings.Join(parts, " ")
	}

	var m map[string]any
	if err := json.Unmarshal(bodyBytes, &m); err != nil {
		return strings.Join(parts, " ")
	}

	getString := func(key string) string {
		v, ok := m[key]
		if !ok || v == nil {
			return ""
		}
		switch t := v.(type) {
		case string:
			return strings.TrimSpace(t)
		case float64:
			if t == 0 {
				return ""
			}
			return fmt.Sprintf("%.0f", t)
		case json.Number:
			return strings.TrimSpace(t.String())
		default:
			return ""
		}
	}

	// Common body targets
	if playerIDStr == "" {
		playerIDStr = getString("player_id")
	}
	if teamIDStr == "" {
		teamIDStr = getString("team_id")
	}
	if matchIDStr == "" {
		matchIDStr = getString("match_id")
	}
	if v := getString("challenge_id"); v != "" {
		parts = add(parts, "challenge_id="+v)
	}
	if seasonStr == "" {
		seasonStr = getString("season")
	}
	if weekStr == "" {
		weekStr = getString("week")
	}
	if seasonStr != "" {
		parts = add(parts, "season="+seasonStr)
	}
	if weekStr != "" {
		parts = add(parts, "week="+weekStr)
	}

	// Useful “what changed” hints
	if v := getString("new_name"); v != "" {
		parts = add(parts, "new_name=\""+v+"\"")
	}
	if v := getString("reason"); v != "" {
		parts = add(parts, "reason=\""+v+"\"")
	}
	if v := getString("enabled"); v != "" {
		parts = add(parts, "enabled="+v)
	}
	if v := getString("scope"); v != "" {
		parts = add(parts, "scope="+v)
	}

	// Enrich common IDs with names (best-effort)
	if playerIDStr != "" {
		pid, err := strconv.ParseInt(playerIDStr, 10, 64)
		if err == nil && pid != 0 {
			var p Player
			if err := DB.Select("display_name, username").First(&p, "id = ?", pid).Error; err == nil {
				display := strings.TrimSpace(p.DisplayName)
				if display == "" {
					display = strings.TrimSpace(p.Username)
				}
				if display != "" {
					parts = add(parts, fmt.Sprintf("player=<@%d> (%s)", pid, display))
				} else {
					parts = add(parts, fmt.Sprintf("player=<@%d>", pid))
				}
			} else {
				parts = add(parts, fmt.Sprintf("player=<@%d>", pid))
			}
		}
	}

	if teamIDStr != "" {
		tid, err := strconv.ParseUint(teamIDStr, 10, 64)
		if err == nil && tid != 0 {
			var t Team
			if err := DB.Select("name").First(&t, "id = ?", uint(tid)).Error; err == nil {
				name := strings.TrimSpace(t.Name)
				if name != "" {
					parts = add(parts, fmt.Sprintf("team=\"%s\"(#%d)", name, tid))
				} else {
					parts = add(parts, fmt.Sprintf("team_id=%d", tid))
				}
			} else {
				parts = add(parts, fmt.Sprintf("team_id=%d", tid))
			}
		}
	}

	if matchIDStr != "" {
		mid, err := strconv.ParseUint(matchIDStr, 10, 64)
		if err == nil && mid != 0 {
			var mMatch Match
			if err := DB.Select("match_code, team_a_id, team_b_id").First(&mMatch, "id = ?", uint(mid)).Error; err == nil {
				mc := strings.TrimSpace(mMatch.MatchCode)
				if mc != "" {
					parts = add(parts, fmt.Sprintf("match=%s(#%d)", mc, mid))
				} else {
					parts = add(parts, fmt.Sprintf("match_id=%d", mid))
				}
			} else {
				parts = add(parts, fmt.Sprintf("match_id=%d", mid))
			}
		}
	}

	return strings.Join(parts, " ")
}

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
			// Don't record audit entries for viewing the audit log itself.
			if r.Method == http.MethodGet && r.URL.Path == "/api/mod/audit-logs" {
				return discordID, true
			}
			// Console audit log for *all* mod actions (even those without Discord logs)
			// Use RequestURI to include query string when present.
			username := strings.TrimSpace(member.User.Username)
			action := modActionLabel(r)
			target := modTargetSummary(r)
			RecordModAudit(ModAuditEntry{
				Time:          time.Now(),
				Action:        action,
				Method:        r.Method,
				Path:          r.URL.Path,
				RequestURI:    r.URL.RequestURI(),
				Target:        target,
				ActorID:       discordID,
				ActorUsername: username,
			})
			if username != "" {
				if target != "" {
					log.Printf("🧰 [MOD] %s — %s %s — %s (by %s / %s)", action, r.Method, r.URL.RequestURI(), target, username, discordID)
				} else {
					log.Printf("🧰 [MOD] %s — %s %s (by %s / %s)", action, r.Method, r.URL.RequestURI(), username, discordID)
				}
			} else {
				if target != "" {
					log.Printf("🧰 [MOD] %s — %s %s — %s (by %s)", action, r.Method, r.URL.RequestURI(), target, discordID)
				} else {
					log.Printf("🧰 [MOD] %s — %s %s (by %s)", action, r.Method, r.URL.RequestURI(), discordID)
				}
			}
			return discordID, true
		}
	}

	log.Printf("🚫 User %s missing League Mod role %s", discordID, modRoleID)
	http.Error(w, "Forbidden: missing League Mod role", http.StatusForbidden)
	return "", false
}

// requireDev checks if the user has the DISCORD_DEV_ROLE_ID role.
func requireDev(w http.ResponseWriter, r *http.Request) (string, bool) {
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", false
	}

	guildID := os.Getenv("DISCORD_GUILD_ID")
	devRoleID := os.Getenv("DISCORD_DEV_ROLE_ID")
	botToken := os.Getenv("DISCORD_BOT_TOKEN")

	if guildID == "" || devRoleID == "" || botToken == "" {
		http.Error(w, "Server not configured for dev role check", http.StatusInternalServerError)
		return "", false
	}

	req, _ := http.NewRequest("GET",
		fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, discordID),
		nil)
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "Failed to reach Discord API", http.StatusInternalServerError)
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		http.Error(w, "Failed to verify Discord role", http.StatusForbidden)
		return "", false
	}

	var member struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&member); err != nil {
		http.Error(w, "Failed to parse Discord response", http.StatusInternalServerError)
		return "", false
	}

	for _, role := range member.Roles {
		if role == devRoleID {
			return discordID, true
		}
	}

	http.Error(w, "Forbidden: missing Dev role", http.StatusForbidden)
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
	m.Status = "Forfeit"

	// Clear map scores
	DB.Where("match_id = ?", m.ID).Delete(&MatchScore{})

	if err := DB.Save(&m).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to set forfeit")
		return
	}

	// Snapshot both teams
	snapshotTeamRoster(m.TeamAID, currentSeason)
	snapshotTeamRoster(m.TeamBID, currentSeason)

	// Freeze match rosters so the detail page shows the rosters at forfeit time
	FreezeMatchRoster(m.ID)

	// Update leaderboards — winner gains ELO + win, loser gets loss
	updateLeaderboards(req.WinnerTeamID, loser, m.MatchCode)

	// Fetch team names
	var teamA, teamB Team
	DB.First(&teamA, m.TeamAID)
	DB.First(&teamB, m.TeamBID)

	winnerTeam := teamA
	loserTeam := teamB
	if req.WinnerTeamID == teamB.ID {
		winnerTeam, loserTeam = teamB, teamA
	}

	// ⭐ MOD LOG → score log channel with embed matching finalized format
	content := fmt.Sprintf(
		"🏳️ **Match Forfeited: %s vs %s**\n%s",
		winnerTeam.Name, loserTeam.Name,
		getAllTeamPings(winnerTeam.ID)+"\n"+getAllTeamPings(loserTeam.ID),
	)

	subAName := "None"
	subBName := "None"
	if m.LeagueSubA != nil {
		var p Player
		DB.First(&p, *m.LeagueSubA)
		subAName = p.DisplayName
	}
	if m.LeagueSubB != nil {
		var p Player
		DB.First(&p, *m.LeagueSubB)
		subBName = p.DisplayName
	}

	desc := fmt.Sprintf(
		"**%s vs %s**\n\n📘 **Match ID**\n%s\n\n🧍 **League Subs**\n• %s Sub: **%s**\n• %s Sub: **%s**\n\n🏳️ **Forfeit Winner**\n%s\n\n🛡️ **Forced by**\n<@%s>",
		winnerTeam.Name, loserTeam.Name,
		m.MatchCode,
		winnerTeam.Name, subAName,
		loserTeam.Name, subBName,
		winnerTeam.Name,
		actorDiscordID,
	)

	SendScoreEmbedWithPings(content, "🏳️ Match Forfeited", desc, 0xE74C3C)

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
	actorDiscordID, ok := requireLeagueMod(w, r)
	if !ok {
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
	m.Status = "Forfeit" // double forfeit = both teams forfeited

	if err := DB.Save(&m).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to set double forfeit")
		return
	}

	// 🧩 Snapshot both rosters for historical record
	snapshotTeamRoster(m.TeamAID, currentSeason)
	snapshotTeamRoster(m.TeamBID, currentSeason)

	// Fetch team names for Discord embed
	var teamA, teamB Team
	DB.First(&teamA, m.TeamAID)
	DB.First(&teamB, m.TeamBID)

	content := fmt.Sprintf(
		"🏳️‍⚖️ **Double Forfeit: %s vs %s**\n%s",
		teamA.Name, teamB.Name,
		getAllTeamPings(teamA.ID)+"\n"+getAllTeamPings(teamB.ID),
	)

	desc := fmt.Sprintf(
		"**%s vs %s**\n\n📘 **Match ID**\n%s\n\n🏳️‍⚖️ **Result**\nBoth teams forfeited — no winner.\n\n🛡️ **Forced by**\n<@%s>",
		teamA.Name, teamB.Name,
		m.MatchCode,
		actorDiscordID,
	)

	SendScoreEmbedWithPings(content, "🏳️‍⚖️ Double Forfeit", desc, 0xE67E22)

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
	t.Rating = clampRating(t.Rating)
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
	/*LogGeneral(
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
	)*/

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

	// 🔔 Notify both teams' captains about deletion
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)
	notifyTeamCaptains(match.TeamAID, "match_deleted",
		"Match Removed",
		fmt.Sprintf("A mod removed your match vs %s (%s)", teamB.Name, match.MatchCode),
		"/matchups",
	)
	notifyTeamCaptains(match.TeamBID, "match_deleted",
		"Match Removed",
		fmt.Sprintf("A mod removed your match vs %s (%s)", teamA.Name, match.MatchCode),
		"/matchups",
	)

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

	if err := DB.Save(&match).Error; err != nil {
		log.Printf("❌ Failed to save schedule confirmation for match %d: %v", match.ID, err)
		http.Error(w, "failed to save confirmation", http.StatusInternalServerError)
		return
	}

	log.Printf("ℹ️ Confirm schedule %d by team %d: A=%t B=%t", match.ID, req.TeamID, match.TeamAScheduleConfirmed, match.TeamBScheduleConfirmed)

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

		// 🚀 Create Discord match channel once schedule is fully confirmed
		botToken := getEnv("DISCORD_BOT_TOKEN", "")
		if botToken != "" {
			dg, err := discordgo.New("Bot " + botToken)
			if err == nil {
				go scheduleMatchChannel(dg, &match)
			} else {
				log.Printf("⚠️ Failed to create Discord session for match channel: %v", err)
			}
		}

		// ================================
		// 🔍 Load team rosters
		// ================================
		var teamAMembers, teamBMembers []TeamMember
		DB.Where("team_id = ?", match.TeamAID).Find(&teamAMembers)
		DB.Where("team_id = ?", match.TeamBID).Find(&teamBMembers)

		// ================================
		// 🧠 Format roster pings (clean)
		// ================================
		formatEmbedPings := func(list []TeamMember) string {
			if len(list) == 0 {
				return "*No players found*"
			}
			if len(list) > 15 {
				// Show first 10, hide the rest inside embed for readability
				p := ""
				for i := 0; i < 10; i++ {
					p += fmt.Sprintf("<@%d> ", list[i].PlayerID)
				}
				return fmt.Sprintf("%s\n…and **%d more**", p, len(list)-10)
			}
			p := ""
			for _, m := range list {
				p += fmt.Sprintf("<@%d> ", m.PlayerID)
			}
			return p
		}

		pingA := formatEmbedPings(teamAMembers)
		pingB := formatEmbedPings(teamBMembers)

		// ================================
		// 📅 Include scheduled date/time
		// ================================
		scheduledDate := "Not Set"
		scheduledDateRel := ""
		if match.ScheduledDate != nil {
			scheduledDate = fmt.Sprintf("<t:%d:F>", match.ScheduledDate.Unix())
			scheduledDateRel = fmt.Sprintf("<t:%d:R>", match.ScheduledDate.Unix())
		}

		// ================================
		// 📝 Build log message
		// ================================
		var logMsg string

		if isReschedule {
			// 🔁 Reschedule log
			logMsg = fmt.Sprintf(
				"🔁 **Match Rescheduled:** %s\n"+
					"🕐 **%s** (%s)\n\n"+
					"Teams: **%s** vs **%s**\n"+
					"Rescheduled by <@%s>\n\n"+
					"🔵 **Team %s Players:**\n%s\n\n"+
					"🔴 **Team %s Players:**\n%s",
				match.MatchCode,
				scheduledDate,
				scheduledDateRel,
				teamA.Name, teamB.Name,
				actorDiscordID,
				teamA.Name, pingA,
				teamB.Name, pingB,
			)
		} else {
			// 🆕 Initial schedule log
			logMsg = fmt.Sprintf(
				"📅 **Match Scheduled:** %s\n"+
					"🕐 **%s** (%s)\n\n"+
					"Teams: **%s** vs **%s**\n"+
					"Confirmed by <@%s>\n\n"+
					"🔵 **Team %s Players:**\n%s\n\n"+
					"🔴 **Team %s Players:**\n%s",
				match.MatchCode,
				scheduledDate,
				scheduledDateRel,
				teamA.Name, teamB.Name,
				actorDiscordID,
				teamA.Name, pingA,
				teamB.Name, pingB,
			)
		}

		// ================================
		// 📤 Send schedule log to MATCHES channel
		// ================================
		mentionSet := make(map[string]bool)
		for _, m := range teamAMembers {
			mentionSet[fmt.Sprintf("%d", m.PlayerID)] = true
		}
		for _, m := range teamBMembers {
			mentionSet[fmt.Sprintf("%d", m.PlayerID)] = true
		}
		if actorDiscordID != "" {
			mentionSet[actorDiscordID] = true
		}

		var mentionUserIDs []string
		for id := range mentionSet {
			mentionUserIDs = append(mentionUserIDs, id)
		}

		frontendURL := getEnv("FRONTEND_URL", "https://gigglesquad.mooo.com")
		log.Printf("✅ Match %d fully confirmed; sending scheduled match log to Discord", match.ID)
		SendDiscordEmbedWithPings(
			fmt.Sprintf("📅 Match Scheduled — %s", match.MatchCode),
			logMsg,
			"View Match",
			fmt.Sprintf("%s/match/%d", frontendURL, match.ID),
			mentionUserIDs,
		)
	}

	respondJSON(w, map[string]any{
		"success":  true,
		"status":   match.Status,
		"match_id": match.ID,
	})
}

func SendScoreEmbedWithPings(content, title, description string, color int) {
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
				"color":       color,
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

		captainPings := getBothCaptainPings(opposingTeam.ID)

		SendDiscordLog(
			fmt.Sprintf(
				"📝 **%s confirmed scores for Match %s**\n👥 Teams: %s vs %s\n👤 By: <@%d>\n⏳ Waiting on: %s captains %s",
				confirmingTeam.Name,
				match.MatchCode,
				teamA.Name, teamB.Name,
				submitterID,
				opposingTeam.Name,
				captainPings,
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
		updateLeaderboards(*match.WinnerID, *match.LoserID, match.MatchCode)
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
			p.Rating = clampRating(p.Rating)
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

	SendScoreEmbedWithPings(content, "🏆 Final Match Result", desc, 0x2ECC71)

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
// Applies underdog bonus based on rating difference; challenge matches get a multiplier.
func updateLeaderboards(winnerID, loserID uint, matchCode string) {
	// Read from .env or fallback
	eloWinPoints := getEnvInt("ELO_WIN_POINTS", 25)
	eloLossPoints := getEnvInt("ELO_LOSS_POINTS", -25)
	defaultPlayerRating := getEnvInt("DEFAULT_PLAYER_RATING", 800)
	defaultTeamRating := getEnvInt("DEFAULT_TEAM_RATING", 800)
	underdogBonusPer100 := getEnvInt("UNDERDOG_BONUS_PER_100", 10)
	underdogLossReductionPer100 := getEnvInt("UNDERDOG_LOSS_REDUCTION_PER_100", 5)
	challengeMultiplier := getEnvInt("CHALLENGE_BONUS_MULTIPLIER", 2)
	placementMatches := getEnvInt("PLACEMENT_MATCHES", 3)

	isChallenge := strings.Contains(matchCode, "CHAL")

	var winner, loser Team
	winnerFound := DB.First(&winner, winnerID).Error == nil
	loserFound := DB.First(&loser, loserID).Error == nil

	// --- TEAM STATS (always increment W/L/matches) ---
	if winnerFound {
		winner.Wins++
		winner.Matches++
	}
	if loserFound {
		loser.Losses++
		loser.Matches++
	}

	// --- PLACEMENT CHECK: only adjust ratings if BOTH teams are out of placement ---
	winnerInPlacement := winnerFound && winner.Matches <= placementMatches
	loserInPlacement := loserFound && loser.Matches <= placementMatches

	if !winnerInPlacement && !loserInPlacement {
		// Both placed — normal ELO adjustment
		underdogBonus := 0
		if winnerFound && loserFound && loser.Rating > winner.Rating {
			diff := loser.Rating - winner.Rating
			underdogBonus = diff * underdogBonusPer100 / 100
			if isChallenge {
				underdogBonus *= challengeMultiplier
			}
		}

		lossReduction := 0
		if winnerFound && loserFound && winner.Rating > loser.Rating {
			diff := winner.Rating - loser.Rating
			lossReduction = diff * underdogLossReductionPer100 / 100
		}

		actualWinPts := eloWinPoints + underdogBonus
		actualLossPts := eloLossPoints + lossReduction
		if actualLossPts > 0 {
			actualLossPts = 0
		}

		if winnerFound {
			winner.Rating += actualWinPts
			winner.Rating = clampRating(winner.Rating)
		}
		if loserFound {
			loser.Rating += actualLossPts
			loser.Rating = clampRating(loser.Rating)
		}

		log.Printf("📊 Leaderboards updated (win: %+d, loss: %+d, challenge=%v): winner=%d loser=%d",
			actualWinPts, actualLossPts, isChallenge, winnerID, loserID)
	} else {
		log.Printf("📊 Placement match recorded: winner=%d (matches=%d) loser=%d (matches=%d)",
			winnerID, winner.Matches, loserID, loser.Matches)
	}

	// --- Check if either team just completed placement ---
	if winnerFound && winner.Matches == placementMatches {
		// Calculate initial rating from W/L
		winner.Rating = defaultTeamRating + (winner.Wins * eloWinPoints) + ((winner.Matches - winner.Wins) * eloLossPoints)
		winner.Rating = clampRating(winner.Rating)
		log.Printf("🎓 Team %d (%s) completed placement: %dW/%dL → rating %d",
			winner.ID, winner.Name, winner.Wins, winner.Losses, winner.Rating)
	}
	if loserFound && loser.Matches == placementMatches {
		loser.Rating = defaultTeamRating + (loser.Wins * eloWinPoints) + ((loser.Matches - loser.Wins) * eloLossPoints)
		loser.Rating = clampRating(loser.Rating)
		log.Printf("🎓 Team %d (%s) completed placement: %dW/%dL → rating %d",
			loser.ID, loser.Name, loser.Wins, loser.Losses, loser.Rating)
	}

	// Save teams
	if winnerFound {
		DB.Save(&winner)
	}
	if loserFound {
		DB.Save(&loser)
	}

	// --- Player stats ---
	var winners, losers []TeamMember
	DB.Where("team_id = ?", winnerID).Find(&winners)
	DB.Where("team_id = ?", loserID).Find(&losers)

	// Players on placement teams don't get rating changes either
	if !winnerInPlacement {
		actualWinPts := eloWinPoints
		if winnerFound && loserFound && loser.Rating > winner.Rating {
			diff := loser.Rating - winner.Rating
			bonus := diff * underdogBonusPer100 / 100
			if isChallenge {
				bonus *= challengeMultiplier
			}
			actualWinPts += bonus
		}
		for _, w := range winners {
			DB.Model(&Player{}).Where("id = ?", w.PlayerID).
				Updates(map[string]any{
					"wins":    gorm.Expr("wins + 1"),
					"matches": gorm.Expr("matches + 1"),
					"rating":  gorm.Expr("LEAST(COALESCE(rating, ?) + ?, ?)", defaultPlayerRating, actualWinPts, getEnvInt("MAX_RATING", 1300)),
				})
		}
	} else {
		for _, w := range winners {
			DB.Model(&Player{}).Where("id = ?", w.PlayerID).
				Updates(map[string]any{
					"wins":    gorm.Expr("wins + 1"),
					"matches": gorm.Expr("matches + 1"),
				})
		}
	}

	if !loserInPlacement {
		actualLossPts := eloLossPoints
		if winnerFound && loserFound && winner.Rating > loser.Rating {
			diff := winner.Rating - loser.Rating
			actualLossPts += diff * underdogLossReductionPer100 / 100
		}
		if actualLossPts > 0 {
			actualLossPts = 0
		}
		for _, l := range losers {
			DB.Model(&Player{}).Where("id = ?", l.PlayerID).
				Updates(map[string]any{
					"losses":  gorm.Expr("losses + 1"),
					"matches": gorm.Expr("matches + 1"),
					"rating":  gorm.Expr("GREATEST(COALESCE(rating, ?) + ?, ?)", defaultPlayerRating, actualLossPts, getEnvInt("MIN_RATING", 0)),
				})
		}
	} else {
		for _, l := range losers {
			DB.Model(&Player{}).Where("id = ?", l.PlayerID).
				Updates(map[string]any{
					"losses":  gorm.Expr("losses + 1"),
					"matches": gorm.Expr("matches + 1"),
				})
		}
	}
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
		ChangedBy string    `json:"changed_by"`
		ChangedAt time.Time `json:"changed_at"`
		Changer   string    `json:"changer"` // username if available
	}

	query := `
		SELECT 
			th.id, th.team_id, th.old_name, th.new_name, CAST(th.changed_by AS TEXT) AS changed_by, th.changed_at,
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
	respondJSON(w, map[string]any{"locked": isRosterLocked()})
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

	// 🔔 Notify target team captains in-app
	notifyTeamCaptains(target.ID, "challenge_received",
		"Challenge Match Received",
		fmt.Sprintf("%s has challenged your team", requester.Name),
		"/myteam",
	)

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

// HandleGoLive checks configured Twitch/YouTube accounts for live streams
// and announces a live cast to Discord, pinging the caster role.
func HandleGoLive(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		session, _ := store.Get(r, "session")
		discordIDStr, _ := session.Values["discord_id"].(string)
		if discordIDStr == "" || !userHasDiscordRole(discordIDStr, os.Getenv("DISCORD_CASTER_ROLE_ID")) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	var req struct {
		MatchID uint `json:"match_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.MatchID == 0 {
		http.Error(w, "Missing match_id", http.StatusBadRequest)
		return
	}

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	// Build stream URL from env
	streamURL := os.Getenv("TWITCH_CHANNEL")
	if streamURL == "" {
		var multi CastLogMulti
		if DB.Where("match_id = ?", req.MatchID).First(&multi).Error == nil && multi.StreamURL != "" {
			streamURL = multi.StreamURL
		}
	}

	msg := ""
	if streamURL != "" {
		msg = fmt.Sprintf("# [ECGL](%s) We are live now casting **%s** vs **%s**", streamURL, teamA.Name, teamB.Name)
	} else {
		msg = fmt.Sprintf("# 🔴 We are live now casting **%s** vs **%s**", teamA.Name, teamB.Name)
	}

	pingRoles := os.Getenv("DISCORD_GO_LIVE_PING_ROLES")
	if pingRoles != "" {
		pings := []string{}
		for _, roleID := range strings.Split(pingRoles, ",") {
			roleID = strings.TrimSpace(roleID)
			if roleID != "" {
				pings = append(pings, fmt.Sprintf("<@&%s>", roleID))
			}
		}
		if len(pings) > 0 {
			msg += "\n\n" + strings.Join(pings, " ")
		}
	}

	channelID := os.Getenv("DISCORD_GO_LIVE_CHANNEL")
	if channelID == "" {
		channelID = os.Getenv("DISCORD_LOG_CHANNEL_GENERAL")
	}
	if channelID == "" {
		http.Error(w, "Missing channel", http.StatusInternalServerError)
		return
	}
	if discordSession == nil {
		http.Error(w, "Discord bot not connected", http.StatusServiceUnavailable)
		return
	}

	_, err := discordSession.ChannelMessageSend(channelID, msg)
	if err != nil {
		http.Error(w, "Failed to send: "+err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{"success": true, "message": "Go-live announcement sent!"})
}

// POST /api/mod/team/adjust-stats
func HandleModAdjustTeamStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

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

	team.Rating = clampRating(req.Rating)
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
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

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

	p.Rating = clampRating(req.Rating)
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
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

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
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

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
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

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
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

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
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

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
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

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
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

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
		Visible    *bool `json:"visible"`
		ModVisible *bool `json:"mod_visible"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var s LeagueSettings
	DB.First(&s)

	if req.Visible != nil {
		s.FinalsVisible = *req.Visible
	}
	if req.ModVisible != nil {
		s.FinalsModVisible = *req.ModVisible
	}
	DB.Save(&s)

	respondJSON(w, map[string]any{
		"success":     true,
		"visible":     s.FinalsVisible,
		"mod_visible": s.FinalsModVisible,
	})
}

func HandleGetFinalsVisible(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var s LeagueSettings
	if err := DB.First(&s).Error; err != nil {
		respondJSON(w, map[string]any{"visible": false, "mod_visible": false})
		return
	}

	respondJSON(w, map[string]any{"visible": s.FinalsVisible, "mod_visible": s.FinalsModVisible})
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

// POST /api/mod/finals/generate-empty — creates an empty bracket structure by size
func HandleModFinalsGenerateEmpty(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		Size int `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Validate size is power of 2 between 2 and 16
	valid := false
	for _, s := range []int{2, 4, 8, 16} {
		if req.Size == s {
			valid = true
			break
		}
	}
	if !valid {
		http.Error(w, "size must be 2, 4, 8, or 16", http.StatusBadRequest)
		return
	}

	season := strings.TrimSpace(currentSeason)
	if season == "" {
		season = "0"
	}

	// Delete existing bracket for this season
	if err := DB.Where("season = ? AND is_finals = true AND archived = false", season).Delete(&Match{}).Error; err != nil {
		http.Error(w, "failed clearing old finals bracket", http.StatusInternalServerError)
		return
	}

	bracketSize := req.Size

	// WB rounds = log2(bracketSize)
	k := 0
	for (1 << k) < bracketSize {
		k++
	}
	lbRounds := 2*k - 2

	now := time.Now()
	code := func(bracket string, round, slot int) string {
		return fmt.Sprintf("%s-Finals-%s-R%dS%d", season, bracket, round, slot)
	}
	create := func(bracket string, round, slot int) Match {
		return Match{
			Season:       season,
			MatchCode:    code(bracket, round, slot),
			TeamAID:      0,
			TeamBID:      0,
			IsFinals:     true,
			Bracket:      bracket,
			BracketRound: round,
			BracketSlot:  slot,
			Status:       "Scheduled",
			ProposedDate: &now,
		}
	}

	lbMatchCount := func(round int) int {
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

	// Create all matches
	var matches []Match

	// Winners bracket
	for r := 1; r <= k; r++ {
		mc := bracketSize / (1 << r)
		for s := 1; s <= mc; s++ {
			matches = append(matches, create("winners", r, s))
		}
	}

	// Losers bracket
	for r := 1; r <= lbRounds; r++ {
		mc := lbMatchCount(r)
		for s := 1; s <= mc; s++ {
			matches = append(matches, create("losers", r, s))
		}
	}

	// Grand Finals (GF1 + GF reset)
	matches = append(matches, create("grand_final", 1, 1))
	matches = append(matches, create("grand_final", 2, 1))

	for i := range matches {
		if err := DB.Create(&matches[i]).Error; err != nil {
			http.Error(w, "failed creating bracket match", http.StatusInternalServerError)
			return
		}
	}

	// Reload to get IDs, then wire up next_match pointers
	var created []Match
	DB.Where("season = ? AND is_finals = true AND archived = false", season).Find(&created)

	byCode := map[string]uint{}
	for _, m := range created {
		byCode[m.MatchCode] = m.ID
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

	// Wire WB
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

	// Wire LB
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

	// Wire GF
	{
		a := code("grand_final", 1, 1)
		b := code("grand_final", 2, 1)
		patch(a, nil, &b)
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Empty %d-team bracket generated", bracketSize),
	})
}

// POST /api/mod/finals/assign-slot — assign a team to a bracket match slot
func HandleModFinalsAssignSlot(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		MatchID uint   `json:"match_id"`
		Slot    string `json:"slot"`    // "team_a" or "team_b"
		TeamID  uint   `json:"team_id"` // 0 to clear
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if req.Slot != "team_a" && req.Slot != "team_b" {
		http.Error(w, "slot must be 'team_a' or 'team_b'", http.StatusBadRequest)
		return
	}

	var m Match
	if err := DB.First(&m, req.MatchID).Error; err != nil {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}
	if !m.IsFinals {
		http.Error(w, "match is not a finals match", http.StatusBadRequest)
		return
	}

	col := "team_a_id"
	if req.Slot == "team_b" {
		col = "team_b_id"
	}

	if err := DB.Model(&Match{}).Where("id = ?", req.MatchID).Update(col, req.TeamID).Error; err != nil {
		http.Error(w, "failed to assign team", http.StatusInternalServerError)
		return
	}

	// Get team name for logging
	teamName := "TBD"
	if req.TeamID != 0 {
		var t Team
		if err := DB.First(&t, req.TeamID).Error; err == nil {
			teamName = t.Name
		}
	}

	log.Printf("📊 Assigned %s to match #%d slot %s", teamName, req.MatchID, req.Slot)

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Assigned %s to %s", teamName, req.Slot),
	})
}

// POST /api/mod/finals/set-winner — set winner on a bracket match and propagate
func HandleModFinalsSetWinner(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		MatchID  uint `json:"match_id"`
		WinnerID uint `json:"winner"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	var m Match
	if err := DB.First(&m, req.MatchID).Error; err != nil {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}

	if req.WinnerID != m.TeamAID && req.WinnerID != m.TeamBID {
		http.Error(w, "winner must be one of the match teams", http.StatusBadRequest)
		return
	}

	// Determine loser
	loserID := m.TeamAID
	if req.WinnerID == m.TeamAID {
		loserID = m.TeamBID
	}

	// Set winner
	DB.Model(&Match{}).Where("id = ?", req.MatchID).Update("winner_id", req.WinnerID)

	// Propagate winner to next match
	if m.WinnerToMatchID != nil && *m.WinnerToMatchID != 0 {
		// Find which slot the winner goes to in next match
		var nextMatch Match
		if err := DB.First(&nextMatch, *m.WinnerToMatchID).Error; err == nil {
			// Place in first empty slot, or team_a if bracket_slot is even, team_b if odd
			if nextMatch.TeamAID == 0 {
				DB.Model(&Match{}).Where("id = ?", nextMatch.ID).Update("team_a_id", req.WinnerID)
			} else if nextMatch.TeamBID == 0 {
				DB.Model(&Match{}).Where("id = ?", nextMatch.ID).Update("team_b_id", req.WinnerID)
			}
		}
	}

	// Propagate loser to losers bracket
	if m.LoserToMatchID != nil && *m.LoserToMatchID != 0 && loserID != 0 {
		var lbMatch Match
		if err := DB.First(&lbMatch, *m.LoserToMatchID).Error; err == nil {
			if lbMatch.TeamAID == 0 {
				DB.Model(&Match{}).Where("id = ?", lbMatch.ID).Update("team_a_id", loserID)
			} else if lbMatch.TeamBID == 0 {
				DB.Model(&Match{}).Where("id = ?", lbMatch.ID).Update("team_b_id", loserID)
			}
		}
	}

	// Get team name for logging
	teamName := fmt.Sprintf("Team #%d", req.WinnerID)
	var t Team
	if err := DB.First(&t, req.WinnerID).Error; err == nil {
		teamName = t.Name
	}

	SendDiscordLog(fmt.Sprintf("🏆 **Finals Winner Set:** %s wins match #%d", teamName, req.MatchID))

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("%s wins!", teamName),
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

	// Match timestamp — use scheduled date if confirmed, otherwise proposed
	ts := "Unknown time"
	if match.ScheduledDate != nil {
		ts = fmt.Sprintf("<t:%d:F>", match.ScheduledDate.Unix())
	} else if match.ProposedDate != nil {
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

	// Multiple breaks: BREAKS=start:end,start:end,...
	var breaks []map[string]string
	breaksEnv := getEnv("BREAKS", "")
	if breaksEnv != "" {
		for _, pair := range strings.Split(breaksEnv, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
			if len(parts) == 2 {
				breaks = append(breaks, map[string]string{
					"start": strings.TrimSpace(parts[0]),
					"end":   strings.TrimSpace(parts[1]),
				})
			}
		}
	}

	// Multiple finals: FINALS=start:end,start:end,...
	var finals []map[string]string
	finalsEnv := getEnv("FINALS", "")
	if finalsEnv != "" {
		for _, pair := range strings.Split(finalsEnv, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
			if len(parts) == 2 {
				finals = append(finals, map[string]string{
					"start": strings.TrimSpace(parts[0]),
					"end":   strings.TrimSpace(parts[1]),
				})
			}
		}
	}

	out := map[string]any{
		"season_start": getEnv("SEASON_START", ""),
		"season_end":   getEnv("SEASON_END", ""),
		"roster_lock":  getEnv("ROSTER_LOCK", ""),
		"breaks":       breaks,
		"finals":       finals,
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
	// Load current player stats
	type P struct {
		Rating  int64
		Wins    int64
		Losses  int64
		Matches int64
	}

	var p P
	if err := DB.Raw(`
		SELECT rating, wins, losses, matches
		FROM players
		WHERE id = ?
	`, playerID).Scan(&p).Error; err != nil {
		return
	}

	// Idempotent insert/update
	DB.Exec(`
		INSERT INTO player_stats_archive
			(player_id, season, archive_rating, archive_wins, archive_losses, archive_matches)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (player_id, season)
		DO UPDATE SET
			archive_rating  = EXCLUDED.archive_rating,
			archive_wins    = EXCLUDED.archive_wins,
			archive_losses  = EXCLUDED.archive_losses,
			archive_matches = EXCLUDED.archive_matches
	`,
		playerID,
		season,
		p.Rating,
		p.Wins,
		p.Losses,
		p.Matches,
	)
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

// HandlePingUnscheduledMatches creates Discord channels for all unscheduled matches,
// pinging both teams and league mods to coordinate scheduling.
func HandlePingUnscheduledMatches(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	season := strings.TrimSpace(r.URL.Query().Get("season"))
	if season == "" {
		season = currentSeason
	}

	if discordSession == nil {
		http.Error(w, "Discord bot is not connected", http.StatusServiceUnavailable)
		return
	}

	count, err := PingUnscheduledMatches(discordSession, season)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if count == 0 {
		respondJSON(w, map[string]any{
			"success": true,
			"created": 0,
			"message": "No unscheduled matches found for the current week.",
		})
		return
	}

	LogGeneral(fmt.Sprintf("📣 Sent unscheduled match ping for %d matches (season %s)", count, season))

	respondJSON(w, map[string]any{
		"success": true,
		"created": count,
		"message": fmt.Sprintf("Pinged %d unscheduled matches for week %d.", count, GetGlobalCurrentWeek()),
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

// POST /api/mod/import-preseason-archive - Import teams from Preseason.json into team_archives
func HandleImportPreseasonArchive(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	// Read the Preseason.json file
	data, err := os.ReadFile("archives/Preseason.json")
	if err != nil {
		modJSONErr(w, 500, "Failed to read Preseason.json: "+err.Error())
		return
	}

	var archive struct {
		Teams []struct {
			ID      uint   `json:"id"`
			Name    string `json:"name"`
			Status  string `json:"status"`
			Rating  int    `json:"rating"`
			Wins    int    `json:"wins"`
			Losses  int    `json:"losses"`
			Matches int    `json:"matches"`
		} `json:"teams"`
	}

	if err := json.Unmarshal(data, &archive); err != nil {
		modJSONErr(w, 500, "Failed to parse Preseason.json: "+err.Error())
		return
	}

	count := 0
	skipped := 0
	for _, t := range archive.Teams {
		// Check if already exists in team_archives for Preseason
		var existing TeamArchive
		if err := DB.Where("team_id = ? AND season = ?", t.ID, "Preseason").First(&existing).Error; err == nil {
			skipped++
			continue // Already exists
		}

		ta := TeamArchive{
			TeamID:  t.ID,
			Name:    t.Name,
			Season:  "Preseason",
			Rating:  t.Rating,
			Wins:    t.Wins,
			Losses:  t.Losses,
			Matches: t.Matches,
		}

		if err := DB.Create(&ta).Error; err != nil {
			log.Printf("⚠️ Failed importing team %d (%s): %v", t.ID, t.Name, err)
			continue
		}
		count++
	}

	LogGeneral(fmt.Sprintf("📦 Imported %d teams from Preseason.json (skipped %d existing)", count, skipped))

	respondJSON(w, map[string]any{
		"success":  true,
		"imported": count,
		"skipped":  skipped,
		"total":    len(archive.Teams),
	})
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

// ═══════════════════════════════════════════════════════════════
// League Settings (mod-only) — GET and POST .env settings
// ═══════════════════════════════════════════════════════════════

// settingsKeys defines the env keys exposed to the settings panel (no secrets).
var settingsKeys = []string{
	"SEASON_START",
	"SEASON_END",
	"ROSTER_LOCK",
	"BREAKS",
	"FINALS",
	"WEEKLY_CHALLENGE_LIMIT",
	"ELO_WIN_POINTS",
	"ELO_LOSS_POINTS",
	"UNDERDOG_BONUS_PER_100",
	"UNDERDOG_LOSS_REDUCTION_PER_100",
	"CHALLENGE_BONUS_MULTIPLIER",
	"MAX_RATING",
	"MIN_RATING",
	"DEFAULT_PLAYER_RATING",
	"DEFAULT_TEAM_RATING",
	"PLACEMENT_MATCHES",
	"MIN_TEAM_PLAYERS",
	"MAX_TEAM_PLAYERS",
	"CURRENT_SEASON",
	"ARENA_MODE_ENABLED",
	"DISCORD_INVITE_URL",
	"FRONTEND_URL",
	"TLS_HOST",
	"DEV_MODE",
	"YOUTUBE_PLAYLIST_ID",
}

// GET /api/mod/settings — returns current settings values
func HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireDev(w, r); !ok {
		return
	}

	settings := map[string]string{}
	for _, key := range settingsKeys {
		settings[key] = os.Getenv(key)
	}

	respondJSON(w, settings)
}

// POST /api/mod/settings — updates settings in memory and persists to .env
func HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireDev(w, r); !ok {
		return
	}

	var incoming map[string]string
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate only allowed keys
	allowed := map[string]bool{}
	for _, k := range settingsKeys {
		allowed[k] = true
	}

	for key, val := range incoming {
		if !allowed[key] {
			http.Error(w, fmt.Sprintf("Key %q is not configurable", key), http.StatusBadRequest)
			return
		}
		os.Setenv(key, val)
	}

	// Persist to .env file
	if err := persistEnvFile(); err != nil {
		log.Printf("❌ Failed to persist .env: %v", err)
		http.Error(w, "Failed to save settings", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{"success": true})
}

// persistEnvFile reads the current .env, updates changed keys, and writes back.
func persistEnvFile() error {
	envPath := ".env"

	// Read existing file
	data, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	existing := map[string]bool{}

	// Update existing lines in place
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		existing[key] = true

		// Check if this key is in our settingsKeys and update its value
		for _, sk := range settingsKeys {
			if key == sk {
				lines[i] = key + "=" + os.Getenv(key)
				break
			}
		}
	}

	// Append any new keys that weren't in the file
	for _, sk := range settingsKeys {
		if !existing[sk] {
			val := os.Getenv(sk)
			if val != "" {
				lines = append(lines, sk+"="+val)
			}
		}
	}

	return os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0644)
}

// =============================================================================
// RULES SECTIONS
// =============================================================================

// GET /api/rules — public, returns all rule sections ordered
func HandleGetRules(w http.ResponseWriter, r *http.Request) {
	var sections []RuleSection
	DB.Order("sort_order ASC, id ASC").Find(&sections)
	respondJSON(w, sections)
}

// POST /api/mod/rules — save all rule sections (replace all)
func HandleSaveRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var incoming []RuleSection
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Replace all sections in a transaction
	tx := DB.Begin()
	tx.Where("1 = 1").Delete(&RuleSection{})
	for i, s := range incoming {
		s.ID = 0
		s.SortOrder = i
		tx.Create(&s)
	}
	tx.Commit()

	respondJSON(w, map[string]string{"status": "ok"})
}

// ===========================================================================
// CLIPS / HIGHLIGHT MONTAGE
// ===========================================================================

// HandleGetClips – public, returns all clips ordered by sort_order desc
func HandleGetClips(w http.ResponseWriter, r *http.Request) {
	var clips []Clip
	DB.Order("sort_order DESC, created_at DESC").Find(&clips)
	respondJSON(w, clips)
}

// HandleAddClip – mod only, add a new clip
func HandleAddClip(w http.ResponseWriter, r *http.Request) {
	actorID, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}

	var body struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		MatchID *uint  `json:"match_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if body.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	clip := Clip{
		Title:   body.Title,
		URL:     body.URL,
		MatchID: body.MatchID,
		AddedBy: actorID,
	}
	if err := DB.Create(&clip).Error; err != nil {
		http.Error(w, "Failed to save clip", http.StatusInternalServerError)
		return
	}
	respondJSON(w, clip)
}

// HandleDeleteClip – mod only, delete a clip by ID
func HandleDeleteClip(w http.ResponseWriter, r *http.Request) {
	_, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}

	var body struct {
		ID uint `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	DB.Delete(&Clip{}, body.ID)
	respondJSON(w, map[string]string{"status": "ok"})
}

// HandleReorderClips – mod only, set sort_order for clips
func HandleReorderClips(w http.ResponseWriter, r *http.Request) {
	_, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}

	var body []struct {
		ID        uint `json:"id"`
		SortOrder int  `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	for _, item := range body {
		DB.Model(&Clip{}).Where("id = ?", item.ID).Update("sort_order", item.SortOrder)
	}
	respondJSON(w, map[string]string{"status": "ok"})
}

// HandleSyncPlaylist – mod only, fetches videos from a public YouTube playlist RSS feed (no API key needed)
func HandleSyncPlaylist(w http.ResponseWriter, r *http.Request) {
	actorID, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}

	playlistID := os.Getenv("YOUTUBE_PLAYLIST_ID")

	// Allow override from request body
	var body struct {
		PlaylistID string `json:"playlist_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.PlaylistID != "" {
		playlistID = body.PlaylistID
	}

	if playlistID == "" {
		http.Error(w, "No playlist ID provided", http.StatusBadRequest)
		return
	}

	// YouTube exposes public playlist contents via Atom RSS feed (no API key required)
	feedURL := fmt.Sprintf("https://www.youtube.com/feeds/videos.xml?playlist_id=%s", playlistID)

	resp, err := http.Get(feedURL)
	if err != nil {
		http.Error(w, "Failed to reach YouTube", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		http.Error(w, fmt.Sprintf("YouTube returned status %d — is the playlist public?", resp.StatusCode), http.StatusBadGateway)
		return
	}

	// Parse Atom XML feed
	type atomEntry struct {
		VideoID string `xml:"videoId"`
		Title   string `xml:"title"`
	}
	type atomFeed struct {
		Entries []atomEntry `xml:"entry"`
	}

	var feed atomFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		http.Error(w, "Failed to parse YouTube feed", http.StatusInternalServerError)
		return
	}

	// Get existing playlist clips to avoid duplicates
	var existing []Clip
	DB.Where("source = ?", "playlist").Find(&existing)
	existingMap := map[string]bool{}
	for _, c := range existing {
		existingMap[c.VideoID] = true
	}

	added := 0
	for _, entry := range feed.Entries {
		if entry.VideoID == "" || existingMap[entry.VideoID] {
			continue
		}

		clip := Clip{
			Title:   entry.Title,
			URL:     "https://www.youtube.com/watch?v=" + entry.VideoID,
			VideoID: entry.VideoID,
			Source:  "playlist",
			AddedBy: actorID,
		}
		if err := DB.Create(&clip).Error; err == nil {
			added++
		}
	}

	respondJSON(w, map[string]any{
		"status":            "ok",
		"total_in_playlist": len(feed.Entries),
		"added":             added,
		"already_existed":   len(feed.Entries) - added,
	})
}

// --- Player Availability ---

// GetPlayerAvailability returns the logged-in player's availability slots.
func GetPlayerAvailability(w http.ResponseWriter, r *http.Request) {
	session, ok := requireLogin(w, r)
	if !ok {
		return
	}
	discordIDStr := session.Values["discord_id"].(string)
	discordID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	var slots []PlayerAvailability
	DB.Where("player_id = ?", discordID).Order("date, start_time").Find(&slots)
	respondJSON(w, slots)
}

// SavePlayerAvailability replaces the logged-in player's availability slots.
func SavePlayerAvailability(w http.ResponseWriter, r *http.Request) {
	session, ok := requireLogin(w, r)
	if !ok {
		return
	}
	discordIDStr := session.Values["discord_id"].(string)
	discordID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	var slots []PlayerAvailability
	if err := json.NewDecoder(r.Body).Decode(&slots); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate each slot
	for i, s := range slots {
		if s.Date == "" {
			http.Error(w, fmt.Sprintf("Missing date at index %d", i), http.StatusBadRequest)
			return
		}
		if s.StartTime == "" || s.EndTime == "" {
			http.Error(w, fmt.Sprintf("Missing start_time or end_time at index %d", i), http.StatusBadRequest)
			return
		}
	}

	// Replace all slots for this player in a transaction
	tx := DB.Begin()
	if err := tx.Where("player_id = ?", discordID).Delete(&PlayerAvailability{}).Error; err != nil {
		tx.Rollback()
		http.Error(w, "Failed to clear existing availability", http.StatusInternalServerError)
		return
	}
	for i := range slots {
		slots[i].ID = 0 // ensure new rows
		slots[i].PlayerID = discordID
	}
	if len(slots) > 0 {
		if err := tx.Create(&slots).Error; err != nil {
			tx.Rollback()
			http.Error(w, "Failed to save availability", http.StatusInternalServerError)
			return
		}
	}
	tx.Commit()

	respondJSON(w, map[string]string{"status": "ok"})
}

// GetTeamAvailability returns aggregated availability for a team.
// Finds time slots where at least 3 players on the team are available on the same date.
func GetTeamAvailability(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	teamID, err := strconv.Atoi(params["id"])
	if err != nil || teamID <= 0 {
		http.Error(w, "invalid team id", http.StatusBadRequest)
		return
	}

	var members []TeamMember
	DB.Where("team_id = ?", teamID).Find(&members)
	if len(members) == 0 {
		respondJSON(w, map[string]any{"dates": []any{}})
		return
	}

	memberIDs := make([]int64, len(members))
	for i, m := range members {
		memberIDs[i] = m.PlayerID
	}

	playerMap := map[int64]string{}
	for _, m := range members {
		var p Player
		if DB.First(&p, m.PlayerID).Error == nil {
			name := p.DisplayName
			if name == "" {
				name = p.Username
			}
			playerMap[m.PlayerID] = name
		}
	}

	var slots []PlayerAvailability
	DB.Where("player_id IN ?", memberIDs).Order("date, start_time").Find(&slots)
	if len(slots) == 0 {
		respondJSON(w, map[string]any{"dates": []any{}})
		return
	}

	const minMinute = 6 * 60
	const maxMinute = 26 * 60
	const slotSize = 30
	totalSlots := (maxMinute - minMinute) / slotSize

	// Group by date -> player -> ranges (raw)
	type rawRange struct {
		Start string
		End   string
	}
	byDate := map[string]map[int64][]rawRange{}
	for _, s := range slots {
		if byDate[s.Date] == nil {
			byDate[s.Date] = map[int64][]rawRange{}
		}
		byDate[s.Date][s.PlayerID] = append(byDate[s.Date][s.PlayerID], rawRange{s.StartTime, s.EndTime})
	}

	type Overlap struct {
		Start   string   `json:"start_time"`
		End     string   `json:"end_time"`
		Players []string `json:"players"`
	}
	type DateEntry struct {
		Date     string    `json:"date"`
		Players  []string  `json:"players"`
		Overlaps []Overlap `json:"overlaps,omitempty"`
	}

	var result []DateEntry

	for date, playerRanges := range byDate {
		// Build per-slot presence for overlap detection
		slotPlayers := make([]map[int64]bool, totalSlots)
		for i := range slotPlayers {
			slotPlayers[i] = map[int64]bool{}
		}

		for pid, ranges := range playerRanges {
			for _, r := range ranges {
				sh, sm := 0, 0
				fmt.Sscanf(r.Start, "%d:%d", &sh, &sm)
				eh, em := 0, 0
				fmt.Sscanf(r.End, "%d:%d", &eh, &em)
				startMin := sh*60 + sm
				endMin := eh*60 + em
				if startMin < minMinute {
					startMin = minMinute
				}
				if endMin > maxMinute {
					endMin = maxMinute
				}
				if startMin >= endMin {
					continue
				}
				startSlot := (startMin - minMinute) / slotSize
				endSlot := (endMin - minMinute) / slotSize
				for i := startSlot; i < endSlot && i < totalSlots; i++ {
					slotPlayers[i][pid] = true
				}
			}
		}

		// All players who have any data on this date
		allPlayers := []string{}
		for pid := range playerRanges {
			if name, ok := playerMap[pid]; ok {
				allPlayers = append(allPlayers, name)
			}
		}
		sort.Strings(allPlayers)

		// Find 3+ overlap ranges
		var overlaps []Overlap
		inRange := false
		rangeStart := 0
		rangePlayers := map[int64]bool{}

		for i := 0; i <= totalSlots; i++ {
			count := 0
			players := map[int64]bool{}
			if i < totalSlots {
				players = slotPlayers[i]
				count = len(players)
			}
			if count >= 3 {
				if !inRange {
					inRange = true
					rangeStart = i
					rangePlayers = map[int64]bool{}
					for pid := range players {
						rangePlayers[pid] = true
					}
				} else {
					for pid := range rangePlayers {
						if !players[pid] {
							delete(rangePlayers, pid)
						}
					}
				}
			} else {
				if inRange {
					sm := minMinute + rangeStart*slotSize
					em := minMinute + i*slotSize
					pNames := []string{}
					for pid := range rangePlayers {
						if name, ok := playerMap[pid]; ok {
							pNames = append(pNames, name)
						}
					}
					sort.Strings(pNames)
					overlaps = append(overlaps, Overlap{
						Start:   fmt.Sprintf("%02d:%02d", (sm/60)%24, sm%60),
						End:     fmt.Sprintf("%02d:%02d", (em/60)%24, em%60),
						Players: pNames,
					})
					inRange = false
				}
			}
		}

		result = append(result, DateEntry{
			Date:     date,
			Players:  allPlayers,
			Overlaps: overlaps,
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })

	respondJSON(w, map[string]any{"dates": result})
}

// computeTeamAvailRanges returns aggregated availability (3+ players) for a team as date->ranges map.
// computeTeamAvailRanges returns aggregated availability (3+ players) for a team as date->ranges map.
func computeTeamAvailRanges(teamID int) map[string][]struct {
	Start     string
	End       string
	PlayerCnt int
} {
	var memberIDs []int64
	DB.Table("team_members").Where("team_id = ?", teamID).Pluck("player_id", &memberIDs)
	if len(memberIDs) < 3 {
		return nil
	}

	var slots []PlayerAvailability
	DB.Where("player_id IN ?", memberIDs).Order("date, start_time").Find(&slots)
	if len(slots) == 0 {
		return nil
	}

	type ds struct {
		date  string
		start string
		end   string
	}
	byDate := map[string][]ds{}
	for _, s := range slots {
		byDate[s.Date] = append(byDate[s.Date], ds{s.Date, s.StartTime, s.EndTime})
	}

	const minMinute = 6 * 60
	const maxMinute = 26 * 60
	const slotSize = 30
	totalSlots := (maxMinute - minMinute) / slotSize

	result := map[string][]struct {
		Start     string
		End       string
		PlayerCnt int
	}{}

	for date, dateSlots := range byDate {
		cov := make([]int, totalSlots)
		for _, d := range dateSlots {
			sp := strings.Split(d.start, ":")
			ep := strings.Split(d.end, ":")
			if len(sp) != 2 || len(ep) != 2 {
				continue
			}
			sh, _ := strconv.Atoi(sp[0])
			sm, _ := strconv.Atoi(sp[1])
			eh, _ := strconv.Atoi(ep[0])
			em, _ := strconv.Atoi(ep[1])
			sm2 := sh*60 + sm
			em2 := eh*60 + em
			if sm2 < minMinute {
				sm2 = minMinute
			}
			if em2 > maxMinute {
				em2 = maxMinute
			}
			if sm2 >= em2 {
				continue
			}
			ss := (sm2 - minMinute) / slotSize
			es := (em2 - minMinute) / slotSize
			for i := ss; i < es && i < totalSlots; i++ {
				cov[i]++
			}
		}

		var ranges []struct {
			Start     string
			End       string
			PlayerCnt int
		}
		inR := false
		rs := 0
		mc := 0
		for i := 0; i <= totalSlots; i++ {
			c := 0
			if i < totalSlots {
				c = cov[i]
			}
			if c >= 3 {
				if !inR {
					inR = true
					rs = i
					mc = c
				} else if c > mc {
					mc = c
				}
			} else {
				if inR {
					smi := minMinute + rs*slotSize
					emi := minMinute + i*slotSize
					ranges = append(ranges, struct {
						Start     string
						End       string
						PlayerCnt int
					}{
						Start:     fmt.Sprintf("%02d:%02d", (smi/60)%24, smi%60),
						End:       fmt.Sprintf("%02d:%02d", (emi/60)%24, emi%60),
						PlayerCnt: mc,
					})
					inR = false
					mc = 0
				}
			}
		}
		if len(ranges) > 0 {
			result[date] = ranges
		}
	}
	return result
}

// GetMatchAvailability returns overlapping availability between both teams in a match.
func GetMatchAvailability(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	matchID, err := strconv.Atoi(params["id"])
	if err != nil || matchID <= 0 {
		http.Error(w, "invalid match id", http.StatusBadRequest)
		return
	}

	var match Match
	if err := DB.First(&match, matchID).Error; err != nil {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}

	teamA := computeTeamAvailRanges(int(match.TeamAID))
	teamB := computeTeamAvailRanges(int(match.TeamBID))

	type OverlapRange struct {
		Start        string `json:"start_time"`
		End          string `json:"end_time"`
		TeamAPlayers int    `json:"team_a_players"`
		TeamBPlayers int    `json:"team_b_players"`
	}
	type DateOverlap struct {
		Date   string         `json:"date"`
		Ranges []OverlapRange `json:"ranges"`
	}

	if teamA == nil || teamB == nil {
		respondJSON(w, map[string]any{
			"dates":           []any{},
			"team_a_has_data": teamA != nil,
			"team_b_has_data": teamB != nil,
			"message":         "Both teams need at least 3 players with availability set.",
		})
		return
	}

	allDates := map[string]bool{}
	for d := range teamA {
		allDates[d] = true
	}
	for d := range teamB {
		allDates[d] = true
	}

	var dates []string
	for d := range allDates {
		if teamA[d] != nil && teamB[d] != nil {
			dates = append(dates, d)
		}
	}
	sort.Strings(dates)

	var result []DateOverlap
	for _, date := range dates {
		var overlaps []OverlapRange
		for _, ra := range teamA[date] {
			for _, rb := range teamB[date] {
				if ra.Start < rb.End && rb.Start < ra.End {
					os := ra.Start
					if rb.Start > os {
						os = rb.Start
					}
					oe := ra.End
					if rb.End < oe {
						oe = rb.End
					}
					overlaps = append(overlaps, OverlapRange{
						Start: os, End: oe,
						TeamAPlayers: ra.PlayerCnt,
						TeamBPlayers: rb.PlayerCnt,
					})
				}
			}
		}
		if len(overlaps) > 1 {
			sort.Slice(overlaps, func(i, j int) bool { return overlaps[i].Start < overlaps[j].Start })
			merged := []OverlapRange{overlaps[0]}
			for i := 1; i < len(overlaps); i++ {
				last := &merged[len(merged)-1]
				if overlaps[i].Start <= last.End {
					if overlaps[i].End > last.End {
						last.End = overlaps[i].End
					}
				} else {
					merged = append(merged, overlaps[i])
				}
			}
			overlaps = merged
		}
		if len(overlaps) > 0 {
			result = append(result, DateOverlap{Date: date, Ranges: overlaps})
		}
	}

	respondJSON(w, map[string]any{
		"dates":           result,
		"team_a_has_data": len(teamA) > 0,
		"team_b_has_data": len(teamB) > 0,
	})
}
