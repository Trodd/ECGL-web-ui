package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Caster production board: for each confirmed match, three caster slots and one
// camera-operator slot. Caster-role users can claim a slot for themselves or
// assign any other player, and release a slot.

var castSlots = []string{"caster1", "caster2", "camera", "sideline"}

func isValidCastSlot(slot string) bool {
	for _, s := range castSlots {
		if s == slot {
			return true
		}
	}
	return false
}

func castSlotLabel(slot string) string {
	switch slot {
	case "caster1":
		return "Caster 1"
	case "caster2":
		return "Caster 2"
	case "camera":
		return "Cam Op"
	case "sideline":
		return "Sideline"
	default:
		return slot
	}
}

// castCrewPlayerIDs returns the player ids holding any cast slot on a match
// (caster1 / caster2 / camera / sideline) according to the caster board.
func castCrewPlayerIDs(matchID uint) []int64 {
	var ids []int64
	DB.Model(&CastAssignment{}).
		Where("match_id = ?", matchID).
		Distinct().
		Pluck("player_id", &ids)
	return ids
}

// matchIsPast reports whether a match is already played/archived, in which case
// the cast crew can still be corrected after the fact.
func matchIsPast(m Match) bool {
	if m.Archived {
		return true
	}
	if m.WinnerID != nil || m.LoserID != nil {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(m.Status), "scheduled")
}

// requireCaster verifies the session user holds the Discord caster role.
func requireCaster(w http.ResponseWriter, r *http.Request) (string, bool) {
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || strings.TrimSpace(discordID) == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", false
	}

	guildID := getEnv("DISCORD_GUILD_ID", "")
	casterRoleID := getEnv("DISCORD_CASTER_ROLE_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")
	if guildID == "" || casterRoleID == "" || botToken == "" {
		http.Error(w, "Caster role not configured", http.StatusInternalServerError)
		return "", false
	}

	// Cached (gateway state first) — see discordMemberRoles.
	if !userHasDiscordRole(discordID, casterRoleID) {
		http.Error(w, "You are not a caster", http.StatusForbidden)
		return "", false
	}

	return discordID, true
}

type castSlotInfo struct {
	Slot        string `json:"slot"`
	Label       string `json:"label"`
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
	Avatar      string `json:"avatar"`
	AvatarURL   string `json:"avatar_url"`
	AssignedBy  string `json:"assigned_by"`
	IsMine      bool   `json:"is_mine"`
}

