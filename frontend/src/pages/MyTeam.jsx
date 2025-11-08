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
  const urlBase = import.meta.env.VITE_API_URL;

  async function loadTeam() {
    try {
      setLoading(true);
      const res = await axios.get(`${urlBase}/api/myteam`, {
        withCredentials: true,
      });

      setData({
        team: res.data.team || {},
        roster: res.data.roster ?? [],
        matches: res.data.matches ?? [],
        requests: res.data.requests ?? [],
        myRole: res.data.myRole || "",
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
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadTeam();
  }, []);

  const { team, roster, matches, requests, myRole } = data;

  if (loading) return <p>⏳ Fetching team data…</p>;
  if (!team?.id) return <p>❌ You are not in a team.</p>;

  async function handleDecision(requestID, action) {
    try {
      await axios.post(
        `${urlBase}/api/team/join/decision`,
        { request_id: requestID, action },
        { withCredentials: true }
      );
      await loadTeam();
    } catch {
      alert("Failed to update request");
    }
  }

  async function handleKick(playerId) {
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
    <div>
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

      {/* ⚙️ Captain Settings */}
      {(myRole === "Captain" || myRole === "Co-Captain") && (
        <div
          className="p-3 mb-4 rounded"
          style={{
            backgroundColor: "#1a1a1a",
            border: "1px solid #333",
            maxWidth: "400px", // ✅ Keeps it narrow
          }}
        >
          <h5 className="text-light mb-3">⚙️ Team Settings</h5>

          {/* 🟢 Toggle Active/Inactive */}
          <div className="d-flex align-items-center justify-content-between mb-3">
            <label className="text-light me-2 mb-0 fw-light">Status</label>
            <select
              className="form-select form-select-sm bg-dark text-light w-auto"
              style={{ minWidth: "120px" }}
              value={team.status}
              onChange={async (e) => {
                try {
                  await axios.post(
                    `${urlBase}/api/team/toggle-status`,
                    { team_id: team.id, status: e.target.value },
                    { withCredentials: true }
                  );
                  await loadTeam();
                } catch {
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
                  try {
                    setData((prev) => ({
                      ...prev,
                      team: { ...prev.team, join_allowed: newAllow },
                    }));

                    const res = await axios.post(
                      `${urlBase}/api/team/toggle-join`,
                      { team_id: team.id, allow: newAllow },
                      { withCredentials: true }
                    );

                    if (res.data?.success) {
                      await loadTeam();
                    } else throw new Error("Backend rejected");
                  } catch (err) {
                    console.error("❌ Failed to toggle join:", err);
                    alert("Failed to update join setting");
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

      <h4>👥 Roster</h4>
      <div style={{ maxWidth: "700px" }}>
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
                  <span className={`roster-role ${m.role?.toLowerCase() || ""}`}>
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
                        const role = e.target.value;
                        if (!role) return;
                        await handlePromote(m.id, role);
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

      <h4 className="mt-4 mb-3">📅 Matches</h4>
      <div className="d-flex flex-column align-items-start gap-3">
        {(() => {
          // filter out forfeited, finished, or completed matches
          const activeMatches = (matches ?? []).filter(
            (m) =>
              m.status &&
              !["Finished", "Forfeit", "Completed", "Cancelled"].includes(
                m.status.trim()
              )
          );

          return activeMatches.length > 0 ? (
            activeMatches.map((m) => (
              <MatchCard
                key={m.id}
                match={m}
                team={team}
                urlBase={urlBase}
                loadTeam={loadTeam}
                myRole={myRole}
              />
            ))
          ) : (
            <p className="text-light">No active matches.</p>
          );
        })()}
      </div>

      <h5 className="mt-4">🏁 Past Matches</h5>
      <div className="d-flex flex-column align-items-start gap-3">
        {(matches ?? [])
          .filter((m) =>
            ["Finished", "Completed", "Forfeit", "Cancelled"].includes(
              m.status?.trim()
            )
          )
          .map((m) => (
            <MatchCard
              key={m.id}
              match={m}
              team={team}
              urlBase={urlBase}
              loadTeam={loadTeam}
              myRole={myRole}
              readOnly={true}
            />
          ))}
      </div>

      {(myRole === "Captain" || myRole === "Co-Captain") &&
        (requests ?? []).length > 0 && (
          <>
            <h4 className="mt-4">📥 Join Requests</h4>
            <ul className="list-group">
              {requests.map((req) => (
                <li
                  key={req.id}
                  className="list-group-item d-flex justify-content-between align-items-center"
                >
                  {req.username} ({req.status})
                  <div>
                    <button
                      className="btn btn-success btn-sm me-2"
                      onClick={() => handleDecision(req.id, "accept")}
                    >
                      Accept
                    </button>
                    <button
                      className="btn btn-danger btn-sm"
                      onClick={() => handleDecision(req.id, "deny")}
                    >
                      Deny
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          </>
        )}
    </div>
  );
}
