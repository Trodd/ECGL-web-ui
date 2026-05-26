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

func SendDiscordEmbed(channelID, title, description, buttonLabel, buttonURL string, mentionUserIDs []string) {
	botToken := os.Getenv("DISCORD_BOT_TOKEN")

	if channelID == "" || botToken == "" {
		log.Println("❌ Missing Discord env vars (Embed not sent)")
		return
	}

	allowedMentions := map[string]any{}
	body := map[string]any{
		"embeds": []any{
			map[string]any{
				"title":       title,
				"description": description,
				"color":       0x3498DB,
			},
		},
	}

	if len(mentionUserIDs) > 0 {
		mentionContent := ""
		for _, id := range mentionUserIDs {
			mentionContent += fmt.Sprintf("<@%s> ", id)
		}
		mentionContent = strings.TrimSpace(mentionContent)
		body["content"] = mentionContent
		allowedMentions["users"] = mentionUserIDs
		body["allowed_mentions"] = allowedMentions
	}

	if buttonLabel != "" && buttonURL != "" {
		body["components"] = []any{
			map[string]any{
				"type": 1,
				"components": []any{
					map[string]any{
						"type":  2,
						"style": 5,
						"label": buttonLabel,
						"url":   buttonURL,
					},
				},
			},
		}
	}

	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(
		"POST",
		"https://discord.com/api/v10/channels/"+channelID+"/messages",
		bytes.NewBuffer(b),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("❌ Failed sending Discord embed:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Failed sending Discord embed: %s %s", resp.Status, strings.TrimSpace(string(body)))
		return
	}
}

func SendDiscordEmbedToGeneral(title, description, buttonLabel, buttonURL string, mentionUserIDs []string) {
	SendDiscordEmbed(os.Getenv("DISCORD_LOG_CHANNEL_GENERAL"), title, description, buttonLabel, buttonURL, mentionUserIDs)
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
	SendDiscordEmbedWithPings("Match Notification", msg, "", "", nil)
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

	// 🔨 Create channel (team-vs-team naming)
	channelName := fmt.Sprintf("%s-vs-%s", sanitizeChannelName(teamA.Name), sanitizeChannelName(teamB.Name))
	channel, err := s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:                 channelName,
		Type:                 discordgo.ChannelTypeGuildText,
		ParentID:             categoryID,
		PermissionOverwrites: overwrites,
	})
	if err != nil {
		return
	}

	now := time.Now().UTC()

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
			"🔒 A League Mod can close this channel with the button below.",
		teamA.Name,
		teamB.Name,
		matchTime,
		matchTimeRel,
		strings.Join(mentions, " "),
	)

	// ---- SEND WITH SAFE PINGS + CLOSE BUTTON ----
	userIDs := make([]string, 0, len(mentions))
	for id := range seen {
		userIDs = append(userIDs, strconv.FormatInt(id, 10))
	}

	s.ChannelMessageSendComplex(channel.ID, &discordgo.MessageSend{
		Content: msg,
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Close Channel",
						Style:    discordgo.DangerButton,
						CustomID: fmt.Sprintf("close_match_channel_%d", m.ID),
						Emoji: &discordgo.ComponentEmoji{
							Name: "🔒",
						},
					},
				},
			},
		},
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

// 🗑️ DELETE MATCH CHANNEL (called by mod via button)
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

// -------------------------------------------------------------------
// 📋 TRANSCRIPT + CLOSE — triggered by mod button click (2-step)
// -------------------------------------------------------------------

