package main

// /draftsignup — posts the Rookie League draft signup card (the same buttons
// as the draft panel) in whatever channel it's used in.
//
// Settings (Rookie League tab):
//
//	ROOKIE_DRAFT_COMMAND_ENABLED          "true" = everyone can use it; otherwise League Mods only
//	ROOKIE_DRAFT_COMMAND_COOLDOWN_MINUTES cooldown per person AND per channel (blank = 10, 0 = none)
//
// League Mods are never blocked by the toggle or the cooldown.

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

const draftCommandDefaultCooldown = 10 * time.Minute

var draftCommandCooldowns = struct {
	mu    sync.Mutex
	until map[string]time.Time // "u:<user id>" / "c:<channel id>" → usable again at
}{until: map[string]time.Time{}}

func draftCommandEnabledForMembers() bool {
	return strings.TrimSpace(os.Getenv("ROOKIE_DRAFT_COMMAND_ENABLED")) == "true"
}

func draftCommandCooldown() time.Duration {
	v := strings.TrimSpace(os.Getenv("ROOKIE_DRAFT_COMMAND_COOLDOWN_MINUTES"))
	if v == "" {
		return draftCommandDefaultCooldown
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return draftCommandDefaultCooldown
	}
	return time.Duration(n) * time.Minute
}

// draftCommandWait returns when the user may post again in this channel (zero
// time = now). It also drops expired entries so the map can't grow forever.
func draftCommandWait(userID, channelID string) time.Time {
	draftCommandCooldowns.mu.Lock()
	defer draftCommandCooldowns.mu.Unlock()

	now := time.Now()
	for k, t := range draftCommandCooldowns.until {
		if now.After(t) {
			delete(draftCommandCooldowns.until, k)
		}
	}
	wait := draftCommandCooldowns.until["u:"+userID]
	if c := draftCommandCooldowns.until["c:"+channelID]; c.After(wait) {
		wait = c
	}
	return wait
}

func startDraftCommandCooldown(userID, channelID string) {
	cd := draftCommandCooldown()
	if cd <= 0 {
		return
	}
	until := time.Now().Add(cd)
	draftCommandCooldowns.mu.Lock()
	draftCommandCooldowns.until["u:"+userID] = until
	draftCommandCooldowns.until["c:"+channelID] = until
	draftCommandCooldowns.mu.Unlock()
}

func handleDraftSignupSlashCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	user := interactionUser(i)
	if user == nil {
		return
	}
	isMod := isInteractionMod(i)

	if !isMod {
		if !leagueIsPublic(LeagueRookie) {
			ticketNotice(s, i, colorWarning, "🔒 The Rookie League isn't open yet.")
			return
		}
		if !draftCommandEnabledForMembers() {
			ticketNotice(s, i, colorDanger, "❌ `/draftsignup` is turned off for members right now — ask a League Mod to post the signup.")
			return
		}
		if wait := draftCommandWait(user.ID, i.ChannelID); time.Now().Before(wait) {
			ticketNotice(s, i, colorWarning, fmt.Sprintf("⏳ The draft signup was posted recently — you can post it again <t:%d:R>.", wait.Unix()))
			return
		}
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:      v2Flag,
			Components: draftPanelContent(),
		},
	})
	if err != nil {
		log.Printf("❌ /draftsignup: %v", err)
		return
	}
	if !isMod {
		startDraftCommandCooldown(user.ID, i.ChannelID)
	}
	log.Printf("📝 %s posted the Rookie draft signup in %s via /draftsignup", user.Username, i.ChannelID)
}
