import { useEffect, useState } from "react";
import axios from "axios";
import { Link } from "react-router-dom";
import { getApiUrl } from "../config";
import { E } from "../components/CustomEmoji";
import TournamentMap from "../components/TournamentMap";

// =====================================================
// Matchups — season: weekly schedule
//            tournament formats: matchup graph (mods can add/generate)
// =====================================================
export default function Matchups() {
    const [flatMatches, setFlatMatches] = useState([]);
    const [selectedSeason, setSelectedSeason] = useState("All");
    const [selectedWeek, setSelectedWeek] = useState("All");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);

    // League format — anything other than "season" gets the tournament map.
    const [leagueFormat, setLeagueFormat] = useState("season");
    const [formatLabel, setFormatLabel] = useState("");
    const [teams, setTeams] = useState([]);
    const isSeason = leagueFormat === "season";

    // Only active teams can be added to a tournament match.
    const activeTeams = teams.filter((t) => (t.status || "Active") === "Active");

    // Mod tools (only rendered for league mods).
    const [isMod, setIsMod] = useState(false);
    const [busy, setBusy] = useState(false);
    const [notice, setNotice] = useState(null);
    const [addForm, setAddForm] = useState(null);

    async function loadMatches() {
        try {
            const res = await axios.get(`${getApiUrl()}/api/matches/public`);
            if (!res.data.success) throw new Error("Invalid response");

            const raw = res.data.matches || {};
            const flat = [];

            Object.entries(raw).forEach(([seasonKey, weeksObj]) => {
                Object.entries(weeksObj).forEach(([weekKey, list]) => {
                    list.forEach((m) => {
                        flat.push({ ...m, season: seasonKey, week: weekKey });
                    });
                });
            });

            setFlatMatches(flat);
            setError(null);
        } catch (err) {
            setError(err.message);
        } finally {
            setLoading(false);
        }
    }

    useEffect(() => {
        setLoading(true);
        loadMatches();

        axios
            .get(`${getApiUrl()}/api/settings`, { withCredentials: true })
            .then((res) => {
                if (res.data?.league_format) setLeagueFormat(res.data.league_format);
                if (res.data?.league_format_label) setFormatLabel(res.data.league_format_label);
            })
            .catch(() => { });

        axios
            .get(`${getApiUrl()}/api/teams`, { withCredentials: true })
            .then((res) => setTeams(Array.isArray(res.data) ? res.data : []))
            .catch(() => setTeams([]));

        axios
            .get(`${getApiUrl()}/api/me`, { withCredentials: true })
            .then((res) => setIsMod(Boolean(res.data?.is_mod)))
            .catch(() => setIsMod(false));
    }, []);

    // -----------------------------
    // MOD ACTIONS
    // -----------------------------
    async function handleGenerate() {
        const ok = confirm(
            "Auto-generate the tournament matchups?\n\n" +
            "Group stage — teams are split into groups of 4 and every pairing inside each " +
            "group is listed now.\n" +
            "Round robin — every team plays every other team once.\n" +
            "Elimination — the first round is created with every team included (top seeds " +
            "get byes); later rounds are added from the graph once results are in.\n\n" +
            "After the tournament starts no new teams can join."
        );
        if (!ok) return;

        setBusy(true);
        setNotice(null);
        try {
            let res;
            try {
                res = await axios.post(
                    `${getApiUrl()}/api/mod/tournament/generate`,
                    { replace: false },
                    { withCredentials: true }
                );
            } catch (err) {
                // Existing matches — ask before wiping them.
                const msg = String(err.response?.data || "");
                if (err.response?.status === 400 && /already exist/i.test(msg)) {
                    if (!confirm(`${msg}\n\nReplace the existing tournament matches?`)) {
                        setBusy(false);
                        return;
                    }
                    res = await axios.post(
                        `${getApiUrl()}/api/mod/tournament/generate`,
                        { replace: true },
                        { withCredentials: true }
                    );
                } else {
                    throw err;
                }
            }
            setNotice({ type: "success", text: res.data?.message || "Matchups generated." });
            await loadMatches();
        } catch (err) {
            setNotice({
                type: "danger",
                text: String(err.response?.data || "Failed to generate matchups"),
            });
        } finally {
            setBusy(false);
        }
    }

    function openAddMatch(preset = {}) {
        setAddForm({
            team_a: "",
            team_b: "",
            bracket: preset.bracket ?? "winners",
            round: preset.round ?? 1,
            slot: preset.slot ?? "",
        });
    }

    async function submitAddMatch() {
        if (!addForm) return;
        if (!addForm.team_a || !addForm.team_b) {
            setNotice({ type: "warning", text: "Pick both teams first." });
            return;
        }
        setBusy(true);
        setNotice(null);
        try {
            const res = await axios.post(
                `${getApiUrl()}/api/mod/tournament/match`,
                {
                    team_a: addForm.team_a,
                    team_b: addForm.team_b,
                    bracket: addForm.bracket,
                    round: Number(addForm.round) || 1,
                    slot: Number(addForm.slot) || 0,
                },
                { withCredentials: true }
            );
            setNotice({ type: "success", text: res.data?.message || "Match added." });
            setAddForm(null);
            await loadMatches();
        } catch (err) {
            setNotice({
                type: "danger",
                text: String(err.response?.data || "Failed to add match"),
            });
        } finally {
            setBusy(false);
        }
    }

    // -----------------------------
    // SEASON FILTER OPTIONS
    // -----------------------------
    const seasonOptions = [
        "All",
        ...Array.from(new Set(flatMatches.map(m => m.season))).sort((a, b) => {
            if (a === "Preseason") return 1;
            if (b === "Preseason") return -1;
            return Number(b) - Number(a);
        }),
    ];

    // -----------------------------
    // WEEK FILTER OPTIONS
    // -----------------------------
    const weekOptions = [
        "All",
        ...Array.from(
            new Set(
                flatMatches
                    .filter(m => selectedSeason === "All" || m.season === selectedSeason)
                    .map(m => m.week)
            )
        ).sort((a, b) => {
            if (a === "Finals") return -1;
            if (b === "Finals") return 1;
            return Number(b) - Number(a);
        }),
    ];

    // -----------------------------
    // APPLY FILTERS
    // -----------------------------
    const filtered = flatMatches.filter(m =>
        (selectedSeason === "All" || m.season === selectedSeason) &&
        (selectedWeek === "All" || m.week === selectedWeek)
    );

    // -----------------------------
    // GROUP FOR DISPLAY
    // -----------------------------
    const grouped = {};
    filtered.forEach(m => {
        grouped[m.season] ??= {};
        grouped[m.season][m.week] ??= [];
        grouped[m.season][m.week].push(m);
    });

    // -----------------------------
    // SORT SEASONS
    // -----------------------------
    const sortedSeasons = Object.keys(grouped).sort((a, b) => {
        if (a === "Preseason") return 1;
        if (b === "Preseason") return -1;
        return Number(b) - Number(a);
    });

    return (
        <div className="d-flex justify-content-center">
            <div style={{ width: "100%", maxWidth: 820 }}>
                <div className="card bg-dark border-secondary p-4 shadow-sm">
                    <h2 className="mb-3"><E n="calendar" /> Matchups</h2>

                    {loading && <p className="text-secondary"><E n="refresh" /> Loading matchups…</p>}
                    {error && <p className="text-danger"><E n="warning" className="emoji-warning" /> {error}</p>}

                    {/* ================= FILTER BAR ================= */}
                    {!loading && !error && (
                        <div className="d-flex flex-wrap gap-2 mb-4">
                            <select
                                className="form-select form-select-sm bg-dark text-light"
                                style={{ width: 160 }}
                                value={selectedSeason}
                                onChange={(e) => {
                                    setSelectedSeason(e.target.value);
                                    setSelectedWeek("All");
                                }}
                            >
                                {seasonOptions.map((s) => (
                                    <option key={s} value={s}>
                                        {s === "All"
                                            ? "All Seasons"
                                            : s === "Preseason"
                                                ? "Preseason"
                                                : `Season ${s}`}
                                    </option>
                                ))}
                            </select>

                            <select
                                className="form-select form-select-sm bg-dark text-light"
                                style={{ width: 140 }}
                                value={selectedWeek}
                                onChange={(e) => setSelectedWeek(e.target.value)}
                            >
                                {weekOptions.map((w) => (
                                    <option key={w} value={w}>
                                        {w === "All"
                                            ? "All Weeks"
                                            : w === "Finals"
                                                ? "Finals 🏁"
                                                : `Week ${w}`}
                                    </option>
                                ))}
                            </select>
                        </div>
                    )}

                    {/* ================= MOD TOOLS (tournament formats) ================= */}
                    {!loading && !error && !isSeason && isMod && (
                        <div className="d-flex flex-wrap gap-2 mb-3">
                            <button
                                className="btn btn-warning btn-sm"
                                onClick={handleGenerate}
                                disabled={busy}
                                title="Build the opening matchups from the current team standings"
                            >
                                {busy ? "Working…" : "⚡ Auto-generate matchups"}
                            </button>
                            <button
                                className="btn btn-outline-info btn-sm"
                                onClick={() => openAddMatch({})}
                                disabled={busy}
                            >
                                + Add match
                            </button>
                        </div>
                    )}

                    {notice && (
                        <div
                            className={`alert ${notice.type === "success"
                                    ? "ecgl-alert-success"
                                    : notice.type === "warning"
                                        ? "ecgl-alert-warning"
                                        : "alert-danger"
                                } small py-2`}
                        >
                            {notice.text}
                        </div>
                    )}

                    {!loading && !error && isSeason && filtered.length === 0 && (
                        <p className="text-secondary">No matches found.</p>
                    )}

                    {/* ================= TOURNAMENT MAP (non-season formats) ================= */}
                    {!loading && !error && !isSeason && (
                        <TournamentMap
                            matches={filtered}
                            formatLabel={formatLabel || "Tournament"}
                            format={leagueFormat}
                            teamCount={teams.length}
                            isMod={isMod}
                            onAddMatch={openAddMatch}
                        />
                    )}

                    {/* ================= MATCHUPS (season) ================= */}
                    {!loading &&
                        !error &&
                        isSeason &&
                        sortedSeasons.map((season) => {
                            const weeks = grouped[season];
                            const sortedWeeks = Object.keys(weeks).sort((a, b) => {
                                if (a === "Finals") return -1;
                                if (b === "Finals") return 1;
                                return Number(b) - Number(a);
                            });

                            return sortedWeeks.map((week) => (
                                <div key={`${season}-${week}`} className="mb-4">
                                    {/* Season / Week Header */}
                                    <h5 className="text-info border-bottom pb-1 mb-3">
                                        {season === "Preseason"
                                            ? "Preseason"
                                            : `Season ${season}`}{" "}
                                        — {week === "Finals" ? <><E n="flag" /> Finals</> : `Week ${week}`}
                                    </h5>

                                    {weeks[week].map((m) => {
                                        const winnerA = m.winner_id === m.team_a_id;
                                        const winnerB = m.winner_id === m.team_b_id;

                                        return (
                                            <Link
                                                to={`/match/${m.id}`}
                                                key={m.id}
                                                className="text-decoration-none"
                                            >
                                                <div
                                                    className="border rounded p-3 mb-2 bg-dark shadow-sm"
                                                    style={{ borderColor: "#3a3a3a" }}
                                                >
                                                    {/* Teams */}
                                                    <div className="d-flex justify-content-between align-items-center mb-1">
                                                        <div className="fw-semibold">
                                                            <span className={winnerA ? "text-success" : ""}>
                                                                {m.team_a} {winnerA && <E n="trophy" className="emoji-gold" />}
                                                            </span>{" "}
                                                            <span className="text-secondary mx-1">vs</span>
                                                            <span className={winnerB ? "text-success" : ""}>
                                                                {m.team_b} {winnerB && <E n="trophy" className="emoji-gold" />}
                                                            </span>

                                                            {m.cast_active && (
                                                                <span
                                                                    className="badge bg-danger ms-2"
                                                                    title="Live / Casted Match"
                                                                >
                                                                    LIVE
                                                                </span>
                                                            )}
                                                        </div>

                                                        {/* Date */}
                                                        <span className="text-secondary small">
                                                            {m.scheduled_date
                                                                ? new Date(
                                                                    m.scheduled_date
                                                                ).toLocaleString([], {
                                                                    dateStyle: "medium",
                                                                    timeStyle: "short",
                                                                })
                                                                : "TBD"}
                                                        </span>
                                                    </div>

                                                    {/* Meta */}
                                                    <div className="small text-secondary">
                                                        <span className="me-2">
                                                            Match ID: <b>{m.match_code}</b>
                                                        </span>
                                                        <span
                                                            className={`badge ${m.status === "Completed" || m.status === "Finished"
                                                                ? "bg-success"
                                                                : m.status === "Forfeit" || m.status === "Forfeited"
                                                                    ? "bg-danger"
                                                                    : m.status === "Scheduled"
                                                                        ? "bg-warning text-dark"
                                                                        : "bg-secondary"
                                                                }`}
                                                        >
                                                            {m.status}
                                                        </span>
                                                    </div>
                                                </div>
                                            </Link>
                                        );
                                    })}
                                </div>
                            ));
                        })}
                </div>
            </div>

            {/* ================= ADD MATCH MODAL ================= */}
            {addForm && (
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
                    onClick={() => setAddForm(null)}
                >
                    <div
                        className="card bg-dark border-secondary p-3"
                        style={{ width: "100%", maxWidth: 420 }}
                        onClick={(e) => e.stopPropagation()}
                    >
                        <h6 className="mb-3">➕ Add tournament match</h6>

                        <label className="form-label small text-secondary mb-1">Team A</label>
                        <select
                            className="form-select form-select-sm bg-dark text-light mb-2"
                            value={addForm.team_a}
                            onChange={(e) => setAddForm({ ...addForm, team_a: e.target.value })}
                        >
                            <option value="">Select team…</option>
                            {activeTeams.map((t) => (
                                <option key={t.id} value={t.name}>
                                    {t.name}
                                </option>
                            ))}
                        </select>

                        <label className="form-label small text-secondary mb-1">Team B</label>
                        <select
                            className="form-select form-select-sm bg-dark text-light mb-2"
                            value={addForm.team_b}
                            onChange={(e) => setAddForm({ ...addForm, team_b: e.target.value })}
                        >
                            <option value="">Select team…</option>
                            {activeTeams.map((t) => (
                                <option key={t.id} value={t.name}>
                                    {t.name}
                                </option>
                            ))}
                        </select>

                        <div className="d-flex gap-2 mb-2">
                            <div className="flex-fill">
                                <label className="form-label small text-secondary mb-1">Bracket</label>
                                <select
                                    className="form-select form-select-sm bg-dark text-light"
                                    value={addForm.bracket}
                                    onChange={(e) => setAddForm({ ...addForm, bracket: e.target.value })}
                                >
                                    <option value="winners">Winners</option>
                                    <option value="losers">Losers</option>
                                    <option value="grand_final">Grand Final</option>
                                    <option value="group_a">Group A</option>
                                    <option value="group_b">Group B</option>
                                    <option value="group_c">Group C</option>
                                    <option value="group_d">Group D</option>
                                    <option value="">Group / Round</option>
                                </select>
                            </div>
                            <div style={{ width: 90 }}>
                                <label className="form-label small text-secondary mb-1">Round</label>
                                <input
                                    type="number"
                                    min="1"
                                    className="form-control form-control-sm bg-dark text-light"
                                    value={addForm.round}
                                    onChange={(e) => setAddForm({ ...addForm, round: e.target.value })}
                                />
                            </div>
                            <div style={{ width: 90 }}>
                                <label className="form-label small text-secondary mb-1">Slot</label>
                                <input
                                    type="number"
                                    min="1"
                                    className="form-control form-control-sm bg-dark text-light"
                                    placeholder="auto"
                                    value={addForm.slot}
                                    onChange={(e) => setAddForm({ ...addForm, slot: e.target.value })}
                                />
                            </div>
                        </div>

                        <div className="d-flex justify-content-end gap-2 mt-2">
                            <button
                                className="btn btn-secondary btn-sm"
                                onClick={() => setAddForm(null)}
                                disabled={busy}
                            >
                                Cancel
                            </button>
                            <button
                                className="btn btn-success btn-sm"
                                onClick={submitAddMatch}
                                disabled={busy}
                            >
                                {busy ? "Adding…" : "Create match"}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
