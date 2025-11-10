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

  // Filter matches by season
  const pastMatches = (matches ?? []).filter((m) =>
    ["Finished", "Completed", "Forfeit", "Cancelled"].includes(m.status?.trim())
  );

  // season filter based on match_code prefix
  const filteredPastMatches = pastMatches.filter((m) => {
    if (selectedSeason === "All") return true;

    const code = m.match_code || "";
    const prefix = code.split("-")[0]?.trim();

    // detect season number prefix (e.g. "1", "2", "3")
    const seasonNum = /^\d+$/.test(prefix) ? Number(prefix) : null;

    if (selectedSeason === "Preseason") {
      return !seasonNum; // no number → preseason
    }

    // extract number from "Season X"
    const seasonNumSelected = parseInt(selectedSeason.replace(/\D/g, ""), 10);
    return seasonNum === seasonNumSelected;
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
        { team_id: team.id, player_id: String(playerId) },
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
        { team_id: team.id, player_id: String(playerId), role },
        { withCredentials: true }
      );
      await loadTeam();
    } catch (err) {
      console.error("Promote failed:", err);
      alert("❌ Failed to promote player");
    }
  }

  return (
    <div
      className="d-flex flex-column align-items-center text-center text-light"
      style={{ width: "100%", minHeight: "100vh", padding: "2rem 1rem" }}
    >
      <div style={{ maxWidth: "900px", width: "100%" }}>
        <h2>🧑 My Team: {team.name}</h2>
        <p>Status: {team.status || "Active"}</p>

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

        {/* ⚙️ Captain Settings - compact card */}
        {(myRole === "Captain" || myRole === "Co-Captain") && (
          <div className="mb-3" style={sectionStyle}>

            <h5 className="text-light mb-3">⚙️ Team Settings</h5>

            {/* 🟢 Toggle Active/Inactive */}
            <div className="d-flex align-items-center justify-content-between mb-3">
              <label className="text-light me-2 mb-0 fw-light">Status</label>
              <select
                className="form-select form-select-sm bg-dark text-light w-auto"
                style={{ minWidth: "120px" }}
                value={team.status}
                onChange={async (e) => {
                  const nextStatus = e.target.value;
                  if (!nextStatus) return;
                  try {
                    await axios.post(
                      `${urlBase}/api/team/toggle-status`,
                      { team_id: team.id, status: nextStatus },
                      { withCredentials: true }
                    );
                    await loadTeam();
                  } catch (err) {
                    console.error("❌ Failed to update status:", err);
                    alert("Failed to update status");
                  }
                }}
              >
                <option value="Active">Active</option>
                <option value="Inactive">Inactive</option>
              </select>
            </div>

            {/* 🔒 Allow Join Requests */}
            <div className="d-flex align-items-center justify-content-between">
              <label className="text-light me-2 mb-0 fw-light">Join Requests</label>
              <div className="form-check form-switch m-0">
                <input
                  className="form-check-input"
                  type="checkbox"
                  checked={!!team.join_allowed}
                  onChange={async (e) => {
                    const newAllow = e.target.checked;
                    if (!team?.id) return;

                    try {
                      // optimistic local update
                      setData((prev) => ({
                        ...prev,
                        team: { ...prev.team, join_allowed: newAllow },
                      }));

                      const res = await axios.post(
                        `${urlBase}/api/team/toggle-join`,
                        { team_id: team.id, allow: newAllow },
                        { withCredentials: true }
                      );

                      if (!res.data?.success) {
                        throw new Error("Backend rejected");
                      }

                      await loadTeam();
                    } catch (err) {
                      console.error("❌ Failed to toggle join:", err);
                      alert("Failed to update join setting");

                      // revert on failure
                      setData((prev) => ({
                        ...prev,
                        team: { ...prev.team, join_allowed: !newAllow },
                      }));
                    }
                  }}
                />
                <label className="form-check-label text-light small ms-2">
                  {team.join_allowed ? "Allowed" : "Blocked"}
                </label>
              </div>
            </div>
          </div>
        )}

        {/* 👥 Roster - bounded width */}
        <h4>👥 Roster</h4>
        <div style={sectionStyle}>
          <ul className="list-group">
            {(roster ?? []).length > 0 ? (
              roster.map((m) => (
                <li
                  key={m.id}
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

        {/* 📅 Active Matches */}
        <h4 className="mt-4 mb-3">📅 Active Matches</h4>
        <div className="d-flex flex-column align-items-center gap-3 w-100">
          {(() => {
            const activeMatches = (matches ?? []).filter(
              (m) =>
                m?.status &&
                !["Finished", "Forfeit", "Completed", "Cancelled"].includes(
                  m.status.trim()
                )
            );

            if (activeMatches.length === 0)
              return <p className="text-light">No active matches.</p>;

            return activeMatches.map((m) => (
              <div
                key={m.id}
                className="p-3 border rounded bg-dark shadow-sm"
                style={{
                  width: "100%",
                  maxWidth: 700,
                  borderColor: "#444",
                }}
              >
                <h5 className="text-light mb-2">
                  {m.match_code || `Match #${m.id}`} —{" "}
                  <span className="text-info">vs {m.opponent}</span>
                </h5>

                {/* 🕒 Always show date/time */}
                <p className="text-light mb-2">
                  Scheduled:{" "}
                  {m.date
                    ? new Date(m.date).toLocaleString([], {
                      dateStyle: "medium",
                      timeStyle: "short",
                    })
                    : "Not scheduled yet"}
                </p>

                {/* 🧑 Captains get full MatchCard with edit options */}
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
              paddingLeft: "12px", // ✅ slight nudge right
            }}
          >

            <h4 className="text-light mb-0">🏁 Past Matches</h4>

            {/* 🔽 Season Filter */}
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
              <thead
                className="table-secondary"
                style={{
                  borderTopLeftRadius: "8px",
                  borderTopRightRadius: "8px",
                }}
              >
                <tr>
                  <th style={{ width: "5%" }}>#</th>
                  <th style={{ width: "25%" }}>Opponent</th>
                  <th style={{ width: "20%" }}>Date</th>
                  <th style={{ width: "15%" }}>Result</th>
                  <th style={{ width: "15%" }}>Status</th>
                  <th style={{ width: "20%" }}>Match ID</th>
                </tr>
              </thead>
              <tbody>
                {filteredPastMatches.length > 0 ? (
                  filteredPastMatches.map((m, idx) => (
                    <tr
                      key={m.id || idx}
                      className="match-row"
                      style={{
                        cursor: "pointer",
                        transition: "background 0.2s ease",
                      }}
                      onClick={() => (window.location.href = `/match/${m.id}`)}
                      title="View match details"
                    >
                      <td>{idx + 1}</td>
                      <td className="fw-semibold">{m.opponent || "Unknown"}</td>
                      <td style={{ whiteSpace: "nowrap" }}>
                        {m.date ? new Date(m.date).toLocaleDateString() : "-"}
                      </td>
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
                      <td className="text-light">
                        {m.status || "Unknown"}
                      </td>
                      <td className="text-light">{m.match_code || m.id}</td>
                    </tr>
                  ))
                ) : (
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

        {/* 📥 Join Requests - compact + aligned with settings */}
        {(myRole === "Captain" || myRole === "Co-Captain") &&
          (requests ?? []).length > 0 && (
            <div
              className="mt-4 p-3 rounded"
              style={{
                backgroundColor: "#1a1a1a",
                border: "1px solid #333",
                maxWidth: "420px", // ✅ same width as settings
              }}
            >
              <h4 className="mb-3 text-light">📥 Join Requests</h4>
              <ul className="list-group">
                {requests.map((req) => (
                  <li
                    key={req.id}
                    className="list-group-item d-flex justify-content-between align-items-center px-2 py-2"
                    style={{
                      backgroundColor: "#111",
                      color: "#f8f9fa",
                      borderRadius: "0.35rem",
                      marginBottom: "4px",
                    }}
                  >
                    <span>
                      <strong>{req.username}</strong>{" "}
                      <span className="text-muted small">
                        ({req.status || "pending"})
                      </span>
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
            </div>
          )}
      </div>
    </div>
  );
}

