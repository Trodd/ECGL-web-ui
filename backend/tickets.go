package main

// Per-league Discord support tickets.
//
// Each league has its own panel channel (where the issue-type buttons live),
// its own category (where private ticket channels are created) and an optional
// transcript channel / support role. All of it is configured from the League
// Settings tab, using the usual `_ROOKIE` suffix for the Rookie League:
//
//	TICKETS_ENABLED[_ROOKIE]               "true" to accept new tickets
//	TICKET_PANEL_CHANNEL_ID[_ROOKIE]       channel that holds the Open Ticket panel
//	TICKET_CATEGORY_ID[_ROOKIE]            category new ticket channels go in
//	TICKET_TRANSCRIPT_CHANNEL_ID[_ROOKIE]  optional; falls back to DISCORD_TRANSCRIPT_CHANNEL_ID
//	TICKET_SUPPORT_ROLE_ID[_ROOKIE]        optional extra role with access (League Mods always have it)
//
// Flow: issue button (Player / Team / Match / Other) → modal → private channel for
// the opener + staff → Close Ticket button → transcript posted, channel deleted.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	ticketStatusOpen   = "open"
	ticketStatusClosed = "closed"

	ticketOpenPrefix         = "ticket_open_"         // + league [+ "_" + type]
	ticketModalPrefix        = "ticket_modal_"        // + league + "_" + type [+ "_" + match/team id]
	ticketPickPrefix         = "ticket_pick_"         // + league + "_" + type (match/team dropdown)
	ticketManualPrefix       = "ticket_manual_"       // + league + "_" + type ("describe it myself")
	ticketClosePrefix        = "ticket_close_"        // + ticket id
	ticketConfirmClosePrefix = "ticket_confirmclose_" // + ticket id
	ticketCancelClose        = "ticket_cancelclose"

	// League Mod tools on the ticket card.
	ticketAddUserPrefix = "ticket_adduser_" // + ticket id (button)
	ticketAddTeamPrefix = "ticket_addteam_" // + ticket id (button)
	ticketSelUserPrefix = "ticket_seluser_" // + ticket id (member picker)
	ticketSelTeamPrefix = "ticket_selteam_" // + ticket id + "_" + list index (team picker)
)

// ticketType is one kind of problem a member can open a ticket for. Each gets
// its own panel button and its own modal wording. Add a row here to add a type
// (max 5 — one Discord button row).
type ticketType struct {
	Slug         string // used in custom IDs and channel names; no underscores
	Label        string // button + ticket label
	Emoji        string
	Blurb        string // one-line description on the panel
	Style        discordgo.ButtonStyle
	SubjectLabel string // first modal field (becomes the ticket subject)
	SubjectHint  string
	DetailsHint  string
	Color        int
}

var ticketTypes = []ticketType{
	{
		Slug: "player", Label: "Player Issue", Emoji: "👤", Style: discordgo.PrimaryButton,
		Blurb:        "report a player, conduct, cheating, smurfing",
		SubjectLabel: "Player(s) involved",
		SubjectHint:  "Discord or in-game name(s)",
		DetailsHint:  "What happened, and when? You can post clips/screenshots in the ticket.",
		Color:        0x5865F2,
	},
	{
		Slug: "team", Label: "Team Issue", Emoji: "🛡️", Style: discordgo.PrimaryButton,
		Blurb:        "rosters, captains, team name/logo, disbanding",
		SubjectLabel: "Team name",
		SubjectHint:  "Which team is this about?",
		DetailsHint:  "Roster changes, captain problems, team name/logo, disbanding…",
		Color:        0x57F287,
	},
	{
		Slug: "match", Label: "Match Issue", Emoji: "⚔️", Style: discordgo.PrimaryButton,
		Blurb:        "score disputes, no-shows, scheduling, disconnects",
		SubjectLabel: "Match (teams & week)",
		SubjectHint:  "e.g. Team A vs Team B — Week 3",
		DetailsHint:  "Score dispute, no-show, scheduling, disconnects, rule breaks…",
		Color:        0xFEE75C,
	},
	{
		Slug: "general", Label: "Other", Emoji: "❓", Style: discordgo.SecondaryButton,
		Blurb:        "anything else",
		SubjectLabel: "Subject",
		SubjectHint:  "What do you need help with?",
		DetailsHint:  "Tell the League Mods what's going on.",
		Color:        0x99AAB5,
	},
}

// ticketTypeBySlug returns the matching type, falling back to "general" (also
// covers panels posted before ticket types existed).
func ticketTypeBySlug(slug string) ticketType {
	for _, t := range ticketTypes {
		if t.Slug == slug {
			return t
		}
	}
	return ticketTypes[len(ticketTypes)-1]
}

// splitLeagueType parses "<league>_<type>[_<extra>]" from a custom ID. Type is
// optional (old panels); extra carries e.g. the picked match ID.
func splitLeagueType(rest string) (string, ticketType, string) {
	parts := strings.SplitN(rest, "_", 3)
	league, slug, extra := parts[0], "", ""
	if len(parts) > 1 {
		slug = parts[1]
	}
	if len(parts) > 2 {
		extra = parts[2]
	}
	return normalizeLeague(league), ticketTypeBySlug(slug), extra
}

// Ticket is one support ticket (one private Discord channel).
type Ticket struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	League      string     `gorm:"index;default:'main'" json:"league"`
	Type        string     `gorm:"index;default:'general'" json:"type"`
	MatchID     *uint      `gorm:"index" json:"match_id"` // set when a match was picked from the list
	TeamID      *uint      `gorm:"index" json:"team_id"`  // set when a team was picked from the list
	Number      int        `json:"number"`
	ChannelID   string     `gorm:"index" json:"channel_id"`
	OpenerID    string     `gorm:"index" json:"opener_id"`
	OpenerName  string     `json:"opener_name"`
	Subject     string     `json:"subject"`
	Description string     `gorm:"type:text" json:"description"`
	Status      string     `gorm:"index;default:'open'" json:"status"`
	ClosedBy    string     `json:"closed_by"`
	CreatedAt   time.Time  `json:"created_at"`
	ClosedAt    *time.Time `json:"closed_at"`
}

// Serialises ticket creation so numbering and the one-open-ticket rule hold.
var ticketMu sync.Mutex

