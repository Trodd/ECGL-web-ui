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

    const bothTeamsConfirmedScores =
        match.team_a_score_confirmed && match.team_b_score_confirmed;

    const myScoresConfirmed =
        (match.team_a_id === team.id && match.team_a_score_confirmed) ||
        (match.team_b_id === team.id && match.team_b_score_confirmed);

    // ✅ Load saved scores from backend
    useEffect(() => {
        if (match.maps && match.maps.length > 0) {
            const preset = {};
            match.maps.forEach((m) => {
                preset[`map${m.map_number}`] = {
                    mode: m.gamemode || "",
                    // ✅ ensure 0 shows up instead of blank
                    a: m.team_a_score !== undefined && m.team_a_score !== null ? m.team_a_score : "",
                    b: m.team_b_score !== undefined && m.team_b_score !== null ? m.team_b_score : "",
                };
            });
            setScores((prev) => {
                // only update if changed
                if (JSON.stringify(prev) !== JSON.stringify(preset)) return preset;
                return prev;
            });
        }
    }, [match.maps]);

    // --- Schedule or edit match time ---
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

    // --- Submit & confirm scores ---
    async function handleConfirmScores() {
        const maps = [1, 2, 3]
            .filter((n) => scores[`map${n}`])
            .map((n) => ({
                map_number: n,
                gamemode: scores[`map${n}`].mode || "",
                team_a_score: Number(scores[`map${n}`].a || 0),
                team_b_score: Number(scores[`map${n}`].b || 0),
            }));

        try {
            await axios.post(
                `${urlBase}/api/match/submit-score`,
                { match_id: match.id, team_id: team.id, maps },
                { withCredentials: true }
            );
            await axios.post(
                `${urlBase}/api/match/confirm-score`,
                { match_id: match.id, team_id: team.id },
                { withCredentials: true }
            );
            setMsg("✅ Scores confirmed — waiting for opponent.");
            // optional light refresh after confirm (not blocking UI)
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
            className="p-3 border rounded bg-dark shadow-sm"
            style={{
                width: "100%",
                maxWidth: 680,
                margin: "0 auto",
                borderColor: "#444",
            }}
        >
            <div className="d-flex justify-content-between align-items-center mb-2">
                <h5 className="mb-0">
                    {match.match_code || `Match #${match.id}`} –{" "}
                    <span className="text-info">vs {match.opponent}</span>
                </h5>
            </div>

            <p className="text-muted mb-2">
                Status: <strong>{match.status || "Pending"}</strong>
            </p>

            {/* 🗓️ Step 1: Schedule / Edit */}
            {isCaptain && (
                <div>
                    <h6 className="text-light mb-2">🗓️ Schedule / Edit Match Time</h6>
                    {!match.date || editing ? (
                        <div className="d-flex align-items-center gap-2 mb-2">
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
                        <>
                            <p className="text-light small mb-2">
                                Scheduled time:{" "}
                                <strong>
                                    {new Date(match.date).toLocaleString([], {
                                        dateStyle: "medium",
                                        timeStyle: "short",
                                    })}
                                </strong>
                            </p>
                            <button
                                className="btn btn-outline-warning btn-sm"
                                onClick={() => setEditing(true)}
                            >
                                ✏️ Edit Date / Time
                            </button>
                        </>
                    )}
                </div>
            )}

            {/* 🎯 Step 2: Scoring */}
            {match.date && isCaptain && (
                <div className="mt-3">
                    <h6 className="text-light mb-2">🎯 Confirm Scores</h6>
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
                                    <label className="form-label text-light mb-1">
                                        Map {n}
                                    </label>
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
                                        <small className="text-info">{team.name} Score:</small>
                                        <input
                                            type="number"
                                            min="0"
                                            className="form-control form-control-sm bg-dark text-light"
                                            value={scores[`map${n}`]?.a || ""}
                                            onChange={(e) =>
                                                updateScore(`map${n}`, "a", e.target.value)
                                            }
                                            disabled={myScoresConfirmed}
                                        />
                                    </div>
                                    <div>
                                        <small className="text-warning">{match.opponent} Score:</small>
                                        <input
                                            type="number"
                                            min="0"
                                            className="form-control form-control-sm bg-dark text-light"
                                            value={scores[`map${n}`]?.b || ""}
                                            onChange={(e) =>
                                                updateScore(`map${n}`, "b", e.target.value)
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
                <div className="mt-3">
                    <p className="text-success mb-0 fw-semibold">
                        ✅ Both teams confirmed — match completed!
                    </p>
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
