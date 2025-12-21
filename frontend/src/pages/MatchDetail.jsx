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
        <div className="mx-auto text-light" style={{ maxWidth: 920 }}>

            {/* ================= HEADER ================= */}
            <div className="d-flex justify-content-between align-items-center mb-3">
                <h3 className="mb-0">
                    Match Details <span className="text-secondary">#{match.id || id}</span>
                </h3>

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
                        .then(res => setMatchData(res.data));
                    axios.get(`${import.meta.env.VITE_API_URL}/api/match/cast/get/${id}`)
                        .then(res => setExistingCast(res.data));
                }}
            />

            {/* ================= MATCH HERO ================= */}
            <div className="card bg-dark border-secondary mb-4 shadow-sm">
                <div className="card-body text-center">

                    <div className="d-flex justify-content-center align-items-center gap-3 mb-2">
                        <Link to={`/teams/${teamA.id}`} className="text-info fw-bold fs-5 text-decoration-none">
                            {teamA.name}
                        </Link>

                        <span className="text-secondary fw-semibold">vs</span>

                        <Link to={`/teams/${teamB.id}`} className="text-info fw-bold fs-5 text-decoration-none">
                            {teamB.name}
                        </Link>
                    </div>

                    <div className={`fw-semibold ${statusColor}`}>
                        {match.status || "Unknown"}
                    </div>

                    <div className="match-date mt-2">
                        {formattedDate}
                    </div>
                </div>
            </div>

            {/* ================= CAST & BROADCAST ================= */}
            {(castCasters.length > 0 || castCamera || streamURL) && (
                <div className="card bg-dark border-info shadow-sm mb-4">
                    <div className="card-header text-info fw-bold">
                        🔴 Match Broadcast
                    </div>

                    <div className="card-body">

                        <div className="mb-3">
                            <div className="small text-secondary">Casters</div>
                            <div className="fw-semibold">
                                {castCasters.length
                                    ? castCasters.map(id => getPlayerName(id)).join(", ")
                                    : "None"}
                            </div>
                        </div>

                        <div className="mb-4">
                            <div className="small text-secondary">Camera Operator</div>
                            <div className="fw-semibold">
                                {castCamera ? getPlayerName(castCamera) : "None"}
                            </div>
                        </div>

                        {streamURL && embedURL && (
                            <div className="rounded overflow-hidden border">
                                <div style={{ aspectRatio: "16 / 9" }}>
                                    <iframe
                                        src={embedURL}
                                        className="w-100 h-100"
                                        allowFullScreen
                                        title="Match Stream"
                                    />
                                </div>
                            </div>
                        )}
                    </div>
                </div>
            )}

            {/* ================= MAP SCORES ================= */}
            <h4 className="mb-3">🗺️ Match Result</h4>

            {(() => {
                const maps = Array.isArray(matchData.map_scores) ? matchData.map_scores : [];
                const validMaps = maps.filter(
                    m => !(m.team_a_score === 0 && m.team_b_score === 0)
                );

                if (!validMaps.length) {
                    return <p className="text-secondary">No map scores recorded.</p>;
                }

                const totalA = validMaps.filter(m => m.team_a_score > m.team_b_score).length;
                const totalB = validMaps.filter(m => m.team_b_score > m.team_a_score).length;
                const winner =
                    totalA > totalB ? teamA.name :
                        totalB > totalA ? teamB.name : null;

                return (
                    <>
                        <div className="text-center mb-3">
                            <span className="badge bg-info fs-6 px-3 py-2">
                                {teamA.name} {totalA} – {totalB} {teamB.name}
                            </span>

                            {winner && (
                                <div className="text-success fw-bold mt-2">
                                    🏆 Winner: {winner}
                                </div>
                            )}
                        </div>

                        <div className="table-responsive">
                            <table className="table table-dark table-striped align-middle text-center">
                                <thead>
                                    <tr>
                                        <th>Map</th>
                                        <th>Mode</th>
                                        <th>{teamA.name}</th>
                                        <th>{teamB.name}</th>
                                        <th></th>
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
                                                <td className={aWin ? "text-success fw-bold" : ""}>
                                                    {m.team_a_score}
                                                </td>
                                                <td className={bWin ? "text-success fw-bold" : ""}>
                                                    {m.team_b_score}
                                                </td>
                                                <td>
                                                    {aWin && "✅ " + teamA.name}
                                                    {bWin && "✅ " + teamB.name}
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

            {/* ================= LEAGUE SUBS ================= */}
            <h4 className="mt-4 mb-3">🧍 League Subs</h4>

            <div className="card bg-dark border-secondary shadow-sm mb-4">
                <div className="card-body d-flex justify-content-between">
                    <div>
                        <div className="small text-secondary">{teamA.name}</div>
                        <div className="fw-semibold">
                            {match.league_sub_a ? getPlayerName(match.league_sub_a) : "None"}
                        </div>
                    </div>

                    <div>
                        <div className="small text-secondary">{teamB.name}</div>
                        <div className="fw-semibold">
                            {match.league_sub_b ? getPlayerName(match.league_sub_b) : "None"}
                        </div>
                    </div>
                </div>
            </div>

            {/* ================= ROSTERS ================= */}
            <h4 className="mt-4 mb-3">👥 Rosters at Match Time</h4>

            <div className="row g-3">
                {[{ team: teamA, roster: rosterA }, { team: teamB, roster: rosterB }].map(
                    ({ team, roster }, i) => (
                        <div className="col-md-6" key={i}>
                            <div className="card bg-dark border-secondary h-100">
                                <div className="card-header text-center fw-bold">
                                    {team.name}
                                </div>

                                <ul className="list-group list-group-flush">
                                    {roster.length ? roster.map(p => (
                                        <li
                                            key={p.player_id}
                                            className="list-group-item bg-dark d-flex justify-content-between"
                                        >
                                            <Link
                                                to={`/players/${p.player_id}`}
                                                className="text-info fw-semibold text-decoration-none"
                                            >
                                                {p.display_name || p.username}
                                            </Link>
                                            <span className="text-secondary">{p.role}</span>
                                        </li>
                                    )) : (
                                        <li className="list-group-item bg-dark text-secondary text-center">
                                            No roster data
                                        </li>
                                    )}
                                </ul>
                            </div>
                        </div>
                    )
                )}
            </div>

            {/* ================= BACK ================= */}
            <div className="mt-4">
                <Link to={`/teams/${teamA.id}`} className="btn btn-outline-light">
                    ← Back to {teamA.name}
                </Link>
            </div>
        </div>
    );
}