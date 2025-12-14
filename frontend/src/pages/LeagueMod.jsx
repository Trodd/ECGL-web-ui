import { useState, useEffect } from "react";
import axios from "axios";
import FinalsBracket from "../components/FinalsBracket";
import FinalsSeedEditor from "../components/FinalsSeedEditor";

export default function LeagueMod() {
    const urlBase = import.meta.env.VITE_API_URL;
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
    const [addPlayerID, setAddPlayerID] = useState("");
    const [removePlayerID, setRemovePlayerID] = useState("");
    const [addRole, setAddRole] = useState("Member");
    const [addPlayerInput, setAddPlayerInput] = useState("");
    const [filteredPlayers, setFilteredPlayers] = useState([]);
    const [playerSearch, setPlayerSearch] = useState("");
    const [playerSuggestions, setPlayerSuggestions] = useState([]);
    const [openId, setOpenId] = useState(null);
    const [menuPos, setMenuPos] = useState({ top: 0, left: 0 });
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
    const [newRating, setNewRating] = useState("");
    const [newWins, setNewWins] = useState("");
    const [newLosses, setNewLosses] = useState("");
    const [newGP, setNewGP] = useState("");
    const [players, setPlayers] = useState([]);
    const [teamMembers, setTeamMembers] = useState([]);
    // Player Rating Editor State
    const [statSearch, setStatSearch] = useState("");
    const [statPlayerID, setStatPlayerID] = useState("");
    const [statSuggestions, setStatSuggestions] = useState([]);

    const [pRating, setPRating] = useState("");
    const [pWins, setPWins] = useState("");
    const [pLosses, setPLosses] = useState("");
    const [pMatches, setPMatches] = useState("");

    // --- Stats tab ---
    const [teamRating, setTeamRating] = useState("");
    const [teamWins, setTeamWins] = useState("");
    const [teamLosses, setTeamLosses] = useState("");
    const [teamGames, setTeamGames] = useState("");
    const [activeTab, setActiveTab] = useState("info");
    const isRole = (m, role) =>
        m.role?.toLowerCase() === role.toLowerCase();

    // Finals data
    const [finalsTeams, setFinalsTeams] = useState([]);
    const [finalsBracket, setFinalsBracket] = useState(null);
    const [finalsVisible, setFinalsVisible] = useState(false);

    // Load on mount
    useEffect(() => {
        axios.get(`${urlBase}/api/finals/visible`)
            .then(res => setFinalsVisible(res.data.visible))
            .catch(() => { });
    }, []);

    useEffect(() => {
        async function loadPlayers() {
            try {
                const res = await axios.get(`${urlBase}/api/players`, { withCredentials: true });
                setPlayers(Array.isArray(res.data) ? res.data : []);
            } catch (err) {
                console.error("❌ Failed to load players:", err);
                setPlayers([]);
            }
        }
        loadPlayers();
    }, []);

    useEffect(() => {
        async function loadStats() {
            if (!playerID) return;

            try {
                const res = await axios.get(`${urlBase}/api/mod/player/stats?id=${playerID}`, {
                    withCredentials: true,
                });

                const d = res.data;

                // Auto-fill the stat editor
                setPRating(d.rating ?? 0);
                setPWins(d.wins ?? 0);
                setPLosses(d.losses ?? 0);
                setPMatches(d.matches ?? 0);

            } catch (err) {
                console.error("❌ Failed to load player stats:", err);
                setMsg("⚠️ Could not load player stats.");
            }
        }

        loadStats();
    }, [playerID]);

    useEffect(() => {
        async function loadTeamStats() {
            if (!teamID) return;

            try {
                const res = await axios.get(
                    `${urlBase}/api/mod/team/stats?id=${teamID}`,
                    { withCredentials: true }
                );

                const d = res.data;
                console.log("Loaded team stats:", d);

                setNewRating(d.rating ?? "");
                setNewWins(d.wins ?? "");
                setNewLosses(d.losses ?? "");
                setNewGP(d.matches ?? "");

            } catch (err) {
                console.error("❌ Failed to load team stats:", err);
                setMsg("⚠️ Could not load team stats.");
            }
        }

        loadTeamStats();
    }, [teamID]);

    useEffect(() => {
        async function loadMembers() {
            if (!teamID) {
                setTeamMembers([]);
                return;
            }

            try {
                const res = await axios.get(`${urlBase}/api/mod/team/members?id=${teamID}`, {
                    withCredentials: true,
                });
                setTeamMembers(res.data || []);
            } catch (err) {
                console.error("Failed to load team members:", err);
                setTeamMembers([]);
            }
        }

        loadMembers();
    }, [teamID]);

    // Search filters
    const [teamSearch, setTeamSearch] = useState("");
    const [matchSearch, setMatchSearch] = useState("");

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
                loadFinals();
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

    async function loadFinals() {
        try {
            const [teamsRes, bracketRes] = await Promise.all([
                axios.get(`${urlBase}/api/finals/teams`, { withCredentials: true }),
                axios.get(`${urlBase}/api/finals/bracket`, { withCredentials: true })
            ]);

            setFinalsTeams(teamsRes.data || []);
            setFinalsBracket(bracketRes.data || null);

        } catch (err) {
            console.error("❌ Failed to load finals:", err);
        }
    }

    // --- Helpers ---
    const HandleModPromoteToCaptain = async (playerID, name) => {
        await safePost(
            "/api/mod/team/promote-captain",
            {
                team_id: parseInt(teamID),
                player_id: String(playerID),
            },
            `Promoted ${name} to Captain`,
            true
        );
    };

    const handleSetRole = async (playerID, role, name) => {
        await safePost(
            "/api/mod/team/set-role",
            {
                team_id: parseInt(teamID),
                player_id: String(playerID),
                role,
            },
            `Set ${name}'s role to ${role}`,
            true
        );
    };

    const handleKickMember = async (playerID, name) => {
        await safePost(
            "/api/mod/player/kick",
            {
                team_id: parseInt(teamID),
                player_id: String(playerID),
            },
            `Kicked ${name} from ${getTeamLabel(teamID)}`,
            true
        );
    };

    const handleAddPlayer = async () => {
        if (!teamID) return setMsg("⚠️ Select a team first.");
        if (!addPlayerID) return setMsg("⚠️ Select a player first.");

        await safePost(
            "/api/mod/team/add-player",
            {
                team_id: parseInt(teamID),
                player_id: String(addPlayerID),
                role: addRole,
            },
            `Added ${addPlayerInput} as ${addRole}`,
            true
        );

        setAddPlayerInput("");
        setAddPlayerID("");
        setFilteredPlayers([]);
    };

    const handlePlayerSearchForAdd = async (e) => {
        const value = e.target.value;
        setAddPlayerInput(value);
        setAddPlayerID("");

        if (!value || value.trim().length < 2) {
            setFilteredPlayers([]);
            return;
        }

        try {
            const res = await axios.get(`${urlBase}/api/players`);
            const list = res.data || [];

            const filtered = list.filter((p) =>
                [p.username, p.display_name, String(p.id)]
                    .join(" ")
                    .toLowerCase()
                    .includes(value.toLowerCase())
            );

            setFilteredPlayers(filtered.slice(0, 8));
        } catch {
            setFilteredPlayers([]);
        }
    };

    const handleAdjustTeamStats = async () => {
        if (!teamID) return setMsg("⚠️ Select a team first.");

        await safePost(
            "/api/mod/team/adjust-stats",
            {
                team_id: parseInt(teamID),
                rating: parseInt(newRating || 0),
                wins: parseInt(newWins || 0),
                losses: parseInt(newLosses || 0),
                matches: parseInt(newGP || 0),
            },
            `Updated stats for ${getTeamLabel(teamID)}`,
            true
        );

        setNewRating("");
        setNewWins("");
        setNewLosses("");
        setNewGP("");
    };

    const handleAdjustPlayerStats = async () => {
        if (!playerID) return setMsg("⚠️ Select a player first.");

        await safePost(
            "/api/mod/player/adjust-stats",
            {
                player_id: String(playerID),
                rating: parseInt(pRating || 0),
                wins: parseInt(pWins || 0),
                losses: parseInt(pLosses || 0),
                matches: parseInt(pMatches || 0),
            },
            `Updated stats for player #${playerID}`,
            true
        );

        setPRating("");
        setPWins("");
        setPLosses("");
        setPMatches("");
    };

    const handleResetChallenges = async () => {
        if (!teamID) return;

        try {
            const res = await axios.post(
                `${import.meta.env.VITE_API_URL}/api/team/reset_challenges`,
                { team_id: teamID }
            );

            if (res.data.success) {
                alert("Team’s challenge matches have been reset!");
            } else {
                alert("Failed to reset challenges.");
            }
        } catch (err) {
            console.error(err);
            alert("Error resetting challenges.");
        }
    };

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

    const resetPlayerLeaderboard = async () => {
        try {
            const res = await axios.post(
                `${urlBase}/api/mod/reset_player_leaderboard`,
                {},
                { withCredentials: true }
            );
            alert(res.data.message || "Player leaderboard reset!");
        } catch (err) {
            console.error("Reset player leaderboard failed:", err);
            alert("Failed to reset player leaderboard");
        }
    };

    // ===========================================================
    // RENDER
    // ===========================================================
    return (
        <div className="league-mod-root">
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
                                <div
                                    className="p-3 rounded mb-3"
                                    style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                                >
                                    <h5 className="text-info mb-2">🎯 Match Selector</h5>
                                    <p className="small text-light mb-2">
                                        Search and select a match to manage administrative actions.
                                    </p>

                                    <input
                                        type="text"
                                        placeholder="🔍 Search match..."
                                        className="form-control bg-dark text-light mb-2"
                                        value={matchSearch}
                                        onChange={(e) => setMatchSearch(e.target.value)}
                                        style={{ maxWidth: 450 }}
                                    />

                                    <select
                                        className="form-select bg-dark text-light mb-2"
                                        value={matchID}
                                        onChange={(e) => {
                                            setMatchID(e.target.value);
                                            setTeamID("");
                                        }}
                                        style={{ maxWidth: 450 }}
                                    >
                                        <option value="">Select Match...</option>
                                        {filteredMatches.map((m) => (
                                            <option key={m.id} value={m.id}>
                                                {m.match_code}: {m.team_a} vs {m.team_b}
                                            </option>
                                        ))}
                                    </select>

                                    {!matchID && (
                                        <p className="small text-warning mt-1">⚠️ Please select a match first.</p>
                                    )}
                                </div>

                                {/* --- FORFEIT TOOLS CARD --- */}
                                <div
                                    className="p-3 rounded mb-3"
                                    style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                                >
                                    <h5 className="text-danger mb-2">🏳️ Forfeit Controls</h5>
                                    <p className="small text-light mb-3">
                                        Choose which team wins by forfeit. Only the two teams involved are shown.
                                    </p>

                                    <div className="d-flex flex-column flex-md-row align-items-start gap-2">
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

                                                return [match.team_a, match.team_b].map((teamName) => {
                                                    const t = teams.find(
                                                        (x) => x.name.toLowerCase() === teamName.toLowerCase()
                                                    );
                                                    if (!t) return null;

                                                    return (
                                                        <option key={t.id} value={t.id}>
                                                            {t.name} (#{t.id})
                                                        </option>
                                                    );
                                                });
                                            })()}
                                        </select>

                                        <button
                                            className="btn btn-outline-danger btn-sm"
                                            disabled={!matchID || !teamID}
                                            onClick={async () => {
                                                await safePost(
                                                    "/api/mod/match/forfeit",
                                                    {
                                                        match_id: parseInt(matchID),
                                                        winner_team_id: parseInt(teamID),
                                                    },
                                                    `Forfeit applied → Winner: ${getTeamLabel(teamID)}`,
                                                    true
                                                );
                                            }}
                                        >
                                            🏳️ Apply Forfeit
                                        </button>
                                    </div>
                                </div>

                                {/* --- OTHER ACTIONS CARD --- */}
                                <div
                                    className="p-3 rounded mb-3"
                                    style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                                >
                                    <h5 className="text-warning mb-3">⚙️ Match Actions</h5>

                                    <div className="d-flex flex-wrap gap-2">

                                        {/* RESET SCHEDULE Button (NEW) */}
                                        <button
                                            className="btn btn-outline-info btn-sm"
                                            disabled={!matchID}
                                            onClick={async () => {
                                                await safePost(
                                                    "/api/mod/match/reset-schedule",
                                                    { match_id: parseInt(matchID) },
                                                    `Reset schedule for ${getMatchLabel(matchID)}`,
                                                    true
                                                );
                                            }}
                                        >
                                            🔄 Reset Schedule
                                        </button>

                                        <button
                                            className="btn btn-outline-warning btn-sm"
                                            disabled={!matchID}
                                            onClick={handleDoubleForfeit}
                                        >
                                            ⚠️ Double Forfeit
                                        </button>

                                        <button
                                            className="btn btn-outline-secondary btn-sm"
                                            disabled={!matchID}
                                            onClick={handleResetMatch}
                                        >
                                            ♻️ Reset Scores
                                        </button>

                                        <button
                                            className="btn btn-outline-danger btn-sm"
                                            disabled={!matchID}
                                            onClick={handleDeleteMatch}
                                        >
                                            🗑️ Delete Match
                                        </button>
                                    </div>
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


                                                            await axios.post(`${urlBase}/mod/match/set-maps`, payload, { withCredentials: true });

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
                            <div className="bg-black text-light p-3 rounded-3 border border-secondary">
                                {/* 🔧 TEAM TOOLS — TABBED SYSTEM */}
                                <div className="bg-black text-light p-3 rounded border border-secondary">

                                    {/* === TEAM SELECTOR === */}
                                    <h5 className="text-info mb-3">🧩 Team Tools</h5>

                                    <div className="d-flex flex-column flex-md-row gap-2 mb-4">
                                        <input
                                            type="text"
                                            placeholder="🔍 Search team..."
                                            className="form-control bg-dark text-light"
                                            value={teamSearch}
                                            onChange={(e) => setTeamSearch(e.target.value)}
                                            style={{ maxWidth: 260 }}
                                        />
                                        <select
                                            className="form-select bg-dark text-light"
                                            value={teamID || ""}
                                            onChange={(e) => {
                                                const id = Number(e.target.value);
                                                setTeamID(isNaN(id) ? 0 : id);
                                                setMsg("");
                                            }}
                                            style={{ maxWidth: 260 }}
                                        >
                                            <option value="">Select Team...</option>
                                            {filteredTeams.map((t) => (
                                                <option key={t.id} value={t.id}>
                                                    {t.name} (#{t.id})
                                                </option>
                                            ))}
                                        </select>
                                    </div>

                                    {!teamID && (
                                        <p className="text-warning small">⚠️ Select a team to manage tools.</p>
                                    )}

                                    {teamID && (
                                        <>

                                            {/*     TABBED NAV BAR   */}
                                            <ul className="nav nav-tabs mb-3" role="tablist">
                                                <li className="nav-item">
                                                    <button
                                                        className={`nav-link ${activeTab === "info" ? "active" : ""}`}
                                                        onClick={() => setActiveTab("info")}
                                                    >
                                                        ℹ️ Info
                                                    </button>
                                                </li>

                                                <li className="nav-item">
                                                    <button
                                                        className={`nav-link ${activeTab === "stats" ? "active" : ""}`}
                                                        onClick={() => setActiveTab("stats")}
                                                    >
                                                        📊 Stats
                                                    </button>
                                                </li>

                                                <li className="nav-item">
                                                    <button
                                                        className={`nav-link ${activeTab === "add" ? "active" : ""}`}
                                                        onClick={() => setActiveTab("add")}
                                                    >
                                                        ➕ Add Player
                                                    </button>
                                                </li>

                                                <li className="nav-item">
                                                    <button
                                                        className={`nav-link ${activeTab === "roster" ? "active" : ""}`}
                                                        onClick={() => setActiveTab("roster")}
                                                    >
                                                        👥 Roster
                                                    </button>
                                                </li>
                                            </ul>

                                            {/* === INFO TAB === */}
                                            {activeTab === "info" && (
                                                <div className="p-3 bg-dark rounded border border-secondary">
                                                    <h6 className="text-info mb-3">ℹ️ Team Information</h6>

                                                    <label className="text-light small mb-1">Rename Team</label>
                                                    <div className="d-flex gap-2 mb-3">
                                                        <input
                                                            type="text"
                                                            placeholder="New name..."
                                                            className="form-control bg-black text-light"
                                                            value={newTeamName}
                                                            onChange={(e) => setNewTeamName(e.target.value)}
                                                            style={{ maxWidth: 250 }}
                                                        />
                                                        <button
                                                            className="btn btn-outline-info btn-sm"
                                                            onClick={async () => {
                                                                if (!newTeamName.trim())
                                                                    return setMsg("⚠️ Enter a new name first.");
                                                                await safePost(
                                                                    "/api/mod/team/rename",
                                                                    { team_id: teamID, new_name: newTeamName.trim() },
                                                                    `Renamed team to ${newTeamName.trim()}`,
                                                                    true
                                                                );
                                                            }}
                                                        >
                                                            Save
                                                        </button>
                                                    </div>

                                                    <div className="d-flex flex-wrap gap-2">
                                                        <button
                                                            className="btn btn-outline-secondary btn-sm"
                                                            onClick={async () => {
                                                                await safePost(
                                                                    "/api/mod/team/lock",
                                                                    { team_id: teamID },
                                                                    "Toggled team lock",
                                                                    true
                                                                );
                                                            }}
                                                        >
                                                            🔒 Lock / Unlock
                                                        </button>
                                                        <button
                                                            className="btn btn-outline-warning btn-sm"
                                                            disabled={!teamID}
                                                            onClick={handleResetChallenges}
                                                        >
                                                            ♻️ Reset Challenge Matches
                                                        </button>

                                                        <button
                                                            className="btn btn-outline-danger btn-sm"
                                                            onClick={async () => {
                                                                if (!confirm("Disband this team?")) return;
                                                                await safePost(
                                                                    "/api/mod/team/disband",
                                                                    { team_id: teamID },
                                                                    "Team disbanded",
                                                                    true
                                                                );
                                                            }}
                                                        >
                                                            🚫 Disband
                                                        </button>

                                                        <button
                                                            className="btn btn-outline-danger btn-sm"
                                                            onClick={async () => {
                                                                if (!confirm("Delete team and all matches?")) return;
                                                                await safePost(
                                                                    "/api/mod/team/delete",
                                                                    { team_id: teamID },
                                                                    "Team deleted",
                                                                    true
                                                                );
                                                            }}
                                                        >
                                                            🗑️ Delete Team
                                                        </button>
                                                    </div>
                                                </div>
                                            )}

                                            {/* === STATS TAB === */}
                                            {activeTab === "stats" && (
                                                <div className="p-3 bg-dark rounded border border-secondary">
                                                    <h6 className="text-info mb-3">📊 Team Rating / W-L / Games Played</h6>

                                                    <label className="text-light small mb-1">Rating</label>
                                                    <input
                                                        type="number"
                                                        className="form-control bg-black text-light mb-2"
                                                        style={{ maxWidth: 180 }}
                                                        value={newRating}
                                                        onChange={(e) => setNewRating(e.target.value)}
                                                    />

                                                    <label className="text-light small mb-1">Wins</label>
                                                    <input
                                                        type="number"
                                                        className="form-control bg-black text-light mb-2"
                                                        style={{ maxWidth: 180 }}
                                                        value={newWins}
                                                        onChange={(e) => setNewWins(e.target.value)}
                                                    />

                                                    <label className="text-light small mb-1">Losses</label>
                                                    <input
                                                        type="number"
                                                        className="form-control bg-black text-light mb-2"
                                                        style={{ maxWidth: 180 }}
                                                        value={newLosses}
                                                        onChange={(e) => setNewLosses(e.target.value)}
                                                    />

                                                    <label className="text-light small mb-1">Games Played</label>
                                                    <input
                                                        type="number"
                                                        className="form-control bg-black text-light mb-3"
                                                        style={{ maxWidth: 180 }}
                                                        value={newGP}
                                                        onChange={(e) => setNewGP(e.target.value)}
                                                    />

                                                    <button
                                                        className="btn btn-outline-info btn-sm"
                                                        onClick={handleAdjustTeamStats}
                                                    >
                                                        💾 Save Stats
                                                    </button>
                                                </div>
                                            )}

                                            {/* === ADD PLAYER TAB === */}
                                            {activeTab === "add" && (
                                                <div className="p-3 bg-dark rounded border border-secondary">
                                                    <h6 className="text-success mb-3">➕ Add Player to Team</h6>

                                                    {/* Player Search Box */}
                                                    <div className="position-relative mb-3" style={{ maxWidth: 300 }}>
                                                        <label className="text-light small mb-1">Search Player</label>
                                                        <input
                                                            type="text"
                                                            className="form-control bg-black text-light"
                                                            placeholder="name or ID..."
                                                            value={addPlayerInput}
                                                            onChange={handlePlayerSearchForAdd}
                                                        />

                                                        {/* Autocomplete */}
                                                        {filteredPlayers.length > 0 && (
                                                            <ul
                                                                className="list-group position-absolute w-100 mt-1 shadow-sm"
                                                                style={{ zIndex: 10 }}
                                                            >
                                                                {filteredPlayers.map((p) => (
                                                                    <li
                                                                        key={p.id}
                                                                        className="list-group-item bg-dark text-light small"
                                                                        onMouseDown={() => {
                                                                            setAddPlayerInput(`${p.display_name} (#${p.id})`);
                                                                            setAddPlayerID(p.id);
                                                                            setFilteredPlayers([]);
                                                                        }}
                                                                        style={{ cursor: "pointer" }}
                                                                    >
                                                                        {p.display_name || p.username} #{p.id}
                                                                    </li>
                                                                ))}
                                                            </ul>
                                                        )}
                                                    </div>

                                                    {/* Role Select */}
                                                    <label className="text-light small mb-1">Role</label>
                                                    <select
                                                        className="form-select bg-black text-light mb-3"
                                                        value={addRole}
                                                        onChange={(e) => setAddRole(e.target.value)}
                                                        style={{ maxWidth: 200 }}
                                                    >
                                                        <option value="Member">Member</option>
                                                        <option value="Co-Captain">Co-Captain</option>
                                                        <option value="Captain">Captain</option>
                                                    </select>

                                                    <button
                                                        className="btn btn-outline-success btn-sm"
                                                        disabled={!addPlayerID}
                                                        onClick={handleAddPlayer}
                                                    >
                                                        ➕ Add Player
                                                    </button>
                                                </div>
                                            )}

                                            {/* === ROSTER TAB === */}
                                            {activeTab === "roster" && (
                                                <div
                                                    className="p-3 bg-dark rounded border border-secondary roster-card"
                                                >
                                                    <h6 className="text-info mb-3">👥 Team Roster</h6>

                                                    {teamMembers.length === 0 ? (
                                                        <p className="text-warning small">No members in this team.</p>
                                                    ) : (
                                                        <ul className="list-group bg-dark">
                                                            {teamMembers.map((m) => (
                                                                <li
                                                                    key={m.player_id}
                                                                    className="list-group-item bg-dark text-light border-secondary
                                                                                d-flex align-items-center gap-2"
                                                                >
                                                                    <span className="flex-grow-1 text-truncate">
                                                                        <b>{m.display_name}</b>{" "}
                                                                        <span className="text-info">#{m.player_id}</span>{" "}
                                                                        <span className="text-warning small">[{m.role}]</span>
                                                                    </span>

                                                                    <div className="flex-shrink-0 position-relative">
                                                                        <button
                                                                            className="btn btn-outline-secondary btn-sm"
                                                                            onClick={(e) => {
                                                                                const rect = e.currentTarget.getBoundingClientRect();

                                                                                setMenuPos({
                                                                                    top: Math.min(rect.bottom + 6, window.innerHeight - 220),
                                                                                    left: Math.min(rect.left, window.innerWidth - 200),
                                                                                });

                                                                                setOpenId(openId === m.player_id ? null : m.player_id);
                                                                            }}
                                                                        >
                                                                            ⋮
                                                                        </button>

                                                                        {openId === m.player_id && (
                                                                            <div
                                                                                className="position-fixed p-2 rounded bg-dark border border-secondary shadow role-popup"
                                                                                style={{
                                                                                    zIndex: 9999,
                                                                                    minWidth: 190,
                                                                                    top: `${menuPos.top}px`,
                                                                                    left: `${menuPos.left}px`,
                                                                                }}
                                                                            >

                                                                                {/* 👑 Captain */}
                                                                                <button
                                                                                    className={`dropdown-item d-flex align-items-center gap-2
                                                                                    ${isRole(m, "Captain") ? "active-role" : ""}`}
                                                                                    onClick={() => {
                                                                                        HandleModPromoteToCaptain(m.player_id, m.display_name);
                                                                                        setOpenId(null);
                                                                                    }}
                                                                                >
                                                                                    👑 Promote to Captain
                                                                                    {isRole(m, "Captain") && (
                                                                                        <span className="ms-auto badge bg-warning text-dark">Current</span>
                                                                                    )}
                                                                                </button>

                                                                                {/* ⭐ Co-Captain */}
                                                                                <button
                                                                                    className={`dropdown-item d-flex align-items-center gap-2
                                                                                    ${isRole(m, "Co-Captain") ? "active-role" : ""}`}
                                                                                    onClick={() => {
                                                                                        handleSetRole(m.player_id, "Co-Captain", m.display_name);
                                                                                        setOpenId(null);
                                                                                    }}
                                                                                >
                                                                                    ⭐ Set Co-Captain
                                                                                    {isRole(m, "Co-Captain") && (
                                                                                        <span className="ms-auto badge bg-success">Current</span>
                                                                                    )}
                                                                                </button>

                                                                                {/* 👤 Member */}
                                                                                <button
                                                                                    className={`dropdown-item d-flex align-items-center gap-2
                                                                                    ${isRole(m, "Member") ? "active-role" : ""}`}
                                                                                    onClick={() => {
                                                                                        handleSetRole(m.player_id, "Member", m.display_name);
                                                                                        setOpenId(null);
                                                                                    }}
                                                                                >
                                                                                    👤 Set Member
                                                                                    {isRole(m, "Member") && (
                                                                                        <span className="ms-auto badge bg-secondary">Current</span>
                                                                                    )}
                                                                                </button>

                                                                                <hr className="dropdown-divider" />

                                                                                {/* 🦶 Kick */}
                                                                                <button
                                                                                    className="dropdown-item text-danger d-flex align-items-center gap-2"
                                                                                    onClick={() => {
                                                                                        handleKickMember(m.player_id, m.display_name);
                                                                                        setOpenId(null);
                                                                                    }}
                                                                                >
                                                                                    🦶 Kick Player
                                                                                </button>
                                                                            </div>
                                                                        )}

                                                                    </div>
                                                                </li>
                                                            ))}
                                                        </ul>
                                                    )}
                                                </div>
                                            )}

                                        </>
                                    )}
                                </div>

                                {/* === Active Controls === */}
                                <div
                                    className="p-3 mb-3 rounded-3"
                                    style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                                >
                                    <h6 className="text-success mb-2">⚙️ Active Controls</h6>
                                    <p className="text-light small mb-2">
                                        Mark one or all teams as <b>Active</b> for the current season.
                                    </p>

                                    <div className="d-flex flex-wrap gap-2">
                                        {/* 🔹 Set Selected Team Active */}
                                        <button
                                            className="btn btn-outline-success btn-sm"
                                            onClick={async () => {
                                                if (!teamID) return setMsg("⚠️ Select a team first.");
                                                await safePost(
                                                    "/api/mod/team/set-active",
                                                    { team_id: parseInt(teamID) },
                                                    `Set ${getTeamLabel(teamID)} to Active`,
                                                    true
                                                );
                                            }}
                                        >
                                            ✅ Set Selected Team Active
                                        </button>

                                        {/* 🔹 Set ALL Teams Active */}
                                        <button
                                            className="btn btn-outline-info btn-sm"
                                            onClick={async () => {
                                                if (!confirm("⚠️ Mark ALL teams as Active?")) return;
                                                await safePost(
                                                    "/api/mod/teams/set-all-active",
                                                    {},
                                                    "Set all teams to Active",
                                                    true
                                                );
                                            }}
                                        >
                                            🌍 Set ALL Teams Active
                                        </button>
                                    </div>
                                </div>

                                {/* === Inactive Controls === */}
                                <div
                                    className="p-3 mb-3 rounded-3"
                                    style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                                >
                                    <h6 className="text-warning mb-2">⚙️ Inactive Controls</h6>
                                    <p className="text-light small mb-2">
                                        Mark one or all teams as inactive for the current season.
                                    </p>
                                    <div className="d-flex flex-wrap gap-2">
                                        <button
                                            className="btn btn-outline-warning btn-sm"
                                            onClick={async () => {
                                                if (!teamID) return setMsg("⚠️ Select a team first.");
                                                await safePost(
                                                    "/api/mod/team/set-inactive",
                                                    { team_id: teamID },
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
                                                if (!confirm("⚠️ Mark ALL teams as Inactive?")) return;
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

                                {/* === Roster Lock Controls === */}
                                <div
                                    className="p-3 rounded-3"
                                    style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                                >
                                    <h6 className="text-info mb-2">🔒 Roster Lock Controls</h6>
                                    <p className="text-light small mb-2">
                                        Locking rosters prevents players from <b>joining</b> or <b>creating</b> teams.
                                    </p>
                                    <div className="d-flex flex-wrap gap-2">
                                        <button
                                            className="btn btn-outline-warning btn-sm"
                                            onClick={async () => {
                                                if (!confirm("⚠️ Lock all rosters?")) return;
                                                await safePost("/api/mod/roster/lock-all", {}, "All rosters locked");
                                            }}
                                        >
                                            🔒 Lock All Rosters
                                        </button>
                                        <button
                                            className="btn btn-outline-success btn-sm"
                                            onClick={async () => {
                                                if (!confirm("✅ Unlock all rosters and allow joins/creations?")) return;
                                                await safePost("/api/mod/roster/unlock-all", {}, "All rosters unlocked");
                                            }}
                                        >
                                            🔓 Unlock All Rosters
                                        </button>
                                    </div>
                                </div>
                                {/* === Global Challenge Controls === */}
                                <div
                                    className="p-3 rounded-3 mt-3"
                                    style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                                >
                                    <h6 className="text-danger mb-2">⚔️ Challenge Controls</h6>

                                    <p className="text-light small mb-2">
                                        Toggle **ALL team challenge requests** ON or OFF.
                                        <br />
                                        • When <b>disabled</b>: Teams cannot toggle challenges in their settings and cannot receive challenge requests.
                                        <br />
                                        • When <b>enabled</b>: Teams may toggle challenge requests normally, but all remain OFF until the captain turns it on.
                                    </p>

                                    <div className="d-flex flex-wrap gap-2">

                                        {/* Enable ALL Challenges */}
                                        <button
                                            className="btn btn-outline-success btn-sm"
                                            onClick={async () => {
                                                if (!confirm("Enable challenges globally? Teams will be able to toggle challenges again.")) return;
                                                await safePost(
                                                    "/api/mod/challenges/enable",
                                                    {},
                                                    "Enabled all team challenges globally",
                                                    true
                                                );
                                            }}
                                        >
                                            ✅ Enable Global Challenges
                                        </button>

                                        {/* Disable ALL Challenges */}
                                        <button
                                            className="btn btn-outline-danger btn-sm"
                                            onClick={async () => {
                                                if (!confirm("Disable ALL challenges? This will block all team challenge toggles.")) return;
                                                await safePost(
                                                    "/api/mod/challenges/disable",
                                                    {},
                                                    "Disabled all team challenges globally",
                                                    true
                                                );
                                            }}
                                        >
                                            🛑 Disable Global Challenges
                                        </button>
                                    </div>
                                </div>
                            </div>
                        }
                    />

                    {/* 🚫 PLAYER TOOLS */}
                    <AccordionItem
                        id="playerTools"
                        title="🚫 Player Tools"
                        children={
                            <div className="bg-black text-light p-3 rounded-3 border border-secondary">
                                {/* === Player Search & Lookup === */}
                                <div
                                    className="p-3 mb-4 rounded-3 position-relative"
                                    style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                                >
                                    <h6 className="text-warning mb-2">🎮 Player Lookup</h6>
                                    <p className="small text-light mb-3">
                                        Type a player's name or ID to perform moderation actions (kick, ban, unban).
                                    </p>

                                    {/* 🔍 Player search with autocomplete */}
                                    <div className="position-relative" style={{ maxWidth: 300 }}>
                                        <input
                                            type="text"
                                            className="form-control bg-dark text-light"
                                            placeholder="Search by name or ID..."
                                            value={playerSearch}
                                            onChange={async (e) => {
                                                const value = e.target.value;
                                                setPlayerSearch(value);
                                                setPlayerID(""); // reset selected
                                                if (value.trim().length >= 2) {
                                                    try {
                                                        const res = await axios.get(`${urlBase}/api/players`);
                                                        const players = res.data || [];
                                                        const filtered = players.filter((p) =>
                                                            [p.username, p.display_name, String(p.id)]
                                                                .join(" ")
                                                                .toLowerCase()
                                                                .includes(value.toLowerCase())
                                                        );
                                                        setPlayerSuggestions(filtered.slice(0, 8));
                                                    } catch {
                                                        setPlayerSuggestions([]);
                                                    }
                                                } else {
                                                    setPlayerSuggestions([]);
                                                }
                                            }}
                                            onBlur={() => setTimeout(() => setPlayerSuggestions([]), 150)}
                                        />

                                        {/* Autocomplete dropdown */}
                                        {playerSuggestions.length > 0 && (
                                            <ul
                                                className="list-group position-absolute w-100 mt-1 shadow-sm"
                                                style={{
                                                    zIndex: 10,
                                                    borderRadius: "0.4rem",
                                                    overflow: "hidden",
                                                }}
                                            >
                                                {playerSuggestions.map((p) => (
                                                    <li
                                                        key={p.id}
                                                        className="list-group-item list-group-item-action bg-dark text-light border-secondary small"
                                                        style={{ cursor: "pointer" }}
                                                        onMouseDown={() => {
                                                            setPlayerSearch(`${p.display_name} (${p.id})`);
                                                            setPlayerID(String(p.id)); // ✅ correct ID assigned
                                                            setPlayerSuggestions([]);
                                                        }}
                                                    >
                                                        {p.display_name || p.username}{" "}
                                                        <span className="text-light">#{p.id}</span>
                                                    </li>
                                                ))}
                                            </ul>
                                        )}
                                    </div>
                                </div>

                                {/* === Player Actions === */}
                                <div
                                    className="p-3 rounded-3"
                                    style={{
                                        backgroundColor: "#151515",
                                        border: "1px solid #333",
                                        boxShadow: "0 0 8px rgba(255,255,255,0.05)",
                                    }}
                                >
                                    <h6 className="text-danger mb-2">⚔️ Player Actions</h6>
                                    <p className="small text-light mb-3">
                                        Apply moderation actions. These actions cannot be undone easily — proceed with caution.
                                    </p>

                                    <div className="d-flex flex-wrap gap-2">
                                        <button
                                            className="btn btn-outline-warning btn-sm"
                                            disabled={!playerID || !teamID}
                                            onClick={handleKickPlayer}
                                        >
                                            🦶 Kick Player
                                        </button>

                                        <button
                                            className="btn btn-outline-danger btn-sm"
                                            disabled={!playerID}
                                            onClick={handleBanPlayer}
                                        >
                                            🚫 Ban Player
                                        </button>

                                        <button
                                            className="btn btn-outline-success btn-sm"
                                            disabled={!playerID}
                                            onClick={handleUnbanPlayer}
                                        >
                                            ✅ Unban Player
                                        </button>

                                        {/* 🆕 Remove Cooldown Button */}
                                        <button
                                            className="btn btn-outline-info btn-sm"
                                            disabled={!playerID}
                                            onClick={async () => {
                                                if (!playerID) {
                                                    setMsg("⚠️ Select a player first.");
                                                    return;
                                                }

                                                try {
                                                    await axios.post(
                                                        `${urlBase}/api/mod/player/remove-cooldown`,
                                                        { player_id: String(playerID) },
                                                        { withCredentials: true }
                                                    );

                                                    setMsg(`✅ Removed cooldown for player #${playerID}`);
                                                } catch (err) {
                                                    console.error("❌ Failed to remove cooldown:", err);
                                                    setMsg("❌ Failed to remove player cooldown");
                                                }
                                            }}
                                        >
                                            ⏳❌ Remove Cooldown
                                        </button>
                                    </div>
                                </div>
                                {/* 🎮 PLAYER RATING / W-L-GP EDITOR (Isolated State) */}
                                <div
                                    className="p-3 mt-4 rounded"
                                    style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                                >
                                    <h6 className="text-info mb-3">🎮 Edit Player Rating / Wins / Losses / Games Played</h6>

                                    {/* 🔍 Player Search Input */}
                                    <label className="text-light small mb-1">Search Player (name or Discord ID)</label>

                                    <div className="position-relative" style={{ maxWidth: 300 }}>
                                        <input
                                            type="text"
                                            className="form-control bg-dark text-light mb-2"
                                            placeholder="Type to search..."
                                            value={statSearch}
                                            onChange={async (e) => {
                                                const value = e.target.value;
                                                setStatSearch(value);
                                                setStatPlayerID(""); // reset selected

                                                if (value.trim().length >= 2) {
                                                    try {
                                                        const res = await axios.get(`${urlBase}/api/players`);
                                                        const players = res.data || [];

                                                        const filtered = players.filter((p) =>
                                                            [p.username, p.display_name, String(p.id)]
                                                                .join(" ")
                                                                .toLowerCase()
                                                                .includes(value.toLowerCase())
                                                        );

                                                        setStatSuggestions(filtered.slice(0, 8));
                                                    } catch {
                                                        setStatSuggestions([]);
                                                    }
                                                } else {
                                                    setStatSuggestions([]);
                                                }
                                            }}
                                            onBlur={() => setTimeout(() => setStatSuggestions([]), 150)}
                                        />

                                        {/* Autocomplete Dropdown */}
                                        {statSuggestions.length > 0 && (
                                            <ul
                                                className="list-group position-absolute w-100 mt-1 shadow-sm"
                                                style={{ zIndex: 10, borderRadius: "0.4rem", overflow: "hidden" }}
                                            >
                                                {statSuggestions.map((p) => (
                                                    <li
                                                        key={p.id}
                                                        className="list-group-item list-group-item-action bg-dark text-light border-secondary small"
                                                        style={{ cursor: "pointer" }}
                                                        onMouseDown={() => {
                                                            setStatSearch(`${p.display_name || p.username} (#${p.id})`);
                                                            setStatPlayerID(String(p.id));   // IMPORTANT: force string
                                                            setStatSuggestions([]);

                                                            // Auto-load stats for selected player
                                                            axios
                                                                .get(`${urlBase}/api/mod/player/stats?id=${p.id}`)
                                                                .then((res) => {
                                                                    setPRating(res.data.rating ?? 0);
                                                                    setPWins(res.data.wins ?? 0);
                                                                    setPLosses(res.data.losses ?? 0);
                                                                    setPMatches(res.data.matches ?? 0);
                                                                })
                                                                .catch(() => {
                                                                    setPRating("");
                                                                    setPWins("");
                                                                    setPLosses("");
                                                                    setPMatches("");
                                                                });
                                                        }}
                                                    >
                                                        {p.display_name || p.username}
                                                        <span className="text-info ms-1">#{p.id}</span>
                                                    </li>
                                                ))}
                                            </ul>
                                        )}
                                    </div>

                                    {/* 🚫 Disable fields until a player is selected */}
                                    {!statPlayerID && (
                                        <p className="text-warning small mt-2">
                                            ⚠️ Select a player from the list to edit stats.
                                        </p>
                                    )}

                                    {statPlayerID && (
                                        <>
                                            {/* Rating */}
                                            <label className="text-light small mb-1 mt-3">Player Rating</label>
                                            <input
                                                type="number"
                                                className="form-control bg-dark text-light mb-3"
                                                placeholder="Rating..."
                                                value={pRating}
                                                onChange={(e) => setPRating(e.target.value)}
                                                style={{ maxWidth: 180 }}
                                            />

                                            {/* Wins */}
                                            <label className="text-light small mb-1">Wins</label>
                                            <input
                                                type="number"
                                                className="form-control bg-dark text-light mb-3"
                                                placeholder="Wins..."
                                                value={pWins}
                                                onChange={(e) => setPWins(e.target.value)}
                                                style={{ maxWidth: 180 }}
                                            />

                                            {/* Losses */}
                                            <label className="text-light small mb-1">Losses</label>
                                            <input
                                                type="number"
                                                className="form-control bg-dark text-light mb-3"
                                                placeholder="Losses..."
                                                value={pLosses}
                                                onChange={(e) => setPLosses(e.target.value)}
                                                style={{ maxWidth: 180 }}
                                            />

                                            {/* Games Played */}
                                            <label className="text-light small mb-1">Games Played</label>
                                            <input
                                                type="number"
                                                className="form-control bg-dark text-light mb-3"
                                                placeholder="Games Played..."
                                                value={pMatches}
                                                onChange={(e) => setPMatches(e.target.value)}
                                                style={{ maxWidth: 180 }}
                                            />

                                            <button
                                                className="btn btn-outline-info btn-sm mt-2"
                                                onClick={async () => {
                                                    await axios.post(
                                                        `${urlBase}/api/mod/player/adjust-stats`,
                                                        {
                                                            player_id: String(statPlayerID),   // send string
                                                            rating: Number(pRating),
                                                            wins: Number(pWins),
                                                            losses: Number(pLosses),
                                                            matches: Number(pMatches),
                                                        }
                                                    );
                                                    alert("✔ Player stats updated!");
                                                }}
                                            >
                                                💾 Save Player Stats
                                            </button>
                                        </>
                                    )}
                                </div>
                            </div>
                        }
                    />

                    {/* 📦 DATA TOOLS */}
                    <AccordionItem
                        id="dataTools"
                        title="📦 Data Tools"
                        children={
                            <div className="d-flex flex-wrap gap-2">

                                <button
                                    className="btn btn-outline-secondary btn-sm"
                                    onClick={handleArchiveSeason}
                                >
                                    Archive Season
                                </button>
                                <button
                                    className="btn btn-outline-info btn-sm"
                                    onClick={async () => {
                                        try {
                                            const seasonRes = await axios.get(`${urlBase}/api/season`, { withCredentials: true });
                                            let season = seasonRes.data?.season ?? "Preseason";

                                            await safePost(
                                                "/api/mod/player/archive-all",
                                                { season },
                                                `Archived all player stats for Season ${season}`
                                            );
                                        } catch (err) {
                                            console.error("Archive failed:", err);
                                            alert("❌ Failed to archive all player stats");
                                        }
                                    }}
                                >
                                    📦 Archive Player Stats (All Players)
                                </button>
                                <button
                                    className="btn btn-outline-primary btn-sm"
                                    onClick={async () => {
                                        try {
                                            const res = await fetch(`${import.meta.env.VITE_API_URL}/api/tools/archive-team-stats`, {
                                                method: "POST",
                                                credentials: "include",
                                            });

                                            const data = await res.json();
                                            alert(`Archived ${data.archived} teams successfully`);
                                        } catch (err) {
                                            alert("Failed to archive team stats");
                                        }
                                    }}
                                >
                                    📦 Archive Team Stats
                                </button>

                                <button
                                    className="btn btn-outline-warning btn-sm"
                                    onClick={handleResetLeaderboard}
                                >
                                    Reset Team Leaderboard
                                </button>
                                <button
                                    className="btn btn-outline-warning"
                                    onClick={resetPlayerLeaderboard}
                                >
                                    Reset Player Leaderboard
                                </button>

                                {/* 🔄  Sync Discord Roles */}
                                <button
                                    className="btn btn-outline-primary btn-sm"
                                    onClick={async () => {
                                        if (!confirm("Sync all player Discord roles?")) return;
                                        try {
                                            await axios.post(
                                                `${urlBase}/api/mod/sync-roles`,
                                                {},
                                                { withCredentials: true }
                                            );
                                            alert("✅ Discord roles synced successfully!");
                                        } catch (err) {
                                            console.error("Sync roles failed:", err);
                                            alert("❌ Failed to sync Discord roles");
                                        }
                                    }}
                                >
                                    🔄 Sync Roles
                                </button>

                            </div>
                        }
                    />
                    {/* 🏆 FINALS MANAGEMENT */}
                    <AccordionItem
                        id="finalsManagement"
                        title="🏆 Finals Setup & Bracket Tools"
                        children={
                            <div className="text-light">

                                {/* STATUS MESSAGE */}
                                {msg && (
                                    <div
                                        className={`alert ${msg.startsWith("✅") ? "alert-success" :
                                            msg.startsWith("⚠️") ? "alert-warning" :
                                                "alert-danger"
                                            } small mb-3`}
                                    >
                                        {msg}
                                    </div>
                                )}

                                {/* ================================ */}
                                {/* SECTION: FINALS TEAMS */}
                                {/* ================================ */}
                                <div
                                    className="p-3 mb-4 rounded shadow-sm"
                                    style={{ backgroundColor: "#161616", border: "1px solid #2d2d2d" }}
                                >
                                    <h4 className="mb-3 d-flex align-items-center text-info">
                                        <span style={{ fontSize: "1.4rem" }}>🏅</span>
                                        <span className="ms-2">Finals Teams</span>
                                    </h4>

                                    {/* Seed Editor */}
                                    {finalsTeams.length > 0 && (
                                        <div className="mb-3">

                                            <FinalsSeedEditor
                                                teams={finalsTeams}
                                                setTeams={(newList) => setFinalsTeams(newList)}
                                                onSave={async (newSeeds) => {
                                                    await safePost(
                                                        "/api/mod/finals/update-seeds",
                                                        { seeds: newSeeds },
                                                        "Updated Finals seeds"
                                                    );
                                                    loadFinals();
                                                }}
                                            />

                                            {/* REMOVE TEAM FROM FINALS */}
                                            <div className="mt-3">
                                                <h6 className="text-light mb-2">❌ Remove Team From Finals</h6>

                                                {finalsTeams.map((ft) => (
                                                    <div
                                                        key={ft.team_id}
                                                        className="d-flex justify-content-between align-items-center
                                                            p-2 mb-2 rounded"
                                                        style={{ backgroundColor: "#111", border: "1px solid #333" }}
                                                    >
                                                        <span>{ft.seed}. {ft.name}</span>

                                                        <button
                                                            className="btn btn-sm btn-outline-danger"
                                                            onClick={() => {
                                                                safePost(
                                                                    "/api/mod/finals/remove-team",
                                                                    { team_id: ft.team_id },
                                                                    `Removed ${ft.name} from Finals`
                                                                ).then(() => {
                                                                    // Update local state instantly
                                                                    setFinalsTeams(prev =>
                                                                        prev
                                                                            .filter(x => x.team_id !== ft.team_id)
                                                                            .map((t, idx) => ({
                                                                                ...t,
                                                                                seed: idx + 1,
                                                                            }))
                                                                    );
                                                                    loadFinals();
                                                                });
                                                            }}
                                                        >
                                                            Remove
                                                        </button>
                                                    </div>
                                                ))}
                                            </div>

                                        </div>
                                    )}

                                    {/* Divider */}
                                    <hr className="border-secondary my-3" />

                                    {/* Add Team to Finals */}
                                    <h6 className="text-light mb-2">➕ Add Team to Finals</h6>
                                    <div className="d-flex gap-2 flex-wrap">
                                        <select
                                            className="form-select bg-dark text-light"
                                            id="addFinalsTeamSelect"
                                            style={{ maxWidth: 300 }}
                                            onChange={(e) => {
                                                const tid = Number(e.target.value);
                                                if (!tid) return;

                                                const t = teams.find(x => x.id === tid);

                                                // Update local state
                                                setFinalsTeams(prev => {
                                                    const updated = [
                                                        ...prev,
                                                        {
                                                            team_id: tid,
                                                            name: t?.name || `Team #${tid}`,
                                                        }
                                                    ];

                                                    // Reseed
                                                    return updated.map((team, index) => ({
                                                        ...team,
                                                        seed: index + 1
                                                    }));
                                                });

                                                e.target.value = "";

                                                safePost(
                                                    "/api/mod/finals/add-team",
                                                    { team_id: tid },
                                                    `Added ${t?.name || "team"} to Finals`
                                                ).then(loadFinals);
                                            }}
                                        >
                                            <option value="">Select a team…</option>

                                            {/* Only show TEAMS NOT already in finals */}
                                            {teams
                                                .filter(t => !finalsTeams.some(ft => ft.team_id === t.id))
                                                .map((t) => (
                                                    <option key={t.id} value={t.id}>
                                                        {t.name}
                                                    </option>
                                                ))}
                                        </select>
                                    </div>
                                </div>

                                {/* ================================ */}
                                {/* SECTION: BRACKET TOOLS */}
                                {/* ================================ */}
                                <div
                                    className="p-3 mb-4 rounded shadow-sm"
                                    style={{ backgroundColor: "#161616", border: "1px solid #2d2d2d" }}
                                >
                                    <h4 className="mb-3 d-flex align-items-center text-warning">
                                        <span style={{ fontSize: "1.4rem" }}>📊</span>
                                        <span className="ms-2">Bracket Management</span>
                                    </h4>

                                    {/* Visibility Toggle */}
                                    <div className="p-3 rounded mb-3"
                                        style={{
                                            backgroundColor: "#0f0f0f",
                                            border: "1px solid #2c2c2c"
                                        }}
                                    >
                                        <div className="form-check form-switch">
                                            <input
                                                className="form-check-input"
                                                type="checkbox"
                                                id="toggleFinalsVisible"
                                                checked={finalsVisible}
                                                onChange={(e) => {
                                                    const visible = e.target.checked;
                                                    safePost(
                                                        "/api/mod/finals/toggle-visible",
                                                        { visible },
                                                        visible ? "Finals tab is now visible" : "Finals tab hidden"
                                                    ).then(() => {
                                                        setFinalsVisible(visible);
                                                        window.dispatchEvent(new Event("finals-visibility-updated"));
                                                    });
                                                }}
                                            />
                                            <label className="form-check-label ms-2" htmlFor="toggleFinalsVisible">
                                                {finalsVisible
                                                    ? "Finals Visible to Public"
                                                    : "Finals Hidden from Public"}
                                            </label>
                                        </div>
                                    </div>

                                    {/* Bracket Actions */}
                                    <div className="d-flex flex-wrap gap-2">

                                        <button
                                            className="btn btn-success"
                                            style={{ minWidth: 170 }}
                                            onClick={() =>
                                                safePost(
                                                    "/api/mod/finals/generate",
                                                    {},
                                                    "Generated Finals Bracket"
                                                ).then(loadFinals)
                                            }
                                        >
                                            🚀 Auto-Generate Bracket
                                        </button>

                                        <button
                                            className="btn btn-danger"
                                            style={{ minWidth: 170 }}
                                            onClick={() =>
                                                safePost(
                                                    "/api/mod/finals/reset",
                                                    {},
                                                    "Reset Finals Bracket"
                                                ).then(loadFinals)
                                            }
                                        >
                                            ♻️ Reset Entire Bracket
                                        </button>

                                        <button
                                            className="btn btn-warning text-dark"
                                            style={{ minWidth: 170 }}
                                            onClick={() =>
                                                safePost(
                                                    "/api/mod/finals/clear-bracket-view",
                                                    {},
                                                    "Cleared Finals Bracket View"
                                                ).then(loadFinals)
                                            }
                                        >
                                            🧹 Clear Bracket (Keep Matches)
                                        </button>

                                    </div>
                                </div>

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
    const [query, setQuery] = useState("");

    useEffect(() => {
        async function loadLogs() {
            try {
                setLoading(true);
                const res = await axios.get(
                    `${urlBase}/api/mod/team/history?q=${encodeURIComponent(query)}`,
                    { withCredentials: true }
                );
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
    }, [query, urlBase]);

    return (
        <div>
            <h6 className="text-light mb-3">📜 Team Rename Logs</h6>

            <div className="d-flex flex-wrap align-items-center gap-2 mb-3">
                <input
                    type="text"
                    className="form-control bg-dark text-light"
                    placeholder="Search by player, old name, or new name..."
                    style={{ maxWidth: 300 }}
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                />
                <button
                    className="btn btn-outline-secondary btn-sm"
                    onClick={() => setQuery("")}
                >
                    Clear
                </button>
            </div>

            {loading ? (
                <p className="text-light">Loading rename logs...</p>
            ) : error ? (
                <div className="alert alert-danger">{error}</div>
            ) : logs.length === 0 ? (
                <p className="text-light">No rename logs found.</p>
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
                                    <td className="text-light">
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
