package main

import (
	"fmt"
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
