package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Rookie draft: players who want to play in the Rookie League but don't have a
// team can enter the draft from the Register tab. Mods then either assign them
// to an existing rookie team or create a brand new team for them.

const (
	draftPending   = "Pending"
	draftAssigned  = "Assigned"
	draftWithdrawn = "Withdrawn"
)

// Draft kinds: the normal player draft, or the separate coach draft.
const (
	draftKindPlayer = "player"
	draftKindCoach  = "coach"
)

// draftKindOf normalises a stored/requested draft kind.
func draftKindOf(kind string) string {
	if strings.EqualFold(strings.TrimSpace(kind), draftKindCoach) {
		return draftKindCoach
	}
	return draftKindPlayer
}

// draftTeamRole is the team_members role a draft entry gets when placed.
func draftTeamRole(kind string) string {
	if draftKindOf(kind) == draftKindCoach {
		return "Coach"
	}
	return "Member"
}

// draftPlayerID returns the logged-in player's Discord ID from the session.
func draftPlayerID(r *http.Request) (int64, bool) {
	session, _ := store.Get(r, "session")
	idStr, ok := session.Values["discord_id"].(string)
	if !ok || strings.TrimSpace(idStr) == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// playerOnTeamInLeague reports whether the player currently has a team in the
// given league.
func playerOnTeamInLeague(playerID int64, league string) bool {
	var count int64
	DB.Raw(`
		SELECT COUNT(*) FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.player_id = ? AND t.league = ?
	`, playerID, normalizeLeague(league)).Scan(&count)
	return count > 0
}

// resolveDraftSignup marks a pending draft signup as assigned when the player
// joins a team through another path (invite accept, mod add, team create).
func resolveDraftSignup(playerID int64, league string, teamID uint) {
	if err := DB.Model(&DraftSignup{}).
		Where("player_id = ? AND league = ? AND status = ?", playerID, normalizeLeague(league), draftPending).
		Updates(map[string]any{"status": draftAssigned, "team_id": teamID}).Error; err != nil {
		fmt.Printf("⚠️ resolveDraftSignup(%d, %s): %v\n", playerID, league, err)
	}
}

func draftPlayerLabel(p Player) string {
	if strings.TrimSpace(p.DisplayName) != "" {
		return p.DisplayName
	}
	return p.Username
}

// --- Player endpoints -------------------------------------------------------

// GET /api/draft/status
func HandleGetDraftStatus(w http.ResponseWriter, r *http.Request) {
	playerID, ok := draftPlayerID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	lg := leagueFromRequest(r)

	var s DraftSignup
	found := DB.Where("player_id = ? AND league = ?", playerID, lg).First(&s).Error == nil

	resp := map[string]any{
		"signed_up":             false,
		"has_team":              playerOnTeamInLeague(playerID, lg),
		"league":                lg,
		"available":             lg == LeagueRookie,
		"team_creation_enabled": leagueAllowsTeamCreation(lg),
		"coach_draft_enabled":   coachDraftEnabled(lg),
	}
	if found && s.Status == draftPending {
		resp["signed_up"] = true
		resp["kind"] = draftKindOf(s.Kind)
		resp["signup"] = s
	}
	respondJSON(w, resp)
}

// POST /api/draft/signup
func HandleDraftSignup(w http.ResponseWriter, r *http.Request) {
	playerID, ok := draftPlayerID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	lg := leagueFromRequest(r)
	if lg != LeagueRookie {
		http.Error(w, "Draft signup is only available for the Rookie League", http.StatusBadRequest)
		return
	}

	var p Player
	if err := DB.First(&p, playerID).Error; err != nil {
		http.Error(w, "You must register before joining the draft", http.StatusBadRequest)
		return
	}
	if !playerIsRegisteredIn(playerID, lg) {
		http.Error(w, "You must register for this league before joining the draft", http.StatusBadRequest)
		return
	}
	if playerIsBannedIn(playerID, lg) {
		http.Error(w, "Your account is banned.", http.StatusForbidden)
		return
	}

	if playerOnTeamInLeague(playerID, lg) {
		http.Error(w, "You are already on a Rookie League team", http.StatusBadRequest)
		return
	}

	var req struct {
		Note string `json:"note"`
		Kind string `json:"kind"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	kind := draftKindOf(req.Kind)
	if kind == draftKindCoach && !coachDraftEnabled(lg) {
		http.Error(w, "Coach signups are closed for the Rookie League right now.", http.StatusBadRequest)
		return
	}

	// Veterans can't play, so they can't enter the player draft. If they sign up
	// for one, put them straight into the coach pool (they can still create or
	// join a team normally at any time). A league mod can lift this per player
	// with the rookie play override.
	if kind == draftKindPlayer && playerLockedToCoach(lg, playerID) {
		if !coachDraftEnabled(lg) {
			http.Error(w,
				"Veterans can't play in the Rookie League. You can still create or join a team — ask a mod about coaching.",
				http.StatusBadRequest)
			return
		}
		kind = draftKindCoach
	}

	// Registered coaches don't play, so they only ever enter the coach draft —
	// even if the client asked for the player draft.
	if kind == draftKindPlayer && isRegisteredRookieCoach(playerID) {
		if !coachDraftEnabled(lg) {
			http.Error(w,
				"You're registered as a coach, so you can only sign up for the coach draft — and coach signups are closed right now.",
				http.StatusBadRequest)
			return
		}
		kind = draftKindCoach
	}

	note := strings.TrimSpace(req.Note)
	if len(note) > 300 {
		note = note[:300]
	}

	var s DraftSignup
	err := DB.Where("player_id = ? AND league = ?", playerID, lg).First(&s).Error

	s.PlayerID = playerID
	s.League = lg
	s.Role = p.Role
	s.Device = p.Device
	s.Timezone = p.Timezone
	s.Note = note
	s.Kind = kind
	s.Status = draftPending
	s.TeamID = nil

	if err != nil {
		if err := DB.Create(&s).Error; err != nil {
			http.Error(w, "Failed to join the draft", http.StatusInternalServerError)
			return
		}
	} else if err := DB.Save(&s).Error; err != nil {
		http.Error(w, "Failed to join the draft", http.StatusInternalServerError)
		return
	}

	if kind == draftKindCoach {
		LogGeneral(fmt.Sprintf("🧑‍🏫 **%s** signed up to **coach** in the Rookie League draft", draftPlayerLabel(p)), lg)
	} else {
		LogGeneral(fmt.Sprintf("📝 **%s** entered the **Rookie League draft**", draftPlayerLabel(p)), lg)
	}

	respondJSON(w, map[string]any{"success": true, "signup": s})
}

// POST /api/draft/withdraw
func HandleDraftWithdraw(w http.ResponseWriter, r *http.Request) {
	playerID, ok := draftPlayerID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	lg := leagueFromRequest(r)

	res := DB.Model(&DraftSignup{}).
		Where("player_id = ? AND league = ? AND status = ?", playerID, lg, draftPending).
		Update("status", draftWithdrawn)
	if res.Error != nil {
		http.Error(w, "Failed to withdraw", http.StatusInternalServerError)
		return
	}
	if res.RowsAffected == 0 {
		http.Error(w, "You are not in the draft", http.StatusNotFound)
		return
	}

	var p Player
	DB.First(&p, playerID)
	LogGeneral(fmt.Sprintf("↩️ **%s** withdrew from the **Rookie League draft**", draftPlayerLabel(p)), lg)

	respondJSON(w, map[string]any{"success": true})
}

// --- Mod endpoints ----------------------------------------------------------

// GET /api/mod/draft
func HandleModListDraft(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	lg := leagueFromRequest(r)

	type draftRow struct {
		ID             uint      `json:"id"`
		PlayerID       int64     `json:"player_id"`
		PlayerIDStr    string    `json:"player_id_str"`
		Username       string    `json:"username"`
		DisplayName    string    `json:"display_name"`
		Avatar         string    `json:"avatar"`
		Role           string    `json:"role"`
		Device         string    `json:"device"`
		Timezone       string    `json:"timezone"`
		Note           string    `json:"note"`
		Kind           string    `json:"kind"`
		CreatedAt      time.Time `json:"created_at"`
		VarsityRating  int       `json:"varsity_rating"`
		VarsityMatches int       `json:"varsity_matches"`
		IsVeteran      bool      `json:"is_veteran"`
		PlayOverride   bool      `json:"play_override"`
	}

	var rows []draftRow
	if err := DB.Raw(`
		SELECT d.id, d.player_id, p.username, p.display_name, p.avatar,
		       d.role, d.device, d.timezone, d.note, d.kind, d.created_at
		FROM draft_signups d
		JOIN players p ON p.id = d.player_id
		WHERE d.league = ? AND d.status = ?
		ORDER BY d.created_at ASC
	`, lg, draftPending).Scan(&rows).Error; err != nil {
		log.Printf("❌ HandleModListDraft: %v", err)
		modJSONErr(w, http.StatusInternalServerError, "failed to load the draft")
		return
	}

	for i := range rows {
		rows[i].PlayerIDStr = strconv.FormatInt(rows[i].PlayerID, 10)
		if s, ok := readPlayerLeagueStats(rows[i].PlayerID, LeagueMain); ok {
			rows[i].VarsityRating = s.Rating
			rows[i].VarsityMatches = s.Matches
		}
		rows[i].IsVeteran = isVeteranInLeague(lg, rows[i].PlayerID)
		rows[i].PlayOverride = playerPlayOverride(rows[i].PlayerID, lg)
	}

	respondJSON(w, map[string]any{
		"success": true,
		"league":  lg,
		"draft":   rows,
	})
}

// POST /api/mod/draft/assign  { signup_id, team_id, override }
func HandleModAssignDraft(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		SignupID uint `json:"signup_id"`
		TeamID   uint `json:"team_id"`
		Override bool `json:"override"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SignupID == 0 || req.TeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "signup_id and team_id are required")
		return
	}

	var s DraftSignup
	if err := DB.First(&s, req.SignupID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "draft entry not found")
		return
	}
	if s.Status != draftPending {
		modJSONErr(w, http.StatusBadRequest, "this draft entry is no longer pending")
		return
	}

	var team Team
	if err := DB.First(&team, req.TeamID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "team not found")
		return
	}
	if normalizeLeague(team.League) != normalizeLeague(s.League) {
		modJSONErr(w, http.StatusBadRequest, "team is not in the same league as this draft entry")
		return
	}

	if playerOnTeamInLeague(s.PlayerID, s.League) {
		modJSONErr(w, http.StatusBadRequest, "player is already on a team in this league")
		return
	}

	if leagueTournamentStarted(s.League) {
		modJSONErr(w, http.StatusBadRequest,
			"the tournament has already started — assign this player to an existing team instead")
		return
	}

	if !req.Override {
		if ok, reason := canJoinRookieTeam(req.TeamID, s.PlayerID); !ok {
			modJSONErr(w, http.StatusForbidden, reason)
			return
		}
	}

	maxPlayers := leagueMaxTeamPlayers(team.League)
	if maxPlayers > 0 {
		// Coaches don't consume a playing roster slot.
		count := teamPlayingCount(req.TeamID)
		if int(count) >= maxPlayers {
			modJSONErr(w, http.StatusBadRequest,
				fmt.Sprintf("team is full (%d/%d)", count, maxPlayers))
			return
		}
	}

	// Rookie-league veterans only ever join as Coaches (they don't play).
	assignRole := draftTeamRole(s.Kind)
	if forced := leagueMemberRole(team.League, s.PlayerID); forced == "Coach" {
		assignRole = "Coach"
	}
	if strings.EqualFold(assignRole, "Coach") {
		if blocked, reason := coachRoleBlocked(req.TeamID); blocked {
			modJSONErr(w, http.StatusBadRequest, reason)
			return
		}
	}

	if err := DB.Create(&TeamMember{TeamID: req.TeamID, PlayerID: s.PlayerID, Role: assignRole}).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to add player to team")
		return
	}

	getPlayerLeagueStats(s.PlayerID, team.League)

	var p Player
	DB.First(&p, s.PlayerID)

	DB.Create(&PlayerHistory{
		PlayerID:   s.PlayerID,
		TeamID:     team.ID,
		TeamName:   team.Name,
		Role:       draftTeamRole(s.Kind),
		Season:     seasonForLeague(team.League),
		IsTeamJoin: true,
	})

	DB.Model(&DraftSignup{}).Where("id = ?", s.ID).
		Updates(map[string]any{"status": draftAssigned, "team_id": team.ID})

	syncDiscordRolesForPlayer(s.PlayerID)

	LogGeneral(fmt.Sprintf(
		"🤝 **%s** was drafted to **%s (#%d)**",
		draftPlayerLabel(p), team.Name, team.ID,
	), team.League)

	respondJSON(w, map[string]any{
		"success": true,
		"team_id": team.ID,
		"team":    team.Name,
	})
}

