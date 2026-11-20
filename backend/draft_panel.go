package main

// Rookie League draft signup panel in Discord. A League Settings option
// (ROOKIE_DRAFT_PANEL_CHANNEL_ID, Rookie tab) picks the channel; the bot keeps
// one Components V2 panel there (see discord_panels.go) with buttons to sign
// up as a player or coach, check your status, and withdraw.
//
// Every rule comes from draftEligibleKind / joinDraft / withdrawFromDraft in
// draft.go — the exact same code the website's Register tab uses.

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	draftJoinPrefix  = "draft_join_"  // + kind (player / coach) — button
	draftModalPrefix = "draft_modal_" // + kind — signup form
	draftStatusID    = "draft_status"
	draftWithdrawID  = "draft_withdraw"

	colorRookie = 0x57F287
)

func draftPanelSpec() panelSpec {
	return panelSpec{
		Key:       "draft_" + LeagueRookie,
		Name:      "Rookie League draft panel",
		ChannelID: strings.TrimSpace(os.Getenv("ROOKIE_DRAFT_PANEL_CHANNEL_ID")),
		Enabled:   true, // a channel being set is the switch
		Build:     draftPanelContent,
	}
}

// siteLink returns FRONTEND_URL + path, or "" when no site URL is configured.
func siteLink(path string) string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("FRONTEND_URL")), "/")
	if base == "" {
		return ""
	}
	return base + path
}

// rookieLink is a site link that opens in the Rookie League (the website
// shows — and registers for — whichever league ?league= selects).
func rookieLink(path string) string {
	return siteLink(path + "?league=" + LeagueRookie)
}

func draftPanelContent() []discordgo.MessageComponent {
	canCreate := leagueAllowsTeamCreation(LeagueRookie)

	intro := "## 📝 Rookie League Draft\nNo team yet? Sign up for the draft and the League Mods will place you on a **Rookie League** team."
	if canCreate {
		intro += "\n\nWant to play with friends instead? You can **create your own Rookie League team** on the website and invite them — no draft needed."
	}

	comps := []discordgo.MessageComponent{
		v2Text(intro),
		v2Divider(),
		v2Section(discordgo.Button{
			Label:    "Join Draft",
			Style:    discordgo.SuccessButton,
			CustomID: draftJoinPrefix + draftKindPlayer,
			Emoji:    &discordgo.ComponentEmoji{Name: "🎮"},
		}, "🎮 **Sign up as a Player**\nGet drafted onto a rookie team."),
	}
	if coachDraftEnabled(LeagueRookie) {
		comps = append(comps, v2Section(discordgo.Button{
			Label:    "Coach Signup",
			Style:    discordgo.PrimaryButton,
			CustomID: draftJoinPrefix + draftKindCoach,
			Emoji:    &discordgo.ComponentEmoji{Name: "🧑‍🏫"},
		}, "🧑‍🏫 **Sign up as a Coach**\nHelp lead a rookie team — veterans welcome."))
	}
	if canCreate {
		createText := "🛡️ **Start your own team**\nCreate a Rookie League team on the website (My Team → Create / Join team) and invite your friends."
		if link := rookieLink("/myteam"); link != "" {
			comps = append(comps, v2Section(discordgo.Button{
				Label: "Create a Team",
				Style: discordgo.LinkButton,
				URL:   link,
				Emoji: &discordgo.ComponentEmoji{Name: "🛡️"},
			}, createText))
		} else {
			comps = append(comps, v2Text(createText))
		}
	}
	comps = append(comps,
		v2Section(discordgo.Button{
			Label:    "My Status",
			Style:    discordgo.SecondaryButton,
			CustomID: draftStatusID,
			Emoji:    &discordgo.ComponentEmoji{Name: "🔎"},
		}, "🔎 **Check your status**\nSee whether you're signed up, or leave the draft."),
		v2Divider(),
		v2Text("-# New to ECGL? **Join Draft** registers you for the Rookie League as you sign up. Main-league veterans are entered as coaches."),
	)
	if canCreate {
		comps = append(comps, v2Text("⚠️ **League Mods can overrule any team that's too stacked (OP)** — including moving players or breaking the team up — to keep the Rookie League fair."))
	}
	if link := rookieLink("/register"); link != "" {
		comps = append(comps, v2Row(discordgo.Button{
			Label: "Register on the Website",
			Style: discordgo.LinkButton,
			URL:   link,
			Emoji: &discordgo.ComponentEmoji{Name: "🔗"},
		}))
	}
	return []discordgo.MessageComponent{v2Container(colorRookie, comps...)}
}

