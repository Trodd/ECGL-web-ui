import { useEffect, useState } from "react";
import axios from "axios";
import MatchCard from "../components/MatchCard";

export default function MyTeam() {
  const [data, setData] = useState({
    team: {},
    roster: [],
    matches: [],
    requests: [],
    myRole: "",
  });
  const [loading, setLoading] = useState(true);
  const [confirmLeave, setConfirmLeave] = useState(false);
  const [msg, setMsg] = useState("");
  const [selectedSeason, setSelectedSeason] = useState("All");
  const [currentSeason, setCurrentSeason] = useState("Preseason");
  const [newTeamName, setNewTeamName] = useState("");
  const [teamSettings, setTeamSettings] = useState({});
  const [challengeRequests, setChallengeRequests] = useState([]);
  const [allowChallenges, setAllowChallenges] = useState(true);
  const [globalChallengesEnabled, setGlobalChallengesEnabled] = useState(true);

  const urlBase = import.meta.env.VITE_API_URL;
  const sectionStyle = {
    backgroundColor: "#1a1a1a",
    border: "1px solid #333",
    borderRadius: "0.5rem",
    padding: "1rem",
    width: "100%",
    maxWidth: "800px",
    margin: "0 auto 1.5rem auto",
  };

  const [accordionOpen, setAccordionOpen] = useState(
    localStorage.getItem("accordion_team_settings_open") === "true"
  );

  useEffect(() => {
    const saved = localStorage.getItem("accordion_team_settings_open");
    if (saved === "true") setAccordionOpen(true);
  }, []);

  const [rosterLocked, setRosterLocked] = useState(false);

  useEffect(() => {
    async function loadRosterLockStatus() {
      try {
        const res = await axios.get(`${urlBase}/api/mod/roster/status`, {
          withCredentials: true,
        });
        setRosterLocked(!!res.data.locked);
      } catch {
        setRosterLocked(false);
      }
    }
    loadRosterLockStatus();
  }, []);

  // 🔄 Load global challenge setting
  useEffect(() => {
    axios
      .get(`${urlBase}/api/settings`, { withCredentials: true })
      .then(res => {
        if (res.data?.challenges_enabled !== undefined) {
          setGlobalChallengesEnabled(res.data.challenges_enabled);
        }
      })
      .catch(() => {
        setGlobalChallengesEnabled(true);
      });
  }, []);

  useEffect(() => {
    axios.get(`${urlBase}/api/myteam`, { withCredentials: true })
      .then(res => {
        setTeamSettings(res.data.team);
        setAllowChallenges(!!res.data.team?.allow_challenges);
        setChallengeRequests(res.data.challenge_requests || []);
      })
      .catch(() => {
        setTeamSettings({});
        setChallengeRequests([]);
      });
  }, []);

  async function loadTeam() {
    try {
      setLoading(true);
      // 🧩 Support ?as=<discord_id> override in DEV_MODE
      const query = new URLSearchParams(window.location.search);
      const asParam = query.get("as");
      const url = asParam
        ? `${urlBase}/api/myteam?as=${asParam}`
        : `${urlBase}/api/myteam`;

      const res = await axios.get(url, { withCredentials: true });

      setData({
        team: res.data?.team || {},
        roster: Array.isArray(res.data?.roster) ? res.data.roster : [],
        matches: Array.isArray(res.data?.matches) ? res.data.matches : [],
        requests: Array.isArray(res.data?.requests) ? res.data.requests : [],
        myRole: res.data?.myRole || "",
      });

      setTeamSettings(res.data.team || {});
      setAllowChallenges(res.data.team?.allow_challenges ?? true);
      setChallengeRequests(res.data.challenge_requests || []);

    } catch (err) {
      console.error("❌ Failed to load MyTeam:", err);
      setData({
        team: {},
        roster: [],
        matches: [],
        requests: [],
        myRole: "",
      });
      setMsg("⚠️ Failed to load team data.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadTeam();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const { team, roster, matches, requests, myRole } = data;
  const isLockedByMods = Boolean(team?.locked);
  const joinDisabled = rosterLocked || isLockedByMods;
  const isCaptain = myRole === "Captain" || myRole === "Co-Captain";

  // 🔄 Load current season label (from backend)
  useEffect(() => {
    axios
      .get(`${urlBase}/api/season`)
      .then((res) => {
        if (res.data?.season) {
          let s = res.data.season.toString().trim();
          if (/^\d+$/.test(s)) s = `Season ${s}`;
          setCurrentSeason(s);
        }
      })
      .catch(() => setCurrentSeason("Preseason"));
  }, []);


  if (loading) return <p>⏳ Fetching team data…</p>;
  if (!team?.id) return <p>❌ You are not in a team.</p>;

  // Build list of season options
  const allSeasons = ["All", "Preseason"];
  if (currentSeason && !allSeasons.includes(currentSeason))
    allSeasons.push(currentSeason);

  // Extract finished past matches OR previous-season matches
  const filteredPastMatches = (matches ?? []).filter((m) => {
    // Normalize backend season
    let seasonNum = String(m.season ?? "").trim();

    // If backend season missing → derive from match_code
    if (!seasonNum) {
      const prefix = (m.match_code ?? "").split("-")[0];
      seasonNum = /^\d+$/.test(prefix) ? prefix : "0";  // 0 = preseason
    }

    // Normalize current season for comparison
    const currentSeasonNum = currentSeason.replace("Season ", "").trim();

    // Determine finished match
    const isFinished = ["Finished", "Completed", "Forfeit", "Cancelled"]
      .includes((m.status ?? "").trim());

    // A match is PAST if:
    // - finished, OR
    // - season does not match current season
    const isPast = isFinished || seasonNum !== currentSeasonNum;
    if (!isPast) return false;

    // Apply dropdown filter
    if (selectedSeason === "All") return true;
    if (selectedSeason === "Preseason") return seasonNum === "0";

    const selectedNum = selectedSeason.replace("Season ", "").trim();
    return seasonNum === selectedNum;
  });


  // --- Actions (with basic crash prevention) ---

  async function handleDecision(requestID, action) {
    if (!requestID || !action) return;
    try {
      await axios.post(
        `${urlBase}/api/team/join/decision`,
        { request_id: requestID, action },
        { withCredentials: true }
      );
      await loadTeam();
    } catch (err) {
      console.error("❌ Failed to update request:", err);
      alert("Failed to update request");
    }
  }

  async function handleKick(playerId) {
    if (!playerId || !team?.id) return;
    try {
      await axios.post(
        `${urlBase}/api/team/kick`,
        { team_id: team.id, player_id: String(playerId) }, // ✅ string
        { withCredentials: true }
      );
      await loadTeam();
    } catch (err) {
      console.error("Kick failed:", err);
      alert("❌ Failed to kick player");
    }
  }

  async function handleLeaveTeam() {
    if (!team?.id) return;
    try {
      await axios.post(
        `${urlBase}/api/team/leave`,
        { team_id: team.id },
        { withCredentials: true }
      );
      alert("✅ You have left the team.");
      setConfirmLeave(false);
      await loadTeam();
    } catch (err) {
      console.error("Leave failed:", err);
      alert("❌ Failed to leave team");
    }
  }

  async function handlePromote(playerId, role) {
    if (!playerId || !role || !team?.id) return;
    try {
      await axios.post(
        `${urlBase}/api/team/promote`,
        { team_id: team.id, player_id: String(playerId), role }, // ✅ string
        { withCredentials: true }
      );
      await loadTeam();
    } catch (err) {
      console.error("Promote failed:", err);
      alert("❌ Failed to promote player");
    }
  }

  async function respondChallenge(id, accept) {
    try {
      await axios.post(
        `${urlBase}/api/challenge/respond`,
        { challenge_id: id, accept },
        { withCredentials: true }
      );

      loadTeam();  // reload MyTeam data
    } catch (err) {
      alert("Error updating challenge: " + err.response?.data);
    }
  }

  return (
    <div
      className="d-flex flex-column align-items-center text-center text-light"
      style={{ width: "100%", minHeight: "100vh", padding: "2rem 1rem" }}
    >
      <div style={{ maxWidth: "900px", width: "100%" }}>
        <h2>🧑{team.name}</h2>
        <p>Status: {team.status || "Active"}</p>

        {roster.some(p => p.on_cooldown && p.role === myRole) && (
          <div className="alert alert-warning small mt-2">
            ⏳ You recently left a team. You cannot play matches for your new team until the next matchup cycle.
          </div>
        )}

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

        {team?.id &&
          (!confirmLeave ? (
            <button
              className="btn btn-outline-danger btn-sm mb-3"
              onClick={() => setConfirmLeave(true)}
            >
              🚪 Leave Team
            </button>
          ) : (
            <div className="d-flex gap-2 mb-3">
              <button className="btn btn-danger btn-sm" onClick={handleLeaveTeam}>
                ✅ Confirm Leave
              </button>
              <button
                className="btn btn-secondary btn-sm"
                onClick={() => setConfirmLeave(false)}
              >
                ❌ Cancel
              </button>
            </div>
          ))}

        {/* ⚙️ Captain/Co-Captain Settings Accordion with Persistent State */}
        {(myRole === "Captain" || myRole === "Co-Captain") && (
          <div
            className="accordion mb-4"
            id="teamSettingsAccordion"
            style={{ maxWidth: "800px", margin: "0 auto" }}
          >
            <div className="accordion-item bg-dark text-light border-secondary rounded-3 overflow-hidden shadow">
              <h2 className="accordion-header">
                <button
                  className={`accordion-button ${accordionOpen ? "" : "collapsed"
                    } bg-dark text-light fw-semibold`}
                  type="button"
                  onClick={() => {
                    const next = !accordionOpen;
                    setAccordionOpen(next);
                    localStorage.setItem("accordion_team_settings_open", next ? "true" : "false");
                  }}
                >
                  ⚙️ Team Settings
                </button>
              </h2>

              <div
                id="teamSettingsCollapse"
                className={`accordion-collapse collapse ${accordionOpen ? "show" : ""}`}
                data-bs-parent="#teamSettingsAccordion"
              >
                <div className="accordion-body bg-black text-light">

                  {/* 🧢 Rename Team */}
                  {myRole === "Captain" && (
                    <div className="mb-4 border-bottom border-secondary pb-3">
                      <h6 className="text-info mb-2">✏️ Rename Team</h6>
                      <div className="d-flex align-items-center gap-2 flex-wrap">
                        <input
                          type="text"
                          className="form-control form-control-sm bg-dark text-light"
                          placeholder="New name..."
                          style={{ width: "200px" }}
                          value={newTeamName}
                          onChange={(e) => setNewTeamName(e.target.value)}
                        />
                        <button
                          className="btn btn-outline-info btn-sm"
                          onClick={async () => {
                            if (!newTeamName.trim())
                              return alert("Enter a new team name first");
                            try {
                              await axios.post(
                                `${urlBase}/api/team/rename`,
                                { team_id: team.id, new_name: newTeamName.trim() },
                                { withCredentials: true }
                              );
                              alert("✅ Team renamed successfully!");
                              await loadTeam();
                              setNewTeamName("");
                            } catch (err) {
                              console.error("❌ Rename failed:", err);
                              alert(err.response?.data || "Failed to rename team");
                            }
                          }}
                        >
                          Save
                        </button>
                      </div>
                    </div>
                  )}

                  {/* 🟢 Team Status Toggle */}
                  <div className="mb-4 border-bottom border-secondary pb-3">
                    <h6 className="text-warning mb-2">🏁 Team Status</h6>
                    <div className="d-flex align-items-center gap-2">
                      <div className="form-check form-switch m-0">
                        <input
                          className="form-check-input"
                          type="checkbox"
                          checked={team.status === "Active"}
                          onChange={async (e) => {
                            const nextStatus = e.target.checked ? "Active" : "Inactive";
                            try {
                              await axios.post(
                                `${urlBase}/api/team/toggle-status`,
                                { team_id: team.id, status: nextStatus },
                                { withCredentials: true }
                              );
                              await loadTeam();
                            } catch (err) {
                              console.error("❌ Failed to update status:", err);
                              alert("Failed to update team status");
                            }
                          }}
                        />
                      </div>
                      <label className="text-light small ms-1">
                        {team.status === "Active"
                          ? "✅ Active / Match-Eligible"
                          : "⛔ Inactive / Hidden"}
                      </label>
                    </div>
                  </div>

                  {/* 🔒 Allow Join Requests */}
                  <div className="mb-4 border-bottom border-secondary pb-3">
                    <h6 className="text-success mb-2">👥 Join Requests</h6>

                    <div className="d-flex align-items-center gap-2">

                      {/* SWITCH WRAPPER — disable ONLY the switch */}
                      <div
                        className="form-check form-switch m-0"
                        style={joinDisabled ? { opacity: 0.5, pointerEvents: "none" } : {}}
                      >
                        <input
                          className="form-check-input"
                          type="checkbox"
                          checked={!!team.join_allowed}
                          disabled={joinDisabled}
                          onChange={async (e) => {
                            if (joinDisabled) return;

                            const newAllow = e.target.checked;

                            try {
                              setData(prev => ({
                                ...prev,
                                team: { ...prev.team, join_allowed: newAllow },
                              }));

                              const res = await axios.post(
                                `${urlBase}/api/team/toggle-join`,
                                { team_id: team.id, allow: newAllow },
                                { withCredentials: true }
                              );

                              if (!res.data?.success) throw new Error("Backend rejected");
                              await loadTeam();
                            } catch (err) {
                              console.error("❌ Toggle failed:", err);
                              setData(prev => ({
                                ...prev,
                                team: { ...prev.team, join_allowed: !newAllow },
                              }));
                            }
                          }}
                        />
                      </div>

                      {/* LABEL — always explain why it's disabled */}
                      <label className="form-check-label text-light small ms-2">
                        {joinDisabled ? (
                          <>
                            {isLockedByMods ? (
                              <span className="text-warning">🔒 Your team roster is locked.</span>
                            ) : rosterLocked ? (
                              <span className="text-warning">🔒 Rosters are Locked league-wide.</span>
                            ) : null}
                          </>
                        ) : (
                          // Normal active label
                          <>
                            {team.join_allowed ? "✅ Allowed" : "🚫 Disabled"}
                          </>
                        )}
                      </label>
                    </div>
                  </div>
                  {/* 🏆 Challenge Match Availability */}
                  <div className="mb-4 border-bottom border-secondary pb-3">
                    <h6 className="text-info mb-2">🏆 Challenge Match Availability</h6>

                    <div className="d-flex align-items-center gap-2">
                      <div className="form-check form-switch m-0">
                        <input
                          className="form-check-input"
                          type="checkbox"
                          checked={allowChallenges}
                          disabled={
                            team.status !== "Active" ||               // Team inactive
                            !globalChallengesEnabled ||         // Global disabled
                            !isCaptain                                // Only captains can toggle
                          }
                          onChange={async (e) => {
                            if (!globalChallengesEnabled) {
                              return alert("🚫 League mods have globally disabled challenge matches.");
                            }

                            if (team.status !== "Active") {
                              return alert("🚫 Your team must be active to enable challenges.");
                            }

                            if (!isCaptain) {
                              return alert("⛔ Only the captain or co-captain can toggle this.");
                            }

                            const next = e.target.checked;

                            try {
                              await axios.post(
                                `${urlBase}/api/team/toggle-challenges`,
                                { team_id: team.id, allow: next },
                                { withCredentials: true }
                              );
                              setAllowChallenges(next);
                            } catch (err) {
                              console.error("Toggle challenges failed:", err);
                              alert("❌ Failed to update challenge toggle.");
                            }
                          }}
                        />
                      </div>

                      <label className="form-check-label text-light small ms-2">
                        {!globalChallengesEnabled
                          ? "⚠️ Challenge matches are currently disabled league-wide."
                          : allowChallenges
                            ? "✅ Accepting Challenge Matches"
                            : "🚫 Not Accepting Challenges"}
                      </label>
                    </div>

                    {/* Inactive Team Warning */}
                    {team.status !== "Active" && (
                      <p className="text-warning small mt-2 mb-0">
                        ⚠️ Team is <b>Inactive</b>. Challenge matches are disabled automatically.
                      </p>
                    )}
                  </div>
                </div>
              </div>
            </div>
          </div>)}

        {/* 👥 Roster - bounded width */}
        <h4>👥 Roster</h4>
        <div style={sectionStyle}>
          <ul className="list-group">
            {(roster ?? []).length > 0 ? (
              roster.map((m) => (
                <li
                  key={`player-${m.id}`}
                  className="list-group-item d-flex justify-content-between align-items-center px-3 py-2"
                  style={{
                    borderRadius: "0.5rem",
                    marginBottom: "6px",
                    backgroundColor: "#1c1c1c",
                  }}
                >
                  <span>
                    <strong>{m.display_name || m.username || "Unknown"}</strong>{" "}
                    <span
                      className={`roster-role ${m.role?.toLowerCase() || ""}`}
                    >
                      {m.role || "-"}
                    </span>
                    {/* ⭐ Cooldown Badge */}
                    {m.on_cooldown && (
                      <span className="badge bg-warning text-dark ms-2">
                        ⏳ Cooldown
                      </span>
                    )}
                  </span>

                  {myRole === "Captain" && m.role !== "Captain" && (
                    <div className="d-flex align-items-center gap-2">
                      <select
                        className="form-select form-select-sm bg-dark text-light"
                        style={{ width: 160 }}
                        defaultValue=""
                        onChange={async (e) => {
                          const newRole = e.target.value;
                          if (!newRole) return;
                          await handlePromote(m.id, newRole);
                          e.target.value = "";
                        }}
                      >
                        <option value="" disabled>
                          Promote to...
                        </option>
                        <option value="Captain">Captain</option>
                        <option value="Co-Captain">Co-Captain</option>
                        <option value="Member">Member</option>
                      </select>
                      <button
                        className="btn btn-danger btn-sm"
                        onClick={() => handleKick(m.id)}
                      >
                        Kick
                      </button>
                    </div>
                  )}
                </li>
              ))
            ) : (
              <li className="list-group-item">No members yet</li>
            )}
          </ul>
        </div>
        {/* 📥 + ⚔️ SIDE-BY-SIDE REQUESTS */}
        {(myRole === "Captain" || myRole === "Co-Captain") && (
          <div
            className="d-flex flex-wrap gap-3 justify-content-center mt-4"
            style={{ width: "100%", maxWidth: "900px" }}
          >
            {/* LEFT COLUMN — JOIN REQUESTS */}
            <div
              className="p-3 rounded"
              style={{
                backgroundColor: "#1a1a1a",
                border: "1px solid #333",
                flex: "1 1 300px",
                maxWidth: "420px",
              }}
            >
              <h4 className="mb-3 text-light">📥 Join Requests</h4>

              {requests.length === 0 ? (
                <p className="text-light small">No join requests.</p>
              ) : (
                <ul className="list-group">
                  {requests.map((req) => (
                    <li
                      key={req.id}
                      className="list-group-item bg-dark text-light d-flex justify-content-between align-items-center px-2 py-2"
                      style={{ borderRadius: "0.35rem", marginBottom: "4px" }}
                    >
                      <span>
                        <strong>{req.display_name || req.username || "Unknown Player"}</strong>
                      </span>

                      <div className="d-flex gap-1">
                        <button
                          className="btn btn-success btn-sm"
                          onClick={() => handleDecision(req.id, "accept")}
                        >
                          ✓
                        </button>
                        <button
                          className="btn btn-danger btn-sm"
                          onClick={() => handleDecision(req.id, "deny")}
                        >
                          ✕
                        </button>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            {/* RIGHT COLUMN — CHALLENGE REQUESTS */}
            <div
              className="p-3 rounded"
              style={{
                backgroundColor: "#1a1a1a",
                border: "1px solid #333",
                flex: "1 1 300px",
                maxWidth: "420px",
              }}
            >
              <h4 className="mb-3 text-light">⚔️ Challenge Requests</h4>

              {challengeRequests.length === 0 ? (
                <p className="text-light small">No challenge requests.</p>
              ) : (
                challengeRequests.map((req) => (
                  <div
                    key={req.id}
                    className="p-3 mb-2 border rounded bg-dark text-light shadow-sm"
                    style={{ borderColor: "#555" }}
                  >
                    <p className="mb-2">
                      <strong>{req.requester_team_name}</strong> has challenged your team (Week{" "}
                      {req.week}).
                    </p>

                    <button
                      className="btn btn-success btn-sm me-2"
                      onClick={() => respondChallenge(req.id, true)}
                    >
                      Accept
                    </button>

                    <button
                      className="btn btn-danger btn-sm"
                      onClick={() => respondChallenge(req.id, false)}
                    >
                      Deny
                    </button>
                  </div>
                ))
              )}
            </div>
          </div>
        )}
        {/* 📅 Active Matches */}
        <h4 className="mt-4 mb-3">📅 Active Matches</h4>
        <div className="d-flex flex-column align-items-center gap-3 w-100">
          {(() => {
            const activeMatches = (matches ?? []).filter((m) => {
              if (!m) return false;

              // -------- Season normalization (same as backend) --------
              let seasonNum = String(m.season ?? "").trim().toLowerCase();

              if (!seasonNum || seasonNum === "preseason") {
                // infer from match_code
                const prefix = (m.match_code ?? "").split("-")[0];
                seasonNum = /^\d+$/.test(prefix) ? prefix : "0"; // preseason = 0
              }

              if (seasonNum.startsWith("season ")) {
                seasonNum = seasonNum.replace("season ", "");
              }

              // Normalize current season ("Season 3" → "3")
              const currentSeasonNum =
                currentSeason.replace("Season ", "").trim() || "0";

              // Finished = NOT active
              const isFinished = ["Finished", "Completed", "Forfeit", "Cancelled"]
                .includes((m.status ?? "").trim());

              return seasonNum === currentSeasonNum && !isFinished;
            });

            if (activeMatches.length === 0)
              return <p className="text-light">No active matches.</p>;

            return activeMatches.map((m) => (
              <div
                key={m.id}
                className="p-3 border rounded bg-dark shadow-sm"
                style={{ width: "100%", maxWidth: 700, borderColor: "#444" }}
              >
                <h5 className="text-light mb-2">
                  {m.match_code || `Match #${m.id}`} —{" "}
                  <span className="text-info">vs {m.opponent}</span>
                </h5>

                <p className="text-light mb-2">
                  Scheduled:{" "}
                  {m.date
                    ? new Date(m.date).toLocaleString([], {
                      dateStyle: "medium",
                      timeStyle: "short",
                    })
                    : "Not scheduled yet"}
                </p>

                {myRole === "Captain" || myRole === "Co-Captain" ? (
                  <MatchCard
                    match={m}
                    team={team}
                    urlBase={urlBase}
                    loadTeam={loadTeam}
                    myRole={myRole}
                  />
                ) : (
                  <p className="text-secondary small mb-0">
                    Waiting for captains to manage match details.
                  </p>
                )}
              </div>
            ));
          })()}
        </div>

        {/* 🏁 Past Matches */}
        <section className="mt-4 mb-5">
          <div
            className="d-flex justify-content-between align-items-center mb-2"
            style={{
              maxWidth: "720px",
              margin: "0 auto",
              paddingLeft: "12px",
            }}
          >
            <h4 className="text-light mb-0">🏁 Past Matches</h4>

            <select
              className="form-select form-select-sm bg-dark text-light"
              style={{ maxWidth: 200 }}
              value={selectedSeason}
              onChange={(e) => setSelectedSeason(e.target.value)}
            >
              {allSeasons.map((s) => (
                <option key={s}>{s}</option>
              ))}
            </select>
          </div>

          <div className="table-responsive rounded shadow-sm" style={sectionStyle}>
            <table className="table table-dark table-striped align-middle text-center table-hover mb-0">
              <thead className="table-secondary">
                <tr>
                  <th>#</th>
                  <th>Opponent</th>
                  <th>Date</th>
                  <th>Result</th>
                  <th>Status</th>
                  <th>Match ID</th>
                </tr>
              </thead>

              <tbody>
                {(matches ?? [])
                  .filter((m) => {
                    if (!m) return false;

                    // --- Normalize season ---
                    let seasonNum = String(m.season ?? "").trim().toLowerCase();
                    const prefix = (m.match_code ?? "").split("-")[0];

                    if (!seasonNum) {
                      seasonNum = /^\d+$/.test(prefix) ? prefix : "0";
                    }
                    if (seasonNum.startsWith("season ")) {
                      seasonNum = seasonNum.replace("season ", "");
                    }

                    const currentSeasonNum =
                      currentSeason.replace("Season ", "").trim() || "0";

                    const isFinished = ["Finished", "Completed", "Forfeit", "Cancelled"]
                      .includes((m.status ?? "").trim());

                    const isPast = isFinished || seasonNum !== currentSeasonNum;
                    if (!isPast) return false;

                    // APPLY FILTER DROPDOWN
                    if (selectedSeason === "All") return true;
                    if (selectedSeason === "Preseason") return seasonNum === "0";

                    const selectedNum = selectedSeason.replace("Season ", "").trim();
                    return seasonNum === selectedNum;
                  })
                  .map((m, idx) => (
                    <tr
                      key={m.id || idx}
                      className="match-row"
                      style={{ cursor: "pointer" }}
                      onClick={() => (window.location.href = `/match/${m.id}`)}
                    >
                      <td>{idx + 1}</td>
                      <td className="fw-semibold">{m.opponent || "Unknown"}</td>
                      <td>{m.date ? new Date(m.date).toLocaleDateString() : "-"}</td>
                      <td
                        className={`fw-bold ${m.result === "Win"
                          ? "text-success"
                          : m.result === "Loss"
                            ? "text-danger"
                            : "text-warning"
                          }`}
                      >
                        {m.result || "Pending"}
                      </td>
                      <td>{m.status}</td>
                      <td>{m.match_code || m.id}</td>
                    </tr>
                  ))}

                {filteredPastMatches.length === 0 && (
                  <tr>
                    <td colSpan="6" className="text-light py-3">
                      No matches found for {selectedSeason}.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </div >
  );
}

