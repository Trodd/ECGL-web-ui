import { useEffect, useState } from "react";
import { useParams, Link } from "react-router-dom";
import axios from "axios";

export default function MatchDetail() {
    const { id } = useParams();

    const [matchData, setMatchData] = useState(null);
    const [loading, setLoading] = useState(true);

    // ⭐ NEW: store logged-in user
    const [me, setMe] = useState(null);

    // ⭐ NEW: read current configured week
    const currentWeek = import.meta.env.VITE_CURRENT_WEEK;

    useEffect(() => {
        axios
            .get(`${import.meta.env.VITE_API_URL}/api/match/${id}`)
            .then((res) => {
                console.log("✅ /api/match response:", res.data);
                setMatchData(res.data);
            })
            .catch((err) => {
                console.error("❌ Failed to load match:", err);
                setMatchData(null);
            })
            .finally(() => setLoading(false));
    }, [id]);

    useEffect(() => {
        axios
            .get(`${import.meta.env.VITE_API_URL}/api/me`, { withCredentials: true })
            .then((res) => setMe(res.data))
            .catch(() => setMe(null));
    }, []);

    async function handleCastRequest() {
        try {
            const res = await axios.post(
                `${import.meta.env.VITE_API_URL}/api/match/cast`,
                { match_id: Number(id) },
                { withCredentials: true }
            );

            if (res?.data?.channel_id) {
                alert("🎥 Cast channel created!");

                const guildID = import.meta.env.VITE_DISCORD_GUILD_ID;
            }
        } catch (err) {
            console.error("❌ Cast Error:", err);
            alert(err.response?.data || "Failed to create cast channel.");
        }
    }

    if (loading) return <p className="text-light">Loading match details...</p>;
    if (!matchData) return <p className="text-danger">⚠️ Match not found.</p>;

    // --- Normalize data ---
    const match = matchData.match || {};
    const teams = matchData.teams || {};
    const maps = Array.isArray(matchData.maps) ? matchData.maps : [];
    const rosterA = Array.isArray(matchData.roster?.a) ? matchData.roster.a : [];
    const rosterB = Array.isArray(matchData.roster?.b) ? matchData.roster.b : [];

    const teamA = teams.a || {};
    const teamB = teams.b || {};

    const date = match.scheduled_date || match.proposed_date;
    const formattedDate = date ? new Date(date).toLocaleString() : "TBD";

    const statusColor =
        match.status === "Finished" || match.status === "Completed"
            ? "text-success"
            : match.status === "Scheduled"
                ? "text-warning"
                : "text-muted";

    return (
        <div className="container py-3 text-light">
            {/* --- Page Title & Caster Button --- */}
            <div className="d-flex justify-content-between align-items-center mb-3">
                <h2 className="mb-0 text-light">
                    Match Details{" "}
                    <small className="text-light ms-2">#{match.id || id}</small>
                </h2>

                {/* ⭐ Caster-only button */}
                {me?.is_caster &&
                    match.status === "Scheduled" && (
                        <button
                            className="btn btn-info btn-sm"
                            onClick={handleCastRequest}
                            style={{ whiteSpace: "nowrap" }}
                        >
                            🎥 Cast Match
                        </button>
                    )}
            </div>

            {/* --- Header --- */}
            <div className="card bg-dark border-secondary mb-4 shadow-sm">
                <div className="card-body text-center">
                    <h4 className="mb-3">
                        <Link
                            to={`/teams/${teamA.id || ""}`}
                            className="text-decoration-none text-light fw-bold"
                        >
                            {teamA.name || "Unknown"}
                        </Link>{" "}
                        <span className="text-secondary">vs</span>{" "}
                        <Link
                            to={`/teams/${teamB.id || ""}`}
                            className="text-decoration-none text-light fw-bold"
                        >
                            {teamB.name || "Unknown"}
                        </Link>
                    </h4>
                    <p className={`mb-2 fw-bold ${statusColor}`}>
                        Status: {match.status || "Unknown"}
                    </p>
                    <p className="text-muted mb-0">Scheduled: {formattedDate}</p>
                </div>
            </div>

            {/* --- Map Scores --- */}
            <h4 className="text-light mb-3">🗺️ Map Scores</h4>

            {(() => {
                const mapData = Array.isArray(matchData.map_scores)
                    ? matchData.map_scores
                    : [];

                const validMaps = mapData.filter(
                    (m) =>
                        !(
                            (m.team_a_score === null || m.team_a_score === 0) &&
                            (m.team_b_score === null || m.team_b_score === 0)
                        )
                );

                if (validMaps.length === 0)
                    return <p className="text-light">No map scores recorded yet.</p>;

                const totalA = validMaps.filter(
                    (m) => m.team_a_score > m.team_b_score
                ).length;
                const totalB = validMaps.filter(
                    (m) => m.team_b_score > m.team_a_score
                ).length;
                const winner =
                    totalA > totalB
                        ? teamA.name
                        : totalB > totalA
                            ? teamB.name
                            : null;

                return (
                    <>
                        <p className="text-info fw-bold mb-2">
                            {teamA.name}: {totalA} – {totalB} : {teamB.name}{" "}
                            {winner && (
                                <span className="ms-2 text-success">
                                    🏆 Winner: {winner}
                                </span>
                            )}
                        </p>

                        <div className="table-responsive">
                            <table className="table table-dark table-striped align-middle text-center">
                                <thead className="table-secondary text-dark">
                                    <tr>
                                        <th>Map</th>
                                        <th>Gamemode</th>
                                        <th>{teamA.name}</th>
                                        <th>{teamB.name}</th>
                                        <th>Winner</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {validMaps.map((m, i) => {
                                        const aWin = m.team_a_score > m.team_b_score;
                                        const bWin = m.team_b_score > m.team_a_score;
                                        const mode =
                                            m.mode && m.mode.trim() !== ""
                                                ? m.mode
                                                : "Unknown";

                                        return (
                                            <tr key={i}>
                                                <td>Map {m.map ?? i + 1}</td>
                                                <td>{mode}</td>
                                                <td className={`fw-bold ${aWin
                                                    ? "text-success"
                                                    : bWin
                                                        ? "text-danger"
                                                        : "text-light"
                                                    }`}>
                                                    {m.team_a_score}
                                                </td>
                                                <td className={`fw-bold ${bWin
                                                    ? "text-success"
                                                    : aWin
                                                        ? "text-danger"
                                                        : "text-light"
                                                    }`}>
                                                    {m.team_b_score}
                                                </td>
                                                <td>
                                                    {aWin ? (
                                                        <span className="text-success fw-semibold">
                                                            ✅ {teamA.name}
                                                        </span>
                                                    ) : bWin ? (
                                                        <span className="text-success fw-semibold">
                                                            ✅ {teamB.name}
                                                        </span>
                                                    ) : (
                                                        <span className="text-secondary">
                                                            Tie
                                                        </span>
                                                    )}
                                                </td>
                                            </tr>
                                        );
                                    })}
                                </tbody>
                            </table>
                        </div>
                    </>
                );
            })()}

            {/* --- Rosters --- */}
            <h4 className="text-light mt-4 mb-3">👥 Rosters at Time of Match</h4>
            <div className="row">
                {[{ team: teamA, roster: rosterA }, { team: teamB, roster: rosterB }].map(
                    ({ team, roster }, i) => (
                        <div className="col-md-6 mb-3" key={i}>
                            <div className="card bg-dark border-secondary">
                                <div className="card-header text-center text-light fw-bold">
                                    {team.name || `Team ${i === 0 ? "A" : "B"}`}
                                </div>
                                <ul className="list-group list-group-flush">
                                    {roster.length ? (
                                        roster.map((p) => (
                                            <li
                                                key={p.player_id || p.username || i}
                                                className="list-group-item bg-dark text-light d-flex justify-content-between align-items-center"
                                            >
                                                <span>{p.display_name || p.username || "Unknown"}</span>
                                                <span>{p.role || "-"}</span>
                                            </li>
                                        ))
                                    ) : (
                                        <li className="list-group-item bg-dark text-muted text-center">
                                            No recorded players.
                                        </li>
                                    )}
                                </ul>
                            </div>
                        </div>
                    )
                )}
            </div>

            {/* --- Back Button --- */}
            <div className="mt-4">
                <Link to={`/teams/${teamA.id || ""}`} className="btn btn-secondary">
                    ← Back to {teamA.name || "Teams"}
                </Link>
            </div>
        </div>
    );
}
