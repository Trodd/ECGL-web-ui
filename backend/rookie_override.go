package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Rookie League veteran play override.
//
// The Rookie League normally forces main-league veterans into the Coach role —
// they sit on a roster but never play. Some veterans are weak enough that a
// league mod wants to treat them as a rookie and let them play. This is a
// per-player, per-league flag that lifts that restriction.

// GET /api/mod/rookie/play-override?player_id=123[&league=rookie]
//
// Returns the veteran status plus the current override for one player.
func HandleGetRookiePlayOverride(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	pid, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("player_id")), 10, 64)
	if err != nil || pid == 0 {
		modJSONErr(w, http.StatusBadRequest, "player_id is required")
		return
	}

	lg := normalizeLeague(r.URL.Query().Get("league"))
	if lg == "" {
		lg = LeagueRookie
	}

	var p Player
	if err := DB.Select("id, display_name, username").First(&p, pid).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "player not found")
		return
	}

	respondJSON(w, map[string]any{
		"success":         true,
		"player_id":       fmt.Sprintf("%d", pid),
		"display_name":    p.DisplayName,
		"username":        p.Username,
		"league":          lg,
		"is_veteran":      isMainLeagueVeteran(pid),
		"play_override":   playerPlayOverride(pid, lg),
		"locked_to_coach": playerLockedToCoach(lg, pid),
		"is_coach":        isRegisteredRookieCoach(pid),
	})
}

// GET /api/mod/rookie/veterans?league=rookie
//
// Lists every Rookie-League-eligible player who counts as a main-league veteran,
// so mods can decide who to whitelist as a normal roster player.
func HandleListRookieVeterans(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	lg := normalizeLeague(r.URL.Query().Get("league"))
	if lg == "" {
		lg = normalizeLeague(r.Header.Get("X-ECGL-League"))
	}
	if lg == "" {
		lg = LeagueRookie
	}

	type cand struct {
		ID          int64
		Username    string
		DisplayName string
		Avatar      string
		Role        string
	}

	// Candidates: anyone registered in this league, plus anyone already on a
	// team in it.
	var cands []cand
	if err := DB.Raw(`
		SELECT p.id, p.username, p.display_name, COALESCE(p.avatar, '') AS avatar,
		       COALESCE(plr.role, p.role, '') AS role
		FROM players p
		LEFT JOIN player_league_roles plr ON plr.player_id = p.id AND plr.league = ?
		WHERE p.username <> '' AND p.display_name <> ''
		  AND (
		        plr.player_id IS NOT NULL
		     OR EXISTS (
		          SELECT 1 FROM team_members tm
		          JOIN teams t ON t.id = tm.team_id
		          WHERE tm.player_id = p.id AND t.league = ?
		        )
		  )
		ORDER BY p.display_name ASC
	`, lg, lg).Scan(&cands).Error; err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to load players: "+err.Error())
		return
	}

	// Their current team + role, if any.
	type membership struct {
		PlayerID int64
		TeamName string
		Role     string
	}
	memberByPlayer := map[int64]membership{}
	{
		var ms []membership
		DB.Raw(`
			SELECT tm.player_id, t.name AS team_name, COALESCE(tm.role, '') AS role
			FROM team_members tm
			JOIN teams t ON t.id = tm.team_id
			WHERE t.league = ?
		`, lg).Scan(&ms)
		for _, m := range ms {
			memberByPlayer[m.PlayerID] = m
		}
	}

	// Overrides already granted.
	overrides := map[int64]bool{}
	forcedVets := map[int64]bool{}
	{
		var ids []int64
		DB.Table("player_league_roles").
			Where("league = ? AND COALESCE(play_override, FALSE) = TRUE", lg).
			Pluck("player_id", &ids)
		for _, id := range ids {
			overrides[id] = true
		}

		var fids []int64
		DB.Table("player_league_roles").
			Where("league = ? AND COALESCE(force_veteran, FALSE) = TRUE", lg).
			Pluck("player_id", &fids)
		for _, id := range fids {
			forcedVets[id] = true
		}
	}

	ruleOn := getEnv("ROOKIE_VETERAN_RULE_ENABLED", "true") == "true"

	out := make([]map[string]any, 0, len(cands))
	for _, c := range cands {
		m := memberByPlayer[c.ID]
		explicit, optedOut := rookieCoachState(c.ID)
		varsityVet := isMainLeagueVeteran(c.ID)
		isVet := varsityVet || forcedVets[c.ID]
		locked := isVet && !overrides[c.ID] && ruleOn

		out = append(out, map[string]any{
			"player_id":       fmt.Sprintf("%d", c.ID),
			"username":        c.Username,
			"display_name":    c.DisplayName,
			"avatar":          c.Avatar,
			"role":            c.Role,
			"team_name":       m.TeamName,
			"team_role":       m.Role,
			"is_veteran":      isVet,
			"varsity_veteran": varsityVet,
			"force_veteran":   forcedVets[c.ID],
			"is_new_player":   !isVet,
			"is_coach":        !overrides[c.ID] && !optedOut && isVet && ruleOn,
			"explicit_coach":  explicit,
			"opted_out":       optedOut,
			"play_override":   overrides[c.ID],
			"locked_to_coach": locked,
		})
	}

	respondJSON(w, map[string]any{
		"success":  true,
		"league":   lg,
		"rule_on":  ruleOn,
		"players":  out,
		"veterans": out,
	})
}

