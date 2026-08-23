package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

// discordMemberInfo is a slim view of a Discord guild member used to build a
// session identity when impersonating a user.
type discordMemberInfo struct {
	Username   string
	GlobalName string
	Nick       string
	Avatar     string
}

// fetchDiscordMember looks up a guild member via the Discord API using the bot token.
func fetchDiscordMember(discordIDStr string) (*discordMemberInfo, error) {
	guildID := getEnv("DISCORD_GUILD_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")
	if guildID == "" || botToken == "" {
		return nil, fmt.Errorf("missing discord guild/bot config")
	}

	req, _ := http.NewRequest("GET",
		fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, discordIDStr),
		nil)
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("discord returned %d", resp.StatusCode)
	}

	var m struct {
		User struct {
			Username   string `json:"username"`
			GlobalName string `json:"global_name"`
			Avatar     string `json:"avatar"`
		} `json:"user"`
		Nick string `json:"nick"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}

	return &discordMemberInfo{
		Username:   m.User.Username,
		GlobalName: m.User.GlobalName,
		Nick:       m.Nick,
		Avatar:     m.User.Avatar,
	}, nil
}

// handleDevImpersonate fully swaps the active session to act as another user,
// so every subsequent request (reads AND writes) runs as that user. Only
// available when DEV_MODE is enabled and the real (pre-impersonation) login
// holds the Dev role.
func handleDevImpersonate(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("DEV_MODE") != "true" {
		http.Error(w, "Dev mode disabled", http.StatusForbidden)
		return
	}

	session, _ := store.Get(r, "session")

	// Resolve the real login: if we're already impersonating, keep the original.
	realID := ""
	if orig, ok := session.Values["dev_original_discord_id"].(string); ok && orig != "" {
		realID = orig
	} else if cur, ok := session.Values["discord_id"].(string); ok {
		realID = cur
	}
	if realID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Only devs may impersonate.
	if !userHasDiscordRole(realID, os.Getenv("DISCORD_DEV_ROLE_ID")) {
		http.Error(w, "Forbidden: missing Dev role", http.StatusForbidden)
		return
	}

	var body struct {
		DiscordID string `json:"discord_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DiscordID == "" {
		http.Error(w, "discord_id required", http.StatusBadRequest)
		return
	}
	targetID := body.DiscordID

	// Build the target identity from the DB, falling back to Discord.
	username := ""
	displayName := ""
	avatar := ""

	var player Player
	if err := DB.First(&player, "id = ?", targetID).Error; err == nil {
		username = player.Username
		displayName = player.DisplayName
		avatar = player.Avatar
	}

	if username == "" || avatar == "" || displayName == "" {
		if m, err := fetchDiscordMember(targetID); err == nil {
			if username == "" {
				username = m.Username
			}
			if displayName == "" {
				displayName = m.Nick
				if displayName == "" {
					displayName = m.GlobalName
				}
				if displayName == "" {
					displayName = m.Username
				}
			}
			if avatar == "" {
				avatar = m.Avatar
			}
		}
	}
	if username == "" {
		username = targetID
	}
	if displayName == "" {
		displayName = username
	}

	// Preserve the true login on first impersonation only.
	if orig, _ := session.Values["dev_original_discord_id"].(string); orig == "" {
		session.Values["dev_original_discord_id"] = session.Values["discord_id"]
		session.Values["dev_original_username"] = session.Values["username"]
		session.Values["dev_original_display_name"] = session.Values["display_name"]
		session.Values["dev_original_avatar"] = session.Values["avatar"]
	}

	// Swap the active identity to the target.
	session.Values["discord_id"] = targetID
	session.Values["username"] = username
	session.Values["display_name"] = displayName
	session.Values["avatar"] = avatar
	session.Values["dev_impersonating"] = true

	if err := session.Save(r, w); err != nil {
		http.Error(w, "Failed to save session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("🕵️ [DEV] %s is now impersonating %s", realID, targetID)
	respondJSON(w, map[string]any{
		"ok":            true,
		"discord_id":    targetID,
		"username":      username,
		"display_name":  displayName,
		"impersonating": true,
	})
}

// handleDevStopImpersonating restores the real login that was active before
// impersonation began.
func handleDevStopImpersonating(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("DEV_MODE") != "true" {
		http.Error(w, "Dev mode disabled", http.StatusForbidden)
		return
	}

	session, _ := store.Get(r, "session")

	originalID, _ := session.Values["dev_original_discord_id"].(string)
	if originalID == "" {
		respondJSON(w, map[string]any{"ok": true, "impersonating": false})
		return
	}

	session.Values["discord_id"] = originalID
	session.Values["username"] = session.Values["dev_original_username"]
	session.Values["display_name"] = session.Values["dev_original_display_name"]
	session.Values["avatar"] = session.Values["dev_original_avatar"]

	delete(session.Values, "dev_original_discord_id")
	delete(session.Values, "dev_original_username")
	delete(session.Values, "dev_original_display_name")
	delete(session.Values, "dev_original_avatar")
	delete(session.Values, "dev_impersonating")

	if err := session.Save(r, w); err != nil {
		http.Error(w, "Failed to save session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("🕵️ [DEV] %s stopped impersonating", originalID)
	respondJSON(w, map[string]any{
		"ok":            true,
		"impersonating": false,
		"discord_id":    originalID,
	})
}
