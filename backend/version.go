package main

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
)

// dataVersion is the global counter (kept for backward compatibility). The
// per-scope counters below are what clients actually compare, so an action only
// blocks when data *relevant to that action* changed — not on every write.
var dataVersion atomic.Int64

// Resource scopes. A write bumps only the scope(s) it touches.
const (
	scopeMatches  = "matches"
	scopeTeams    = "teams"
	scopePlayers  = "players"
	scopeSettings = "settings"
	scopeMedia    = "media"
	scopeMisc     = "misc"
)

var allScopes = []string{
	scopeMatches, scopeTeams, scopePlayers, scopeSettings, scopeMedia, scopeMisc,
}

var (
	scopeMu       sync.RWMutex
	scopeVersions = map[string]int64{}
)

// scopeForTable maps a database table to the resource scope clients check
// before acting on that kind of data.
func scopeForTable(table string) string {
	switch table {
	case "matches", "match_scores", "match_rosters", "cast_logs", "cast_log_multis",
		"cast_logs_multi", "finals_teams", "finals_archives":
		return scopeMatches
	case "teams", "team_members", "team_join_requests", "team_logos", "team_archives",
		"challenge_requests":
		return scopeTeams
	case "players", "player_histories", "player_history", "player_availabilities",
		"player_availability", "player_stats_archive":
		return scopePlayers
	case "settings", "league_settings", "config", "rule_sections":
		return scopeSettings
	case "clips":
		return scopeMedia
	}
	// Prefix fallbacks so newly added tables still land in a sensible scope.
	switch {
	case strings.HasPrefix(table, "match") || strings.HasPrefix(table, "finals") ||
		strings.HasPrefix(table, "cast"):
		return scopeMatches
	case strings.HasPrefix(table, "team") || strings.HasPrefix(table, "challenge"):
		return scopeTeams
	case strings.HasPrefix(table, "player"):
		return scopePlayers
	case strings.HasPrefix(table, "setting") || table == "config":
		return scopeSettings
	}
	if table == "" {
		return ""
	}
	return scopeMisc
}

// InitDataVersion seeds the version and registers GORM write callbacks that
// bump it automatically after every successful Create/Update/Delete.
// Must be called after InitDB().
func InitDataVersion() {
	// Seed from the current unix time so a server restart (redeploy) also
	// changes every scope, forcing open tabs to pick up the new bundle.
	seed := time.Now().Unix()
	dataVersion.Store(seed)
	scopeMu.Lock()
	for _, s := range allScopes {
		scopeVersions[s] = seed
	}
	scopeMu.Unlock()

	bump := func(db *gorm.DB) {
		if db == nil || db.Error != nil {
			return
		}
		// Skip per-user / internal churn that should NOT force everyone else
		// to reload (notifications are personal; mod audit logs are internal).
		switch db.Statement.Table {
		case "notifications", "mod_audit_logs":
			return
		}
		bumpDataVersion(scopeForTable(db.Statement.Table))
	}

	DB.Callback().Create().After("gorm:create").Register("ecgl:bump-data-version", bump)
	DB.Callback().Update().After("gorm:update").Register("ecgl:bump-data-version", bump)
	DB.Callback().Delete().After("gorm:delete").Register("ecgl:bump-data-version", bump)

	log.Println("✅ Data-version callbacks registered")
}

// bumpDataVersion bumps the global counter and any explicit scopes. Handlers
// that write via raw SQL (DB.Exec) must pass the scopes they affected because
// raw SQL bypasses the GORM callbacks.
func bumpDataVersion(scopes ...string) {
	dataVersion.Add(1)
	scopeMu.Lock()
	for _, s := range scopes {
		if s == "" {
			continue
		}
		scopeVersions[s]++
	}
	scopeMu.Unlock()
}

// CurrentDataVersion returns the current global data version.
func CurrentDataVersion() int64 {
	return dataVersion.Load()
}

// currentScopeVersions returns a snapshot of every scope version.
func currentScopeVersions() map[string]int64 {
	scopeMu.RLock()
	defer scopeMu.RUnlock()
	out := make(map[string]int64, len(scopeVersions))
	for k, v := range scopeVersions {
		out[k] = v
	}
	return out
}

// GET /api/version — public. Returns per-scope versions. Clients compare only
// the scopes relevant to the action they are about to perform, so unrelated
// changes no longer force a reload. `?scope=` returns a single value.
func HandleGetVersion(w http.ResponseWriter, r *http.Request) {
	scopes := currentScopeVersions()
	if want := strings.TrimSpace(r.URL.Query().Get("scope")); want != "" {
		resp := map[string]any{"version": CurrentDataVersion()}
		if v, ok := scopes[want]; ok {
			resp["scope"] = want
			resp["value"] = v
		}
		respondJSON(w, resp)
		return
	}
	respondJSON(w, map[string]any{
		"version": CurrentDataVersion(),
		"scopes":  scopes,
	})
}
