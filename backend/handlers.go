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
		ID       int64
		Username string
		Role     string
		Device   string
		Timezone string
	}

	var rows []raw
	if err := DB.Table("players").
		Select("id, username, role, device, timezone").
		Where("username <> ''").
		Scan(&rows).Error; err != nil {

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
			"id":       strconv.FormatInt(r.ID, 10),
			"username": r.Username,
			"role":     r.Role,
			"device":   r.Device,
			"timezone": r.Timezone,
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
	if err := DB.Where("status = ?", "Active").Find(&teams).Error; err != nil {
		http.Error(w, "Failed to load teams", http.StatusInternalServerError)
		return
	}
	respondJSON(w, teams)
}

func GetTeam(w http.ResponseWriter, r *http.Request) {
	// crash prevention: guard param parse
	params := mux.Vars(r)
	teamID, err := strconv.Atoi(params["id"])
	if err != nil || teamID <= 0 {
		http.Error(w, "invalid team id", http.StatusBadRequest)
		return
	}

	// crash prevention: find team safely
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

	// --- Load roster (include display_name) ---
	type RosterPlayer struct {
		ID          uint   `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Rating      int    `json:"rating"`
	}
	var roster []RosterPlayer
	if err := DB.Table("team_members").
		Select("players.id, players.username, players.display_name, team_members.role, players.rating").
		Joins("JOIN players ON players.id = team_members.player_id").
		Where("team_members.team_id = ?", teamID).
		Scan(&roster).Error; err != nil {
		log.Printf("❌ GetTeam: roster query failed for team %d: %v", teamID, err)
		roster = []RosterPlayer{} // crash prevention: return empty array
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

	// crash prevention: never return nil arrays
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
	// player from session
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
		http.Error(w, "player not found", http.StatusNotFound)
		return
	}

	// find membership safely
	var membership TeamMember
	result := DB.Where("player_id = ?", playerID).
		Order("team_id DESC").
		Limit(1).
		Find(&membership)

	if result.RowsAffected == 0 {
		// no team
		respondJSON(w, map[string]any{
			"team":     nil,
			"roster":   []any{},
			"matches":  []any{},
			"requests": []any{},
			"myRole":   "",
		})
		return
	} else if result.Error != nil {
		log.Printf("❌ DB error in GetMyTeam for player %d: %v", playerID, result.Error)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// load team
	var team Team
	if err := DB.First(&team, membership.TeamID).Error; err != nil {
		http.Error(w, "team not found", http.StatusNotFound)
		return
	}

	// --- load roster (IDs as strings) ---
	type RosterPlayer struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Rating      int    `json:"rating"`
	}
	var roster []RosterPlayer
	rows, err := DB.Table("team_members").
		Select("players.id, players.username, players.display_name, team_members.role, players.rating").
		Joins("JOIN players ON players.id = team_members.player_id").
		Where("team_members.team_id = ?", membership.TeamID).
		Rows()
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			var username, displayName, role string
			var rating int
			if err := rows.Scan(&id, &username, &displayName, &role, &rating); err == nil {
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

	// --- load matches (include ID + MatchCode) ---
	var matches []struct {
		ID        uint   `json:"id"`
		MatchCode string `json:"match_code"`
		Opponent  string `json:"opponent"`
		Date      string `json:"date"`
		Result    string `json:"result"`
	}
	DB.Raw(`
		SELECT
			m.id,
			m.match_code,
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
		ORDER BY m.scheduled_date DESC NULLS LAST`,
		sql.Named("id", membership.TeamID),
	).Scan(&matches)

	// --- load requests (IDs as strings) ---
	type JoinRequestResponse struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Status   string `json:"status"`
	}
	var requests []JoinRequestResponse
	if membership.Role == "Captain" || membership.Role == "Co-Captain" {
		rows, _ := DB.Table("team_join_requests").
			Select("team_join_requests.id, players.username, team_join_requests.status").
			Joins("JOIN players ON players.id = team_join_requests.player_id").
			Where("team_join_requests.team_id = ? AND team_join_requests.status = 'pending'", membership.TeamID).
			Rows()
		defer rows.Close()
		for rows.Next() {
			var id uint
			var username, status string
			if err := rows.Scan(&id, &username, &status); err == nil {
				requests = append(requests, JoinRequestResponse{
					ID:       strconv.FormatUint(uint64(id), 10),
					Username: username,
					Status:   status,
				})
			}
		}
	}

	// ✅ respond
	respondJSON(w, map[string]any{
		"team": map[string]any{
			"id":     team.ID,
			"name":   team.Name,
			"status": team.Status,
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

	// ensure team exists
	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// already in a team?
	var membership TeamMember
	if err := DB.Where("player_id = ?", playerID).Take(&membership).Error; err == nil {
		http.Error(w, "Player already belongs to a team", http.StatusBadRequest)
		return
	}

	// duplicate request?
	var existingReq TeamJoinRequest
	if err := DB.Where("player_id = ? AND team_id = ? AND status = ?", playerID, req.TeamID, "pending").
		First(&existingReq).Error; err == nil {
		http.Error(w, "Join request already pending", http.StatusBadRequest)
		return
	}

	// create join request
	join := TeamJoinRequest{
		PlayerID: playerID,
		TeamID:   req.TeamID,
		Status:   "pending",
	}
	if err := DB.Create(&join).Error; err != nil {
		http.Error(w, "Failed to save join request", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{"success": true, "message": "Join request submitted"})
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

	// create team
	team := Team{Name: req.Name, Status: "Active"}
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
		"message": "Team created successfully",
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
		log.Printf("✅ Player %d added to team %d as Member", jr.PlayerID, jr.TeamID)

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
		TeamA         string     `json:"team_a"`
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
			t1.name AS team_a,
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
		TeamA         string     `json:"team_a"`
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
			TeamA:         m.TeamA,
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

// --- Schedule a match (Captain/Co-Captain only) ---
func HandleScheduleMatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID  uint   `json:"team_id"`
		MatchID uint   `json:"match_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// ✅ Get player from session
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	// ✅ Ensure requester is Captain or Co-Captain
	role, err := getMemberRole(req.TeamID, playerID)
	if err != nil || (role != "Captain" && role != "Co-Captain") {
		http.Error(w, "Only Captains or Co-Captains can schedule matches", http.StatusForbidden)
		return
	}

	// ✅ Ensure match exists
	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	// ✅ Parse date safely
	var parsed *time.Time
	if req.Date != "" {
		t, err := time.Parse(time.RFC3339, req.Date)
		if err == nil {
			parsed = &t
		}
	}

	match.ScheduledDate = parsed
	match.Status = "Scheduled"
	match.UpdatedAt = time.Now()

	if err := DB.Save(&match).Error; err != nil {
		http.Error(w, "Failed to update match schedule", http.StatusInternalServerError)
		return
	}

	log.Printf("📅 Match %d scheduled for %v by player %d", match.ID, parsed, playerID)
	respondJSON(w, map[string]any{
		"success": true,
		"message": "Match scheduled successfully",
		"match":   match,
	})
}

// --- Submit match scores (Captain/Co-Captain only) ---
func HandleSubmitScore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID  uint `json:"team_id"`
		MatchID uint `json:"match_id"`
		Maps    []struct {
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

	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	role, err := getMemberRole(req.TeamID, playerID)
	if err != nil || (role != "Captain" && role != "Co-Captain") {
		http.Error(w, "Only Captains or Co-Captains can submit scores", http.StatusForbidden)
		return
	}

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}
	if match.ScheduledDate == nil {
		http.Error(w, "Cannot submit scores until match is scheduled", http.StatusBadRequest)
		return
	}

	// ✅ Enforce gamemode limit (max 2 per type)
	gamemodeCount := map[string]int{}
	for _, m := range req.Maps {
		gamemodeCount[m.Gamemode]++
		if gamemodeCount[m.Gamemode] > 2 {
			http.Error(w, "Gamemode can only be used twice per match", http.StatusBadRequest)
			return
		}
	}

	// ✅ Clear old scores
	DB.Where("match_id = ?", req.MatchID).Delete(&MatchScore{})

	// ✅ Insert new map scores
	for _, m := range req.Maps {
		score := MatchScore{
			MatchID:    req.MatchID,
			MapNumber:  m.MapNumber,
			Gamemode:   m.Gamemode,
			TeamAScore: m.TeamAScore,
			TeamBScore: m.TeamBScore,
		}
		DB.Create(&score)
	}

	// ✅ Compute winner
	var scores []MatchScore
	DB.Where("match_id = ?", req.MatchID).Find(&scores)

	winsA, winsB := 0, 0
	for _, s := range scores {
		if s.TeamAScore > s.TeamBScore {
			winsA++
		} else if s.TeamBScore > s.TeamAScore {
			winsB++
		}
	}

	if winsA > winsB {
		match.WinnerID = &match.TeamAID
		match.LoserID = &match.TeamBID
	} else if winsB > winsA {
		match.WinnerID = &match.TeamBID
		match.LoserID = &match.TeamAID
	}
	match.Status = "Completed"
	DB.Save(&match)

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Scores submitted successfully",
	})
}

