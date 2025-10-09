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

  // captain tools
  const [selectedMatch, setSelectedMatch] = useState("");
  const [localDate, setLocalDate] = useState("");
  const [scores, setScores] = useState({});
  const [msg, setMsg] = useState("");
  const [submitLoading, setSubmitLoading] = useState(false);

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

  // captain tools functions
  async function handleScheduleMatch() {
    if (!selectedMatch || !localDate) {
      setMsg("⚠️ Select a match and time first.");
      return;
    }
    setSubmitLoading(true);
    try {
      const utc = new Date(localDate).toISOString();
      await axios.post(
        `${urlBase}/api/match/schedule`,
        { match_id: selectedMatch, team_id: team.id, date: utc },
        { withCredentials: true }
      );
      setMsg("✅ Match scheduled successfully.");
      await loadTeam();
    } catch (err) {
      console.error(err);
      setMsg("❌ Failed to schedule match.");
    } finally {
      setSubmitLoading(false);
    }
  }

  async function handleSubmitScores() {
    if (!selectedMatch) {
      setMsg("⚠️ Choose a match first.");
      return;
    }
    setSubmitLoading(true);
    try {
      const maps = [1, 2, 3]
        .filter((n) => scores[`map${n}`])
        .map((n) => ({
          map_number: n,
          team_a_score: Number(scores[`map${n}`].a || 0),
          team_b_score: Number(scores[`map${n}`].b || 0),
        }));
      await axios.post(
        `${urlBase}/api/match/submit-score`,
        { match_id: selectedMatch, team_id: team.id, maps },
        { withCredentials: true }
      );
      setMsg("✅ Scores submitted successfully.");
      await loadTeam();
    } catch (err) {
      console.error(err);
      setMsg("❌ Failed to submit scores.");
    } finally {
      setSubmitLoading(false);
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
            <button
              className="btn btn-danger btn-sm"
              onClick={handleLeaveTeam}
            >
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
        {(matches ?? []).length > 0 ? (
          matches.map((m) => (
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
          <p className="text-light">No matches yet.</p>
        )}
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