type ticketConfig struct {
	Enabled             bool
	PanelChannelID      string
	CategoryID          string
	TranscriptChannelID string
	SupportRoleID       string
}

// leagueEnv reads a per-league env key: BASE for Varsity, BASE_ROOKIE for Rookie.
func leagueEnv(base, league string) string {
	if normalizeLeague(league) == LeagueRookie {
		return strings.TrimSpace(os.Getenv(base + "_ROOKIE"))
	}
	return strings.TrimSpace(os.Getenv(base))
}

func loadTicketConfig(league string) ticketConfig {
	cfg := ticketConfig{
		Enabled:             leagueEnv("TICKETS_ENABLED", league) == "true",
		PanelChannelID:      leagueEnv("TICKET_PANEL_CHANNEL_ID", league),
		CategoryID:          leagueEnv("TICKET_CATEGORY_ID", league),
		TranscriptChannelID: leagueEnv("TICKET_TRANSCRIPT_CHANNEL_ID", league),
		SupportRoleID:       leagueEnv("TICKET_SUPPORT_ROLE_ID", league),
	}
	if cfg.TranscriptChannelID == "" {
		cfg.TranscriptChannelID = strings.TrimSpace(os.Getenv("DISCORD_TRANSCRIPT_CHANNEL_ID"))
	}
	return cfg
}

func ticketLeagueName(league string) string {
	if normalizeLeague(league) == LeagueRookie {
		return "Rookie League"
	}
	return "Varsity League"
}

/*
====================================================
COMPONENTS V2 HELPERS
====================================================
Every ticket message uses Discord's Components V2 layout (containers,
sections, text displays). V2 messages can't carry `content` or embeds —
all text lives in TextDisplay components, and attachments must be
referenced by a File component to show.
*/

const (
	v2Flag         = discordgo.MessageFlagsIsComponentsV2
	v2Ephemeral    = discordgo.MessageFlagsIsComponentsV2 | discordgo.MessageFlagsEphemeral
	colorNeutral   = 0x5865F2
	colorSuccess   = 0x57F287
	colorDanger    = 0xED4245
	colorWarning   = 0xFEE75C
	colorTicketLog = 0x3498DB
)

func v2Text(s string) discordgo.MessageComponent { return discordgo.TextDisplay{Content: s} }

func v2Divider() discordgo.MessageComponent {
	on, size := true, discordgo.SeparatorSpacingSizeSmall
	return discordgo.Separator{Divider: &on, Spacing: &size}
}

func v2Container(color int, comps ...discordgo.MessageComponent) discordgo.MessageComponent {
	c := color
	return discordgo.Container{AccentColor: &c, Components: comps}
}

func v2Row(comps ...discordgo.MessageComponent) discordgo.MessageComponent {
	return discordgo.ActionsRow{Components: comps}
}

// v2Section: text on the left, a button or thumbnail on the right.
func v2Section(accessory discordgo.MessageComponent, texts ...string) discordgo.MessageComponent {
	comps := make([]discordgo.MessageComponent, 0, len(texts))
	for _, t := range texts {
		comps = append(comps, v2Text(t))
	}
	return discordgo.Section{Components: comps, Accessory: accessory}
}

// ticketNotice replies to an interaction with a small ephemeral V2 card.
// Extra components (e.g. a button row) go inside the card under the text.
func ticketNotice(s *discordgo.Session, i *discordgo.InteractionCreate, color int, text string, extra ...discordgo.MessageComponent) {
	comps := append([]discordgo.MessageComponent{v2Text(text)}, extra...)
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:      v2Ephemeral,
			Components: []discordgo.MessageComponent{v2Container(color, comps...)},
		},
	})
	if err != nil {
		log.Printf("⚠️ ticket notice: %v", err)
	}
}

// ticketUpdateNotice replaces the clicked (ephemeral) message with a V2 card.
func ticketUpdateNotice(s *discordgo.Session, i *discordgo.InteractionCreate, color int, text string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Flags:      v2Flag,
			Components: []discordgo.MessageComponent{v2Container(color, v2Text(text))},
		},
	})
}

// ticketFollowup sends an ephemeral V2 follow-up to a deferred interaction,
// falling back to plain text if Discord rejects the V2 payload.
func ticketFollowup(s *discordgo.Session, i *discordgo.InteractionCreate, color int, text string, extra ...discordgo.MessageComponent) {
	comps := append([]discordgo.MessageComponent{v2Text(text)}, extra...)
	_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Flags:      v2Ephemeral,
		Components: []discordgo.MessageComponent{v2Container(color, comps...)},
	})
	if err != nil {
		log.Printf("⚠️ ticket V2 followup failed, sending plain: %v", err)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: text,
			Flags:   discordgo.MessageFlagsEphemeral,
		})
	}
}

// componentText flattens the text of V2 components (for transcripts).
func componentText(comps []discordgo.MessageComponent) string {
	var parts []string
	var walk func([]discordgo.MessageComponent)
	walk = func(cs []discordgo.MessageComponent) {
		for _, c := range cs {
			switch v := c.(type) {
			case *discordgo.TextDisplay:
				parts = append(parts, v.Content)
			case *discordgo.Section:
				walk(v.Components)
			case *discordgo.Container:
				walk(v.Components)
			}
		}
	}
	walk(comps)
	return strings.Join(parts, " | ")
}

/*
====================================================
DISCORD INTERACTIONS
====================================================
*/

