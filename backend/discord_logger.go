package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
)

// -------------------------------------------------------------------
// ⚙️ Base function — sends a message to ANY Discord channel
// -------------------------------------------------------------------
func SendDiscordChannelLog(channelID, msg string) {
	botToken := os.Getenv("DISCORD_BOT_TOKEN")

	if channelID == "" || botToken == "" {
		log.Println("⚠️ Discord logging disabled (missing channel ID or bot token)")
		return
	}

	body, _ := json.Marshal(map[string]string{"content": msg})

	req, _ := http.NewRequest(
		"POST",
		"https://discord.com/api/v10/channels/"+channelID+"/messages",
		bytes.NewBuffer(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("❌ Failed sending Discord log:", err)
		return
	}
	resp.Body.Close()
}

// -------------------------------------------------------------------
// 📢 Backwards-compatible function for GENERAL logs
// Uses DISCORD_LOG_CHANNEL_GENERAL instead of the old var
// -------------------------------------------------------------------
func SendDiscordLog(msg string) {
	channelID := os.Getenv("DISCORD_LOG_CHANNEL_GENERAL")
	SendDiscordChannelLog(channelID, msg)
}

// -------------------------------------------------------------------
// 🎮 General Logs (players, teams, join requests, etc.)
// -------------------------------------------------------------------
func LogGeneral(msg string) {
	channelID := os.Getenv("DISCORD_LOG_CHANNEL_GENERAL")
	SendDiscordChannelLog(channelID, msg)
}

// -------------------------------------------------------------------
// 📅 Match Scheduling Logs
// -------------------------------------------------------------------
func LogMatch(msg string) {
	channelID := os.Getenv("DISCORD_LOG_CHANNEL_MATCHES")
	SendDiscordChannelLog(channelID, msg)
}

// -------------------------------------------------------------------
// 📝 Score / Forfeit Logs
// -------------------------------------------------------------------
func LogScore(msg string) {
	channelID := os.Getenv("DISCORD_LOG_CHANNEL_SCORES")
	SendDiscordChannelLog(channelID, msg)
}

// -------------------------------------------------------------------
// 🏷️ Discord Role Management (Add/Remove Roles)
// -------------------------------------------------------------------

func DiscordAddRole(discordID, roleID string) error {
	queueRoleJob(func() {
		botToken := os.Getenv("DISCORD_BOT_TOKEN")
		guildID := os.Getenv("DISCORD_GUILD_ID")

		if botToken == "" || guildID == "" || discordID == "" || roleID == "" {
			log.Println("⚠️ Missing Discord role env vars or IDs")
			return
		}

		url := "https://discord.com/api/v10/guilds/" + guildID +
			"/members/" + discordID + "/roles/" + roleID

		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "ECGL-Bot (RoleSync)")
		req.Header.Set("Authorization", "Bot "+botToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Println("❌ Failed to add Discord role:", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 204 {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("❌ AddRole FAILED: %d %s", resp.StatusCode, body)
		}
	})

	return nil
}

func DiscordRemoveRole(discordID, roleID string) error {
	queueRoleJob(func() {
		botToken := os.Getenv("DISCORD_BOT_TOKEN")
		guildID := os.Getenv("DISCORD_GUILD_ID")

		if botToken == "" || guildID == "" || discordID == "" || roleID == "" {
			log.Println("⚠️ Missing Discord role env vars or IDs")
			return
		}

		url := "https://discord.com/api/v10/guilds/" + guildID +
			"/members/" + discordID + "/roles/" + roleID

		req, _ := http.NewRequest("DELETE", url, nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "ECGL-Bot (RoleSync)")
		req.Header.Set("Authorization", "Bot "+botToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Println("❌ Failed to remove Discord role:", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 204 {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("❌ RemoveRole FAILED: %d %s", resp.StatusCode, body)
		}
	})

	return nil
}
