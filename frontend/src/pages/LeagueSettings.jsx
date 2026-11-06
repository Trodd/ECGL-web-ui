import { useEffect, useState, useCallback } from "react";
import axios from "axios";
import { toast } from "../toast";
import { getApiUrl } from "../config";
import { LEAGUES, getLeague, setLeague, leagueLabel, leagueLink } from "../league";
import { E } from "../components/CustomEmoji";

// Helper: parse "start:end, start:end" into [{start, end}, ...]
function parseRanges(str) {
    if (!str || !str.trim()) return [];
    return str.split(",").map(pair => {
        const [start, end] = pair.trim().split(":").map(s => s.trim());
        return { start: start || "", end: end || "" };
    });
}

// Helper: serialize [{start, end}, ...] back to "start:end, start:end"
function serializeRanges(ranges) {
    return ranges
        .filter(r => r.start && r.end)
        .map(r => `${r.start}:${r.end}`)
        .join(", ");
}

// Date range list component
function DateRangeList({ value, onChange }) {
    const ranges = parseRanges(value);

    const update = (newRanges) => {
        onChange(serializeRanges(newRanges));
    };

    const addRange = () => {
        update([...ranges, { start: "", end: "" }]);
    };

    const removeRange = (idx) => {
        update(ranges.filter((_, i) => i !== idx));
    };

    const setField = (idx, field, val) => {
        const copy = [...ranges];
        copy[idx] = { ...copy[idx], [field]: val };
        update(copy);
    };

    return (
        <div>
            {ranges.map((r, idx) => (
                <div key={idx} className="d-flex gap-2 mb-2 align-items-center">
                    <input
                        type="date"
                        className="form-control form-control-sm"
                        style={{ background: "var(--bg-elevated)", border: "1px solid var(--border-default)", color: "var(--text-primary)" }}
                        value={r.start}
                        onChange={(e) => setField(idx, "start", e.target.value)}
                    />
                    <span style={{ color: "var(--text-tertiary)" }}>→</span>
                    <input
                        type="date"
                        className="form-control form-control-sm"
                        style={{ background: "var(--bg-elevated)", border: "1px solid var(--border-default)", color: "var(--text-primary)" }}
                        value={r.end}
                        onChange={(e) => setField(idx, "end", e.target.value)}
                    />
                    <button
                        type="button"
                        className="btn btn-sm btn-outline-danger"
                        onClick={() => removeRange(idx)}
                        title="Remove"
                    >✕</button>
                </div>
            ))}
            <button type="button" className="btn btn-sm btn-outline-secondary" onClick={addRange}>
                + Add Period
            </button>
        </div>
    );
}

const MAIN_SETTING_GROUPS = [
    {
        label: "Season & Calendar",
        keys: [
            { key: "SEASON_START", label: "Season Start", type: "date" },
            { key: "SEASON_END", label: "Season End", type: "date" },
            { key: "ROSTER_LOCK", label: "Roster Lock Date", type: "date" },
            { key: "BREAKS", label: "Break Weeks", type: "dateranges" },
            { key: "FINALS", label: "Finals Periods", type: "dateranges" },
            { key: "CURRENT_SEASON", label: "Current Season #", type: "number" },
        ],
    },
    {
        label: "Rating & Points",
        keys: [
            { key: "ELO_WIN_POINTS", label: "Win Points", type: "number" },
            { key: "ELO_LOSS_POINTS", label: "Loss Points", type: "number" },
            { key: "UNDERDOG_BONUS_PER_100", label: "Underdog Bonus per 100 Rating", type: "number" },
            { key: "UNDERDOG_LOSS_REDUCTION_PER_100", label: "Underdog Loss Reduction per 100 Rating", type: "number" },
            { key: "CHALLENGE_BONUS_MULTIPLIER", label: "Challenge Bonus Multiplier", type: "number" },
            { key: "MAX_RATING", label: "Max Rating Cap", type: "number" },
            { key: "MIN_RATING", label: "Min Rating Floor", type: "number" },
            { key: "DEFAULT_PLAYER_RATING", label: "Default Player Rating", type: "number" },
            { key: "DEFAULT_TEAM_RATING", label: "Default Team Rating", type: "number" },
            { key: "PLACEMENT_MATCHES", label: "Placement Matches Required", type: "number" },
        ],
    },
    {
        label: "Team Rules",
        keys: [
            { key: "MIN_TEAM_PLAYERS", label: "Varsity Min Team Players", type: "number", hint: "Rookie League has its own limits in the Rookie League settings." },
            { key: "MAX_TEAM_PLAYERS", label: "Varsity Max Team Players", type: "number", hint: "Rookie League has its own limits in the Rookie League settings." },
            { key: "WEEKLY_CHALLENGE_LIMIT", label: "Weekly Challenge Limit", type: "number" },
            { key: "ARENA_MODE_ENABLED", label: "Arena Mode Enabled", type: "toggle" },
        ],
    },
    {
        label: "URLs & Environment",
        keys: [
            { key: "DISCORD_INVITE_URL", label: "Discord Invite URL", type: "text" },
            { key: "FRONTEND_URL", label: "Frontend URL", type: "text" },
            { key: "TLS_HOST", label: "TLS Host", type: "text" },
            { key: "DEV_MODE", label: "Dev Mode", type: "toggle" },
        ],
    },
];

