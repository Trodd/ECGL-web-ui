package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Tournament tooling for leagues that are NOT run as a regular season
// (ROOKIE_FORMAT = round_robin | single_elim | double_elim | group_stage).
//
// There is no automatic schedule for those formats: mods either generate the
// opening matchups here, or add individual matches from the Matchups graph.

// roundRobinRounds builds a circle-method schedule for n participants.
// It returns rounds, each holding pairs of 0-based indices into the team slice.
// An odd team count gets a bye each round (the extra index is skipped).
func roundRobinRounds(n int) [][][2]int {
	if n < 2 {
		return nil
	}

	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	if n%2 == 1 {
		order = append(order, -1) // bye
		n++
	}

	rounds := make([][][2]int, 0, n-1)
	for r := 0; r < n-1; r++ {
		pairs := make([][2]int, 0, n/2)
		for i := 0; i < n/2; i++ {
			a := order[i]
			b := order[n-1-i]
			if a == -1 || b == -1 {
				continue
			}
			pairs = append(pairs, [2]int{a, b})
		}
		rounds = append(rounds, pairs)

		// Rotate everything except the first entry.
		last := order[n-1]
		copy(order[2:], order[1:n-1])
		order[1] = last
	}
	return rounds
}

// tournamentBracketCode maps a bracket name to the short code used in match codes.
func tournamentBracketCode(bracket string) string {
	b := strings.ToLower(strings.TrimSpace(bracket))

	if strings.HasPrefix(b, "group") {
		suffix := strings.TrimPrefix(b, "group_")
		suffix = strings.TrimPrefix(suffix, "group")
		if suffix != "" {
			return "G" + strings.ToUpper(suffix)
		}
		return "G"
	}

	switch b {
	case "winners", "wb", "upper":
		return "WB"
	case "losers", "lb", "lower":
		return "LB"
	case "grand_final", "gf":
		return "GF"
	default:
		return "T"
	}
}

// normalizeTournamentBracket maps client input to a stored bracket name.
func normalizeTournamentBracket(bracket string) string {
	b := strings.ToLower(strings.TrimSpace(bracket))

	if strings.HasPrefix(b, "group") {
		if b == "group" || b == "groups" {
			return "group_a"
		}
		if !strings.HasPrefix(b, "group_") {
			b = "group_" + strings.TrimPrefix(b, "group")
		}
		return b
	}

	switch b {
	case "winners", "wb", "upper":
		return "winners"
	case "losers", "lb", "lower":
		return "losers"
	case "grand_final", "gf":
		return "grand_final"
	default:
		return ""
	}
}

// bracketSeedOrder returns the standard bracket seed order for a power-of-two
// bracket size, e.g. 8 -> [1 8 4 5 2 7 3 6]. Consecutive pairs are first-round
// matchups, and seeds above the team count are byes for the top seeds.
func bracketSeedOrder(size int) []int {
	if size <= 1 {
		return []int{1}
	}
	prev := bracketSeedOrder(size / 2)
	out := make([]int, 0, size)
	for _, s := range prev {
		out = append(out, s, size+1-s)
	}
	return out
}

// createTournamentMatch inserts a single scheduled tournament match and pings
// both captains.
func createTournamentMatch(lg, season, code, bracket string, round, slot int, a, b Team) (Match, error) {
	now := time.Now()

	m := Match{
		League:       lg,
		MatchCode:    code,
		TeamAID:      a.ID,
		TeamBID:      b.ID,
		ProposedDate: &now,
		Status:       "Scheduled",
		Season:       season,
		Bracket:      bracket,
		BracketRound: round,
		BracketSlot:  slot,
	}

	if err := DB.Create(&m).Error; err != nil {
		return m, err
	}

	body := fmt.Sprintf("A mod added a match: %s vs %s", a.Name, b.Name)
	link := fmt.Sprintf("/match/%d", m.ID)
	notifyTeamCaptains(a.ID, "match_added", "New Match Added", body, link)
	notifyTeamCaptains(b.ID, "match_added", "New Match Added", body, link)

	return m, nil
}