// RegisterTicketHandlers wires the ticket buttons, modal and channel cleanup.
func RegisterTicketHandlers(dg *discordgo.Session) {
	dg.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.Type {
		case discordgo.InteractionMessageComponent:
			id := i.MessageComponentData().CustomID
			switch {
			case strings.HasPrefix(id, ticketOpenPrefix):
				league, tt, _ := splitLeagueType(strings.TrimPrefix(id, ticketOpenPrefix))
				handleTicketOpenButton(s, i, league, tt)
			case strings.HasPrefix(id, ticketPickPrefix):
				league, tt, _ := splitLeagueType(strings.TrimPrefix(id, ticketPickPrefix))
				handleTicketPicked(s, i, league, tt)
			case strings.HasPrefix(id, ticketManualPrefix):
				league, tt, _ := splitLeagueType(strings.TrimPrefix(id, ticketManualPrefix))
				respondTicketModal(s, i, league, tt, nil)
			case strings.HasPrefix(id, ticketClosePrefix):
				handleTicketCloseButton(s, i, strings.TrimPrefix(id, ticketClosePrefix))
			case strings.HasPrefix(id, ticketConfirmClosePrefix):
				handleTicketConfirmClose(s, i, strings.TrimPrefix(id, ticketConfirmClosePrefix))
			case strings.HasPrefix(id, ticketAddUserPrefix):
				handleTicketAddUserButton(s, i, strings.TrimPrefix(id, ticketAddUserPrefix))
			case strings.HasPrefix(id, ticketAddTeamPrefix):
				handleTicketAddTeamButton(s, i, strings.TrimPrefix(id, ticketAddTeamPrefix))
			case strings.HasPrefix(id, ticketSelUserPrefix):
				handleTicketUsersSelected(s, i, strings.TrimPrefix(id, ticketSelUserPrefix))
			case strings.HasPrefix(id, ticketSelTeamPrefix):
				ticketID, _, _ := strings.Cut(strings.TrimPrefix(id, ticketSelTeamPrefix), "_")
				handleTicketTeamSelected(s, i, ticketID)
			case id == ticketCancelClose:
				ticketUpdateNotice(s, i, colorNeutral, "↩️ Ticket close cancelled.")
			}
		case discordgo.InteractionModalSubmit:
			id := i.ModalSubmitData().CustomID
			if strings.HasPrefix(id, ticketModalPrefix) {
				league, tt, extra := splitLeagueType(strings.TrimPrefix(id, ticketModalPrefix))
				handleTicketModalSubmit(s, i, league, tt, extra)
			}
		}
	})

	// A ticket channel deleted by hand in Discord closes its ticket so the
	// opener isn't stuck with a phantom "you already have a ticket".
	dg.AddHandler(func(s *discordgo.Session, c *discordgo.ChannelDelete) {
		if c == nil || c.Channel == nil {
			return
		}
		now := time.Now()
		DB.Model(&Ticket{}).
			Where("channel_id = ? AND status = ?", c.ID, ticketStatusOpen).
			Updates(map[string]any{"status": ticketStatusClosed, "closed_at": &now, "closed_by": "channel deleted"})
	})
}

func interactionUser(i *discordgo.InteractionCreate) *discordgo.User {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User
	}
	return i.User
}

// openTicketFor returns the user's open ticket in a league whose channel still
// exists. Tickets whose channel has vanished are closed on the way.
func openTicketFor(s *discordgo.Session, league, userID string) (*Ticket, bool) {
	var t Ticket
	if err := DB.Where("league = ? AND opener_id = ? AND status = ?", league, userID, ticketStatusOpen).
		Order("id DESC").First(&t).Error; err != nil {
		return nil, false
	}
	if t.ChannelID != "" {
		if _, err := s.Channel(t.ChannelID); err == nil {
			return &t, true
		}
	}
	now := time.Now()
	DB.Model(&t).Updates(map[string]any{"status": ticketStatusClosed, "closed_at": &now, "closed_by": "channel missing"})
	return nil, false
}

// Step 1: issue-type button. Match and Team issues first show a dropdown of
// the presser's own matchups / teams; everything else goes straight to the form.
func handleTicketOpenButton(s *discordgo.Session, i *discordgo.InteractionCreate, league string, tt ticketType) {
	user := interactionUser(i)
	if user == nil {
		return
	}

	cfg := loadTicketConfig(league)
	if !cfg.Enabled || cfg.CategoryID == "" {
		ticketNotice(s, i, colorDanger, fmt.Sprintf("🚫 %s tickets are currently closed. Please try again later.", ticketLeagueName(league)))
		return
	}

	if t, ok := openTicketFor(s, league, user.ID); ok {
		ticketNotice(s, i, colorWarning, fmt.Sprintf("You already have an open %s ticket: <#%s>", ticketLeagueName(league), t.ChannelID))
		return
	}

	if ticketHasPicker(tt) {
		respondTicketPicker(s, i, league, tt, user.ID)
		return
	}
	respondTicketModal(s, i, league, tt, nil)
}

// ticketTarget is the match or team a ticket is about, once picked from the
// dropdown and verified to belong to the user.
type ticketTarget struct {
	ID      uint
	Subject string // ticket subject + form title
	Info    string // extra lines for the ticket embed
	Path    string // site path for the "View" link button, e.g. /match/12
	Match   *Match
	Team    *Team
}

// respondTicketModal shows the ticket form. With a picked target the subject
// is already known, so only the details box is asked for.
func respondTicketModal(s *discordgo.Session, i *discordgo.InteractionCreate, league string, tt ticketType, target *ticketTarget) {
	customID := ticketModalPrefix + league + "_" + tt.Slug
	rows := []discordgo.MessageComponent{}
	if target != nil {
		customID += "_" + strconv.FormatUint(uint64(target.ID), 10)
	} else {
		rows = append(rows, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.TextInput{
				CustomID:    "subject",
				Label:       tt.SubjectLabel,
				Style:       discordgo.TextInputShort,
				Placeholder: tt.SubjectHint,
				Required:    true,
				MinLength:   2,
				MaxLength:   100,
			},
		}})
	}
	rows = append(rows, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.TextInput{
			CustomID:    "details",
			Label:       "Details",
			Style:       discordgo.TextInputParagraph,
			Placeholder: tt.DetailsHint,
			Required:    true,
			MaxLength:   1500,
		},
	}})

	title := fmt.Sprintf("%s — %s", tt.Label, ticketLeagueName(league))
	if target != nil {
		title = truncateRunes(target.Subject, 45)
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID:   customID,
			Title:      title,
			Components: rows,
		},
	})
	if err != nil {
		log.Printf("❌ ticket modal: %v", err)
	}
}

/*
----------------------------------------------------
Pickers (Match Issue / Team Issue tickets)
----------------------------------------------------
*/

const ticketPickNotListed = "none"

func ticketHasPicker(tt ticketType) bool {
	return tt.Slug == "match" || tt.Slug == "team"
}

func parseDiscordID(id string) (int64, bool) {
	v, err := strconv.ParseInt(id, 10, 64)
	return v, err == nil
}

