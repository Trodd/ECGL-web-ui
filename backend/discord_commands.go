package main

import (
	"fmt"
	"log"
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

	if data.Name != "team" || len(data.Options) == 0 {
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
