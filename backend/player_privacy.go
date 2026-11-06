package main

import (
	"net/http"
	"strconv"
	"strings"
)

// hiddenAvailabilityPlayers returns the subset of playerIDs that opted out of
// sharing their availability with people outside their own team.
func hiddenAvailabilityPlayers(playerIDs []int64) map[int64]bool {
	out := map[int64]bool{}
	if len(playerIDs) == 0 {
		return out
	}

	var rows []int64
	DB.Model(&PlayerSettings{}).
		Where("player_id IN ? AND COALESCE(hide_availability, FALSE) = TRUE", playerIDs).
		Pluck("player_id", &rows)

	for _, id := range rows {
		out[id] = true
	}
	return out
}

// requesterSeesTeam reports whether the current session user may see private
// availability for a team: they're on the roster, or they're staff.
func requesterSeesTeam(r *http.Request, teamID int) bool {
	if r == nil {
		return false
	}

	session, _ := store.Get(r, "session")
	discordIDStr, _ := session.Values["discord_id"].(string)
	discordIDStr = strings.TrimSpace(discordIDStr)
	if discordIDStr == "" {
		return false
	}

	playerID, err := strconv.ParseInt(discordIDStr, 10, 64)
	if err == nil && playerID > 0 {
		var count int64
		DB.Model(&TeamMember{}).
			Where("team_id = ? AND player_id = ?", teamID, playerID).
			Count(&count)
		if count > 0 {
			return true
		}
	}

	return sessionIsStaff(r)
}
