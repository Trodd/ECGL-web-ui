package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

/*
====================================================
PREFIX COMMAND REGISTRATION
====================================================
*/

const CommandPrefix = "!"

const prefixCommandCooldown = 30 * time.Second

var prefixCooldown = struct {
	mu   sync.Mutex
	last map[string]time.Time
}{
	last: map[string]time.Time{},
}

func RegisterPrefixCommands(dg *discordgo.Session) {
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// 🧱 Crash prevention
		if m == nil || m.Author == nil || m.Author.Bot {
			return
		}

		content := strings.TrimSpace(m.Content)
		if !strings.HasPrefix(content, CommandPrefix) {
			return
		}

		parts := strings.Fields(content)
		if len(parts) == 0 {
			return
		}

		cmd := strings.ToLower(strings.TrimPrefix(parts[0], CommandPrefix))
		args := []string{}
		if len(parts) > 1 {
			args = parts[1:]
		}

		// Only enforce gating/cooldown for known commands
		switch cmd {
		case "team", "cap":
			// ok
		default:
			return
		}

		allowed, denyMsg := canUsePrefixCommands(m.Author.ID)
		if !allowed {
			// For eligibility denials (unregistered, League Sub, Banned, etc.): delete the command and stay silent.
			_ = denyMsg // reserved for future UX tweaks
			silentDeny(s, m)
			return
		}

		if remaining, ok := takePrefixCooldown(m.Author.ID); !ok {
			denyWithMessage(s, m, fmt.Sprintf("You're on cooldown — try again in %ds.", int(remaining.Seconds())+1))
			return
		}

		switch cmd {
		case "team":
			handlePingTeam(s, m, args)

		case "cap":
			handlePingTeamCaptains(s, m, args)
		}
	})
}

/*
====================================================
HELPERS
====================================================
*/

func canUsePrefixCommands(discordID string) (bool, string) {
	p, ok := loadPlayerByDiscordID(discordID)
	if !ok {
		return false, "Only registered Players can use ECGL ! commands. Register on the site first."
	}

	// Basic registration completeness check
	if strings.TrimSpace(p.Role) == "" || strings.TrimSpace(p.Device) == "" || strings.TrimSpace(p.Timezone) == "" {
		return false, "Only registered Players can use ECGL ! commands. Register on the site first."
	}

	// Explicit blocks
	if strings.EqualFold(p.Role, "Banned") {
		return false, "You are banned and cannot use ECGL ! commands."
	}
	if strings.EqualFold(p.Role, "League Sub") {
		return false, "League Subs cannot use ECGL ! commands."
	}

	// Only Players may use prefix commands
	if !strings.EqualFold(p.Role, "Player") {
		return false, "Only registered Players can use ECGL ! commands."
	}

	return true, ""
}

func loadPlayerByDiscordID(discordID string) (Player, bool) {
	if discordID == "" {
		return Player{}, false
	}
	id, err := strconv.ParseInt(discordID, 10, 64)
	if err != nil {
		return Player{}, false
	}
	var p Player
	if err := DB.First(&p, id).Error; err != nil {
		return Player{}, false
	}
	return p, true
}

func takePrefixCooldown(userID string) (time.Duration, bool) {
	if userID == "" {
		return 0, true
	}
	now := time.Now()

	prefixCooldown.mu.Lock()
	defer prefixCooldown.mu.Unlock()

	if last, ok := prefixCooldown.last[userID]; ok {
		if since := now.Sub(last); since < prefixCommandCooldown {
			return prefixCommandCooldown - since, false
		}
	}

	prefixCooldown.last[userID] = now
	return 0, true
}

// Resolve team by ID or name (case-insensitive)
func resolveTeam(arg string) (*Team, error) {
	var team Team

	// Try numeric ID
	if id, err := strconv.Atoi(arg); err == nil {
		if err := DB.First(&team, id).Error; err == nil {
			return &team, nil
		}
	}

	// Try name
	if err := DB.
		Where("LOWER(name) = LOWER(?)", arg).
		First(&team).Error; err == nil {
		return &team, nil
	}

	return nil, fmt.Errorf("team not found")
}

func silentDeny(s *discordgo.Session, m *discordgo.MessageCreate) {
	_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
}

func denyWithMessage(s *discordgo.Session, m *discordgo.MessageCreate, msg string) {
	// Best-effort cleanup
	_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
	if strings.TrimSpace(msg) == "" {
		return
	}
	_, _ = s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> %s", m.Author.ID, msg))
}

/*
====================================================
!pingteam — ping all members of ONE team
====================================================
*/