// GET /api/casters/matches
// Only matches where BOTH teams confirmed a schedule, and that aren't finished.
func HandleGetCasterMatches(w http.ResponseWriter, r *http.Request) {
	me, ok := requireCaster(w, r)
	if !ok {
		return
	}

	lg := leagueFromRequest(r)

	type matchRow struct {
		ID            uint
		MatchCode     string
		Season        string
		Week          string
		Status        string
		ScheduledDate *time.Time
		TeamAID       uint
		TeamA         string
		TeamBID       uint
		TeamB         string
		MapCount      int
	}

	// ?archived=1 lists played/archived matches so cast crews can be corrected
	// after the fact. Default is the live board (scheduled, not yet played).
	archivedOnly := r.URL.Query().Get("archived") == "1"
	search := strings.TrimSpace(r.URL.Query().Get("q"))

	var rows []matchRow

	baseSelect := `
		SELECT m.id, m.match_code, COALESCE(m.season,'') AS season, COALESCE(m.week,'') AS week,
		       m.status, m.scheduled_date,
		       m.team_a_id, t1.name AS team_a,
		       m.team_b_id, t2.name AS team_b,
		       (SELECT COUNT(*) FROM match_scores ms WHERE ms.match_id = m.id) AS map_count
		FROM matches m
		JOIN teams t1 ON t1.id = m.team_a_id
		JOIN teams t2 ON t2.id = m.team_b_id
		WHERE m.league = ?`

	var query string
	args := []any{lg}

	if archivedOnly {
		query = baseSelect + `
		  AND (
		        COALESCE(m.archived, FALSE) = TRUE
		     OR m.winner_id IS NOT NULL
		     OR m.loser_id IS NOT NULL
		     OR LOWER(m.status) <> 'scheduled'
		  )`

		// Free-text search across teams, match code, season and week.
		if search != "" {
			like := "%" + strings.ToLower(search) + "%"
			query += `
		  AND (
		        LOWER(t1.name) LIKE ?
		     OR LOWER(t2.name) LIKE ?
		     OR LOWER(m.match_code) LIKE ?
		     OR LOWER(COALESCE(m.season, '')) LIKE ?
		     OR LOWER(COALESCE(m.week, '')) LIKE ?
		  )`
			args = append(args, like, like, like, like, like)
		}

		query += `
		ORDER BY m.scheduled_date DESC NULLS LAST, m.id DESC
		LIMIT 300`
	} else {
		query = baseSelect + `
		  AND m.team_a_schedule_confirmed = TRUE
		  AND m.team_b_schedule_confirmed = TRUE
		  AND COALESCE(m.archived, FALSE) = FALSE
		  AND LOWER(m.status) = 'scheduled'
		  AND m.winner_id IS NULL
		  AND m.loser_id IS NULL
		ORDER BY m.scheduled_date ASC NULLS LAST, m.id ASC`
	}

	if err := DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		log.Printf("❌ HandleGetCasterMatches: %v", err)
		http.Error(w, "Failed to load matches", http.StatusInternalServerError)
		return
	}

	type matchOut struct {
		ID            uint           `json:"id"`
		MatchCode     string         `json:"match_code"`
		Season        string         `json:"season"`
		Week          string         `json:"week"`
		Status        string         `json:"status"`
		ScheduledDate *time.Time     `json:"scheduled_date"`
		TeamAID       uint           `json:"team_a_id"`
		TeamA         string         `json:"team_a"`
		TeamBID       uint           `json:"team_b_id"`
		TeamB         string         `json:"team_b"`
		StreamChannel string         `json:"stream_channel"`
		MatchType     string         `json:"match_type"`
		Slots         []castSlotInfo `json:"slots"`
	}

	out := make([]matchOut, 0, len(rows))

	for _, m := range rows {
		slots := make([]castSlotInfo, 0, len(castSlots))

		var assigns []CastAssignment
		DB.Where("match_id = ?", m.ID).Find(&assigns)
		bySlot := map[string]CastAssignment{}
		for _, a := range assigns {
			bySlot[a.Slot] = a
		}

		for _, slot := range castSlots {
			info := castSlotInfo{Slot: slot, Label: castSlotLabel(slot)}
			if a, found := bySlot[slot]; found {
				var p Player
				if err := DB.Select("id, username, display_name, avatar").
					First(&p, a.PlayerID).Error; err == nil {
					info.PlayerID = strconv.FormatInt(p.ID, 10)
					info.DisplayName = p.DisplayName
					info.Username = p.Username
					info.Avatar = p.Avatar
					info.AvatarURL = discordAvatarURL(p.ID, p.Avatar)
				} else {
					info.PlayerID = strconv.FormatInt(a.PlayerID, 10)
				}
				info.AssignedBy = strconv.FormatInt(a.AssignedBy, 10)
				info.IsMine = strconv.FormatInt(a.PlayerID, 10) == me
			}
			slots = append(slots, info)
		}

		out = append(out, matchOut{
			ID:            m.ID,
			MatchCode:     m.MatchCode,
			Season:        m.Season,
			Week:          m.Week,
			Status:        m.Status,
			ScheduledDate: m.ScheduledDate,
			TeamAID:       m.TeamAID,
			TeamA:         m.TeamA,
			TeamBID:       m.TeamBID,
			TeamB:         m.TeamB,
			StreamChannel: castStreamFor(m.ID),
			MatchType:     castMatchTypeOrDefault(m.ID, m.MapCount),
			Slots:         slots,
		})
	}

	respondJSON(w, map[string]any{
		"success":            true,
		"league":             lg,
		"archived":           archivedOnly,
		"stream_options":     castStreamOptions(),
		"match_type_options": castMatchTypeOptions(),
		"matches":            out,
	})
}