// POST /api/mod/rookie/veteran-flag
// Body: {"player_id":"123","veteran":true,"league":"rookie"}
//
// veteran=true  → treat this player as a veteran (Coach only), even though they
// never played a Varsity match — a seasoned rookie-leaguer can still be far too
// strong for the league.
// veteran=false → back to a normal new player.
func HandleSetRookieVeteranFlag(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}

	var req struct {
		PlayerID FlexibleID `json:"player_id"`
		Veteran  *bool      `json:"veteran"`
		League   string     `json:"league"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.PlayerID.Int64() == 0 || req.Veteran == nil {
		modJSONErr(w, http.StatusBadRequest, "player_id and veteran are required")
		return
	}
	pid := req.PlayerID.Int64()

	lg := normalizeLeague(req.League)
	if lg == "" {
		lg = normalizeLeague(r.Header.Get("X-ECGL-League"))
	}
	if lg == "" {
		lg = LeagueRookie
	}
	if lg != LeagueRookie {
		modJSONErr(w, http.StatusBadRequest,
			"the veteran flag only applies to the Rookie League")
		return
	}

	var p Player
	if err := DB.Select("id, display_name, username").First(&p, pid).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "player not found")
		return
	}

	varsityVet := isMainLeagueVeteran(pid)
	if !*req.Veteran && varsityVet {
		modJSONErr(w, http.StatusBadRequest,
			"that player is a Varsity veteran — use the play override instead of unmarking them")
		return
	}

	if err := setPlayerForceVeteran(pid, lg, *req.Veteran); err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to save the veteran flag: "+err.Error())
		return
	}

	if *req.Veteran {
		// Marking someone a veteran overrides any earlier "allow play" grace.
		DB.Exec(`UPDATE player_league_roles SET play_override = FALSE, updated_at = NOW()
			WHERE player_id = ? AND league = ?`, pid, lg)
	}

	// Move them between Coach and Member on their current team, and between the
	// coach and player draft pools. Someone who deliberately registered as a
	// coach stays on the coach side.
	explicitCoach := isExplicitRookieCoach(pid)
	if *req.Veteran {
		applyPlayOverrideToRoster(pid, lg, false)
	} else if !explicitCoach {
		applyPlayOverrideToRoster(pid, lg, true)
	}

	kind := draftKindPlayer
	if *req.Veteran || explicitCoach {
		kind = draftKindCoach
	}
	DB.Model(&DraftSignup{}).
		Where("player_id = ? AND league = ? AND status = ?", pid, lg, draftPending).
		Update("kind", kind)

	action := "unmarked as veteran"
	if *req.Veteran {
		action = "marked as veteran"
	}

	RecordModAudit(ModAuditEntry{
		Action:  "rookie_veteran_flag",
		Method:  r.Method,
		Path:    r.URL.Path,
		Target:  fmt.Sprintf("player %d (%s) %s", pid, p.DisplayName, action),
		ActorID: actor,
	})
	LogGeneral(fmt.Sprintf("🧑‍🏫 **%s** %s for the Rookie League", p.DisplayName, action), lg)

	respondJSON(w, map[string]any{
		"success":       true,
		"player_id":     fmt.Sprintf("%d", pid),
		"league":        lg,
		"is_veteran":    *req.Veteran || varsityVet,
		"force_veteran": *req.Veteran,
		"message":       fmt.Sprintf("%s %s.", p.DisplayName, action),
	})
}

// POST /api/mod/rookie/play-override
// Body: {"player_id":"123","allow":true,"league":"rookie"}
//
// allow=true  → this veteran counts as a normal (playing) rookie player
// allow=false → back to the default veteran rule (Coach only)
func HandleSetRookiePlayOverride(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireLeagueMod(w, r)
	if !ok {
		return
	}

	var req struct {
		PlayerID FlexibleID `json:"player_id"`
		Allow    *bool      `json:"allow"`
		League   string     `json:"league"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.PlayerID.Int64() == 0 || req.Allow == nil {
		modJSONErr(w, http.StatusBadRequest, "player_id and allow are required")
		return
	}
	pid := req.PlayerID.Int64()

	lg := normalizeLeague(req.League)
	if lg == "" {
		lg = normalizeLeague(r.Header.Get("X-ECGL-League"))
	}
	if lg == "" {
		lg = LeagueRookie
	}
	if lg != LeagueRookie {
		modJSONErr(w, http.StatusBadRequest,
			"the veteran play override only applies to the Rookie League")
		return
	}

	var p Player
	if err := DB.Select("id, display_name, username").First(&p, pid).Error; err != nil {
		modJSONErr(w, http.StatusNotFound, "player not found")
		return
	}

	isVet := isMainLeagueVeteran(pid)
	if *req.Allow && !isVet {
		modJSONErr(w, http.StatusBadRequest,
			"that player already plays as a normal rookie — no override needed")
		return
	}
	if *req.Allow && isExplicitRookieCoach(pid) && !isVet {
		modJSONErr(w, http.StatusBadRequest,
			"that player registered as a coach on purpose — remove their coach registration first")
		return
	}

	if err := setPlayerPlayOverride(pid, lg, *req.Allow); err != nil {
		modJSONErr(w, http.StatusInternalServerError, "failed to save the override: "+err.Error())
		return
	}

	// Any team they are already on needs to move between Coach and Member.
	applyPlayOverrideToRoster(pid, lg, *req.Allow)

	// Keep their pending draft signup in the matching pool.
	if *req.Allow {
		DB.Model(&DraftSignup{}).
			Where("player_id = ? AND league = ? AND status = ?", pid, lg, draftPending).
			Update("kind", draftKindPlayer)
	} else {
		DB.Model(&DraftSignup{}).
			Where("player_id = ? AND league = ? AND status = ?", pid, lg, draftPending).
			Update("kind", draftKindCoach)
	}

	action := "revoked"
	if *req.Allow {
		action = "granted"
	}

	RecordModAudit(ModAuditEntry{
		Action:  "rookie_play_override_" + action,
		Method:  r.Method,
		Path:    r.URL.Path,
		Target:  fmt.Sprintf("player %d (%s)", pid, p.DisplayName),
		ActorID: actor,
	})

	detail := "back to Coach only"
	if *req.Allow {
		detail = "counts as a normal rookie player"
	}
	LogGeneral(fmt.Sprintf("🧑‍🏫 Rookie play override %s for **%s** — %s",
		action, p.DisplayName, detail), lg)

	respondJSON(w, map[string]any{
		"success":         true,
		"player_id":       fmt.Sprintf("%d", pid),
		"league":          lg,
		"is_veteran":      isVet,
		"play_override":   *req.Allow,
		"locked_to_coach": isVet && !*req.Allow,
		"message":         fmt.Sprintf("Rookie play override %s for %s.", action, p.DisplayName),
	})
}

// applyPlayOverrideToRoster flips this player's existing rookie team membership
// between Coach and Member so a granted override immediately makes them a
// playable roster member.
func applyPlayOverrideToRoster(playerID int64, lg string, allow bool) {
	type row struct {
		TeamID uint
		Role   string
	}
	var rows []row
	DB.Raw(`
		SELECT tm.team_id, COALESCE(tm.role, '') AS role
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.player_id = ? AND t.league = ?
	`, playerID, lg).Scan(&rows)

	for _, r := range rows {
		isCoach := strings.EqualFold(strings.TrimSpace(r.Role), "coach")

		switch {
		case allow && isCoach:
			DB.Exec(`UPDATE team_members SET role = 'Member' WHERE team_id = ? AND player_id = ?`,
				r.TeamID, playerID)
		case !allow && !isCoach:
			// Only demote to Coach when the team has a free coach slot.
			if blocked, _ := coachRoleBlocked(r.TeamID); blocked {
				continue
			}
			DB.Exec(`UPDATE team_members SET role = 'Coach' WHERE team_id = ? AND player_id = ?`,
				r.TeamID, playerID)
		}
	}
}