// --- Get match with map scores ---
func HandleGetMatch(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	matchID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid match ID", http.StatusBadRequest)
		return
	}

	// fetch match
	var match Match
	if err := DB.First(&match, matchID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Match not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// fetch map scores
	var maps []MatchScore
	if err := DB.Where("match_id = ?", match.ID).Find(&maps).Error; err != nil {
		http.Error(w, "Failed to load map scores", http.StatusInternalServerError)
		return
	}

	// fetch team names
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	// --- Fetch historical rosters from player_history ---
	type HistoryPlayer struct {
		PlayerID    int64  `json:"player_id"`
		DisplayName string `json:"display_name"`
		Username    string `json:"username"`
		Role        string `json:"role"`
	}
	var rosterA, rosterB []HistoryPlayer

	DB.Raw(`
		SELECT p.id AS player_id, p.display_name, p.username, ph.role
		FROM player_history ph
		JOIN players p ON p.id = ph.player_id
		WHERE ph.team_id = ? AND ph.season = COALESCE(?, 'Preseason')
	`, match.TeamAID, "Preseason").Scan(&rosterA)

	DB.Raw(`
		SELECT p.id AS player_id, p.display_name, p.username, ph.role
		FROM player_history ph
		JOIN players p ON p.id = ph.player_id
		WHERE ph.team_id = ? AND ph.season = COALESCE(?, 'Preseason')
	`, match.TeamBID, "Preseason").Scan(&rosterB)

	// ✅ Return clean JSON, even if arrays are empty
	respondJSON(w, map[string]any{
		"match":  match,
		"teams":  map[string]any{"a": teamA, "b": teamB},
		"maps":   maps,
		"roster": map[string]any{"a": rosterA, "b": rosterB},
	})
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

	log.Printf("👤 Discord member: %s (%s)", member.User.Username, member.User.ID)
	log.Printf("🎭 Roles returned: %+v", member.Roles)
	log.Printf("🔎 Expecting League Mod role ID: %s", modRoleID)

	for _, role := range member.Roles {
		if role == modRoleID {
			log.Printf("✅ League Mod verified for %s", discordID)
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
	// verify winner belongs to the match
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

	// optional: wipe any lingering map rows
	_ = DB.Where("match_id = ?", m.ID).Delete(&MatchScore{}).Error

	if err := DB.Save(&m).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to set forfeit")
		return
	}

	respondJSON(w, map[string]any{"success": true, "message": "match forfeited", "match_id": m.ID, "winner": req.WinnerTeamID})
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

	_ = DB.Where("match_id = ?", m.ID).Delete(&MatchScore{}).Error
	m.WinnerID = nil
	m.LoserID = nil
	m.Status = "Completed" // Completed (double forfeit)
	if err := DB.Save(&m).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to set double forfeit")
		return
	}
	respondJSON(w, map[string]any{"success": true, "message": "double forfeit applied", "match_id": m.ID})
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

	// clear old rows + reinsert
	DB.Where("match_id = ?", m.ID).Delete(&MatchScore{})
	for _, mapData := range req.Maps {
		DB.Create(&MatchScore{
			MatchID:    m.ID,
			MapNumber:  mapData.MapNumber,
			Gamemode:   mapData.Gamemode,
			TeamAScore: mapData.TeamAScore,
			TeamBScore: mapData.TeamBScore,
		})
	}

	m.Status = "Completed"
	DB.Save(&m)
	log.Printf("✏️ Mod edited scores for match %d", m.ID)
	respondJSON(w, map[string]any{"success": true, "message": "scores updated"})
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