// POST /api/mod/draft/transfer
//
// Moves a player who is already on a team in this league to another team —
// used to rebalance rosters (e.g. a player is too strong for their current
// rookie team). Body: { player_id, to_team_id, override }
func HandleModTransferPlayer(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		PlayerID string `json:"player_id"`
		ToTeamID uint   `json:"to_team_id"`
		Override bool   `json:"override"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		modJSONErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	playerID, err := strconv.ParseInt(strings.TrimSpace(req.PlayerID), 10, 64)
	if err != nil || playerID <= 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid player id")
		return
	}
	if req.ToTeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "to_team_id is required")
		return
	}

	lg := leagueFromRequest(r)

	// Where are they now?
	var current struct {
		TeamID   uint
		TeamName string
		Role     string
	}
	if err := DB.Raw(`
		SELECT tm.team_id, t.name AS team_name, tm.role
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.player_id = ? AND t.league = ?
		LIMIT 1
	`, playerID, lg).Scan(&current).Error; err != nil || current.TeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "player is not on a team in this league")
		return
	}

	if current.TeamID == req.ToTeamID {
		modJSONErr(w, http.StatusBadRequest, "player is already on that team")
		return
	}

	var toTeam Team
	if err := DB.First(&toTeam, req.ToTeamID).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "target team not found")
		return
	}
	if normalizeLeague(toTeam.League) != lg {
		modJSONErr(w, http.StatusBadRequest, "target team is in a different league")
		return
	}

	maxPlayers := leagueMaxTeamPlayers(toTeam.League)
	// Coaches don't occupy a playing slot.
	toCount := teamPlayingCount(toTeam.ID)
	if maxPlayers > 0 && int(toCount) >= maxPlayers {
		modJSONErr(w, http.StatusBadRequest,
			fmt.Sprintf("%s is full (%d/%d)", toTeam.Name, toCount, maxPlayers))
		return
	}

	if !req.Override {
		if ok, reason := canJoinRookieTeam(toTeam.ID, playerID); !ok {
			modJSONErr(w, http.StatusForbidden, reason)
			return
		}
	}

	var p Player
	DB.First(&p, playerID)
	label := draftPlayerLabel(p)

	fromTeamID := current.TeamID
	wasLeader := roleRank(current.Role) <= 1 // Captain or Co-Captain

	// Role on the new team, plus the one-coach-per-team rule.
	moveRole := leagueMemberRole(toTeam.League, playerID)
	if strings.EqualFold(moveRole, "Coach") {
		if blocked, reason := coachRoleBlocked(toTeam.ID); blocked {
			modJSONErr(w, http.StatusBadRequest, reason)
			return
		}
	}

	// Move: remove from the old team, add to the new one.
	if err := DB.Where("team_id = ? AND player_id = ?", fromTeamID, playerID).
		Delete(&TeamMember{}).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to remove player from their team")
		return
	}

	if err := DB.Omit("Player").Create(&TeamMember{
		TeamID:   toTeam.ID,
		PlayerID: playerID,
		Role:     moveRole,
	}).Error; err != nil {
		// Put them back where they were so nothing is lost.
		DB.Omit("Player").Create(&TeamMember{
			TeamID:   fromTeamID,
			PlayerID: playerID,
			Role:     current.Role,
		})
		modJSONErr(w, http.StatusInternalServerError, "failed to add player to the new team")
		return
	}

	getPlayerLeagueStats(playerID, toTeam.League)

	DB.Create(&PlayerHistory{
		PlayerID:   playerID,
		TeamID:     toTeam.ID,
		TeamName:   toTeam.Name,
		Role:       "Member",
		Season:     seasonForLeague(toTeam.League),
		IsTeamJoin: true,
	})

	// If a leader moved, promote someone on their old team (co-captain first).
	if wasLeader {
		var next TeamMember
		if err := DB.Where("team_id = ? AND role = ?", fromTeamID, "Co-Captain").
			First(&next).Error; err == nil {
			DB.Model(&TeamMember{}).
				Where("team_id = ? AND player_id = ?", fromTeamID, next.PlayerID).
				Update("role", "Captain")
			syncDiscordRolesForPlayer(next.PlayerID)
		} else if err := DB.Where("team_id = ?", fromTeamID).
			First(&next).Error; err == nil {
			DB.Model(&TeamMember{}).
				Where("team_id = ? AND player_id = ?", fromTeamID, next.PlayerID).
				Update("role", "Captain")
			syncDiscordRolesForPlayer(next.PlayerID)
		}
	}

	syncDiscordRolesForPlayer(playerID)

	LogGeneral(fmt.Sprintf(
		"🔀 **%s** moved from **%s** to **%s**", label, current.TeamName, toTeam.Name,
	), lg)

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("%s moved from %s to %s", label, current.TeamName, toTeam.Name),
		"from":    current.TeamName,
		"to":      toTeam.Name,
	})
}

// POST /api/mod/draft/create-team
//
// Creates a brand new team and adds one or more draft signups to it. Captaincy
// is optional — drafted players can simply be grouped onto the same team, and a
// captain can be assigned later from the team tools.
//
// Body: { signup_ids: [int], signup_id?: int, name: string,
//
//	captain_player_id?: string, override?: bool }
func HandleModCreateTeamForDraft(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		SignupIDs       []uint `json:"signup_ids"`
		SignupID        uint   `json:"signup_id"`
		Name            string `json:"name"`
		CaptainPlayerID string `json:"captain_player_id"`
		Override        bool   `json:"override"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		modJSONErr(w, http.StatusBadRequest, "invalid payload")
		return
	}

	ids := req.SignupIDs
	if len(ids) == 0 && req.SignupID > 0 {
		ids = []uint{req.SignupID}
	}
	if len(ids) == 0 {
		modJSONErr(w, http.StatusBadRequest, "select at least one draft signup")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		// Nothing typed in — make up a unique "Adjective Noun" name.
		name = randomTeamName()
	}

	var signups []DraftSignup
	if err := DB.Where("id IN ?", ids).Find(&signups).Error; err != nil || len(signups) != len(ids) {
		modJSONErr(w, http.StatusNotFound, "one or more draft entries were not found")
		return
	}

	league := ""
	for _, s := range signups {
		if s.Status != draftPending {
			modJSONErr(w, http.StatusBadRequest, "a selected draft entry is no longer pending")
			return
		}
		if league == "" {
			league = normalizeLeague(s.League)
		} else if normalizeLeague(s.League) != league {
			modJSONErr(w, http.StatusBadRequest, "all selected players must be from the same league")
			return
		}
		if playerOnTeamInLeague(s.PlayerID, s.League) {
			var p Player
			DB.First(&p, s.PlayerID)
			modJSONErr(w, http.StatusBadRequest,
				draftPlayerLabel(p)+" is already on a team in this league")
			return
		}
	}

	maxPlayers := leagueMaxTeamPlayers(league)
	// Count only the signups that will actually play (veterans join as coaches).
	playingSignups := 0
	for _, s := range signups {
		if leagueMemberRole(league, s.PlayerID) != "Coach" {
			playingSignups++
		}
	}
	if maxPlayers > 0 && playingSignups > maxPlayers {
		modJSONErr(w, http.StatusBadRequest,
			fmt.Sprintf("a team can hold at most %d players", maxPlayers))
		return
	}

	// Optional captain — must be one of the selected players. Without one, the
	// team is created with all members and a captain is promoted later.
	var captainID int64
	if strings.TrimSpace(req.CaptainPlayerID) != "" {
		id, err := strconv.ParseInt(strings.TrimSpace(req.CaptainPlayerID), 10, 64)
		if err != nil || id <= 0 {
			modJSONErr(w, http.StatusBadRequest, "invalid captain player id")
			return
		}
		for _, s := range signups {
			if s.PlayerID == id {
				captainID = id
				break
			}
		}
		if captainID == 0 {
			modJSONErr(w, http.StatusBadRequest, "captain must be one of the selected players")
			return
		}
	}

	// Rookie composition rule for the whole group at once.
	if !req.Override && league == LeagueRookie && getEnv("ROOKIE_VETERAN_RULE_ENABLED", "true") == "true" {
		veterans := 0
		for _, s := range signups {
			if playerLockedToCoach(league, s.PlayerID) {
				veterans++
			}
		}
		rookies := len(signups) - veterans
		perVeteran := getEnvInt("ROOKIE_NEW_PLAYERS_PER_VETERAN", 3)
		if perVeteran <= 0 {
			perVeteran = 3
		}
		if veterans > 0 && rookies < perVeteran*veterans {
			modJSONErr(w, http.StatusForbidden, fmt.Sprintf(
				"Rookie teams allow 1 main-league veteran per %d new players — this group has %d veteran(s) and %d new player(s). Tick Force to override.",
				perVeteran, veterans, rookies))
			return
		}
	}

	var dup Team
	if err := DB.Where("LOWER(name) = LOWER(?)", name).First(&dup).Error; err == nil {
		modJSONErr(w, http.StatusBadRequest, "a team with that name already exists")
		return
	}

	ls := ensureLeagueSettings(league)

	team := Team{
		League:          league,
		Name:            name,
		Status:          "Active",
		Rating:          getEnvInt("DEFAULT_TEAM_RATING", 1000),
		AllowChallenges: ls.ChallengesEnabled,
		JoinAllowed:     true,
	}
	if err := DB.Create(&team).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to create team")
		return
	}

	var names []string
	for _, s := range signups {
		role := "Member"
		if captainID != 0 && s.PlayerID == captainID {
			role = "Captain"
		}
		if forced := leagueMemberRole(league, s.PlayerID); forced == "Coach" {
			role = "Coach"
		}

		if err := DB.Omit("Player").Create(&TeamMember{
			TeamID:   team.ID,
			PlayerID: s.PlayerID,
			Role:     role,
		}).Error; err != nil {
			modJSONErr(w, http.StatusInternalServerError, "failed to add a drafted player to the team")
			return
		}

		getPlayerLeagueStats(s.PlayerID, league)

		var p Player
		DB.First(&p, s.PlayerID)
		names = append(names, draftPlayerLabel(p))

		DB.Create(&PlayerHistory{
			PlayerID:   s.PlayerID,
			TeamID:     team.ID,
			TeamName:   team.Name,
			Role:       role,
			Season:     seasonForLeague(league),
			IsTeamJoin: true,
		})

		DB.Model(&DraftSignup{}).Where("id = ?", s.ID).
			Updates(map[string]any{"status": draftAssigned, "team_id": team.ID})

		syncDiscordRolesForPlayer(s.PlayerID)
	}

	LogGeneral(fmt.Sprintf(
		"🏗️ New team **%s (#%d)** created from the draft: **%s**",
		team.Name, team.ID, strings.Join(names, ", "),
	), league)

	respondJSON(w, map[string]any{
		"success": true,
		"team_id": team.ID,
		"team":    team.Name,
		"players": len(names),
	})
}

