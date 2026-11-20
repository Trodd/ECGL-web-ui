package main

// Player registration shared by the website (/api/register in main.go) and
// the Discord draft panel's in-Discord registration (draft_register.go).

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm/clause"
)

// Option lists for Discord's quick registration (draft_register.go). These
// mirror the website: frontend/src/profileOptions.js (DEVICE_OPTIONS /
// TIMEZONE_GROUPS) and the hard-coded lists in pages/Register.jsx —
// keep them in sync when adding options there.
type regOption struct{ Value, Label string }

var regDeviceOptions = []regOption{
	{"rift", "PCVR (SteamVR / Oculus PC)"},
	{"quest_link", "Quest + Link/AirLink"},
	{"quest_native", "Quest Native (standalone)"},
}

var regTimezoneGroups = []struct {
	Label   string
	Options []regOption
}{
	{"North America", []regOption{
		{"US/Eastern", "US/Eastern (EST/EDT)"},
		{"US/Central", "US/Central (CST/CDT)"},
		{"US/Mountain", "US/Mountain (MST/MDT)"},
		{"US/Arizona", "US/Arizona (no DST)"},
		{"US/Pacific", "US/Pacific (PST/PDT)"},
		{"US/Alaska", "US/Alaska"},
		{"US/Hawaii", "US/Hawaii"},
		{"Canada/Atlantic", "Canada/Atlantic"},
		{"Canada/Newfoundland", "Canada/Newfoundland"},
	}},
	{"Europe", []regOption{
		{"Europe/London", "Europe/London (UK)"},
		{"Europe/Dublin", "Europe/Dublin (Ireland)"},
		{"Europe/Lisbon", "Europe/Lisbon (Portugal)"},
		{"Europe/Paris", "Europe/Paris (France)"},
		{"Europe/Berlin", "Europe/Berlin (Germany)"},
		{"Europe/Rome", "Europe/Rome (Italy)"},
		{"Europe/Madrid", "Europe/Madrid (Spain)"},
		{"Europe/Amsterdam", "Europe/Amsterdam (Netherlands)"},
		{"Europe/Brussels", "Europe/Brussels (Belgium)"},
		{"Europe/Vienna", "Europe/Vienna (Austria)"},
		{"Europe/Oslo", "Europe/Oslo (Norway)"},
		{"Europe/Stockholm", "Europe/Stockholm (Sweden)"},
		{"Europe/Copenhagen", "Europe/Copenhagen (Denmark)"},
		{"Europe/Helsinki", "Europe/Helsinki (Finland)"},
		{"Europe/Athens", "Europe/Athens (Greece)"},
		{"Europe/Sofia", "Europe/Sofia (Bulgaria)"},
		{"Europe/Warsaw", "Europe/Warsaw (Poland)"},
		{"Europe/Budapest", "Europe/Budapest (Hungary)"},
		{"Europe/Prague", "Europe/Prague (Czechia)"},
		{"Europe/Bucharest", "Europe/Bucharest (Romania)"},
		{"Europe/Moscow", "Europe/Moscow (Russia)"},
	}},
}

// registerPlayer creates/updates a player's registration for a league: the
// global players row, the per-league role, Discord roles and the signup log.
// Callers must already have confirmed the user is in the Discord server.
func registerPlayer(discordID int64, username, displayName, role, device, timezone, league string) (Player, *userError) {
	discordIDStr := strconv.FormatInt(discordID, 10)

	// Banned players can't re-register — the upsert below would overwrite the
	// global "Banned" role and quietly lift the ban.
	if playerIsBannedIn(discordID, league) {
		return Player{}, userErr(http.StatusForbidden, "Your account is banned.")
	}
	var current Player
	if DB.Select("role").First(&current, discordID).Error == nil && strings.EqualFold(current.Role, "Banned") {
		return Player{}, userErr(http.StatusForbidden, "Your account is banned.")
	}

	if device == "" {
		return Player{}, userErr(http.StatusBadRequest, "Please select a device")
	}
	switch strings.ToLower(role) {
	case "player":
		role = "Player"
	case "league sub":
		role = "League Sub"
	default:
		return Player{}, userErr(http.StatusBadRequest, "Invalid role")
	}

	player := Player{
		ID:          discordID,
		Username:    username,
		DisplayName: displayName,
		Role:        role,
		Device:      device,
		Timezone:    timezone,
	}

	// ✅ Default rating
	if player.Rating == 0 {
		player.Rating = getEnvInt("DEFAULT_PLAYER_RATING", 800)
	}

	// 🛡️ Detect if this is a real signup vs a duplicate submit.
	// If the player already exists with the same role/device/timezone,
	// treat it as a no-op so we don't double-ping Discord or re-apply roles.
	var existing Player
	alreadyRegistered := false
	if err := DB.First(&existing, discordID).Error; err == nil {
		if strings.EqualFold(existing.Role, role) &&
			strings.EqualFold(existing.Device, device) &&
			strings.EqualFold(existing.Timezone, timezone) &&
			strings.TrimSpace(existing.Role) != "" {
			alreadyRegistered = true
		}
	}

	// ✅ Upsert player row
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"username", "role", "device", "timezone"}),
	}).Create(&player).Error; err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok {
			log.Printf("❌ Postgres error: Code=%s Detail=%s Message=%s", pgErr.Code, pgErr.Detail, pgErr.Message)
		}
		return Player{}, userErr(http.StatusInternalServerError, "Database error")
	}

	DB.First(&player, discordID)

	// Store the role for the league being registered for (players.role stays as
	// the primary global value used by Discord role sync).
	_ = setPlayerRoleInLeague(discordID, league, player.Role)

	// Re-sync Discord roles so the Rookie League player/sub role follows the
	// league they just registered for.
	go syncDiscordRolesForPlayer(discordID)

	// A coach whose registration role changed loses their coaching slot.
	if !alreadyRegistered && existing.ID != 0 {
		enforceCoachRoleChange(discordID, existing.Role, player.Role)
	}

	if !alreadyRegistered {
		// ⭐ Discord role handling — only on first signup or role change
		rolePlayer := os.Getenv("DISCORD_PLAYER_ROLE_ID")
		roleSub := os.Getenv("DISCORD_LEAGUE_SUB_ROLE_ID")

		// Cleanup previous roles
		go DiscordRemoveRole(discordIDStr, rolePlayer)
		go DiscordRemoveRole(discordIDStr, roleSub)

		// Assign correct role
		if role == "Player" {
			go DiscordAddRole(discordIDStr, rolePlayer)
		}
		if role == "League Sub" {
			go DiscordAddRole(discordIDStr, roleSub)
		}

		// Log registration (async so a Discord interaction isn't held up)
		go SendDiscordLog(
			fmt.Sprintf("🟢 **<@%s>** has signed up as a **%s** in timezone **%s**",
				discordIDStr, player.Role, player.Timezone,
			),
		)
	}

	player.Registered = true
	return player, nil
}
