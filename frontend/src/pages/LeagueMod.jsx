import { useState, useEffect } from "react";
import axios from "axios";

export default function LeagueMod() {
    const [me, setMe] = useState(null);
    const [loading, setLoading] = useState(true);
    const [msg, setMsg] = useState("");
    const [week, setWeek] = useState("");
    const [matchID, setMatchID] = useState("");
    const [teamID, setTeamID] = useState("");
    const [playerID, setPlayerID] = useState("");
    const [teams, setTeams] = useState([]);
    const [matches, setMatches] = useState([]);
    const [season, setSeason] = useState(null);
    const [previewMatches, setPreviewMatches] = useState([]);
    const [previewWeek, setPreviewWeek] = useState("");
    const [showPreview, setShowPreview] = useState(false);
    const [newMatchA, setNewMatchA] = useState("");
    const [newMatchB, setNewMatchB] = useState("");
    const [previewSeason, setPreviewSeason] = useState("");

    // Search filters
    const [teamSearch, setTeamSearch] = useState("");
    const [matchSearch, setMatchSearch] = useState("");

    const urlBase = import.meta.env.VITE_API_URL;

    // --- Fetch moderator session ---
    useEffect(() => {
        axios
            .get(`${urlBase}/api/me`, { withCredentials: true })
            .then((res) => setMe(res.data))
            .catch(() => setMe(null))
            .finally(() => setLoading(false));
    }, []);

    // --- Fetch current season ---
    useEffect(() => {
        axios
            .get(`${urlBase}/api/season`)
            .then((res) => {
                if (res.data?.season) {
                    setSeason(res.data.season.toString());
                }
            })
            .catch(() => setSeason(null));
    }, []);

    // --- Load teams + matches (filtered by current season) ---
    useEffect(() => {
        async function loadLists() {
            try {
                const [teamRes, matchRes] = await Promise.all([
                    axios.get(`${urlBase}/api/teams`, { withCredentials: true }),
                    axios.get(`${urlBase}/api/matches/public`, { withCredentials: true }),
                ]);

                setTeams(Array.isArray(teamRes.data) ? teamRes.data : []);

                const matchList = [];
                if (matchRes.data?.matches) {
                    Object.entries(matchRes.data.matches).forEach(([seasonKey, weeks]) => {
                        if (season && seasonKey !== season) return;
                        Object.values(weeks).forEach((list) =>
                            list.forEach((m) =>
                                matchList.push({
                                    id: m.id,
                                    match_code: m.match_code,
                                    team_a: m.team_a,
                                    team_b: m.team_b,
                                })
                            )
                        );
                    });
                }
                setMatches(matchList);
            } catch (err) {
                console.error("❌ Failed to load lists:", err);
                setMsg("⚠️ Failed to load team/match data.");
            }
        }
        if (season) loadLists();
    }, [season]);

    if (loading) return <p>⏳ Checking permissions...</p>;
    if (!me) return <p>🔐 Please log in to access the League Mod Panel.</p>;
    if (!me.is_mod)
        return <p>🚫 You do not have permission to view this panel.</p>;

    // --- Helpers ---
    const getTeamLabel = (id) => {
        const t = teams.find((x) => x.id === parseInt(id));
        return t ? `${t.name} (#${t.id})` : `Team #${id}`;
    };

    const getMatchLabel = (id) => {
        const m = matches.find((x) => x.id === parseInt(id));
        return m ? `${m.match_code}: ${m.team_a} vs ${m.team_b}` : `Match #${id}`;
    };

    async function safePost(endpoint, payload, successMsg) {
        try {
            const res = await axios.post(`${urlBase}${endpoint}`, payload, {
                withCredentials: true,
            });
            setMsg(`✅ ${successMsg}`);
            console.log(res.data);
        } catch (err) {
            console.error("❌ API error:", err.response?.data || err.message);
            setMsg("❌ Request failed — check console for details.");
        }
    }

    // --- Filters ---
    const filteredTeams = teams.filter((t) =>
        t.name.toLowerCase().includes(teamSearch.toLowerCase())
    );
    const filteredMatches = matches.filter((m) =>
        `${m.match_code} ${m.team_a} ${m.team_b}`
            .toLowerCase()
            .includes(matchSearch.toLowerCase())
    );

    // ===========================================================
    // ACTION HANDLERS
    // ===========================================================
    const handlePreviewWeek = async () => {
        if (!week || isNaN(week)) {
            setMsg("⚠️ Please enter a valid week number first.");
            return;
        }

        try {
            const res = await axios.get(`${urlBase}/api/mod/matches/preview?week=${week}`, {
                withCredentials: true,
            });
            if (res.data?.matches?.length) {
                setPreviewMatches(res.data.matches);
                setPreviewWeek(week);
                setPreviewSeason(res.data.season || currentSeason || "1");
                setShowPreview(true);
                setMsg(`✅ Preview generated for Week ${week}.`);
            } else {
                setMsg("⚠️ No matches returned from preview.");
            }
        } catch (err) {
            console.error("❌ Preview failed:", err);
            setMsg("❌ Failed to load preview.");
        }
    };

    const handlePublishWeek = async () => {
        if (!previewWeek) return setMsg("⚠️ Preview a week first.");
        await safePost(
            "/api/mod/matches/generate",
            { week: parseInt(previewWeek) },
            `Published matches for Week ${previewWeek}`
        );
        setShowPreview(false);
        setPreviewMatches([]);
    };

    const handleForceSchedule = async () => {
        if (!matchID) return setMsg("⚠️ Select a match first.");

        // Get any valid team to satisfy backend requirement
        const actingTeam = teams.length > 0 ? teams[0].id : 1;
        const date = new Date().toISOString();

        await safePost(
            "/api/match/schedule", // ✅ normal route (shared)
            { match_id: parseInt(matchID), team_id: actingTeam, date },
            `Force scheduled ${getMatchLabel(matchID)}`
        );
    };

    const handleForfeit = async () => {
        if (!matchID || !teamID)
            return setMsg("⚠️ Select both a match and the winning team first.");

        await safePost(
            "/api/mod/match/forfeit", // ✅ mod route
            { match_id: parseInt(matchID), winner_team_id: parseInt(teamID) },
            `Forced forfeit for ${getMatchLabel(matchID)} → Winner: ${getTeamLabel(teamID)}`
        );
    };

    const handleDoubleForfeit = async () => {
        if (!matchID) return setMsg("⚠️ Select a match first.");

        await safePost(
            "/api/mod/match/double-forfeit", // ✅ mod route
            { match_id: parseInt(matchID) },
            `Applied double forfeit to ${getMatchLabel(matchID)}`
        );
    };

    const handleResetMatch = async () => {
        if (!matchID) return setMsg("⚠️ Select a match first.");

        await safePost(
            "/api/mod/match/reset", // ✅ mod route
            { match_id: parseInt(matchID) },
            `Reset ${getMatchLabel(matchID)}`
        );
    };

    const handleDeleteMatch = async () => {
        if (!matchID) return setMsg("⚠️ Select a match first.");

        await safePost(
            "/api/mod/match/delete", // ✅ mod route
            { match_id: parseInt(matchID) },
            `Deleted ${getMatchLabel(matchID)}`
        );
    };

    // --- SCORE TOOLS ---
    const handleForceSubmitScores = async () => {
        if (!matchID) return setMsg("⚠️ Select a match first.");
        await safePost(
            "/api/match/submit-score",
            { match_id: parseInt(matchID), team_id: 0, maps: [] },
            `Force submitted ${getMatchLabel(matchID)}`
        );
    };

    const handleEditScores = async () => {
        if (!matchID) return setMsg("⚠️ Select a match first.");
        await safePost(
            "/api/mod/match/edit-score",
            { match_id: parseInt(matchID) },
            `Edited map scores for ${getMatchLabel(matchID)}`
        );
    };

    const handleAdjustRating = async () => {
        if (!teamID) return setMsg("⚠️ Select a team first.");
        await safePost(
            "/api/mod/team/adjust-rating",
            { team_id: parseInt(teamID), delta: 25 },
            `Adjusted rating for ${getTeamLabel(teamID)}`
        );
    };

    // --- TEAM / PLAYER / DATA TOOLS (same as before) ---
    const handleRenameTeam = async () => {
        if (!teamID) return setMsg("⚠️ Select a team first.");
        const newName = prompt("Enter new team name:");
        if (!newName) return;
        await safePost(
            "/api/mod/team/rename",
            { team_id: parseInt(teamID), new_name: newName },
            `Renamed ${getTeamLabel(teamID)} to ${newName}`
        );
    };

    const handleDisbandTeam = async () => {
        if (!teamID) return setMsg("⚠️ Select a team first.");
        await safePost(
            "/api/mod/team/disband",
            { team_id: parseInt(teamID) },
            `Disbanded ${getTeamLabel(teamID)}`
        );
    };

    const handleLockTeam = async () => {
        if (!teamID) return setMsg("⚠️ Select a team first.");
        await safePost(
            "/api/mod/team/lock",
            { team_id: parseInt(teamID) },
            `Toggled lock for ${getTeamLabel(teamID)}`
        );
    };

    const handleKickPlayer = async () => {
        if (!teamID || !playerID)
            return setMsg("⚠️ Select team and enter player ID.");
        await safePost(
            "/api/team/kick",
            { team_id: parseInt(teamID), player_id: playerID },
            `Kicked player ${playerID} from ${getTeamLabel(teamID)}`
        );
    };

    const handleBanPlayer = async () => {
        if (!playerID) return setMsg("⚠️ Enter player ID first.");
        await safePost("/api/mod/player/ban", { player_id: playerID }, `Banned player ${playerID}`);
    };

    const handleUnbanPlayer = async () => {
        if (!playerID) return setMsg("⚠️ Enter player ID first.");
        await safePost("/api/mod/player/unban", { player_id: playerID }, `Unbanned player ${playerID}`);
    };

    const handleArchiveSeason = async () => {
        const reset = confirm("Archive current season and reset everything?");
        await safePost("/api/mod/season/archive", { reset_after: reset }, "Archived season data");
    };

    const handleResetLeaderboard = async () => {
        await safePost("/api/mod/leaderboard/reset", {}, "Reset leaderboards");
    };

    const handleSyncHistory = async () => {
        await safePost("/api/mod/player/sync-history", {}, "Synced player history");
    };

    const handleRebuildELO = async () => {
        await safePost("/api/mod/elo/rebuild", {}, "Rebuilt ELO rankings");
    };

    // ===========================================================
    // RENDER
    // ===========================================================
    return (
        <div className="text-light container mt-3 mb-5">
            <h2>🛠️ League Moderator Panel</h2>
            <p className="text-light small mb-3">
                Season {season || "?"} • Admin tools for ECGL moderators
            </p>

            {msg && (
                <div
                    className={`alert ${msg.startsWith("✅")
                        ? "alert-success"
                        : msg.startsWith("⚠️")
                            ? "alert-warning"
                            : "alert-danger"
                        } small`}
                >
                    {msg}
                </div>
            )}

            <div className="accordion" id="modAccordion">
                {/* 🏁 MATCH TOOLS */}
                <AccordionItem
                    id="matchTools"
                    title="🏁 Match Tools"
                    children={
                        <>
                            {/* --- Weekly Match Generation --- */}
                            <h6 className="mb-2">📅 Generate Weekly Matches</h6>

                            {/* Week Input + Small Preview Button */}
                            <div className="d-flex gap-2 align-items-center mb-3" style={{ maxWidth: 320 }}>
                                <input
                                    type="number"
                                    className="form-control bg-dark text-light"
                                    placeholder="Enter week..."
                                    value={week}
                                    onChange={(e) => setWeek(e.target.value)}
                                />
                                <button
                                    className="btn btn-outline-info btn-sm"
                                    onClick={handlePreviewWeek}
                                    style={{ whiteSpace: "nowrap" }}
                                >
                                    👀 Preview
                                </button>
                            </div>

                            {/* --- Preview Section (full width, below input) --- */}
                            {showPreview && previewMatches.length > 0 && (
                                <div
                                    className="bg-dark p-3 rounded shadow-sm mb-4"
                                    style={{
                                        width: "100%",
                                        maxWidth: "1100px",
                                        margin: "0 auto",
                                        border: "1px solid rgba(255,255,255,0.1)",
                                    }}
                                >
                                    <div className="d-flex justify-content-between align-items-center mb-3">
                                        <h5 className="text-light mb-0">📋 Preview for Week {previewWeek}</h5>
                                        <button
                                            className="btn btn-success btn-sm"
                                            onClick={handlePublishWeek}
                                        >
                                            ✅ Confirm & Publish
                                        </button>
                                    </div>

                                    {/* 🔹 Manual Add Matchup */}
                                    <div
                                        className="bg-secondary bg-opacity-10 p-3 rounded mb-3"
                                        style={{ maxWidth: "900px", margin: "0 auto" }}
                                    >
                                        <h6 className="text-info mb-2">➕ Add Custom Matchup</h6>
                                        <div className="d-flex flex-wrap align-items-center gap-2">
                                            <select
                                                className="form-select bg-dark text-light"
                                                value={newMatchA}
                                                onChange={(e) => setNewMatchA(e.target.value)}
                                                style={{ maxWidth: 250 }}
                                            >
                                                <option value="">Select Team A...</option>
                                                {teams.map((t) => (
                                                    <option key={t.id} value={t.name}>
                                                        {t.name}
                                                    </option>
                                                ))}
                                            </select>
                                            <select
                                                className="form-select bg-dark text-light"
                                                value={newMatchB}
                                                onChange={(e) => setNewMatchB(e.target.value)}
                                                style={{ maxWidth: 250 }}
                                            >
                                                <option value="">Select Team B...</option>
                                                {teams.map((t) => (
                                                    <option key={t.id} value={t.name}>
                                                        {t.name}
                                                    </option>
                                                ))}
                                            </select>
                                            <button
                                                className="btn btn-outline-success btn-sm"
                                                onClick={() => {
                                                    if (!newMatchA || !newMatchB || newMatchA === newMatchB) {
                                                        setMsg("⚠️ Please select two different teams.");
                                                        return;
                                                    }
                                                    const exists = previewMatches.some(
                                                        (m) =>
                                                            (m.team_a === newMatchA && m.team_b === newMatchB) ||
                                                            (m.team_a === newMatchB && m.team_b === newMatchA)
                                                    );
                                                    if (exists) {
                                                        setMsg("⚠️ That matchup already exists.");
                                                        return;
                                                    }
                                                    setPreviewMatches((prev) => {
                                                        const nextNumber = prev.length + 1;
                                                        const newCode = `${previewSeason || currentSeason}-Week${previewWeek}-M${String(nextNumber).padStart(3, "0")}`;
                                                        return [
                                                            ...prev,
                                                            {
                                                                team_a: newMatchA,
                                                                team_b: newMatchB,
                                                                match_code: newCode,
                                                            },
                                                        ];
                                                    });
                                                    setNewMatchA("");
                                                    setNewMatchB("");
                                                    setMsg("✅ Added custom matchup.");
                                                }}
                                            >
                                                ➕ Add
                                            </button>
                                        </div>
                                    </div>

                                    {/* 🔹 Team Match Count Indicator */}
                                    {previewMatches.length > 0 && (
                                        <div
                                            className="bg-secondary bg-opacity-10 p-2 rounded mb-3"
                                            style={{ maxWidth: "900px", margin: "0 auto" }}
                                        >
                                            <h6 className="text-info mb-2">
                                                ⚙️ Team Match Counts
                                            </h6>
                                            <ul className="list-unstyled text-light small mb-0">
                                                {Object.entries(
                                                    previewMatches.reduce((counts, m) => {
                                                        counts[m.team_a] = (counts[m.team_a] || 0) + 1;
                                                        counts[m.team_b] = (counts[m.team_b] || 0) + 1;
                                                        return counts;
                                                    }, {})
                                                )
                                                    .sort(([aName], [bName]) => aName.localeCompare(bName))
                                                    .map(([team, count]) => (
                                                        <li key={team}>
                                                            <span
                                                                className={`fw-semibold ${count > 2
                                                                    ? "text-danger"
                                                                    : count === 2
                                                                        ? "text-warning"
                                                                        : "text-success"
                                                                    }`}
                                                            >
                                                                {team}
                                                            </span>{" "}
                                                            — {count} match{count !== 1 ? "es" : ""}
                                                        </li>
                                                    ))}
                                            </ul>
                                        </div>
                                    )}

                                    <table
                                        className="table table-dark table-hover table-striped align-middle text-center w-100"
                                        style={{
                                            borderCollapse: "separate",
                                            borderSpacing: "0 6px",
                                        }}
                                    >
                                        <thead className="table-secondary text-dark">
                                            <tr>
                                                <th style={{ width: "5%" }}>#</th>
                                                <th style={{ width: "40%" }}>Team A</th>
                                                <th style={{ width: "40%" }}>Team B</th>
                                                <th style={{ width: "15%" }}>Actions</th>
                                            </tr>
                                        </thead>
                                        <tbody>
                                            {previewMatches.map((m, idx) => (
                                                <tr key={idx}>
                                                    <td>{idx + 1}</td>
                                                    <td className="fw-semibold text-info">{m.team_a}</td>
                                                    <td className="fw-semibold text-warning">{m.team_b}</td>
                                                    <td>
                                                        <div className="d-flex justify-content-center gap-2">
                                                            <button
                                                                className="btn btn-sm btn-outline-danger"
                                                                onClick={() =>
                                                                    setPreviewMatches((prev) =>
                                                                        prev.filter((_, i) => i !== idx)
                                                                    )
                                                                }
                                                            >
                                                                🗑️ Remove
                                                            </button>
                                                        </div>
                                                    </td>
                                                </tr>
                                            ))}
                                        </tbody>
                                    </table>
                                </div>
                            )}

                            {/* 🧨 Clear Week Matchups */}
                            <div className="mt-3">
                                <h6 className="text-danger mb-2">🧨 Clear Week Matchups</h6>
                                <div className="d-flex gap-2 align-items-center" style={{ maxWidth: 300 }}>
                                    <input
                                        type="number"
                                        className="form-control bg-dark text-light"
                                        placeholder="Enter week..."
                                        value={week}
                                        onChange={(e) => setWeek(e.target.value)}
                                    />
                                    <button
                                        className="btn btn-outline-danger btn-sm"
                                        onClick={async () => {
                                            if (!week) return setMsg("⚠️ Enter a week number first.");
                                            if (!confirm(`⚠️ This will permanently delete all matches in Week ${week}. Continue?`))
                                                return;

                                            try {
                                                const res = await axios.post(
                                                    `${urlBase}/api/mod/matches/clear-week`,
                                                    { season: "1", week: week },
                                                    { withCredentials: true }
                                                );
                                                console.log(res.data);
                                                setMsg(`✅ Cleared all matches for Week ${week}`);
                                            } catch (err) {
                                                console.error(err);
                                                setMsg("❌ Failed to clear matches.");
                                            }
                                        }}
                                    >
                                        🗑️ Clear Week
                                    </button>
                                </div>
                            </div>

                            {/* --- Match Admin Tools --- */}
                            <h6>🕒 Select Match (Current Season)</h6>
                            <input
                                type="text"
                                placeholder="Search match..."
                                className="form-control bg-dark text-light mb-2"
                                value={matchSearch}
                                onChange={(e) => setMatchSearch(e.target.value)}
                                style={{ maxWidth: 400 }}
                            />
                            <select
                                className="form-select bg-dark text-light mb-2"
                                value={matchID}
                                onChange={(e) => setMatchID(e.target.value)}
                                style={{ maxWidth: 400 }}
                            >
                                <option value="">Select Match...</option>
                                {filteredMatches.map((m) => (
                                    <option key={m.id} value={m.id}>
                                        {m.match_code}: {m.team_a} vs {m.team_b}
                                    </option>
                                ))}
                            </select>

                            <div className="d-flex flex-wrap gap-2">
                                <button
                                    className="btn btn-outline-light btn-sm"
                                    onClick={handleForceSchedule}
                                >
                                    Force Schedule
                                </button>
                                <button
                                    className="btn btn-outline-danger btn-sm"
                                    onClick={handleForfeit}
                                >
                                    Forfeit
                                </button>
                                <button
                                    className="btn btn-outline-warning btn-sm"
                                    onClick={handleDoubleForfeit}
                                >
                                    Double Forfeit
                                </button>
                                <button
                                    className="btn btn-outline-secondary btn-sm"
                                    onClick={handleResetMatch}
                                >
                                    Reset Match
                                </button>
                                <button
                                    className="btn btn-outline-danger btn-sm"
                                    onClick={handleDeleteMatch}
                                >
                                    Delete Match
                                </button>
                            </div>
                        </>
                    }
                />

                {/* 🧾 SCORE TOOLS */}
                <AccordionItem
                    id="scoreTools"
                    title="🧾 Score Tools"
                    children={
                        <>
                            <input
                                type="text"
                                placeholder="Search match..."
                                className="form-control bg-dark text-light mb-2"
                                value={matchSearch}
                                onChange={(e) => setMatchSearch(e.target.value)}
                                style={{ maxWidth: 400 }}
                            />
                            <select
                                className="form-select bg-dark text-light mb-2"
                                value={matchID}
                                onChange={(e) => setMatchID(e.target.value)}
                                style={{ maxWidth: 400 }}
                            >
                                <option value="">Select Match...</option>
                                {filteredMatches.map((m) => (
                                    <option key={m.id} value={m.id}>
                                        {m.match_code}: {m.team_a} vs {m.team_b}
                                    </option>
                                ))}
                            </select>

                            <div className="d-flex flex-wrap gap-2">
                                <button className="btn btn-outline-primary btn-sm" onClick={handleForceSubmitScores}>Force Submit</button>
                                <button className="btn btn-outline-warning btn-sm" onClick={handleEditScores}>Edit Scores</button>
                                <button className="btn btn-outline-info btn-sm" onClick={handleAdjustRating}>Adjust Rating</button>
                            </div>
                        </>
                    }
                />

                {/* 👥 TEAM TOOLS */}
                <AccordionItem
                    id="teamTools"
                    title="👥 Team Tools"
                    children={
                        <>
                            <input
                                type="text"
                                placeholder="Search team..."
                                className="form-control bg-dark text-light mb-2"
                                value={teamSearch}
                                onChange={(e) => setTeamSearch(e.target.value)}
                                style={{ maxWidth: 300 }}
                            />
                            <select
                                className="form-select bg-dark text-light mb-2"
                                value={teamID}
                                onChange={(e) => setTeamID(e.target.value)}
                                style={{ maxWidth: 300 }}
                            >
                                <option value="">Select Team...</option>
                                {filteredTeams.map((t) => (
                                    <option key={t.id} value={t.id}>
                                        {t.name} (#{t.id})
                                    </option>
                                ))}
                            </select>

                            <div className="d-flex flex-wrap gap-2">
                                <button className="btn btn-outline-info btn-sm" onClick={handleRenameTeam}>Rename</button>
                                <button className="btn btn-outline-danger btn-sm" onClick={handleDisbandTeam}>Disband</button>
                                <button className="btn btn-outline-secondary btn-sm" onClick={handleLockTeam}>Lock / Unlock</button>
                            </div>
                        </>
                    }
                />

                {/* 🚫 PLAYER TOOLS */}
                <AccordionItem
                    id="playerTools"
                    title="🚫 Player Tools"
                    children={
                        <>
                            <div className="mb-2">
                                <label className="form-label text-light small mb-1">
                                    🎮 Player ID (Discord ID or DB ID)
                                </label>
                                <input
                                    className="form-control bg-dark text-light"
                                    placeholder="Enter player ID..."
                                    value={playerID}
                                    onChange={(e) => setPlayerID(e.target.value)}
                                    style={{ maxWidth: 250 }}
                                />
                            </div>

                            <div className="mb-2">
                                <label className="form-label text-light small mb-1">
                                    👥 Search Team (optional — for kick/promotion actions)
                                </label>
                                <input
                                    type="text"
                                    placeholder="Type team name..."
                                    className="form-control bg-dark text-light mb-2"
                                    value={teamSearch}
                                    onChange={(e) => setTeamSearch(e.target.value)}
                                    style={{ maxWidth: 300 }}
                                />
                            </div>

                            <div className="mb-3">
                                <label className="form-label text-light small mb-1">
                                    🔍 Select Team (if needed)
                                </label>
                                <select
                                    className="form-select bg-dark text-light"
                                    value={teamID}
                                    onChange={(e) => setTeamID(e.target.value)}
                                    style={{ maxWidth: 300 }}
                                >
                                    <option value="">Select Team...</option>
                                    {filteredTeams.map((t) => (
                                        <option key={t.id} value={t.id}>
                                            {t.name} (#{t.id})
                                        </option>
                                    ))}
                                </select>
                            </div>

                            <div className="d-flex flex-wrap gap-2">
                                <button className="btn btn-outline-warning btn-sm" onClick={handleKickPlayer}>
                                    🦶 Kick Player
                                </button>
                                <button className="btn btn-outline-danger btn-sm" onClick={handleBanPlayer}>
                                    🚫 Ban Player
                                </button>
                                <button className="btn btn-outline-success btn-sm" onClick={handleUnbanPlayer}>
                                    ✅ Unban Player
                                </button>
                            </div>
                        </>
                    }
                />

                {/* 📦 DATA TOOLS */}
                <AccordionItem
                    id="dataTools"
                    title="📦 Data Tools"
                    children={
                        <div className="d-flex flex-wrap gap-2">
                            <button className="btn btn-outline-secondary btn-sm" onClick={handleArchiveSeason}>Archive Season</button>
                            <button className="btn btn-outline-warning btn-sm" onClick={handleResetLeaderboard}>Reset Leaderboard</button>
                            <button className="btn btn-outline-success btn-sm" onClick={handleSyncHistory}>Sync Player History</button>
                            <button className="btn btn-outline-info btn-sm" onClick={handleRebuildELO}>Rebuild ELO</button>
                        </div>
                    }
                />
            </div>
        </div>
    );
}

// --- Accordion Helper ---
function AccordionItem({ id, title, children }) {
    return (
        <div className="accordion-item bg-dark text-light border-secondary">
            <h2 className="accordion-header" id={`${id}-header`}>
                <button
                    className="accordion-button collapsed bg-dark text-light"
                    type="button"
                    data-bs-toggle="collapse"
                    data-bs-target={`#${id}`}
                >
                    {title}
                </button>
            </h2>
            <div id={id} className="accordion-collapse collapse" data-bs-parent="#modAccordion">
                <div className="accordion-body">{children}</div>
            </div>
        </div>
    );
}
