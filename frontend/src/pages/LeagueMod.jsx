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

    const urlBase = import.meta.env.VITE_API_URL;

    // Fetch logged-in user info
    useEffect(() => {
        axios
            .get(`${urlBase}/api/me`, { withCredentials: true })
            .then((res) => setMe(res.data))
            .catch(() => setMe(null))
            .finally(() => setLoading(false));
    }, []);

    if (loading) return <p>⏳ Checking permissions...</p>;
    if (!me) return <p>🔐 Please log in to access the League Mod Panel.</p>;
    if (!me.is_mod)
        return <p>🚫 You do not have permission to view this panel.</p>;

    // ✅ Generalized helper
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

    // ===========================================================
    // 🏁 MATCH TOOLS
    // ===========================================================
    const handleGenerateWeek = async () => {
        if (!week || isNaN(week)) {
            setMsg("⚠️ Please enter a valid week number first.");
            return;
        }
        await safePost(
            "/api/matches/generate",
            { week: parseInt(week) },
            `Generated matches for Week ${week}`
        );
    };

    const handleForceSchedule = async () => {
        if (!matchID) return setMsg("⚠️ Enter a match ID first.");
        const date = new Date().toISOString();
        await safePost(
            "/api/match/schedule",
            { match_id: parseInt(matchID), team_id: 0, date },
            `Force scheduled match #${matchID}`
        );
    };

    const handleForfeit = async () => {
        if (!matchID || !teamID)
            return setMsg("⚠️ Enter match ID and winning team ID first.");
        await safePost(
            "/api/mod/match/forfeit",
            { match_id: parseInt(matchID), winner_team_id: parseInt(teamID) },
            `Forced forfeit for match #${matchID}`
        );
    };

    const handleDoubleForfeit = async () => {
        if (!matchID) return setMsg("⚠️ Enter a match ID first.");
        await safePost(
            "/api/mod/match/double-forfeit",
            { match_id: parseInt(matchID) },
            `Double forfeit applied for match #${matchID}`
        );
    };

    const handleResetMatch = async () => {
        if (!matchID) return setMsg("⚠️ Enter a match ID first.");
        await safePost(
            "/api/mod/match/reset",
            { match_id: parseInt(matchID) },
            `Reset match #${matchID}`
        );
    };

    const handleDeleteMatch = async () => {
        if (!matchID) return setMsg("⚠️ Enter a match ID first.");
        await safePost(
            "/api/mod/match/delete",
            { match_id: parseInt(matchID) },
            `Deleted match #${matchID}`
        );
    };

    // ===========================================================
    // 🧾 SCORE TOOLS
    // ===========================================================
    const handleForceSubmitScores = async () => {
        if (!matchID) return setMsg("⚠️ Enter match ID first.");
        await safePost(
            "/api/match/submit-score",
            { match_id: parseInt(matchID), team_id: 0, maps: [] },
            `Forced finalization of match #${matchID}`
        );
    };

    const handleEditScores = async () => {
        if (!matchID) return setMsg("⚠️ Enter match ID first.");
        await safePost(
            "/api/mod/match/edit-score",
            { match_id: parseInt(matchID) },
            `Edited map scores for match #${matchID}`
        );
    };

    const handleAdjustRating = async () => {
        if (!teamID) return setMsg("⚠️ Enter team ID first.");
        await safePost(
            "/api/mod/team/adjust-rating",
            { team_id: parseInt(teamID), delta: 25 },
            `Adjusted team rating for team #${teamID}`
        );
    };

    // ===========================================================
    // 👥 TEAM TOOLS
    // ===========================================================
    const handleRenameTeam = async () => {
        if (!teamID) return setMsg("⚠️ Enter team ID first.");
        const newName = prompt("Enter new team name:");
        if (!newName) return;
        await safePost(
            "/api/mod/team/rename",
            { team_id: parseInt(teamID), new_name: newName },
            `Renamed team #${teamID} to ${newName}`
        );
    };

    const handleDisbandTeam = async () => {
        if (!teamID) return setMsg("⚠️ Enter team ID first.");
        await safePost(
            "/api/mod/team/disband",
            { team_id: parseInt(teamID) },
            `Disbanded team #${teamID}`
        );
    };

    const handleLockTeam = async () => {
        if (!teamID) return setMsg("⚠️ Enter team ID first.");
        await safePost(
            "/api/mod/team/lock",
            { team_id: parseInt(teamID) },
            `Toggled lock status for team #${teamID}`
        );
    };

    // ===========================================================
    // 🚫 PLAYER TOOLS
    // ===========================================================
    const handleKickPlayer = async () => {
        if (!teamID || !playerID)
            return setMsg("⚠️ Enter team ID and player ID first.");
        await safePost(
            "/api/team/kick",
            { team_id: parseInt(teamID), player_id: playerID },
            `Kicked player ${playerID} from team #${teamID}`
        );
    };

    const handleBanPlayer = async () => {
        if (!playerID) return setMsg("⚠️ Enter player ID first.");
        await safePost(
            "/api/mod/player/ban",
            { player_id: playerID },
            `Banned player ${playerID}`
        );
    };

    const handleUnbanPlayer = async () => {
        if (!playerID) return setMsg("⚠️ Enter player ID first.");
        await safePost(
            "/api/mod/player/unban",
            { player_id: playerID },
            `Unbanned player ${playerID}`
        );
    };

    const handleEditStats = async () => {
        if (!playerID) return setMsg("⚠️ Enter player ID first.");
        await safePost(
            "/api/mod/player/edit-stats",
            { player_id: playerID, wins: 1, losses: 0 },
            `Edited stats for player ${playerID}`
        );
    };

    // ===========================================================
    // 📦 DATA TOOLS
    // ===========================================================
    const handleArchiveSeason = async () => {
        const reset = confirm("Archive current season and reset everything?");
        await safePost("/api/mod/season/archive", { format: "json", reset_after: reset }, "Archived season data");
    };

    const handleResetLeaderboard = async () => {
        await safePost("/api/mod/leaderboard/reset", {}, "Reset leaderboards");
    };

    const handleSyncHistory = async () => {
        await safePost(
            "/api/mod/player/sync-history",
            {},
            "Synchronized player history"
        );
    };

    const handleRebuildELO = async () => {
        await safePost("/api/mod/elo/rebuild", {}, "Rebuilt ELO rankings");
    };

    // ===========================================================
    // RENDER UI
    // ===========================================================
    return (
        <div className="text-light container mt-3 mb-5">
            <h2 className="mb-2">🛠️ League Moderator Panel</h2>
            <p className="text-muted small mb-3">
                Restricted admin tools for ECGL League Moderators.
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
                            <h6>📅 Generate Weekly Matches</h6>
                            <div className="d-flex gap-2 mb-3" style={{ maxWidth: 300 }}>
                                <input
                                    type="number"
                                    className="form-control bg-dark text-light"
                                    placeholder="Enter week..."
                                    value={week}
                                    onChange={(e) => setWeek(e.target.value)}
                                />
                                <button className="btn btn-primary" onClick={handleGenerateWeek}>
                                    Generate
                                </button>
                            </div>
                            <h6>🕒 Match Admin</h6>
                            <input
                                className="form-control bg-dark text-light mb-2"
                                placeholder="Match ID"
                                value={matchID}
                                onChange={(e) => setMatchID(e.target.value)}
                                style={{ maxWidth: 200 }}
                            />
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
                                className="form-control bg-dark text-light mb-2"
                                placeholder="Match ID"
                                value={matchID}
                                onChange={(e) => setMatchID(e.target.value)}
                                style={{ maxWidth: 200 }}
                            />
                            <div className="d-flex flex-wrap gap-2">
                                <button
                                    className="btn btn-outline-primary btn-sm"
                                    onClick={handleForceSubmitScores}
                                >
                                    Force Submit
                                </button>
                                <button
                                    className="btn btn-outline-warning btn-sm"
                                    onClick={handleEditScores}
                                >
                                    Edit Scores
                                </button>
                                <button
                                    className="btn btn-outline-info btn-sm"
                                    onClick={handleAdjustRating}
                                >
                                    Adjust Rating
                                </button>
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
                                className="form-control bg-dark text-light mb-2"
                                placeholder="Team ID"
                                value={teamID}
                                onChange={(e) => setTeamID(e.target.value)}
                                style={{ maxWidth: 200 }}
                            />
                            <div className="d-flex flex-wrap gap-2">
                                <button
                                    className="btn btn-outline-info btn-sm"
                                    onClick={handleRenameTeam}
                                >
                                    Rename
                                </button>
                                <button
                                    className="btn btn-outline-danger btn-sm"
                                    onClick={handleDisbandTeam}
                                >
                                    Disband
                                </button>
                                <button
                                    className="btn btn-outline-secondary btn-sm"
                                    onClick={handleLockTeam}
                                >
                                    Lock / Unlock
                                </button>
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
                            <input
                                className="form-control bg-dark text-light mb-2"
                                placeholder="Player ID"
                                value={playerID}
                                onChange={(e) => setPlayerID(e.target.value)}
                                style={{ maxWidth: 200 }}
                            />
                            <input
                                className="form-control bg-dark text-light mb-2"
                                placeholder="Team ID"
                                value={teamID}
                                onChange={(e) => setTeamID(e.target.value)}
                                style={{ maxWidth: 200 }}
                            />
                            <div className="d-flex flex-wrap gap-2">
                                <button
                                    className="btn btn-outline-warning btn-sm"
                                    onClick={handleKickPlayer}
                                >
                                    Kick
                                </button>
                                <button
                                    className="btn btn-outline-danger btn-sm"
                                    onClick={handleBanPlayer}
                                >
                                    Ban
                                </button>
                                <button
                                    className="btn btn-outline-success btn-sm"
                                    onClick={handleUnbanPlayer}
                                >
                                    Unban
                                </button>
                                <button
                                    className="btn btn-outline-info btn-sm"
                                    onClick={handleEditStats}
                                >
                                    Edit Stats
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
                            <button
                                className="btn btn-outline-secondary btn-sm"
                                onClick={handleArchiveSeason}
                            >
                                Archive Season
                            </button>
                            <button
                                className="btn btn-outline-warning btn-sm"
                                onClick={handleResetLeaderboard}
                            >
                                Reset Leaderboard
                            </button>
                            <button
                                className="btn btn-outline-success btn-sm"
                                onClick={handleSyncHistory}
                            >
                                Sync Player History
                            </button>
                            <button
                                className="btn btn-outline-info btn-sm"
                                onClick={handleRebuildELO}
                            >
                                Rebuild ELO
                            </button>
                        </div>
                    }
                />
            </div>
        </div>
    );
}

// Accordion item helper
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
            <div
                id={id}
                className="accordion-collapse collapse"
                data-bs-parent="#modAccordion"
            >
                <div className="accordion-body">{children}</div>
            </div>
        </div>
    );
}
