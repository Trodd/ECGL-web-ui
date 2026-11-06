package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PlayerSettings holds per-player preferences that aren't part of the roster
// record itself: UI defaults, notification mutes, availability defaults and
// privacy switches. Profile fields (role / device / timezone / display name)
// stay on the Player row.
type PlayerSettings struct {
	PlayerID int64 `gorm:"primaryKey" json:"player_id"`

	// UI preferences (also readable by the frontend on boot).
	DefaultLeague string `json:"default_league" gorm:"default:''"`
	LandingPage   string `json:"landing_page" gorm:"default:''"`
	CompactCards  bool   `json:"compact_cards" gorm:"default:false"`
	ReduceMotion  bool   `json:"reduce_motion" gorm:"default:false"`
	NotifySound   bool   `json:"notify_sound" gorm:"default:true"`

	// Availability editor defaults.
	DefaultStartTime string `json:"default_start_time" gorm:"default:'18:00'"`
	DefaultEndTime   string `json:"default_end_time" gorm:"default:'22:00'"`

	// Sub / privacy switches.
	AvailableAsSub   bool `json:"available_as_sub" gorm:"default:false"`
	HideAvailability bool `json:"hide_availability" gorm:"default:false"`

	// MutedNotificationTypes is a comma separated list of notification `type`s
	// the player doesn't want to receive.
	MutedNotificationTypes string `json:"muted_notification_types" gorm:"default:''"`

	// OverlayToken is a personal token for stream overlays.
	OverlayToken string    `json:"overlay_token"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (PlayerSettings) TableName() string { return "player_settings" }

// Profile fields are chosen from fixed lists (see frontend/profileOptions.js)
// rather than free text, so the API validates them too.
var (
	allowedProfileRoles   = []string{"Player", "League Sub"}
	allowedProfileDevices = []string{"rift", "quest_link", "quest_native"}
	timezonePattern       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*/[A-Za-z0-9_+-]+$`)
)

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// notificationTypes are the notification categories players can mute.
var notificationTypes = []string{
	"join_request",
	"matchup_posted",
	"schedule_proposed",
	"schedule_confirmed",
	"score_submitted",
	"challenge_received",
	"cast_assigned",
	"draft",
}

// playerSettingsFor returns (creating if needed) the settings row for a player.
func playerSettingsFor(playerID int64) PlayerSettings {
	var ps PlayerSettings
	if err := DB.First(&ps, "player_id = ?", playerID).Error; err == nil {
		if strings.TrimSpace(ps.DefaultStartTime) == "" {
			ps.DefaultStartTime = "18:00"
		}
		if strings.TrimSpace(ps.DefaultEndTime) == "" {
			ps.DefaultEndTime = "22:00"
		}
		return ps
	}

	ps = PlayerSettings{
		PlayerID:         playerID,
		DefaultStartTime: "18:00",
		DefaultEndTime:   "22:00",
		NotifySound:      true,
	}
	if err := DB.Create(&ps).Error; err != nil {
		// Not fatal — fall back to in-memory defaults.
		return ps
	}
	return ps
}

// notificationMuted reports whether a player muted a notification type.
func notificationMuted(playerID int64, ntype string) bool {
	ntype = strings.ToLower(strings.TrimSpace(ntype))
	if ntype == "" {
		return false
	}
	ps := playerSettingsFor(playerID)
	for _, t := range strings.Split(ps.MutedNotificationTypes, ",") {
		if strings.ToLower(strings.TrimSpace(t)) == ntype {
			return true
		}
	}
	return false
}

