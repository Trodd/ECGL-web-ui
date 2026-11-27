package main

// Echo Combat Stats (echocombat.com) client.
//
// Used to give League Mods real player stats when judging whether someone is a
// "rookie" or a seasoned player, so Rookie League teams can be balanced fairly.
//
// API (see https://echocombat.com/api/v1/openapi.json):
//   GET /api/v1/players/{user_id}?period=day|week|month|all&map=…
//   GET /api/v1/players?q=<name>&limit=&offset=
// Auth: Authorization: Bearer <key>  or  X-API-Key: <key>  or  ?key=<key>
// Limits: 30 requests/minute and 1000/hour per key.
//
// Keys are Discord user ids, which is exactly what our players.id is, so no
// name matching is needed.

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	echoStatsBase       = "https://echocombat.com"
	echoStatsCacheTTL   = 6 * time.Hour
	echoStatsMinSpacing = 2200 * time.Millisecond // ~27 req/min, under the 30/min cap
)

type echoStatsCacheEntry struct {
	body    []byte
	expires time.Time
}

var (
	echoStatsMu        sync.Mutex
	echoStatsCache     = map[string]echoStatsCacheEntry{}
	echoStatsLastCall  time.Time
	echoStatsClient    = &http.Client{Timeout: 8 * time.Second}
	echoStatsDisabled  bool
	echoStatsDisabledM sync.Mutex
)

// echoStatsAPIKey returns the configured key, or "" when the integration is off.
func echoStatsAPIKey() string {
	return strings.TrimSpace(os.Getenv("ECHOCOMBAT_API_KEY"))
}

// echoStatsEnabled reports whether the stats integration is configured.
func echoStatsEnabled() bool {
	echoStatsDisabledM.Lock()
	defer echoStatsDisabledM.Unlock()
	return echoStatsAPIKey() != "" && !echoStatsDisabled
}

// echoStatsGet performs a rate-limited, cached GET against the stats API.
func echoStatsGet(path string) ([]byte, error) {
	key := echoStatsAPIKey()
	if key == "" {
		return nil, fmt.Errorf("ECHOCOMBAT_API_KEY is not set")
	}

	echoStatsMu.Lock()
	if e, ok := echoStatsCache[path]; ok && time.Now().Before(e.expires) {
		body := e.body
		echoStatsMu.Unlock()
		return body, nil
	}
	// Space calls out so we stay well inside the per-minute limit.
	if wait := echoStatsMinSpacing - time.Since(echoStatsLastCall); wait > 0 {
		echoStatsMu.Unlock()
		time.Sleep(wait)
		echoStatsMu.Lock()
	}
	echoStatsLastCall = time.Now()
	echoStatsMu.Unlock()

	url := echoStatsBase + path

	var body []byte
	var status int
	for attempt := 0; attempt < 3; attempt++ {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-API-Key", key)
		req.Header.Set("Accept", "application/json")

		resp, err := echoStatsClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ = io.ReadAll(resp.Body)
		status = resp.StatusCode
		resp.Body.Close()

		if status == http.StatusOK {
			break
		}
		if status == http.StatusTooManyRequests {
			time.Sleep(time.Duration(attempt+1) * 3 * time.Second)
			continue
		}
		break
	}

	switch status {
	case http.StatusOK:
		echoStatsMu.Lock()
		if len(echoStatsCache) > 2000 {
			echoStatsCache = map[string]echoStatsCacheEntry{}
		}
		echoStatsCache[path] = echoStatsCacheEntry{body: body, expires: time.Now().Add(echoStatsCacheTTL)}
		echoStatsMu.Unlock()
		return body, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("no stats found")
	case http.StatusUnauthorized, http.StatusForbidden:
		// Bad/revoked key — stop hammering the API until restart.
		echoStatsDisabledM.Lock()
		echoStatsDisabled = true
		echoStatsDisabledM.Unlock()
		return nil, fmt.Errorf("stats API rejected the key (check ECHOCOMBAT_API_KEY)")
	default:
		return nil, fmt.Errorf("stats API returned %d", status)
	}
}

