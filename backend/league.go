package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

// Leagues. The main league is the default so existing behaviour is unchanged;
// the rookie league is an additional, fully separate set of teams & matches.
const (
	LeagueMain   = "main"
	LeagueRookie = "rookie"
)

type leagueCtxKey struct{}

// normalizeLeague maps any client-supplied value to a known league id.
func normalizeLeague(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "rookie", "rookies", "rookieleague", "rookie-league", "r":
		return LeagueRookie
	default:
		return LeagueMain
	}
}

// leagueFromRequest returns the league resolved by LeagueContextMiddleware.
func leagueFromRequest(r *http.Request) string {
	if r == nil {
		return LeagueMain
	}
	if v, ok := r.Context().Value(leagueCtxKey{}).(string); ok && v != "" {
		return v
	}
	return LeagueMain
}

// LeagueContextMiddleware resolves the active league from the X-ECGL-League
// header (or ?league=) and stores it on the request context. Defaults to main.
func LeagueContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		league := LeagueMain
		if v := r.Header.Get("X-ECGL-League"); strings.TrimSpace(v) != "" {
			league = normalizeLeague(v)
		} else if v := r.URL.Query().Get("league"); strings.TrimSpace(v) != "" {
			league = normalizeLeague(v)
		}
		ctx := context.WithValue(r.Context(), leagueCtxKey{}, league)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ensureLeagueSchema adds the `league` discriminator columns, backfills any
// existing rows to the main league, and guarantees a settings row per league.
// Safe to run on every startup.
func ensureLeagueSchema() {
	stmts := []string{
		`ALTER TABLE teams ADD COLUMN IF NOT EXISTS league TEXT DEFAULT 'main'`,
		`ALTER TABLE matches ADD COLUMN IF NOT EXISTS league TEXT DEFAULT 'main'`,
		`ALTER TABLE league_settings ADD COLUMN IF NOT EXISTS league TEXT DEFAULT 'main'`,
		`UPDATE teams SET league = 'main' WHERE league IS NULL OR league = ''`,
		`UPDATE matches SET league = 'main' WHERE league IS NULL OR league = ''`,
		`UPDATE league_settings SET league = 'main' WHERE league IS NULL OR league = ''`,
		`CREATE INDEX IF NOT EXISTS idx_teams_league ON teams (league)`,
		`CREATE INDEX IF NOT EXISTS idx_matches_league ON matches (league)`,
		// Match codes only need to be unique within a league, so main and rookie
		// can both use e.g. "1-Week1-M001". Replace the legacy global unique.
		`ALTER TABLE matches DROP CONSTRAINT IF EXISTS uni_matches_match_code`,
		`DROP INDEX IF EXISTS uni_matches_match_code`,
		`DROP INDEX IF EXISTS idx_matches_match_code`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_matches_league_code ON matches (league, match_code)`,

		// Archives carry a league dimension so seasons don't mix.
		`ALTER TABLE team_archives ADD COLUMN IF NOT EXISTS league TEXT DEFAULT 'main'`,
		`ALTER TABLE player_stats_archive ADD COLUMN IF NOT EXISTS league TEXT DEFAULT 'main'`,
		`ALTER TABLE finals_archive ADD COLUMN IF NOT EXISTS league TEXT DEFAULT 'main'`,
		`UPDATE team_archives SET league = 'main' WHERE league IS NULL OR league = ''`,
		`UPDATE player_stats_archive SET league = 'main' WHERE league IS NULL OR league = ''`,
		`UPDATE finals_archive SET league = 'main' WHERE league IS NULL OR league = ''`,
		`CREATE INDEX IF NOT EXISTS idx_team_archives_league ON team_archives (league)`,
		`CREATE INDEX IF NOT EXISTS idx_psa_league ON player_stats_archive (league)`,

		// Finals archives are now unique per (league, season) instead of season.
		`ALTER TABLE finals_archive DROP CONSTRAINT IF EXISTS finals_archive_season_key`,
		`DROP INDEX IF EXISTS idx_finals_archives_season`,
		`DROP INDEX IF EXISTS idx_finals_archive_season`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_finals_archive_league_season ON finals_archive (league, season)`,
	}
	for _, s := range stmts {
		if err := DB.Exec(s).Error; err != nil {
			log.Printf("⚠️ ensureLeagueSchema: %v", err)
		}
	}

	ensureLeagueSettings(LeagueMain)
	ensureLeagueSettings(LeagueRookie)
}

// ensureLeagueSettings returns (creating if needed) the settings row for a
// league. The main league reuses the legacy single row (id = 1).
func ensureLeagueSettings(league string) LeagueSettings {
	league = normalizeLeague(league)

	var ls LeagueSettings
	if err := DB.Where("league = ?", league).Order("id ASC").First(&ls).Error; err == nil {
		return ls
	}

	if league == LeagueMain {
		if err := DB.First(&ls, 1).Error; err == nil {
			DB.Model(&ls).Update("league", LeagueMain)
			return ls
		}
	}

	ls = LeagueSettings{
		League:               league,
		CurrentWeek:          1,
		WeeklyChallengeLimit: 1,
		ChallengesEnabled:    true,
	}
	if err := DB.Create(&ls).Error; err != nil {
		log.Printf("⚠️ ensureLeagueSettings(%s): %v", league, err)
	}
	return ls
}

