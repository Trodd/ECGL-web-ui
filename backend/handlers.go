package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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

type FlexibleID int64

func (f *FlexibleID) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*f = FlexibleID(i)
		return nil
	}

	// Fallback: try number
	var i int64
	if err := json.Unmarshal(data, &i); err == nil {
		*f = FlexibleID(i)
		return nil
	}

	return errors.New("invalid FlexibleID")
}

func (f FlexibleID) Int64() int64 {
	return int64(f)
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
		"id":      team.ID,
		"name":    team.Name,
		"status":  team.Status,
		"roster":  roster,
		"matches": matches,
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

	// 🧩 DEV MODE OVERRIDE — allows ?as=<discord_id> for debugging
	devMode := os.Getenv("DEV_MODE") == "true"
	if devMode {
		if overrideID := r.URL.Query().Get("as"); overrideID != "" {
			log.Printf("🧪 [DEV_MODE] Impersonating player %s", overrideID)
			discordID = overrideID
		}
	}

	// 🚫 Still block if no Discord ID and not in dev override
	if discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	var player Player
	if err := DB.First(&player, playerID).Error; err != nil {
		http.Error(w, "player not found", http.StatusNotFound)
		return
	}

	var membership TeamMember
	result := DB.Where("player_id = ?", playerID).Order("team_id DESC").Limit(1).Find(&membership)
	if result.RowsAffected == 0 {
		respondJSON(w, map[string]any{
			"team":     nil,
			"roster":   []any{},
			"matches":  []any{},
			"requests": []any{},
			"myRole":   "",
		})
		return
	} else if result.Error != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	var team Team
	if err := DB.First(&team, membership.TeamID).Error; err != nil {
		http.Error(w, "team not found", http.StatusNotFound)
		return
	}

	// --- roster ---
	type RosterPlayer struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Rating      int    `json:"rating"`
	}
	var roster []RosterPlayer
	var rosterRows *sql.Rows
	var err error
	rosterRows, err = DB.Table("team_members").
		Select("players.id, players.username, players.display_name, team_members.role, players.rating").
		Joins("JOIN players ON players.id = team_members.player_id").
		Where("team_members.team_id = ?", membership.TeamID).
		Rows()
	if err == nil {
		defer rosterRows.Close()
		for rosterRows.Next() {
			var id int64
			var username, displayName, role string
			var rating int
			if err := rosterRows.Scan(&id, &username, &displayName, &role, &rating); err == nil {
				roster = append(roster, RosterPlayer{
					ID:          strconv.FormatInt(id, 10),
					Username:    username,
					DisplayName: displayName,
					Role:        role,
					Rating:      rating,
				})
			}
		}
	}

	// --- matches (with map scores + confirmation flags) ---
	type MatchWithMaps struct {
		ID                  uint         `json:"id"`
		MatchCode           string       `json:"match_code"`
		Opponent            string       `json:"opponent"`
		Date                *time.Time   `json:"date"`
		Result              string       `json:"result"`
		Maps                []MatchScore `json:"maps"`
		TeamAID             uint         `json:"team_a_id"`
		TeamBID             uint         `json:"team_b_id"`
		TeamAScoreConfirmed bool         `json:"team_a_score_confirmed"`
		TeamBScoreConfirmed bool         `json:"team_b_score_confirmed"`
		Status              string       `json:"status"`
	}

	var matches []MatchWithMaps
	matchRows, err := DB.Raw(`
		SELECT
			m.id,
			m.match_code,
			CASE WHEN m.team_a_id = @id THEN t2.name ELSE t1.name END AS opponent,
			m.scheduled_date AS date,
			CASE
				WHEN m.winner_id = @id THEN 'Win'
				WHEN m.loser_id = @id THEN 'Loss'
				ELSE 'Pending'
			END AS result,
			m.team_a_id,
			m.team_b_id,
			m.team_a_score_confirmed,
			m.team_b_score_confirmed,
			m.status
		FROM matches m
		JOIN teams t1 ON m.team_a_id = t1.id
		JOIN teams t2 ON m.team_b_id = t2.id
		WHERE (m.team_a_id = @id OR m.team_b_id = @id)
		ORDER BY 
			CASE 
				WHEN m.status IN ('Completed','Finished','Forfeit','Cancelled') THEN 1 
				ELSE 0 
			END,
			m.scheduled_date DESC NULLS LAST`,
		sql.Named("id", membership.TeamID),
	).Rows()

	if err == nil {
		defer matchRows.Close()
		for matchRows.Next() {
			var m MatchWithMaps
			if err := matchRows.Scan(
				&m.ID, &m.MatchCode, &m.Opponent, &m.Date, &m.Result,
				&m.TeamAID, &m.TeamBID,
				&m.TeamAScoreConfirmed, &m.TeamBScoreConfirmed,
				&m.Status,
			); err != nil {
				log.Printf("❌ Failed to scan match row: %v", err)
				continue
			}
			var maps []MatchScore
			DB.Where("match_id = ?", m.ID).Find(&maps)

			// Normalize and attach numeric fields safely
			for i := range maps {
				if maps[i].Gamemode == "" {
					maps[i].Gamemode = "Capture Point"
				}
			}
			m.Maps = maps
			matches = append(matches, m)
		}
	}

	// --- pending join requests ---
	type JoinRequestResponse struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Status   string `json:"status"`
	}
	var requests []JoinRequestResponse
	if membership.Role == "Captain" || membership.Role == "Co-Captain" {
		reqRows, _ := DB.Table("team_join_requests").
			Select("team_join_requests.id, players.username, team_join_requests.status").
			Joins("JOIN players ON players.id = team_join_requests.player_id").
			Where("team_join_requests.team_id = ? AND team_join_requests.status = 'pending'", membership.TeamID).
			Rows()
		defer reqRows.Close()
		for reqRows.Next() {
			var id uint
			var username, status string
			if err := reqRows.Scan(&id, &username, &status); err == nil {
				requests = append(requests, JoinRequestResponse{
					ID:       strconv.FormatUint(uint64(id), 10),
					Username: username,
					Status:   status,
				})
			}
		}
	}

	respondJSON(w, map[string]any{
		"team": map[string]any{
			"id":           team.ID,
			"name":         team.Name,
			"status":       team.Status,
			"join_allowed": team.JoinAllowed,
		},
		"roster":   roster,
		"matches":  matches,
		"requests": requests,
		"myRole":   membership.Role,
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
	if rosterLocked {
		http.Error(w, "Roster is currently locked. Joining teams is disabled.", http.StatusForbidden)
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

	// ✅ Player comes from session, not frontend
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	// verify membership
	var member TeamMember
	if err := DB.Where("team_id = ? AND player_id = ?", req.TeamID, playerID).
		First(&member).Error; err != nil {
		http.Error(w, "Not a team member", http.StatusNotFound)
		return
	}

	// Save role before removing
	role := member.Role
	if err := DB.Delete(&member).Error; err != nil {
		http.Error(w, "Failed to leave team", http.StatusInternalServerError)
		return
	}

	// Check remaining members
	var remaining []TeamMember
	DB.Where("team_id = ?", req.TeamID).Find(&remaining)

	if len(remaining) == 0 {
		DB.Model(&Team{}).
			Where("id = ?", req.TeamID).
			Update("status", "Disbanded")
		log.Printf("🏴‍☠️ Team %d marked as Disbanded (last member left)", req.TeamID)
	} else if role == "Captain" {
		var next TeamMember
		if err := DB.Where("team_id = ? AND role = ?", req.TeamID, "Co-Captain").
			First(&next).Error; err == nil {
			DB.Model(&next).Update("role", "Captain")
			log.Printf("👑 Co-Captain %d promoted to Captain (team %d)", next.PlayerID, req.TeamID)
		} else if err := DB.Where("team_id = ?", req.TeamID).First(&next).Error; err == nil {
			DB.Model(&next).Update("role", "Captain")
			log.Printf("👑 Member %d promoted to Captain (team %d)", next.PlayerID, req.TeamID)
		}
	}

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
	ORDER BY m.created_at DESC
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

		// ✅ Normalize missing season/week safely
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

	// --- Filter by season/week query if provided ---
	var filtered []PublicMatch
	for _, m := range normalized {
		if (season == "" || strings.EqualFold(m.Season, season)) &&
			(week == "" || m.Week == week) {
			filtered = append(filtered, m)
		}
	}

	// --- Group by season + week ---
	grouped := map[string]map[string][]PublicMatch{}
	for _, m := range filtered {
		// ✅ Force empty/null seasons into "Preseason" here too (safety net)
		if m.Season == "" || strings.EqualFold(m.Season, "null") {
			m.Season = "Preseason"
		}
		if _, ok := grouped[m.Season]; !ok {
			grouped[m.Season] = map[string][]PublicMatch{}
		}
		grouped[m.Season][m.Week] = append(grouped[m.Season][m.Week], m)
	}

	// --- Return clean response ---
	respondJSON(w, map[string]any{
		"success": true,
		"matches": grouped,
	})
}

func getMemberRole(teamID uint, playerID int64) (string, error) {
	var member TeamMember
	if err := DB.Where("team_id = ? AND player_id = ?", teamID, playerID).
		First(&member).Error; err != nil {
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

	// target ID
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
		respondJSON(w, map[string]any{"success": true, "message": "Member kicked, team disbanded"})
		return
	}

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

	// target
	playerID := int64(req.PlayerID) // ✅ always valid int64 now

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

	respondJSON(w, map[string]any{
		"success":      true,
		"join_allowed": req.Allow,
		"message": fmt.Sprintf("Join requests %s",
			map[bool]string{true: "enabled", false: "disabled"}[req.Allow]),
	})
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

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	date, err := time.Parse(time.RFC3339, req.Date)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	// ✅ Allow update if it was already scheduled
	oldDate := ""
	if match.ScheduledDate != nil {
		oldDate = match.ScheduledDate.Format(time.RFC1123)
	}

	match.ScheduledDate = &date
	match.Status = "Scheduled"
	match.TeamAScheduleConfirmed = true
	match.TeamBScheduleConfirmed = true

	if err := DB.Save(&match).Error; err != nil {
		http.Error(w, "Failed to update match", http.StatusInternalServerError)
		return
	}

	if oldDate == "" {
		log.Printf("📅 Match #%d scheduled by Team %d for %s", match.ID, req.TeamID, date.Format(time.RFC1123))
	} else {
		log.Printf("✏️ Match #%d rescheduled by Team %d: %s → %s", match.ID, req.TeamID, oldDate, date.Format(time.RFC1123))
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Match scheduled for %s", date.Format(time.RFC1123)),
	})
}

// / --- POST /api/match/submit-score ---
// One team enters or edits scores. Resets confirmations until both re-confirm.
func HandleSubmitScore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID    uint   `json:"match_id"`
		TeamID     uint   `json:"team_id"`
		LeagueSubA *int64 `json:"league_sub_a"`
		LeagueSubB *int64 `json:"league_sub_b"`
		Maps       []struct {
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

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// ✅ Ensure submitting team is actually in this match
	isTeamA := req.TeamID == match.TeamAID
	isTeamB := req.TeamID == match.TeamBID
	if !isTeamA && !isTeamB {
		http.Error(w, "You are not part of this match", http.StatusForbidden)
		return
	}

	// 🚫 Prevent same sub for both teams
	if req.LeagueSubA != nil && req.LeagueSubB != nil && *req.LeagueSubA == *req.LeagueSubB {
		http.Error(w, "The same League Sub cannot be used for both teams.", http.StatusBadRequest)
		return
	}

	// 🧍 Store League Subs (if provided)
	match.LeagueSubA = req.LeagueSubA
	match.LeagueSubB = req.LeagueSubB

	// --- Get existing map scores to detect changes ---
	var existing []MatchScore
	DB.Where("match_id = ?", req.MatchID).Find(&existing)

	// --- Build new canonical score data ---
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

		our, their := m.TeamAScore, m.TeamBScore
		aScore, bScore := 0, 0

		if isTeamA {
			aScore, bScore = our, their
		} else {
			aScore, bScore = their, our
		}

		newScores = append(newScores, MatchScore{
			MatchID:    req.MatchID,
			MapNumber:  mapNum,
			Gamemode:   mode,
			TeamAScore: aScore,
			TeamBScore: bScore,
		})
	}

	// --- Compare newScores vs existing ---
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

	// --- Only update if something actually changed ---
	if changed {
		if err := DB.Where("match_id = ?", req.MatchID).Delete(&MatchScore{}).Error; err != nil {
			http.Error(w, "Failed to clear previous scores", http.StatusInternalServerError)
			return
		}

		for _, s := range newScores {
			if err := DB.Create(&s).Error; err != nil {
				log.Printf("❌ Failed to insert score for map %d (match %d): %v", s.MapNumber, req.MatchID, err)
			}
		}

		match.TeamAScoreConfirmed = false
		match.TeamBScoreConfirmed = false
		match.Status = "Pending Confirmation"

		if err := DB.Save(&match).Error; err != nil {
			log.Printf("❌ Failed to update match after score change: %v", err)
		}

		log.Printf("📝 Team %d submitted NEW scores for match #%d (normalized vs TeamA/TeamB)", req.TeamID, match.ID)
	} else {
		if err := DB.Save(&match).Error; err != nil {
			log.Printf("❌ Failed to re-save match without score changes: %v", err)
		}
		log.Printf("🔁 Team %d re-submitted SAME scores for match #%d — confirmations preserved", req.TeamID, match.ID)
	}

	respondJSON(w, map[string]any{
		"success": true,
		"changed": changed,
		"message": "Scores saved. Both teams must confirm to finalize.",
	})
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

	// --- Load rosters (player_history → fallback team_members) ---
	type RosterPlayer struct {
		PlayerID    int64  `json:"player_id"`
		DisplayName string `json:"display_name"`
		Username    string `json:"username"`
		Role        string `json:"role"`
	}

	var rosterA, rosterB []RosterPlayer

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

	// --- Final Response ---
	respondJSON(w, map[string]any{
		"match":      match,
		"teams":      map[string]any{"a": teamA, "b": teamB},
		"map_scores": filtered,
		"roster":     map[string]any{"a": rosterA, "b": rosterB},
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

func getUint(body any) (uint, bool) {
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
}

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
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
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

	// ✅ Verify winner belongs to the match
	if !(req.WinnerTeamID == m.TeamAID || req.WinnerTeamID == m.TeamBID) {
		modJSONErr(w, http.StatusBadRequest, "winner_team_id not in this match")
		return
	}

	var loser uint
	if req.WinnerTeamID == m.TeamAID {
		loser = m.TeamBID
	} else {
		loser = m.TeamAID
	}

	m.WinnerID = &req.WinnerTeamID
	m.LoserID = &loser
	m.Status = "Completed"

	// 🧹 Clear any lingering map rows
	_ = DB.Where("match_id = ?", m.ID).Delete(&MatchScore{}).Error

	if err := DB.Save(&m).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to set forfeit")
		return
	}

	// 🧩 Snapshot both teams' rosters for historical accuracy
	snapshotTeamRoster(m.TeamAID, currentSeason)
	snapshotTeamRoster(m.TeamBID, currentSeason)

	log.Printf("🏁 Mod marked match #%d as forfeited (Winner: %d, Loser: %d)", m.ID, req.WinnerTeamID, loser)

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
	if _, ok := requireLeagueMod(w, r); !ok {
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

	// mark as banned
	if p.Role != "Banned" {
		p.Role = "Banned"
		if err := DB.Save(&p).Error; err != nil {
			modJSONErr(w, http.StatusInternalServerError, "failed to ban")
			return
		}
	}
	// remove from any team
	_ = DB.Where("player_id = ?", req.PlayerID.Int64()).Delete(&TeamMember{}).Error

	respondJSON(w, map[string]any{"success": true, "message": "player banned", "player_id": p.ID})
}

// POST /api/mod/player/unban
// body: { "player_id": "<discord id or number>" }
func ModPlayerUnban(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
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
	respondJSON(w, map[string]any{"success": true, "message": "player unbanned", "player_id": p.ID})
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

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}

	// Update confirmation
	if match.TeamAID == req.TeamID {
		match.TeamAScheduleConfirmed = true
	} else if match.TeamBID == req.TeamID {
		match.TeamBScheduleConfirmed = true
	} else {
		http.Error(w, "team not part of match", http.StatusForbidden)
		return
	}

	// Both confirmed → mark scheduled
	if match.TeamAScheduleConfirmed && match.TeamBScheduleConfirmed {
		match.Status = "Scheduled"
		now := time.Now()
		match.ScheduleConfirmedAt = &now
	}

	if err := DB.Save(&match).Error; err != nil {
		http.Error(w, "failed to update", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success":  true,
		"match_id": match.ID,
		"status":   match.Status,
	})
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

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// ✅ Mark this team's confirmation
	if req.TeamID == match.TeamAID {
		match.TeamAScoreConfirmed = true
	} else if req.TeamID == match.TeamBID {
		match.TeamBScoreConfirmed = true
	} else {
		http.Error(w, "Team not part of match", http.StatusForbidden)
		return
	}

	// --- Both confirmed? check if scores already match ---
	if match.TeamAScoreConfirmed && match.TeamBScoreConfirmed {
		var maps []MatchScore
		DB.Where("match_id = ?", match.ID).Find(&maps)

		if len(maps) == 0 {
			log.Printf("⚠️ No map scores found for match #%d during confirm", match.ID)
		}

		totalA, totalB := 0, 0
		for _, s := range maps {
			if s.TeamAScore > s.TeamBScore {
				totalA++
			} else if s.TeamBScore > s.TeamAScore {
				totalB++
			}
		}

		// Decide winner/loser
		if totalA != totalB {
			var winnerID, loserID uint
			if totalA > totalB {
				winnerID, loserID = match.TeamAID, match.TeamBID
			} else {
				winnerID, loserID = match.TeamBID, match.TeamAID
			}

			match.WinnerID = &winnerID
			match.LoserID = &loserID
			match.Status = "Completed"

			if err := DB.Save(&match).Error; err != nil {
				http.Error(w, "Failed to finalize match", http.StatusInternalServerError)
				return
			}

			updateLeaderboards(winnerID, loserID)
			log.Printf("🏁 Match #%d completed (Winner: %d, Loser: %d)", match.ID, winnerID, loserID)

			// 🧩 Snapshot both teams' rosters for historical accuracy
			snapshotTeamRoster(match.TeamAID, currentSeason)
			snapshotTeamRoster(match.TeamBID, currentSeason)

		} else {
			match.Status = "Completed"
			DB.Save(&match)
			log.Printf("🤝 Match #%d completed (tie)", match.ID)

			// 🧩 Still snapshot for record keeping (tie matches count too)
			snapshotTeamRoster(match.TeamAID, currentSeason)
			snapshotTeamRoster(match.TeamBID, currentSeason)
		}

		respondJSON(w, map[string]any{
			"success": true,
			"status":  "Completed",
			"message": "Match finalized.",
		})
		return
	}

	// ✅ Only one side confirmed → just update flag
	if err := DB.Save(&match).Error; err != nil {
		http.Error(w, "Failed to update confirmation", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"status":  "Pending Confirmation",
		"message": "Waiting for opponent confirmation.",
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