// ticketTeamsFor returns the user's teams in a league.
func ticketTeamsFor(league, discordID string) []Team {
	playerID, ok := parseDiscordID(discordID)
	if !ok {
		return nil
	}
	var teams []Team
	DB.Joins("JOIN team_members tm ON tm.team_id = teams.id").
		Where("tm.player_id = ? AND teams.league = ?", playerID, league).
		Order("teams.name ASC").
		Limit(24).
		Find(&teams)
	return teams
}

// ticketMatchesFor returns the user's non-archived matches in a league — via
// any of their teams, or as a league sub — newest first, capped so the list
// plus "not listed" fits a Discord select (25). Deliberately not filtered by
// season label: the season setting and the matches' season tags don't always
// line up, and archiving already drops old seasons.
func ticketMatchesFor(league, discordID string) []Match {
	playerID, ok := parseDiscordID(discordID)
	if !ok {
		return nil
	}

	teamIDs := []uint{0} // keep IN () valid; subs can still match
	for _, t := range ticketTeamsFor(league, discordID) {
		teamIDs = append(teamIDs, t.ID)
	}

	var matches []Match
	DB.Where("league = ? AND archived = false", league).
		Where("(team_a_id IN ? OR team_b_id IN ? OR league_sub_a = ? OR league_sub_b = ?)", teamIDs, teamIDs, playerID, playerID).
		Order("id DESC").
		Limit(24).
		Find(&matches)
	return matches
}

func teamNameMap(ids []uint) map[uint]string {
	var teams []Team
	DB.Select("id", "name").Where("id IN ?", ids).Find(&teams)
	names := map[uint]string{}
	for _, t := range teams {
		names[t.ID] = t.Name
	}
	return names
}

func nameOr(names map[uint]string, id uint) string {
	if n := names[id]; n != "" {
		return n
	}
	return "TBD"
}

// matchTitle: "Team A vs Team B — Week 3" (or Finals).
func matchTitle(m Match, teamA, teamB string) string {
	title := fmt.Sprintf("%s vs %s", teamA, teamB)
	if m.IsFinals {
		return title + " — Finals"
	}
	if strings.TrimSpace(m.Week) != "" {
		return title + " — Week " + m.Week
	}
	return title
}

