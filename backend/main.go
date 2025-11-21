package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
		// ✅ Existing record — update username/display if changed
		updates := map[string]any{}
		if existing.Username != user.Username {
			updates["username"] = user.Username
		}
		if existing.DisplayName != serverDisplay {
			updates["display_name"] = serverDisplay
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
	discordID, _ := strconv.ParseInt(discordIDStr, 10, 64)

	var player Player
	err := DB.First(&player, "id = ?", discordID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondJSON(w, map[string]any{
			"registered": false,
			"id":         discordIDStr,
			"username":   session.Values["username"],
			"avatar":     session.Values["avatar"],
			"is_mod":     false,
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
			"is_mod":     false,
		})
		return
	}

	// --- ✅ Discord Role Check for League Mod ---
	isMod := false
	guildID := getEnv("DISCORD_GUILD_ID", "")
	modRoleID := getEnv("DISCORD_LEAGUE_MOD_ROLE_ID", "")
	botToken := getEnv("DISCORD_BOT_TOKEN", "")

	if guildID != "" && modRoleID != "" && botToken != "" {
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
						break
					}
				}
			}
			resp.Body.Close()
		}
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
		"registered": true,
		"id":         discordIDStr,
		"username":   player.Username,
		"role":       player.Role,
		"device":     player.Device,
		"timezone":   player.Timezone,
		"avatar":     session.Values["avatar"],
		"is_mod":     isMod,
		"is_caster":  isCaster,
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

	// ⭐ NEW — Discord role handling
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

	player.Registered = true

	// Log registration
	SendDiscordLog(
		fmt.Sprintf("🟢 **<@%s>** has signed up as a **%s** in timezone **%s**",
			discordIDStr, player.Role, player.Timezone,
		),
	)

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

func main() {
	// ✅ Load .env first
	_ = godotenv.Load()

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

	// Auth routes
	r.HandleFunc("/login", handleLogin).Methods("GET")
	r.HandleFunc("/callback", handleCallback).Methods("GET")
	r.HandleFunc("/logout", LogoutHandler).Methods("GET")
	r.HandleFunc("/api/matches/generate", HandleGenerateWeeklyMatches).Methods("POST")
	r.HandleFunc("/api/match/confirm-schedule", HandleConfirmSchedule).Methods("POST")
	r.HandleFunc("/api/match/confirm-score", HandleConfirmScore).Methods("POST")
	r.HandleFunc("/api/match/schedule", HandleScheduleMatch).Methods("POST")
	r.HandleFunc("/api/match/submit-score", HandleSubmitScore).Methods("POST")
	r.HandleFunc("/api/match/cast", HandleRequestCast).Methods("POST")
	r.HandleFunc("/api/match/confirm-coinflip", HandleConfirmCoinFlip).Methods("POST")
	r.HandleFunc("/api/matches/public", HandlePublicMatches).Methods("GET")
	r.HandleFunc("/api/settings", GetSettings).Methods("GET")
	r.HandleFunc("/api/challenge/request", HandleChallengeRequest).Methods("POST")
	r.HandleFunc("/api/challenge/respond", HandleChallengeRespond).Methods("POST")
	r.HandleFunc("/api/team/toggle-challenges", HandleToggleChallenges).Methods("POST")

	// League Mod routes (all requireLeagueMod inside handlers)
	r.HandleFunc("/api/mod/match/reset", ModMatchReset).Methods("POST")
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
	r.HandleFunc("/api/mod/leaderboard/reset", ModLeaderboardReset).Methods("POST")
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
	r.HandleFunc("/api/team/rename", CaptainRenameTeam).Methods("POST")

	// Players
	api.HandleFunc("/register", handleRegister).Methods("POST")
	api.HandleFunc("/unregister", handleUnregister).Methods("POST")
	api.HandleFunc("/players", GetPlayers).Methods("GET")
	api.HandleFunc("/me", handleMe).Methods("GET")
	api.HandleFunc("/player/{id:[0-9]+}", GetPlayerDetail).Methods("GET")

	// Leaderboards
	api.HandleFunc("/leaderboard/players", GetPlayerLeaderboard).Methods("GET")
	api.HandleFunc("/leaderboard/teams", GetTeamLeaderboard).Methods("GET")

	// STATIC FRONTEND + SPA FALLBACK
	distDir := "../frontend/dist"

	// Serve static assets (JS, CSS, PNG, manifest, etc)
	r.PathPrefix("/assets/").Handler(http.StripPrefix("/", http.FileServer(http.Dir(distDir))))

	// SPA fallback for all non-API routes
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow API routes to 404 through normal handler
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		staticPath := filepath.Join(distDir, r.URL.Path)
		if info, err := os.Stat(staticPath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, staticPath)
			return
		}

		// Fallback → serve React index.html
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

	// HTTP (port 80) listener for Let's Encrypt challenges
	go func() {
		log.Println("🌐 Listening on :80 for ACME HTTP-01 challenges")
		http.ListenAndServe(":80", certManager.HTTPHandler(nil))
	}()

	log.Println("🚀 ECGL running at https://" + host)
	log.Fatal(server.ListenAndServeTLS("", ""))

}
