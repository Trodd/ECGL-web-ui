package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

/*
====================================================
PREFIX COMMAND REGISTRATION
====================================================
*/

const CommandPrefix = "!"

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

// Registered ECGL player check
func isRegisteredUser(discordID string) bool {
	if discordID == "" {
		return false
	}

	id, err := strconv.ParseInt(discordID, 10, 64)
	if err != nil {
		return false
	}

	var p Player
	if err := DB.First(&p, id).Error; err != nil {
		return false
	}

	// Fully registered
	return p.Role != "" && p.Device != "" && p.Timezone != ""
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

/*
====================================================
!pingteam — ping all members of ONE team
====================================================
*/

func handlePingTeam(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	// 🧱 Safety
	if !isRegisteredUser(m.Author.ID) {
		silentDeny(s, m)
		return
	}

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
	// 🧱 Safety
	if !isRegisteredUser(m.Author.ID) {
		silentDeny(s, m)
		return
	}

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
