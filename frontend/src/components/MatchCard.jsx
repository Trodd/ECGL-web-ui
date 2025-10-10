import { useState } from "react";
import axios from "axios";

export default function MatchCard({ match, team, urlBase, loadTeam, myRole }) {
    const isCaptain = myRole === "Captain" || myRole === "Co-Captain";
    const [editing, setEditing] = useState(false);
    const [localDate, setLocalDate] = useState(
        match.date ? new Date(match.date).toISOString().slice(0, 16) : ""
    );
    const [scores, setScores] = useState({});
    const [msg, setMsg] = useState("");

    const bothTeamsConfirmedSchedule =
        match.team_a_schedule_confirmed && match.team_b_schedule_confirmed;
    const bothTeamsConfirmedScores =
        match.team_a_score_confirmed && match.team_b_score_confirmed;

    const myScheduleConfirmed =
        (match.team_a_id === team.id && match.team_a_schedule_confirmed) ||
        (match.team_b_id === team.id && match.team_b_schedule_confirmed);

    const myScoresConfirmed =
        (match.team_a_id === team.id && match.team_a_score_confirmed) ||
        (match.team_b_id === team.id && match.team_b_score_confirmed);

    async function handleSchedule() {
        if (!localDate) {
            setMsg("⚠️ Please pick a date and time first.");
            return;
        }
        try {
            const utc = new Date(localDate).toISOString();
            await axios.post(
                `${urlBase}/api/match/schedule`,
                { match_id: match.id, team_id: team.id, date: utc },
                { withCredentials: true }
            );
            setMsg("✅ Match schedule proposed! Waiting for confirmation.");
            await loadTeam();
        } catch {
            setMsg("❌ Failed to schedule match.");
        }
    }

    async function handleConfirmSchedule() {
        try {
            await axios.post(
                `${urlBase}/api/match/confirm-schedule`,
                { match_id: match.id, team_id: team.id },
                { withCredentials: true }
            );
            setMsg("✅ You confirmed the match time!");
            await loadTeam();
        } catch {
            setMsg("❌ Failed to confirm schedule.");
        }
    }

    async function handleSubmitScores() {
        const maps = [1, 2, 3]
            .filter((n) => scores[`map${n}`])
            .map((n) => ({
                map_number: n,
                gamemode: scores[`map${n}`].mode,
                team_a_score: Number(scores[`map${n}`].a || 0),
                team_b_score: Number(scores[`map${n}`].b || 0),
            }));

        const modeCount = maps.reduce((a, m) => {
            a[m.gamemode] = (a[m.gamemode] || 0) + 1;
            return a;
        }, {});
        if (Object.values(modeCount).some((c) => c > 2)) {
            setMsg("⚠️ Each gamemode can only be used twice.");
            return;
        }

        try {
            await axios.post(
                `${urlBase}/api/match/submit-score`,
                { match_id: match.id, team_id: team.id, maps },
                { withCredentials: true }
            );
            setMsg("✅ Scores submitted! Waiting for opponent confirmation.");
            await loadTeam();
        } catch {
            setMsg("❌ Failed to submit scores.");
        }
    }

    async function handleConfirmScores() {
        try {
            await axios.post(
                `${urlBase}/api/match/confirm-score`,
                { match_id: match.id, team_id: team.id },
                { withCredentials: true }
            );
            setMsg("✅ You confirmed the final scores!");
            await loadTeam();
        } catch {
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
            style={{ width: "100%", maxWidth: 680, margin: "0 auto", borderColor: "#444" }}
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

            {/* STEP 1: Scheduling (only until both confirmed) */}
            {!bothTeamsConfirmedSchedule && isCaptain && (
                <div>
                    <h6 className="text-light mb-2">🗓️ Schedule Match</h6>

                    {(!match.date || editing) ? (
                        <div className="d-flex align-items-center gap-2 mb-2">
                            <input
                                type="datetime-local"
                                className="form-control bg-dark text-light"
                                style={{ maxWidth: 240 }}
                                value={localDate}
                                onChange={(e) => setLocalDate(e.target.value)}
                            />
                            <button className="btn btn-primary btn-sm" onClick={handleSchedule}>
                                {editing ? "Update" : "Schedule"}
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
                                Proposed time:{" "}
                                <strong>
                                    {new Date(match.date).toLocaleString([], {
                                        dateStyle: "medium",
                                        timeStyle: "short",
                                    })}
                                </strong>
                            </p>

                            {!myScheduleConfirmed ? (
                                <button
                                    className="btn btn-outline-success btn-sm"
                                    onClick={handleConfirmSchedule}
                                >
                                    ✅ Confirm Match Time
                                </button>
                            ) : (
                                <p className="text-warning small mb-0">
                                    ⏳ Waiting for opponent to confirm...
                                </p>
                            )}
                        </>
                    )}
                </div>
            )}

            {/* STEP 2: Scoring (only after both teams confirm schedule) */}
            {bothTeamsConfirmedSchedule && !bothTeamsConfirmedScores && isCaptain && (
                <div className="mt-3">
                    <h6 className="text-light mb-2">🎯 Submit Scores</h6>
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
                                        />
                                    </div>
                                </div>
                            </div>
                        ))}
                    </div>

                    <div className="d-flex gap-2 mt-3 align-items-center">
                        <button className="btn btn-success btn-sm" onClick={handleSubmitScores}>
                            Submit Final Scores
                        </button>

                        {!myScoresConfirmed && (
                            <button
                                className="btn btn-outline-success btn-sm"
                                onClick={handleConfirmScores}
                            >
                                ✅ Confirm Scores
                            </button>
                        )}
                        {myScoresConfirmed && !bothTeamsConfirmedScores && (
                            <p className="text-warning small mb-0">
                                ⏳ Waiting for opponent to confirm...
                            </p>
                        )}
                    </div>
                </div>
            )}

            {/* STEP 3: Finalized */}
            {bothTeamsConfirmedSchedule && bothTeamsConfirmedScores && (
                <div className="mt-3">
                    <p className="text-success mb-0 fw-semibold">
                        ✅ Both teams confirmed — match finalized!
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