// matchSummary: one-line status for the picker / ticket embed.
func matchSummary(m Match) string {
	parts := []string{}
	if strings.TrimSpace(m.Season) != "" {
		parts = append(parts, "Season "+m.Season)
	}
	if m.Status != "" {
		parts = append(parts, m.Status)
	}
	if m.TeamAScore > 0 || m.TeamBScore > 0 {
		parts = append(parts, fmt.Sprintf("%d–%d", m.TeamAScore, m.TeamBScore))
	}
	if m.ScheduledDate != nil {
		parts = append(parts, m.ScheduledDate.UTC().Format("Mon Jan 2, 15:04 UTC"))
	} else {
		parts = append(parts, "not scheduled")
	}
	return strings.Join(parts, " · ")
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func idPtr(id uint) *uint { return &id }

// teamRoleFor returns the user's role on a team ("Captain", "Player", …).
func teamRoleFor(teamID uint, discordID string) string {
	playerID, _ := parseDiscordID(discordID)
	var tm TeamMember
	if DB.Where("team_id = ? AND player_id = ?", teamID, playerID).First(&tm).Error != nil {
		return ""
	}
	return tm.Role
}

// respondTicketPicker shows the user's matchups or teams as a dropdown. When
// there's nothing to pick, it says so and offers to describe it manually.
func respondTicketPicker(s *discordgo.Session, i *discordgo.InteractionCreate, league string, tt ticketType, userID string) {
	var options []discordgo.SelectMenuOption
	var prompt, placeholder, notListed, empty string

	switch tt.Slug {
	case "match":
		matches := ticketMatchesFor(league, userID)
		ids := []uint{}
		for _, m := range matches {
			ids = append(ids, m.TeamAID, m.TeamBID)
		}
		names := teamNameMap(ids)
		for _, m := range matches {
			options = append(options, discordgo.SelectMenuOption{
				Label:       truncateRunes(matchTitle(m, nameOr(names, m.TeamAID), nameOr(names, m.TeamBID)), 100),
				Description: truncateRunes(matchSummary(m), 100),
				Value:       strconv.FormatUint(uint64(m.ID), 10),
				Emoji:       &discordgo.ComponentEmoji{Name: "⚔️"},
			})
		}
		prompt, placeholder = "⚔️ **Which match is this about?**", "Pick one of your matchups"
		notListed = "My match isn't listed"
		empty = fmt.Sprintf("⚔️ We couldn't find any %s matches for you — you may not be on a team with matches yet, or the schedule isn't out.", ticketLeagueName(league))

	case "team":
		for _, t := range ticketTeamsFor(league, userID) {
			desc := "Your team"
			if role := teamRoleFor(t.ID, userID); role != "" {
				desc = "You: " + role
			}
			options = append(options, discordgo.SelectMenuOption{
				Label:       truncateRunes(t.Name, 100),
				Description: truncateRunes(fmt.Sprintf("%s · %d-%d", desc, t.Wins, t.Losses), 100),
				Value:       strconv.FormatUint(uint64(t.ID), 10),
				Emoji:       &discordgo.ComponentEmoji{Name: "🛡️"},
			})
		}
		prompt, placeholder = "🛡️ **Which team is this about?**", "Pick one of your teams"
		notListed = "A different team"
		empty = fmt.Sprintf("🛡️ You're not on a %s team right now.", ticketLeagueName(league))
	}

	manualID := ticketManualPrefix + league + "_" + tt.Slug

	if len(options) == 0 {
		ticketNotice(s, i, tt.Color, empty,
			v2Divider(),
			v2Section(discordgo.Button{
				Label:    "Describe it myself",
				Style:    discordgo.PrimaryButton,
				CustomID: manualID,
				Emoji:    &discordgo.ComponentEmoji{Name: "✏️"},
			}, "You can still open a ticket and describe it yourself."),
		)
		return
	}

	options = append(options, discordgo.SelectMenuOption{
		Label:       notListed,
		Description: "Describe it yourself",
		Value:       ticketPickNotListed,
		Emoji:       &discordgo.ComponentEmoji{Name: "✏️"},
	})

	ticketNotice(s, i, tt.Color,
		fmt.Sprintf("%s\n-# %s · pick below and you'll only need to add the details.", prompt, ticketLeagueName(league)),
		v2Row(discordgo.SelectMenu{
			MenuType:    discordgo.StringSelectMenu,
			CustomID:    ticketPickPrefix + league + "_" + tt.Slug,
			Placeholder: placeholder,
			Options:     options,
		}),
	)
}

// resolveTicketTarget re-verifies that a picked match/team belongs to the user
// and builds what the ticket shows about it.
func resolveTicketTarget(league string, tt ticketType, userID string, id uint) (*ticketTarget, bool) {
	switch tt.Slug {
	case "match":
		for _, m := range ticketMatchesFor(league, userID) {
			if m.ID != id {
				continue
			}
			m := m
			names := teamNameMap([]uint{m.TeamAID, m.TeamBID})
			info := matchSummary(m)
			if m.MatchCode != "" {
				info = fmt.Sprintf("`%s` · %s", m.MatchCode, info)
			}
			if m.ScheduledDate != nil {
				info += fmt.Sprintf("\n🕒 <t:%d:F>", m.ScheduledDate.Unix())
			}
			if m.DiscordChannelID != nil && *m.DiscordChannelID != "" {
				info += fmt.Sprintf("\n💬 Match channel: <#%s>", *m.DiscordChannelID)
			}
			return &ticketTarget{
				ID:      m.ID,
				Subject: matchTitle(m, nameOr(names, m.TeamAID), nameOr(names, m.TeamBID)),
				Info:    info,
				Path:    fmt.Sprintf("/match/%d", m.ID),
				Match:   &m,
			}, true
		}
	case "team":
		for _, t := range ticketTeamsFor(league, userID) {
			if t.ID != id {
				continue
			}
			t := t
			info := fmt.Sprintf("Record %d-%d · Rating %d", t.Wins, t.Losses, t.Rating)
			if role := teamRoleFor(t.ID, userID); role != "" {
				info = fmt.Sprintf("Opener's role: **%s**\n%s", role, info)
			}
			return &ticketTarget{
				ID:      t.ID,
				Subject: t.Name,
				Info:    info,
				Path:    fmt.Sprintf("/teams/%d", t.ID),
				Team:    &t,
			}, true
		}
	}
	return nil, false
}

// Dropdown choice → open the details form for it (or the free-text form).
func handleTicketPicked(s *discordgo.Session, i *discordgo.InteractionCreate, league string, tt ticketType) {
	user := interactionUser(i)
	values := i.MessageComponentData().Values
	if user == nil || len(values) == 0 {
		return
	}
	if values[0] == ticketPickNotListed {
		respondTicketModal(s, i, league, tt, nil)
		return
	}
	id, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil {
		ticketNotice(s, i, colorDanger, "❌ Invalid choice.")
		return
	}
	target, ok := resolveTicketTarget(league, tt, user.ID, uint(id))
	if !ok {
		ticketNotice(s, i, colorDanger, "❌ That couldn't be found for you anymore. Please try again.")
		return
	}
	respondTicketModal(s, i, league, tt, target)
}

// modalValue pulls a text input's value out of a modal submission.
func modalValue(data discordgo.ModalSubmitInteractionData, customID string) string {
	for _, c := range data.Components {
		row, ok := c.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, inner := range row.Components {
			if ti, ok := inner.(*discordgo.TextInput); ok && ti.CustomID == customID {
				return strings.TrimSpace(ti.Value)
			}
		}
	}
	return ""
}

// Step 2: modal submitted → create the private ticket channel.
func handleTicketModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate, league string, tt ticketType, targetIDStr string) {
	user := interactionUser(i)
	if user == nil {
		return
	}

	// Creating a channel can take longer than Discord's 3s window.
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	data := i.ModalSubmitData()
	subject := modalValue(data, "subject")
	details := modalValue(data, "details")

	// A match/team picked from the list: re-verify it's theirs and use it as subject.
	var target *ticketTarget
	if targetIDStr != "" {
		id, err := strconv.ParseUint(targetIDStr, 10, 64)
		t, ok := resolveTicketTarget(league, tt, user.ID, uint(id))
		if err != nil || !ok {
			ticketFollowup(s, i, colorDanger, "❌ That couldn't be found for you anymore. Please try again.")
			return
		}
		target = t
		subject = t.Subject
	}
	if subject == "" {
		subject = tt.Label
	}

	cfg := loadTicketConfig(league)
	guildID := getEnv("DISCORD_GUILD_ID", i.GuildID)
	if !cfg.Enabled || cfg.CategoryID == "" || guildID == "" {
		ticketFollowup(s, i, colorDanger, fmt.Sprintf("🚫 %s tickets are currently closed.", ticketLeagueName(league)))
		return
	}

	ticketMu.Lock()
	defer ticketMu.Unlock()

	if t, ok := openTicketFor(s, league, user.ID); ok {
		ticketFollowup(s, i, colorWarning, fmt.Sprintf("You already have an open ticket: <#%s>", t.ChannelID))
		return
	}

	var number int
	DB.Raw(`SELECT COALESCE(MAX(number), 0) + 1 FROM tickets WHERE league = ?`, league).Scan(&number)
	if number < 1 {
		number = 1
	}

	staffAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages |
		discordgo.PermissionReadMessageHistory | discordgo.PermissionAttachFiles |
		discordgo.PermissionEmbedLinks)

	overwrites := []*discordgo.PermissionOverwrite{
		{ID: guildID, Type: discordgo.PermissionOverwriteTypeRole, Deny: discordgo.PermissionViewChannel},
		{ID: user.ID, Type: discordgo.PermissionOverwriteTypeMember, Allow: staffAllow},
	}
	if botID := ticketBotUserID(s); botID != "" {
		overwrites = append(overwrites, &discordgo.PermissionOverwrite{
			ID: botID, Type: discordgo.PermissionOverwriteTypeMember,
			Allow: staffAllow | discordgo.PermissionManageChannels | discordgo.PermissionManageMessages,
		})
	}
	modRoleID := strings.TrimSpace(os.Getenv("DISCORD_LEAGUE_MOD_ROLE_ID"))
	for _, roleID := range uniqueNonEmpty(modRoleID, cfg.SupportRoleID) {
		overwrites = append(overwrites, &discordgo.PermissionOverwrite{
			ID: roleID, Type: discordgo.PermissionOverwriteTypeRole,
			Allow: staffAllow | discordgo.PermissionManageMessages,
		})
	}

	name := ticketChannelName(league, tt, number)

	ch, err := s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:                 name,
		Type:                 discordgo.ChannelTypeGuildText,
		Topic:                fmt.Sprintf("%s · %s #%d — %s", ticketLeagueName(league), tt.Label, number, subject),
		ParentID:             cfg.CategoryID,
		PermissionOverwrites: overwrites,
	})
	if err != nil {
		log.Printf("❌ ticket channel create (%s): %v", league, err)
		ticketFollowup(s, i, colorDanger, "❌ Couldn't create your ticket channel. Please let a League Mod know.")
		return
	}

	ticket := Ticket{
		League:      league,
		Type:        tt.Slug,
		Number:      number,
		ChannelID:   ch.ID,
		OpenerID:    user.ID,
		OpenerName:  user.Username,
		Subject:     subject,
		Description: details,
		Status:      ticketStatusOpen,
	}
	if target != nil && target.Match != nil {
		ticket.MatchID = idPtr(target.Match.ID)
	}
	if target != nil && target.Team != nil {
		ticket.TeamID = idPtr(target.Team.ID)
	}
	if err := DB.Create(&ticket).Error; err != nil {
		log.Printf("❌ ticket save: %v", err)
		s.ChannelDelete(ch.ID)
		ticketFollowup(s, i, colorDanger, "❌ Couldn't save your ticket. Please try again.")
		return
	}

	// Ping the opener plus whoever handles tickets for this league.
	pingRole := cfg.SupportRoleID
	if pingRole == "" {
		pingRole = modRoleID
	}
	mentions := fmt.Sprintf("<@%s>", user.ID)
	allowedRoles := []string{}
	if pingRole != "" {
		mentions += fmt.Sprintf(" <@&%s>", pingRole)
		allowedRoles = append(allowedRoles, pingRole)
	}

	_, err = s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{
		Flags: v2Flag,
		// Mentions inside a TextDisplay still notify, limited by AllowedMentions.
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Users: []string{user.ID},
			Roles: allowedRoles,
		},
		Components: []discordgo.MessageComponent{
			v2Text(mentions),
			ticketWelcomeCard(&ticket, tt, target, user, details),
		},
	})
	if err != nil {
		log.Printf("⚠️ ticket welcome message: %v", err)
	}

	log.Printf("🎫 %s ticket #%d opened by %s (%s)", ticketLeagueName(league), number, user.Username, ch.ID)
	ticketFollowup(s, i, colorSuccess, fmt.Sprintf("✅ Your ticket has been created: <#%s>", ch.ID))
}

