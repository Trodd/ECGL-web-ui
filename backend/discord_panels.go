package main

// Bot-managed "panel" messages: a long-lived Components V2 message with
// buttons that the bot keeps up to date — the support-ticket panel for each
// league (tickets.go) and the Rookie League draft panel (draft_panel.go).
//
// Each panel is identified by a key. Its message is posted once, edited in
// place on every startup (so code changes show up without a new post),
// moved when its channel setting changes, and reposted if someone deletes it.

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// DiscordPanel remembers where a panel message lives.
type DiscordPanel struct {
	PanelKey  string `gorm:"primaryKey"`
	ChannelID string
	MessageID string
	UpdatedAt time.Time
}

// panelSpec describes one panel.
type panelSpec struct {
	Key       string // e.g. "tickets_main", "draft_rookie"
	Name      string // for logs / errors, e.g. "Rookie League draft panel"
	ChannelID string // configured channel ("" = not set)
	Enabled   bool   // allowed to auto-post when it has never been posted
	Build     func() []discordgo.MessageComponent
}

func loadPanel(key string) (DiscordPanel, bool) {
	var p DiscordPanel
	ok := DB.Where("panel_key = ?", key).First(&p).Error == nil && p.MessageID != ""
	return p, ok
}

// postPanel (re)posts a panel in its configured channel, removing the previous
// panel message wherever it was. Returns the channel it was posted in.
func postPanel(s *discordgo.Session, spec panelSpec) (string, error) {
	if s == nil {
		return "", fmt.Errorf("discord bot is not connected")
	}
	if old, ok := loadPanel(spec.Key); ok && old.ChannelID != "" {
		_ = s.ChannelMessageDelete(old.ChannelID, old.MessageID)
	}
	if spec.ChannelID == "" {
		DB.Where("panel_key = ?", spec.Key).Delete(&DiscordPanel{})
		return "", fmt.Errorf("no channel is set for the %s", spec.Name)
	}

	msg, err := s.ChannelMessageSendComplex(spec.ChannelID, &discordgo.MessageSend{
		Flags:      v2Flag,
		Components: spec.Build(),
	})
	if err != nil {
		return "", fmt.Errorf("could not post the %s in <#%s>: %w", spec.Name, spec.ChannelID, err)
	}
	DB.Save(&DiscordPanel{PanelKey: spec.Key, ChannelID: spec.ChannelID, MessageID: msg.ID, UpdatedAt: time.Now()})
	return spec.ChannelID, nil
}

// isDiscordGone reports whether a REST error means the message/channel no
// longer exists (as opposed to a transient failure we shouldn't act on).
func isDiscordGone(err error) bool {
	var rest *discordgo.RESTError
	if !errors.As(err, &rest) {
		return false
	}
	if rest.Message != nil {
		switch rest.Message.Code {
		case discordgo.ErrCodeUnknownMessage, discordgo.ErrCodeUnknownChannel:
			return true
		}
	}
	return rest.Response != nil && rest.Response.StatusCode == http.StatusNotFound
}

// refreshPanel brings a panel up to date: edits it in place (no new post, no
// notification), moves it if the channel setting changed, replaces a classic
// (pre-V2) message, and reposts it if the old message was deleted. Panels with
// no channel set are left alone.
func refreshPanel(s *discordgo.Session, spec panelSpec) {
	if s == nil || spec.ChannelID == "" {
		return
	}
	repost := func(why string) {
		if _, err := postPanel(s, spec); err != nil {
			log.Printf("⚠️ %s (%s): %v", spec.Name, why, err)
		} else {
			log.Printf("📌 Posted %s (%s)", spec.Name, why)
		}
	}

	panel, ok := loadPanel(spec.Key)
	if !ok {
		if spec.Enabled {
			repost("first post")
		}
		return
	}
	if panel.ChannelID != spec.ChannelID {
		repost("channel changed")
		return
	}

	existing, err := s.ChannelMessage(panel.ChannelID, panel.MessageID)
	if err == nil && existing.Flags&v2Flag == 0 {
		repost("upgraded to Components V2") // classic messages can't be edited into V2
		return
	}
	if err == nil {
		components := spec.Build()
		_, err = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:         panel.MessageID,
			Channel:    panel.ChannelID,
			Flags:      v2Flag,
			Components: &components,
		})
	}
	switch {
	case err == nil:
		DB.Model(&DiscordPanel{}).Where("panel_key = ?", spec.Key).Update("updated_at", time.Now())
		log.Printf("📌 Refreshed %s", spec.Name)
	case isDiscordGone(err):
		repost("message was missing")
	default:
		// Transient error: leave it be rather than risk a duplicate panel.
		log.Printf("⚠️ %s refresh: %v", spec.Name, err)
	}
}

