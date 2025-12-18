import { useEffect, useState } from "react";
import { useParams, Link } from "react-router-dom";
import axios from "axios";
import CastModal from "../components/CastModal";

export default function MatchDetail() {
    const { id } = useParams();
    const [matchData, setMatchData] = useState(null);
    const [loading, setLoading] = useState(true);
    const [me, setMe] = useState(null);
    const [players, setPlayers] = useState([]);
    const [showCastModal, setShowCastModal] = useState(false);
    const [existingCast, setExistingCast] = useState(null);

    // -----------------------------------------------------
    // LOAD ALL PLAYERS
    // -----------------------------------------------------
    useEffect(() => {
        axios
            .get(`${import.meta.env.VITE_API_URL}/api/players`, { withCredentials: true })
            .then((res) => {
                const list = Array.isArray(res.data) ? res.data : [];

                // IMPORTANT FIX → stringify IDs
                const normalized = list.map(p => ({
                    ...p,
                    id: String(p.id),
                }));

                setPlayers(normalized);
            })
            .catch(() => setPlayers([]));
    }, []);

    // Helper: Safe name lookup
    function getPlayerName(id) {
        if (!id) return "None";

        // 1) Look inside rosterA / rosterB (frozen snapshot first)
        const allRoster = [...rosterA, ...rosterB];
        const snap = allRoster.find(r => String(r.player_id) === String(id));
        if (snap) return snap.display_name || snap.username;

        // 2) Fall back to global players list
        const p = players.find(x => String(x.id) === String(id));
        if (p) return p.display_name || p.username;

        return "Unknown";
    }

    // -----------------------------------------------------
    // LOAD MATCH DETAILS
    // -----------------------------------------------------
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

    // -----------------------------------------------------
    // LOAD USER (CASTER, MOD, ETC.)
    // -----------------------------------------------------
    useEffect(() => {
        axios
            .get(`${import.meta.env.VITE_API_URL}/api/me`, { withCredentials: true })
            .then((res) => setMe(res.data))
            .catch(() => setMe(null));
    }, []);

    // -----------------------------------------------------
    // INITIAL LOAD — CAST INFO
    // -----------------------------------------------------
    useEffect(() => {
        axios
            .get(`${import.meta.env.VITE_API_URL}/api/match/cast/get/${id}`, {
                withCredentials: true,
            })
            .then((res) => setExistingCast(res.data || null))
            .catch(() => setExistingCast(null));
    }, [id]);

    // -----------------------------------------------------
    // RELOAD CAST DATA AFTER PLAYERS LOAD
    // -----------------------------------------------------
    useEffect(() => {
        if (!players.length) return;

        axios
            .get(`${import.meta.env.VITE_API_URL}/api/match/cast/get/${id}`, {
                withCredentials: true,
            })
            .then((res) => setExistingCast(res.data || null))
            .catch(() => { });
    }, [players]);

    if (loading) return <p className="text-light">Loading match details...</p>;
    if (!matchData) return <p className="text-danger">⚠️ Match not found.</p>;

    // -----------------------------------------------------
    // NORMALIZE MATCH DATA
    // -----------------------------------------------------
    const match = matchData.match || {};
    const teams = matchData.teams || {};
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
                : "text-light";

    function convertToEmbed(url) {
        if (!url) return "";

        // youtu.be short links
        if (url.includes("youtu.be/")) {
            const id = url.split("youtu.be/")[1].split(/[?&]/)[0];
            return `https://www.youtube.com/embed/${id}`;
        }

        // youtube.com/watch?v=xxxx
        if (url.includes("watch?v=")) {
            const id = url.split("watch?v=")[1].split(/[?&]/)[0];
            return `https://www.youtube.com/embed/${id}`;
        }

        // youtube.com/live/xxxx
        if (url.includes("/live/")) {
            const id = url.split("/live/")[1].split(/[?&]/)[0];
            return `https://www.youtube.com/embed/${id}`;
        }

        return "";
    }

    // -----------------------------------------------------
    // NORMALIZE CAST (STRING IDs SO LOOKUP ALWAYS MATCHES)
    // -----------------------------------------------------
    const cast = matchData.cast || {};
    const castCasters = (cast.casters || []).map(String);
    const castCamera = cast.camera ? String(cast.camera) : "";
    const streamURL = cast.stream_url || "";
    const embedURL = convertToEmbed(streamURL);

    async function openCastEditor() {
        try {
            const res = await axios.get(
                `${import.meta.env.VITE_API_URL}/api/match/cast/get/${id}`,
                { withCredentials: true }
            );
            setExistingCast(res.data || { casters: [], camera: "" });
        } catch {
            setExistingCast({ casters: [], camera: "" });
        }

        setShowCastModal(true);
    }

    function convertToEmbed(url) {
        if (!url) return "";

        // youtu.be short links
        if (url.includes("youtu.be/")) {
            const id = url.split("youtu.be/")[1].split(/[?&]/)[0];
            return `https://www.youtube.com/embed/${id}`;
        }

        // youtube.com/watch?v=xxxx
        if (url.includes("watch?v=")) {
            const id = url.split("watch?v=")[1].split(/[?&]/)[0];
            return `https://www.youtube.com/embed/${id}`;
        }

        // youtube.com/live/xxxx
        if (url.includes("/live/")) {
            const id = url.split("/live/")[1].split(/[?&]/)[0];
            return `https://www.youtube.com/embed/${id}`;
        }

        return "";
    }

    return (
        <div className="container py-3 text-light">
            {/* --- Page Title & Caster Button --- */}
            <div className="d-flex justify-content-between align-items-center mb-3">
                <h2 className="mb-0 text-light">
                    Match Details <small className="ms-2">#{match.id || id}</small>
                </h2>

                {me && (match.isFinals || match.scheduled_date) && (
                    <button
                        className="btn btn-info btn-sm"
                        onClick={openCastEditor}
                    >
                        🎥 {existingCast ? "Edit Cast" : "Cast Match"}
                    </button>
                )}
            </div>

            <CastModal
                show={showCastModal}
                onClose={() => setShowCastModal(false)}
                matchID={id}
                existingCast={existingCast}
                urlBase={import.meta.env.VITE_API_URL}
                onSaved={() => {
                    axios.get(`${import.meta.env.VITE_API_URL}/api/match/${id}`)
                        .then((res) => setMatchData(res.data));

                    axios.get(`${import.meta.env.VITE_API_URL}/api/match/cast/get/${id}`)
                        .then((res) => setExistingCast(res.data));
                }}
            />

            {/* --- Header --- */}
            <div className="card bg-dark border-secondary mb-4 shadow-sm">
                <div className="card-body text-center">
                    <h4 className="mb-3">
                        <Link to={`/teams/${teamA.id}`} className="text-light fw-bold">
                            {teamA.name}
                        </Link>{" "}
                        <span className="text-secondary">vs</span>{" "}
                        <Link to={`/teams/${teamB.id}`} className="text-light fw-bold">
                            {teamB.name}
                        </Link>
                    </h4>
                    <p className={`mb-2 fw-bold ${statusColor}`}>
                        Status: {match.status || "Unknown"}
                    </p>
                    <p className="text-light mb-0">Scheduled: {formattedDate}</p>
                </div>
            </div>

            {/* 🎥 CAST & BROADCAST */}
            {(castCasters.length > 0 || castCamera || streamURL) && (
                <div className="card bg-dark border-info shadow-sm mb-4">
                    <div className="card-header border-info text-info fw-bold d-flex align-items-center">
                        <span style={{ fontSize: "1.4rem", marginRight: "8px" }}>🎥</span>
                        Match Cast & Broadcast
                    </div>

                    <div className="card-body text-light">

                        {/* === CAST INFO === */}
                        <p className="mb-2">
                            <strong className="text-info">Casters:</strong><br />
                            {castCasters.length > 0
                                ? castCasters.map((id) => getPlayerName(id)).join(", ")
                                : "No casters assigned"}
                        </p>

                        <p className="mb-4">
                            <strong className="text-warning">Camera Operator:</strong><br />
                            {castCamera
                                ? getPlayerName(castCamera)
                                : "None assigned"}
                        </p>

                        {/* === BROADCAST VIDEO === */}
                        {streamURL && embedURL && (
                            <>
                                <div
                                    style={{
                                        position: "relative",
                                        paddingBottom: "56.25%",
                                        height: 0,
                                        overflow: "hidden",
                                        borderRadius: "10px",
                                    }}
                                >
                                    <iframe
                                        src={embedURL}
                                        frameBorder="0"
                                        allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
                                        allowFullScreen
                                        title="Match Stream"
                                        style={{
                                            position: "absolute",
                                            top: 0,
                                            left: 0,
                                            width: "100%",
                                            height: "100%",
                                            borderRadius: "10px",
                                        }}
                                    ></iframe>
                                </div>

                                <p className="mt-3 mb-0 text-center">
                                    <a
                                        href={streamURL}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        className="text-info"
                                    >
                                        🔗 Open on YouTube
                                    </a>
                                </p>
                            </>
                        )}

                    </div>
                </div>
            )}

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

                                        return (
                                            <tr key={i}>
                                                <td>Map {m.map ?? i + 1}</td>
                                                <td>{m.mode || "Unknown"}</td>
                                                <td className={`fw-bold ${aWin ? "text-success" : bWin ? "text-danger" : "text-light"}`}>
                                                    {m.team_a_score}
                                                </td>
                                                <td className={`fw-bold ${bWin ? "text-success" : aWin ? "text-danger" : "text-light"}`}>
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
                                                        <span className="text-secondary">Tie</span>
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

            {/* --- League Subs --- */}
            <h4 className="text-light mt-4 mb-3">🧍 League Subs</h4>

            <div className="card bg-dark border-secondary mb-4 shadow-sm">
                <div className="card-body">
                    <p className="mb-2">
                        <strong className="text-info">{teamA.name} Sub:</strong>{" "}
                        {match.league_sub_a
                            ? getPlayerName(String(match.league_sub_a))
                            : "None"}
                    </p>

                    <p className="mb-0">
                        <strong className="text-warning">{teamB.name} Sub:</strong>{" "}
                        {match.league_sub_b
                            ? getPlayerName(String(match.league_sub_b))
                            : "None"}
                    </p>
                </div>
            </div>

            {/* --- Rosters --- */}
            <h4 className="text-light mt-4 mb-3">👥 Rosters at Time of Match</h4>
            <div className="row">
                {[{ team: teamA, roster: rosterA }, { team: teamB, roster: rosterB }].map(
                    ({ team, roster }, i) => (
                        <div className="col-md-6 mb-3" key={i}>
                            <div className="card bg-dark border-secondary">
                                <div className="card-header text-center text-light fw-bold">
                                    {team.name}
                                </div>
                                <ul className="list-group list-group-flush">
                                    {roster.length ? (
                                        roster.map((p, idx) => (
                                            <li
                                                key={idx}
                                                className="list-group-item bg-dark text-light d-flex justify-content-between align-items-center"
                                            >
                                                {/* CLICKABLE PLAYER NAME */}
                                                <Link
                                                    to={`/players/${p.player_id}`}
                                                    className="text-info text-decoration-none fw-bold"
                                                >
                                                    {p.display_name || p.username}
                                                </Link>

                                                <span>{p.role || "-"}</span>
                                            </li>
                                        ))
                                    ) : (
                                        <li className="list-group-item bg-dark text-light text-center">
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
                <Link to={`/teams/${teamA.id}`} className="btn btn-secondary">
                    ← Back to {teamA.name}
                </Link>
            </div>
        </div>
    );
}