// leagueWeek returns the current week for a league.
func leagueWeek(league string) int {
	ls := ensureLeagueSettings(league)
	if ls.CurrentWeek > 0 {
		return ls.CurrentWeek
	}
	return 1
}

// seasonForLeague returns the current season label for a league. The main
// league keeps the legacy env/config behaviour; other leagues use their own
// config key (current_season_<league>) and fall back to "1".
func seasonForLeague(league string) string {
	league = normalizeLeague(league)

	if league == LeagueMain {
		if strings.TrimSpace(currentSeason) != "" {
			return strings.TrimSpace(currentSeason)
		}
		if v := os.Getenv("CURRENT_SEASON"); v != "" {
			return strings.TrimSpace(v)
		}
		return "1"
	}

	key := "current_season_" + league
	var v string
	DB.Raw(`SELECT value FROM config WHERE key = ? LIMIT 1`, key).Scan(&v)
	if strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if e := os.Getenv("CURRENT_SEASON_" + strings.ToUpper(league)); e != "" {
		return strings.TrimSpace(e)
	}
	return "1"
}

// LeagueDisplayName returns a human-readable league name.
func LeagueDisplayName(league string) string {
	if normalizeLeague(league) == LeagueRookie {
		return "Rookie League"
	}
	return "Main League"
}

// leagueTag returns a Discord-friendly prefix that identifies the league.
func leagueTag(league string) string {
	if normalizeLeague(league) == LeagueRookie {
		return "🧪 **[Rookie League]**"
	}
	return "🏆 **[Main League]**"
}

// tagLeague prepends a league tag to a message when a non-empty league is given.
func tagLeague(msg, league string) string {
	if strings.TrimSpace(league) == "" {
		return msg
	}
	return leagueTag(league) + " " + msg
}

// isMainLeagueVeteran reports whether a player is a main-league veteran.
//
// By default anyone who currently plays in, or has ever played in, the main
// league is a veteran. Two optional ceilings relax this so weak/new players
// who only just made a main team still count as "new":
//   - ROOKIE_VETERAN_MAX_RATING: players at or below this rating stay "new"
//   - ROOKIE_VETERAN_MAX_GAMES:  players at or below this many matches stay "new"
//
// A main-league player is a veteran if they are above either ceiling. If both
// are 0 (unset) any main-league participation makes them a veteran.
func isMainLeagueVeteran(playerID int64) bool {
	onMain := false
	var count int64

	DB.Raw(`
		SELECT COUNT(*) FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.player_id = ? AND t.league = ?
	`, playerID, LeagueMain).Scan(&count)
	if count > 0 {
		onMain = true
	} else {
		DB.Raw(`
			SELECT COUNT(*) FROM player_history ph
			JOIN teams t ON t.id = ph.team_id
			WHERE ph.player_id = ? AND t.league = ?
		`, playerID, LeagueMain).Scan(&count)
		onMain = count > 0
	}

	if !onMain {
		return false
	}

	maxRating := getEnvInt("ROOKIE_VETERAN_MAX_RATING", 0)
	maxGames := getEnvInt("ROOKIE_VETERAN_MAX_GAMES", 0)
	if maxRating <= 0 && maxGames <= 0 {
		return true // legacy behaviour: any main-league participation
	}

	var p struct {
		Rating  int
		Matches int
	}
	DB.Raw(`SELECT rating, matches FROM players WHERE id = ?`, playerID).Scan(&p)

	// Above the "still new" rating ceiling → veteran.
	if maxRating > 0 && p.Rating > maxRating {
		return true
	}
	// Above the "still new" games ceiling → veteran.
	if maxGames > 0 && p.Matches > maxGames {
		return true
	}
	return false
}

// rookieRosterCounts returns (# main-league veterans, # new players) on a team.
func rookieRosterCounts(teamID uint) (veterans int, rookies int) {
	var ids []int64
	DB.Table("team_members").Where("team_id = ?", teamID).Pluck("player_id", &ids)
	for _, id := range ids {
		if isMainLeagueVeteran(id) {
			veterans++
		}
	}
	return veterans, len(ids) - veterans
}

// canJoinRookieTeam enforces the rookie-league composition rule:
// 1 main-league veteran per N new players (N configurable; default 3).
func canJoinRookieTeam(teamID uint, playerID int64) (bool, string) {
	var team Team
	if err := DB.First(&team, teamID).Error; err != nil {
		return false, "Team not found"
	}
	if normalizeLeague(team.League) != LeagueRookie {
		return true, ""
	}

	// The whole rule can be toggled off from Settings → Rookie League.
	if getEnv("ROOKIE_VETERAN_RULE_ENABLED", "true") != "true" {
		return true, ""
	}

	// New players are always allowed.
	if !isMainLeagueVeteran(playerID) {
		return true, ""
	}

	perVeteran := getEnvInt("ROOKIE_NEW_PLAYERS_PER_VETERAN", 3)
	if perVeteran <= 0 {
		perVeteran = 3
	}

	veterans, rookies := rookieRosterCounts(teamID)
	// Require `perVeteran` new players for every veteran (including this one).
	if rookies < perVeteran*(veterans+1) {
		return false, fmt.Sprintf(
			"Rookie teams allow 1 main-league veteran per %d new players — add more new players first.",
			perVeteran,
		)
	}
	return true, ""
}
