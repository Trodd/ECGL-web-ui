package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

// Rookie League coaching.
//
// Every Rookie League coach is a roster member who never plays. Veterans are
// coaches AUTOMATICALLY — they don't have to register — but they can unregister
// (opt out), and a league mod can override a specific veteran so they play as a
// normal rookie instead.

// rookieCoachState returns the stored coach flags for a player.
func rookieCoachState(playerID int64) (explicit, optedOut bool) {
	var row struct {
		IsRookieCoach     bool
		RookieCoachOptOut bool
	}
	DB.Table("player_settings").
		Select("is_rookie_coach, COALESCE(rookie_coach_opt_out, FALSE) AS rookie_coach_opt_out").
		Where("player_id = ?", playerID).
		Limit(1).
		Scan(&row)
	return row.IsRookieCoach, row.RookieCoachOptOut
}

// isExplicitRookieCoach reports whether the player actively registered as a coach.
func isExplicitRookieCoach(playerID int64) bool {
	explicit, _ := rookieCoachState(playerID)
	return explicit
}

// rookieCoachOptedOut reports whether the player unregistered from coaching.
func rookieCoachOptedOut(playerID int64) bool {
	_, optedOut := rookieCoachState(playerID)
	return optedOut
}

// isRegisteredRookieCoach reports whether the player counts as a Rookie League
// coach right now. Veterans are coaches automatically, unless they opted out or
// a league mod granted them the play override.
func isRegisteredRookieCoach(playerID int64) bool {
	// A mod override wins: they play as a normal rookie.
	if playerPlayOverride(playerID, LeagueRookie) {
		return false
	}

	explicit, optedOut := rookieCoachState(playerID)
	if explicit {
		return true
	}
	if optedOut {
		return false
	}

	// Veterans are coaches automatically.
	if getEnv("ROOKIE_VETERAN_RULE_ENABLED", "true") != "true" {
		return false
	}
	return isVeteranInLeague(LeagueRookie, playerID)
}

// teamHasCoach reports whether a team already has a coach on its roster.
func teamHasCoach(teamID uint) bool {
	var count int64
	DB.Table("team_members").
		Where("team_id = ? AND LOWER(TRIM(COALESCE(role, ''))) = 'coach'", teamID).
		Count(&count)
	return count > 0
}

// coachRoleBlocked returns a message when a coach can't be added to this team.
func coachRoleBlocked(teamID uint) (bool, string) {
	if teamHasCoach(teamID) {
		return true, "That team already has a coach — only one coach is allowed per team."
	}
	return false, ""
}

// setRookieCoachFlag writes the coach registration flag for a player.
func setRookieCoachFlag(playerID int64, value bool) error {
	// Make sure the settings row exists so the UPDATE has something to hit.
	playerSettingsFor(playerID)

	return DB.Exec(
		`UPDATE player_settings SET is_rookie_coach = ?, updated_at = NOW() WHERE player_id = ?`,
		value, playerID,
	).Error
}

// GET /api/coach/status
func HandleGetCoachStatus(w http.ResponseWriter, r *http.Request) {
	playerID, ok := draftPlayerID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	lg := leagueFromRequest(r)
	explicit, optedOut := rookieCoachState(playerID)
	isCoach := isRegisteredRookieCoach(playerID)

	resp := map[string]any{
		"league":                lg,
		"available":             lg == LeagueRookie,
		"is_coach":              isCoach,
		"auto_coach":            isCoach && !explicit,
		"opted_out":             optedOut,
		"is_veteran":            isMainLeagueVeteran(playerID),
		"play_override":         playerPlayOverride(playerID, LeagueRookie),
		"has_team":              playerOnTeamInLeague(playerID, lg),
		"coach_draft_enabled":   coachDraftEnabled(lg),
		"team_creation_enabled": leagueAllowsTeamCreation(lg),
	}

	// Teams in this league that don't have a coach yet — handy for the UI.
	if lg == LeagueRookie {
		type coachTeam struct {
			ID      uint   `json:"id"`
			Name    string `json:"name"`
			Players int    `json:"players"`
		}
		var teams []coachTeam
		DB.Raw(`
			SELECT t.id, t.name,
			       (SELECT COUNT(*) FROM team_members m
			         WHERE m.team_id = t.id AND LOWER(TRIM(COALESCE(m.role,''))) <> 'coach') AS players
			FROM teams t
			WHERE t.league = ?
			  AND COALESCE(t.status, '') <> 'Test'
			  AND NOT EXISTS (
			        SELECT 1 FROM team_members c
			        WHERE c.team_id = t.id AND LOWER(c.role) = 'coach'
			  )
			ORDER BY players ASC, t.id ASC
		`, lg).Scan(&teams)
		resp["teams_without_coach"] = teams
	}

	respondJSON(w, resp)
}

