package main

import (
	"context"
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