// RegisterCloseChannelHandler registers the interaction handler for the
// "Close Channel" button on match channels with a 2-step confirmation.
func RegisterCloseChannelHandler(dg *discordgo.Session) {
	dg.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		// Only handle message component (button) interactions
		if i.Type != discordgo.InteractionMessageComponent {
			return
		}

		customID := i.MessageComponentData().CustomID

		// --- STEP 1: Initial "Close Channel" button → show confirmation ---
		if strings.HasPrefix(customID, "close_match_channel_") && !strings.HasPrefix(customID, "close_match_channel_confirm_") {
			matchIDStr := strings.TrimPrefix(customID, "close_match_channel_")

			// Mod check
			if !isInteractionMod(i) {
				respondInteractionEphemeral(s, i, "❌ Only League Mods can close match channels.")
				return
			}

			// Respond with confirmation prompt (ephemeral)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "⚠️ **Are you sure you want to close this channel?**\nThis will transcript all messages and delete the channel.",
					Flags:   discordgo.MessageFlagsEphemeral,
					Components: []discordgo.MessageComponent{
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.Button{
									Label:    "Confirm Close",
									Style:    discordgo.DangerButton,
									CustomID: fmt.Sprintf("close_match_channel_confirm_%s", matchIDStr),
									Emoji: &discordgo.ComponentEmoji{
										Name: "✅",
									},
								},
								discordgo.Button{
									Label:    "Cancel",
									Style:    discordgo.SecondaryButton,
									CustomID: "close_match_channel_cancel",
								},
							},
						},
					},
				},
			})
			return
		}

		// --- CANCEL: dismiss the confirmation ---
		if customID == "close_match_channel_cancel" {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseUpdateMessage,
				Data: &discordgo.InteractionResponseData{
					Content:    "❌ Channel close cancelled.",
					Components: []discordgo.MessageComponent{},
				},
			})
			return
		}

		// --- STEP 2: Confirmed close ---
		if strings.HasPrefix(customID, "close_match_channel_confirm_") {
			matchIDStr := strings.TrimPrefix(customID, "close_match_channel_confirm_")
			matchID, err := strconv.ParseUint(matchIDStr, 10, 64)
			if err != nil {
				respondInteractionEphemeral(s, i, "❌ Invalid match ID.")
				return
			}

			// Double-check mod role
			if !isInteractionMod(i) {
				respondInteractionEphemeral(s, i, "❌ Only League Mods can close match channels.")
				return
			}

			// ACK with deferred update
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Flags: discordgo.MessageFlagsEphemeral,
				},
			})

			// Load match
			var match Match
			if err := DB.First(&match, matchID).Error; err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: "❌ Match not found.",
					Flags:   discordgo.MessageFlagsEphemeral,
				})
				return
			}

			channelID := i.ChannelID

			// --- TRANSCRIPT THE CHANNEL ---
			transcriptChannelID := os.Getenv("DISCORD_TRANSCRIPT_CHANNEL_ID")
			if transcriptChannelID != "" {
				transcriptMatchChannel(s, channelID, transcriptChannelID, &match)
			} else {
				log.Println("⚠️ DISCORD_TRANSCRIPT_CHANNEL_ID not set, skipping transcript")
			}

			// --- DELETE THE CHANNEL ---
			_, err = s.ChannelDelete(channelID)
			if err != nil {
				log.Printf("❌ Failed to delete channel %s: %v", channelID, err)
			}

			// Clear DB references
			DB.Model(&match).Updates(map[string]any{
				"discord_channel_id": nil,
				"channel_created_at": nil,
			})

			log.Printf("✅ Match channel %s closed by mod %s", channelID, i.Member.User.Username)
		}
	})
}

// isInteractionMod checks if the interaction member has the league mod role.
func isInteractionMod(i *discordgo.InteractionCreate) bool {
	modRoleID := os.Getenv("DISCORD_LEAGUE_MOD_ROLE_ID")
	if modRoleID == "" || i.Member == nil {
		return false
	}
	for _, role := range i.Member.Roles {
		if role == modRoleID {
			return true
		}
	}
	return false
}