// EchoPlayerStats is one player's echocombat.com profile plus the fairness
// metrics the Rookie League mod tools care about.
type EchoPlayerStats struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Level     int    `json:"level"`
	FirstSeen string `json:"first_seen_at"`
	LastSeen  string `json:"last_seen_at"`
	Period    string `json:"period"`

	Games    int     `json:"games"`
	Wins     int     `json:"wins"`
	Losses   int     `json:"losses"`
	Decided  int     `json:"decided_games"`
	Hours    float64 `json:"hours_played"`
	DeathsPG float64 `json:"deaths_per_game"`
	TopSpeed float64 `json:"top_speed"`
	WinRate  float64 `json:"win_rate"`
	// WinRateKnown is false when none of their games were decided, so a 0 here
	// means "unknown", not "never won".
	WinRateKnown bool `json:"win_rate_known"`
	DaysKnown    int  `json:"days_known"`

	// Derived fairness signals (0-100).
	ExperienceScore int    `json:"experience_score"`
	SkillScore      int    `json:"skill_score"`
	Eligibility     int    `json:"eligibility"`
	Tier            string `json:"tier"`
	Verdict         string `json:"verdict"`

	// How the upstream record was matched (name search only).
	MatchedName string `json:"matched_name,omitempty"`
	MatchScore  int    `json:"match_score,omitempty"`
}

// echoStatsRaw mirrors the API payload.
type echoStatsRaw struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Level     int    `json:"level"`
	FirstSeen string `json:"first_seen_at"`
	LastSeen  string `json:"last_seen_at"`
	Period    string `json:"period"`
	Stats     struct {
		Games            int     `json:"games"`
		CountedGames     int     `json:"counted_games"`
		DecidedGames     int     `json:"decided_games"`
		Wins             int     `json:"wins"`
		Losses           int     `json:"losses"`
		HoursPlayed      float64 `json:"hours_played"`
		DeathsPerGameEst float64 `json:"deaths_per_game_est"`
		TopSpeed         float64 `json:"top_speed"`
		PctLeftEarly     float64 `json:"pct_left_early"`
		PayloadPushS     float64 `json:"payload_push_s"`
		EmoteCount       int     `json:"emote_count"`
	} `json:"stats"`
}