// POST /api/casters/assign  { match_id, slot, player_id? }
// player_id omitted → claim the slot for yourself.
func HandleAssignCastSlot(w http.ResponseWriter, r *http.Request) {
	me, ok := requireCaster(w, r)
	if !ok {
		return
	}

	var req struct {
		MatchID  uint   `json:"match_id"`
		Slot     string `json:"slot"`
		PlayerID string `json:"player_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	slot := strings.ToLower(strings.TrimSpace(req.Slot))
	if !isValidCastSlot(slot) {
		http.Error(w, "Invalid slot", http.StatusBadRequest)
		return
	}

	targetStr := strings.TrimSpace(req.PlayerID)
	if targetStr == "" {
		targetStr = me
	}
	targetID, err := strconv.ParseInt(targetStr, 10, 64)
	if err != nil || targetID <= 0 {
		http.Error(w, "Invalid player id", http.StatusBadRequest)
		return
	}

	var target Player
	if err := DB.First(&target, targetID).Error; err != nil {
		http.Error(w, "Player not found", http.StatusNotFound)
		return
	}

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}
	if !matchIsPast(match) && (!match.TeamAScheduleConfirmed || !match.TeamBScheduleConfirmed) {
		http.Error(w, "Both teams must confirm the schedule before casting can be assigned",
			http.StatusForbidden)
		return
	}

	// A person may claim several slots on the same match (solo casting: e.g.
	// caster + cam op), so no duplicate-slot guard here.

	// Only notify when this is their first slot on the match.
	var slotsHeld int64
	DB.Model(&CastAssignment{}).
		Where("match_id = ? AND player_id = ?", match.ID, targetID).
		Count(&slotsHeld)

	meID, _ := strconv.ParseInt(me, 10, 64)

	var existing CastAssignment
	err = DB.Where("match_id = ? AND slot = ?", match.ID, slot).First(&existing).Error
	if err != nil {
		existing = CastAssignment{
			MatchID:    match.ID,
			Slot:       slot,
			PlayerID:   targetID,
			AssignedBy: meID,
		}
		if err := DB.Create(&existing).Error; err != nil {
			log.Printf("❌ HandleAssignCastSlot create: %v", err)
			http.Error(w, "Failed to assign slot", http.StatusInternalServerError)
			return
		}
	} else {
		if err := DB.Model(&existing).Updates(map[string]any{
			"player_id":   targetID,
			"assigned_by": meID,
		}).Error; err != nil {
			log.Printf("❌ HandleAssignCastSlot update: %v", err)
			http.Error(w, "Failed to assign slot", http.StatusInternalServerError)
			return
		}
	}

	if targetID != meID && slotsHeld == 0 {
		createNotification(targetID, "cast_assigned",
			"You're on a cast",
			fmt.Sprintf("You were assigned as %s for %s", castSlotLabel(slot), match.MatchCode),
			"/casters",
		)
	}

	// Grant the assignee access to the match's private Discord channel. This used
	// to happen when a cast was saved from the match page; slots are claimed here
	// now, so the channel permissions follow the claim.
	channelID := ""
	if match.DiscordChannelID != nil {
		channelID = strings.TrimSpace(*match.DiscordChannelID)
	}
	if channelID != "" {
		botToken := getEnv("DISCORD_BOT_TOKEN", "")
		if discordSession != nil && botToken != "" {
			if err := addMemberOverwriteHTTP(channelID, targetID, botToken); err != nil {
				log.Printf("⚠️ HandleAssignCastSlot: channel member perms for %d: %v", targetID, err)
			}
			if casterRoleID := strings.TrimSpace(getEnv("DISCORD_CASTER_ROLE_ID", "")); casterRoleID != "" {
				if err := addRoleOverwriteHTTP(channelID, casterRoleID, botToken); err != nil {
					log.Printf("⚠️ HandleAssignCastSlot: channel role perms: %v", err)
				}
			}
		}
	}

	// No Discord log for cast slots — claiming/releasing is routine bookkeeping
	// and would spam the general channel.

	respondJSON(w, map[string]any{
		"success":    true,
		"slot":       slot,
		"match_id":   match.ID,
		"match_code": match.MatchCode,
		"player_id":  targetStr,
		"channel_id": channelID,
		"message":    fmt.Sprintf("%s → %s", castSlotLabel(slot), draftPlayerLabel(target)),
	})
}

// castStreamOptions returns the selectable stream channels (STREAM_CHANNELS,
// comma separated, falling back to TWITCH_CHANNEL).
func castStreamOptions() []string {
	raw := strings.TrimSpace(getEnv("STREAM_CHANNELS", ""))
	if raw == "" {
		if def := strings.TrimSpace(getEnv("TWITCH_CHANNEL", "")); def != "" {
			return []string{def}
		}
		return []string{}
	}

	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// castStreamFor returns the stream channel selected for a match.
func castStreamFor(matchID uint) string {
	var multi CastLogMulti
	if err := DB.Where("match_id = ?", matchID).First(&multi).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(multi.StreamURL)
}

// POST /api/casters/stream  { match_id, stream_url }
// Picks (or clears) the stream channel used by Go Live for this match.
func HandleSetCastStream(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireCaster(w, r); !ok {
		return
	}

	var req struct {
		MatchID   uint   `json:"match_id"`
		StreamURL string `json:"stream_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	url := strings.TrimSpace(req.StreamURL)
	if url != "" && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		http.Error(w, "Stream channel must be a full https URL", http.StatusBadRequest)
		return
	}

	var multi CastLogMulti
	err := DB.Where("match_id = ?", match.ID).First(&multi).Error
	if err != nil {
		multi = CastLogMulti{MatchID: match.ID, StreamURL: url}
		if err := DB.Create(&multi).Error; err != nil {
			log.Printf("❌ HandleSetCastStream create: %v", err)
			http.Error(w, "Failed to save stream channel", http.StatusInternalServerError)
			return
		}
	} else if err := DB.Model(&multi).Update("stream_url", url).Error; err != nil {
		log.Printf("❌ HandleSetCastStream update: %v", err)
		http.Error(w, "Failed to save stream channel", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success":        true,
		"match_id":       match.ID,
		"stream_channel": url,
	})
}

