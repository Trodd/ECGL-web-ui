package main

import (
	"strings"
	"time"
)

// Per-league registration role.
//
// `players.role` is still the canonical/"primary" value (Discord role sync and
// older screens use it), but each league now has its own role so a player can be
// a Player in Varsity and a League Sub in Rookie (or vice versa).
type PlayerLeagueRole struct {
	PlayerID     int64     `gorm:"primaryKey" json:"player_id"`
	League       string    `gorm:"primaryKey" json:"league"`
	Role         string    `json:"role"`
	PlayOverride bool      `json:"play_override"`
	ForceVeteran bool      `json:"force_veteran"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (PlayerLeagueRole) TableName() string { return "player_league_roles" }

// playerRoleInLeague returns the player's registration role for a league.
// Falls back to the global players.role when no per-league row exists.
func playerRoleInLeague(playerID int64, league string) string {
	lg := normalizeLeague(league)

	var role string
	DB.Table("player_league_roles").
		Select("role").
		Where("player_id = ? AND league = ?", playerID, lg).
		Limit(1).
		Scan(&role)

	if strings.TrimSpace(role) != "" {
		return role
	}

	var p Player
	if err := DB.Select("role").First(&p, playerID).Error; err == nil {
		return p.Role
	}
	return ""
}

// setPlayerRoleInLeague stores the role for one league (upsert).
func setPlayerRoleInLeague(playerID int64, league, role string) error {
	lg := normalizeLeague(league)
	role = strings.TrimSpace(role)

	return DB.Exec(`
		INSERT INTO player_league_roles (player_id, league, role, updated_at)
		VALUES (?, ?, ?, NOW())
		ON CONFLICT (player_id, league) DO UPDATE SET role = EXCLUDED.role, updated_at = NOW()
	`, playerID, lg, role).Error
}

// playerIsLeagueSubIn reports whether the player is a League Sub in a league.
func playerIsLeagueSubIn(playerID int64, league string) bool {
	return strings.EqualFold(playerRoleInLeague(playerID, league), "League Sub")
}

// playerIsBannedIn reports whether the player is banned in a league.
func playerIsBannedIn(playerID int64, league string) bool {
	return strings.EqualFold(playerRoleInLeague(playerID, league), "Banned")
}

// playerIsRegisteredIn reports whether the player finished registration for a league.
func playerIsRegisteredIn(playerID int64, league string) bool {
	return strings.TrimSpace(playerRoleInLeague(playerID, league)) != ""
}

// playerPlayOverride reports whether a league mod allowed this player to play as a
// normal roster player despite a composition rule (e.g. the Rookie League
// veteran rule, which normally forces veterans into the Coach role).
func playerPlayOverride(playerID int64, league string) bool {
	lg := normalizeLeague(league)

	var v bool
	DB.Table("player_league_roles").
		Select("COALESCE(play_override, FALSE)").
		Where("player_id = ? AND league = ?", playerID, lg).
		Limit(1).
		Scan(&v)

	return v
}

// setPlayerPlayOverride stores the per-league play override (upsert).
func setPlayerPlayOverride(playerID int64, league string, allow bool) error {
	lg := normalizeLeague(league)

	if err := DB.Exec(`
		INSERT INTO player_league_roles (player_id, league, play_override, updated_at)
		VALUES (?, ?, ?, NOW())
		ON CONFLICT (player_id, league) DO UPDATE
		   SET play_override = EXCLUDED.play_override, updated_at = NOW()
	`, playerID, lg, allow).Error; err != nil {
		return err
	}

	// Keep players.role usable: forcing a banned/unregistered player into the
	// roster doesn't change their role, so nothing else to do here.
	return nil
}

// playerForcedVeteran reports whether a league mod marked this player as a
// veteran even though they never played a main-league match.
func playerForcedVeteran(playerID int64, league string) bool {
	lg := normalizeLeague(league)

	var v bool
	DB.Table("player_league_roles").
		Select("COALESCE(force_veteran, FALSE)").
		Where("player_id = ? AND league = ?", playerID, lg).
		Limit(1).
		Scan(&v)

	return v
}

// setPlayerForceVeteran stores the per-league "treat as veteran" flag (upsert).
func setPlayerForceVeteran(playerID int64, league string, veteran bool) error {
	lg := normalizeLeague(league)

	return DB.Exec(`
		INSERT INTO player_league_roles (player_id, league, force_veteran, updated_at)
		VALUES (?, ?, ?, NOW())
		ON CONFLICT (player_id, league) DO UPDATE
		   SET force_veteran = EXCLUDED.force_veteran, updated_at = NOW()
	`, playerID, lg, veteran).Error
}