// RegisterDraftPanelHandlers wires the draft panel's buttons and form.
func RegisterDraftPanelHandlers(dg *discordgo.Session) {
	dg.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.Type {
		case discordgo.InteractionMessageComponent:
			id := i.MessageComponentData().CustomID
			switch {
			case strings.HasPrefix(id, draftJoinPrefix):
				handleDraftJoinButton(s, i, strings.TrimPrefix(id, draftJoinPrefix))
			case id == draftStatusID:
				handleDraftStatus(s, i)
			case id == draftWithdrawID:
				handleDraftWithdrawButton(s, i)
			default:
				handleDraftRegisterComponent(s, i, id) // in-Discord registration card
			}
		case discordgo.InteractionModalSubmit:
			if id := i.ModalSubmitData().CustomID; strings.HasPrefix(id, draftModalPrefix) {
				handleDraftModalSubmit(s, i, strings.TrimPrefix(id, draftModalPrefix))
			}
		}
	})
}

func interactionPlayerID(i *discordgo.InteractionCreate) (int64, string, bool) {
	u := interactionUser(i)
	if u == nil {
		return 0, "", false
	}
	id, err := strconv.ParseInt(u.ID, 10, 64)
	return id, u.ID, err == nil
}

func draftKindLabel(kind string) string {
	if draftKindOf(kind) == draftKindCoach {
		return "Coach"
	}
	return "Player"
}

func draftWithdrawRow() discordgo.MessageComponent {
	return v2Row(discordgo.Button{
		Label:    "Leave the Draft",
		Style:    discordgo.DangerButton,
		CustomID: draftWithdrawID,
		Emoji:    &discordgo.ComponentEmoji{Name: "↩️"},
	})
}

// "Join Draft" / "Coach Signup" → check eligibility, then ask for an optional note.
func handleDraftJoinButton(s *discordgo.Session, i *discordgo.InteractionCreate, requested string) {
	playerID, _, ok := interactionPlayerID(i)
	if !ok {
		return
	}
	if !playerIsRegisteredIn(playerID, LeagueRookie) {
		// Not registered: auto-register (existing profile) or ask region + device.
		if startDraftRegistration(s, i, playerID, requested) {
			return
		}
	}
	kind, derr := draftEligibleKind(playerID, LeagueRookie, requested)
	if derr != nil {
		ticketNotice(s, i, colorDanger, "❌ "+derr.Error())
		return
	}

	// Pre-fill their existing note if they're re-signing / updating.
	var existing DraftSignup
	DB.Where("player_id = ? AND league = ? AND status = ?", playerID, LeagueRookie, draftPending).First(&existing)

	title := "Rookie Draft — Player Signup"
	hint := "Availability, experience, preferred role… (optional)"
	if kind == draftKindCoach {
		title = "Rookie Draft — Coach Signup"
		hint = "Coaching experience, availability… (optional)"
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			// Carry the button they pressed; joinDraft re-applies the rules (e.g.
			// veterans → coach) and the reply explains if the kind changed.
			CustomID: draftModalPrefix + draftKindOf(requested),
			Title:    title,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "note",
						Label:       "About you",
						Style:       discordgo.TextInputParagraph,
						Placeholder: hint,
						Value:       existing.Note,
						Required:    false,
						MaxLength:   300,
					},
				}},
			},
		},
	})
	if err != nil {
		log.Printf("❌ draft modal: %v", err)
	}
}

