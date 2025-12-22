import { useState, useEffect } from "react";
import axios from "axios";

export default function MatchCard({ match, team, urlBase, loadTeam, myRole }) {
    const isCaptain = myRole === "Captain" || myRole === "Co-Captain";
    const [localDate, setLocalDate] = useState(
        match.date ? new Date(match.date).toISOString().slice(0, 16) : ""
    );
    const [scores, setScores] = useState({});
    const [msg, setMsg] = useState("");
    const [editing, setEditing] = useState(false);
    const [coinFlipConfirmed, setCoinFlipConfirmed] = useState(false);
    const [coinFlipResult, setCoinFlipResult] = useState(null);
    const [coinFlipCall, setCoinFlipCall] = useState("");

    const isTeamA = match.team_a_id === team.id;
    const isTeamB = match.team_b_id === team.id;

    const bothTeamsConfirmedScores =
        match.team_a_score_confirmed && match.team_b_score_confirmed;

    const myScoresConfirmed =
        (isTeamA && match.team_a_score_confirmed) ||
        (isTeamB && match.team_b_score_confirmed);

    // 🧍 League Subs
    const [leagueSubs, setLeagueSubs] = useState([]);
    const [selectedSubA, setSelectedSubA] = useState("");
    const [selectedSubB, setSelectedSubB] = useState("");
    const pingForSub = async (matchID, teamID) => {
        console.log("PingForSub CLICKED:", { matchID, teamID }); // DEBUG

        try {
            await axios.post(`${import.meta.env.VITE_API_URL}/api/match/ping-sub`, {
                match_id: matchID,
                team_id: teamID,
            });

            alert("Sub request sent!");
        } catch (err) {
            console.error("Sub ping failed:", err);
            alert("Failed to send sub request.");
        }
    };

    // ✅ Load saved scores
    useEffect(() => {
        if (!match.maps || match.maps.length === 0) return;

        const preset = {};
        match.maps.forEach((m) => {
            const isViewingTeamA = team.id === match.team_a_id;

            preset[`map${m.map_number}`] = {
                mode: m.gamemode || "",

                // Stable mapping that never flips on reload
                our: isViewingTeamA ? m.team_a_score : m.team_b_score,
                their: isViewingTeamA ? m.team_b_score : m.team_a_score,
            };
        });

        setScores(preset);
    }, [match.maps, match.team_a_id, team.id]);

    // Load saved league subs — BUT normalize based on which team you are
    useEffect(() => {
        const subA = match.league_sub_a ? String(match.league_sub_a) : "";
        const subB = match.league_sub_b ? String(match.league_sub_b) : "";

        if (team.id === match.team_a_id) {
            // You are Team A → store normally
            setSelectedSubA(subA);
            setSelectedSubB(subB);
        } else {
            // You are Team B → flip perspective
            setSelectedSubA(subB);
            setSelectedSubB(subA);
        }
    }, [
        match.league_sub_a,
        match.league_sub_b,
        match.team_a_id,
        team.id
    ]);

    // 🔄 Load League Subs once
    useEffect(() => {
        axios
            .get(`${urlBase}/api/players?role=League Sub`, { withCredentials: true })
            .then((res) => {
                if (Array.isArray(res.data)) {
                    // 🚨 FORCE STRING IDs
                    const cleaned = res.data.map((p) => ({
                        ...p,
                        id: String(p.id),
                    }));
                    setLeagueSubs(cleaned);
                }
            })
            .catch(() => setLeagueSubs([]));
    }, []);

    // --- Schedule / Edit Match Time ---
    async function handleSchedule() {
        if (!localDate) return setMsg("⚠️ Please pick a date and time first.");
        try {
            const utc = new Date(localDate).toISOString();
            await axios.post(
                `${urlBase}/api/match/schedule`,
                { match_id: match.id, team_id: team.id, date: utc },
                { withCredentials: true }
            );
            setMsg("✅ Match scheduled successfully!");
            setEditing(false);
            await loadTeam();
        } catch {
            setMsg("❌ Failed to schedule match.");
        }
    }

    // --- Submit & Confirm Scores ---
    async function handleConfirmScores() {
        const maps = [1, 2, 3]
            .filter((n) => scores[`map${n}`])
            .map((n) => {
                const s = scores[`map${n}`];

                return {
                    map_number: n,
                    gamemode: s.mode || "",

                    // ALWAYS submit scores in TeamA/TeamB alignment.
                    team_a_score:
                        match.team_a_id === team.id
                            ? Number(s.our || 0)
                            : Number(s.their || 0),

                    team_b_score:
                        match.team_b_id === team.id
                            ? Number(s.our || 0)
                            : Number(s.their || 0),
                };
            });

        try {
            await axios.post(
                `${urlBase}/api/match/submit-score`,
                {
                    match_id: match.id,
                    team_id: team.id,
                    maps,
                    league_sub_a: selectedSubA ? String(selectedSubA) : null,
                    league_sub_b: selectedSubB ? String(selectedSubB) : null,
                },
                { withCredentials: true }
            );
            await axios.post(
                `${urlBase}/api/match/confirm-score`,
                { match_id: match.id, team_id: team.id },
                { withCredentials: true }
            );
            setMsg("✅ Scores confirmed — waiting for opponent.");
            setTimeout(() => loadTeam(), 2000);
        } catch (err) {
            console.error(err);
            setMsg("❌ Failed to confirm scores.");
        }
    }

    const updateScore = (map, field, val) =>
        setScores((p) => ({
            ...p,
            [map]: { ...p[map], [field]: val },
        }));

    return (
        <div className="match-card-root mx-auto">

            {/* ================= MATCH HEADER ================= */}
            <div className="match-card-header">
                <div className="d-flex justify-content-between align-items-center w-100">
                    <h5 className="mb-0 fw-bold">
                        {team.name} vs {match.opponent}
                    </h5>
                    <span className={`status-pill ${match.status || "Pending"}`}>
                        {match.status || "Pending"}
                    </span>
                </div>
            </div>

            {/* ================= CARD BODY ================= */}
            <div className="match-card-body">

                {/* ================= SCHEDULE ================= */}
                <div className="match-section">
                    <h6 className="section-title">🗓️ Match Time</h6>

                    {isCaptain && (
                        <>
                            {!match.date || editing ? (
                                <div className="d-flex flex-wrap align-items-center gap-2 mt-2">
                                    <input
                                        type="datetime-local"
                                        className="form-control bg-dark text-light"
                                        style={{ maxWidth: 240 }}
                                        value={localDate}
                                        onChange={(e) => setLocalDate(e.target.value)}
                                    />

                                    <button
                                        className="btn btn-primary btn-sm"
                                        onClick={handleSchedule}
                                    >
                                        {match.date ? "Save Changes" : "Schedule"}
                                    </button>

                                    {editing && (
                                        <button
                                            className="btn btn-outline-secondary btn-sm"
                                            onClick={() => setEditing(false)}
                                        >
                                            Cancel
                                        </button>
                                    )}
                                </div>
                            ) : (
                                <button
                                    className="btn btn-outline-warning btn-sm mt-2"
                                    onClick={() => setEditing(true)}
                                >
                                    ✏️ Edit Date / Time
                                </button>
                            )}
                        </>
                    )}
                </div>

                {/* ================= SCORING ================= */}
                {match.date && isCaptain && (
                    <div className="match-section">

                        <h6 className="section-title">🎯 Match Setup & Scoring</h6>

                        {/* ================= COIN FLIP ================= */}
                        <div className="sub-card">
                            <label className="fw-bold mb-1 d-block">🎲 Coin Flip</label>

                            <div className="d-flex align-items-center gap-2 flex-wrap">
                                <select
                                    className="form-select bg-dark text-light"
                                    value={coinFlipCall}
                                    disabled={coinFlipConfirmed}
                                    onChange={(e) => {
                                        setCoinFlipCall(e.target.value);
                                        setCoinFlipConfirmed(false);
                                    }}
                                    style={{ maxWidth: 200 }}
                                >
                                    <option value="">Select…</option>
                                    <option value="HEADS">Heads</option>
                                    <option value="TAILS">Tails</option>
                                </select>

                                {!coinFlipConfirmed ? (
                                    <button
                                        className="btn btn-sm btn-primary"
                                        disabled={!coinFlipCall}
                                        onClick={async () => {
                                            try {
                                                const res = await axios.post(
                                                    `${urlBase}/api/match/confirm-coinflip`,
                                                    {
                                                        match_id: match.id,
                                                        team_id: team.id,
                                                        coin_flip_call: coinFlipCall,
                                                    },
                                                    { withCredentials: true }
                                                );
                                                setCoinFlipConfirmed(true);
                                                setCoinFlipResult(res.data?.winner || null);
                                                alert("🎲 Coin flip confirmed!");
                                            } catch {
                                                alert("Failed to confirm coin flip.");
                                            }
                                        }}
                                    >
                                        Confirm Flip
                                    </button>
                                ) : (
                                    <span className="text-success fw-semibold">✔ Confirmed</span>
                                )}
                            </div>
                        </div>

                        {/* ================= SUBS ================= */}
                        <div className="sub-card">
                            <label className="fw-bold mb-2 d-block">🧍 League Subs</label>

                            <div className="d-flex flex-wrap gap-3">
                                <select
                                    className="form-select form-select-sm bg-dark text-light"
                                    value={selectedSubA}
                                    onChange={(e) => setSelectedSubA(e.target.value)}
                                    disabled={myScoresConfirmed}
                                    style={{ minWidth: 200 }}
                                >
                                    <option value="">Your Team Sub</option>
                                    {leagueSubs.map((s) => (
                                        <option key={s.id} value={s.id}>
                                            {s.display_name || s.username}
                                        </option>
                                    ))}
                                </select>

                                <select
                                    className="form-select form-select-sm bg-dark text-light"
                                    value={selectedSubB}
                                    onChange={(e) => setSelectedSubB(e.target.value)}
                                    disabled={myScoresConfirmed}
                                    style={{ minWidth: 200 }}
                                >
                                    <option value="">Opponent Sub</option>
                                    {leagueSubs.map((s) => (
                                        <option key={s.id} value={s.id}>
                                            {s.display_name || s.username}
                                        </option>
                                    ))}
                                </select>

                                <button
                                    className="btn btn-warning btn-sm"
                                    onClick={() => pingForSub(match.id, team.id)}
                                >
                                    🔔 Ping for Sub
                                </button>
                            </div>
                        </div>

                        {/* ================= MAPS ================= */}
                        <div className="row g-3 mt-2">
                            {[1, 2, 3].map((n) => (
                                <div className="col-md-4" key={n}>
                                    <div className="map-card">
                                        <h6 className="mb-2">Map {n}</h6>

                                        <select
                                            className="form-select form-select-sm bg-dark text-light mb-2"
                                            value={scores[`map${n}`]?.mode || ""}
                                            onChange={(e) =>
                                                updateScore(`map${n}`, "mode", e.target.value)
                                            }
                                            disabled={myScoresConfirmed}
                                        >
                                            <option value="">Gamemode…</option>
                                            <option value="Capture Point">Capture Point</option>
                                            <option value="Payload">Payload</option>
                                        </select>

                                        <input
                                            type="number"
                                            min="0"
                                            className="form-control form-control-sm bg-dark text-light mb-2"
                                            placeholder="Your score"
                                            value={scores[`map${n}`]?.our || ""}
                                            onChange={(e) =>
                                                updateScore(`map${n}`, "our", e.target.value)
                                            }
                                            disabled={myScoresConfirmed}
                                        />

                                        <input
                                            type="number"
                                            min="0"
                                            className="form-control form-control-sm bg-dark text-light"
                                            placeholder="Opponent score"
                                            value={scores[`map${n}`]?.their || ""}
                                            onChange={(e) =>
                                                updateScore(`map${n}`, "their", e.target.value)
                                            }
                                            disabled={myScoresConfirmed}
                                        />
                                    </div>
                                </div>
                            ))}
                        </div>

                        {/* ================= CONFIRM ================= */}
                        <div className="d-flex align-items-center gap-2 mt-3">
                            <button
                                className="btn btn-success btn-sm"
                                onClick={handleConfirmScores}
                                disabled={bothTeamsConfirmedScores || myScoresConfirmed}
                            >
                                ✅ Confirm Scores
                            </button>

                            {myScoresConfirmed && !bothTeamsConfirmedScores && (
                                <span className="text-warning small">
                                    ⏳ Waiting for opponent…
                                </span>
                            )}
                        </div>
                    </div>
                )}

                {/* ================= FINAL ================= */}
                {bothTeamsConfirmedScores && (
                    <div className="match-final">
                        ✅ Both teams confirmed — match completed!
                    </div>
                )}

                {msg && (
                    <div
                        className={`small mt-2 ${msg.startsWith("✅")
                                ? "text-success"
                                : msg.startsWith("⚠️")
                                    ? "text-warning"
                                    : "text-danger"
                            }`}
                    >
                        {msg}
                    </div>
                )}
            </div>
        </div>
    );
}