// transcriptMatchChannel fetches all messages from a channel, builds a .txt
// transcript file, and posts it with a match info embed to the transcript channel.
func transcriptMatchChannel(s *discordgo.Session, sourceChannelID, transcriptChannelID string, m *Match) {
	// Fetch all messages (up to 100 per request)
	var allMessages []*discordgo.Message
	beforeID := ""

	for {
		msgs, err := s.ChannelMessages(sourceChannelID, 100, beforeID, "", "")
		if err != nil {
			log.Printf("❌ Failed to fetch messages from %s: %v", sourceChannelID, err)
			break
		}
		if len(msgs) == 0 {
			break
		}
		allMessages = append(allMessages, msgs...)
		beforeID = msgs[len(msgs)-1].ID
		if len(msgs) < 100 {
			break
		}
	}

	if len(allMessages) == 0 {
		return
	}

	// Reverse messages so they're in chronological order
	for i, j := 0, len(allMessages)-1; i < j; i, j = i+1, j-1 {
		allMessages[i], allMessages[j] = allMessages[j], allMessages[i]
	}

	// Load team names
	var teamA, teamB Team
	DB.First(&teamA, m.TeamAID)
	DB.First(&teamB, m.TeamBID)

	// Build transcript as plain text file
	var transcript strings.Builder
	transcript.WriteString(fmt.Sprintf("ECGL Match Transcript - Match #%d\n", m.ID))
	transcript.WriteString(fmt.Sprintf("%s vs %s\n", teamA.Name, teamB.Name))
	if m.ScheduledDate != nil {
		transcript.WriteString(fmt.Sprintf("Scheduled: %s\n", m.ScheduledDate.Format("2006-01-02 15:04 UTC")))
	}
	transcript.WriteString(fmt.Sprintf("Status: %s\n", m.Status))
	if m.TeamAScore > 0 || m.TeamBScore > 0 {
		transcript.WriteString(fmt.Sprintf("Score: %s %d - %d %s\n", teamA.Name, m.TeamAScore, m.TeamBScore, teamB.Name))
	}
	transcript.WriteString(strings.Repeat("─", 50) + "\n\n")

	for _, msg := range allMessages {
		if msg.Author == nil {
			continue
		}
		ts := msg.Timestamp.Format("2006-01-02 15:04:05")
		line := fmt.Sprintf("[%s] %s: %s\n", ts, msg.Author.Username, msg.Content)
		if len(msg.Attachments) > 0 {
			for _, att := range msg.Attachments {
				line += fmt.Sprintf("  [Attachment: %s]\n", att.URL)
			}
		}
		transcript.WriteString(line)
	}

	// Create file reader
	fileName := fmt.Sprintf("transcript-match-%d-%s-vs-%s.txt",
		m.ID,
		sanitizeChannelName(teamA.Name),
		sanitizeChannelName(teamB.Name),
	)
	fileReader := strings.NewReader(transcript.String())

	// Build embed with match info
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("📋 Match #%d — %s vs %s", m.ID, teamA.Name, teamB.Name),
		Color: 0x3498DB,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Teams", Value: fmt.Sprintf("%s vs %s", teamA.Name, teamB.Name), Inline: true},
			{Name: "Season", Value: m.Season, Inline: true},
			{Name: "Week", Value: m.Week, Inline: true},
			{Name: "Status", Value: m.Status, Inline: true},
			{Name: "Messages", Value: fmt.Sprintf("%d", len(allMessages)), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Channel closed at %s", time.Now().UTC().Format("2006-01-02 15:04 UTC")),
		},
	}

	if m.ScheduledDate != nil {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name: "Match Time", Value: fmt.Sprintf("<t:%d:F>", m.ScheduledDate.Unix()), Inline: true,
		})
	}

	// Send embed + file attachment
	_, err := s.ChannelMessageSendComplex(transcriptChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Files: []*discordgo.File{
			{
				Name:   fileName,
				Reader: fileReader,
			},
		},
	})
	if err != nil {
		log.Printf("❌ Failed to send transcript to %s: %v", transcriptChannelID, err)
	}
}

// sanitizeChannelName converts a team name to a valid Discord channel name segment.
func sanitizeChannelName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	// Remove characters not allowed in Discord channel names
	var clean strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			clean.WriteRune(r)
		}
	}
	result := clean.String()
	if result == "" {
		return "team"
	}
	return result
}

// respondInteractionEphemeral sends an ephemeral message response to an interaction.
func respondInteractionEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// 📅 SCHEDULE CHANNEL CREATION for a specific match
func scheduleMatchChannel(s *discordgo.Session, m *Match) {
	if s == nil || m == nil || m.ScheduledDate == nil {
		return
	}

	openAt := m.ScheduledDate.Add(-1 * time.Hour)
	now := time.Now().UTC()

	// If already past open time → create immediately
	if now.After(openAt) {
		createMatchChannel(s, m)
		return
	}

	delay := openAt.Sub(now)

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		<-timer.C

		createMatchChannel(s, m)
	}()
}
