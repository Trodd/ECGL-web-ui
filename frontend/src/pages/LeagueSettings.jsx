import { useEffect, useState } from "react";
import axios from "axios";
import { getApiUrl } from "../config";

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
            { key: "CHALLENGE_BONUS_MULTIPLIER", label: "Challenge Bonus Multiplier", type: "number" },
            { key: "MAX_RATING", label: "Max Rating Cap", type: "number" },
            { key: "MIN_RATING", label: "Min Rating Floor", type: "number" },
            { key: "DEFAULT_PLAYER_RATING", label: "Default Player Rating", type: "number" },
            { key: "DEFAULT_TEAM_RATING", label: "Default Team Rating", type: "number" },
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
            <h2>⚙️ League Settings</h2>
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
        </div>
    );
}
