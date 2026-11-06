package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// GET /api/caster/matches
//
// Public, machine-readable caster feed for the Discord caster bot and any other
// consumer. No authentication required.
//
// Returns one entry per match with teams, ranks, ratings, logos, rosters,
// scheduling, the series format and the current cast assignments
// (casters / cam op / sideline).
//
// Query params (all optional):
//
//	league   main | rookie   (default: main)
//	status   e.g. Scheduled
//	confirmed=1              (only matches both teams schedule-confirmed)
//	limit    int             (default 200)
func HandleCasterBotMatches(w http.ResponseWriter, r *http.Request) {
	lg := normalizeLeague(r.URL.Query().Get("league"))

	// A league hidden from the public can't be read through the open feed.
	if !leagueIsPublic(lg) && !sessionIsStaff(r) {
		lg = LeagueMain
	}
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	onlyConfirmed := r.URL.Query().Get("confirmed") == "1"

	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	q := DB.Model(&Match{}).
		Where("league = ?", lg).
		Where("COALESCE(archived, FALSE) = FALSE").
		Where("winner_id IS NULL AND loser_id IS NULL")

	if statusFilter != "" {
		q = q.Where("LOWER(status) = LOWER(?)", statusFilter)
	} else {
		// Default: only matches still waiting to be played.
		q = q.Where("LOWER(status) = 'scheduled'")
	}
	if onlyConfirmed {
		q = q.Where("team_a_schedule_confirmed = TRUE AND team_b_schedule_confirmed = TRUE")
	}

	var matches []Match
	if err := q.Order("scheduled_date ASC NULLS LAST, id ASC").Limit(limit).Find(&matches).Error; err != nil {
		log.Printf("❌ HandleCasterBotMatches: %v", err)
		http.Error(w, "Failed to load matches", http.StatusInternalServerError)
		return
	}

	ranks := teamRankMap(lg)
	baseURL := strings.TrimRight(getEnv("FRONTEND_URL", "https://ecgleague.com"), "/")

	type entry struct {
		ID          uint       `json:"id"`
		MatchID     string     `json:"match_id"`
		TeamA       string     `json:"team_a"`
		TeamB       string     `json:"team_b"`
		TeamARank   *int       `json:"team_a_rank"`
		TeamBRank   *int       `json:"team_b_rank"`
		TeamARating int        `json:"team_a_rating"`
		TeamBRating int        `json:"team_b_rating"`
		TeamALogo   string     `json:"team_a_logo"`
		TeamBLogo   string     `json:"team_b_logo"`
		TeamARoster []string   `json:"team_a_roster"`
		TeamBRoster []string   `json:"team_b_roster"`
		MatchDate   string     `json:"match_date"`
		MatchTime   string     `json:"match_time"`
		Timestamp   int64      `json:"match_timestamp"`
		WeekNumber  string     `json:"week_number"`
		MatchType   string     `json:"match_type"`
		StreamCh    *string    `json:"stream_channel"`
		Casters     []castCrew `json:"casters"`
		CamOp       *castCrew  `json:"cam_op"`
		Sideline    *castCrew  `json:"sideline"`
	}

	out := make([]entry, 0, len(matches))

	for _, m := range matches {
		var teamA, teamB Team
		DB.First(&teamA, m.TeamAID)
		DB.First(&teamB, m.TeamBID)

		// Schedule — fall back to the proposed date when nothing is set.
		when := m.ScheduledDate
		if when == nil {
			when = m.ProposedDate
		}

		dateStr := ""
		timeStr := ""
		var ts int64
		if when != nil {
			t := when.UTC()
			dateStr = t.Format("2006-01-02")
			timeStr = t.Format("15:04")
			ts = t.Unix()
		}

		// Series format — the caster board selection wins, otherwise derive it
		// from the number of played maps (default Bo3).
		matchType := castMatchTypeOrDefault(m.ID, matchMapCount(m))

		// Cast assignments.
		var casts []CastAssignment
		DB.Where("match_id = ?", m.ID).Find(&casts)

		casters := []castCrew{}
		var camOp, sideline *castCrew
		for _, c := range casts {
			person, ok := playerCastInfo(c.PlayerID)
			if !ok {
				continue
			}
			switch c.Slot {
			case "caster1":
				person.Slot = 1
				casters = append(casters, person)
			case "caster2":
				person.Slot = 2
				casters = append(casters, person)
			case "camera":
				camOp = &person
			case "sideline":
				sideline = &person
			}
		}
		sort.Slice(casters, func(i, j int) bool { return casters[i].Slot < casters[j].Slot })

		var streamCh *string
		var castLog CastLogMulti
		if err := DB.Where("match_id = ?", m.ID).First(&castLog).Error; err == nil {
			if strings.TrimSpace(castLog.StreamURL) != "" {
				s := castLog.StreamURL
				streamCh = &s
			}
		}

		weekNumber := strings.TrimSpace(m.Week)
		if weekNumber == "" {
			weekNumber = strings.TrimSpace(m.Season)
		}

		out = append(out, entry{
			ID:          m.ID,
			MatchID:     m.MatchCode,
			TeamA:       teamA.Name,
			TeamB:       teamB.Name,
			TeamARank:   rankPtr(ranks, teamA.ID),
			TeamBRank:   rankPtr(ranks, teamB.ID),
			TeamARating: teamA.Rating,
			TeamBRating: teamB.Rating,
			TeamALogo:   absoluteTeamLogo(baseURL, teamA),
			TeamBLogo:   absoluteTeamLogo(baseURL, teamB),
			TeamARoster: teamRosterNames(teamA.ID),
			TeamBRoster: teamRosterNames(teamB.ID),
			MatchDate:   dateStr,
			MatchTime:   timeStr,
			Timestamp:   ts,
			WeekNumber:  weekNumber,
			MatchType:   matchType,
			StreamCh:    streamCh,
			Casters:     casters,
			CamOp:       camOp,
			Sideline:    sideline,
		})
	}

	respondJSON(w, map[string]any{
		"success": true,
		"matches": out,
	})
}

