package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

// ─── Helper: create a notification for a player ───
func createNotification(playerID int64, ntype, title, message, link string) {
	n := Notification{
		PlayerID: playerID,
		Type:     ntype,
		Title:    title,
		Message:  message,
		Link:     link,
	}
	if err := DB.Create(&n).Error; err != nil {
		log.Printf("⚠️ Failed to create notification for player %d: %v", playerID, err)
	}
}

// ─── Helper: notify all captains/co-captains of a team ───
func notifyTeamCaptains(teamID uint, ntype, title, message, link string) {
	var members []TeamMember
	DB.Where("team_id = ? AND (role = 'Captain' OR role = 'Co-Captain')", teamID).Find(&members)
	for _, m := range members {
		createNotification(m.PlayerID, ntype, title, message, link)
	}
}

// ─── GET /api/notifications ───
func HandleGetNotifications(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	var notifications []Notification
	DB.Where("player_id = ?", playerID).Order("created_at DESC").Limit(50).Find(&notifications)

	var unread int64
	DB.Model(&Notification{}).Where("player_id = ? AND read = false", playerID).Count(&unread)

	respondJSON(w, map[string]any{
		"notifications": notifications,
		"unread_count":  unread,
	})
}

// ─── GET /api/notifications/count ───
func HandleGetNotificationCount(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	var unread int64
	DB.Model(&Notification{}).Where("player_id = ? AND read = false", playerID).Count(&unread)

	respondJSON(w, map[string]any{"unread_count": unread})
}

// ─── POST /api/notifications/read ───
func HandleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	var req struct {
		ID uint `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	DB.Model(&Notification{}).Where("id = ? AND player_id = ?", req.ID, playerID).Update("read", true)
	respondJSON(w, map[string]any{"success": true})
}

// ─── POST /api/notifications/read-all ───
func HandleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	discordID, ok := session.Values["discord_id"].(string)
	if !ok || discordID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	playerID, _ := strconv.ParseInt(discordID, 10, 64)

	DB.Model(&Notification{}).Where("player_id = ? AND read = false", playerID).Update("read", true)
	respondJSON(w, map[string]any{"success": true})
}
