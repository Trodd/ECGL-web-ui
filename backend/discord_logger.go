package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// -------------------------------------------------------------------
// ⚙️ Base function — sends a message to ANY Discord channel
// -------------------------------------------------------------------
func SendDiscordChannelLog(channelID, msg string) {
	botToken := os.Getenv("DISCORD_BOT_TOKEN")

	// Always log to local console as well, so Discord-logged actions are visible
	// even when Discord logging is disabled/misconfigured.
	if channelID == "" {
		log.Printf("📣 [DISCORD] (no channel) %s", msg)
	} else {
		log.Printf("📣 [DISCORD][channel:%s] %s", channelID, msg)
	}

	if channelID == "" || botToken == "" {
		log.Println("⚠️ Discord logging disabled (missing channel ID or bot token)")
		return
	}

	body, _ := json.Marshal(map[string]string{"content": msg})

	req, _ := http.NewRequest(
		"POST",
		"https://discord.com/api/v10/channels/"+channelID+"/messages",
		bytes.NewBuffer(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("❌ Failed sending Discord log:", err)
		return
	}
	resp.Body.Close()
}

// -------------------------------------------------------------------
// 📢 Backwards-compatible function for GENERAL logs
// Uses DISCORD_LOG_CHANNEL_GENERAL instead of the old var
// -------------------------------------------------------------------
func SendDiscordLog(msg string) {
	channelID := os.Getenv("DISCORD_LOG_CHANNEL_GENERAL")
	SendDiscordChannelLog(channelID, msg)
}

// -------------------------------------------------------------------
// 🎮 General Logs (players, teams, join requests, etc.)
// -------------------------------------------------------------------
func LogGeneral(msg string) {
	channelID := os.Getenv("DISCORD_LOG_CHANNEL_GENERAL")
	SendDiscordChannelLog(channelID, msg)
}

// -------------------------------------------------------------------
// 📅 Match Scheduling Logs
// -------------------------------------------------------------------
func LogMatch(msg string) {
	channelID := os.Getenv("DISCORD_LOG_CHANNEL_MATCHES")
	SendDiscordChannelLog(channelID, msg)
}

// -------------------------------------------------------------------
// 📝 Score / Forfeit Logs
// -------------------------------------------------------------------
func LogScore(msg string) {
	channelID := os.Getenv("DISCORD_LOG_CHANNEL_SCORES")
	SendDiscordChannelLog(channelID, msg)
}

// -------------------------------------------------------------------
// 🏷️ Discord Role Management (Add/Remove Roles)
// -------------------------------------------------------------------

func DiscordAddRole(discordID, roleID string) error {
	queueRoleJob(func() {
		botToken := os.Getenv("DISCORD_BOT_TOKEN")
		guildID := os.Getenv("DISCORD_GUILD_ID")

		if botToken == "" || guildID == "" || discordID == "" || roleID == "" {
			log.Println("⚠️ Missing Discord role env vars or IDs")
			return
		}

		url := "https://discord.com/api/v10/guilds/" + guildID +
			"/members/" + discordID + "/roles/" + roleID

		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "ECGL-Bot (RoleSync)")
		req.Header.Set("Authorization", "Bot "+botToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Println("❌ Failed to add Discord role:", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 204 {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("❌ AddRole FAILED: %d %s", resp.StatusCode, body)
		}
	})

	return nil
}

func DiscordRemoveRole(discordID, roleID string) error {
	queueRoleJob(func() {
		botToken := os.Getenv("DISCORD_BOT_TOKEN")
		guildID := os.Getenv("DISCORD_GUILD_ID")

		if botToken == "" || guildID == "" || discordID == "" || roleID == "" {
			log.Println("⚠️ Missing Discord role env vars or IDs")
			return
		}

		url := "https://discord.com/api/v10/guilds/" + guildID +
			"/members/" + discordID + "/roles/" + roleID

		req, _ := http.NewRequest("DELETE", url, nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "ECGL-Bot (RoleSync)")
		req.Header.Set("Authorization", "Bot "+botToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Println("❌ Failed to remove Discord role:", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 204 {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("❌ RemoveRole FAILED: %d %s", resp.StatusCode, body)
		}
	})

	return nil
}

func StartMatchChannelScheduler(session *discordgo.Session) {
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()

		for {
			<-ticker.C
			processMatchChannels(session)
		}
	}()
}

func processMatchChannels(s *discordgo.Session) {
	now := time.Now().UTC()

	var matches []Match
	DB.Where("match_time IS NOT NULL").
		Where("status = ?", "Scheduled").
		Find(&matches)

	for _, m := range matches {
		if m.ScheduledDate == nil {
			return
		}

		matchTime := m.ScheduledDate.UTC()

		// 🟢 CREATE CHANNEL
		if m.DiscordChannelID == nil &&
			now.After(matchTime.Add(-1*time.Hour)) &&
			now.Before(matchTime.Add(2*time.Hour)) {

			createMatchChannel(s, &m)
		}

		// 🔴 DELETE CHANNEL
		if m.DiscordChannelID != nil &&
			m.ChannelCreatedAt != nil &&
			now.After(m.ChannelCreatedAt.Add(3*time.Hour)) {

			deleteMatchChannel(s, &m)
		}
	}
}

func createMatchChannel(s *discordgo.Session, m *Match) {
	if s == nil || m == nil || m.ScheduledDate == nil {
		return
	}

	categoryID := os.Getenv("MATCH_CHANNEL_CATEGORY_ID")
	guildID := os.Getenv("DISCORD_GUILD_ID")
	if categoryID == "" || guildID == "" {
		return
	}

	// Load teams
	var teamA, teamB Team
	if err := DB.First(&teamA, m.TeamAID).Error; err != nil {
		return
	}
	if err := DB.First(&teamB, m.TeamBID).Error; err != nil {
		return
	}

	// Deny @everyone
	overwrites := []*discordgo.PermissionOverwrite{
		{
			ID:   guildID,
			Type: discordgo.PermissionOverwriteTypeRole,
			Deny: discordgo.PermissionViewChannel,
		},
	}

	// ✅ ALLOW BOT (REQUIRED)
	botUserID := os.Getenv("DISCORD_CLIENT_ID")
	if botUserID != "" {
		overwrites = append(overwrites, &discordgo.PermissionOverwrite{
			ID:   botUserID,
			Type: discordgo.PermissionOverwriteTypeMember,
			Allow: discordgo.PermissionViewChannel |
				discordgo.PermissionSendMessages |
				discordgo.PermissionReadMessageHistory |
				discordgo.PermissionEmbedLinks |
				discordgo.PermissionAttachFiles,
		})
	}

	var casterIDs []int64
	{
		var cast CastLogMulti
		if err := DB.Where("match_id = ?", m.ID).First(&cast).Error; err == nil {
			if len(cast.Casters) > 0 {
				_ = json.Unmarshal(cast.Casters, &casterIDs)
			}
		}
	}

	if len(casterIDs) > 0 {
		casterRoleID := os.Getenv("DISCORD_CASTER_ROLE_ID")
		if casterRoleID != "" {
			overwrites = append(overwrites, &discordgo.PermissionOverwrite{
				ID:   casterRoleID,
				Type: discordgo.PermissionOverwriteTypeRole,
				Allow: discordgo.PermissionViewChannel |
					discordgo.PermissionSendMessages |
					discordgo.PermissionReadMessageHistory,
			})
		}
	}

	var membersA, membersB []TeamMember
	DB.Where("team_id = ?", m.TeamAID).Find(&membersA)
	DB.Where("team_id = ?", m.TeamBID).Find(&membersB)

	seen := map[int64]bool{}
	mentions := []string{}

	// 🔧 split helper: add with optional ping
	add := func(id int64, ping bool) {
		if id == 0 || seen[id] {
			return
		}
		seen[id] = true

		overwrites = append(overwrites, &discordgo.PermissionOverwrite{
			ID:   strconv.FormatInt(id, 10),
			Type: discordgo.PermissionOverwriteTypeMember,
			Allow: discordgo.PermissionViewChannel |
				discordgo.PermissionSendMessages |
				discordgo.PermissionReadMessageHistory,
		})

		if ping {
			mentions = append(mentions, fmt.Sprintf("<@%d>", id))
		}
	}

	// 🔵 Team rosters (pinged)
	for _, tm := range membersA {
		add(tm.PlayerID, true)
	}
	for _, tm := range membersB {
		add(tm.PlayerID, true)
	}

	// 🔧 ADD CASTERS (permissions only, NO ping)
	{
		var cast CastLogMulti
		if err := DB.Where("match_id = ?", m.ID).First(&cast).Error; err == nil {
			var casterIDs []int64
			if len(cast.Casters) > 0 {
				_ = json.Unmarshal(cast.Casters, &casterIDs)
			}

			for _, cid := range casterIDs {
				add(cid, false) // ← no mention
			}
		}
	}

	// 🔨 Create channel
	channel, err := s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:                 fmt.Sprintf("match-%d", m.ID),
		Type:                 discordgo.ChannelTypeGuildText,
		ParentID:             categoryID,
		PermissionOverwrites: overwrites,
	})
	if err != nil {
		return
	}

	now := time.Now().UTC()
	closeAt := now.Add(3 * time.Hour)

	DB.Model(m).Updates(map[string]any{
		"discord_channel_id": channel.ID,
		"channel_created_at": now,
	})

	// ---- MATCH TIME FORMATTING ----
	matchTime := "TBD"
	matchTimeRel := ""
	if m.ScheduledDate != nil {
		matchTime = fmt.Sprintf("<t:%d:F>", m.ScheduledDate.Unix())
		matchTimeRel = fmt.Sprintf("⏳ **Starts:** <t:%d:R>\n", m.ScheduledDate.Unix())
	}

	// ---- BUILD REMINDER MESSAGE ----
	msg := fmt.Sprintf(
		"🏁 **Match Channel Opened**\n"+
			"🔵 **%s** vs 🔴 **%s**\n\n"+
			"📅 **Match Time:** %s\n"+
			"%s\n"+
			"👥 %s\n\n"+
			"🔒 **Channel closes:** <t:%d:R> *(<t:%d:F>)*",
		teamA.Name,
		teamB.Name,
		matchTime,
		matchTimeRel,
		strings.Join(mentions, " "),
		closeAt.Unix(),
		closeAt.Unix(),
	)

	// ---- SEND WITH SAFE PINGS ----
	userIDs := make([]string, 0, len(mentions))
	for id := range seen {
		userIDs = append(userIDs, strconv.FormatInt(id, 10))
	}

	s.ChannelMessageSendComplex(channel.ID, &discordgo.MessageSend{
		Content: msg,
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Users: userIDs,
		},
	})

	if len(casterIDs) > 0 {
		mentions := []string{}
		for _, id := range casterIDs {
			mentions = append(mentions, fmt.Sprintf("<@%d>", id))
		}

		castMsg := fmt.Sprintf(
			"🎥 **THIS MATCH IS BEING CASTED**\n\n"+
				"Casters: %s\n\n"+
				"⛔ **DO NOT START THE MATCH** until the **casters** give the green light.\n"+
				"🎙️ Please coordinate stream setup here.",
			strings.Join(mentions, " "),
		)

		_, err := s.ChannelMessageSend(channel.ID, castMsg)
		if err != nil {
			log.Printf("❌ Failed to send cast message on channel create: %v", err)
		} else {
			log.Printf("✅ Cast message sent on channel creation (%s)", channel.ID)
		}
	}
}

