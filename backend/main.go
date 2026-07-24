package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// --- Utils
func mustGet(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}
func isHTTPS(u string) bool {
	return strings.HasPrefix(strings.ToLower(u), "https://")
}

// --- Globals (but initialized later in main)
var (
	store     *sessions.CookieStore
	oauthConf *oauth2.Config
)

// --- Logger that silences noisy TLS errors
type quietErrorLog struct{}

func (l quietErrorLog) Write(p []byte) (n int, err error) {
	msg := string(p)
	if strings.Contains(msg, "acme/autocert: missing server name") ||
		strings.Contains(msg, "acme/autocert: host") ||
		strings.Contains(msg, "client sent an HTTP request to an HTTPS server") ||
		strings.Contains(msg, "tls:") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "wsarecv") {
		return len(p), nil
	}
	return os.Stderr.Write(p)
}

// --- Login
func handleLogin(w http.ResponseWriter, r *http.Request) {
	url := oauthConf.AuthCodeURL("random-state", oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusFound)
}

// --- Callback
func handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "no code in request", http.StatusBadRequest)
		return
	}

	token, err := oauthConf.Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	client := oauthConf.Client(context.Background(), token)
	resp, err := client.Get("https://discord.com/api/users/@me")
	if err != nil {
		http.Error(w, "failed to get user info", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var user struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
		Avatar     string `json:"avatar"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		http.Error(w, "failed to parse user info", http.StatusInternalServerError)
		return
	}

	// ✅ Fetch guild member info to get the nickname / display name used in the ECGL Discord
	guildID := getEnv("DISCORD_GUILD_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")
	serverDisplay := user.GlobalName // fallback

	if guildID != "" && botToken != "" {
		req, _ := http.NewRequest("GET",
			fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, user.ID),
			nil)
		req.Header.Set("Authorization", "Bot "+botToken)

		if resp2, err := http.DefaultClient.Do(req); err == nil && resp2.StatusCode == 200 {
			var member struct {
				Nick string `json:"nick"`
			}
			if json.NewDecoder(resp2.Body).Decode(&member) == nil && member.Nick != "" {
				serverDisplay = member.Nick
			}
			resp2.Body.Close()
		}
	}

	// --- Save session ---
	session, _ := store.Get(r, "session")
	session.Values["discord_id"] = user.ID
	session.Values["username"] = user.Username
	session.Values["display_name"] = serverDisplay // ✅ now stores the guild nickname
	session.Values["avatar"] = user.Avatar
	_ = session.Save(r, w)

	// --- Auto-link or create player record ---
	discordID, _ := strconv.ParseInt(user.ID, 10, 64)

	var existing Player
	err = DB.First(&existing, "id = ?", discordID).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 🆕 Create placeholder record for new user
		newPlayer := Player{
			ID:          discordID,
			Username:    user.Username,
			DisplayName: serverDisplay,
			Avatar:      user.Avatar,
			Role:        "",  // not registered yet
			Timezone:    "",  // empty until registration
			Rating:      800, // default starting ELO
			Wins:        0,
			Losses:      0,
			Matches:     0,
		}
		if err := DB.Create(&newPlayer).Error; err != nil {
			log.Printf("❌ Failed to create new player record for %s (%s): %v", user.Username, user.ID, err)
		} else {
			log.Printf("🆕 Created new player record for %s (%s)", user.Username, user.ID)
		}
	} else if err == nil {
		// ✅ Existing record — update username/display/avatar if changed
		updates := map[string]any{}
		if existing.Username != user.Username {
			updates["username"] = user.Username
		}
		if existing.DisplayName != serverDisplay {
			updates["display_name"] = serverDisplay
		}
		if existing.Avatar != user.Avatar {
			updates["avatar"] = user.Avatar
		}
		if len(updates) > 0 {
			DB.Model(&existing).Updates(updates)
			log.Printf("🔄 Updated player info for %s (%s)", user.Username, user.ID)
		} else {
			log.Printf("🔗 Linked existing player record for %s (%s)", user.Username, user.ID)
		}
	} else {
		log.Printf("⚠️ Error checking player record for %s (%s): %v", user.Username, user.ID, err)
	}

	// --- Redirect to frontend ---
	frontend := mustGet("FRONTEND_URL", "http://localhost:5173")
	http.Redirect(w, r, frontend, http.StatusSeeOther)
}

// --- Me
func handleMe(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	discordIDStr, ok := session.Values["discord_id"].(string)
	if !ok || discordIDStr == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// DEV MODE impersonation override
	if os.Getenv("DEV_MODE") == "true" {
		if overrideID := r.URL.Query().Get("as"); overrideID != "" {
			discordIDStr = overrideID
		}
	}

	discordID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	// --- ✅ Discord Role Check for League Mod (check early, before registration check) ---
	isMod := false
	isDev := false
	guildID := getEnv("DISCORD_GUILD_ID", "")
	modRoleID := getEnv("DISCORD_LEAGUE_MOD_ROLE_ID", "")
	devRoleID := getEnv("DISCORD_DEV_ROLE_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")

	if guildID != "" && botToken != "" {
		req, _ := http.NewRequest("GET",
			fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, discordIDStr),
			nil)
		req.Header.Set("Authorization", "Bot "+botToken)

		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == 200 {
			var member struct {
				Roles []string `json:"roles"`
			}
			if json.NewDecoder(resp.Body).Decode(&member) == nil {
				for _, role := range member.Roles {
					if role == modRoleID {
						isMod = true
					}
					if role == devRoleID {
						isDev = true
					}
				}
			}
			resp.Body.Close()
		}
	}

	var player Player
	err := DB.First(&player, "id = ?", discordID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondJSON(w, map[string]any{
			"registered": false,
			"id":         discordIDStr,
			"username":   session.Values["username"],
			"avatar":     session.Values["avatar"],
			"is_mod":     isMod,
			"is_dev":     isDev,
		})
		return
	}

	// ❌ row exists but registration cleared
	if player.Role == "" || player.Device == "" || player.Timezone == "" {
		respondJSON(w, map[string]any{
			"registered": false,
			"id":         discordIDStr,
			"username":   session.Values["username"],
			"avatar":     session.Values["avatar"],
			"is_mod":     isMod,
			"is_dev":     isDev,
		})
		return
	}

	casterRoleID := getEnv("DISCORD_CASTER_ROLE_ID", "")

	isCaster := false
	if guildID != "" && botToken != "" && casterRoleID != "" {
		req, _ := http.NewRequest("GET",
			fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, discordIDStr),
			nil)
		req.Header.Set("Authorization", "Bot "+botToken)

		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == 200 {
			var member struct {
				Roles []string `json:"roles"`
			}
			if json.NewDecoder(resp.Body).Decode(&member) == nil {
				for _, role := range member.Roles {
					if role == casterRoleID {
						isCaster = true
						break
					}
				}
			}
			resp.Body.Close()
		}
	}

	// ✅ active registered player + mod info
	respondJSON(w, map[string]any{
		"registered":   true,
		"id":           discordIDStr,
		"username":     player.Username,
		"display_name": session.Values["display_name"],
		"role":         player.Role,
		"device":       player.Device,
		"timezone":     player.Timezone,
		"avatar":       session.Values["avatar"],
		"is_mod":       isMod,
		"is_dev":       isDev,
		"is_caster":    isCaster,
		"dev_mode":     os.Getenv("DEV_MODE") == "true",
	})
}

// --- Registration
func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Device   string `json:"device"`
		Timezone string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// ✅ Always get the Discord ID from session
	session, _ := store.Get(r, "session")
	discordIDStr, ok := session.Values["discord_id"].(string)
	if !ok || discordIDStr == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	discordID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	guildID := getEnv("DISCORD_GUILD_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")

	if guildID == "" || botToken == "" {
		http.Error(w, "Server misconfigured: missing guild settings", http.StatusInternalServerError)
		return
	}

	// Discord API: check if user is in the guild
	checkURL := fmt.Sprintf(
		"https://discord.com/api/v10/guilds/%s/members/%s",
		guildID, discordIDStr,
	)

	reqCheck, _ := http.NewRequest("GET", checkURL, nil)
	reqCheck.Header.Set("Authorization", "Bot "+botToken)

	respCheck, err := http.DefaultClient.Do(reqCheck)
	if err != nil {
		http.Error(w, "Discord check failed — try again.", http.StatusServiceUnavailable)
		return
	}
	defer respCheck.Body.Close()

	// 404 = NOT IN SERVER → BLOCK REGISTRATION
	if respCheck.StatusCode == 404 {
		respondJSON(w, map[string]any{
			"error":        true,
			"message":      "You must join the official ECGL Discord server before registering.",
			"need_discord": true,
		})
		return
	}

	// Anything except 200 = FAIL
	if respCheck.StatusCode != 200 {
		http.Error(w, "Unable to verify Discord membership.", http.StatusForbidden)
		return
	}

	if req.Device == "quest_native" {
		http.Error(w, "Echo Combat is only available on PC (Rift or Quest+Link)", http.StatusForbidden)
		return
	}

	switch strings.ToLower(req.Role) {
	case "player":
		req.Role = "Player"
	case "league sub":
		req.Role = "League Sub"
	default:
		http.Error(w, "Invalid role", http.StatusBadRequest)
		return
	}

	player := Player{
		ID:          discordID,
		Username:    req.Username,
		DisplayName: session.Values["display_name"].(string),
		Role:        req.Role,
		Device:      req.Device,
		Timezone:    req.Timezone,
	}

	// ✅ Default rating
	if player.Rating == 0 {
		player.Rating = getEnvInt("DEFAULT_PLAYER_RATING", 800)
	}

	// 🛡️ Detect if this is a real signup vs a duplicate submit.
	// If the player already exists with the same role/device/timezone,
	// treat it as a no-op so we don't double-ping Discord or re-apply roles.
	var existing Player
	alreadyRegistered := false
	if err := DB.First(&existing, discordID).Error; err == nil {
		if strings.EqualFold(existing.Role, req.Role) &&
			strings.EqualFold(existing.Device, req.Device) &&
			strings.EqualFold(existing.Timezone, req.Timezone) &&
			strings.TrimSpace(existing.Role) != "" {
			alreadyRegistered = true
		}
	}

	// ✅ Upsert player row
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"username", "role", "device", "timezone"}),
	}).Create(&player).Error; err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok {
			log.Printf("❌ Postgres error: Code=%s Detail=%s Message=%s", pgErr.Code, pgErr.Detail, pgErr.Message)
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	DB.First(&player, discordID)

	if !alreadyRegistered {
		// ⭐ Discord role handling — only on first signup or role change
		rolePlayer := os.Getenv("DISCORD_PLAYER_ROLE_ID")
		roleSub := os.Getenv("DISCORD_LEAGUE_SUB_ROLE_ID")

		// Cleanup previous roles
		go DiscordRemoveRole(discordIDStr, rolePlayer)
		go DiscordRemoveRole(discordIDStr, roleSub)

		// Assign correct role
		if req.Role == "Player" {
			go DiscordAddRole(discordIDStr, rolePlayer)
		}
		if req.Role == "League Sub" {
			go DiscordAddRole(discordIDStr, roleSub)
		}

		// Log registration
		SendDiscordLog(
			fmt.Sprintf("🟢 **<@%s>** has signed up as a **%s** in timezone **%s**",
				discordIDStr, player.Role, player.Timezone,
			),
		)
	}

	player.Registered = true

	respondJSON(w, player)
}

func handleUnregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// get Discord ID
	session, _ := store.Get(r, "session")
	discordIDStr, ok := session.Values["discord_id"].(string)
	if !ok || discordIDStr == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	discordID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	// Discord role IDs
	rolePlayer := os.Getenv("DISCORD_PLAYER_ROLE_ID")
	roleSub := os.Getenv("DISCORD_LEAGUE_SUB_ROLE_ID")
	roleCaptain := os.Getenv("DISCORD_CAPTAIN_ROLE_ID")
	roleCoCaptain := os.Getenv("DISCORD_CO_CAPTAIN_ROLE_ID")

	// load memberships
	var memberships []TeamMember
	if err := DB.Where("player_id = ?", discordID).Find(&memberships).Error; err == nil {
		for _, m := range memberships {

			role := m.Role

			var team Team
			DB.First(&team, m.TeamID)

			// log history
			DB.Create(&PlayerHistory{
				PlayerID: discordID,
				TeamID:   m.TeamID,
				TeamName: team.Name,
				Role:     "Unregistered (" + role + ")",
				Season:   currentSeason,
			})

			// ⭐ Remove Discord Captain / Co-Captain roles when unregistering
			if role == "Captain" {
				go DiscordRemoveRole(discordIDStr, roleCaptain)
			}
			if role == "Co-Captain" {
				go DiscordRemoveRole(discordIDStr, roleCoCaptain)
			}

			// remove from team
			DB.Delete(&TeamMember{}, "player_id = ? AND team_id = ?", discordID, m.TeamID)

			// check remaining members
			var remaining []TeamMember
			DB.Where("team_id = ?", m.TeamID).Find(&remaining)

			// auto-disband if empty
			if len(remaining) == 0 {
				DB.Delete(&Team{}, m.TeamID)

				SendDiscordLog(fmt.Sprintf(
					"🗑️ **Team Disbanded:** **%s (#%d)** — last member unregistered",
					team.Name, team.ID,
				))
				continue
			}

			// ⭐ CAPTAIN LEFT → AUTO PROMOTION (and Discord role updates)
			if role == "Captain" {

				var next TeamMember

				// Promote Co-Captain first
				if err := DB.Where("team_id = ? AND role = ?", m.TeamID, "Co-Captain").
					First(&next).Error; err == nil {

					DB.Model(&next).Update("role", "Captain")

					nextDiscordID := strconv.FormatInt(next.PlayerID, 10)

					// Discord role changes
					go DiscordAddRole(nextDiscordID, roleCaptain)
					go DiscordRemoveRole(nextDiscordID, roleCoCaptain)

				} else if err := DB.Where("team_id = ?", m.TeamID).
					First(&next).Error; err == nil {

					// No co-captain, promote first member
					DB.Model(&next).Update("role", "Captain")

					nextDiscordID := strconv.FormatInt(next.PlayerID, 10)

					go DiscordAddRole(nextDiscordID, roleCaptain)
					go DiscordRemoveRole(nextDiscordID, roleCoCaptain)
				}
			}
		}
	}

	// ❌ Clear pending join requests
	DB.Where("player_id = ?", discordID).Delete(&TeamJoinRequest{})

	// Reset registration fields (keep stats)
	if err := DB.Model(&Player{}).Where("id = ?", discordID).
		Updates(map[string]any{
			"role":     "",
			"device":   "",
			"timezone": "",
		}).Error; err != nil {
		log.Printf("❌ Failed to clear player registration: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// ⭐ Remove Player / Sub roles after unregister
	go DiscordRemoveRole(discordIDStr, rolePlayer)
	go DiscordRemoveRole(discordIDStr, roleSub)

	// log
	SendDiscordLog(fmt.Sprintf("🔴 <@%s> has left the league", discordIDStr))

	respondJSON(w, map[string]any{
		"success":    true,
		"registered": false,
		"message":    "Unregistered successfully (removed from teams, disbanded empty teams, reassigned captain if needed, logged history, kept stats)",
	})
}

// --- Get current season ---
func HandleGetSeason(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, map[string]any{
		"season": currentSeason,
	})
}

var currentSeason string
var rosterLocked bool = false

// syncPlayerAvatars runs once at startup to backfill avatar/username/display_name
// for all players using the Discord bot's guild member list.
func syncPlayerAvatars(dg *discordgo.Session) {
	guildID := os.Getenv("DISCORD_GUILD_ID")
	if guildID == "" {
		log.Println("⚠️ DISCORD_GUILD_ID not set, skipping avatar sync")
		return
	}

	// Get all player IDs from DB
	var playerIDs []int64
	if err := DB.Table("players").Pluck("id", &playerIDs).Error; err != nil {
		log.Printf("⚠️ Avatar sync: failed to load player IDs: %v", err)
		return
	}

	if len(playerIDs) == 0 {
		return
	}

	log.Printf("🔄 Avatar sync: checking %d players...", len(playerIDs))
	updated := 0

	for _, pid := range playerIDs {
		memberID := strconv.FormatInt(pid, 10)
		member, err := dg.GuildMember(guildID, memberID)
		if err != nil {
			continue // user may have left the guild
		}
		if member.User == nil {
			continue
		}

		updates := map[string]any{}

		if member.User.Avatar != "" {
			updates["avatar"] = member.User.Avatar
		}
		if member.User.Username != "" {
			updates["username"] = member.User.Username
		}
		// Use guild nick > global display name > username
		displayName := member.Nick
		if displayName == "" && member.User.GlobalName != "" {
			displayName = member.User.GlobalName
		}
		if displayName != "" {
			updates["display_name"] = displayName
		}

		if len(updates) > 0 {
			DB.Table("players").Where("id = ?", pid).Updates(updates)
			updated++
		}
	}

	log.Printf("✅ Avatar sync complete: updated %d/%d players", updated, len(playerIDs))
}

func main() {
	// ✅ Load .env first
	_ = godotenv.Load()
	rand.Seed(time.Now().UnixNano())

	// ✅ Set current season from env (with fallback)
	currentSeason = os.Getenv("CURRENT_SEASON")
	if currentSeason == "" {
		currentSeason = "S1" // default if missing
	}
	log.Printf("📅 Current season: %s", currentSeason)

	// ✅ Initialize session store after env
	frontend := mustGet("FRONTEND_URL", "https://gigglesquad.mooo.com")
	secret := mustGet("SESSION_SECRET", "supersecretkey")
	store = sessions.NewCookieStore([]byte(secret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		SameSite: http.SameSiteNoneMode,
		Secure:   isHTTPS(frontend),
	}

	// ✅ Initialize OAuth config after env
	oauthConf = &oauth2.Config{
		RedirectURL:  mustGet("OAUTH_REDIRECT", frontend+"/callback"),
		ClientID:     mustGet("DISCORD_CLIENT_ID", ""),
		ClientSecret: mustGet("DISCORD_CLIENT_SECRET", ""),
		Scopes:       []string{"identify"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://discord.com/api/oauth2/authorize",
			TokenURL: "https://discord.com/api/oauth2/token",
		},
	}
	log.Printf("🔑 Discord ClientID=%q", oauthConf.ClientID)

	// ✅ DB
	InitDB()

	// =====================================================
	// DISCORD BOT (PREFIX COMMANDS)
	// =====================================================
	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("❌ DISCORD_BOT_TOKEN not set")
	}

	dg, err := discordgo.New("Bot " + botToken)
	if err != nil {
		log.Fatalf("❌ Discord session error: %v", err)
	}

	// REQUIRED for !commands
	dg.Identify.Intents =
		discordgo.IntentsGuilds |
			discordgo.IntentsGuildMessages |
			discordgo.IntentsMessageContent

	// 🔹 REGISTER PREFIX COMMAND HANDLERS HERE
	RegisterPrefixCommands(dg)

	// 🔹 REGISTER SLASH COMMAND HANDLERS (commands registered after Open)
	RegisterSlashHandlers(dg)

	// 🔹 REGISTER BUTTON INTERACTION HANDLERS
	RegisterCloseChannelHandler(dg)

	// Open Discord gateway
	if err := dg.Open(); err != nil {
		log.Fatalf("❌ Failed to connect to Discord: %v", err)
	}

	defer dg.Close()

	log.Println("🤖 Discord bot connected (prefix commands enabled)")

	// 🔹 REGISTER SLASH COMMANDS (requires active connection)
	RegisterSlashCommands(dg)

	// 🔹 START MATCH CHANNEL SCHEDULER
	StartMatchChannelScheduler(dg)

	// 🔄 One-time sync: backfill avatars for all players from Discord
	go syncPlayerAvatars(dg)

	r := mux.NewRouter()

	// ✅ CORS Middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			frontend := mustGet("FRONTEND_URL", "https://gigglesquad.mooo.com")
			frontendDev := mustGet("FRONTEND_URL_DEV", "http://localhost:5173")
			origin := r.Header.Get("Origin")
			if origin == frontend || origin == frontendDev {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})

	// Cast system routes
	r.HandleFunc("/api/match/cast", HandleSetCast).Methods("POST")
	r.HandleFunc("/api/match/cast/get/{id}", HandleGetCast).Methods("GET")
	r.HandleFunc("/api/match/cast/delete", HandleDeleteCast).Methods("POST")
	r.HandleFunc("/api/match/cast/request", HandleRequestCast).Methods("POST")

	// Auth routes
	r.HandleFunc("/login", handleLogin).Methods("GET")
	r.HandleFunc("/callback", handleCallback).Methods("GET")
	r.HandleFunc("/logout", LogoutHandler).Methods("GET")
	r.HandleFunc("/api/matches/generate", HandleGenerateWeeklyMatches).Methods("POST")
	r.HandleFunc("/api/match/confirm-schedule", HandleConfirmSchedule).Methods("POST")
	r.HandleFunc("/api/match/confirm-score", HandleConfirmScore).Methods("POST")
	r.HandleFunc("/api/match/schedule", HandleScheduleMatch).Methods("POST")
	r.HandleFunc("/api/match/submit-score", HandleSubmitScore).Methods("POST")
	r.HandleFunc("/api/match/confirm-coinflip", HandleConfirmCoinFlip).Methods("POST")
	r.HandleFunc("/api/overlay/match/{id:[0-9]+}", HandleOverlayMatch).Methods("GET")
	r.HandleFunc("/api/matches/public", HandlePublicMatches).Methods("GET")
	r.HandleFunc("/api/settings", GetSettings).Methods("GET")
	r.HandleFunc("/api/challenge/request", HandleChallengeRequest).Methods("POST")
	r.HandleFunc("/api/challenge/respond", HandleChallengeRespond).Methods("POST")
	r.HandleFunc("/api/team/toggle-challenges", HandleToggleChallenges).Methods("POST")
	r.HandleFunc("/api/team/rename", CaptainRenameTeam).Methods("POST")
	r.HandleFunc("/api/team/logo", HandleUploadTeamLogo).Methods("POST")
	r.HandleFunc("/api/team/logo/{teamID:[0-9]+}", HandleGetTeamLogo).Methods("GET")
	r.HandleFunc("/api/team/logo/{teamID:[0-9]+}/{version}", HandleGetTeamLogo).Methods("GET")
	r.HandleFunc("/api/season/calendar", HandleGetSeasonCalendar).Methods("GET")

	// Notifications
	r.HandleFunc("/api/notifications", HandleGetNotifications).Methods("GET")
	r.HandleFunc("/api/notifications/count", HandleGetNotificationCount).Methods("GET")
	r.HandleFunc("/api/notifications/read", HandleMarkNotificationRead).Methods("POST")
	r.HandleFunc("/api/notifications/read-all", HandleMarkAllNotificationsRead).Methods("POST")

	// League Mod routes (all requireLeagueMod inside handlers)
	r.HandleFunc("/api/mod/audit-logs", HandleModAuditLogs).Methods("GET")
	r.HandleFunc("/api/mod/audit-logs", HandleClearModAuditLogs).Methods("DELETE")
	r.HandleFunc("/api/mod/match/reset", ModMatchReset).Methods("POST")
	r.HandleFunc("/api/mod/match/reset-schedule", ModResetMatchSchedule).Methods("POST")
	r.HandleFunc("/api/mod/match/forfeit", ModMatchForfeit).Methods("POST")
	r.HandleFunc("/api/mod/match/double-forfeit", ModMatchDoubleForfeit).Methods("POST")
	r.HandleFunc("/api/mod/match", ModMatchDelete).Methods("DELETE")
	r.HandleFunc("/api/mod/match/add", HandleModAddMatch).Methods("POST")
	r.HandleFunc("/api/mod/match/set-maps", HandleModSetMaps).Methods("POST")
	r.HandleFunc("/api/mod/match/delete", HandleModDeleteMatch).Methods("POST")
	r.HandleFunc("/api/mod/match/schedule", ModForceSchedule).Methods("POST")
	r.HandleFunc("/api/mod/matches/generate", HandleGenerateWeeklyMatches).Methods("POST")
	r.HandleFunc("/api/mod/team/set-active", ModSetTeamActive).Methods("POST")
	r.HandleFunc("/api/mod/teams/set-all-active", ModSetAllTeamsActive).Methods("POST")
	r.HandleFunc("/api/mod/team/set-inactive", HandleModSetTeamInactive).Methods("POST")
	r.HandleFunc("/api/mod/teams/set-all-inactive", HandleModSetAllTeamsInactive).Methods("POST")
	r.HandleFunc("/api/mod/team/rename", ModTeamRename).Methods("POST")
	r.HandleFunc("/api/mod/team/adjust-rating", ModTeamAdjustRating).Methods("POST")
	r.HandleFunc("/api/mod/team/disband", ModTeamDisband).Methods("POST")
	r.HandleFunc("/api/mod/player/kick", ModPlayerKick).Methods("POST")
	r.HandleFunc("/api/mod/player/ban", ModPlayerBan).Methods("POST")
	r.HandleFunc("/api/mod/player/unban", ModPlayerUnban).Methods("POST")
	r.HandleFunc("/api/mod/team/delete", HandleModDeleteTeam).Methods("POST")
	r.HandleFunc("/api/mod/leaderboard/reset", HandleResetTeamLeaderboard).Methods("POST")
	r.HandleFunc("/api/mod/reset_player_leaderboard", HandleResetPlayerLeaderboard).Methods("POST")
	r.HandleFunc("/api/mod/match/edit-score", ModMatchEditScore).Methods("POST")
	r.HandleFunc("/api/mod/season/archive", ModSeasonArchive).Methods("POST")
	r.HandleFunc("/api/mod/matches/preview", HandlePreviewWeeklyMatches).Methods("GET")
	r.HandleFunc("/api/mod/matches/clear-week", HandleModClearWeek).Methods("POST")
	r.HandleFunc("/api/team/toggle-status", HandleToggleTeamStatus).Methods("POST")
	r.HandleFunc("/api/team/toggle-join", HandleToggleTeamJoinAllowed).Methods("POST")
	r.HandleFunc("/api/mod/team/history", ModGetTeamHistory).Methods("GET")
	r.HandleFunc("/api/mod/roster/lock-all", ModRosterLockAll).Methods("POST")
	r.HandleFunc("/api/mod/roster/unlock-all", ModRosterUnlockAll).Methods("POST")
	r.HandleFunc("/api/mod/roster/status", GetRosterLockStatus).Methods("GET")
	r.HandleFunc("/api/mod/team/history", ModTeamHistory).Methods("GET")
	r.HandleFunc("/api/mod/team/add-player", ModAddPlayerToTeam).Methods("POST")
	r.HandleFunc("/api/mod/sync-roles", HandleModSyncRoles).Methods("POST")
	r.HandleFunc("/api/mod/challenges/enable", HandleEnableGlobalChallenges).Methods("POST")
	r.HandleFunc("/api/mod/challenges/disable", HandleDisableGlobalChallenges).Methods("POST")
	r.HandleFunc("/api/mod/team/adjust-stats", HandleModAdjustTeamStats).Methods("POST")
	r.HandleFunc("/api/mod/team/stats", HandleModGetTeamStats).Methods("GET")
	r.HandleFunc("/api/mod/player/adjust-stats", HandleModAdjustPlayerStats).Methods("POST")
	r.HandleFunc("/api/mod/player/stats", HandleModGetPlayerStats).Methods("GET")
	r.HandleFunc("/api/mod/team/members", HandleModGetTeamMembers).Methods("GET")
	r.HandleFunc("/api/mod/team/set-role", HandleModSetTeamRole).Methods("POST")
	r.HandleFunc("/api/mod/team/promote-captain", HandleModPromoteToCaptain).Methods("POST")
	r.HandleFunc("/api/mod/team/lock", HandleModToggleTeamLock).Methods("POST")
	r.HandleFunc("/api/mod/player/remove-cooldown", ModRemoveCooldown).Methods("POST")
	r.HandleFunc("/api/mod/player/archive-all", HandleArchiveAllPlayers).Methods("POST")
	r.HandleFunc("/api/mod/settings", HandleGetSettings).Methods("GET")
	r.HandleFunc("/api/mod/settings", HandleUpdateSettings).Methods("POST")
	r.HandleFunc("/api/rules", HandleGetRules).Methods("GET")
	r.HandleFunc("/api/mod/rules", HandleSaveRules).Methods("POST")
	r.HandleFunc("/api/tools/archive-team-stats", HandleArchiveTeamStats).Methods("POST")
	r.HandleFunc("/api/mod/import-preseason-archive", HandleImportPreseasonArchive).Methods("POST")
	r.HandleFunc("/api/team/archive", HandleGetTeamArchive).Methods("GET")
	r.HandleFunc("/api/check-discord", HandleCheckDiscordMembership).Methods("GET")
	r.HandleFunc("/api/discord/info", HandleGetDiscordServerInfo).Methods("GET")
	r.HandleFunc("/api/discord/server-info", HandleGetDiscordServerInfo).Methods("GET")
	r.HandleFunc("/api/mod/finals/archive", HandleArchiveFinals).Methods("POST")
	r.HandleFunc("/api/finals/archive", HandleGetFinalsArchive).Methods("GET")

	// Clips (highlight montage)
	r.HandleFunc("/api/clips", HandleGetClips).Methods("GET")
	r.HandleFunc("/api/mod/clips", HandleAddClip).Methods("POST")
	r.HandleFunc("/api/mod/clips/delete", HandleDeleteClip).Methods("POST")
	r.HandleFunc("/api/mod/clips/reorder", HandleReorderClips).Methods("POST")
	r.HandleFunc("/api/mod/clips/sync-playlist", HandleSyncPlaylist).Methods("POST")

	// Subrouter for /api
	api := r.PathPrefix("/api").Subrouter()

	// Matches
	api.HandleFunc("/match/schedule", HandleScheduleMatch).Methods("POST")
	api.HandleFunc("/match/score", HandleSubmitScore).Methods("POST")
	api.HandleFunc("/match/{id:[0-9]+}", HandleGetMatch).Methods("GET")
	api.HandleFunc("/matches/team/{teamID:[0-9]+}", HandleGetTeamMatches).Methods("GET")
	api.HandleFunc("/matches/player/{playerID:[0-9]+}", HandleGetPlayerMatches).Methods("GET")
	api.HandleFunc("/matches", GetMatches).Methods("GET")
	api.HandleFunc("/season", HandleGetSeason).Methods("GET")
	api.HandleFunc("/matches/generate", HandleGenerateWeeklyMatches).Methods("POST")

	// Teams & MyTeam
	api.HandleFunc("/team/leave", HandleLeaveTeam).Methods("POST")
	api.HandleFunc("/myteam", GetMyTeam).Methods("GET") // ✅ session-based only
	api.HandleFunc("/teams", GetTeams).Methods("GET")
	api.HandleFunc("/team/{id:[0-9]+}", GetTeam).Methods("GET")
	api.HandleFunc("/team/request", handleRequestJoinTeam).Methods("POST")
	api.HandleFunc("/team/create", handleCreateTeam).Methods("POST")
	api.HandleFunc("/team/{teamID:[0-9]+}/joinrequests", GetTeamJoinRequests).Methods("GET")
	api.HandleFunc("/team/join/decision", HandleJoinRequestDecision).Methods("POST")
	api.HandleFunc("/team/kick", HandleKickMember).Methods("POST")
	api.HandleFunc("/team/promote", HandlePromoteMember).Methods("POST")
	api.HandleFunc("/match/ping-sub", HandleMatchPingSub).Methods("POST")
	r.HandleFunc("/api/team/reset_challenges", HandleResetTeamChallenges).Methods("POST")

	// Players
	api.HandleFunc("/register", handleRegister).Methods("POST")
	api.HandleFunc("/unregister", handleUnregister).Methods("POST")
	api.HandleFunc("/players", GetPlayers).Methods("GET")
	api.HandleFunc("/me", handleMe).Methods("GET")
	api.HandleFunc("/player/{id:[0-9]+}", GetPlayerDetail).Methods("GET")

	// Leaderboards
	api.HandleFunc("/leaderboard/players", GetPlayerLeaderboard).Methods("GET")
	api.HandleFunc("/leaderboard/teams", GetTeamLeaderboard).Methods("GET")
	api.HandleFunc("/leaderboard/seasons", GetLeaderboardSeasons).Methods("GET")
	api.HandleFunc("/leaderboard/teams/history", GetTeamLeaderboardBySeason).Methods("GET")
	api.HandleFunc("/leaderboard/players/history", GetPlayerLeaderboardBySeason).Methods("GET")

	// --- Finals Visibility ---
	api.HandleFunc("/finals/visible", HandleGetFinalsVisible).Methods("GET")
	api.HandleFunc("/mod/finals/toggle-visible", HandleModToggleFinalsVisible).Methods("POST")

	// --- Finals (public) ---
	api.HandleFunc("/finals/teams", HandleGetFinalsTeams).Methods("GET")
	api.HandleFunc("/finals/bracket", HandleGetFinalsBracket).Methods("GET")

	// --- Finals (mod tools) ---
	api.HandleFunc("/mod/finals/add-team", HandleModFinalsAddTeam).Methods("POST")
	api.HandleFunc("/mod/finals/remove-team", HandleModFinalsRemoveTeam).Methods("POST")
	api.HandleFunc("/mod/finals/generate", HandleModFinalsGenerate).Methods("POST")
	api.HandleFunc("/mod/finals/generate-empty", HandleModFinalsGenerateEmpty).Methods("POST")
	api.HandleFunc("/mod/finals/assign-slot", HandleModFinalsAssignSlot).Methods("POST")
	api.HandleFunc("/mod/finals/set-winner", HandleModFinalsSetWinner).Methods("POST")
	api.HandleFunc("/mod/finals/reset", HandleModFinalsReset).Methods("POST")
	api.HandleFunc("/mod/finals/update-match", HandleModFinalsUpdateMatch).Methods("POST")
	api.HandleFunc("/mod/finals/clear-bracket-view", HandleModFinalsClearBracketView).Methods("POST")
	api.HandleFunc("/mod/finals/set-seeds", HandleModFinalsSetSeeds).Methods("POST")

	// STATIC FRONTEND + SPA FALLBACK
	distDir := "../frontend/dist"

	// Serve static assets (JS, CSS, PNG, manifest, etc)
	r.PathPrefix("/assets/").Handler(http.StripPrefix("/", http.FileServer(http.Dir(distDir))))

	// Serve uploaded assets (team logos, etc)
	uploadsDir := mustGet("UPLOADS_DIR", "uploads")
	r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsDir))))

	// SPA fallback for all non-API routes
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow API routes to 404 through normal handler
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		staticPath := filepath.Join(distDir, r.URL.Path)
		if info, err := os.Stat(staticPath); err == nil && !info.IsDir() {
			// Avoid aggressive caching for the service worker.
			if r.URL.Path == "/sw.js" {
				w.Header().Set("Cache-Control", "no-store, must-revalidate")
				w.Header().Set("Service-Worker-Allowed", "/")
			}
			// Avoid stale HTML (iOS Safari can cache index.html hard).
			if strings.HasSuffix(strings.ToLower(r.URL.Path), ".html") {
				w.Header().Set("Cache-Control", "no-store, must-revalidate")
			}
			http.ServeFile(w, r, staticPath)
			return
		}

		// Fallback → serve React index.html
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
	})

	// TLS SETUP (autocert / Let's Encrypt)
	host := mustGet("TLS_HOST", "ecgleague.com")

	certManager := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache("certs"),
		HostPolicy: autocert.HostWhitelist(host, "www."+host),
	}

	server := &http.Server{
		Addr:    ":443",
		Handler: r,
		TLSConfig: &tls.Config{
			GetCertificate: certManager.GetCertificate,
			NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
			MinVersion:     tls.VersionTLS12,
		},
		ErrorLog: log.New(quietErrorLog{}, "", 0),
	}

	// HTTP (port 80) listener for Let's Encrypt challenge
	go func() {
		log.Println("🌐 Listening on :80 for ACME HTTP-01 challenges")
		http.ListenAndServe(":80", certManager.HTTPHandler(nil))
	}()

	log.Println("🚀 ECGL running at https://" + host)
	log.Fatal(server.ListenAndServeTLS("", ""))

}
