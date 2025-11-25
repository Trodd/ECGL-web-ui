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

    // Load saved league subs (NEW — INSERT THIS)
    useEffect(() => {
        if (match.league_sub_a !== null && match.league_sub_a !== undefined) {
            setSelectedSubA(String(match.league_sub_a));
        }
        if (match.league_sub_b !== null && match.league_sub_b !== undefined) {
            setSelectedSubB(String(match.league_sub_b));
        }
    }, [match.league_sub_a, match.league_sub_b]);

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
        <div
            className="p-3 border rounded bg-dark shadow-sm mx-auto text-light d-flex flex-column align-items-center"
            style={{
                width: "100%",
                maxWidth: 700,
                borderColor: "#444",
                textAlign: "center",
                backgroundColor: "#121212",
            }}
        >
            <div className="d-flex justify-content-between align-items-center mb-2">
            </div>

            <p className="text-muted mb-2">
                Status: <strong>{match.status || "Pending"}</strong>
            </p>

            {/* 🗓️ Step 1: Schedule / Edit or View Time */}
            <div className="mb-3">

                {/* 🧑 Captains get edit controls */}
                {isCaptain && (
                    <>
                        {!match.date || editing ? (
                            <div className="d-flex align-items-center gap-2 mt-2">
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
                                        className="btn btn-secondary btn-sm"
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

            {/* 🎯 Step 2: Scoring */}
            {match.date && isCaptain && (
                <div className="mt-3">
                    <h6 className="text-light mb-2">🎯 Confirm Scores</h6>

                    {/* 🎲 Coin Flip Section */}
                    <div className="mb-3">
                        <label className="form-label text-light fw-bold">🎲 Coin Flip — Your Call</label>

                        <div className="d-flex align-items-center gap-2">
                            <select
                                className="form-select bg-dark text-light"
                                value={coinFlipCall}
                                disabled={coinFlipConfirmed}   // lock after confirming
                                onChange={(e) => {
                                    setCoinFlipCall(e.target.value);
                                    setCoinFlipConfirmed(false); // reset if changed
                                }}
                                style={{ maxWidth: 200 }}
                            >
                                <option value="">Select...</option>
                                <option value="HEADS">Heads</option>
                                <option value="TAILS">Tails</option>
                            </select>

                            {/* Confirm Button */}
                            {!coinFlipConfirmed && (
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
                                        } catch (err) {
                                            console.error("Coin flip confirm error:", err);
                                            alert("Failed to confirm coin flip.");
                                        }
                                    }}
                                >
                                    Confirm Flip
                                </button>
                            )}

                            {/* Show result if confirmed */}
                            {coinFlipConfirmed && (
                                <span className="text-success fw-bold">
                                    ✔ Flip Confirmed
                                </span>
                            )}
                        </div>
                    </div>

                    {/* 🧍 League Subs */}
                    <div className="d-flex flex-wrap align-items-center gap-3 mb-3">
                        <div>
                            <label className="form-label text-info small mb-1">
                                League Sub for {team.name}
                            </label>
                            <select
                                className="form-select form-select-sm bg-dark text-light"
                                value={selectedSubA}
                                onChange={(e) => setSelectedSubA(e.target.value)}
                                disabled={myScoresConfirmed}
                                style={{ minWidth: 200 }}
                            >
                                <option value="">None</option>
                                {leagueSubs.map((s) => (
                                    <option key={s.id} value={s.id}>
                                        {s.display_name || s.username}
                                    </option>
                                ))}
                            </select>
                        </div>

                        <div>
                            <label className="form-label text-warning small mb-1">
                                League Sub for {match.opponent}
                            </label>
                            <select
                                className="form-select form-select-sm bg-dark text-light"
                                value={selectedSubB}
                                onChange={(e) => setSelectedSubB(e.target.value)}
                                disabled={myScoresConfirmed}
                                style={{ minWidth: 200 }}
                            >
                                <option value="">None</option>
                                {leagueSubs.map((s) => (
                                    <option key={s.id} value={s.id}>
                                        {s.display_name || s.username}
                                    </option>
                                ))}
                            </select>
                        </div>
                    </div>

                    <div className="row g-3">
                        {[1, 2, 3].map((n) => (
                            <div className="col-md-4" key={n}>
                                <div
                                    className="p-3 rounded shadow-sm"
                                    style={{
                                        backgroundColor: "#151515",
                                        border: "1px solid rgba(255,255,255,0.1)",
                                    }}
                                >
                                    <label className="form-label text-light mb-1">Map {n}</label>
                                    <select
                                        className="form-select form-select-sm bg-dark text-light mb-2"
                                        value={scores[`map${n}`]?.mode || ""}
                                        onChange={(e) =>
                                            updateScore(`map${n}`, "mode", e.target.value)
                                        }
                                        disabled={myScoresConfirmed}
                                    >
                                        <option value="">Gamemode...</option>
                                        <option value="Capture Point">Capture Point</option>
                                        <option value="Payload">Payload</option>
                                    </select>

                                    <div className="mb-2">
                                        <small className="text-info">Your Score:</small>
                                        <input
                                            type="number"
                                            min="0"
                                            className="form-control form-control-sm bg-dark text-light"
                                            value={scores[`map${n}`]?.our || ""}
                                            onChange={(e) =>
                                                updateScore(`map${n}`, "our", e.target.value)
                                            }
                                            disabled={myScoresConfirmed}
                                        />
                                    </div>
                                    <div>
                                        <small className="text-warning">Opponent’s Score:</small>
                                        <input
                                            type="number"
                                            min="0"
                                            className="form-control form-control-sm bg-dark text-light"
                                            value={scores[`map${n}`]?.their || ""}
                                            onChange={(e) =>
                                                updateScore(`map${n}`, "their", e.target.value)
                                            }
                                            disabled={myScoresConfirmed}
                                        />
                                    </div>
                                </div>
                            </div>
                        ))}
                    </div>

                    <div className="d-flex gap-2 mt-3 align-items-center">
                        <button
                            className="btn btn-success btn-sm"
                            onClick={handleConfirmScores}
                            disabled={bothTeamsConfirmedScores || myScoresConfirmed}
                        >
                            ✅ Confirm Scores
                        </button>

                        {myScoresConfirmed && !bothTeamsConfirmedScores && (
                            <p className="text-warning small mb-0">
                                ⏳ Waiting for opponent to confirm (editing resets confirmation)...
                            </p>
                        )}
                    </div>
                </div>
            )}

            {/* ✅ Step 3: Finalized */}
            {bothTeamsConfirmedScores && (
                <div className="mt-3 text-light small">
                    <p className="text-success mb-0 fw-semibold">
                        ✅ Both teams confirmed — match completed!
                    </p>
                    {(selectedSubA || selectedSubB) && (
                        <p className="text-muted mt-1 mb-0">
                            {selectedSubA &&
                                `League Sub (Your Team): ${leagueSubs.find((s) => String(s.id) === String(selectedSubA))
                                    ?.display_name ||
                                "Unknown"
                                }`}
                            {selectedSubA && selectedSubB && " • "}
                            {selectedSubB &&
                                `Opponent Sub: ${leagueSubs.find((s) => String(s.id) === String(selectedSubB))?.display_name ||
                                "Unknown"
                                }`}
                        </p>
                    )}
                </div>
            )}

            {msg && (
                <p
                    className={`small mt-2 mb-0 ${msg.startsWith("✅")
                        ? "text-success"
                        : msg.startsWith("⚠️")
                            ? "text-warning"
                            : "text-danger"
                        }`}
                >
                    {msg}
                </p>
            )}
        </div>
    );
}