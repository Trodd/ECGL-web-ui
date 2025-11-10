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
    const [previewSeed, setPreviewSeed] = useState(null);
    const [mapScores, setMapScores] = useState({
        map1a: "",
        map1b: "",
        map2a: "",
        map2b: "",
        map3a: "",
        map3b: "",
        mode1: "",
        mode2: "",
        mode3: "",
    });
    const [newTeamName, setNewTeamName] = useState("");

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

    // --- Load Teams (reusable) ---
    async function loadTeams() {
        try {
            const res = await axios.get(`${urlBase}/api/teams`, { withCredentials: true });
            setTeams(Array.isArray(res.data) ? res.data : []);
            console.log("✅ Refreshed team list");
        } catch (err) {
            console.error("❌ Failed to load teams:", err);
            setMsg("⚠️ Failed to refresh teams.");
        }
    }

    // --- Helpers ---
    const getTeamLabel = (id) => {
        const t = teams.find((x) => x.id === parseInt(id));
        return t ? `${t.name} (#${t.id})` : `Team #${id}`;
    };

    const getMatchLabel = (id) => {
        const m = matches.find((x) => x.id === parseInt(id));
        return m ? `${m.match_code}: ${m.team_a} vs ${m.team_b}` : `Match #${id}`;
    };

    async function safePost(endpoint, payload, successMsg, shouldReload = false) {
        try {
            const res = await axios.post(`${urlBase}${endpoint}`, payload, {
                withCredentials: true,
            });
            setMsg(`✅ ${successMsg}`);
            console.log(res.data);

            // 🔁 Refresh team list after certain mod actions
            if (shouldReload) await loadTeams();
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
                setPreviewSeed(res.data.seed || null); // ✅ save deterministic seed
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
        if (!previewWeek || previewMatches.length === 0)
            return setMsg("⚠️ Preview or edit matches before publishing.");

        await safePost(
            "/api/mod/matches/generate",
            { week: parseInt(previewWeek), matches: previewMatches },
            `Published ${previewMatches.length} matches for Week ${previewWeek}`
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
            { team_id: parseInt(teamID), new_name: newTeamName.trim() },
            `Renamed ${getTeamLabel(teamID)} to ${newTeamName.trim()}`,
            true
        );
    };

    const handleDisbandTeam = async () => {
        if (!teamID) return setMsg("⚠️ Select a team first.");
        await safePost(
            "/api/mod/team/disband",
            { team_id: parseInt(teamID) },
            `Disbanded ${getTeamLabel(teamID)}`,
            true
        );
    };

    const handleLockTeam = async () => {
        if (!teamID) return setMsg("⚠️ Select a team first.");
        await safePost(
            "/api/mod/team/lock",
            { team_id: parseInt(teamID) },
            `Toggled lock for ${getTeamLabel(teamID)}`,
            true
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
                {/* 🗓️ WEEKLY MATCH MANAGEMENT */}
                <AccordionItem
                    id="weeklyMatches"
                    title="🗓️ Weekly Match Management"
                    children={
                        <>
                            <h6 className="mb-3">📅 Generate or Clear Weekly Matchups</h6>

                            {/* --- Generate Weekly Matches --- */}
                            <div
                                className="p-3 mb-4 rounded"
                                style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                            >
                                <h6 className="text-success mb-2">⚙️ Generate Weekly Matches</h6>
                                <div
                                    className="d-flex gap-2 align-items-center"
                                    style={{ maxWidth: 320 }}
                                >
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

                                {/* --- Preview Section (Full Editing) --- */}
                                {showPreview && previewMatches.length > 0 && (
                                    <div
                                        className="bg-dark p-3 rounded shadow-sm mt-3"
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

                                        {/* 🔹 Add Custom Matchup */}
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
                                                            const newCode = `${previewSeason || "1"}-Week${previewWeek}-M${String(
                                                                nextNumber
                                                            ).padStart(3, "0")}`;
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
                                                <h6 className="text-info mb-2">⚙️ Team Match Counts</h6>
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

                                        {/* 🔹 Editable Match Table */}
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
                            </div>
                            {/* --- Clear Week Matchups --- */}
                            <div
                                className="p-3 rounded"
                                style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                            >
                                <h6 className="text-danger mb-2">🧹 Clear Week Matchups</h6>
                                <div
                                    className="d-flex gap-2 align-items-center"
                                    style={{ maxWidth: 320 }}
                                >
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
                                            if (
                                                !confirm(
                                                    `⚠️ This will permanently delete all matches in Week ${week}. Continue?`
                                                )
                                            )
                                                return;

                                            try {
                                                const res = await axios.post(
                                                    `${urlBase}/api/mod/matches/clear-week`,
                                                    { season: "1", week },
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
                            {/* --- Add Additional Matchup (Current Week) --- */}
                            <div
                                className="p-3 mt-4 rounded"
                                style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                            >
                                <h6 className="text-info mb-2">➕ Add Additional Matchup (Current Week)</h6>
                                <p className="small text-light mb-2">
                                    Select two teams to add a new matchup for the active week.
                                </p>

                                <div className="d-flex flex-wrap align-items-center gap-2 mb-2">
                                    <select
                                        className="form-select bg-dark text-light"
                                        value={newMatchA}
                                        onChange={(e) => setNewMatchA(e.target.value)}
                                        style={{ maxWidth: 220 }}
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
                                        style={{ maxWidth: 220 }}
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
                                        disabled={!newMatchA || !newMatchB || newMatchA === newMatchB}
                                        onClick={async () => {
                                            if (!newMatchA || !newMatchB || newMatchA === newMatchB) {
                                                setMsg("⚠️ Please select two different teams.");
                                                return;
                                            }

                                            try {
                                                const res = await axios.post(
                                                    `${urlBase}/api/mod/match/add`,
                                                    { team_a: newMatchA, team_b: newMatchB },
                                                    { withCredentials: true }
                                                );
                                                setMsg(`✅ Added ${res.data.match_code} successfully.`);
                                                setNewMatchA("");
                                                setNewMatchB("");

                                                // Refresh matches list
                                                const matchRes = await axios.get(`${urlBase}/api/matches/public`, {
                                                    withCredentials: true,
                                                });
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
                                                console.error("❌ Failed to add match:", err);
                                                setMsg("❌ Failed to add additional match.");
                                            }
                                        }}
                                    >
                                        ➕ Add Match
                                    </button>
                                </div>
                            </div>
                        </>
                    }
                />
                {/* 🏁 MATCH TOOLS */}
                <AccordionItem
                    id="matchTools"
                    title="🏁 Match Tools"
                    children={
                        <>
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
                                className="form-select bg-dark text-light mb-3"
                                value={matchID}
                                onChange={(e) => {
                                    setMatchID(e.target.value);
                                    setTeamID(""); // ✅ auto-reset winner when match changes
                                }}
                                style={{ maxWidth: 400 }}
                            >
                                <option value="">Select Match...</option>
                                {filteredMatches.map((m) => (
                                    <option key={m.id} value={m.id}>
                                        {m.match_code}: {m.team_a} vs {m.team_b}
                                    </option>
                                ))}
                            </select>

                            {/* --- Forfeit Tools --- */}
                            <div
                                className="p-3 mb-3 rounded"
                                style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                            >
                                <h6 className="text-danger mb-2">🏳️ Forfeit Controls</h6>
                                <p className="small text-light mb-2">
                                    Select the winning team for this match, then click <b>Forfeit</b>.
                                </p>

                                <div className="d-flex flex-wrap align-items-center gap-2 mb-2">
                                    {/* Winning Team Select – filtered to only the 2 teams from the chosen match */}
                                    <select
                                        className="form-select bg-dark text-light"
                                        value={teamID}
                                        onChange={(e) => setTeamID(e.target.value)}
                                        style={{ maxWidth: 300 }}
                                        disabled={!matchID}
                                    >
                                        <option value="">
                                            {matchID ? "Select Winning Team..." : "Select a match first..."}
                                        </option>
                                        {(() => {
                                            const match = matches.find((m) => m.id === parseInt(matchID));
                                            if (!match) return null;

                                            // Only show the two teams from this match
                                            return [match.team_a, match.team_b].map((teamName) => {
                                                const team = teams.find(
                                                    (t) => t.name.toLowerCase() === teamName.toLowerCase()
                                                );
                                                if (!team) return null;
                                                return (
                                                    <option key={team.id} value={team.id}>
                                                        {team.name} (#{team.id})
                                                    </option>
                                                );
                                            });
                                        })()}
                                    </select>

                                    <button
                                        className="btn btn-outline-danger btn-sm"
                                        disabled={!matchID || !teamID}
                                        onClick={async () => {
                                            if (!matchID || !teamID) {
                                                setMsg("⚠️ Select both a match and a winning team first.");
                                                return;
                                            }

                                            await safePost(
                                                "/api/mod/match/forfeit",
                                                {
                                                    match_id: parseInt(matchID),
                                                    winner_team_id: parseInt(teamID),
                                                },
                                                `Applied forfeit for ${getMatchLabel(matchID)} → Winner: ${getTeamLabel(
                                                    teamID
                                                )}`
                                            );
                                        }}
                                    >
                                        🏳️ Forfeit
                                    </button>
                                </div>
                            </div>

                            {/* --- Other Match Actions --- */}
                            <div className="d-flex flex-wrap gap-2">
                                <button
                                    className="btn btn-outline-light btn-sm"
                                    onClick={handleForceSchedule}
                                >
                                    Force Schedule
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
                                className="form-select bg-dark text-light mb-3"
                                value={matchID}
                                onChange={(e) => {
                                    setMatchID(e.target.value);
                                    setMapScores({ map1a: "", map1b: "", map2a: "", map2b: "", map3a: "", map3b: "" });
                                }}
                                style={{ maxWidth: 400 }}
                            >
                                <option value="">Select Match...</option>
                                {filteredMatches.map((m) => (
                                    <option key={m.id} value={m.id}>
                                        {m.match_code}: {m.team_a} vs {m.team_b}
                                    </option>
                                ))}
                            </select>

                            {(() => {
                                const match = matches.find((m) => m.id === parseInt(matchID));
                                if (!match) return null;

                                return (
                                    <div
                                        className="p-3 mb-3 rounded"
                                        style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                                    >
                                        <h6 className="text-info mb-3">🧾 Enter Map Scores</h6>

                                        <table className="table table-dark table-striped align-middle text-center mb-3" style={{ maxWidth: 500 }}>
                                            <thead>
                                                <tr>
                                                    <th>Map</th>
                                                    <th>Gamemode</th>
                                                    <th>{match.team_a}</th>
                                                    <th>{match.team_b}</th>
                                                </tr>
                                            </thead>
                                            <tbody>
                                                {[1, 2, 3].map((mapNum) => (
                                                    <tr key={mapNum}>
                                                        <td className="text-secondary">Map {mapNum}</td>
                                                        <td>
                                                            <select
                                                                className="form-select bg-dark text-light"
                                                                value={mapScores[`mode${mapNum}`] || ""}
                                                                onChange={(e) =>
                                                                    setMapScores((prev) => ({ ...prev, [`mode${mapNum}`]: e.target.value }))
                                                                }
                                                            >
                                                                <option value="">Gamemode...</option>
                                                                <option value="Payload">Payload</option>
                                                                <option value="Capture Point">Capture Point</option>
                                                            </select>
                                                        </td>
                                                        <td>
                                                            <input
                                                                type="number"
                                                                className="form-control bg-dark text-light text-center"
                                                                value={mapScores[`map${mapNum}a`] || ""}
                                                                onChange={(e) =>
                                                                    setMapScores((prev) => ({ ...prev, [`map${mapNum}a`]: e.target.value }))
                                                                }
                                                            />
                                                        </td>
                                                        <td>
                                                            <input
                                                                type="number"
                                                                className="form-control bg-dark text-light text-center"
                                                                value={mapScores[`map${mapNum}b`] || ""}
                                                                onChange={(e) =>
                                                                    setMapScores((prev) => ({ ...prev, [`map${mapNum}b`]: e.target.value }))
                                                                }
                                                            />
                                                        </td>
                                                    </tr>
                                                ))}
                                            </tbody>
                                        </table>

                                        <div className="d-flex flex-wrap gap-2">
                                            <button
                                                className="btn btn-outline-success btn-sm"
                                                disabled={
                                                    !Object.values(mapScores).some((v) => v && v !== "")
                                                }
                                                onClick={async () => {
                                                    try {
                                                        const payload = {
                                                            match_id: parseInt(matchID),
                                                            maps: [
                                                                {
                                                                    map: 1,
                                                                    mode: mapScores.mode1 || "",
                                                                    team_a_score: parseInt(mapScores.map1a || 0),
                                                                    team_b_score: parseInt(mapScores.map1b || 0),
                                                                },
                                                                {
                                                                    map: 2,
                                                                    mode: mapScores.mode2 || "",
                                                                    team_a_score: parseInt(mapScores.map2a || 0),
                                                                    team_b_score: parseInt(mapScores.map2b || 0),
                                                                },
                                                                {
                                                                    map: 3,
                                                                    mode: mapScores.mode3 || "",
                                                                    team_a_score: parseInt(mapScores.map3a || 0),
                                                                    team_b_score: parseInt(mapScores.map3b || 0),
                                                                },
                                                            ],
                                                        };


                                                        await axios.post(`${urlBase}/api/mod/match/set-maps`, payload, { withCredentials: true });

                                                        setMsg(
                                                            `✅ Saved map scores for ${match.match_code}: ${match.team_a} vs ${match.team_b}`
                                                        );
                                                        setMapScores({ map1a: "", map1b: "", map2a: "", map2b: "", map3a: "", map3b: "" });
                                                    } catch (err) {
                                                        console.error("❌ Failed to save map scores:", err);
                                                        setMsg("❌ Failed to save map scores.");
                                                    }
                                                }}
                                            >
                                                💾 Save Map Scores
                                            </button>

                                            <button
                                                className="btn btn-outline-warning btn-sm"
                                                disabled={!matchID}
                                                onClick={handleEditScores}
                                            >
                                                ✏️ Edit Scores
                                            </button>

                                            <button
                                                className="btn btn-outline-info btn-sm"
                                                disabled={!matchID}
                                                onClick={handleAdjustRating}
                                            >
                                                ⚖️ Adjust Rating
                                            </button>
                                        </div>
                                    </div>
                                );
                            })()}
                        </>
                    }
                />

                {/* 👥 TEAM TOOLS */}
                <AccordionItem
                    id="teamTools"
                    title="👥 Team Tools"
                    children={
                        <>
                            {/* 🔍 Search for a team */}
                            <label className="form-label text-light small mb-1">
                                🔍 Search for Team
                            </label>
                            <input
                                type="text"
                                placeholder="Type to search teams..."
                                className="form-control bg-dark text-light mb-2"
                                value={teamSearch}
                                onChange={(e) => setTeamSearch(e.target.value)}
                                style={{ maxWidth: 300 }}
                            />

                            {/* 🧩 Select Team */}
                            <label className="form-label text-light small mb-1">
                                🧩 Select Team
                            </label>
                            <select
                                className="form-select bg-dark text-light mb-3"
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

                            {/* ✏️ Rename Team */}
                            <label className="form-label text-light small mb-1">
                                ✏️ New Team Name
                            </label>
                            <input
                                type="text"
                                placeholder="Enter new team name..."
                                className="form-control bg-dark text-light mb-3"
                                value={newTeamName}
                                onChange={(e) => setNewTeamName(e.target.value)}
                                style={{ maxWidth: 300 }}
                            />

                            {/* 🧰 Team Management Buttons */}
                            <div className="d-flex flex-wrap gap-2 mb-3">
                                <button
                                    className="btn btn-outline-info btn-sm"
                                    onClick={async () => {
                                        if (!teamID) return setMsg("⚠️ Select a team first.");
                                        if (!newTeamName.trim())
                                            return setMsg("⚠️ Enter a new name before renaming.");
                                        await safePost(
                                            "/api/mod/team/rename",
                                            { team_id: parseInt(teamID), new_name: newTeamName.trim() },
                                            `Renamed ${getTeamLabel(teamID)} to ${newTeamName.trim()}`,
                                            true
                                        );
                                    }}
                                >
                                    ✏️ Rename
                                </button>

                                <button
                                    className="btn btn-outline-danger btn-sm"
                                    onClick={async () => {
                                        if (!teamID) return setMsg("⚠️ Select a team first.");
                                        if (
                                            !confirm(
                                                `⚠️ Are you sure you want to disband ${getTeamLabel(teamID)}?`
                                            )
                                        )
                                            return;
                                        await safePost(
                                            "/api/mod/team/disband",
                                            { team_id: parseInt(teamID) },
                                            `Disbanded ${getTeamLabel(teamID)}`,
                                            true
                                        );
                                    }}
                                >
                                    🚫 Disband
                                </button>

                                <button
                                    className="btn btn-outline-secondary btn-sm"
                                    onClick={async () => {
                                        if (!teamID) return setMsg("⚠️ Select a team first.");
                                        await safePost(
                                            "/api/mod/team/lock",
                                            { team_id: parseInt(teamID) },
                                            `Toggled lock for ${getTeamLabel(teamID)}`,
                                            true
                                        );
                                    }}
                                >
                                    🔒 Lock / Unlock
                                </button>

                                <button
                                    className="btn btn-outline-danger btn-sm"
                                    onClick={async () => {
                                        if (!teamID) return setMsg("⚠️ Select a team first.");
                                        if (
                                            !confirm(
                                                `⚠️ This will permanently delete ${getTeamLabel(
                                                    teamID
                                                )} and all related matches. Continue?`
                                            )
                                        )
                                            return;
                                        await safePost(
                                            "/api/mod/team/delete",
                                            { team_id: parseInt(teamID) },
                                            `Deleted ${getTeamLabel(teamID)}`,
                                            true
                                        );
                                    }}
                                >
                                    🗑️ Delete Team
                                </button>
                            </div>

                            {/* ⚙️ Inactive Controls */}
                            <div
                                className="p-3 rounded"
                                style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                            >
                                <h6 className="text-warning mb-2">⚙️ Inactive Controls</h6>
                                <p className="text-light small mb-2">
                                    Mark one or all teams as inactive for the current season.
                                </p>
                                <div className="d-flex flex-wrap gap-2 mb-2">
                                    <button
                                        className="btn btn-outline-warning btn-sm"
                                        onClick={async () => {
                                            if (!teamID) return setMsg("⚠️ Select a team first.");
                                            await safePost(
                                                "/api/mod/team/set-inactive",
                                                { team_id: parseInt(teamID) },
                                                `Set ${getTeamLabel(teamID)} to Inactive`,
                                                true
                                            );
                                        }}
                                    >
                                        ⚠️ Set Selected Team Inactive
                                    </button>

                                    <button
                                        className="btn btn-outline-danger btn-sm"
                                        onClick={async () => {
                                            if (
                                                !confirm("⚠️ This will mark ALL teams as Inactive. Continue?")
                                            )
                                                return;
                                            await safePost(
                                                "/api/mod/teams/set-all-inactive",
                                                {},
                                                "Set all teams to Inactive",
                                                true
                                            );
                                        }}
                                    >
                                        🧨 Set ALL Teams Inactive
                                    </button>
                                </div>
                            </div>
                            {/* 🔒 Roster Lock Controls */}
                            <div
                                className="p-3 mt-3 rounded"
                                style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                            >
                                <h6 className="text-info mb-2">🔒 Roster Lock Controls</h6>
                                <p className="text-light small mb-2">
                                    Locking rosters prevents all players from <b>joining</b> or <b>creating</b> teams.
                                </p>

                                <div className="d-flex flex-wrap gap-2">
                                    <button
                                        className="btn btn-outline-warning btn-sm"
                                        onClick={async () => {
                                            if (!confirm("⚠️ Lock all rosters? This will block all join/create team actions."))
                                                return;
                                            await safePost(
                                                "/api/mod/roster/lock-all",
                                                {},
                                                "All rosters locked"
                                            );
                                        }}
                                    >
                                        🔒 Lock All Rosters
                                    </button>

                                    <button
                                        className="btn btn-outline-success btn-sm"
                                        onClick={async () => {
                                            if (!confirm("✅ Unlock all rosters and allow joins/creations?")) return;
                                            await safePost(
                                                "/api/mod/roster/unlock-all",
                                                {},
                                                "All rosters unlocked"
                                            );
                                        }}
                                    >
                                        🔓 Unlock All Rosters
                                    </button>
                                </div>
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
                {/* 📜 TEAM HISTORY */}
                <AccordionItem
                    id="teamHistory"
                    title="📜 Team Rename Logs"
                    children={
                        <TeamRenameHistory urlBase={urlBase} />
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

function TeamRenameHistory({ urlBase }) {
    const [logs, setLogs] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [teamFilter, setTeamFilter] = useState("");

    useEffect(() => {
        async function loadLogs() {
            try {
                setLoading(true);
                const url = teamFilter
                    ? `${urlBase}/api/mod/team/history?team_id=${teamFilter}`
                    : `${urlBase}/api/mod/team/history`;
                const res = await axios.get(url, { withCredentials: true });
                setLogs(res.data?.history || []);
                setError("");
            } catch (err) {
                console.error("❌ Failed to fetch rename history:", err);
                setError("Failed to load rename history.");
            } finally {
                setLoading(false);
            }
        }
        loadLogs();
    }, [teamFilter, urlBase]);

    return (
        <div>
            <h6 className="text-light mb-3">📜 Team Rename Logs</h6>

            <div className="d-flex flex-wrap align-items-center gap-2 mb-3">
                <input
                    type="number"
                    className="form-control bg-dark text-light"
                    placeholder="Filter by Team ID..."
                    style={{ maxWidth: 180 }}
                    value={teamFilter}
                    onChange={(e) => setTeamFilter(e.target.value)}
                />
                <button
                    className="btn btn-outline-secondary btn-sm"
                    onClick={() => setTeamFilter("")}
                >
                    Clear Filter
                </button>
            </div>

            {loading ? (
                <p className="text-light">Loading rename logs...</p>
            ) : error ? (
                <div className="alert alert-danger">{error}</div>
            ) : logs.length === 0 ? (
                <p className="text-muted">No rename logs found.</p>
            ) : (
                <div className="table-responsive">
                    <table className="table table-dark table-striped align-middle text-center">
                        <thead>
                            <tr>
                                <th>#</th>
                                <th>Team ID</th>
                                <th>Old Name</th>
                                <th>New Name</th>
                                <th>Changed By</th>
                                <th>Date</th>
                            </tr>
                        </thead>
                        <tbody>
                            {logs.map((log, idx) => (
                                <tr key={log.id}>
                                    <td>{idx + 1}</td>
                                    <td>{log.team_id}</td>
                                    <td className="text-danger">{log.old_name}</td>
                                    <td className="text-success">{log.new_name}</td>
                                    <td className="text-info">
                                        {log.changer
                                            ? `${log.changer} (${log.changed_by})`
                                            : log.changed_by}
                                    </td>
                                    <td className="text-muted">
                                        {new Date(log.changed_at).toLocaleString([], {
                                            dateStyle: "medium",
                                            timeStyle: "short",
                                        })}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}