// castMatchTypeOptions are the series formats a caster can pick from.
func castMatchTypeOptions() []string {
	return []string{"Best of 1", "Best of 3", "Best of 5", "Best of 7"}
}

func isValidCastMatchType(v string) bool {
	for _, o := range castMatchTypeOptions() {
		if o == v {
			return true
		}
	}
	return false
}

// defaultMatchType derives the series format from the number of recorded maps,
// defaulting to Best of 3.
func defaultMatchType(mapCount int) string {
	bestOf := 3
	if mapCount > bestOf {
		bestOf = mapCount
	}
	return fmt.Sprintf("Best of %d", bestOf)
}

// castMatchTypeFor returns the caster-selected series format, or "" when unset.
func castMatchTypeFor(matchID uint) string {
	var multi CastLogMulti
	if err := DB.Select("match_type").Where("match_id = ?", matchID).First(&multi).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(multi.MatchType)
}

// castMatchTypeOrDefault returns the caster-selected format, falling back to the
// derived one when nothing has been chosen yet.
func castMatchTypeOrDefault(matchID uint, mapCount int) string {
	if v := castMatchTypeFor(matchID); v != "" {
		return v
	}
	return defaultMatchType(mapCount)
}

// POST /api/casters/match-type  { match_id, match_type }
// Sets (or clears, with an empty match_type) the series format the caster bot
// announces for this match.
func HandleSetCastMatchType(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireCaster(w, r); !ok {
		return
	}

	var req struct {
		MatchID   uint   `json:"match_id"`
		MatchType string `json:"match_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	value := strings.TrimSpace(req.MatchType)
	if value != "" && !isValidCastMatchType(value) {
		http.Error(w, "Invalid match type", http.StatusBadRequest)
		return
	}

	var multi CastLogMulti
	err := DB.Where("match_id = ?", match.ID).First(&multi).Error
	if err != nil {
		multi = CastLogMulti{MatchID: match.ID, MatchType: value}
		if err := DB.Create(&multi).Error; err != nil {
			log.Printf("❌ HandleSetCastMatchType create: %v", err)
			http.Error(w, "Failed to save match type", http.StatusInternalServerError)
			return
		}
	} else if err := DB.Model(&multi).Update("match_type", value).Error; err != nil {
		log.Printf("❌ HandleSetCastMatchType update: %v", err)
		http.Error(w, "Failed to save match type", http.StatusInternalServerError)
		return
	}

	effective := value
	if effective == "" {
		effective = defaultMatchType(matchMapCount(match))
	}

	respondJSON(w, map[string]any{
		"success":    true,
		"match_id":   match.ID,
		"match_type": effective,
		"message":    effective,
	})
}

// POST /api/casters/crew-ready  { match_id }
//
// Posts "casters are ready" in the match's private channel and pings everyone
// on both teams.
func HandleCasterCrewReady(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireCaster(w, r); !ok {
		return
	}

	var req struct {
		MatchID uint `json:"match_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var match Match
	if err := DB.First(&match, req.MatchID).Error; err != nil {
		http.Error(w, "Match not found", http.StatusNotFound)
		return
	}

	if match.DiscordChannelID == nil || strings.TrimSpace(*match.DiscordChannelID) == "" {
		http.Error(w,
			"This match has no private channel yet — it's created about an hour before the scheduled time.",
			http.StatusBadRequest)
		return
	}

	if hasCaster, hasCam := matchCastReady(match.ID); !hasCaster || !hasCam {
		http.Error(w, "Assign at least one caster and a cam op before calling the crew ready.",
			http.StatusBadRequest)
		return
	}
	if discordSession == nil {
		http.Error(w, "Discord bot not connected", http.StatusServiceUnavailable)
		return
	}

	var playerIDs []int64
	DB.Table("team_members").
		Where("team_id IN ?", []uint{match.TeamAID, match.TeamBID}).
		Pluck("player_id", &playerIDs)

	mentions := make([]string, 0, len(playerIDs))
	for _, id := range playerIDs {
		mentions = append(mentions, fmt.Sprintf("<@%d>", id))
	}

	msg := "🎥 **Casters are ready** — we can start whenever you are!"
	if len(mentions) > 0 {
		msg += "\n" + strings.Join(mentions, " ")
	}

	if _, err := discordSession.ChannelMessageSend(*match.DiscordChannelID, msg); err != nil {
		log.Printf("❌ HandleCasterCrewReady send: %v", err)
		http.Error(w, "Failed to send message", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"message": "Crew-ready message sent to the match channel.",
	})
}

// POST /api/casters/release  { match_id, slot }
func HandleReleaseCastSlot(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireCaster(w, r); !ok {
		return
	}

	var req struct {
		MatchID uint   `json:"match_id"`
		Slot    string `json:"slot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MatchID == 0 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	slot := strings.ToLower(strings.TrimSpace(req.Slot))
	if !isValidCastSlot(slot) {
		http.Error(w, "Invalid slot", http.StatusBadRequest)
		return
	}

	res := DB.Where("match_id = ? AND slot = ?", req.MatchID, slot).Delete(&CastAssignment{})
	if res.Error != nil {
		http.Error(w, "Failed to release slot", http.StatusInternalServerError)
		return
	}
	if res.RowsAffected == 0 {
		http.Error(w, "That slot is already empty", http.StatusNotFound)
		return
	}

	respondJSON(w, map[string]any{
		"success": true,
		"slot":    slot,
		"message": castSlotLabel(slot) + " released",
	})
}