// --- Auto-generated team names ---------------------------------------------

var teamNameAdjectives = []string{
	"Brave", "Silent", "Rapid", "Cosmic", "Frozen", "Golden", "Crimson", "Iron",
	"Neon", "Swift", "Wild", "Lucky", "Fierce", "Clever", "Rusty", "Solar",
	"Lunar", "Thunder", "Stormy", "Frosty", "Blazing", "Shadow", "Electric", "Wired",
	"Quantum", "Turbo", "Mighty", "Nimble", "Sneaky", "Bold", "Merry", "Vivid",
	"Gritty", "Shiny", "Hazy", "Primal", "Astral", "Hyper", "Ancient", "Restless",
}

var teamNameNouns = []string{
	"Otter", "Falcon", "Comet", "Badger", "Phoenix", "Cobra", "Yeti", "Panda",
	"Lynx", "Raven", "Wolf", "Tiger", "Piranha", "Manta", "Golem", "Wraith",
	"Nomad", "Pilot", "Sentinel", "Vector", "Quasar", "Nova", "Meteor", "Ember",
	"Glacier", "Circuit", "Sparrow", "Rhino", "Bison", "Orca", "Ferret", "Hedgehog",
	"Dragon", "Wombat", "Lizard", "Turtle", "Pelican", "Ocelot", "Bandit", "Drifter",
}