// syncPanelAfterSettings reacts to a settings save: reposts the panel when its
// channel changed (or it was just enabled with no panel yet), and edits it in
// place when a setting that changes its content changed.
func syncPanelAfterSettings(s *discordgo.Session, spec panelSpec, prev map[string]string, channelKey, enabledKey string, contentKeys ...string) {
	if s == nil {
		return
	}
	_, hasPanel := loadPanel(spec.Key)

	if before, changed := prev[channelKey]; changed && strings.TrimSpace(before) != spec.ChannelID {
		if spec.ChannelID == "" && !hasPanel {
			return
		}
		if _, err := postPanel(s, spec); err != nil {
			log.Printf("⚠️ %s: %v", spec.Name, err)
		}
		return
	}
	if _, changed := prev[enabledKey]; changed && enabledKey != "" && spec.Enabled && !hasPanel {
		if _, err := postPanel(s, spec); err != nil {
			log.Printf("⚠️ %s: %v", spec.Name, err)
		}
		return
	}
	for _, k := range contentKeys {
		if _, changed := prev[k]; changed {
			refreshPanel(s, spec)
			return
		}
	}
}

// RefreshDiscordPanels updates every panel; called on startup.
func RefreshDiscordPanels(s *discordgo.Session) {
	if s == nil {
		return
	}
	for _, league := range []string{LeagueMain, LeagueRookie} {
		refreshPanel(s, ticketPanelSpec(league))
	}
	refreshPanel(s, draftPanelSpec())
}

// syncDiscordPanelsAfterSettings runs after the League Settings page saves.
func syncDiscordPanelsAfterSettings(prev map[string]string) {
	s := discordSession
	syncPanelAfterSettings(s, ticketPanelSpec(LeagueMain), prev, "TICKET_PANEL_CHANNEL_ID", "TICKETS_ENABLED")
	syncPanelAfterSettings(s, ticketPanelSpec(LeagueRookie), prev, "TICKET_PANEL_CHANNEL_ID_ROOKIE", "TICKETS_ENABLED_ROOKIE")
	syncPanelAfterSettings(s, draftPanelSpec(), prev, "ROOKIE_DRAFT_PANEL_CHANNEL_ID", "",
		"ROOKIE_COACH_DRAFT_ENABLED", "ROOKIE_TEAM_CREATION_ENABLED", "FRONTEND_URL")
}

// migrateTicketPanelsTable moves rows from the old ticket_panels table into
// discord_panels (keys "tickets_<league>") once, then drops the old table.
func migrateTicketPanelsTable() {
	const name = "discord_panels_from_ticket_panels_v1"
	if migrationApplied(name) {
		return
	}
	var exists bool
	DB.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'ticket_panels')`).Scan(&exists)
	if exists {
		if err := DB.Exec(`
			INSERT INTO discord_panels (panel_key, channel_id, message_id, updated_at)
			SELECT 'tickets_' || league, channel_id, message_id, COALESCE(updated_at, NOW())
			FROM ticket_panels
			ON CONFLICT (panel_key) DO NOTHING`).Error; err != nil {
			log.Printf("⚠️ migrate ticket_panels: %v", err)
			return
		}
		DB.Exec(`DROP TABLE IF EXISTS ticket_panels`)
		log.Printf("📌 Moved ticket panels into discord_panels")
	}
	if err := markMigrationApplied(name); err != nil {
		log.Printf("⚠️ %s marker: %v", name, err)
	}
}
