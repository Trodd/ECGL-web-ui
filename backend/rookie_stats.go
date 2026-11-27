package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// Rookie League fairness helpers backed by echocombat.com stats.
//
// A League Mod can pull a player's real Echo Combat stats (kills/deaths, matches,
// win rate, time played) to decide whether they're too seasoned for the Rookie
// League — and to balance teams when a draft group looks lopsided.

// GET /api/mod/rookie/echo-stats?player_id=123[&period=all]
//
// Proxies echocombat.com so the API key never reaches the browser. Returns
// { success, configured, player_id, period, stats }.
func HandleGetEchoStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	pid := strings.TrimSpace(r.URL.Query().Get("player_id"))
	if pid == "" {
		modJSONErr(w, http.StatusBadRequest, "player_id is required")
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period"))

	if !echoStatsEnabled() {
		respondJSON(w, map[string]any{
			"success":    false,
			"configured": false,
			"player_id":  pid,
			"message":    "Echo Combat stats are not configured (set ECHOCOMBAT_API_KEY in .env).",
		})
		return
	}

	stats, err := echoStatsForPlayerRef(pid, period)
	if err != nil {
		respondJSON(w, map[string]any{
			"success":    false,
			"configured": true,
			"player_id":  pid,
			"message":    err.Error(),
		})
		return
	}

	respondJSON(w, map[string]any{
		"success":    true,
		"configured": true,
		"player_id":  pid,
		"period":     periodOrDefault(period),
		"stats":      stats,
	})
}

// GET /api/player/{id}/echo-stats[?period=all]
//
// The same stats for the public player detail page. Any logged-in member can
// read it (keeps the API key server-side and stops anonymous scraping of the
// upstream quota). Results are cached for 6 hours, so repeat views are free.
func HandleGetPlayerEchoStats(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	if id, _ := session.Values["discord_id"].(string); strings.TrimSpace(id) == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	pid := strings.TrimSpace(mux.Vars(r)["id"])
	if pid == "" {
		http.Error(w, "player id required", http.StatusBadRequest)
		return
	}

	if !echoStatsEnabled() {
		respondJSON(w, map[string]any{
			"success":    false,
			"configured": false,
			"player_id":  pid,
			"message":    "Echo Combat stats are not configured.",
		})
		return
	}

	stats, err := echoStatsForPlayerRef(pid, r.URL.Query().Get("period"))
	if err != nil {
		respondJSON(w, map[string]any{
			"success":    false,
			"configured": true,
			"player_id":  pid,
			"message":    err.Error(),
		})
		return
	}

	respondJSON(w, map[string]any{
		"success":    true,
		"configured": true,
		"player_id":  pid,
		"period":     periodOrDefault(r.URL.Query().Get("period")),
		"stats":      stats,
	})
}

func periodOrDefault(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "day", "week", "month":
		return strings.ToLower(strings.TrimSpace(p))
	default:
		return "all"
	}
}

// echoStatsForPlayerRef accepts our own player id (as a string) and resolves it
// to echocombat stats, falling back to a name search when the id isn't known
// upstream (echocombat keeps its own ids for older accounts).
func echoStatsForPlayerRef(ref string, period string) (*EchoPlayerStats, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("player id required")
	}

	if localID, err := strconv.ParseInt(ref, 10, 64); err == nil && localID != 0 {
		return echoCombatStatsForPlayer(localID, period)
	}

	return echoCombatPlayerStats(ref, period)
}

// GET /api/mod/rookie/echo-stats/batch?player_ids=1,2,3[&period=all]
//
// Same thing for a handful of players (draft pool), so a mod can see the whole
// group at once. Sequentially fetched because the API is rate limited.
func HandleGetEchoStatsBatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	raw := strings.TrimSpace(r.URL.Query().Get("player_ids"))
	if raw == "" {
		modJSONErr(w, http.StatusBadRequest, "player_ids is required")
		return
	}

	ids := strings.Split(raw, ",")
	if len(ids) > 30 {
		ids = ids[:30]
	}

	configured := echoStatsEnabled()
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		entry := map[string]any{"player_id": id}
		if configured {
			if stats, err := echoStatsForPlayerRef(id, r.URL.Query().Get("period")); err == nil {
				entry["stats"] = stats
				entry["success"] = true
			} else {
				entry["message"] = err.Error()
				entry["success"] = false
			}
		}
		out = append(out, entry)
	}

	respondJSON(w, map[string]any{
		"success":    configured,
		"configured": configured,
		"players":    out,
	})
}
