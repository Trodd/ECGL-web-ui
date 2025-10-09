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
            setMsg("✅ Match scheduled successfully!");
            await loadTeam();
        } catch {
            setMsg("❌ Failed to schedule match.");
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
            setMsg("✅ Scores submitted successfully!");
            await loadTeam();
        } catch {
            setMsg("❌ Failed to submit scores.");
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
                {isCaptain && match.date && (
                    <button
                        className="btn btn-sm btn-outline-light"
                        onClick={() => setEditing((e) => !e)}
                    >
                        🗓️ Edit
                    </button>
                )}
            </div>

            <p className="text-muted mb-2">
                Status: <strong>{match.result || "Pending"}</strong>
            </p>

            {/* schedule form */}
            {(!match.date || editing) && isCaptain && (
                <div>
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
                            <button className="btn btn-secondary btn-sm" onClick={() => setEditing(false)}>
                                Cancel
                            </button>
                        )}
                    </div>
                    {msg && (
                        <p
                            className={`small mb-0 ${msg.startsWith("✅")
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
            )}

            {/* score submission */}
            {match.date && !editing && isCaptain && (
                <div className="mt-3">
                    <h6 className="text-light mb-2">🎯 Submit Scores</h6>
                    <div className="row g-3">
                        {[1, 2, 3].map((n) => (
                            <div className="col-md-4" key={n}>
                                <div className="border rounded p-2 bg-secondary-subtle">
                                    <label className="form-label text-light mb-1">Map {n}</label>
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

                    <div className="d-flex gap-2 mt-3">
                        <button className="btn btn-success btn-sm" onClick={handleSubmitScores}>
                            Submit Final Scores
                        </button>
                    </div>
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
            )}
        </div>
    );
}