// teamNameTaken reports whether any team (any league) already uses this name.
func teamNameTaken(name string) bool {
	var count int64
	DB.Model(&Team{}).Where("LOWER(name) = LOWER(?)", name).Count(&count)
	return count > 0
}

// randomTeamName returns a unique "Adjective Noun" team name. Team names are
// globally unique, so it retries until it finds a free combination.
func randomTeamName() string {
	for i := 0; i < 60; i++ {
		adj := teamNameAdjectives[rand.Intn(len(teamNameAdjectives))]
		noun := teamNameNouns[rand.Intn(len(teamNameNouns))]
		name := adj + " " + noun
		if !teamNameTaken(name) {
			return name
		}
	}
	// Extremely unlikely: fall back to suffixed names.
	for i := 0; i < 60; i++ {
		adj := teamNameAdjectives[rand.Intn(len(teamNameAdjectives))]
		noun := teamNameNouns[rand.Intn(len(teamNameNouns))]
		name := fmt.Sprintf("%s %s %d", adj, noun, 2+rand.Intn(98))
		if !teamNameTaken(name) {
			return name
		}
	}
	return fmt.Sprintf("New Team %d", time.Now().Unix()%100000)
}

// GET /api/mod/draft/team-name — suggest an unused "Adjective Noun" name.
func HandleGetDraftTeamName(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}
	respondJSON(w, map[string]any{"success": true, "name": randomTeamName()})
}

