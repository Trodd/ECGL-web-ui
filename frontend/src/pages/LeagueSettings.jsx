import { useEffect, useState, useCallback } from "react";
import axios from "axios";
import { getApiUrl } from "../config";
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

const SETTING_GROUPS = [
    {
        label: "Season & Calendar",
        keys: [
            { key: "SEASON_START", label: "Season Start", type: "date" },
            { key: "SEASON_END", label: "Season End", type: "date" },
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
            { key: "MIN_TEAM_PLAYERS", label: "Min Team Players", type: "number" },
            { key: "MAX_TEAM_PLAYERS", label: "Max Team Players", type: "number" },
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

export default function LeagueSettings() {
    const [settings, setSettings] = useState({});
    const [original, setOriginal] = useState({});
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [message, setMessage] = useState(null);

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
                setMessage({ type: "danger", text: msg });
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
        setMessage(null);

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
            setMessage({ type: "success", text: "Settings saved successfully! Restart the server for some changes to take full effect." });
        } catch (err) {
            setMessage({ type: "danger", text: err.response?.data || "Failed to save settings." });
        } finally {
            setSaving(false);
        }
    };

    const handleReset = () => {
        setSettings({ ...original });
        setMessage(null);
    };

    if (loading) return <p className="text-secondary p-4">Loading settings…</p>;

    return (
        <div className="page-content">
            <h2><E n="gear" /> League Settings</h2>
            <p className="text-secondary mb-4">
                Configure league parameters. Changes are saved to the server environment.
            </p>

            {message && (
                <div className={`alert alert-${message.type} py-2`} role="alert">
                    {message.text}
                </div>
            )}

            {SETTING_GROUPS.map((group) => (
                <div key={group.label} className="card mb-4" style={{ background: "var(--bg-card)", border: "1px solid var(--border-default)" }}>
                    <div className="card-header" style={{ background: "var(--bg-elevated)", borderBottom: "1px solid var(--border-default)" }}>
                        <h5 className="mb-0" style={{ color: "var(--text-primary)" }}>{group.label}</h5>
                    </div>
                    <div className="card-body">
                        {group.keys.map(({ key, label, type, hint }) => (
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
        title: "⚠️ Platform Requirement Notice",
        content: `> Echo Combat is available only through PCVR (SteamVR, Oculus PC, or Quest via Link/Air Link).
> This league is for PCVR players only. ❌ Quest-native players are not eligible.`,
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
        content: `### ⚠️ Platform
- Must play on PCVR using SteamVR, Oculus PC, or Link/Air Link
- ❌ Quest-native unsupported

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
    const [message, setMessage] = useState(null);

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
        setMessage(null);
        try {
            await axios.post(`${getApiUrl()}/api/mod/rules`, sections, { withCredentials: true });
            setOriginal(JSON.stringify(sections));
            setMessage({ type: "success", text: "Rules saved!" });
        } catch (err) {
            setMessage({ type: "danger", text: err.response?.data || "Failed to save rules." });
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

            {message && (
                <div className={`alert alert-${message.type} py-2`} role="alert">
                    {message.text}
                </div>
            )}

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
