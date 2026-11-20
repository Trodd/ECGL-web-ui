package main

// Quick Rookie League registration from the Discord draft panel.
//
// Pressing Join Draft (or Coach Signup) when you're not registered for the
// Rookie League signs you straight up:
//
//   - Already have a complete site profile (e.g. a Varsity player)? You're
//     registered for the Rookie League as a Player using that profile —
//     nothing to fill in, and your global/Varsity role is left untouched.
//   - Otherwise an ephemeral card asks for just your timezone (the same list,
//     grouped by region, as the website's Register tab) and device. One press
//     registers you as a Player and enters you into the draft.
//
// The card is stateless: the picks so far are encoded in every component's
// custom ID, so nothing is lost if the bot restarts mid-way.

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const (
	dregTZPrefix     = "dreg_tz" // + group index + "|"
	dregDevicePrefix = "dreg_dev|"
	dregSubmitPrefix = "dreg_go|"
)

// allTimezones flattens the website's timezone groups (registration.go);
// regState.TZ indexes into this list.
func allTimezones() []regOption {
	var out []regOption
	for _, g := range regTimezoneGroups {
		out = append(out, g.Options...)
	}
	return out
}

func timezoneIndex(value string) int {
	for i, o := range allTimezones() {
		if strings.EqualFold(o.Value, value) {
			return i
		}
	}
	return -1
}

func deviceIndex(value string) int {
	for i, o := range regDeviceOptions {
		if strings.EqualFold(o.Value, value) {
			return i
		}
	}
	return -1
}

// regState is what's been picked so far (-1 = not yet) plus the draft kind
// the player was joining.
type regState struct {
	TZ, Device int
	Kind       string
}

func (st regState) encode() string {
	return fmt.Sprintf("%d.%d.%s", st.TZ, st.Device, draftKindOf(st.Kind))
}

func decodeRegState(s string) regState {
	st := regState{TZ: -1, Device: -1, Kind: draftKindPlayer}
	parts := strings.SplitN(s, ".", 3)
	if len(parts) == 3 {
		st.TZ, _ = strconv.Atoi(parts[0])
		st.Device, _ = strconv.Atoi(parts[1])
		st.Kind = draftKindOf(parts[2])
	}
	return st
}

func pickedLabel(opts []regOption, idx int) string {
	if idx >= 0 && idx < len(opts) {
		return opts[idx].Label
	}
	return "_not picked_"
}

// regSelect renders a dropdown whose option values are offset+index, so the
// two timezone dropdowns share one index space.
func regSelect(customID, placeholder string, opts []regOption, selected, offset int) discordgo.MessageComponent {
	options := make([]discordgo.SelectMenuOption, len(opts))
	for i, o := range opts {
		options[i] = discordgo.SelectMenuOption{
			Label:   o.Label,
			Value:   strconv.Itoa(offset + i),
			Default: offset+i == selected,
		}
	}
	return v2Row(discordgo.SelectMenu{
		MenuType:    discordgo.StringSelectMenu,
		CustomID:    customID,
		Placeholder: placeholder,
		Options:     options,
	})
}

// draftRegisterCard builds the timezone + device card for the current picks.
func draftRegisterCard(st regState, problem string) []discordgo.MessageComponent {
	enc := st.encode()
	ready := st.TZ >= 0 && st.Device >= 0

	label := "Sign Me Up"
	intro := "### 📝 Join the Rookie League Draft\nJust pick your **timezone** (from your region below) and **device** — you'll be registered as a **Player** and entered into the draft in one go."
	if st.Kind == draftKindCoach {
		label = "Sign Me Up as Coach"
		intro = "### 🧑‍🏫 Coach in the Rookie League\nJust pick your **timezone** (from your region below) and **device** — you'll be registered and entered into the coach draft in one go."
	}

	summary := fmt.Sprintf("**Timezone:** %s\n**Device:** %s",
		pickedLabel(allTimezones(), st.TZ), pickedLabel(regDeviceOptions, st.Device))
	if problem != "" {
		summary += "\n❌ " + problem
	}

	comps := []discordgo.MessageComponent{
		v2Text(intro),
		v2Divider(),
		v2Text(summary),
	}
	// One dropdown per region, same groups and labels as the website. (Discord
	// allows 25 options per dropdown, so the regions can't share one.)
	offset := 0
	for gi, g := range regTimezoneGroups {
		comps = append(comps, regSelect(fmt.Sprintf("%s%d|%s", dregTZPrefix, gi, enc), g.Label+" — pick your timezone", g.Options, st.TZ, offset))
		offset += len(g.Options)
	}
	comps = append(comps, regSelect(dregDevicePrefix+enc, "Device you play on", regDeviceOptions, st.Device, 0))

	buttons := []discordgo.MessageComponent{
		discordgo.Button{
			Label:    label,
			Style:    discordgo.SuccessButton,
			CustomID: dregSubmitPrefix + enc,
			Disabled: !ready,
			Emoji:    &discordgo.ComponentEmoji{Name: "✅"},
		},
	}
	if link := rookieLink("/register"); link != "" {
		buttons = append(buttons, discordgo.Button{
			Label: "Full Registration on the Website",
			Style: discordgo.LinkButton,
			URL:   link,
			Emoji: &discordgo.ComponentEmoji{Name: "🔗"},
		})
	}
	comps = append(comps, v2Row(buttons...))

	return []discordgo.MessageComponent{v2Container(colorRookie, comps...)}
}