func handlePingTeam(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) == 0 {
		return
	}

	// Try full args as team name first
	argStr := strings.Join(args, " ")
	team, err := resolveTeam(argStr)

	message := ""

	if err != nil {
		// fallback: first token = team, rest = message
		team, err = resolveTeam(args[0])
		if err != nil {
			silentDeny(s, m)
			return
		}
		if len(args) > 1 {
			message = strings.Join(args[1:], " ")
		}
	}

	var members []TeamMember
	DB.Where("team_id = ?", team.ID).Find(&members)

	if len(members) == 0 {
		silentDeny(s, m)
		return
	}

	// 🧹 Delete the command message
	_ = s.ChannelMessageDelete(m.ChannelID, m.ID)

	requester := fmt.Sprintf("<@%s>", m.Author.ID)

	// Build ping message
	out := fmt.Sprintf("📣 **%s** — pinged by %s\n", team.Name, requester)

	for _, tm := range members {
		out += fmt.Sprintf("<@%d> ", tm.PlayerID)
	}

	if strings.TrimSpace(message) != "" {
		out += "\n\n💬 " + message
	}

	s.ChannelMessageSend(m.ChannelID, out)
}

/*
====================================================
!pingcaptains — ping captain + co-captain of ONE team
====================================================
*/

func handlePingTeamCaptains(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) == 0 {
		return
	}

	argStr := strings.Join(args, " ")
	team, err := resolveTeam(argStr)

	message := ""

	if err != nil {
		team, err = resolveTeam(args[0])
		if err != nil {
			silentDeny(s, m)
			return
		}
		if len(args) > 1 {
			message = strings.Join(args[1:], " ")
		}
	}

	var captains []TeamMember
	DB.Where(
		"team_id = ? AND (role = 'Captain' OR role = 'Co-Captain')",
		team.ID,
	).Find(&captains)

	if len(captains) == 0 {
		silentDeny(s, m)
		return
	}

	// 🧹 Delete the command message
	_ = s.ChannelMessageDelete(m.ChannelID, m.ID)

	requester := fmt.Sprintf("<@%s>", m.Author.ID)

	out := fmt.Sprintf("🚨 **%s captains** — %s\n", team.Name, requester)

	for _, c := range captains {
		out += fmt.Sprintf("<@%d> ", c.PlayerID)
	}

	if strings.TrimSpace(message) != "" {
		out += "\n\n💬 " + message
	}

	s.ChannelMessageSend(m.ChannelID, out)
}

/*
====================================================
SLASH COMMANDS — /team ping & /team captains
====================================================
*/

func RegisterSlashHandlers(dg *discordgo.Session) {
	dg.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			handleSlashCommand(s, i)
		case discordgo.InteractionApplicationCommandAutocomplete:
			handleAutocomplete(s, i)
		}
	})
}

func RegisterSlashCommands(dg *discordgo.Session) {
	guildID := getEnv("DISCORD_GUILD_ID", "")
	if guildID == "" {
		log.Printf("⚠️ DISCORD_GUILD_ID not set — skipping slash command registration")
		return
	}

	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "team",
			Description: "Team management commands",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "ping",
					Description: "Ping all members of a team",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:         "team",
							Description:  "Team name or ID",
							Type:         discordgo.ApplicationCommandOptionString,
							Required:     true,
							Autocomplete: true,
						},
						{
							Name:        "message",
							Description: "Optional message to include",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    false,
						},
					},
				},
				{
					Name:        "captains",
					Description: "Ping the captain and co-captain of a team",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:         "team",
							Description:  "Team name or ID",
							Type:         discordgo.ApplicationCommandOptionString,
							Required:     true,
							Autocomplete: true,
						},
						{
							Name:        "message",
							Description: "Optional message to include",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    false,
						},
					},
				},
			},
		},
		{
			Name:        "addsub",
			Description: "Add a League Sub to the current match channel",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "player",
					Description: "The League Sub to add to this channel",
					Type:        discordgo.ApplicationCommandOptionUser,
					Required:    true,
				},
				{
					Name:         "team",
					Description:  "Which team the sub is playing for",
					Type:         discordgo.ApplicationCommandOptionString,
					Required:     true,
					Autocomplete: true,
				},
			},
		},
	}

	currentNames := map[string]bool{}
	for _, c := range commands {
		currentNames[c.Name] = true
	}

	botID := dg.State.User.ID

	// ── Clean up stale GLOBAL commands (from old/different systems) ──
	globalCmds, err := dg.ApplicationCommands(botID, "")
	if err != nil {
		log.Printf("⚠️ Could not fetch global commands for cleanup: %v", err)
	} else {
		for _, cmd := range globalCmds {
			if err := dg.ApplicationCommandDelete(botID, "", cmd.ID); err != nil {
				log.Printf("⚠️ Failed to delete stale global command /%s: %v", cmd.Name, err)
			} else {
				log.Printf("🧹 Deleted stale global command: /%s", cmd.Name)
			}
		}
	}

	// ── Fetch & clean up stale GUILD commands, then register current ones ──
	existing, err := dg.ApplicationCommands(botID, guildID)
	if err != nil {
		log.Printf("⚠️ Could not fetch guild commands: %v", err)
		return
	}

	existingByName := map[string]*discordgo.ApplicationCommand{}
	for _, cmd := range existing {
		existingByName[cmd.Name] = cmd
	}

	// Delete stale guild commands
	for _, cmd := range existing {
		if !currentNames[cmd.Name] {
			if err := dg.ApplicationCommandDelete(botID, guildID, cmd.ID); err != nil {
				log.Printf("⚠️ Failed to delete stale guild command /%s: %v", cmd.Name, err)
			} else {
				log.Printf("🧹 Deleted stale guild command: /%s", cmd.Name)
			}
		}
	}

	// Register/update current commands
	for _, cmd := range commands {
		if existingCmd, ok := existingByName[cmd.Name]; ok {
			_, err := dg.ApplicationCommandEdit(botID, guildID, existingCmd.ID, cmd)
			if err != nil {
				log.Printf("❌ Failed to update /%s: %v", cmd.Name, err)
			} else {
				log.Printf("✅ Updated slash command: /%s", cmd.Name)
			}
		} else {
			_, err := dg.ApplicationCommandCreate(botID, guildID, cmd)
			if err != nil {
				log.Printf("❌ Failed to register /%s: %v", cmd.Name, err)
			} else {
				log.Printf("✅ Registered slash command: /%s", cmd.Name)
			}
		}
	}
}

func handleAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return
	}

	var focused *discordgo.ApplicationCommandInteractionDataOption
	var findFocused func(opts []*discordgo.ApplicationCommandInteractionDataOption)
	findFocused = func(opts []*discordgo.ApplicationCommandInteractionDataOption) {
		for _, opt := range opts {
			if opt.Focused {
				focused = opt
				return
			}
			if len(opt.Options) > 0 {
				findFocused(opt.Options)
			}
		}
	}
	findFocused(data.Options)

	if focused == nil || focused.Name != "team" {
		return
	}

	// ── /addsub team autocomplete: show the two teams in this match channel ──
	if data.Name == "addsub" {
		var match Match
		if err := DB.Where("discord_channel_id = ?", i.ChannelID).First(&match).Error; err != nil {
			return // not a match channel — no choices
		}

		var teamA, teamB Team
		DB.First(&teamA, match.TeamAID)
		DB.First(&teamB, match.TeamBID)

		choices := []*discordgo.ApplicationCommandOptionChoice{
			{Name: teamA.Name, Value: teamA.Name},
			{Name: teamB.Name, Value: teamB.Name},
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionApplicationCommandAutocompleteResult,
			Data: &discordgo.InteractionResponseData{
				Choices: choices,
			},
		})
		return
	}

	// ── /team autocomplete: search all teams ──
	query := strings.ToLower(strings.TrimSpace(focused.StringValue()))

	var teams []Team
	DB.Where("LOWER(name) LIKE ?", "%"+query+"%").Order("name ASC").Limit(25).Find(&teams)

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(teams))
	for _, t := range teams {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  t.Name,
			Value: t.Name,
		})
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	})
}

func handleSlashCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()

	switch data.Name {
	case "team":
		handleTeamSlashCommand(s, i, data)
	case "addsub":
		handleAddSubSlashCommand(s, i, data)
	}
}

func handleTeamSlashCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	allowed, _ := canUsePrefixCommands(i.Member.User.ID)
	if !allowed {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Only registered Players can use ECGL commands. Register on the site first.",
				Flags:   1 << 6,
			},
		})
		return
	}

	if len(data.Options) == 0 {
		return
	}

	subCmd := data.Options[0]
	var teamOpt, msgOpt *discordgo.ApplicationCommandInteractionDataOption
	for _, opt := range subCmd.Options {
		switch opt.Name {
		case "team":
			teamOpt = opt
		case "message":
			msgOpt = opt
		}
	}

	if teamOpt == nil {
		return
	}

	team, err := resolveTeam(teamOpt.StringValue())
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ Team not found: **%s**", teamOpt.StringValue()),
				Flags:   1 << 6,
			},
		})
		return
	}

	message := ""
	if msgOpt != nil {
		message = msgOpt.StringValue()
	}
	requester := fmt.Sprintf("<@%s>", i.Member.User.ID)

	switch subCmd.Name {
	case "ping":
		var members []TeamMember
		DB.Where("team_id = ?", team.ID).Find(&members)

		if len(members) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("❌ **%s** has no members.", team.Name),
					Flags:   1 << 6,
				},
			})
			return
		}

		out := fmt.Sprintf("📣 **%s** — pinged by %s\n", team.Name, requester)
		for _, tm := range members {
			out += fmt.Sprintf("<@%d> ", tm.PlayerID)
		}
		if strings.TrimSpace(message) != "" {
			out += "\n\n💬 " + message
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: out,
			},
		})

	case "captains":
		var captains []TeamMember
		DB.Where("team_id = ? AND (role = 'Captain' OR role = 'Co-Captain')", team.ID).Find(&captains)

		if len(captains) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("❌ **%s** has no captains.", team.Name),
					Flags:   1 << 6,
				},
			})
			return
		}

		out := fmt.Sprintf("🚨 **%s captains** — %s\n", team.Name, requester)
		for _, c := range captains {
			out += fmt.Sprintf("<@%d> ", c.PlayerID)
		}
		if strings.TrimSpace(message) != "" {
			out += "\n\n💬 " + message
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: out,
			},
		})
	}
}