func addCasterToExistingChannel(
	s *discordgo.Session,
	channelID string,
	casterID int64,
) {
	if s == nil || channelID == "" || casterID == 0 {
		return
	}

	queueRoleJob(func() {
		allow := int64(
			discordgo.PermissionViewChannel |
				discordgo.PermissionSendMessages |
				discordgo.PermissionReadMessageHistory,
		)

		deny := int64(0)

		// ✅ YOUR discordgo order: (channelID, targetID, type, allow, deny)
		err := s.ChannelPermissionSet(
			channelID,
			strconv.FormatInt(casterID, 10),
			discordgo.PermissionOverwriteTypeMember,
			allow,
			deny,
		)

		if err != nil {
			log.Printf("❌ Failed to add caster %d to channel %s: %v", casterID, channelID, err)
		}
	})
}

func ensureCasterRoleOverwrite(
	s *discordgo.Session,
	channelID string,
) {
	casterRoleID := os.Getenv("DISCORD_CASTER_ROLE_ID")
	if s == nil || channelID == "" || casterRoleID == "" {
		return
	}

	queueRoleJob(func() {
		allow := int64(
			discordgo.PermissionViewChannel |
				discordgo.PermissionSendMessages |
				discordgo.PermissionReadMessageHistory,
		)

		// 🔴 IMPORTANT: explicitly override deny
		deny := int64(0)

		err := s.ChannelPermissionSet(
			channelID,
			casterRoleID,
			discordgo.PermissionOverwriteTypeRole,
			allow,
			deny,
		)

		if err != nil {
			log.Printf("❌ Failed caster ROLE overwrite %s: %v", channelID, err)
		} else {
			log.Printf("✅ Ensured caster ROLE overwrite on %s", channelID)
		}
	})
}