// teamRankMap returns team id → leaderboard position for a league.
func teamRankMap(league string) map[uint]int {
	var ids []uint
	DB.Table("teams").
		Where("league = ?", normalizeLeague(league)).
		Where("COALESCE(status, '') <> 'Test'").
		Order("rating DESC, wins DESC, losses ASC").
		Pluck("id", &ids)

	out := make(map[uint]int, len(ids))
	for i, id := range ids {
		out[id] = i + 1
	}
	return out
}

func rankPtr(ranks map[uint]int, teamID uint) *int {
	if r, ok := ranks[teamID]; ok {
		n := r
		return &n
	}
	return nil
}

// teamStanding is a team's leaderboard snapshot for a league.
type teamStanding struct {
	Rank     *int   `json:"rank"`
	Rating   int    `json:"rating"`
	Wins     int    `json:"wins"`
	Losses   int    `json:"losses"`
	Matches  int    `json:"matches"`
	Division string `json:"division"`
	Tier     string `json:"tier"`
}

// buildStanding returns rank/rating info for a team using a precomputed rank map.
func buildStanding(ranks map[uint]int, t Team) teamStanding {
	placementMatches := getEnvInt("PLACEMENT_MATCHES", 3)

	s := teamStanding{
		Rank:    rankPtr(ranks, t.ID),
		Rating:  t.Rating,
		Wins:    t.Wins,
		Losses:  t.Losses,
		Matches: t.Matches,
	}
	if s.Matches < placementMatches {
		s.Division = "Placement"
	} else {
		s.Division, s.Tier = GetDivisionTier(t.Rating)
	}
	return s
}

// absoluteTeamLogo returns a full URL for the team's logo.
func absoluteTeamLogo(baseURL string, t Team) string {
	logo := strings.TrimSpace(t.LogoURL)
	if logo == "" {
		return fmt.Sprintf("%s/api/team/logo/%d", baseURL, t.ID)
	}
	if strings.HasPrefix(logo, "http://") || strings.HasPrefix(logo, "https://") {
		return logo
	}
	return baseURL + logo
}

// teamRosterNames returns display names, captain first.
func teamRosterNames(teamID uint) []string {
	type row struct {
		Name string
	}
	var rows []row
	DB.Raw(`
		SELECT COALESCE(NULLIF(p.display_name, ''), p.username) AS name
		FROM team_members tm
		JOIN players p ON p.id = tm.player_id
		WHERE tm.team_id = ?
		ORDER BY CASE
		           WHEN tm.role = 'Captain' THEN 0
		           WHEN tm.role = 'Co-Captain' THEN 1
		           ELSE 2
		         END,
		         p.id ASC
	`, teamID).Scan(&rows)

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if strings.TrimSpace(r.Name) != "" {
			out = append(out, r.Name)
		}
	}
	return out
}

// castCrew is one person on the cast, with their avatar paired to the name.
// Slot is only set for the two caster seats (1 or 2).
type castCrew struct {
	UserID      string `json:"user_id"`
	Slot        int    `json:"slot,omitempty"`
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
	AvatarURL   string `json:"avatar_url"`
}

// castCrewFor returns the caster-board crew for a match: the two caster seats
// ordered by slot, plus cam op and sideline when assigned.
func castCrewFor(matchID uint) (casters []castCrew, camOp, sideline *castCrew) {
	var assigns []CastAssignment
	DB.Where("match_id = ?", matchID).Find(&assigns)

	casters = []castCrew{}
	for _, a := range assigns {
		person, ok := playerCastInfo(a.PlayerID)
		if !ok {
			continue
		}
		switch a.Slot {
		case "caster1":
			person.Slot = 1
			casters = append(casters, person)
		case "caster2":
			person.Slot = 2
			casters = append(casters, person)
		case "camera":
			camOp = &person
		case "sideline":
			sideline = &person
		}
	}
	sort.Slice(casters, func(i, j int) bool { return casters[i].Slot < casters[j].Slot })

	return casters, camOp, sideline
}

// playerCastInfo returns a cast-crew entry (name, username, avatar URL) for a
// player id. ok is false when the player no longer exists or has no name.
func playerCastInfo(id int64) (castCrew, bool) {
	var p Player
	if err := DB.Select("id, display_name, username, avatar").First(&p, id).Error; err != nil {
		return castCrew{}, false
	}

	display := strings.TrimSpace(p.DisplayName)
	if display == "" {
		display = strings.TrimSpace(p.Username)
	}
	if display == "" {
		return castCrew{}, false
	}

	return castCrew{
		UserID:      strconv.FormatInt(p.ID, 10),
		DisplayName: display,
		Username:    p.Username,
		AvatarURL:   discordAvatarURL(p.ID, p.Avatar),
	}, true
}

// discordAvatarURL turns a Discord avatar hash into a CDN URL.
func discordAvatarURL(id int64, hash string) string {
	h := strings.TrimSpace(hash)
	if id <= 0 || h == "" {
		return ""
	}
	if strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "https://") {
		return h
	}
	ext := "png"
	if strings.HasPrefix(h, "a_") {
		ext = "gif"
	}
	return fmt.Sprintf("https://cdn.discordapp.com/avatars/%d/%s.%s?size=1024", id, h, ext)
}

// matchMapCount returns how many maps have recorded scores.
func matchMapCount(m Match) int {
	var n int64
	DB.Model(&MatchScore{}).Where("match_id = ?", m.ID).Count(&n)
	return int(n)
}
