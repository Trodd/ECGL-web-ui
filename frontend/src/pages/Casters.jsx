import { useEffect, useMemo, useState } from "react";
import axios from "axios";
import { Link } from "react-router-dom";
import { getApiUrl } from "../config";
import { E } from "../components/CustomEmoji";
import { leagueLabel } from "../league";

const SLOTS = [
    { key: "caster1", label: "Caster 1" },
    { key: "caster2", label: "Caster 2" },
    { key: "camera", label: "Cam Op" },
    { key: "sideline", label: "Sideline" },
];

// "In 7d" / "In 3h" / "Started 12m ago"
function countdownText(dateStr) {
    if (!dateStr) return "";
    const diff = new Date(dateStr).getTime() - Date.now();
    if (Number.isNaN(diff)) return "";

    const abs = Math.abs(diff);
    const d = Math.floor(abs / 86400000);
    const h = Math.floor((abs % 86400000) / 3600000);
    const m = Math.floor((abs % 3600000) / 60000);

    let span;
    if (d > 0) span = `${d}d`;
    else if (h > 0) span = `${h}h`;
    else if (m > 0) span = `${m}m`;
    else span = "<1m";

    return diff >= 0 ? `In ${span}` : `Started ${span} ago`;
}

// A match can only go live / call the crew once at least one caster and a cam op
// are assigned.
function slotFilled(match, slotKey) {
    return Boolean(match.slots?.find((s) => s.slot === slotKey && s.player_id));
}

function castCrewReady(match) {
    const hasCaster = slotFilled(match, "caster1") || slotFilled(match, "caster2");
    const hasCam = slotFilled(match, "camera");
    return hasCaster && hasCam;
}

function formatMatchDate(dateStr) {
    if (!dateStr) return "Time TBD";
    return new Date(dateStr).toLocaleString([], {
        weekday: "long",
        month: "long",
        day: "numeric",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
    });
}