// ticketChannelName: league initial + ticket type + number, e.g. "vmatch-0001"
// (Varsity) or "rteam-0003" (Rookie). Discord lowercases text channel names.
func ticketChannelName(league string, tt ticketType, number int) string {
	kind := tt.Slug
	if kind == "general" {
		kind = "other" // matches the "Other" button
	}
	initial := strings.ToLower(ticketLeagueName(league)[:1])
	return fmt.Sprintf("%s%s-%04d", initial, kind, number)
}

// quoteBlock renders user text as a Discord block quote.
func quoteBlock(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, l := range lines {
		lines[i] = "> " + l
	}
	return strings.Join(lines, "\n")
}

// ticketWelcomeCard is the V2 container at the top of every ticket channel:
// header with the opener's avatar, what it's about, their details, and the
// Close / View buttons.
func ticketWelcomeCard(t *Ticket, tt ticketType, target *ticketTarget, user *discordgo.User, details string) discordgo.MessageComponent {
	if strings.TrimSpace(details) == "" {
		details = "_No details given._"
	}

	header := fmt.Sprintf("## %s %s\n**Ticket #%04d** · %s\nOpened by <@%s>",
		tt.Emoji, tt.Label, t.Number, ticketLeagueName(t.League), user.ID)
	headerSection := v2Text(header)
	if avatar := user.AvatarURL("128"); avatar != "" {
		headerSection = v2Section(discordgo.Thumbnail{Media: discordgo.UnfurledMediaItem{URL: avatar}}, header)
	}

	about := fmt.Sprintf("**%s**\n%s", tt.SubjectLabel, t.Subject)
	if target != nil && target.Info != "" {
		about += "\n" + target.Info
	}

	buttons := []discordgo.MessageComponent{
		discordgo.Button{
			Label:    "Close Ticket",
			Style:    discordgo.DangerButton,
			CustomID: fmt.Sprintf("%s%d", ticketClosePrefix, t.ID),
			Emoji:    &discordgo.ComponentEmoji{Name: "🔒"},
		},
	}
	if target != nil {
		if base := strings.TrimRight(strings.TrimSpace(os.Getenv("FRONTEND_URL")), "/"); base != "" {
			view := "View Match"
			if target.Team != nil {
				view = "View Team"
			}
			buttons = append(buttons, discordgo.Button{
				Label: view,
				Style: discordgo.LinkButton,
				URL:   base + target.Path,
				Emoji: &discordgo.ComponentEmoji{Name: "🔗"},
			})
		}
	}

	return v2Container(tt.Color,
		headerSection,
		v2Divider(),
		v2Text(about),
		v2Text("**Details**\n"+quoteBlock(details)),
		v2Divider(),
		v2Text("-# A League Mod will be with you soon. Press **Close Ticket** when you're done."),
		v2Row(buttons...),
		v2Divider(),
		v2Text("-# 🛠️ League Mod tools"),
		v2Row(
			discordgo.Button{
				Label:    "Add User",
				Style:    discordgo.SecondaryButton,
				CustomID: fmt.Sprintf("%s%d", ticketAddUserPrefix, t.ID),
				Emoji:    &discordgo.ComponentEmoji{Name: "👤"},
			},
			discordgo.Button{
				Label:    "Add Team",
				Style:    discordgo.SecondaryButton,
				CustomID: fmt.Sprintf("%s%d", ticketAddTeamPrefix, t.ID),
				Emoji:    &discordgo.ComponentEmoji{Name: "🛡️"},
			},
		),
	)
}