const ROOKIE_SETTING_GROUPS = [
    {
        label: "Rookie Season & Calendar",
        keys: [
            { key: "CURRENT_SEASON_ROOKIE", label: "Rookie Season #", type: "number" },
            { key: "SEASON_START_ROOKIE", label: "Rookie Season Start", type: "date" },
            { key: "SEASON_END_ROOKIE", label: "Rookie Season End", type: "date" },
            { key: "BREAKS_ROOKIE", label: "Rookie Break Weeks", type: "dateranges" },
            { key: "FINALS_ROOKIE", label: "Rookie Finals Periods", type: "dateranges" },
        ],
    },
    {
        label: "Rookie Format",
        keys: [
            {
                key: "ROOKIE_FORMAT",
                label: "League Format",
                type: "select",
                hint: "Season runs the weekly generated schedule + finals. Every other format has no auto schedule — mods add matches and enter scores manually.",
                options: [
                    { value: "season", label: "Season — weekly schedule + finals" },
                    { value: "round_robin", label: "Round robin — everyone plays everyone" },
                    { value: "single_elim", label: "Single elimination bracket" },
                    { value: "double_elim", label: "Double elimination bracket" },
                    { value: "group_stage", label: "Group stage → playoffs" },
                ],
            },
        ],
    },
    {
        label: "Rookie Veteran Rule",
        keys: [
            { key: "ROOKIE_VETERAN_RULE_ENABLED", label: "Enforce Veteran Rule", type: "toggle", hint: "Limit main-league veterans on rookie teams." },
            { key: "ROOKIE_NEW_PLAYERS_PER_VETERAN", label: "New Players per Veteran", type: "number", hint: "How many new players are required for each main-league veteran." },
            { key: "ROOKIE_VETERAN_MAX_RATING", label: "Veteran Max Rating", type: "number", hint: "Players at or below this rating are still treated as new (not veterans). 0 = ignore rating." },
            { key: "ROOKIE_VETERAN_MAX_GAMES", label: "Veteran Max Games", type: "number", hint: "Players at or below this many matches are still treated as new. 0 = ignore games." },
        ],
    },
    {
        label: "Rookie Team Rules",
        keys: [
            { key: "ROOKIE_MIN_TEAM_PLAYERS", label: "Rookie Min Team Players", type: "number", hint: "Minimum roster size for a rookie team (falls back to the global value when blank)." },
            { key: "ROOKIE_MAX_TEAM_PLAYERS", label: "Rookie Max Team Players", type: "number", hint: "Maximum roster size for a rookie team (falls back to the global value when blank)." },
        ],
    },
    {
        label: "Rookie Draft & Team Creation",
        keys: [
            {
                key: "ROOKIE_TEAM_CREATION_ENABLED",
                label: "Allow Players to Create Teams",
                type: "toggle",
                hint: "On: players can form their own rookie teams. Off: team creation is closed and players can only sign up for the draft, where a mod places them on a team.",
            },
            {
                key: "ROOKIE_COACH_DRAFT_ENABLED",
                label: "Allow Coach Draft Signups",
                type: "toggle",
                hint: "On: the Register tab also offers a separate sign-up to coach a rookie team. Coaches are placed on a team with the Coach role.",
            },
            {
                key: "ROOKIE_LEAGUE_PUBLIC",
                label: "Rookie League Publicly Visible",
                type: "toggle",
                hint: "Off: the Rookie League is hidden from everyone except League Mods and Devs — it won't appear in the league switcher and its API data falls back to Varsity.",
            },
        ],
    },
];

