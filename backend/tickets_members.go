package main

// League Mod tools on a ticket: add a single user, or a whole team, to the
// private ticket channel. Only members with DISCORD_LEAGUE_MOD_ROLE_ID can use
// them (the ticket support role does not get these). See tickets.go for the
// rest of the ticket system.

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const (
	ticketAddUserMax  = 10  // members per pick
	ticketTeamPerList = 25  // Discord select option limit
	ticketTeamLists   = 4   // select menus shown (→ up to 100 teams)
	ticketMentionMax  = 100 // AllowedMentions user cap
)

// ticketMemberAllow is what an added user/team member can do in the ticket.
const ticketMemberAllow = int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages |
	discordgo.PermissionReadMessageHistory | discordgo.PermissionAttachFiles |
	discordgo.PermissionEmbedLinks)

// ticketForModTool loads an open ticket and checks the clicker is a League
// Mod. On failure it has already replied to the interaction.
func ticketForModTool(s *discordgo.Session, i *discordgo.InteractionCreate, idStr string) (*Ticket, bool) {
	if !isInteractionMod(i) {
		ticketNotice(s, i, colorDanger, "❌ Only League Mods can add people to tickets.")
		return nil, false
	}
	t, ok := loadTicketByIDString(idStr)
	if !ok {
		ticketNotice(s, i, colorDanger, "❌ Ticket not found.")
		return nil, false
	}
	if t.Status != ticketStatusOpen {
		ticketNotice(s, i, colorWarning, "This ticket is already closed.")
		return nil, false
	}
	return t, true
}

func grantTicketAccess(s *discordgo.Session, channelID, userID string) error {
	return s.ChannelPermissionSet(channelID, userID, discordgo.PermissionOverwriteTypeMember, ticketMemberAllow, 0)
}

// announceTicketAdd posts a public V2 note in the ticket and pings who was added.
func announceTicketAdd(s *discordgo.Session, t *Ticket, text string, pinged []string) {
	if len(pinged) > ticketMentionMax {
		pinged = pinged[:ticketMentionMax]
	}
	_, err := s.ChannelMessageSendComplex(t.ChannelID, &discordgo.MessageSend{
		Flags:           v2Flag,
		AllowedMentions: &discordgo.MessageAllowedMentions{Users: pinged},
		Components:      []discordgo.MessageComponent{v2Container(colorSuccess, v2Text(text))},
	})
	if err != nil {
		log.Printf("⚠️ ticket add announcement: %v", err)
	}
}

func mentionList(ids []string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = "<@" + id + ">"
	}
	return strings.Join(parts, " ")
}

/*
----------------------------------------------------
Add User
----------------------------------------------------
*/

// "Add User" → ephemeral member picker (Discord's built-in user search).
func handleTicketAddUserButton(s *discordgo.Session, i *discordgo.InteractionCreate, idStr string) {
	t, ok := ticketForModTool(s, i, idStr)
	if !ok {
		return
	}
	minVals := 1
	ticketNotice(s, i, colorNeutral,
		fmt.Sprintf("### 👤 Add people to ticket #%04d\nPick up to %d members — they'll be able to see and reply in this channel.", t.Number, ticketAddUserMax),
		v2Row(discordgo.SelectMenu{
			MenuType:    discordgo.UserSelectMenu,
			CustomID:    fmt.Sprintf("%s%d", ticketSelUserPrefix, t.ID),
			Placeholder: "Search members…",
			MinValues:   &minVals,
			MaxValues:   ticketAddUserMax,
		}),
	)
}

// Members picked → grant each access and announce it in the ticket.
func handleTicketUsersSelected(s *discordgo.Session, i *discordgo.InteractionCreate, idStr string) {
	t, ok := ticketForModTool(s, i, idStr)
	if !ok {
		return
	}
	data := i.MessageComponentData()

	var added, skipped []string
	for _, uid := range data.Values {
		if u := data.Resolved.Users[uid]; u != nil && u.Bot {
			skipped = append(skipped, uid)
			continue
		}
		if err := grantTicketAccess(s, t.ChannelID, uid); err != nil {
			log.Printf("⚠️ ticket #%d add user %s: %v", t.Number, uid, err)
			skipped = append(skipped, uid)
			continue
		}
		added = append(added, uid)
	}

	if len(added) == 0 {
		ticketUpdateNotice(s, i, colorDanger, "❌ Nobody was added (bots can't be added, or Discord refused the change).")
		return
	}

	mod := interactionUser(i)
	announceTicketAdd(s, t, fmt.Sprintf("👤 %s %s added to this ticket by <@%s>.",
		mentionList(added), pluralWas(len(added)), mod.ID), added)

	msg := fmt.Sprintf("✅ Added %s to ticket #%04d.", mentionList(added), t.Number)
	if len(skipped) > 0 {
		msg += fmt.Sprintf("\n-# Skipped %d (bots or errors).", len(skipped))
	}
	ticketUpdateNotice(s, i, colorSuccess, msg)
	log.Printf("🎫 Ticket #%d (%s): %d user(s) added by %s", t.Number, t.League, len(added), mod.Username)
}