// canCloseTicket: the opener, League Mods and the league's support role.
func canCloseTicket(i *discordgo.InteractionCreate, t *Ticket) bool {
	user := interactionUser(i)
	if user != nil && user.ID == t.OpenerID {
		return true
	}
	if isInteractionMod(i) {
		return true
	}
	support := loadTicketConfig(t.League).SupportRoleID
	if support != "" && i.Member != nil {
		for _, r := range i.Member.Roles {
			if r == support {
				return true
			}
		}
	}
	return false
}

func loadTicketByIDString(idStr string) (*Ticket, bool) {
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return nil, false
	}
	var t Ticket
	if err := DB.First(&t, uint(id)).Error; err != nil {
		return nil, false
	}
	return &t, true
}

// Step 3: "Close Ticket" → ephemeral confirmation.
func handleTicketCloseButton(s *discordgo.Session, i *discordgo.InteractionCreate, idStr string) {
	t, ok := loadTicketByIDString(idStr)
	if !ok {
		ticketNotice(s, i, colorDanger, "❌ Ticket not found.")
		return
	}
	if t.Status != ticketStatusOpen {
		ticketNotice(s, i, colorWarning, "This ticket is already closed.")
		return
	}
	if !canCloseTicket(i, t) {
		ticketNotice(s, i, colorDanger, "❌ Only the ticket opener or League Mods can close this ticket.")
		return
	}

	ticketNotice(s, i, colorDanger,
		"### ⚠️ Close this ticket?\nA transcript will be saved and this channel will be **deleted**.",
		v2Row(
			discordgo.Button{
				Label:    "Confirm Close",
				Style:    discordgo.DangerButton,
				CustomID: fmt.Sprintf("%s%d", ticketConfirmClosePrefix, t.ID),
				Emoji:    &discordgo.ComponentEmoji{Name: "✅"},
			},
			discordgo.Button{
				Label:    "Cancel",
				Style:    discordgo.SecondaryButton,
				CustomID: ticketCancelClose,
			},
		),
	)
}

// Step 4: confirmed → transcript, delete channel, mark closed.
func handleTicketConfirmClose(s *discordgo.Session, i *discordgo.InteractionCreate, idStr string) {
	t, ok := loadTicketByIDString(idStr)
	if !ok {
		ticketNotice(s, i, colorDanger, "❌ Ticket not found.")
		return
	}
	if !canCloseTicket(i, t) {
		ticketNotice(s, i, colorDanger, "❌ Only the ticket opener or League Mods can close this ticket.")
		return
	}

	user := interactionUser(i)
	closer := "unknown"
	if user != nil {
		closer = user.Username
	}

	// Claim the close atomically so a double-click can't run it twice.
	now := time.Now()
	res := DB.Model(&Ticket{}).
		Where("id = ? AND status = ?", t.ID, ticketStatusOpen).
		Updates(map[string]any{"status": ticketStatusClosed, "closed_at": &now, "closed_by": closer})
	if res.Error != nil || res.RowsAffected == 0 {
		ticketNotice(s, i, colorWarning, "This ticket is already closed.")
		return
	}
	t.Status, t.ClosedAt, t.ClosedBy = ticketStatusClosed, &now, closer

	ticketUpdateNotice(s, i, colorNeutral, "🔒 Closing ticket and saving transcript…")

	if cfg := loadTicketConfig(t.League); cfg.TranscriptChannelID != "" {
		transcriptTicketChannel(s, t, cfg.TranscriptChannelID)
	} else {
		log.Printf("⚠️ No ticket transcript channel set for %s — skipping transcript", t.League)
	}

	if _, err := s.ChannelDelete(t.ChannelID); err != nil {
		log.Printf("❌ Failed to delete ticket channel %s: %v", t.ChannelID, err)
	}
	log.Printf("🔒 %s ticket #%d closed by %s", ticketLeagueName(t.League), t.Number, closer)
}

// fetchChannelMessages returns every message in a channel, oldest first.
func fetchChannelMessages(s *discordgo.Session, channelID string) []*discordgo.Message {
	var all []*discordgo.Message
	beforeID := ""
	for {
		msgs, err := s.ChannelMessages(channelID, 100, beforeID, "", "")
		if err != nil {
			log.Printf("❌ Failed to fetch messages from %s: %v", channelID, err)
			break
		}
		if len(msgs) == 0 {
			break
		}
		all = append(all, msgs...)
		beforeID = msgs[len(msgs)-1].ID
		if len(msgs) < 100 {
			break
		}
	}
	for a, b := 0, len(all)-1; a < b; a, b = a+1, b-1 {
		all[a], all[b] = all[b], all[a]
	}
	return all
}

func transcriptTicketChannel(s *discordgo.Session, t *Ticket, transcriptChannelID string) {
	msgs := fetchChannelMessages(s, t.ChannelID)

	var b strings.Builder
	tt := ticketTypeBySlug(t.Type)
	fmt.Fprintf(&b, "ECGL %s Ticket #%04d — %s\n", ticketLeagueName(t.League), t.Number, tt.Label)
	fmt.Fprintf(&b, "%s: %s\n", tt.SubjectLabel, t.Subject)
	fmt.Fprintf(&b, "Opened by: %s (%s) at %s\n", t.OpenerName, t.OpenerID, t.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&b, "Closed by: %s\n", t.ClosedBy)
	b.WriteString(strings.Repeat("─", 50) + "\n\n")
	for _, m := range msgs {
		if m.Author == nil {
			continue
		}
		content := m.Content
		for _, e := range m.Embeds {
			if e.Title != "" || e.Description != "" {
				content += fmt.Sprintf(" [Embed: %s — %s]", e.Title, e.Description)
			}
		}
		// Components V2 messages (like the ticket's own welcome card) keep all
		// their text in components rather than content/embeds.
		if txt := componentText(m.Components); txt != "" {
			if content != "" {
				content += " "
			}
			content += "[Card: " + strings.ReplaceAll(txt, "\n", " / ") + "]"
		}
		fmt.Fprintf(&b, "[%s] %s: %s\n", m.Timestamp.Format("2006-01-02 15:04:05"), m.Author.Username, content)
		for _, att := range m.Attachments {
			fmt.Fprintf(&b, "  [Attachment: %s]\n", att.URL)
		}
	}

	fileName := fmt.Sprintf("ticket-%s-%04d.txt", t.League, t.Number)

	about := fmt.Sprintf("**%s:** %s", tt.SubjectLabel, t.Subject)
	if t.MatchID != nil {
		about += fmt.Sprintf(" · match #%d", *t.MatchID)
	}
	if t.TeamID != nil {
		about += fmt.Sprintf(" · team #%d", *t.TeamID)
	}

	card := v2Container(tt.Color,
		v2Text(fmt.Sprintf("## 📋 %s %s — Ticket #%04d\n%s", tt.Emoji, tt.Label, t.Number, ticketLeagueName(t.League))),
		v2Divider(),
		v2Text(about),
		v2Text(fmt.Sprintf("**Opened by** <@%s> · <t:%d:f>\n**Closed by** %s · <t:%d:f>\n**Messages** %d",
			t.OpenerID, t.CreatedAt.Unix(), t.ClosedBy, time.Now().Unix(), len(msgs))),
		v2Divider(),
		// V2 only shows attachments that a component points at.
		discordgo.FileComponent{File: discordgo.UnfurledMediaItem{URL: "attachment://" + fileName}},
	)

	_, err := s.ChannelMessageSendComplex(transcriptChannelID, &discordgo.MessageSend{
		Flags:           v2Flag,
		Components:      []discordgo.MessageComponent{card},
		AllowedMentions: &discordgo.MessageAllowedMentions{}, // don't ping anyone from the log
		Files: []*discordgo.File{{
			Name:   fileName,
			Reader: strings.NewReader(b.String()),
		}},
	})
	if err != nil {
		log.Printf("❌ Failed to send ticket transcript to %s: %v", transcriptChannelID, err)
	}
}