// bannedAnywhere: banned globally, in Varsity or in the Rookie League — they
// can't self-register here.
func bannedAnywhere(playerID int64) bool {
	var p Player
	if DB.Select("role").First(&p, playerID).Error == nil && strings.EqualFold(p.Role, "Banned") {
		return true
	}
	return playerIsBannedIn(playerID, LeagueMain) || playerIsBannedIn(playerID, LeagueRookie)
}

// autoRegisterRookieFromProfile registers a player who already has a complete
// site profile for the Rookie League as a Player. Only the Rookie League role
// is set — their global/Varsity role (e.g. League Sub) is left alone.
func autoRegisterRookieFromProfile(playerID int64) bool {
	var p Player
	if DB.First(&p, playerID).Error != nil {
		return false
	}
	if strings.TrimSpace(p.Role) == "" || strings.TrimSpace(p.Device) == "" || strings.TrimSpace(p.Timezone) == "" {
		return false
	}
	if err := setPlayerRoleInLeague(playerID, LeagueRookie, "Player"); err != nil {
		log.Printf("⚠️ rookie auto-register %d: %v", playerID, err)
		return false
	}
	go syncDiscordRolesForPlayer(playerID)
	go SendDiscordLog(fmt.Sprintf("🟢 **<@%d>** registered for the **Rookie League** as a **Player** from the Discord draft panel", playerID), LeagueRookie)
	return true
}

// startDraftRegistration handles a Join Draft / Coach Signup press from
// someone not registered for the Rookie League. Returns true if it responded
// (card or refusal); false means they're now registered and the caller should
// carry on with the normal draft flow.
func startDraftRegistration(s *discordgo.Session, i *discordgo.InteractionCreate, playerID int64, kind string) bool {
	if bannedAnywhere(playerID) {
		ticketNotice(s, i, colorDanger, "❌ Your account is banned.")
		return true
	}
	if autoRegisterRookieFromProfile(playerID) {
		return false // complete profile → straight into the normal draft form
	}

	st := regState{TZ: -1, Device: -1, Kind: draftKindOf(kind)}
	var p Player
	if DB.First(&p, playerID).Error == nil { // partial profile: pre-fill what we can
		st.TZ = timezoneIndex(p.Timezone)
		st.Device = deviceIndex(p.Device)
	}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: v2Ephemeral, Components: draftRegisterCard(st, "")},
	}); err != nil {
		log.Printf("⚠️ draft register card: %v", err)
	}
	return true
}

func updateRegisterCard(s *discordgo.Session, i *discordgo.InteractionCreate, comps []discordgo.MessageComponent) {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Flags: v2Flag, Components: comps},
	}); err != nil {
		log.Printf("⚠️ draft register card update: %v", err)
	}
}