// POST /api/coach/register — registers the logged-in player as a rookie coach.
func HandleCoachRegister(w http.ResponseWriter, r *http.Request) {
	playerID, ok := draftPlayerID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	lg := leagueFromRequest(r)
	if lg != LeagueRookie {
		http.Error(w, "Coach registration is only available for the Rookie League", http.StatusBadRequest)
		return
	}

	var p Player
	if err := DB.First(&p, playerID).Error; err != nil {
		http.Error(w, "You must register before signing up as a coach", http.StatusBadRequest)
		return
	}
	if !playerIsRegisteredIn(playerID, lg) {
		http.Error(w, "You must register for this league before signing up as a coach", http.StatusBadRequest)
		return
	}
	if playerIsBannedIn(playerID, lg) {
		http.Error(w, "Your account is banned.", http.StatusForbidden)
		return
	}

	if err := setRookieCoachFlag(playerID, true); err != nil {
		log.Printf("❌ HandleCoachRegister(%d): %v", playerID, err)
		http.Error(w, "Failed to register as a coach: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Registering clears any earlier opt-out.
	DB.Exec(`UPDATE player_settings SET rookie_coach_opt_out = FALSE, updated_at = NOW() WHERE player_id = ?`, playerID)

	LogGeneral("🧑‍🏫 **"+draftPlayerLabel(p)+"** registered as a **Rookie League coach**", lg)

	respondJSON(w, map[string]any{
		"success":  true,
		"is_coach": true,
		"message":  "You're registered as a Rookie League coach. Join a team that needs one.",
		"teams_without_coach": func() any {
			return teamIDsWithoutCoach(lg)
		}(),
	})
}

// POST /api/coach/unregister — removes the coach registration.
func HandleCoachUnregister(w http.ResponseWriter, r *http.Request) {
	playerID, ok := draftPlayerID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	lg := leagueFromRequest(r)
	if lg != LeagueRookie {
		http.Error(w, "Coach registration is only available for the Rookie League", http.StatusBadRequest)
		return
	}

	ps := playerSettingsFor(playerID)
	if err := setRookieCoachFlag(playerID, false); err != nil {
		log.Printf("❌ HandleCoachUnregister(%d): %v", playerID, err)
		http.Error(w, "Failed to remove coach registration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = ps

	// Veterans are coaches automatically, so clearing the flag isn't enough —
	// mark an explicit opt-out so we don't put them straight back in.
	DB.Exec(`UPDATE player_settings SET rookie_coach_opt_out = TRUE, updated_at = NOW() WHERE player_id = ?`, playerID)

	// If they're currently sitting on a team as a coach, take them off it so the
	// team frees the slot up. Their coach-only membership was the reason they
	// were there.
	if role := currentRookieRole(playerID, lg); strings.EqualFold(role, "coach") {
		DB.Where("player_id = ? AND LOWER(TRIM(COALESCE(role,''))) = 'coach'", playerID).
			Delete(&TeamMember{})
	}

	respondJSON(w, map[string]any{"success": true, "is_coach": false, "opted_out": true,
		"message": "You're no longer registered as a Rookie League coach."})
}

// currentRookieRole returns the player's team role in a league ("" if none).
func currentRookieRole(playerID int64, league string) string {
	var role string
	DB.Raw(`
		SELECT tm.role
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.player_id = ? AND t.league = ?
		LIMIT 1
	`, playerID, normalizeLeague(league)).Scan(&role)
	return strings.TrimSpace(role)
}

// enforceCoachRoleChange removes a coach from their rookie team when their
// registration role changes (Player ⇄ League Sub) while they hold a Coach slot.
// Controlled by the ROOKIE_COACH_ROLE_CHANGE_REMOVES setting.
func enforceCoachRoleChange(playerID int64, oldRole, newRole string) {
	if strings.EqualFold(strings.TrimSpace(oldRole), strings.TrimSpace(newRole)) {
		return
	}
	if getEnv("ROOKIE_COACH_ROLE_CHANGE_REMOVES", "true") != "true" {
		return
	}

	// Any rookie team where this player is the coach.
	type membership struct {
		TeamID   uint
		TeamName string
	}
	var teams []membership
	DB.Raw(`
		SELECT t.id AS team_id, t.name AS team_name
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.player_id = ?
		  AND t.league = ?
		  AND LOWER(TRIM(COALESCE(tm.role, ''))) = 'coach'
	`, playerID, LeagueRookie).Scan(&teams)

	if len(teams) == 0 {
		return
	}

	for _, t := range teams {
		if err := DB.Where("team_id = ? AND player_id = ? AND LOWER(TRIM(COALESCE(role,''))) = 'coach'",
			t.TeamID, playerID).Delete(&TeamMember{}).Error; err != nil {
			log.Printf("⚠️ enforceCoachRoleChange: remove %d from team %d: %v", playerID, t.TeamID, err)
			continue
		}

		log.Printf("🚪 Coach %d removed from team %d (%s) — role changed %s → %s",
			playerID, t.TeamID, t.TeamName, oldRole, newRole)

		createNotification(playerID, "coach_removed",
			"Removed from team",
			"You changed your role, so you were removed as coach of "+t.TeamName+".",
			"/myteam",
		)

		DB.Create(&PlayerHistory{
			PlayerID:   playerID,
			TeamID:     t.TeamID,
			TeamName:   t.TeamName,
			Role:       "Coach",
			Season:     seasonForLeague(LeagueRookie),
			IsTeamJoin: false,
		})

		LogGeneral(fmt.Sprintf(
			"🚪 **Coach removed** — role changed to **%s**, so they left **%s**", newRole, t.TeamName,
		), LeagueRookie)
	}
}

// teamIDsWithoutCoach is a light version of the list for the register response.
func teamIDsWithoutCoach(league string) []map[string]any {
	type row struct {
		ID   uint
		Name string
	}
	var rows []row
	DB.Raw(`
		SELECT t.id, t.name
		FROM teams t
		WHERE t.league = ?
		  AND COALESCE(t.status, '') <> 'Test'
		  AND NOT EXISTS (
		        SELECT 1 FROM team_members c
		        WHERE c.team_id = t.id AND LOWER(c.role) = 'coach'
		  )
		ORDER BY t.name ASC
	`, normalizeLeague(league)).Scan(&rows)

	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{"id": r.ID, "name": r.Name})
	}
	return out
}
