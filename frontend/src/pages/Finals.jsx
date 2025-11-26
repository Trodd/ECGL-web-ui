import { useState, useEffect } from "react";
import axios from "axios";

export default function FinalsPage() {
    const urlBase = import.meta.env.VITE_API_URL;

    const [season, setSeason] = useState("1");
    const [teamsInput, setTeamsInput] = useState("");
    const [bracket, setBracket] = useState({});
    const [msg, setMsg] = useState("");
    const [loading, setLoading] = useState(true);

    // ========================================
    // LOAD FINALS BRACKET
    // ========================================
    async function loadBracket() {
        try {
            const res = await axios.get(`${urlBase}/api/mod/finals/list?season=${season}`, {
                withCredentials: true,
            });

            setBracket(res.data.bracket || {});
        } catch (err) {
            console.error("❌ Failed loading finals:", err);
            setMsg("❌ Failed to load finals bracket");
        } finally {
            setLoading(false);
        }
    }

    useEffect(() => {
        loadBracket();
    }, [season]);

    // ========================================
    // CREATE FINALS
    // ========================================
    async function handleCreateFinals(mode) {
        let teams = [];

        if (mode === "manual") {
            teams = teamsInput
                .split(",")
                .map((t) => t.trim())
                .filter((t) => t.length > 0);

            if (teams.length < 2) {
                setMsg("⚠️ Need at least 2 teams for manual finals setup.");
                return;
            }
        } else {
            // Auto mode
            try {
                const r = await axios.get(`${urlBase}/api/teams`);
                teams = r.data.map(t => t.name);
            } catch {
                return setMsg("❌ Cannot load team list for auto finals.");
            }
        }

        try {
            await axios.post(
                `${urlBase}/api/mod/finals/create`,
                { season, teams },
                { withCredentials: true }
            );

            setMsg("✅ Finals created successfully!");
            loadBracket();
        } catch (err) {
            console.error(err);
            setMsg("❌ Failed to create finals.");
        }
    }

    // ========================================
    // SET WINNER / LOSER
    // ========================================
    async function setPlacement(matchID, winner, loser) {
        try {
            await axios.post(
                `${urlBase}/api/mod/finals/set-placement`,
                { match_id: matchID, winner, loser },
                { withCredentials: true }
            );

            setMsg(`🏆 Updated match ${matchID}`);
            loadBracket();
        } catch (err) {
            console.error(err);
            setMsg("❌ Failed to update match result.");
        }
    }

    // ========================================
    // RENDER MATCH CARD
    // ========================================
    function MatchCard({ m }) {
        return (
            <div
                className="p-2 mb-2 bg-dark border border-secondary rounded text-light"
                style={{ width: "260px" }}
            >
                <div className="fw-bold mb-1">{m.match_code}</div>

                <div className="d-flex justify-content-between">
                    <span>{m.team_a_name || "??"}</span>
                    <button
                        className="btn btn-sm btn-success"
                        onClick={() => setPlacement(m.id, m.team_a_name, m.team_b_name)}
                        disabled={!m.team_a_name || !m.team_b_name}
                    >
                        Win
                    </button>
                </div>

                <div className="d-flex justify-content-between mt-1">
                    <span>{m.team_b_name || "??"}</span>
                    <button
                        className="btn btn-sm btn-success"
                        onClick={() => setPlacement(m.id, m.team_b_name, m.team_a_name)}
                        disabled={!m.team_a_name || !m.team_b_name}
                    >
                        Win
                    </button>
                </div>

                {m.winner_name && (
                    <div className="mt-2 small text-success">
                        Winner: <b>{m.winner_name}</b>
                    </div>
                )}
                {m.loser_name && (
                    <div className="small text-danger">
                        Loser: <b>{m.loser_name}</b>
                    </div>
                )}
            </div>
        );
    }

    // ========================================
    // PAGE
    // ========================================
    if (loading) return <p className="text-light">⏳ Loading finals...</p>;

    return (
        <div className="container text-light mt-4 mb-5">

            <h2>🏆 Finals Bracket</h2>
            <p className="small text-muted">Season {season}</p>

            {msg && (
                <div className={`alert ${msg.startsWith("✅") ? "alert-success" : "alert-warning"}`}>
                    {msg}
                </div>
            )}

            {/* CREATE FINALS SECTION */}
            <div className="p-3 bg-dark border border-secondary rounded mb-4">
                <h5 className="text-info">⚙️ Create Finals</h5>

                <label className="small text-light">Season</label>
                <input
                    type="text"
                    className="form-control bg-black text-light mb-2"
                    value={season}
                    onChange={(e) => setSeason(e.target.value)}
                    style={{ maxWidth: 120 }}
                />

                {/* Manual mode */}
                <label className="small text-light">Teams (comma-separated)</label>
                <textarea
                    className="form-control bg-black text-light mb-2"
                    placeholder="TeamA, TeamB, TeamC..."
                    value={teamsInput}
                    onChange={(e) => setTeamsInput(e.target.value)}
                    style={{ maxWidth: 500, height: 70 }}
                />

                <div className="d-flex gap-2">
                    <button className="btn btn-outline-success" onClick={() => handleCreateFinals("manual")}>
                        ➕ Create (Manual)
                    </button>

                    <button className="btn btn-outline-primary" onClick={() => handleCreateFinals("auto")}>
                        ⚡ Create Automatically
                    </button>
                </div>
            </div>

            {/* BRACKET */}
            <h4 className="text-info mt-4 mb-3">📋 Double-Elimination Bracket</h4>

            <div className="row">
                {Object.keys(bracket).length === 0 && (
                    <p className="text-warning">No finals created yet.</p>
                )}

                {Object.entries(bracket).map(([round, matches]) => (
                    <div className="col-md-3 mb-4" key={round}>
                        <h5 className="text-warning">{round}</h5>

                        {matches.map((m) => (
                            <MatchCard key={m.id} m={m} />
                        ))}
                    </div>
                ))}
            </div>
        </div>
    );
}