func pluralWas(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

/*
----------------------------------------------------
Add Team
----------------------------------------------------
*/

// ticketInvolvedTeams: teams the ticket is about (picked team, both sides of
// a picked match) plus the opener's own teams in that league.
func ticketInvolvedTeams(t *Ticket) map[uint]bool {
	involved := map[uint]bool{}
	if t.TeamID != nil {
		involved[*t.TeamID] = true
	}
	if t.MatchID != nil {
		var m Match
		if DB.Select("id", "team_a_id", "team_b_id").First(&m, *t.MatchID).Error == nil {
			involved[m.TeamAID] = true
			involved[m.TeamBID] = true
		}
	}
	for _, team := range ticketTeamsFor(t.League, t.OpenerID) {
		involved[team.ID] = true
	}
	return involved
}

// "Add Team" → ephemeral list of the league's teams, involved teams first.
func handleTicketAddTeamButton(s *discordgo.Session, i *discordgo.InteractionCreate, idStr string) {
	t, ok := ticketForModTool(s, i, idStr)
	if !ok {
		return
	}

	var teams []Team
	DB.Select("id", "name").Where("league = ?", t.League).Find(&teams)
	if len(teams) == 0 {
		ticketNotice(s, i, colorWarning, fmt.Sprintf("There are no %s teams to add.", ticketLeagueName(t.League)))
		return
	}

	// Member counts for the option descriptions.
	type countRow struct {
		TeamID uint
		N      int
	}
	var counts []countRow
	DB.Raw(`SELECT team_id, COUNT(*) AS n FROM team_members GROUP BY team_id`).Scan(&counts)
	members := map[uint]int{}
	for _, c := range counts {
		members[c.TeamID] = c.N
	}

	involved := ticketInvolvedTeams(t)
	sort.SliceStable(teams, func(a, b int) bool {
		if involved[teams[a].ID] != involved[teams[b].ID] {
			return involved[teams[a].ID]
		}
		return strings.ToLower(teams[a].Name) < strings.ToLower(teams[b].Name)
	})

	options := make([]discordgo.SelectMenuOption, 0, len(teams))
	for _, team := range teams {
		desc := fmt.Sprintf("%d member(s)", members[team.ID])
		emoji := "🛡️"
		if involved[team.ID] {
			desc = "Involved in this ticket · " + desc
			emoji = "⭐"
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:       truncateRunes(team.Name, 100),
			Description: desc,
			Value:       strconv.FormatUint(uint64(team.ID), 10),
			Emoji:       &discordgo.ComponentEmoji{Name: emoji},
		})
	}

	// Discord caps a select at 25 options, so long team lists are split.
	comps := []discordgo.MessageComponent{}
	shown := 0
	for idx := 0; idx < ticketTeamLists && shown < len(options); idx++ {
		end := shown + ticketTeamPerList
		if end > len(options) {
			end = len(options)
		}
		chunk := options[shown:end]
		placeholder := "Pick a team"
		if len(options) > ticketTeamPerList {
			placeholder = fmt.Sprintf("Teams %s – %s", firstRune(chunk[0].Label), firstRune(chunk[len(chunk)-1].Label))
			if idx == 0 && len(involved) > 0 {
				placeholder = "⭐ Involved teams first — pick a team"
			}
		}
		comps = append(comps, v2Row(discordgo.SelectMenu{
			MenuType:    discordgo.StringSelectMenu,
			CustomID:    fmt.Sprintf("%s%d_%d", ticketSelTeamPrefix, t.ID, idx),
			Placeholder: placeholder,
			Options:     chunk,
		}))
		shown = end
	}

	text := fmt.Sprintf("### 🛡️ Add a team to ticket #%04d\nEveryone on the team you pick will be able to see and reply in this channel, and gets pinged.", t.Number)
	if shown < len(options) {
		text += fmt.Sprintf("\n-# Showing %d of %d teams.", shown, len(options))
	}
	ticketNotice(s, i, colorNeutral, text, comps...)
}

func firstRune(s string) string {
	for _, r := range s {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// Team picked → grant every member access and announce it.
func handleTicketTeamSelected(s *discordgo.Session, i *discordgo.InteractionCreate, idStr string) {
	t, ok := ticketForModTool(s, i, idStr)
	if !ok {
		return
	}
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	teamID, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil {
		ticketUpdateNotice(s, i, colorDanger, "❌ Invalid team.")
		return
	}

	var team Team
	if DB.Select("id", "name", "league").First(&team, uint(teamID)).Error != nil || normalizeLeague(team.League) != t.League {
		ticketUpdateNotice(s, i, colorDanger, "❌ That team isn't in this ticket's league.")
		return
	}

	var roster []TeamMember
	DB.Where("team_id = ?", team.ID).Find(&roster)
	if len(roster) == 0 {
		ticketUpdateNotice(s, i, colorWarning, fmt.Sprintf("**%s** has no members to add.", team.Name))
		return
	}

	var added []string
	failed := 0
	for _, m := range roster {
		uid := strconv.FormatInt(m.PlayerID, 10)
		if err := grantTicketAccess(s, t.ChannelID, uid); err != nil {
			log.Printf("⚠️ ticket #%d add %s member %s: %v", t.Number, team.Name, uid, err)
			failed++
			continue
		}
		added = append(added, uid)
	}

	if len(added) == 0 {
		ticketUpdateNotice(s, i, colorDanger, fmt.Sprintf("❌ Couldn't add anyone from **%s** — Discord refused the change.", team.Name))
		return
	}

	mod := interactionUser(i)
	announceTicketAdd(s, t, fmt.Sprintf("🛡️ **%s** was added to this ticket by <@%s>.\n%s",
		team.Name, mod.ID, mentionList(added)), added)

	msg := fmt.Sprintf("✅ Added **%s** (%d member(s)) to ticket #%04d.", team.Name, len(added), t.Number)
	if failed > 0 {
		msg += fmt.Sprintf("\n-# %d member(s) couldn't be added (they may have left the server).", failed)
	}
	ticketUpdateNotice(s, i, colorSuccess, msg)
	log.Printf("🎫 Ticket #%d (%s): team %s (%d) added by %s", t.Number, t.League, team.Name, len(added), mod.Username)
}