// eligibleTournamentTeams returns active teams of a league that meet the
// league's minimum roster size, strongest first.
func eligibleTournamentTeams(lg string) []Team {
	minPlayers := leagueMinTeamPlayers(lg)

	var all []Team
	DB.Where("league = ? AND status = ? AND name NOT IN ('(No Team)', 'League Sub')", lg, "Active").
		Find(&all)

	out := make([]Team, 0, len(all))
	for _, t := range all {
		var count int64
		DB.Table("team_members").Where("team_id = ?", t.ID).Count(&count)
		if int(count) >= minPlayers {
			out = append(out, t)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Rating != out[j].Rating {
			return out[i].Rating > out[j].Rating
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// POST /api/mod/tournament/generate  { replace: bool }
//
// Builds the opening matchups for the league's current format:
//   - round_robin / group_stage → every team plays every other team once
//   - single_elim / double_elim → seeded first round (1 v N, 2 v N-1, …), byes skipped
//
// Later rounds are added by mods from the Matchups graph once results are in.
func HandleModGenerateTournament(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	lg := leagueFromRequest(r)
	format := leagueFormat(lg)
	if format == FormatSeason {
		modJSONErr(w, http.StatusBadRequest,
			"this league runs a season — use the weekly schedule generator instead")
		return
	}

	season := strings.TrimSpace(seasonForLeague(lg))
	if season == "" {
		modJSONErr(w, http.StatusBadRequest, "season is not configured")
		return
	}

	var req struct {
		Replace bool `json:"replace"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	// Existing tournament matches?
	var existing int64
	DB.Model(&Match{}).
		Where("league = ? AND season = ?", lg, season).
		Where("COALESCE(bracket, '') <> '' OR match_code ILIKE ?", "%-Tourney-%").
		Count(&existing)

	if existing > 0 && !req.Replace {
		modJSONErr(w, http.StatusBadRequest, fmt.Sprintf(
			"%d tournament match(es) already exist — confirm to replace them", existing))
		return
	}

	if req.Replace && existing > 0 {
		if err := DB.Where("league = ? AND season = ?", lg, season).
			Where("COALESCE(bracket, '') <> '' OR match_code ILIKE ?", "%-Tourney-%").
			Delete(&Match{}).Error; err != nil {
			modJSONErr(w, http.StatusInternalServerError, "failed to clear old tournament matches")
			return
		}
	}

	teams := eligibleTournamentTeams(lg)
	if len(teams) < 2 {
		modJSONErr(w, http.StatusBadRequest,
			"need at least 2 active teams with a full roster to generate matchups")
		return
	}

	created := 0

	if format == FormatRoundRobin || format == FormatGroupStage {
		// Group stage splits teams into groups of 4; round robin is one big
		// group. Either way the full pairing list is generated up front — no
		// results are needed for later rounds.
		type groupPlan struct {
			label   string // "A", "B", … ("" for a single round-robin group)
			bracket string // "group_a", … ("" for round robin)
			members []Team
		}

		var groups []groupPlan

		if format == FormatGroupStage {
			const letters = "ABCDEFGH"
			gi := 0
			for start := 0; start < len(teams); start += 4 {
				end := start + 4
				if end > len(teams) {
					end = len(teams)
				}
				if end-start < 2 {
					continue
				}
				letter := string(letters[gi%len(letters)])
				groups = append(groups, groupPlan{
					label:   letter,
					bracket: "group_" + strings.ToLower(letter),
					members: teams[start:end],
				})
				gi++
			}
		} else {
			groups = append(groups, groupPlan{members: teams})
		}

		for _, g := range groups {
			schedule := roundRobinRounds(len(g.members))
			for ri, pairs := range schedule {
				round := ri + 1
				for si, p := range pairs {
					slot := si + 1

					var code string
					if g.label != "" {
						code = fmt.Sprintf("%s-Tourney-G%s-R%dS%d", season, g.label, round, slot)
					} else {
						code = fmt.Sprintf("%s-Tourney-RR-R%dS%d", season, round, slot)
					}

					if _, err := createTournamentMatch(
						lg, season, code, g.bracket, round, slot,
						g.members[p[0]], g.members[p[1]],
					); err != nil {
						log.Printf("⚠️ tournament generate (%s): %v", code, err)
						continue
					}
					created++
				}
			}
		}
	} else {
		// Elimination: full seeded bracket — every team is included. When the team
		// count isn't a power of two the top seeds get byes; everyone else plays a
		// play-in match in round 1.
		size := 1
		for size < len(teams) {
			size *= 2
		}
		if size > 128 {
			size = 128
		}

		order := bracketSeedOrder(size)
		slot := 0
		for i := 0; i+1 < len(order); i += 2 {
			aSeed, bSeed := order[i], order[i+1]
			slot++
			if aSeed > len(teams) || bSeed > len(teams) {
				continue // bye — that seed advances without playing
			}

			code := fmt.Sprintf("%s-Tourney-WB-R1S%d", season, slot)
			if _, err := createTournamentMatch(
				lg, season, code, "winners", 1, slot,
				teams[aSeed-1], teams[bSeed-1],
			); err != nil {
				log.Printf("⚠️ tournament generate (%s): %v", code, err)
				continue
			}
			created++
		}
	}

	if created == 0 {
		modJSONErr(w, http.StatusBadRequest, "no matchups could be generated for this league")
		return
	}

	LogGeneral(fmt.Sprintf("⚡ %s matchups generated for **%s** (%d matches)",
		leagueFormatLabel(format), LeagueDisplayName(lg), created), lg)

	respondJSON(w, map[string]any{
		"success": true,
		"created": created,
		"format":  leagueFormatLabel(format),
		"message": fmt.Sprintf("Generated %d %s matchup(s)", created, leagueFormatLabel(format)),
	})
}

// POST /api/mod/tournament/match
//
// Adds a single tournament match (used by the "+ add match" control on the
// Matchups graph).
// Body: { team_a, team_b, bracket?, round?, slot? }
func HandleModAddTournamentMatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	var req struct {
		TeamA   string `json:"team_a"`
		TeamB   string `json:"team_b"`
		Bracket string `json:"bracket"`
		Round   int    `json:"round"`
		Slot    int    `json:"slot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		modJSONErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.TeamA) == "" || strings.TrimSpace(req.TeamB) == "" ||
		strings.EqualFold(strings.TrimSpace(req.TeamA), strings.TrimSpace(req.TeamB)) {
		modJSONErr(w, http.StatusBadRequest, "pick two different teams")
		return
	}

	lg := leagueFromRequest(r)
	season := strings.TrimSpace(seasonForLeague(lg))
	if season == "" {
		modJSONErr(w, http.StatusBadRequest, "season is not configured")
		return
	}

	var a, b Team
	if err := DB.Where("league = ? AND LOWER(name) = LOWER(?)", lg, req.TeamA).First(&a).Error; err != nil {
		modJSONErr(w, http.StatusBadRequest, "team A not found in this league")
		return
	}
	if err := DB.Where("league = ? AND LOWER(name) = LOWER(?)", lg, req.TeamB).First(&b).Error; err != nil {
		modJSONErr(w, http.StatusBadRequest, "team B not found in this league")
		return
	}

	bracket := normalizeTournamentBracket(req.Bracket)
	round := req.Round
	if round <= 0 {
		round = 1
	}

	slot := req.Slot
	if slot <= 0 {
		var count int64
		DB.Model(&Match{}).
			Where("league = ? AND season = ? AND COALESCE(bracket, '') = ? AND bracket_round = ?", lg, season, bracket, round).
			Count(&count)
		slot = int(count) + 1
	}

	code := fmt.Sprintf("%s-Tourney-%s-R%dS%d", season, tournamentBracketCode(bracket), round, slot)

	// Ensure the code is unique within the league.
	base := code
	for i := 2; ; i++ {
		var dup int64
		DB.Model(&Match{}).Where("league = ? AND match_code = ?", lg, code).Count(&dup)
		if dup == 0 {
			break
		}
		code = fmt.Sprintf("%s-%d", base, i)
		if i > 50 {
			modJSONErr(w, http.StatusInternalServerError, "could not allocate a match code")
			return
		}
	}

	m, err := createTournamentMatch(lg, season, code, bracket, round, slot, a, b)
	if err != nil {
		log.Printf("❌ HandleModAddTournamentMatch: %v", err)
		modJSONErr(w, http.StatusInternalServerError, "failed to create match")
		return
	}

	respondJSON(w, map[string]any{
		"success":    true,
		"match_id":   m.ID,
		"match_code": m.MatchCode,
		"message":    fmt.Sprintf("Added %s: %s vs %s", m.MatchCode, a.Name, b.Name),
	})
}
