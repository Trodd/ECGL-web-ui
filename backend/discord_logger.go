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

	var membersA, membersB []TeamMember
	DB.Where("team_id = ?", m.TeamAID).Find(&membersA)
	DB.Where("team_id = ?", m.TeamBID).Find(&membersB)

	seen := map[int64]bool{}
	mentions := []string{}

	add := func(id int64) {
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

		mentions = append(mentions, fmt.Sprintf("<@%d>", id))
	}

	for _, tm := range membersA {
		add(tm.PlayerID)
	}
	for _, tm := range membersB {
		add(tm.PlayerID)
	}

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