// echoNameKey normalises a name for comparison: lowercase, letters+digits only.
// "🙏FatherHungis🙏" and "FatherHungis" both become "fatherhungis".
func echoNameKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// echoSearchName makes a searchable query out of a display name: drops emoji
// and other non-ASCII characters, collapses whitespace.
func echoSearchName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 && r >= 32 {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// echoFindItem is one hit from the upstream player search.
type echoFindItem struct {
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	Games     int     `json:"games"`
	Hours     float64 `json:"hours_played"`
	LastSeen  string  `json:"last_seen_at"`
	FirstSeen string  `json:"first_seen_at"`
	// Score is how confidently this hit matches our player (0-100).
	Score int `json:"-"`
}

// echoQueryVariants builds the search queries to try for one name, most
// precise first.
//
// The upstream search is a case-insensitive *substring* match on the raw name,
// so:
//
//	"Alpha-Phoenix"  → hits with  q=Alpha-Phoenix  (exact)
//	"alpha_phoenix"  → misses with q=alphaphoenix  (hyphen breaks the substring)
//	                 → hits with  q=phoenix        (single token)
//
// Hence the single-token fallbacks. Everything is still verified afterwards by
// echoNameMatches, so a loose query can't attach the wrong player.
func echoQueryVariants(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	candidates := []string{
		name,
		echoSearchName(name),
		echoDeseparate(name),
	}

	// Tokens: split on non-alphanumerics and camelCase boundaries.
	spaced := make([]rune, 0, len(name)+4)
	var prev rune
	for _, r := range name {
		if r >= 'A' && r <= 'Z' && prev >= 'a' && prev <= 'z' {
			spaced = append(spaced, ' ')
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			spaced = append(spaced, r)
		} else {
			spaced = append(spaced, ' ')
		}
		prev = r
	}
	tokens := strings.Fields(string(spaced))
	// Longest first — the most distinctive part of the name.
	sort.SliceStable(tokens, func(i, j int) bool { return len(tokens[i]) > len(tokens[j]) })
	for i, t := range tokens {
		if i >= 2 {
			break
		}
		if len(t) >= 4 {
			candidates = append(candidates, t)
		}
	}

	queries := []string{}
	for _, q := range candidates {
		q = strings.TrimSpace(q)
		if q == "" || len(echoNameKey(q)) < 3 {
			continue
		}
		dup := false
		for _, existing := range queries {
			if strings.EqualFold(existing, q) {
				dup = true
				break
			}
		}
		if !dup {
			queries = append(queries, q)
		}
	}
	return queries
}

// echoSearchUpstream finds players whose name matches ours. budget limits how
// many upstream searches a single request may spend (the API allows 30/min).
func echoSearchUpstream(name string, budget *int) []echoFindItem {
	seen := map[string]bool{}
	out := []echoFindItem{}

	for _, q := range echoQueryVariants(name) {
		if budget != nil {
			if *budget <= 0 {
				break
			}
			*budget--
		}

		body, err := echoStatsGet("/api/v1/players?q=" + url.QueryEscape(q) + "&limit=10")
		if err != nil {
			continue
		}

		var res struct {
			Items []echoFindItem `json:"items"`
		}
		if json.Unmarshal(body, &res) != nil {
			continue
		}

		for _, it := range res.Items {
			uid := strings.TrimSpace(it.UserID)
			if uid == "" || seen[uid] {
				continue
			}
			score := echoMatchScore(name, it.Name)
			if score <= 0 {
				continue
			}
			seen[uid] = true
			it.Score = score
			out = append(out, it)
		}

		// An exact name match is as good as it gets — stop burning quota.
		if topScore(out) >= 100 {
			break
		}
	}

	// Best match first — a weaker query can still surface the right player.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

func topScore(items []echoFindItem) int {
	best := 0
	for _, it := range items {
		if it.Score > best {
			best = it.Score
		}
	}
	return best
}

// echoMatchScore rates how likely an upstream name is our player, 0 = no match.
//
//	100  same name after normalising (emoji/punctuation ignored)
//	 90  our name then a separator then extra text ("minileo99 (the forgotten")
//	 80  one name is a prefix of the other with a separator in between
//	 70  a distinctive token is shared (>=4 chars, e.g. "phoenix")
//	 55  one normalised name contains the other (>=4 chars)
func echoMatchScore(ourName, upstreamName string) int {
	want := echoNameKey(ourName)
	got := echoNameKey(upstreamName)
	if want == "" || got == "" {
		return 0
	}
	if want == got {
		return 100
	}

	n := strings.ToLower(strings.TrimSpace(upstreamName))
	w := strings.ToLower(strings.TrimSpace(ourName))

	// "minileo99 (the forgotten" for "minileo99"
	if w != "" && strings.HasPrefix(n, w) {
		r := strings.TrimLeft(n[len(w):], " \t")
		if r == "" {
			return 90
		}
		c := r[0]
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return 90
		}
	}
	// The reverse: our stored name carries annotations the archive doesn't have,
	// e.g. "Alpha-Phoenix (10) [51.77%]" for "Alpha-Phoenix".
	if n != "" && strings.HasPrefix(w, n) {
		r := strings.TrimLeft(w[len(n):], " \t")
		if r == "" {
			return 85
		}
		c := r[0]
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return 85
		}
	}
	// "alpha_phoenix" for "alphaphoenix"-ish names, and the reverse — but not
	// when the extra text is just a number ("psycho" vs "psycho1144"), which is
	// usually a different person.
	if rest, ok := prefixRest(got, want); ok && !allDigits(rest) {
		return 80
	}
	if rest, ok := prefixRest(want, got); ok && !allDigits(rest) {
		return 80
	}

	// Shared distinctive token.
	ours := echoTokens(ourName)
	theirs := echoTokens(upstreamName)
	for _, a := range ours {
		if len(a) < 4 {
			continue
		}
		for _, b := range theirs {
			if a == b {
				return 70
			}
		}
	}

	if len(want) >= 4 && len(got) >= 4 &&
		(strings.Contains(got, want) || strings.Contains(want, got)) {
		// "psycho" inside "psycho1144" is a different person: only accept when
		// the extra characters aren't purely a number.
		if rest, ok := prefixRest(got, want); ok && allDigits(rest) {
			return 0
		}
		if rest, ok := prefixRest(want, got); ok && allDigits(rest) {
			return 0
		}
		return 55
	}

	return 0
}

// echoTokens splits a name into lowercase alphanumeric tokens.
func echoTokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	return fields
}