export default function Casters() {
    const urlBase = getApiUrl();

    const [me, setMe] = useState(null);
    const [loading, setLoading] = useState(true);
    const [meLoading, setMeLoading] = useState(true);
    const [firstLoad, setFirstLoad] = useState(true);
    const [matches, setMatches] = useState([]);
    const [msg, setMsg] = useState("");
    const [busy, setBusy] = useState("");
    const [assignFor, setAssignFor] = useState(null); // { match, slot }
    const [playerQuery, setPlayerQuery] = useState("");
    const [players, setPlayers] = useState([]);
    const [streamOptions, setStreamOptions] = useState([]);
    const [matchTypeOptions, setMatchTypeOptions] = useState([]);
    const [view, setView] = useState("board"); // "board" | "archive"
    const [search, setSearch] = useState(""); // archived-match search
    // Match details modal
    const [details, setDetails] = useState(null); // { loading, data, error }

    async function openDetails(matchId) {
        setDetails({ loading: true, data: null, error: "" });
        try {
            const res = await axios.get(`${urlBase}/api/match/${matchId}`, {
                withCredentials: true,
            });
            setDetails({ loading: false, data: res.data, error: "" });
        } catch (err) {
            setDetails({
                loading: false,
                data: null,
                error: err.response?.data || "Failed to load match",
            });
        }
    }

    async function loadMatches(scope = "board", q = "") {
        setLoading(true);
        try {
            const params = new URLSearchParams();
            if (scope === "archive") {
                params.set("archived", "1");
                const term = q.trim();
                if (term) params.set("q", term);
            }
            const qs = params.toString();
            const url = `${urlBase}/api/casters/matches${qs ? `?${qs}` : ""}`;
            const res = await axios.get(url, {
                withCredentials: true,
            });
            setMatches(Array.isArray(res.data?.matches) ? res.data.matches : []);
            setStreamOptions(Array.isArray(res.data?.stream_options) ? res.data.stream_options : []);
            setMatchTypeOptions(
                Array.isArray(res.data?.match_type_options) ? res.data.match_type_options : []
            );
        } catch (err) {
            setMatches([]);
            setMsg("❌ " + (err.response?.data || "Failed to load matches"));
        } finally {
            setLoading(false);
            setFirstLoad(false);
        }
    }

    useEffect(() => {
        axios
            .get(`${urlBase}/api/me`, { withCredentials: true })
            .then((res) => setMe(res.data || null))
            .catch(() => setMe(null))
            .finally(() => setMeLoading(false));

        axios
            .get(`${urlBase}/api/players`, { withCredentials: true })
            .then((res) => setPlayers(Array.isArray(res.data) ? res.data : []))
            .catch(() => setPlayers([]));
    }, []);

    useEffect(() => {
        setMsg("");
        // Debounce typing so we don't hammer the API on every keystroke.
        const delay = search.trim() ? 250 : 0;
        const t = setTimeout(() => loadMatches(view, search), delay);
        return () => clearTimeout(t);
    }, [view, search]);

    const isCaster = Boolean(me?.is_caster);

    async function claim(match, slot) {
        setBusy(`${match.id}:${slot}`);
        setMsg("");
        try {
            const res = await axios.post(
                `${urlBase}/api/casters/assign`,
                { match_id: match.id, slot },
                { withCredentials: true }
            );
            setMsg("✅ " + (res.data?.message || "Slot claimed."));
            await loadMatches(view, search);
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to claim slot"));
        } finally {
            setBusy("");
        }
    }

    async function assign(match, slot, player) {
        setBusy(`${match.id}:${slot}`);
        setMsg("");
        try {
            const res = await axios.post(
                `${urlBase}/api/casters/assign`,
                { match_id: match.id, slot, player_id: String(player.id) },
                { withCredentials: true }
            );
            setMsg("✅ " + (res.data?.message || "Slot assigned."));
            setAssignFor(null);
            setPlayerQuery("");
            await loadMatches(view, search);
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to assign slot"));
        } finally {
            setBusy("");
        }
    }

    async function release(match, slot) {
        setBusy(`${match.id}:${slot}`);
        setMsg("");
        try {
            const res = await axios.post(
                `${urlBase}/api/casters/release`,
                { match_id: match.id, slot },
                { withCredentials: true }
            );
            setMsg("✅ " + (res.data?.message || "Slot released."));
            await loadMatches(view, search);
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to release slot"));
        } finally {
            setBusy("");
        }
    }

    // Same behaviour as the Go Live button on the match page: announces to the
    // configured go-live channel.
    async function goLive(match) {
        if (!confirm(`Announce ${match.team_a} vs ${match.team_b} as LIVE?`)) return;
        setBusy(`live:${match.id}`);
        setMsg("");
        try {
            const res = await axios.post(
                `${urlBase}/api/match/go-live`,
                { match_id: match.id },
                { withCredentials: true }
            );
            setMsg("✅ " + (res.data?.message || "Go-live announcement sent."));
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to announce go live"));
        } finally {
            setBusy("");
        }
    }

    // Pick the stream channel used by Go Live for this match.
    async function setStream(match, url) {
        setBusy(`stream:${match.id}`);
        setMsg("");
        try {
            await axios.post(
                `${urlBase}/api/casters/stream`,
                { match_id: match.id, stream_url: url },
                { withCredentials: true }
            );
            await loadMatches(view, search);
            setMsg(url ? "✅ Stream channel set." : "Stream channel cleared.");
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to set stream channel"));
        } finally {
            setBusy("");
        }
    }

    // Pick the series format (Best of N) announced for this match.
    async function setMatchType(match, value) {
        setBusy(`type:${match.id}`);
        setMsg("");
        try {
            await axios.post(
                `${urlBase}/api/casters/match-type`,
                { match_id: match.id, match_type: value },
                { withCredentials: true }
            );
            await loadMatches(view, search);
            setMsg(`✅ Match type set to ${value || "default"}.`);
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to set match type"));
        } finally {
            setBusy("");
        }
    }

    // Pings both teams in the match's private Discord channel.
    async function crewReady(match) {
        setBusy(`crew:${match.id}`);
        setMsg("");
        try {
            const res = await axios.post(
                `${urlBase}/api/casters/crew-ready`,
                { match_id: match.id },
                { withCredentials: true }
            );
            setMsg("✅ " + (res.data?.message || "Crew-ready message sent."));
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to send crew-ready message"));
        } finally {
            setBusy("");
        }
    }

    const filteredPlayers = useMemo(() => {
        const q = playerQuery.trim().toLowerCase();
        if (!q) return players.slice(0, 12);
        return players
            .filter(
                (p) =>
                    (p.display_name || "").toLowerCase().includes(q) ||
                    (p.username || "").toLowerCase().includes(q)
            )
            .slice(0, 20);
    }, [players, playerQuery]);

    if (firstLoad || meLoading) {
        return (
            <div className="page-content">
                <p className="text-secondary"><E n="refresh" /> Loading cast board…</p>
            </div>
        );
    }

    if (!isCaster) {
        return (
            <div className="page-content">
                <div className="card bg-dark border-secondary p-4" style={{ maxWidth: 560 }}>
                    <h3><E n="lock" /> Casters Only</h3>
                    <p className="text-secondary mb-0">
                        This page is for the ECGL casting crew. Ask a League Mod for the Caster role
                        if you should have access.
                    </p>
                </div>
            </div>
        );
    }

    return (
        <div className="page-content">
            <h2 className="mb-1">🎥 Caster Board <span className="badge bg-secondary ms-2">{leagueLabel()}</span></h2>
            <p className="text-secondary small mb-2">
                {view === "archive"
                    ? "Played and archived matches from this league. Cast crews can still be corrected after the fact."
                    : "Only matches where both teams confirmed their schedule appear here. Claim a slot for yourself, or assign someone else."}
            </p>

            <div className="d-flex gap-2 mb-3">
                {[
                    { key: "board", label: "Upcoming" },
                    { key: "archive", label: "Archived" },
                ].map((t) => (
                    <button
                        key={t.key}
                        type="button"
                        className="btn btn-sm px-3"
                        style={
                            view === t.key
                                ? {
                                    background: "#ff8c42",
                                    color: "#141414",
                                    border: "1px solid #ff8c42",
                                    fontWeight: 700,
                                }
                                : {
                                    background: "transparent",
                                    color: "#9aa4b2",
                                    border: "1px solid #3f3f3f",
                                }
                        }
                        onClick={() => setView(t.key)}
                    >
                        {t.label}
                    </button>
                ))}
            </div>

            {view === "archive" && (
                <div className="mb-3" style={{ maxWidth: 420 }}>
                    <div className="d-flex gap-2">
                        <input
                            type="text"
                            className="form-control form-control-sm"
                            placeholder="Search past matches — team, match code, season, week…"
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                            style={{
                                background: "#1f1f1f",
                                color: "#e6e6e6",
                                border: "1px solid #ff8c42",
                                fontSize: "0.82rem",
                            }}
                        />
                        {search && (
                            <button
                                type="button"
                                className="btn btn-outline-secondary btn-sm px-2"
                                onClick={() => setSearch("")}
                                title="Clear search"
                            >
                                ✕
                            </button>
                        )}
                    </div>
                    <div className="small text-secondary mt-1">
                        {loading
                            ? "Searching…"
                            : `${matches.length} match${matches.length === 1 ? "" : "es"} found`}
                    </div>
                </div>
            )}

            {msg && (
                <div
                    className={`alert ${msg.startsWith("✅")
                            ? "ecgl-alert-success"
                            : msg.startsWith("⚠️")
                                ? "ecgl-alert-warning"
                                : "alert-danger"
                        } small py-2`}
                >
                    {msg}
                </div>
            )}

            {matches.length === 0 && (
                <p className="text-secondary">
                    {view === "archive"
                        ? search.trim()
                            ? `No past matches found for “${search.trim()}”.`
                            : "No played or archived matches in this league yet."
                        : "No confirmed matches waiting for casters right now."}
                </p>
            )}

            <div className="d-flex flex-column gap-3">
                {[...matches]
                    .sort((a, b) => {
                        const ta = a.scheduled_date ? new Date(a.scheduled_date).getTime() : Infinity;
                        const tb = b.scheduled_date ? new Date(b.scheduled_date).getTime() : Infinity;
                        // Upcoming: soonest first. Archive: newest first.
                        return view === "archive" ? tb - ta : ta - tb;
                    })
                    .map((m) => (
                    <div
                        key={m.id}
                        className="card p-3"
                        style={{
                            maxWidth: 760,
                            background: "#141414",
                            border: "1px solid #ff8c42",
                            boxShadow: "0 0 14px rgba(255,140,66,0.22)",
                            borderRadius: 6,
                        }}
                    >
                        <div className="d-flex justify-content-between align-items-start gap-2">
                            <h5
                                className="mb-0 text-light fw-bold text-uppercase"
                                style={{ letterSpacing: 0.5 }}
                            >
                                {m.team_a} <span style={{ color: "#ff8c42" }}>VS</span> {m.team_b}
                            </h5>
                            <span
                                className="badge"
                                style={{
                                    background: "#1f1f1f",
                                    color: "#ffb277",
                                    border: "1px solid #ff8c42",
                                    fontSize: "0.85rem",
                                    fontWeight: 700,
                                    padding: "0.4em 0.65em",
                                    letterSpacing: 0.3,
                                    borderRadius: 4,
                                    flexShrink: 0,
                                }}
                                title="Match ID"
                            >
                                Match #{m.id}
                            </span>
                        </div>

                        <div className="small mt-1 mb-3" style={{ color: "#9aa4b2" }}>
                            <span style={{ color: "#c3cbd6" }}>{m.match_code}</span>
                            {" • "}
                            {formatMatchDate(m.scheduled_date)}
                            {m.match_type && (
                                <>
                                    {" • "}
                                    <span style={{ color: "#ffb277" }}>{m.match_type}</span>
                                </>
                            )}
                            {m.scheduled_date && (
                                <span style={{ color: "#4dabf7" }}> {countdownText(m.scheduled_date)}</span>
                            )}
                        </div>

                        <div className="row g-2">
                            {SLOTS.map(({ key, label }) => {
                                const info =
                                    m.slots?.find((s) => s.slot === key) || { slot: key };
                                const filled = Boolean(info.player_id);
                                const isBusy = busy === `${m.id}:${key}`;

                                return (
                                    <div className="col-12 col-sm-6" key={key}>
                                        <div
                                            className="p-2 h-100 d-flex justify-content-between align-items-center gap-2"
                                            style={{
                                                minHeight: 64,
                                                border: "1px dashed #3f3f3f",
                                                borderRadius: 4,
                                                background: filled ? "#16281f" : "transparent",
                                            }}
                                        >
                                            <div style={{ minWidth: 0 }}>
                                                <div
                                                    style={{
                                                        color: "#8b95a5",
                                                        fontSize: "0.65rem",
                                                        letterSpacing: 1,
                                                    }}
                                                >
                                                    {label.toUpperCase()}:
                                                </div>
                                                <div className="text-light small text-truncate">
                                                    {filled ? (
                                                        <>
                                                            {info.display_name || info.username || info.player_id}
                                                            {info.is_mine && (
                                                                <span className="badge bg-info text-dark ms-1">
                                                                    you
                                                                </span>
                                                            )}
                                                        </>
                                                    ) : (
                                                        <span style={{ color: "#6c757d" }}>Open</span>
                                                    )}
                                                </div>
                                            </div>

                                            <div className="d-flex gap-1 flex-shrink-0">
                                                {filled ? (
                                                    <>
                                                        <button
                                                            className="btn btn-outline-light btn-sm py-1 px-3 text-uppercase"
                                                            style={{ fontSize: "0.7rem" }}
                                                            disabled={isBusy}
                                                            onClick={() => setAssignFor({ match: m, slot: key })}
                                                        >
                                                            Change
                                                        </button>
                                                        <button
                                                            className="btn btn-outline-danger btn-sm py-1 px-3 text-uppercase"
                                                            style={{ fontSize: "0.7rem" }}
                                                            disabled={isBusy}
                                                            onClick={() => release(m, key)}
                                                        >
                                                            Release
                                                        </button>
                                                    </>
                                                ) : (
                                                    <>
                                                        <button
                                                            className="btn btn-success btn-sm py-1 px-3 text-uppercase fw-semibold"
                                                            style={{ fontSize: "0.75rem" }}
                                                            disabled={isBusy}
                                                            onClick={() => claim(m, key)}
                                                        >
                                                            {isBusy ? "…" : "Claim"}
                                                        </button>
                                                        <button
                                                            className="btn btn-outline-info btn-sm py-1 px-3 fw-semibold"
                                                            style={{ fontSize: "0.75rem" }}
                                                            disabled={isBusy}
                                                            onClick={() => setAssignFor({ match: m, slot: key })}
                                                        >
                                                            Assign
                                                        </button>
                                                    </>
                                                )}
                                            </div>
                                        </div>
                                    </div>
                                );
                            })}
                        </div>

                        <div className="d-flex flex-wrap align-items-center gap-2 mt-3">
                            <span style={{ color: "#8b95a5", fontSize: "0.68rem", letterSpacing: 1 }}>
                                STREAM:
                            </span>
                            <select
                                className="form-select form-select-sm"
                                style={{
                                    maxWidth: 330,
                                    background: "#1f1f1f",
                                    color: "#e6e6e6",
                                    border: "1px solid #3f3f3f",
                                    fontSize: "0.78rem",
                                }}
                                value={m.stream_channel || ""}
                                disabled={busy === `stream:${m.id}`}
                                onChange={(e) => setStream(m, e.target.value)}
                            >
                                <option value="">Select stream channel…</option>
                                {streamOptions.map((o) => (
                                    <option key={o} value={o}>
                                        {o.replace(/^https?:\/\/(www\.)?/, "")}
                                    </option>
                                ))}
                                {m.stream_channel && !streamOptions.includes(m.stream_channel) && (
                                    <option value={m.stream_channel}>
                                        {m.stream_channel.replace(/^https?:\/\/(www\.)?/, "")}
                                    </option>
                                )}
                            </select>

                            <span
                                className="ms-2"
                                style={{ color: "#8b95a5", fontSize: "0.68rem", letterSpacing: 1 }}
                            >
                                TYPE:
                            </span>
                            <select
                                className="form-select form-select-sm"
                                style={{
                                    maxWidth: 140,
                                    background: "#1f1f1f",
                                    color: "#ffb277",
                                    border: "1px solid #ff8c42",
                                    fontWeight: 700,
                                    fontSize: "0.78rem",
                                }}
                                value={m.match_type || ""}
                                disabled={busy === `type:${m.id}`}
                                onChange={(e) => setMatchType(m, e.target.value)}
                                title="Series format shown in the caster API"
                            >
                                {m.match_type && !matchTypeOptions.includes(m.match_type) && (
                                    <option value={m.match_type}>{m.match_type}</option>
                                )}
                                {(matchTypeOptions.length > 0
                                    ? matchTypeOptions
                                    : ["Best of 1", "Best of 3", "Best of 5", "Best of 7"]
                                ).map((o) => (
                                    <option key={o} value={o}>
                                        {o}
                                    </option>
                                ))}
                            </select>
                        </div>

                        <div className="d-flex justify-content-between align-items-end mt-3">
                            <span
                                style={{ color: "#6c757d", letterSpacing: 1, fontSize: "0.68rem" }}
                                title={m.match_code}
                            >
                                {m.season ? `SEASON ${m.season}` : "MATCH"} • {m.status.toUpperCase()}
                            </span>
                            <div className="d-flex align-items-center gap-2">
                                {view === "board" && (
                                    <>
                                        <button
                                            type="button"
                                            className="btn btn-danger btn-sm px-3 fw-semibold"
                                            style={{ borderRadius: 2, fontSize: "0.72rem" }}
                                            disabled={
                                                !castCrewReady(m) ||
                                                !m.stream_channel ||
                                                busy === `live:${m.id}`
                                            }
                                            onClick={() => goLive(m)}
                                            title={
                                                !castCrewReady(m)
                                                    ? "Assign at least one caster and a cam op first"
                                                    : !m.stream_channel
                                                        ? "Select a stream channel first"
                                                        : "Post the live announcement (same channel as the match page)"
                                            }
                                        >
                                            {busy === `live:${m.id}` ? "…" : "🔴 Go Live"}
                                        </button>
                                        <button
                                            type="button"
                                            className="btn btn-outline-info btn-sm px-3"
                                            style={{ borderRadius: 2, fontSize: "0.72rem" }}
                                            disabled={!castCrewReady(m) || busy === `crew:${m.id}`}
                                            onClick={() => crewReady(m)}
                                            title={
                                                castCrewReady(m)
                                                    ? "Ping both teams in the match's private channel"
                                                    : "Assign at least one caster and a cam op first"
                                            }
                                        >
                                            {busy === `crew:${m.id}` ? "…" : "🎥 Crew Ready"}
                                        </button>
                                    </>
                                )}
                                <button
                                    type="button"
                                    className="btn btn-outline-secondary btn-sm px-3"
                                    style={{ borderRadius: 2, fontSize: "0.72rem" }}
                                    onClick={() => openDetails(m.id)}
                                >
                                    Match Details
                                </button>
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            {/* ============ ASSIGN MODAL ============ */}
            {assignFor && (
                <div
                    style={{
                        position: "fixed",
                        inset: 0,
                        background: "rgba(0,0,0,0.72)",
                        zIndex: 10001,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        padding: 16,
                    }}
                    onClick={() => setAssignFor(null)}
                >
                    <div
                        className="card bg-dark border-secondary p-3"
                        style={{ width: "100%", maxWidth: 420 }}
                        onClick={(e) => e.stopPropagation()}
                    >
                        <h6 className="mb-2">
                            Assign {SLOTS.find((s) => s.key === assignFor.slot)?.label} —{" "}
                            {assignFor.match.match_code}
                        </h6>

                        <input
                            className="form-control bg-dark text-light mb-2"
                            placeholder="Search players…"
                            value={playerQuery}
                            onChange={(e) => setPlayerQuery(e.target.value)}
                            autoFocus
                        />

                        <div style={{ maxHeight: 260, overflowY: "auto" }}>
                            {filteredPlayers.length === 0 && (
                                <p className="text-secondary small mb-0">No players found.</p>
                            )}
                            {filteredPlayers.map((p) => (
                                <button
                                    key={p.id}
                                    className="btn btn-dark border-secondary w-100 text-start mb-1"
                                    onClick={() => assign(assignFor.match, assignFor.slot, p)}
                                >
                                    <span className="text-light">{p.display_name || p.username}</span>
                                    <span className="text-secondary small ms-2">@{p.username}</span>
                                </button>
                            ))}
                        </div>

                        <div className="d-flex justify-content-end mt-2">
                            <button
                                className="btn btn-secondary btn-sm"
                                onClick={() => setAssignFor(null)}
                            >
                                Cancel
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* ============ MATCH DETAILS MODAL ============ */}
            {details && (
                <div
                    style={{
                        position: "fixed",
                        inset: 0,
                        background: "rgba(0,0,0,0.75)",
                        zIndex: 10002,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        padding: 16,
                    }}
                    onClick={() => setDetails(null)}
                >
                    <div
                        className="card bg-dark border-secondary p-3"
                        style={{ width: "100%", maxWidth: 640, maxHeight: "85vh", overflowY: "auto" }}
                        onClick={(e) => e.stopPropagation()}
                    >
                        {details.loading && (
                            <p className="text-secondary mb-0"><E n="refresh" /> Loading match…</p>
                        )}

                        {details.error && (
                            <p className="text-danger mb-0">{details.error}</p>
                        )}

                        {details.data && (() => {
                            const mm = details.data.match || {};
                            const ta = details.data.teams?.a || {};
                            const tb = details.data.teams?.b || {};
                            const sa = details.data.standings?.a || {};
                            const sb = details.data.standings?.b || {};
                            const maps = details.data.map_scores || [];
                            const rosterA = details.data.roster?.a || [];
                            const rosterB = details.data.roster?.b || [];

                            return (
                                <>
                                    <div className="d-flex justify-content-between align-items-start">
                                        <h5 className="mb-0 text-light text-uppercase">
                                            {ta.name} <span style={{ color: "#ff8c42" }}>VS</span> {tb.name}
                                        </h5>
                                        <span className="badge bg-warning text-dark">{mm.status}</span>
                                    </div>

                                    <div className="small text-secondary mt-1">
                                        <span style={{ color: "#c3cbd6" }}>{mm.match_code}</span>
                                        {" • Match #"}{mm.id}
                                        {mm.season && ` • Season ${mm.season}`}
                                        {mm.week && ` • Week ${mm.week}`}
                                    </div>
                                    <div className="small text-secondary mb-3">
                                        {formatMatchDate(mm.scheduled_date)}
                                    </div>

                                    <div className="d-flex justify-content-center gap-3 mb-3 flex-wrap">
                                        {[
                                            { team: ta, st: sa },
                                            { team: tb, st: sb },
                                        ].map(({ team, st }, i) => (
                                            <div
                                                key={team.id ?? i}
                                                className="text-center px-3 py-2 rounded"
                                                style={{ background: "#1f1f1f", border: "1px solid #ff8c42" }}
                                            >
                                                <div
                                                    className="small text-uppercase"
                                                    style={{ color: "#ffb277", fontWeight: 700 }}
                                                >
                                                    {team.name}
                                                </div>
                                                <div className="small text-light">
                                                    {st.rank ? `#${st.rank}` : "Unranked"}
                                                    <span className="text-secondary"> · </span>
                                                    {st.rating ?? 0} SR
                                                </div>
                                                <div className="small text-secondary">
                                                    {st.wins ?? 0}W–{st.losses ?? 0}L
                                                    {st.matches != null && ` · ${st.matches} played`}
                                                </div>
                                                <div className="small text-secondary">
                                                    {st.division || "Placement"}
                                                    {st.tier ? ` ${st.tier}` : ""}
                                                </div>
                                            </div>
                                        ))}
                                    </div>

                                    <div
                                        className="d-flex justify-content-center align-items-center gap-3 mb-3"
                                        style={{ fontSize: "1.4rem", fontWeight: 700 }}
                                    >
                                        <span className="text-light">{mm.team_a_score ?? 0}</span>
                                        <span className="text-secondary" style={{ fontSize: "0.9rem" }}>—</span>
                                        <span className="text-light">{mm.team_b_score ?? 0}</span>
                                    </div>

                                    {maps.length > 0 && (
                                        <table className="table table-dark table-sm text-center mb-3">
                                            <thead>
                                                <tr>
                                                    <th>Map</th>
                                                    <th>Mode</th>
                                                    <th>{ta.name}</th>
                                                    <th>{tb.name}</th>
                                                </tr>
                                            </thead>
                                            <tbody>
                                                {maps.map((mp, i) => (
                                                    <tr key={i}>
                                                        <td>{mp.map ?? i + 1}</td>
                                                        <td>{mp.mode || "-"}</td>
                                                        <td>{mp.team_a_score ?? 0}</td>
                                                        <td>{mp.team_b_score ?? 0}</td>
                                                    </tr>
                                                ))}
                                            </tbody>
                                        </table>
                                    )}

                                    <div className="row g-3">
                                        {[
                                            { team: ta, roster: rosterA },
                                            { team: tb, roster: rosterB },
                                        ].map(({ team, roster }) => (
                                            <div className="col-12 col-sm-6" key={team.id}>
                                                <div className="text-info small mb-1">{team.name}</div>
                                                {roster.length === 0 ? (
                                                    <p className="text-secondary small mb-0">No roster.</p>
                                                ) : (
                                                    <ul className="list-unstyled mb-0">
                                                        {roster.map((p) => (
                                                            <li key={p.player_id} className="small text-light">
                                                                {p.display_name || p.username}
                                                                {p.role && (
                                                                    <span className="text-secondary"> · {p.role}</span>
                                                                )}
                                                            </li>
                                                        ))}
                                                    </ul>
                                                )}
                                            </div>
                                        ))}
                                    </div>

                                    <div className="d-flex justify-content-between mt-3">
                                        <Link
                                            to={`/match/${mm.id}`}
                                            className="btn btn-outline-info btn-sm"
                                        >
                                            Open full page →
                                        </Link>
                                        <button
                                            className="btn btn-secondary btn-sm"
                                            onClick={() => setDetails(null)}
                                        >
                                            Close
                                        </button>
                                    </div>
                                </>
                            );
                        })()}
                    </div>
                </div>
            )}
        </div>
    );
}