// Form submitted → enter the draft with the same code the website uses.
func handleDraftModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate, requested string) {
	playerID, _, ok := interactionPlayerID(i)
	if !ok {
		return
	}
	// joinDraft writes to the DB and posts a log message — give it time.
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})

	signup, derr := joinDraft(playerID, LeagueRookie, requested, modalValue(i.ModalSubmitData(), "note"))
	if derr != nil {
		ticketFollowup(s, i, colorDanger, "❌ "+derr.Error())
		return
	}

	text := fmt.Sprintf("### ✅ You're in the Rookie League draft\nSigned up as a **%s**. A League Mod will place you on a team. Press **My Status** on the draft panel any time to check.",
		draftKindLabel(signup.Kind))
	if draftKindOf(signup.Kind) != draftKindOf(requested) {
		text += "\n-# Veterans and registered coaches go into the coach draft."
	}
	if signup.Note != "" {
		text += "\n**Your note**\n" + quoteBlock(signup.Note)
	}
	ticketFollowup(s, i, colorSuccess, text, draftWithdrawRow())
}

// "My Status" → where they stand, with the right next action.
func handleDraftStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	playerID, discordID, ok := interactionPlayerID(i)
	if !ok {
		return
	}
	if teams := ticketTeamsFor(LeagueRookie, discordID); len(teams) > 0 {
		ticketNotice(s, i, colorRookie, fmt.Sprintf("### 🛡️ You're on a team\nYou're on **%s** in the Rookie League — no need for the draft.", teams[0].Name))
		return
	}

	var signup DraftSignup
	if DB.Where("player_id = ? AND league = ? AND status = ?", playerID, LeagueRookie, draftPending).First(&signup).Error == nil {
		text := fmt.Sprintf("### 📝 You're in the draft\nSigned up as a **%s** <t:%d:R>. Waiting for a League Mod to place you on a team.",
			draftKindLabel(signup.Kind), signup.UpdatedAt.Unix())
		if signup.Note != "" {
			text += "\n**Your note**\n" + quoteBlock(signup.Note)
		}
		ticketNotice(s, i, colorRookie, text, draftWithdrawRow())
		return
	}

	ticketNotice(s, i, colorNeutral, "### 🔎 Not signed up\nYou're not in the Rookie League draft right now.",
		v2Row(discordgo.Button{
			Label:    "Join Draft",
			Style:    discordgo.SuccessButton,
			CustomID: draftJoinPrefix + draftKindPlayer,
			Emoji:    &discordgo.ComponentEmoji{Name: "🎮"},
		}))
}

// "Leave the Draft" → withdraw.
func handleDraftWithdrawButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	playerID, _, ok := interactionPlayerID(i)
	if !ok {
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	if derr := withdrawFromDraft(playerID, LeagueRookie); derr != nil {
		ticketFollowup(s, i, colorWarning, "ℹ️ "+derr.Error()+".")
		return
	}
	msg := "↩️ You've left the Rookie League draft. You can sign up again any time from the draft panel."
	if cd := draftRejoinCooldown(); cd > 0 {
		msg = fmt.Sprintf("↩️ You've left the Rookie League draft. You can sign up again from the draft panel <t:%d:R>.",
			time.Now().Add(cd).Unix())
	}
	ticketFollowup(s, i, colorNeutral, msg)
}

// POST /api/mod/draft/panel — (re)post the Rookie League draft panel.
func HandlePostDraftPanel(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireDev(w, r); !ok {
		return
	}
	ch, err := postPanel(discordSession, draftPanelSpec())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	respondJSON(w, map[string]any{"success": true, "channel_id": ch})
}