func sendChannelMessageHTTP(channelID, content, botToken string) error {
	if channelID == "" || content == "" || botToken == "" {
		return fmt.Errorf("missing params")
	}

	payload := map[string]any{
		"content": content,
	}

	b, _ := json.Marshal(payload)

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(b))
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send message failed %s %s", resp.Status, body)
	}

	return nil
}

func deleteMatchChannel(s *discordgo.Session, m *Match) {
	if s == nil || m == nil || m.DiscordChannelID == nil {
		return
	}

	_, err := s.ChannelDelete(*m.DiscordChannelID)
	if err != nil {
		log.Printf("⚠️ Failed to delete channel %s: %v", *m.DiscordChannelID, err)
	}

	DB.Model(m).Updates(map[string]any{
		"discord_channel_id": nil,
		"channel_created_at": nil,
	})
}

func RecoverMatchChannels(s *discordgo.Session) {
	if s == nil {
		return
	}

	var matches []Match

	// Only matches with existing channels
	DB.Where(
		"discord_channel_id IS NOT NULL AND channel_created_at IS NOT NULL",
	).Find(&matches)

	now := time.Now().UTC()

	for _, m := range matches {
		if m.DiscordChannelID == nil || m.ChannelCreatedAt == nil {
			continue
		}

		closeAt := m.ChannelCreatedAt.Add(3 * time.Hour)

		// Channel already expired → delete immediately
		if now.After(closeAt) {
			_, _ = s.ChannelDelete(*m.DiscordChannelID)

			DB.Model(&m).Updates(map[string]any{
				"discord_channel_id": nil,
				"channel_created_at": nil,
			})
			continue
		}

		// Still active → reschedule deletion
		go scheduleChannelDeletion(
			s,
			*m.DiscordChannelID,
			closeAt.Sub(now),
			m.ID,
		)
	}
}