func ticketBotUserID(s *discordgo.Session) string {
	if s != nil && s.State != nil && s.State.User != nil {
		return s.State.User.ID
	}
	return strings.TrimSpace(os.Getenv("DISCORD_CLIENT_ID"))
}

func uniqueNonEmpty(vals ...string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range vals {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

/*
====================================================
PANEL
====================================================
*/

// ticketPanelSpec describes a league's ticket panel for the shared panel
// helpers in discord_panels.go.
func ticketPanelSpec(league string) panelSpec {
	league = normalizeLeague(league)
	cfg := loadTicketConfig(league)
	return panelSpec{
		Key:       "tickets_" + league,
		Name:      ticketLeagueName(league) + " ticket panel",
		ChannelID: cfg.PanelChannelID,
		Enabled:   cfg.Enabled,
		Build:     func() []discordgo.MessageComponent { return ticketPanelContent(league) },
	}
}

// postTicketPanel (re)posts a league's ticket panel in its panel channel.
func postTicketPanel(s *discordgo.Session, league string) (string, error) {
	return postPanel(s, ticketPanelSpec(league))
}

// ticketPanelContent builds the V2 panel: a header, then one section per
// issue type with its button beside the description. Used both to post a new
// panel and to edit an existing one in place.
func ticketPanelContent(league string) []discordgo.MessageComponent {
	comps := []discordgo.MessageComponent{
		v2Text(fmt.Sprintf("## 🎫 %s Support\nNeed help from the League Mods? Pick the option that best fits your problem to open a **private ticket** with the mod team.",
			ticketLeagueName(league))),
		v2Divider(),
	}
	for _, tt := range ticketTypes {
		comps = append(comps, v2Section(discordgo.Button{
			Label:    tt.Label,
			Style:    tt.Style,
			CustomID: ticketOpenPrefix + league + "_" + tt.Slug,
			Emoji:    &discordgo.ComponentEmoji{Name: tt.Emoji},
		}, fmt.Sprintf("%s **%s**\n%s", tt.Emoji, tt.Label, tt.Blurb)))
	}
	comps = append(comps,
		v2Divider(),
		v2Text("-# 🔒 Only you and the League Mods can see your ticket. One open ticket per person per league."),
	)
	return []discordgo.MessageComponent{v2Container(colorNeutral, comps...)}
}

/*
====================================================
HTTP
====================================================
*/

// POST /api/mod/tickets/panel — (re)post the active league's ticket panel.
func HandlePostTicketPanel(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireDev(w, r); !ok {
		return
	}
	league := leagueFromRequest(r)
	ch, err := postTicketPanel(discordSession, league)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	respondJSON(w, map[string]any{"success": true, "channel_id": ch, "league": league})
}

// GET /api/mod/discord/channels — categories and text channels for the
// settings pickers.
func HandleListDiscordChannels(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireDev(w, r); !ok {
		return
	}
	guildID := getEnv("DISCORD_GUILD_ID", "")
	if discordSession == nil || guildID == "" {
		http.Error(w, "Discord bot not connected", http.StatusServiceUnavailable)
		return
	}
	chans, err := discordSession.GuildChannels(guildID)
	if err != nil {
		http.Error(w, "Failed to load Discord channels", http.StatusBadGateway)
		return
	}
	sort.SliceStable(chans, func(a, b int) bool { return chans[a].Position < chans[b].Position })

	type item struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		ParentID   string `json:"parent_id,omitempty"`
		ParentName string `json:"parent_name,omitempty"`
	}
	catNames := map[string]string{}
	categories := []item{}
	for _, c := range chans {
		if c.Type == discordgo.ChannelTypeGuildCategory {
			catNames[c.ID] = c.Name
			categories = append(categories, item{ID: c.ID, Name: c.Name})
		}
	}
	text := []item{}
	for _, c := range chans {
		if c.Type == discordgo.ChannelTypeGuildText || c.Type == discordgo.ChannelTypeGuildNews {
			text = append(text, item{ID: c.ID, Name: c.Name, ParentID: c.ParentID, ParentName: catNames[c.ParentID]})
		}
	}
	// Group text channels under their category, in category order.
	catOrder := map[string]int{"": -1}
	for idx, c := range categories {
		catOrder[c.ID] = idx
	}
	sort.SliceStable(text, func(a, b int) bool { return catOrder[text[a].ParentID] < catOrder[text[b].ParentID] })

	roles := []item{}
	if gr, err := discordSession.GuildRoles(guildID); err == nil {
		sort.SliceStable(gr, func(a, b int) bool { return gr[a].Position > gr[b].Position })
		for _, role := range gr {
			if role.ID == guildID || role.Managed {
				continue // skip @everyone and bot/integration roles
			}
			roles = append(roles, item{ID: role.ID, Name: role.Name})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"categories": categories, "text_channels": text, "roles": roles})
}