// handleDraftRegisterComponent handles the card's dropdowns and its sign-up
// button. Returns false if the custom ID isn't one of ours.
func handleDraftRegisterComponent(s *discordgo.Session, i *discordgo.InteractionCreate, id string) bool {
	pick := func() int {
		if v := i.MessageComponentData().Values; len(v) > 0 {
			if n, err := strconv.Atoi(v[0]); err == nil {
				return n
			}
		}
		return -1
	}

	switch {
	case strings.HasPrefix(id, dregTZPrefix):
		_, enc, _ := strings.Cut(strings.TrimPrefix(id, dregTZPrefix), "|")
		st := decodeRegState(enc)
		st.TZ = pick()
		updateRegisterCard(s, i, draftRegisterCard(st, ""))
	case strings.HasPrefix(id, dregDevicePrefix):
		st := decodeRegState(strings.TrimPrefix(id, dregDevicePrefix))
		st.Device = pick()
		updateRegisterCard(s, i, draftRegisterCard(st, ""))
	case strings.HasPrefix(id, dregSubmitPrefix):
		submitDraftRegistration(s, i, decodeRegState(strings.TrimPrefix(id, dregSubmitPrefix)))
	default:
		return false
	}
	return true
}

// submitDraftRegistration registers the player as a Player and enters them
// into the draft in one step.
func submitDraftRegistration(s *discordgo.Session, i *discordgo.InteractionCreate, st regState) {
	playerID, _, ok := interactionPlayerID(i)
	user := interactionUser(i)
	if !ok || user == nil {
		return
	}
	tzs := allTimezones()
	if st.TZ < 0 || st.TZ >= len(tzs) || st.Device < 0 || st.Device >= len(regDeviceOptions) {
		updateRegisterCard(s, i, draftRegisterCard(st, "Pick your timezone and device first."))
		return
	}
	if bannedAnywhere(playerID) {
		updateRegisterCard(s, i, draftRegisterCard(st, "Your account is banned."))
		return
	}

	timezone := tzs[st.TZ].Value
	device := regDeviceOptions[st.Device].Value

	displayName := user.GlobalName
	if strings.TrimSpace(displayName) == "" {
		displayName = user.Username
	}

	var existing Player
	DB.First(&existing, playerID)

	if strings.TrimSpace(existing.Role) != "" {
		// Already has a site role (e.g. a Varsity League Sub with no device on
		// file): save the picks and add the Rookie role, but never overwrite
		// their global role.
		if err := DB.Model(&Player{}).Where("id = ?", playerID).
			Updates(map[string]any{"device": device, "timezone": timezone}).Error; err != nil {
			updateRegisterCard(s, i, draftRegisterCard(st, "Couldn't save your profile — please try again."))
			return
		}
		if !autoRegisterRookieFromProfile(playerID) {
			updateRegisterCard(s, i, draftRegisterCard(st, "Couldn't register you — please try again."))
			return
		}
	} else if _, uerr := registerPlayer(playerID, user.Username, displayName, "Player", device, timezone, LeagueRookie); uerr != nil {
		updateRegisterCard(s, i, draftRegisterCard(st, uerr.Error()))
		return
	}

	signup, derr := joinDraft(playerID, LeagueRookie, st.Kind, "")
	if derr != nil {
		// Registered fine, but the draft refused (e.g. veteran with coach signups closed).
		updateRegisterCard(s, i, []discordgo.MessageComponent{v2Container(colorWarning,
			v2Text("### ✅ Registered for the Rookie League\n…but you couldn't be entered into the draft:\n❌ "+derr.Error()),
		)})
		return
	}

	text := fmt.Sprintf("### ✅ You're in the Rookie League draft\nRegistered as a **Player** (%s · %s) and signed up for the draft as a **%s**. A League Mod will place you on a team — press **My Status** on the draft panel any time to check.",
		tzs[st.TZ].Label, regDeviceOptions[st.Device].Label, draftKindLabel(signup.Kind))
	if draftKindOf(signup.Kind) != st.Kind {
		text += "\n-# Veterans and registered coaches go into the coach draft."
	}
	text += "\n-# Want to leave a note for the mods? Use **Add a Note** below."

	updateRegisterCard(s, i, []discordgo.MessageComponent{v2Container(colorSuccess,
		v2Text(text),
		v2Row(
			discordgo.Button{
				Label: "Add a Note", Style: discordgo.SecondaryButton,
				CustomID: draftJoinPrefix + signup.Kind, Emoji: &discordgo.ComponentEmoji{Name: "📝"},
			},
			discordgo.Button{
				Label: "Leave the Draft", Style: discordgo.DangerButton,
				CustomID: draftWithdrawID, Emoji: &discordgo.ComponentEmoji{Name: "↩️"},
			},
		),
	)})
	log.Printf("🟢 %s registered + joined the Rookie draft from Discord", user.Username)
}
