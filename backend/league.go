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
// A league hidden from the public (ROOKIE_LEAGUE_PUBLIC=false) silently falls
// back to the main league for anyone who isn't a league mod or dev.
func LeagueContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		league := LeagueMain
		if v := r.Header.Get("X-ECGL-League"); strings.TrimSpace(v) != "" {
			league = normalizeLeague(v)
		} else if v := r.URL.Query().Get("league"); strings.TrimSpace(v) != "" {
			league = normalizeLeague(v)
		}

		if !leagueIsPublic(league) && !sessionIsStaff(r) {
			league = LeagueMain
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

		// Repair player history rows whose team was hard-deleted: the FK nulls
		// team_id but the team name survives, so re-link them when a team with
		// that name exists again (keeps former-member lists intact).
		`UPDATE player_history ph SET team_id = t.id
		 FROM teams t
		 WHERE ph.team_id IS NULL
		   AND COALESCE(ph.team_name, '') <> ''
		   AND LOWER(t.name) = LOWER(ph.team_name)`,
		`CREATE INDEX IF NOT EXISTS idx_team_archives_league ON team_archives (league)`,
		`CREATE INDEX IF NOT EXISTS idx_psa_league ON player_stats_archive (league)`,

		// Finals archives are now unique per (league, season) instead of season.
		`ALTER TABLE finals_archive DROP CONSTRAINT IF EXISTS finals_archive_season_key`,
		`DROP INDEX IF EXISTS idx_finals_archives_season`,
		`DROP INDEX IF EXISTS idx_finals_archive_season`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_finals_archive_league_season ON finals_archive (league, season)`,

		// One-time migration markers.
		`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ DEFAULT NOW())`,

		// Per-league player stats. Main league mirrors the legacy columns; other
		// leagues always start each player at the default rating with a clean
		// record so Varsity progress never carries over.
	}
	defRating := getEnvInt("DEFAULT_PLAYER_RATING", 800)
	stmts = append(stmts,
		`INSERT INTO player_league_stats (player_id, league, rating, wins, losses, matches)
		 SELECT id, 'main', rating, wins, losses, matches FROM players
		 ON CONFLICT (player_id, league) DO NOTHING`,
		fmt.Sprintf(`INSERT INTO player_league_stats (player_id, league, rating, wins, losses, matches)
		 SELECT DISTINCT tm.player_id, t.league,
		   CASE WHEN t.league = 'main' THEN p.rating  ELSE %d END,
		   CASE WHEN t.league = 'main' THEN p.wins    ELSE 0 END,
		   CASE WHEN t.league = 'main' THEN p.losses  ELSE 0 END,
		   CASE WHEN t.league = 'main' THEN p.matches ELSE 0 END
		 FROM team_members tm
		 JOIN teams t ON t.id = tm.team_id
		 JOIN players p ON p.id = tm.player_id
		 ON CONFLICT (player_id, league) DO NOTHING`, defRating),
	)
	for _, s := range stmts {
		if err := DB.Exec(s).Error; err != nil {
			log.Printf("⚠️ ensureLeagueSchema: %v", err)
		}
	}

	ensureLeagueSettings(LeagueMain)
	ensureLeagueSettings(LeagueRookie)

	// One-time reset: rookie records created before per-league stats existed were
	// seeded from Varsity values. Reset them to a clean default record. Varsity
	// (main) rows are never touched.
	if !migrationApplied("rookie_stats_reset_default_v1") {
		if err := DB.Exec(
			`UPDATE player_league_stats SET rating = ?, wins = 0, losses = 0, matches = 0 WHERE league = ?`,
			defRating, LeagueRookie,
		).Error; err != nil {
			log.Printf("⚠️ rookie stats reset: %v", err)
		} else if err := markMigrationApplied("rookie_stats_reset_default_v1"); err != nil {
			log.Printf("⚠️ rookie stats reset marker: %v", err)
		}
	}
}

// migrationApplied reports whether a one-time migration has already run.
func migrationApplied(name string) bool {
	var count int64
	if err := DB.Raw(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// markMigrationApplied records that a one-time migration has run.
func markMigrationApplied(name string) error {
	return DB.Exec(
		`INSERT INTO schema_migrations (name) VALUES (?) ON CONFLICT (name) DO NOTHING`,
		name,
	).Error
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

// leagueAllowsTeamCreation reports whether players may create their own teams
// in a league. Varsity always allows it; the Rookie League can be switched to
// draft-only with ROOKIE_TEAM_CREATION_ENABLED=false.
func leagueAllowsTeamCreation(league string) bool {
	if normalizeLeague(league) != LeagueRookie {
		return true
	}
	return getEnv("ROOKIE_TEAM_CREATION_ENABLED", "true") == "true"
}

// coachDraftEnabled reports whether the league runs a separate coach draft.
func coachDraftEnabled(league string) bool {
	if normalizeLeague(league) != LeagueRookie {
		return false
	}
	return getEnv("ROOKIE_COACH_DRAFT_ENABLED", "false") == "true"
}

// leagueIsPublic reports whether a league is visible to non-staff visitors.
// The Rookie League can be hidden with ROOKIE_LEAGUE_PUBLIC=false.
func leagueIsPublic(league string) bool {
	if normalizeLeague(league) != LeagueRookie {
		return true
	}
	return getEnv("ROOKIE_LEAGUE_PUBLIC", "true") == "true"
}

// sessionIsStaff reports whether the logged-in session user is a league mod or
// dev. Role checks are cached (see discordMemberRoles), so this is cheap enough
// to call from middleware.
func sessionIsStaff(r *http.Request) bool {
	if r == nil {
		return false
	}
	session, _ := store.Get(r, "session")
	discordID, _ := session.Values["discord_id"].(string)
	if strings.TrimSpace(discordID) == "" {
		return false
	}
	if userHasDiscordRole(discordID, getEnv("DISCORD_LEAGUE_MOD_ROLE_ID", "")) {
		return true
	}
	return userHasDiscordRole(discordID, getEnv("DISCORD_DEV_ROLE_ID", ""))
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
	return "Varsity League"
}

// leagueTag returns a Discord-friendly prefix that identifies the league.
func leagueTag(league string) string {
	if normalizeLeague(league) == LeagueRookie {
		return "🧪 **[Rookie League]**"
	}
	return "🏆 **[Varsity League]**"
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
// This is an ALL-SEASON check: a player is a main-league player if they are on a
// main-league team now, or have ever been on one in any season.
//
// Two optional ceilings then relax it so weak/new players who only just made a
// main team still count as "new":
//   - ROOKIE_VETERAN_MAX_RATING: players at or below this rating stay "new"
//   - ROOKIE_VETERAN_MAX_GAMES:  players at or below this many matches stay "new"
//
// A main-league player is a veteran if they are above either ceiling. If both
// are 0 (unset) any main-league participation makes them a veteran.
func isMainLeagueVeteran(playerID int64) bool {
	onMain := false
	var count int64

	// Current main-league membership.
	DB.Raw(`
		SELECT COUNT(*) FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.player_id = ? AND t.league = ?
	`, playerID, LeagueMain).Scan(&count)
	if count > 0 {
		onMain = true
	} else {
		// Historical membership (all seasons). If the team row still exists we
		// use its league. If the team was hard-deleted, infer the league from
		// that team's matches; if that is unknowable, assume main-league so the
		// preserved player history isn't silently ignored.
		DB.Raw(`
			SELECT COUNT(*) FROM player_history ph
			LEFT JOIN teams t ON t.id = ph.team_id
			WHERE ph.player_id = ?
			  AND (
				t.league = 'main'
				OR (
					t.id IS NULL
					AND COALESCE((
						SELECT bool_or(m.league = 'main')
						FROM matches m
						WHERE m.team_a_id = ph.team_id OR m.team_b_id = ph.team_id
					), TRUE)
				)
			  )
		`, playerID).Scan(&count)
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
	// Prefer the main-league per-league stats row (the legacy player columns are
	// kept in sync for main, but fall back to them if the row is missing).
	DB.Raw(`SELECT rating, matches FROM player_league_stats WHERE player_id = ? AND league = ?`, playerID, LeagueMain).Scan(&p)
	if p.Rating == 0 && p.Matches == 0 {
		DB.Raw(`SELECT rating, matches FROM players WHERE id = ?`, playerID).Scan(&p)
	}

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

// League formats. "season" is the default: a weekly generated schedule plus
// finals. Every other format is run manually by mods (no auto scheduling).
const (
	FormatSeason     = "season"
	FormatRoundRobin = "round_robin"
	FormatSingleElim = "single_elim"
	FormatDoubleElim = "double_elim"
	FormatGroupStage = "group_stage"
)

// normalizeLeagueFormat maps any stored/client value to a known format id.
func normalizeLeagueFormat(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "round_robin", "roundrobin", "round-robin", "rr":
		return FormatRoundRobin
	case "single_elim", "single-elim", "singleelim", "single_elimination", "knockout":
		return FormatSingleElim
	case "double_elim", "double-elim", "doubleelim", "double_elimination":
		return FormatDoubleElim
	case "group_stage", "group-stage", "groupstage", "groups", "group":
		return FormatGroupStage
	default:
		return FormatSeason
	}
}

// leagueFormat returns the format for a league. The main league always runs a
// season; the Rookie League can be switched with ROOKIE_FORMAT.
func leagueFormat(league string) string {
	if normalizeLeague(league) == LeagueRookie {
		return normalizeLeagueFormat(getEnv("ROOKIE_FORMAT", FormatSeason))
	}
	return FormatSeason
}

// leagueFormatLabel is a human-readable name for a format id.
func leagueFormatLabel(format string) string {
	switch normalizeLeagueFormat(format) {
	case FormatRoundRobin:
		return "Round Robin"
	case FormatSingleElim:
		return "Single Elimination"
	case FormatDoubleElim:
		return "Double Elimination"
	case FormatGroupStage:
		return "Group Stage → Playoffs"
	default:
		return "Season"
	}
}

// leagueUsesWeeklySchedule reports whether the weekly schedule generator applies
// to this league. Only the "season" format is auto-scheduled; tournaments are
// run manually by mods.
func leagueUsesWeeklySchedule(league string) bool {
	return leagueFormat(league) == FormatSeason
}

// leagueTournamentStarted reports whether a non-season league has already begun
// (i.e. tournament matches exist for the current season). Once a tournament has
// started no new teams may enter — season leagues stay open all year.
func leagueTournamentStarted(league string) bool {
	if leagueUsesWeeklySchedule(league) {
		return false
	}

	season := strings.TrimSpace(seasonForLeague(league))
	if season == "" {
		return false
	}

	var count int64
	DB.Model(&Match{}).
		Where("league = ? AND season = ?", normalizeLeague(league), season).
		Where("COALESCE(bracket, '') <> '' OR match_code ILIKE ?", "%-Tourney-%").
		Count(&count)

	return count > 0
}

// leagueMinTeamPlayers returns the minimum roster size for a league. The Rookie
// League can override the global value with ROOKIE_MIN_TEAM_PLAYERS.
func leagueMinTeamPlayers(league string) int {
	if normalizeLeague(league) == LeagueRookie {
		if v := getEnvInt("ROOKIE_MIN_TEAM_PLAYERS", 0); v > 0 {
			return v
		}
	}
	return getEnvInt("MIN_TEAM_PLAYERS", 3)
}

// leagueMaxTeamPlayers returns the maximum roster size for a league. The Rookie
// League can override the global value with ROOKIE_MAX_TEAM_PLAYERS.
func leagueMaxTeamPlayers(league string) int {
	if normalizeLeague(league) == LeagueRookie {
		if v := getEnvInt("ROOKIE_MAX_TEAM_PLAYERS", 0); v > 0 {
			return v
		}
	}
	return getEnvInt("MAX_TEAM_PLAYERS", 5)
}