export default function LeagueSettings() {
    const [settings, setSettings] = useState({});
    const [original, setOriginal] = useState({});
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [clearingLogs, setClearingLogs] = useState(false);
    // 🧪 Dev test-match tool
    const [devTeams, setDevTeams] = useState([]);
    const [devBusy, setDevBusy] = useState(false);
    const [devMsg, setDevMsg] = useState(null);
    const [devForm, setDevForm] = useState({
        team_a_id: "",
        team_b_id: "",
        made_up: false,
        team_a_name: "Test Team A",
        team_b_name: "Test Team B",
        scheduled_date: "",
        week: "",
        confirm: true,
    });
    // Which league's settings this page is showing (Varsity / Rookie).
    const [viewLeague, setViewLeague] = useState(getLeague());

    // Stay in sync when the league is switched from the sidebar switcher too.
    useEffect(() => {
        function onLeague(e) {
            if (e?.detail) setViewLeague(e.detail);
        }
        window.addEventListener("ecgl-league-changed", onLeague);
        return () => window.removeEventListener("ecgl-league-changed", onLeague);
    }, []);

    // Teams for the dev test-match picker.
    useEffect(() => {
        axios
            .get(`${getApiUrl()}/api/teams`, { withCredentials: true })
            .then((res) => setDevTeams(Array.isArray(res.data) ? res.data : []))
            .catch(() => setDevTeams([]));
    }, [viewLeague]);

    useEffect(() => {
        axios
            .get(`${getApiUrl()}/api/mod/settings`, { withCredentials: true })
            .then((res) => {
                setSettings(res.data);
                setOriginal(res.data);
                setLoading(false);
            })
            .catch((err) => {
                const msg = err.response?.data || "Failed to load settings. Do you have the Dev role?";
                toast(msg, "danger");
                setLoading(false);
            });
    }, []);

    const handleChange = (key, value) => {
        setSettings((prev) => ({ ...prev, [key]: value }));
    };

    const hasChanges = () => {
        return Object.keys(settings).some((k) => settings[k] !== original[k]);
    };

    const handleSave = async () => {
        setSaving(true);
        

        // Only send changed keys
        const changed = {};
        for (const key of Object.keys(settings)) {
            if (settings[key] !== original[key]) {
                changed[key] = settings[key];
            }
        }

        try {
            await axios.post(`${getApiUrl()}/api/mod/settings`, changed, { withCredentials: true });
            setOriginal({ ...settings });

            // Season changes affect the season shown across the whole site,
            // which is cached when the page loads — reload so it refreshes.
            if ("CURRENT_SEASON" in changed || "CURRENT_SEASON_ROOKIE" in changed) {
                toast("Season updated — refreshing…", "success");
                setTimeout(() => window.location.reload(), 700);
            } else {
                toast("Settings saved successfully! Restart the server for some changes to take full effect.", "success");
            }
        } catch (err) {
            toast(err.response?.data || "Failed to save settings.", "danger");
        } finally {
            setSaving(false);
        }
    };

    const handleReset = () => {
        setSettings({ ...original });
        
    };

    // Switch which league's settings are shown (also switches the app league).
    const handleSwitchLeague = (id) => {
        if (id === viewLeague) return;
        if (hasChanges() && !confirm("You have unsaved changes. Switch league and discard them?")) {
            return;
        }
        setLeague(id);
        setViewLeague(id);
        setSettings({ ...original });
        
    };

    const handleClearLogs = async () => {
        if (!confirm("Delete ALL mod audit log entries? This cannot be undone.")) return;
        try {
            setClearingLogs(true);
            await axios.delete(`${getApiUrl()}/api/mod/audit-logs`, { withCredentials: true });
            toast("Audit logs cleared.", "success");
        } catch (err) {
            toast("Failed to clear audit logs.", "danger");
        } finally {
            setClearingLogs(false);
        }
    };

    // 🧪 Create a throwaway match for testing.
    const handleCreateTestMatch = async () => {
        if (devForm.made_up) {
            if (!devForm.team_a_name.trim() || !devForm.team_b_name.trim()) {
                setDevMsg({ type: "danger", text: "Give both made-up teams a name." });
                return;
            }
            if (devForm.team_a_name.trim().toLowerCase() === devForm.team_b_name.trim().toLowerCase()) {
                setDevMsg({ type: "danger", text: "Made-up team names must differ." });
                return;
            }
        } else {
            if (!devForm.team_a_id || !devForm.team_b_id) {
                setDevMsg({ type: "danger", text: "Pick two teams first." });
                return;
            }
            if (devForm.team_a_id === devForm.team_b_id) {
                setDevMsg({ type: "danger", text: "Pick two different teams." });
                return;
            }
        }

        setDevBusy(true);
        setDevMsg(null);
        try {
            const payload = {
                league: viewLeague,
                scheduled_date: devForm.scheduled_date || "",
                week: Number(devForm.week) || 0,
                confirm: !!devForm.confirm,
            };
            if (devForm.made_up) {
                payload.team_a_name = devForm.team_a_name.trim();
                payload.team_b_name = devForm.team_b_name.trim();
            } else {
                payload.team_a_id = Number(devForm.team_a_id);
                payload.team_b_id = Number(devForm.team_b_id);
            }

            const res = await axios.post(`${getApiUrl()}/api/dev/test-match`, payload, {
                withCredentials: true,
            });
            setDevMsg({
                type: "success",
                text: res.data?.message || "Test match created.",
                matchId: res.data?.match_id,
            });
            setDevForm({ ...devForm, team_a_id: "", team_b_id: "" });
        } catch (err) {
            setDevMsg({
                type: "danger",
                text: err.response?.data || "Failed to create test match",
            });
        } finally {
            setDevBusy(false);
        }
    };

    // ── Data tools handlers ──
    const handleArchiveSeason = async () => {
        const reset = confirm("Archive current season and reset everything?");
        try {
            await axios.post(`${getApiUrl()}/api/mod/season/archive`, { reset_after: reset }, { withCredentials: true });
            toast("Archived season data", "success");
        } catch (err) {
            toast("Failed to archive season", "danger");
        }
    };

    const handleResetLeaderboard = async () => {
        try {
            await axios.post(`${getApiUrl()}/api/mod/leaderboard/reset`, {}, { withCredentials: true });
            toast("Reset leaderboards", "success");
        } catch (err) {
            toast("Failed to reset leaderboards", "danger");
        }
    };

    const handleResetPlayerLeaderboard = async () => {
        try {
            const res = await axios.post(`${getApiUrl()}/api/mod/reset_player_leaderboard`, {}, { withCredentials: true });
            toast(res.data.message || "Player leaderboard reset!", "success");
        } catch (err) {
            toast("Failed to reset player leaderboard", "danger");
        }
    };

    const handleArchivePlayerStats = async () => {
        try {
            const seasonRes = await axios.get(`${getApiUrl()}/api/season`, { withCredentials: true });
            const season = seasonRes.data?.season ?? "Preseason";
            await axios.post(`${getApiUrl()}/api/mod/player/archive-all`, { season }, { withCredentials: true });
            toast(`Archived all player stats for Season ${season}`, "success");
        } catch (err) {
            console.error("Archive failed:", err);
            toast("Failed to archive player stats", "danger");
        }
    };

    const handleArchiveTeamStats = async () => {
        try {
            const res = await fetch(`${getApiUrl()}/api/tools/archive-team-stats`, {
                method: "POST",
                credentials: "include",
            });
            const data = await res.json();
            toast(`Archived ${data.archived} teams successfully`, "success");
        } catch (err) {
            toast("Failed to archive team stats", "danger");
        }
    };

    const handleSyncRoles = async () => {
        if (!confirm("Sync all player Discord roles?")) return;
        try {
            await axios.post(`${getApiUrl()}/api/mod/sync-roles`, {}, { withCredentials: true });
            toast("Discord roles synced successfully!", "success");
        } catch (err) {
            console.error("Sync roles failed:", err);
            toast("Failed to sync Discord roles", "danger");
        }
    };

    if (loading) return <p className="text-secondary p-4">Loading settings…</p>;

    return (
        <div className="page-content">
            <h2><E n="gear" /> League Settings <span className="badge bg-secondary ms-2">{leagueLabel(viewLeague)}</span></h2>
            <p className="text-secondary mb-3">
                Configure {leagueLabel(viewLeague)} parameters. Changes are saved to the server environment.
            </p>

            <ul className="nav nav-pills mb-4 flex-wrap gap-2">
                {LEAGUES.map((l) => (
                    <li className="nav-item" key={l.id}>
                        <button
                            type="button"
                            className={`nav-link ${viewLeague === l.id ? "active" : ""}`}
                            onClick={() => handleSwitchLeague(l.id)}
                        >
                            {l.label}
                        </button>
                    </li>
                ))}
            </ul>


            {[
                viewLeague === "rookie"
                    ? { section: "Rookie League", groups: ROOKIE_SETTING_GROUPS }
                    : { section: "Varsity League", groups: MAIN_SETTING_GROUPS },
            ].map(({ section, groups }) => (
                <div key={section} className="mb-2">
                    <h4 className="mb-3 mt-4" style={{ color: "var(--text-primary)" }}>{section}</h4>
                    {groups.map((group) => (
                        <div key={group.label} className="card mb-4" style={{ background: "var(--bg-card)", border: "1px solid var(--border-default)" }}>
                            <div className="card-header" style={{ background: "var(--bg-elevated)", borderBottom: "1px solid var(--border-default)" }}>
                                <h5 className="mb-0" style={{ color: "var(--text-primary)" }}>{group.label}</h5>
                            </div>
                            <div className="card-body">
                                {group.keys.map(({ key, label, type, hint, options }) => (
                                    <div key={key} className="row mb-3 align-items-center">
                                        <div className="col-sm-4">
                                            <label className="form-label mb-0" style={{ color: "var(--text-secondary)" }}>
                                                {label}
                                            </label>
                                            {hint && <small className="d-block" style={{ color: "var(--text-tertiary)", fontSize: "0.75rem" }}>{hint}</small>}
                                        </div>
                                        <div className="col-sm-8">
                                            {type === "toggle" ? (
                                                <div className="form-check form-switch">
                                                    <input
                                                        className="form-check-input"
                                                        type="checkbox"
                                                        checked={settings[key] === "true"}
                                                        onChange={(e) => handleChange(key, e.target.checked ? "true" : "false")}
                                                    />
                                                </div>
                                            ) : type === "dateranges" ? (
                                                <DateRangeList
                                                    value={settings[key] || ""}
                                                    onChange={(val) => handleChange(key, val)}
                                                />
                                            ) : type === "select" ? (
                                                <select
                                                    className="form-select form-select-sm"
                                                    style={{
                                                        background: "var(--bg-elevated)",
                                                        border: "1px solid var(--border-default)",
                                                        color: "var(--text-primary)",
                                                    }}
                                                    value={settings[key] || ""}
                                                    onChange={(e) => handleChange(key, e.target.value)}
                                                >
                                                    {(options || []).map((o) => (
                                                        <option key={o.value} value={o.value}>
                                                            {o.label}
                                                        </option>
                                                    ))}
                                                </select>
                                            ) : (
                                                <input
                                                    type={type === "number" ? "number" : type === "date" ? "date" : "text"}
                                                    className="form-control form-control-sm"
                                                    style={{
                                                        background: "var(--bg-elevated)",
                                                        border: "1px solid var(--border-default)",
                                                        color: "var(--text-primary)",
                                                    }}
                                                    value={settings[key] || ""}
                                                    onChange={(e) => handleChange(key, e.target.value)}
                                                />
                                            )}
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </div>
                    ))}
                </div>
            ))}

            <div className="d-flex gap-2 mb-4">
                <button
                    className="btn btn-primary"
                    onClick={handleSave}
                    disabled={saving || !hasChanges()}
                >
                    {saving ? "Saving…" : "Save Settings"}
                </button>
                <button
                    className="btn btn-outline-secondary"
                    onClick={handleReset}
                    disabled={!hasChanges()}
                >
                    Reset Changes
                </button>
            </div>

            <div className="card mb-4" style={{ background: "var(--bg-card)", border: "1px solid var(--border-default)" }}>
                <div className="card-header" style={{ background: "var(--bg-elevated)", borderBottom: "1px solid var(--border-default)" }}>
                    <h5 className="mb-0" style={{ color: "var(--text-primary)" }}>🧪 Dev — Create Test Match</h5>
                </div>
                <div className="card-body">
                    <p className="text-secondary small">
                        Creates a throwaway match in {leagueLabel(viewLeague)} coded{" "}
                        <code>&lt;season&gt;-Week&lt;n&gt;-T###</code> so you can test casting, scoring
                        and match pages. Delete it afterwards with Matches → Delete.
                    </p>

                    {devMsg && (
                        <div className={`alert alert-${devMsg.type} py-2 small`} role="alert">
                            {devMsg.text}
                            {devMsg.matchId ? (
                                <>
                                    {" "}
                                    <a href={leagueLink(`/match/${devMsg.matchId}`)} className="alert-link">
                                        Open match →
                                    </a>
                                </>
                            ) : null}
                        </div>
                    )}

                    <div className="row g-2 align-items-end">
                        <div className="col-12">
                            <div className="form-check">
                                <input
                                    className="form-check-input"
                                    type="checkbox"
                                    id="devMadeUp"
                                    checked={devForm.made_up}
                                    onChange={(e) =>
                                        setDevForm({ ...devForm, made_up: e.target.checked })
                                    }
                                />
                                <label className="form-check-label small text-secondary" htmlFor="devMadeUp">
                                    Use made-up teams (creates hidden <code>Test</code> teams — no real
                                    players or teams are touched)
                                </label>
                            </div>
                        </div>

                        {devForm.made_up ? (
                            <>
                                <div className="col-12 col-md-4">
                                    <label className="form-label small text-secondary mb-0">Team A name</label>
                                    <input
                                        className="form-control form-control-sm bg-dark text-light"
                                        placeholder="Test Team A"
                                        value={devForm.team_a_name}
                                        onChange={(e) =>
                                            setDevForm({ ...devForm, team_a_name: e.target.value })
                                        }
                                    />
                                </div>
                                <div className="col-12 col-md-4">
                                    <label className="form-label small text-secondary mb-0">Team B name</label>
                                    <input
                                        className="form-control form-control-sm bg-dark text-light"
                                        placeholder="Test Team B"
                                        value={devForm.team_b_name}
                                        onChange={(e) =>
                                            setDevForm({ ...devForm, team_b_name: e.target.value })
                                        }
                                    />
                                </div>
                            </>
                        ) : (
                            <>
                                <div className="col-12 col-md-4">
                                    <label className="form-label small text-secondary mb-0">Team A</label>
                                    <select
                                        className="form-select form-select-sm bg-dark text-light"
                                        value={devForm.team_a_id}
                                        onChange={(e) =>
                                            setDevForm({ ...devForm, team_a_id: e.target.value })
                                        }
                                    >
                                        <option value="">Select team…</option>
                                        {devTeams.map((t) => (
                                            <option key={t.id} value={t.id}>
                                                {t.name} (#{t.id})
                                            </option>
                                        ))}
                                    </select>
                                </div>

                                <div className="col-12 col-md-4">
                                    <label className="form-label small text-secondary mb-0">Team B</label>
                                    <select
                                        className="form-select form-select-sm bg-dark text-light"
                                        value={devForm.team_b_id}
                                        onChange={(e) =>
                                            setDevForm({ ...devForm, team_b_id: e.target.value })
                                        }
                                    >
                                        <option value="">Select team…</option>
                                        {devTeams.map((t) => (
                                            <option key={t.id} value={t.id}>
                                                {t.name} (#{t.id})
                                            </option>
                                        ))}
                                    </select>
                                </div>
                            </>
                        )}

                        <div className="col-12 col-md-4">
                            <label className="form-label small text-secondary mb-0">Scheduled (optional)</label>
                            <input
                                type="datetime-local"
                                className="form-control form-control-sm bg-dark text-light"
                                value={devForm.scheduled_date}
                                onChange={(e) => setDevForm({ ...devForm, scheduled_date: e.target.value })}
                            />
                        </div>

                        <div className="col-12 col-md-3">
                            <label className="form-label small text-secondary mb-0">Week (optional)</label>
                            <input
                                type="number"
                                min="1"
                                className="form-control form-control-sm bg-dark text-light"
                                placeholder="current week"
                                value={devForm.week}
                                onChange={(e) => setDevForm({ ...devForm, week: e.target.value })}
                            />
                        </div>

                        <div className="col-12 col-md-5">
                            <div className="form-check">
                                <input
                                    className="form-check-input"
                                    type="checkbox"
                                    id="devConfirm"
                                    checked={devForm.confirm}
                                    onChange={(e) => setDevForm({ ...devForm, confirm: e.target.checked })}
                                />
                                <label className="form-check-label small text-secondary" htmlFor="devConfirm">
                                    Mark both teams as schedule-confirmed (shows up on the caster board)
                                </label>
                            </div>
                        </div>

                        <div className="col-12 col-md-4">
                            <button
                                className="btn btn-warning btn-sm w-100"
                                onClick={handleCreateTestMatch}
                                disabled={devBusy}
                            >
                                {devBusy ? "Creating…" : "Create test match"}
                            </button>
                        </div>
                    </div>
                </div>
            </div>

            <div className="card mb-4" style={{ background: "var(--bg-card)", border: "1px solid var(--border-default)" }}>
                <div className="card-header" style={{ background: "var(--bg-elevated)", borderBottom: "1px solid var(--border-default)" }}>
                    <h5 className="mb-0" style={{ color: "var(--text-primary)" }}>🧹 Maintenance</h5>
                </div>
                <div className="card-body">
                    <p className="text-secondary">Clear the mod action log entries from the database.</p>
                    <button
                        className="btn btn-outline-danger btn-sm"
                        onClick={handleClearLogs}
                        disabled={clearingLogs}
                    >
                        {clearingLogs ? "Clearing…" : "Clear Mod Audit Logs"}
                    </button>
                </div>
            </div>

            <div className="card mb-4" style={{ background: "var(--bg-card)", border: "1px solid var(--border-default)" }}>
                <div className="card-header" style={{ background: "var(--bg-elevated)", borderBottom: "1px solid var(--border-default)" }}>
                    <h5 className="mb-0" style={{ color: "var(--text-primary)" }}>≡ Data Tools</h5>
                </div>
                <div className="card-body">
                    <div className="d-flex flex-wrap gap-2">
                        <button className="btn btn-outline-secondary btn-sm" onClick={handleArchiveSeason}>Archive Season</button>
                        <button className="btn btn-outline-info btn-sm" onClick={handleArchivePlayerStats}>📦 Archive Player Stats (All Players)</button>
                        <button className="btn btn-outline-primary btn-sm" onClick={handleArchiveTeamStats}>📦 Archive Team Stats</button>
                        <button className="btn btn-outline-warning btn-sm" onClick={handleResetLeaderboard}>Reset Team Leaderboard</button>
                        <button className="btn btn-outline-warning btn-sm" onClick={handleResetPlayerLeaderboard}>Reset Player Leaderboard</button>
                        <button className="btn btn-outline-primary btn-sm" onClick={handleSyncRoles}>🔄 Sync Roles</button>
                    </div>
                </div>
            </div>

            <div className="card mb-4" style={{ background: "var(--bg-card)", border: "1px solid var(--border-default)" }}>
                <div className="card-header" style={{ background: "var(--bg-elevated)", borderBottom: "1px solid var(--border-default)" }}>
                    <h5 className="mb-0" style={{ color: "var(--text-primary)" }}>▶ Clips</h5>
                </div>
                <div className="card-body">
                    <ClipsManager urlBase={getApiUrl()} setMsg={(text) => toast(text)} />
                </div>
            </div>

            <hr className="my-5" />
            <RulesEditor />
        </div>
    );
}

// =============================================================================
// RULES EDITOR
// =============================================================================

const DEFAULT_RULES = [
    {
        title: "✅ Platform Requirements",
        content: `> All supported headsets are eligible — PCVR (SteamVR / Oculus PC) and Quest (standalone/native or via Link/Air Link).`,
    },
    {
        title: "🏁 Signups",
        content: `- ✅ Sign up as a Player or League Sub
- 👥 Teams must roster 3–5 players
- 📝 Log in with Discord and register on the website`,
    },
    {
        title: "📅 Match Types",
        content: `- 🔄 Assigned Matches – matches automatically generated each week by the league.
- ⚔️ Challenge Matches – optional extra matches that teams may choose to schedule (limit: 1 per team per week).
- 📅 Each team receives 2 scheduled matchups per week
- ⚠️ Postponed matches must be played before the next week
- 🚫 Matches not completed on time are auto-forfeited (ELO loss)`,
    },
    {
        title: "👥 Team Size & Match Format",
        content: `- 🟦 Matches are played as 3v3 by default
- 🟩 4v4 is allowed if—and only if—both teams have 4 eligible players ready to play
- 🟧 If either team has only 3 players, the match is automatically 3v3
- 🤝 Minimum 3 players required to avoid forfeit
- 🔄 Both formats follow the same ruleset (best-of-3 maps)`,
    },
    {
        title: "🎮 Match Flow",
        content: `- 🗺️ Best-of-3 Maps (first to 2 wins)
- 🎲 Coin flip or agreement determines Map 1
- 🔁 Loser picks next map and side
- 🚫 No repeat maps
- 📊 Scoring is done inside My Team tab under Active Matches
- 🛡️ Opponent must confirm scores
- 📈 Leaderboard updates automatically`,
    },
    {
        title: "🛠️ Gamemodes",
        content: `### ⚙️ Payload
- Teams alternate attacking and defending
- Team that pushes the payload farther wins
- If both finish, overtime uses remaining time

### 🎯 Capture Point
- Best-of-3 rounds
- First team to win 2 rounds wins the map

### 🔫 Loadout Rules
- 🚫 Combat chassis only — no exceptions
- 📦 Loadout limits scale with match size:
  - 🔹 3v3: Max 1 weapon / tac mod / ordnance per team
  - 🔹 4v4: Max 2 weapons / tac mods / ordnances per team
- ⚠️ Experimentals allowed (majority vote), but risky:
  - Some may break loadouts or cause conflicts
  - Using exploits, leaving the map, or entering enemy spawn = penalties or forfeit`,
    },
    {
        title: "🏆 ELO Rankings",
        content: `- ELO gained or lost each match
- Tracked for both teams and players`,
    },
    {
        title: "🔄 Subs & Rosters",
        content: `- 🔍 Use Find Subs to ping eligible substitutes
- ⚖️ All subs allowed
- 1 league sub allowed per match
- 👥 Team roster minimum: 3 players
- 🌐 Players must stay under 200ms ping`,
    },
    {
        title: "🏷️ Team Rules",
        content: `- Team name must match registration
- No slurs, offensive content, or impersonation
- Violations may block match actions`,
    },
    {
        title: "⏱️ Match Timing",
        content: `- ⌛ 10+ minutes late → possible forfeit
- ⏸️ 10-minute break allowed between maps
- 🤝 3 players minimum required to start
- 🟦 4v4 allowed if both teams agree and have 4 players`,
    },
    {
        title: "🚫 Conduct",
        content: `- Respect players and staff
- No toxicity, threats, hacking, cheating, or trolling
- Evidence required for disputes
- Rule violations may result in map loss, match loss, or bans`,
    },
    {
        title: "📋 Eligibility",
        content: `### ✅ Platform
- Any supported headset: PCVR (SteamVR / Oculus PC) or Quest (standalone/native or Link/Air Link)
- Quest-native is supported

### 👥 Team Size
- Teams must roster 3–5 players
- Minimum 3 registered to compete
- Must use the same Discord account in-game

### 🎖️ League Subs
- Must sign up as a Sub
- Sub can play for any team
- Max 1 sub per match for each team
- Cannot join roster

### 🌍 Network
- Must stay under 200ms ping
- Wired strongly recommended`,
    },
    {
        title: "❌ Forfeits & Inactivity",
        content: `### 📅 Scheduling Expectations
Weekly matchups must be completed before the next cycle.
Teams must propose times promptly or risk forfeits.

### ❌ Forfeit Conditions
- Cancelling last-minute without reschedule
- No-show at agreed time
- Failure to attempt scheduling before deadline

### ⚖️ Forfeit Outcomes
- One-team forfeit → win for opponent
- Double forfeit → both lose
- Affects ELO and standings

### 🚫 Postponement Policy
Only valid if Echo VR servers are offline or widespread technical issues occur.
Mods must be contacted immediately with context.
Otherwise → double forfeit.`,
    },
];

function RulesEditor() {
    const [sections, setSections] = useState([]);
    const [original, setOriginal] = useState([]);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);

    useEffect(() => {
        axios.get(`${getApiUrl()}/api/rules`)
            .then(res => {
                const data = res.data && res.data.length > 0 ? res.data : DEFAULT_RULES;
                setSections(data.map(s => ({ title: s.title, content: s.content })));
                setOriginal(JSON.stringify(data.map(s => ({ title: s.title, content: s.content }))));
                setLoading(false);
            })
            .catch(() => {
                // Pre-fill with defaults so existing rules are editable
                setSections(DEFAULT_RULES.map(s => ({ ...s })));
                setOriginal(JSON.stringify(DEFAULT_RULES));
                setLoading(false);
            });
    }, []);

    const hasChanges = useCallback(() => {
        return JSON.stringify(sections) !== original;
    }, [sections, original]);

    const updateSection = (idx, field, value) => {
        setSections(prev => prev.map((s, i) => i === idx ? { ...s, [field]: value } : s));
    };

    const addSection = () => {
        setSections(prev => [...prev, { title: "", content: "" }]);
    };

    const removeSection = (idx) => {
        setSections(prev => prev.filter((_, i) => i !== idx));
    };

    const moveSection = (idx, dir) => {
        const newIdx = idx + dir;
        if (newIdx < 0 || newIdx >= sections.length) return;
        setSections(prev => {
            const arr = [...prev];
            [arr[idx], arr[newIdx]] = [arr[newIdx], arr[idx]];
            return arr;
        });
    };

    const handleSave = async () => {
        setSaving(true);
        
        try {
            await axios.post(`${getApiUrl()}/api/mod/rules`, sections, { withCredentials: true });
            setOriginal(JSON.stringify(sections));
            toast("Rules saved!", "success");
        } catch (err) {
            toast(err.response?.data || "Failed to save rules.", "danger");
        } finally {
            setSaving(false);
        }
    };

    if (loading) return <p className="text-secondary">Loading rules editor…</p>;

    return (
        <div>
            <h3 className="mb-2">📜 Rules Editor</h3>
            <p className="text-secondary mb-3" style={{ fontSize: "0.85rem" }}>
                Each section has a title (include emoji) and content. Content format:<br />
                <code>- item</code> = bullet list &nbsp;|&nbsp;
                <code>{">"} text</code> = blockquote &nbsp;|&nbsp;
                <code>### Heading</code> = sub-heading &nbsp;|&nbsp;
                Plain text = paragraph<br />
                Indent sub-items with <code>&nbsp;&nbsp;- sub item</code>
            </p>


            {sections.map((section, idx) => (
                <div
                    key={idx}
                    className="card mb-3"
                    style={{ background: "var(--bg-card)", border: "1px solid var(--border-default)" }}
                >
                    <div
                        className="card-header d-flex align-items-center gap-2"
                        style={{ background: "var(--bg-elevated)", borderBottom: "1px solid var(--border-default)" }}
                    >
                        <span style={{ color: "var(--text-tertiary)", fontSize: "0.8rem" }}>#{idx + 1}</span>
                        <input
                            type="text"
                            className="form-control form-control-sm flex-grow-1"
                            style={{ background: "var(--bg-elevated)", border: "1px solid var(--border-default)", color: "var(--text-primary)" }}
                            placeholder="Section title (e.g. 🏁 Signups)"
                            value={section.title}
                            onChange={(e) => updateSection(idx, "title", e.target.value)}
                        />
                        <button
                            className="btn btn-sm btn-outline-secondary"
                            onClick={() => moveSection(idx, -1)}
                            disabled={idx === 0}
                            title="Move up"
                        >↑</button>
                        <button
                            className="btn btn-sm btn-outline-secondary"
                            onClick={() => moveSection(idx, 1)}
                            disabled={idx === sections.length - 1}
                            title="Move down"
                        >↓</button>
                        <button
                            className="btn btn-sm btn-outline-danger"
                            onClick={() => removeSection(idx)}
                            title="Remove section"
                        >✕</button>
                    </div>
                    <div className="card-body p-2">
                        <textarea
                            className="form-control form-control-sm"
                            style={{
                                background: "var(--bg-elevated)",
                                border: "1px solid var(--border-default)",
                                color: "var(--text-primary)",
                                fontFamily: "monospace",
                                fontSize: "0.8rem",
                                minHeight: "120px",
                                resize: "vertical",
                            }}
                            placeholder={"- ✅ Bullet item\n- 📝 Another item\n  - Sub-item\n### Sub-heading\n> Blockquote text\nPlain paragraph"}
                            value={section.content}
                            onChange={(e) => updateSection(idx, "content", e.target.value)}
                        />
                    </div>
                </div>
            ))}

            <div className="d-flex gap-2 mb-4">
                <button className="btn btn-outline-secondary" onClick={addSection}>
                    + Add Section
                </button>
                <button
                    className="btn btn-primary"
                    onClick={handleSave}
                    disabled={saving || !hasChanges()}
                >
                    {saving ? "Saving…" : "Save Rules"}
                </button>
            </div>
        </div>
    );
}

// =============================================================================
// CLIPS MANAGER
// =============================================================================

function ClipsManager({ urlBase, setMsg }) {
    const [clips, setClips] = useState([]);
    const [newTitle, setNewTitle] = useState("");
    const [newUrl, setNewUrl] = useState("");
    const [playlistUrl, setPlaylistUrl] = useState("");
    const [syncing, setSyncing] = useState(false);

    function loadClips() {
        axios
            .get(`${urlBase}/api/clips`, { withCredentials: true })
            .then((res) => setClips(Array.isArray(res.data) ? res.data : []))
            .catch(() => setClips([]));
    }

    useEffect(() => { loadClips(); }, []);

    async function addClip() {
        if (!newUrl.trim()) {
            setMsg("⚠️ URL is required");
            return;
        }
        try {
            await axios.post(
                `${urlBase}/api/mod/clips`,
                { title: newTitle.trim(), url: newUrl.trim() },
                { withCredentials: true }
            );
            setMsg("✅ Clip added");
            setNewTitle("");
            setNewUrl("");
            loadClips();
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to add clip"));
        }
    }

    async function deleteClip(id) {
        try {
            await axios.post(
                `${urlBase}/api/mod/clips/delete`,
                { id },
                { withCredentials: true }
            );
            setMsg("✅ Clip removed");
            loadClips();
        } catch (err) {
            setMsg("❌ Failed to delete clip");
        }
    }

    function extractPlaylistId(input) {
        if (!input) return "";
        try {
            const u = new URL(input);
            const listParam = u.searchParams.get("list");
            if (listParam) return listParam;
        } catch { /* not a URL */ }
        return input.trim();
    }

    async function syncPlaylist() {
        const plId = extractPlaylistId(playlistUrl);
        if (!plId) {
            setMsg("⚠️ Enter a YouTube playlist URL or ID");
            return;
        }
        setSyncing(true);
        try {
            const res = await axios.post(
                `${urlBase}/api/mod/clips/sync-playlist`,
                { playlist_id: plId },
                { withCredentials: true }
            );
            const d = res.data;
            setMsg(`✅ Playlist synced — ${d.added} new clips added (${d.already_existed} already existed, ${d.total_in_playlist} total in playlist)`);
            loadClips();
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to sync playlist"));
        } finally {
            setSyncing(false);
        }
    }

    return (
        <div>
            <p className="text-secondary small">
                Add YouTube or Twitch clip URLs to showcase on the home page, or sync from a YouTube playlist.
            </p>

            {/* Playlist Sync */}
            <div className="card bg-secondary bg-opacity-10 border-secondary mb-4 p-3">
                <h6 className="mb-2">📋 Auto-Sync from YouTube Playlist</h6>
                <p className="text-secondary small mb-2">
                    Paste a YouTube playlist URL to automatically import all videos as clips. No API key needed — works with any public playlist.
                </p>
                <div className="row g-2">
                    <div className="col-md-8">
                        <input
                            className="form-control form-control-sm bg-dark text-light border-secondary"
                            placeholder="https://www.youtube.com/playlist?list=PLxxxxxxx or playlist ID"
                            value={playlistUrl}
                            onChange={(e) => setPlaylistUrl(e.target.value)}
                        />
                    </div>
                    <div className="col-md-4">
                        <button
                            className="btn btn-sm btn-primary w-100"
                            onClick={syncPlaylist}
                            disabled={syncing}
                        >
                            {syncing ? "⏳ Syncing..." : "🔄 Sync Playlist"}
                        </button>
                    </div>
                </div>
            </div>

            {/* Manual Add */}
            <h6>➕ Add Individual Clip</h6>
            <div className="row g-2 mb-3">
                <div className="col-md-4">
                    <input
                        className="form-control form-control-sm bg-dark text-light border-secondary"
                        placeholder="Title (optional)"
                        value={newTitle}
                        onChange={(e) => setNewTitle(e.target.value)}
                    />
                </div>
                <div className="col-md-5">
                    <input
                        className="form-control form-control-sm bg-dark text-light border-secondary"
                        placeholder="YouTube / Twitch clip URL"
                        value={newUrl}
                        onChange={(e) => setNewUrl(e.target.value)}
                    />
                </div>
                <div className="col-md-3">
                    <button className="btn btn-sm btn-success w-100" onClick={addClip}>
                        + Add Clip
                    </button>
                </div>
            </div>

            {/* Clip List */}
            {clips.length === 0 ? (
                <p className="text-secondary">No clips yet.</p>
            ) : (
                <table className="table table-dark table-striped table-sm align-middle">
                    <thead>
                        <tr>
                            <th>Title</th>
                            <th>URL</th>
                            <th>Source</th>
                            <th>Added</th>
                            <th></th>
                        </tr>
                    </thead>
                    <tbody>
                        {clips.map((c) => (
                            <tr key={c.id}>
                                <td>{c.title || <em className="text-secondary">untitled</em>}</td>
                                <td>
                                    <a href={c.url} target="_blank" rel="noreferrer" className="text-info small" style={{ wordBreak: "break-all" }}>
                                        {c.url.length > 50 ? c.url.slice(0, 50) + "…" : c.url}
                                    </a>
                                </td>
                                <td>
                                    <span className={`badge ${c.source === "playlist" ? "bg-info" : "bg-secondary"}`}>
                                        {c.source || "manual"}
                                    </span>
                                </td>
                                <td className="small text-secondary">
                                    {c.created_at ? new Date(c.created_at).toLocaleDateString() : ""}
                                </td>
                                <td>
                                    <button
                                        className="btn btn-sm btn-outline-danger"
                                        onClick={() => deleteClip(c.id)}
                                    >
                                        🗑️
                                    </button>
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            )}
        </div>
    );
}