// GET /api/mod/draft/teams — every team in the league with its current roster,
// so mods can see free slots / veteran counts while placing draft players.
func HandleModListDraftTeams(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	lg := leagueFromRequest(r)
	maxSlots := leagueMaxTeamPlayers(lg)

	var teams []Team
	if err := DB.
		Where("league = ? AND name NOT IN ('(No Team)', 'League Sub')", lg).
		Order("CASE WHEN status = 'Active' THEN 0 ELSE 1 END, name ASC").
		Find(&teams).Error; err != nil {
		log.Printf("❌ HandleModListDraftTeams: %v", err)
		modJSONErr(w, http.StatusInternalServerError, "failed to load teams")
		return
	}

	type memberRow struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Avatar      string `json:"avatar"`
		Role        string `json:"role"`
		IsVeteran   bool   `json:"is_veteran"`
	}

	type teamRow struct {
		ID       uint        `json:"id"`
		Name     string      `json:"name"`
		Status   string      `json:"status"`
		Rating   int         `json:"rating"`
		MaxSlots int         `json:"max_slots"`
		Members  []memberRow `json:"members"`
		Veterans int         `json:"veterans"`
		Rookies  int         `json:"rookies"`
	}

	out := make([]teamRow, 0, len(teams))

	for _, t := range teams {
		var ids []int64
		DB.Table("team_members").Where("team_id = ?", t.ID).Pluck("player_id", &ids)

		row := teamRow{
			ID:       t.ID,
			Name:     t.Name,
			Status:   t.Status,
			Rating:   t.Rating,
			MaxSlots: maxSlots,
			Members:  []memberRow{},
		}

		// Captain / co-captain first, then the rest.
		sort.SliceStable(ids, func(i, j int) bool {
			ri := roleRank(teamMemberRole(t.ID, ids[i]))
			rj := roleRank(teamMemberRole(t.ID, ids[j]))
			return ri < rj
		})

		for _, pid := range ids {
			var p Player
			if err := DB.Select("id, username, display_name, avatar").First(&p, pid).Error; err != nil {
				continue
			}

			vet := isVeteranInLeague(lg, pid)
			if vet {
				row.Veterans++
			} else {
				row.Rookies++
			}

			row.Members = append(row.Members, memberRow{
				ID:          strconv.FormatInt(p.ID, 10),
				Username:    p.Username,
				DisplayName: p.DisplayName,
				Avatar:      p.Avatar,
				Role:        teamMemberRole(t.ID, pid),
				IsVeteran:   vet,
			})
		}

		out = append(out, row)
	}

	respondJSON(w, map[string]any{
		"success":   true,
		"league":    lg,
		"max_slots": maxSlots,
		"teams":     out,
	})
}