func scheduleChannelDeletion(
	s *discordgo.Session,
	channelID string,
	delay time.Duration,
	matchID uint,
) {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	<-timer.C

	_, _ = s.ChannelDelete(channelID)

	DB.Model(&Match{}).Where("id = ?", matchID).Updates(map[string]any{
		"discord_channel_id": nil,
		"channel_created_at": nil,
	})
}

func scheduleMatchChannel(s *discordgo.Session, m *Match) {
	if s == nil || m == nil || m.ScheduledDate == nil {
		return
	}

	openAt := m.ScheduledDate.Add(-1 * time.Hour)
	closeAfter := 3 * time.Hour

	now := time.Now().UTC()

	// If already past open time → create immediately
	if now.After(openAt) {
		createMatchChannel(s, m)
		go scheduleChannelDeletion(s, *m.DiscordChannelID, closeAfter, m.ID)
		return
	}

	delay := openAt.Sub(now)

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		<-timer.C

		createMatchChannel(s, m)

		// Reload match to get channel ID
		var updated Match
		if err := DB.First(&updated, m.ID).Error; err == nil && updated.DiscordChannelID != nil {
			go scheduleChannelDeletion(
				s,
				*updated.DiscordChannelID,
				closeAfter,
				updated.ID,
			)
		}
	}()
}