// prefixRest reports whether long starts with short, returning the remainder.
func prefixRest(long, short string) (string, bool) {
	if short == "" || long == short || !strings.HasPrefix(long, short) {
		return "", false
	}
	return long[len(short):], true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// echoResolvePlayerID returns the echocombat user id for a player name, if the
// upstream search finds one that genuinely matches.
func echoResolvePlayerID(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	want := echoNameKey(name)
	if want == "" {
		return ""
	}

	key := "name:" + want
	echoStatsMu.Lock()
	if e, ok := echoStatsCache[key]; ok && time.Now().Before(e.expires) {
		out := string(e.body)
		echoStatsMu.Unlock()
		return out
	}
	echoStatsMu.Unlock()

	budget := 4
	items := echoSearchUpstream(name, &budget)
	if len(items) == 0 {
		return ""
	}

	uid := items[0].UserID
	echoStatsMu.Lock()
	if len(echoStatsCache) > 2000 {
		echoStatsCache = map[string]echoStatsCacheEntry{}
	}
	echoStatsCache[key] = echoStatsCacheEntry{
		body:    []byte(uid),
		expires: time.Now().Add(24 * time.Hour),
	}
	echoStatsMu.Unlock()

	return uid
}

// echoDeseparate removes the separators players routinely swap between Discord
// and in-game names: father_hungis → fatherhungis, ddrx09. → ddrx09.
func echoDeseparate(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '.', '_', '-', ' ', '\t', '\'', '`', '|', '~':
			continue
		}
		if r < 128 && r >= 32 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// echoStatsMissCache remembers ids we couldn't resolve, so a player page view
// doesn't re-burn API quota every time.
var echoStatsMiss = map[string]time.Time{}

func echoSeenMiss(id string) bool {
	echoStatsMu.Lock()
	defer echoStatsMu.Unlock()
	if t, ok := echoStatsMiss[id]; ok && time.Since(t) < time.Hour {
		return true
	}
	return false
}

func echoMarkMiss(id string) {
	echoStatsMu.Lock()
	defer echoStatsMu.Unlock()
	if len(echoStatsMiss) > 5000 {
		echoStatsMiss = map[string]time.Time{}
	}
	echoStatsMiss[id] = time.Now()
}

// echoCombatStatsForPlayer resolves a *local* player id to their echocombat
// stats: try the Discord id first, then fall back to searching by the names we
// store for them.
func echoCombatStatsForPlayer(playerID int64, period string) (*EchoPlayerStats, error) {
	key := fmt.Sprintf("%d", playerID)
	if echoSeenMiss(key) {
		return nil, fmt.Errorf("no stats found")
	}

	direct, err := echoCombatPlayerStats(key, period)
	if err == nil {
		return direct, nil
	}
	if !strings.Contains(err.Error(), "no stats") && !strings.Contains(err.Error(), "not configured") {
		return nil, err
	}

	var p Player
	if DB.Select("username, display_name").First(&p, playerID).Error != nil {
		echoMarkMiss(key)
		return nil, err
	}

	// Upstream searches are rate limited, so cap the total spent on one player.
	queryBudget := 6
	for _, name := range []string{p.DisplayName, p.Username} {
		if queryBudget <= 0 {
			break
		}
		for _, hit := range echoSearchUpstream(name, &queryBudget) {
			if hit.UserID == "" || hit.UserID == key {
				continue
			}
			if st, err2 := echoCombatPlayerStats(hit.UserID, period); err2 == nil {
				st.MatchedName = hit.Name
				st.MatchScore = hit.Score
				return st, nil
			}
			// The profile call failed, but the search result itself already
			// carries games / hours / last-played — good enough to judge a rookie.
			if hit.Games > 0 {
				st := &EchoPlayerStats{
					UserID:      hit.UserID,
					Name:        hit.Name,
					MatchedName: hit.Name,
					MatchScore:  hit.Score,
					FirstSeen:   hit.FirstSeen,
					LastSeen:    hit.LastSeen,
					Period:      period,
					Games:       hit.Games,
					Hours:       hit.Hours,
				}
				st.DaysKnown = daysSince(st.FirstSeen)
				scoreEchoPlayer(st)
				st.Verdict = st.Verdict + " (summary only — full profile unavailable)"
				return st, nil
			}
		}
	}

	echoMarkMiss(key)
	return nil, fmt.Errorf("no stats found")
}

// echoCombatPlayerStats returns a player's stats for a period.
func echoCombatPlayerStats(discordID string, period string) (*EchoPlayerStats, error) {
	period = strings.TrimSpace(strings.ToLower(period))
	switch period {
	case "day", "week", "month", "all":
	default:
		period = "all"
	}

	body, err := echoStatsGet(fmt.Sprintf("/api/v1/players/%s?period=%s", discordID, period))
	if err != nil {
		return nil, err
	}

	var raw echoStatsRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unexpected stats response")
	}

	out := &EchoPlayerStats{
		UserID:    raw.UserID,
		Name:      raw.Name,
		Level:     raw.Level,
		FirstSeen: raw.FirstSeen,
		LastSeen:  raw.LastSeen,
		Period:    raw.Period,
		Games:     raw.Stats.Games,
		Wins:      raw.Stats.Wins,
		Losses:    raw.Stats.Losses,
		Decided:   raw.Stats.DecidedGames,
		Hours:     raw.Stats.HoursPlayed,
		DeathsPG:  raw.Stats.DeathsPerGameEst,
		TopSpeed:  raw.Stats.TopSpeed,
	}
	out.WinRate = winRate(out.Wins, out.Losses)
	out.WinRateKnown = out.Wins+out.Losses > 0
	out.DaysKnown = daysSince(raw.FirstSeen)
	scoreEchoPlayer(out)
	return out, nil
}