// isHHMM validates a "HH:MM" clock string.
func isHHMM(v string) bool {
	parts := strings.Split(strings.TrimSpace(v), ":")
	if len(parts) != 2 {
		return false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return h >= 0 && h <= 26 && m >= 0 && m < 60
}

func newOverlayToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// GET /api/player/settings
//
// Everything the Player Settings page needs: editable profile fields, current
// teams, cooldown state and the stored preferences.
func GetPlayerSettings(w http.ResponseWriter, r *http.Request) {
	session, ok := requireLogin(w, r)
	if !ok {
		return
	}
	discordIDStr, _ := session.Values["discord_id"].(string)
	playerID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	var p Player
	if err := DB.First(&p, playerID).Error; err != nil {
		http.Error(w, "Player not found", http.StatusNotFound)
		return
	}

	ps := playerSettingsFor(playerID)

	type teamRow struct {
		League   string `json:"league"`
		TeamID   uint   `json:"team_id"`
		TeamName string `json:"team_name"`
		Role     string `json:"role"`
	}
	var teams []teamRow
	DB.Raw(`
		SELECT t.league, t.id AS team_id, t.name AS team_name, tm.role
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.player_id = ?
		ORDER BY t.league ASC, t.name ASC
	`, playerID).Scan(&teams)

	respondJSON(w, map[string]any{
		"profile": map[string]any{
			"id":           strconv.FormatInt(p.ID, 10),
			"username":     p.Username,
			"display_name": p.DisplayName,
			"role":         p.Role,
			"device":       p.Device,
			"timezone":     p.Timezone,
			"name_locked":  p.NameLocked,
			"avatar":       p.Avatar,
		},
		"prefs":              ps,
		"teams":              teams,
		"notification_types": notificationTypes,
		"cooldown": map[string]any{
			"last_left_team_at": p.LastLeftTeamAt,
			"on_cooldown":       p.LastLeftTeamAt != nil,
		},
	})
}

// POST /api/player/settings
//
// Updates profile fields (role / device / timezone / display name) and the
// preference block in one call.
func SavePlayerSettings(w http.ResponseWriter, r *http.Request) {
	session, ok := requireLogin(w, r)
	if !ok {
		return
	}
	discordIDStr, _ := session.Values["discord_id"].(string)
	playerID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	var p Player
	if err := DB.First(&p, playerID).Error; err != nil {
		http.Error(w, "Player not found", http.StatusNotFound)
		return
	}

	var req struct {
		// Profile
		Role        *string `json:"role"`
		Device      *string `json:"device"`
		Timezone    *string `json:"timezone"`
		DisplayName *string `json:"display_name"`
		NameLocked  *bool   `json:"name_locked"`
		// Preferences
		DefaultLeague          *string `json:"default_league"`
		LandingPage            *string `json:"landing_page"`
		CompactCards           *bool   `json:"compact_cards"`
		ReduceMotion           *bool   `json:"reduce_motion"`
		NotifySound            *bool   `json:"notify_sound"`
		DefaultStartTime       *string `json:"default_start_time"`
		DefaultEndTime         *string `json:"default_end_time"`
		AvailableAsSub         *bool   `json:"available_as_sub"`
		HideAvailability       *bool   `json:"hide_availability"`
		MutedNotificationTypes *string `json:"muted_notification_types"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	trim := func(s *string, max int) string {
		v := strings.TrimSpace(*s)
		if len(v) > max {
			v = v[:max]
		}
		return v
	}

	profile := map[string]any{}
	if req.Role != nil {
		role := trim(req.Role, 40)
		if role != "" && !containsString(allowedProfileRoles, role) {
			http.Error(w, "Unknown role", http.StatusBadRequest)
			return
		}
		profile["role"] = role
	}
	if req.Device != nil {
		device := trim(req.Device, 60)
		if device != "" && !containsString(allowedProfileDevices, device) {
			http.Error(w, "Unknown device", http.StatusBadRequest)
			return
		}
		profile["device"] = device
	}
	if req.Timezone != nil {
		tz := trim(req.Timezone, 60)
		if tz != "" && !timezonePattern.MatchString(tz) {
			http.Error(w, "Unknown timezone", http.StatusBadRequest)
			return
		}
		profile["timezone"] = tz
	}
	if req.DisplayName != nil {
		name := trim(req.DisplayName, 60)
		if name == "" {
			// Clearing the override hands the name back to Discord.
			name = p.Username
			profile["name_locked"] = false
		}
		profile["display_name"] = name
	}
	if req.NameLocked != nil {
		profile["name_locked"] = *req.NameLocked
	}
	if len(profile) > 0 {
		if err := DB.Model(&Player{}).Where("id = ?", playerID).Updates(profile).Error; err != nil {
			http.Error(w, "Failed to save profile", http.StatusInternalServerError)
			return
		}
	}

	ps := playerSettingsFor(playerID)
	updates := map[string]any{}

	if req.DefaultLeague != nil {
		updates["default_league"] = normalizeLeague(*req.DefaultLeague)
	}
	if req.LandingPage != nil {
		updates["landing_page"] = trim(req.LandingPage, 60)
	}
	if req.CompactCards != nil {
		updates["compact_cards"] = *req.CompactCards
	}
	if req.ReduceMotion != nil {
		updates["reduce_motion"] = *req.ReduceMotion
	}
	if req.NotifySound != nil {
		updates["notify_sound"] = *req.NotifySound
	}
	if req.DefaultStartTime != nil {
		if !isHHMM(*req.DefaultStartTime) {
			http.Error(w, "default_start_time must be HH:MM", http.StatusBadRequest)
			return
		}
		updates["default_start_time"] = strings.TrimSpace(*req.DefaultStartTime)
	}
	if req.DefaultEndTime != nil {
		if !isHHMM(*req.DefaultEndTime) {
			http.Error(w, "default_end_time must be HH:MM", http.StatusBadRequest)
			return
		}
		updates["default_end_time"] = strings.TrimSpace(*req.DefaultEndTime)
	}
	if req.AvailableAsSub != nil {
		updates["available_as_sub"] = *req.AvailableAsSub
	}
	if req.HideAvailability != nil {
		updates["hide_availability"] = *req.HideAvailability
	}
	if req.MutedNotificationTypes != nil {
		// Keep only known types, de-duplicated.
		allowed := map[string]bool{}
		for _, t := range notificationTypes {
			allowed[t] = true
		}
		seen := map[string]bool{}
		clean := []string{}
		for _, t := range strings.Split(*req.MutedNotificationTypes, ",") {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" || !allowed[t] || seen[t] {
				continue
			}
			seen[t] = true
			clean = append(clean, t)
		}
		updates["muted_notification_types"] = strings.Join(clean, ",")
	}

	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
		if err := DB.Model(&PlayerSettings{}).Where("player_id = ?", ps.PlayerID).Updates(updates).Error; err != nil {
			http.Error(w, "Failed to save settings", http.StatusInternalServerError)
			return
		}
	}

	// Keep the session's cached display name in step with a rename.
	if req.DisplayName != nil {
		var fresh Player
		if DB.Select("display_name").First(&fresh, playerID).Error == nil {
			session.Values["display_name"] = fresh.DisplayName
			_ = session.Save(r, w)
		}
	}

	respondJSON(w, map[string]any{
		"success": true,
		"prefs":   playerSettingsFor(playerID),
	})
}

// POST /api/player/overlay-token — rotates (or creates) the personal overlay token.
func RotateOverlayToken(w http.ResponseWriter, r *http.Request) {
	session, ok := requireLogin(w, r)
	if !ok {
		return
	}
	discordIDStr, _ := session.Values["discord_id"].(string)
	playerID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	ps := playerSettingsFor(playerID)
	token := newOverlayToken()
	if token == "" {
		http.Error(w, "Failed to generate a token", http.StatusInternalServerError)
		return
	}

	if err := DB.Model(&PlayerSettings{}).Where("player_id = ?", ps.PlayerID).
		Update("overlay_token", token).Error; err != nil {
		http.Error(w, "Failed to save the token", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{"success": true, "overlay_token": token})
}

// GET /api/player/export — downloads everything ECGL stores about the player.
func ExportPlayerData(w http.ResponseWriter, r *http.Request) {
	session, ok := requireLogin(w, r)
	if !ok {
		return
	}
	discordIDStr, _ := session.Values["discord_id"].(string)
	playerID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	var p Player
	if err := DB.First(&p, playerID).Error; err != nil {
		http.Error(w, "Player not found", http.StatusNotFound)
		return
	}

	var stats []PlayerLeagueStats
	DB.Where("player_id = ?", playerID).Find(&stats)

	var history []PlayerHistory
	DB.Where("player_id = ?", playerID).Order("id ASC").Find(&history)

	var availability []PlayerAvailability
	DB.Where("player_id = ?", playerID).Order("date, start_time").Find(&availability)

	var matches []Match
	DB.Where("team_a_id IN (?) OR team_b_id IN (?)",
		DB.Model(&TeamMember{}).Select("team_id").Where("player_id = ?", playerID),
		DB.Model(&TeamMember{}).Select("team_id").Where("player_id = ?", playerID),
	).Order("id ASC").Limit(1000).Find(&matches)

	ps := playerSettingsFor(playerID)

	payload := map[string]any{
		"exported_at": time.Now().UTC(),
		"profile": map[string]any{
			"id":           strconv.FormatInt(p.ID, 10),
			"username":     p.Username,
			"display_name": p.DisplayName,
			"role":         p.Role,
			"device":       p.Device,
			"timezone":     p.Timezone,
		},
		"league_stats": stats,
		"team_history": history,
		"availability": availability,
		"matches":      matches,
		"prefs":        ps,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="ecgl-player-%d.json"`, playerID))
	_ = json.NewEncoder(w).Encode(payload)
}