// POST /api/mod/draft/release
//
// Takes a player off their current team and puts them back into the draft pool
// (used by dragging a roster member back onto the draft list).
// Body: { player_id }
func HandleModReleasePlayer(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		PlayerID string `json:"player_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		modJSONErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	playerID, err := strconv.ParseInt(strings.TrimSpace(req.PlayerID), 10, 64)
	if err != nil || playerID <= 0 {
		modJSONErr(w, http.StatusBadRequest, "invalid player id")
		return
	}

	lg := leagueFromRequest(r)

	var current struct {
		TeamID   uint
		TeamName string
		Role     string
	}
	if err := DB.Raw(`
		SELECT tm.team_id, t.name AS team_name, tm.role
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.player_id = ? AND t.league = ?
		LIMIT 1
	`, playerID, lg).Scan(&current).Error; err != nil || current.TeamID == 0 {
		modJSONErr(w, http.StatusBadRequest, "player is not on a team in this league")
		return
	}

	var p Player
	DB.First(&p, playerID)
	label := draftPlayerLabel(p)

	fromTeamID := current.TeamID
	wasLeader := roleRank(current.Role) <= 1

	if err := DB.Where("team_id = ? AND player_id = ?", fromTeamID, playerID).
		Delete(&TeamMember{}).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to remove player from their team")
		return
	}

	// Promote a new leader on the team they left.
	if wasLeader {
		var next TeamMember
		if err := DB.Where("team_id = ? AND role = ?", fromTeamID, "Co-Captain").
			First(&next).Error; err == nil {
			DB.Model(&TeamMember{}).
				Where("team_id = ? AND player_id = ?", fromTeamID, next.PlayerID).
				Update("role", "Captain")
			syncDiscordRolesForPlayer(next.PlayerID)
		} else if err := DB.Where("team_id = ?", fromTeamID).
			First(&next).Error; err == nil {
			DB.Model(&TeamMember{}).
				Where("team_id = ? AND player_id = ?", fromTeamID, next.PlayerID).
				Update("role", "Captain")
			syncDiscordRolesForPlayer(next.PlayerID)
		}
	}

	// Put them back in the draft pool.
	var s DraftSignup
	if err := DB.Where("player_id = ? AND league = ?", playerID, lg).First(&s).Error; err != nil {
		s = DraftSignup{PlayerID: playerID, League: lg}
	}
	s.Role = p.Role
	s.Device = p.Device
	s.Timezone = p.Timezone
	s.Status = draftPending
	s.TeamID = nil
	if s.ID == 0 {
		if err := DB.Create(&s).Error; err != nil {
			modJSONErr(w, http.StatusInternalServerError, "failed to return player to the draft")
			return
		}
	} else if err := DB.Save(&s).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to return player to the draft")
		return
	}

	syncDiscordRolesForPlayer(playerID)

	LogGeneral(fmt.Sprintf(
		"↩️ **%s** was removed from **%s** and returned to the draft",
		label, current.TeamName,
	), lg)

	respondJSON(w, map[string]any{
		"success": true,
		"message": fmt.Sprintf("%s removed from %s and returned to the draft", label, current.TeamName),
	})
}

// teamMemberRole returns the role of a player on a team ("" when not found).
func teamMemberRole(teamID uint, playerID int64) string {
	var tm TeamMember
	if err := DB.Where("team_id = ? AND player_id = ?", teamID, playerID).First(&tm).Error; err != nil {
		return ""
	}
	return tm.Role
}

// roleRank orders captain, co-captain, then everyone else.
func roleRank(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "captain":
		return 0
	case "co-captain":
		return 1
	default:
		return 2
	}
}

// --- Auto assign ------------------------------------------------------------

// draftAddMember adds a draft signup to an existing team and closes the signup.
func draftAddMember(teamID uint, league string, s DraftSignup) bool {
	role := draftTeamRole(s.Kind)
	// Rookie-league veterans only ever join as Coaches (they don't play).
	if forced := leagueMemberRole(league, s.PlayerID); forced == "Coach" {
		role = "Coach"
	}
	if err := DB.Omit("Player").Create(&TeamMember{
		TeamID:   teamID,
		PlayerID: s.PlayerID,
		Role:     role,
	}).Error; err != nil {
		log.Printf("❌ draftAddMember(%d → team %d): %v", s.PlayerID, teamID, err)
		return false
	}

	getPlayerLeagueStats(s.PlayerID, league)

	var team Team
	DB.Select("name").First(&team, teamID)

	DB.Create(&PlayerHistory{
		PlayerID:   s.PlayerID,
		TeamID:     teamID,
		TeamName:   team.Name,
		Role:       role,
		Season:     seasonForLeague(league),
		IsTeamJoin: true,
	})

	DB.Model(&DraftSignup{}).Where("id = ?", s.ID).
		Updates(map[string]any{"status": draftAssigned, "team_id": teamID})

	syncDiscordRolesForPlayer(s.PlayerID)
	return true
}

// POST /api/mod/draft/auto-assign
//
// Balances all pending draft signups onto teams using the league's roster rules:
//   - fills existing teams that have space first (new players before veterans),
//   - only places a main-league veteran where the 1-veteran-per-N-new-players
//     ratio still holds,
//   - then forms new teams (auto-named) of at least MIN_TEAM_PLAYERS and at most
//     MAX_TEAM_PLAYERS, again honouring the veteran ratio.
//
// Anything that cannot be placed within the rules is left in the draft.
func HandleModAutoAssignDraft(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	lg := leagueFromRequest(r)

	var req struct {
		Override bool   `json:"override"` // ignore the veteran ratio rule
		Kind     string `json:"kind"`     // "player" (default) | "coach"
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if draftKindOf(req.Kind) == draftKindCoach {
		autoAssignCoaches(w, lg)
		return
	}

	var pending []DraftSignup
	// Coaches are excluded — they're placed by hand and must not consume roster
	// slots or skew the veteran/rookie balancing.
	if err := DB.Where("league = ? AND status = ? AND COALESCE(kind, 'player') <> ?",
		lg, draftPending, draftKindCoach).
		Order("created_at ASC").Find(&pending).Error; err != nil {
		log.Printf("❌ HandleModAutoAssignDraft: %v", err)
		modJSONErr(w, http.StatusInternalServerError, "failed to load the draft")
		return
	}
	if len(pending) == 0 {
		modJSONErr(w, http.StatusBadRequest, "no pending draft signups")
		return
	}

	ruleOn := !req.Override && getEnv("ROOKIE_VETERAN_RULE_ENABLED", "true") == "true"
	perVeteran := getEnvInt("ROOKIE_NEW_PLAYERS_PER_VETERAN", 3)
	if perVeteran <= 0 {
		perVeteran = 3
	}
	maxPlayers := leagueMaxTeamPlayers(lg)
	minPlayers := leagueMinTeamPlayers(lg)
	if minPlayers < 1 {
		minPlayers = 1
	}

	// Split the pool. Anyone who already found a team is just closed out.
	// Veterans are never auto-placed as players: in the Rookie League they may
	// only join as Coaches (via the coach draft), so they stay in the draft with
	// a note telling mods to move them to the coach pool. A league mod can flip
	// a specific veteran back to a playable player with the rookie play override.
	var rooks, vets []DraftSignup
	for _, s := range pending {
		if playerOnTeamInLeague(s.PlayerID, lg) {
			DB.Model(&DraftSignup{}).Where("id = ?", s.ID).Update("status", draftAssigned)
			continue
		}
		if playerLockedToCoach(lg, s.PlayerID) {
			vets = append(vets, s)
			continue
		}
		rooks = append(rooks, s)
	}

	type teamSlot struct {
		ID    uint
		Name  string
		Total int
		Vets  int
	}

	var existing []Team
	DB.Where("league = ? AND status = ? AND name NOT IN ('(No Team)', 'League Sub')", lg, "Active").
		Order("id ASC").Find(&existing)

	slots := make([]teamSlot, 0, len(existing))
	for _, t := range existing {
		// Coaches don't occupy playing slots.
		total := teamPlayingCount(t.ID)
		slots = append(slots, teamSlot{ID: t.ID, Name: t.Name, Total: int(total), Vets: 0})
	}

	assignedExisting := 0

	// Pass 1 — top up existing teams with new players (never breaks the ratio).
	for len(rooks) > 0 {
		placed := false
		for i := range slots {
			if len(rooks) == 0 {
				break
			}
			if maxPlayers > 0 && slots[i].Total >= maxPlayers {
				continue
			}
			if draftAddMember(slots[i].ID, lg, rooks[0]) {
				rooks = rooks[1:]
				slots[i].Total++
				assignedExisting++
				placed = true
			}
		}
		if !placed {
			break
		}
	}

	// Pass 2 — veterans only where the ratio still holds.
	for len(vets) > 0 {
		placed := false
		for i := range slots {
			if len(vets) == 0 {
				break
			}
			if maxPlayers > 0 && slots[i].Total >= maxPlayers {
				continue
			}
			rooksOnTeam := slots[i].Total - slots[i].Vets
			if ruleOn && rooksOnTeam < perVeteran*(slots[i].Vets+1) {
				continue
			}
			if draftAddMember(slots[i].ID, lg, vets[0]) {
				vets = vets[1:]
				slots[i].Total++
				slots[i].Vets++
				assignedExisting++
				placed = true
			}
		}
		if !placed {
			break
		}
	}

	// Pass 3 — build new teams from whatever is left.
	ls := ensureLeagueSettings(lg)
	type createdTeam struct {
		ID      uint     `json:"id"`
		Name    string   `json:"name"`
		Players []string `json:"players"`
	}
	created := []createdTeam{}

	limit := maxPlayers
	if limit <= 0 {
		limit = len(rooks) + len(vets)
	}

	for iter := 0; iter < 200; iter++ {
		if len(rooks)+len(vets) < minPlayers {
			break
		}

		vetTake, rookTake := 0, 0
		if len(vets) > 0 {
			if ruleOn {
				if len(rooks) >= perVeteran && perVeteran+1 <= limit {
					vetTake = 1
					rookTake = perVeteran
				}
			} else {
				vetTake = 1
			}
		}

		// Top up to the team limit — extra new players first, then (only when
		// the rule is off) extra veterans.
		for vetTake+rookTake < limit {
			if rookTake < len(rooks) {
				rookTake++
				continue
			}
			if !ruleOn && vetTake < len(vets) {
				vetTake++
				continue
			}
			break
		}

		if vetTake+rookTake < minPlayers {
			break
		}

		group := append([]DraftSignup{}, vets[:vetTake]...)
		group = append(group, rooks[:rookTake]...)
		vets = vets[vetTake:]
		rooks = rooks[rookTake:]

		team := Team{
			League:          lg,
			Name:            randomTeamName(),
			Status:          "Active",
			Rating:          getEnvInt("DEFAULT_TEAM_RATING", 1000),
			AllowChallenges: ls.ChallengesEnabled,
			JoinAllowed:     true,
		}
		if err := DB.Create(&team).Error; err != nil {
			log.Printf("❌ auto-assign: create team: %v", err)
			break
		}

		entry := createdTeam{ID: team.ID, Name: team.Name}
		for _, s := range group {
			if draftAddMember(team.ID, lg, s) {
				var p Player
				DB.First(&p, s.PlayerID)
				entry.Players = append(entry.Players, draftPlayerLabel(p))
			}
		}
		created = append(created, entry)

		LogGeneral(fmt.Sprintf(
			"🏗️ Auto-assigned **%s (#%d)** from the draft: **%s**",
			team.Name, team.ID, strings.Join(entry.Players, ", "),
		), lg)
	}

	// Pass 4 — veterans. They may enter the draft like anyone else, they just
	// can't play: each one is placed as a Coach on a team that doesn't have one
	// yet (smallest roster first). They never consume a playing slot.
	coachesPlaced := 0
	if len(vets) > 0 {
		type noCoachTeam struct {
			ID      uint
			Name    string
			Players int
		}
		var coachTeams []noCoachTeam
		DB.Raw(`
			SELECT t.id, t.name,
			       (SELECT COUNT(*) FROM team_members m
			         WHERE m.team_id = t.id AND LOWER(TRIM(COALESCE(m.role,''))) <> 'coach') AS players
			FROM teams t
			WHERE t.league = ?
			  AND COALESCE(t.status,'') <> 'Test'
			  AND NOT EXISTS (
			        SELECT 1 FROM team_members c
			        WHERE c.team_id = t.id AND LOWER(c.role) = 'coach'
			  )
			ORDER BY players ASC, t.id ASC
		`, lg).Scan(&coachTeams)

		idx := 0
		remainingVets := vets[:0]
		for _, s := range vets {
			if idx >= len(coachTeams) {
				remainingVets = append(remainingVets, s)
				continue
			}
			if draftAddMember(coachTeams[idx].ID, lg, s) {
				coachesPlaced++
				idx++
				continue
			}
			remainingVets = append(remainingVets, s)
		}
		vets = remainingVets
	}

	// Whatever is left stays in the draft.
	var leftoverNames []string
	for _, s := range vets {
		var p Player
		DB.First(&p, s.PlayerID)
		leftoverNames = append(leftoverNames, draftPlayerLabel(p))
	}
	for _, s := range rooks {
		var p Player
		DB.First(&p, s.PlayerID)
		leftoverNames = append(leftoverNames, draftPlayerLabel(p))
	}

	msg := fmt.Sprintf("Placed %d player(s) into existing teams and created %d new team(s).",
		assignedExisting, len(created))
	if coachesPlaced > 0 {
		msg += fmt.Sprintf(" %d veteran(s) placed as Coach.", coachesPlaced)
	}
	if len(leftoverNames) > 0 {
		msg += fmt.Sprintf(" %d player(s) could not be placed under the roster rules and remain in the draft: %s.",
			len(leftoverNames), strings.Join(leftoverNames, ", "))
	}

	LogGeneral(fmt.Sprintf("⚡ Draft auto-assign: %s", msg), lg)

	respondJSON(w, map[string]any{
		"success":           true,
		"assigned_existing": assignedExisting,
		"teams_created":     created,
		"leftover":          leftoverNames,
		"message":           msg,
	})
}

// autoAssignCoaches places every pending coach signup on a team that doesn't
// have a coach yet, smallest roster first. Roster size caps are ignored — a
// coach doesn't take a player slot.
func autoAssignCoaches(w http.ResponseWriter, lg string) {
	var coaches []DraftSignup
	if err := DB.Where("league = ? AND status = ? AND COALESCE(kind, 'player') = ?",
		lg, draftPending, draftKindCoach).
		Order("created_at ASC").Find(&coaches).Error; err != nil {
		log.Printf("❌ autoAssignCoaches: %v", err)
		modJSONErr(w, http.StatusInternalServerError, "failed to load coach signups")
		return
	}
	if len(coaches) == 0 {
		modJSONErr(w, http.StatusBadRequest, "no pending coach signups")
		return
	}

	// Teams in this league that don't have a coach yet, smallest roster first.
	type teamRow struct {
		ID      uint
		Name    string
		Members int
	}
	var teams []teamRow
	if err := DB.Raw(`
		SELECT t.id, t.name, COUNT(tm.player_id) AS members
		FROM teams t
		LEFT JOIN team_members tm ON tm.team_id = t.id
		WHERE t.league = ?
		  AND COALESCE(t.status, '') <> 'Test'
		  AND NOT EXISTS (
		        SELECT 1 FROM team_members c
		        WHERE c.team_id = t.id AND LOWER(c.role) = 'coach'
		  )
		GROUP BY t.id, t.name
		ORDER BY members ASC, t.id ASC
	`, lg).Scan(&teams).Error; err != nil {
		log.Printf("❌ autoAssignCoaches teams: %v", err)
		modJSONErr(w, http.StatusInternalServerError, "failed to load teams")
		return
	}

	assigned := 0
	leftover := []string{}
	idx := 0

	for _, s := range coaches {
		if idx >= len(teams) {
			var p Player
			DB.First(&p, s.PlayerID)
			leftover = append(leftover, draftPlayerLabel(p))
			continue
		}
		team := teams[idx]
		idx++

		if draftAddMember(team.ID, lg, s) {
			assigned++
			continue
		}

		var p Player
		DB.First(&p, s.PlayerID)
		leftover = append(leftover, draftPlayerLabel(p))
	}

	msg := fmt.Sprintf("Placed %d coach(es).", assigned)
	if len(leftover) > 0 {
		msg += fmt.Sprintf(" %d coach(es) could not be placed (every team already has one): %s.",
			len(leftover), strings.Join(leftover, ", "))
	}

	LogGeneral(fmt.Sprintf("🧑‍🏫 Draft coach auto-assign: %s", msg), lg)

	respondJSON(w, map[string]any{
		"success":  true,
		"assigned": assigned,
		"leftover": leftover,
		"message":  msg,
	})
}