func handleAddSubSlashCommand(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	// ── Must be used in a match channel ──
	channelID := i.ChannelID
	var match Match
	if err := DB.Where("discord_channel_id = ?", channelID).First(&match).Error; err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ This command can only be used in a match channel.",
				Flags:   1 << 6,
			},
		})
		return
	}

	// ── Extract options ──
	var targetUser *discordgo.User
	var teamChoice string
	for _, opt := range data.Options {
		switch opt.Name {
		case "player":
			targetUser = opt.UserValue(s)
		case "team":
			teamChoice = opt.StringValue()
		}
	}

	if targetUser == nil || teamChoice == "" {
		return
	}

	// ── Load teams for the match ──
	var teamA, teamB Team
	DB.First(&teamA, match.TeamAID)
	DB.First(&teamB, match.TeamBID)

	// Resolve the team name (from autocomplete) back to "a" or "b"
	var teamSide string
	teamName := teamChoice
	if strings.EqualFold(teamChoice, teamA.Name) || teamChoice == strconv.FormatUint(uint64(teamA.ID), 10) {
		teamSide = "a"
		teamName = teamA.Name
	} else if strings.EqualFold(teamChoice, teamB.Name) || teamChoice == strconv.FormatUint(uint64(teamB.ID), 10) {
		teamSide = "b"
		teamName = teamB.Name
	} else {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ **%s** is not one of the teams in this match.", teamChoice),
				Flags:   1 << 6,
			},
		})
		return
	}

	// ── Authorization: League Mod OR captain/co-captain of the chosen team ──
	callerID := i.Member.User.ID
	isMod := isInteractionMod(i)

	if !isMod {
		// Check if caller is captain/co-captain of the chosen team
		chosenTeamID := match.TeamAID
		if teamSide == "b" {
			chosenTeamID = match.TeamBID
		}

		discordID, err := strconv.ParseInt(callerID, 10, 64)
		if err != nil {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "❌ Could not verify your identity.",
					Flags:   1 << 6,
				},
			})
			return
		}

		var tm TeamMember
		if err := DB.Where("player_id = ? AND team_id = ? AND role IN ?",
			discordID, chosenTeamID, []string{"Captain", "Co-Captain"}).First(&tm).Error; err != nil {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("❌ You must be a Captain or Co-Captain of **%s** to add a sub for them.", teamName),
					Flags:   1 << 6,
				},
			})
			return
		}
	}

	// ── Verify the target has the League Sub Discord role ──
	subRoleID := os.Getenv("DISCORD_LEAGUE_SUB_ROLE_ID")
	if subRoleID == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ DISCORD_LEAGUE_SUB_ROLE_ID is not configured.",
				Flags:   1 << 6,
			},
		})
		return
	}

	member, err := s.GuildMember(os.Getenv("DISCORD_GUILD_ID"), targetUser.ID)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ Could not fetch Discord member info for <@%s>.", targetUser.ID),
				Flags:   1 << 6,
			},
		})
		return
	}

	hasSubRole := false
	for _, roleID := range member.Roles {
		if roleID == subRoleID {
			hasSubRole = true
			break
		}
	}

	if !hasSubRole {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ <@%s> does not have the League Sub role. Only League Subs can be added with this command.", targetUser.ID),
				Flags:   1 << 6,
			},
		})
		return
	}

	// ── Add the sub to text channel + only the chosen team's voice channel ──
	if err := addSubToMatchChannels(s, &match, targetUser.ID, teamSide); err != nil {
		log.Printf("❌ Failed to add sub %s to match %d channels: %v", targetUser.ID, match.ID, err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ Failed to add <@%s> to some channels. Check logs for details.", targetUser.ID),
				Flags:   1 << 6,
			},
		})
		return
	}

	// ── Success ──
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("✅ <@%s> has been added to this match channel and the **%s** voice channel.", targetUser.ID, teamName),
		},
	})

	log.Printf("✅ League Sub %s added to match %d (team %s) by %s", targetUser.ID, match.ID, teamSide, i.Member.User.Username)
}