func winRate(wins, losses int) float64 {
	decided := wins + losses
	if decided <= 0 {
		return 0
	}
	return math.Round(float64(wins)/float64(decided)*10000) / 10000
}

func daysSince(iso string) int {
	for _, layout := range []string{"2006-01-02T15:04:05.999", time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, iso); err == nil {
			return int(time.Since(t).Hours() / 24)
		}
	}
	return 0
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// scoreEchoPlayer fills the derived fairness metrics.
//
// The goal is a transparent "is this person really a rookie?" signal:
//   - experience: how much they've actually played (games 60%, hours 25%,
//     account age 15%) — 400 games ≈ maxed out
//   - skill: win rate (50%), level (30%), deaths per game (20%)
//
// Eligibility weights experience 70% / skill 30%, because a lot of low-skill
// playtime is exactly what a Rookie League should still shelter someone from…
// but a very high win rate is what makes a team lopsided.
func scoreEchoPlayer(p *EchoPlayerStats) {
	expGames := clamp01(float64(p.Games) / 400)
	expHours := clamp01(p.Hours / 60)
	expAge := clamp01(float64(p.DaysKnown) / 365)
	experience := 100 * (0.60*expGames + 0.25*expHours + 0.15*expAge)

	// Win rate only exists for *decided* games. Plenty of archived games have no
	// winner recorded (practice, left early, no result), so a player can have
	// hundreds of games and zero wins AND zero losses. Treat that as unknown
	// (neutral) rather than as a 0% win rate.
	skillWin := 0.5
	if p.WinRateKnown {
		skillWin = clamp01((p.WinRate - 0.35) / 0.35) // 35% win rate → 0, 70% → 1
	}
	skillLevel := clamp01(float64(p.Level) / 10)
	// Fewer deaths per game is better. 10+ → 0, 2 → 1.
	skillDeath := clamp01((10 - p.DeathsPG) / 8)
	skill := 100 * (0.50*skillWin + 0.30*skillLevel + 0.20*skillDeath)

	p.ExperienceScore = int(math.Round(experience))
	p.SkillScore = int(math.Round(skill))
	p.Eligibility = int(math.Round(0.70*experience + 0.30*skill))

	switch {
	case p.Games < 30 && p.Hours < 5:
		p.Tier = "New"
		p.Verdict = "Barely played — genuinely new."
	case p.Eligibility >= 70:
		p.Tier = "Veteran"
		p.Verdict = "Very experienced — should probably be a coach, or a mod override to play."
	case p.Eligibility >= 45:
		p.Tier = "Seasoned"
		p.Verdict = "Experienced enough to matter — check who they're teaming with."
	case p.Eligibility >= 22:
		p.Tier = "Developing"
		p.Verdict = "Some experience — fine for the Rookie League."
	default:
		p.Tier = "Rookie"
		p.Verdict = "Rookie-friendly."
	}
}
