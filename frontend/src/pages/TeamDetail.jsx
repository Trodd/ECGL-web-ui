import { useEffect, useMemo, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import axios from "axios";

export default function TeamDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const urlBase = import.meta.env.VITE_API_URL;

  // ───────────────────────────────────────────────
  // 🔹 STATE
  // ───────────────────────────────────────────────
  const [team, setTeam] = useState(null);
  const [selectedSeason, setSelectedSeason] = useState("All");
  const [currentSeason, setCurrentSeason] = useState("Preseason");
  const [error, setError] = useState(null);

  const [settings, setSettings] = useState(null);
  const [teamSettings, setTeamSettings] = useState(null);

  // 🔹 Needed for challenge button rules
  const [myTeam, setMyTeam] = useState(null);
  const [myTeamID, setMyTeamID] = useState(null);
  const [isCaptain, setIsCaptain] = useState(false);
  const [archive, setArchive] = useState([]);

  useEffect(() => {
    if (!id) return;

    fetch(`${urlBase}/api/team/archive?id=${id}`)
      .then(res => res.json())
      .then(data => setArchive(Array.isArray(data) ? data : []))
      .catch(() => setArchive([]));
  }, [id]);

  // ───────────────────────────────────────────────
  // 🔸 LOAD GLOBAL SETTINGS + MY TEAM INFO
  // ───────────────────────────────────────────────
  useEffect(() => {
    // League settings
    axios
      .get(`${urlBase}/api/settings`, { withCredentials: true })
      .then((res) => setSettings(res.data))
      .catch(() => setSettings(null));

    // My team + role
    axios
      .get(`${urlBase}/api/myteam`, { withCredentials: true })
      .then((res) => {
        setMyTeam(res.data.team || null);
        setMyTeamID(res.data.team?.id || null);
        setIsCaptain(
          res.data.myRole === "Captain" || res.data.myRole === "Co-Captain"
        );
      })
      .catch(() => {
        setMyTeam(null);
        setMyTeamID(null);
        setIsCaptain(false);
      });
  }, []);

  // ───────────────────────────────────────────────
  // 🔸 LOAD TEAM'S OWN SETTINGS (allow_challenges)
  // ───────────────────────────────────────────────
  useEffect(() => {
    if (!team?.id) return;

    axios
      .get(`${urlBase}/api/team/${team.id}`, { withCredentials: true })
      .then((res) => setTeamSettings(res.data))
      .catch(() => setTeamSettings(null));
  }, [team?.id]);

  // ───────────────────────────────────────────────
  // 🔸 LOAD TEAM PAGE INFO
  // ───────────────────────────────────────────────
  useEffect(() => {
    setError(null);

    axios
      .get(`${urlBase}/api/team/${id}`)
      .then((res) => setTeam(res.data))
      .catch(() => setError("Failed to load team details"));
  }, [id]);

  // ───────────────────────────────────────────────
  // 🔸 LOAD CURRENT SEASON
  // ───────────────────────────────────────────────
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

  // ───────────────────────────────────────────────
  // 🔸 SAFETY HANDLING
  // ───────────────────────────────────────────────
  const matches = Array.isArray(team?.matches) ? team.matches : [];
  const roster = Array.isArray(team?.roster) ? team.roster : [];

  // ───────────────────────────────────────────────
  // 🔸 BUILD SEASON FILTER OPTIONS
  // ───────────────────────────────────────────────
  const allSeasons = useMemo(() => {
    const base = ["Preseason"];
    if (currentSeason && !base.includes(currentSeason))
      base.push(currentSeason);

    return ["All", ...base];
  }, [currentSeason]);

  // ───────────────────────────────────────────────
  // 🔸 MATCH FILTERING (unchanged)
  // ───────────────────────────────────────────────
  const filteredMatches = useMemo(() => {
    if (!matches.length) return [];

    if (selectedSeason === "All") return matches;

    return matches.filter((m) => {
      const code = (m.match_code || "").toLowerCase();

      if (selectedSeason === "Preseason") {
        return code.startsWith("week") || code.startsWith("preseason");
      }

      const num = selectedSeason.replace(/[^0-9]/g, "");
      if (!num) return false;

      return code.startsWith(`${num}-`);
    });
  }, [matches, selectedSeason]);

  // ───────────────────────────────────────────────
  // 🔸 SEND CHALLENGE REQUEST
  // ───────────────────────────────────────────────
  async function handleChallengeRequest() {
    if (!myTeamID || !team?.id) return alert("Invalid team selection.");

    try {
      await axios.post(
        `${urlBase}/api/challenge/request`,
        {
          requester_team_id: myTeamID,
          target_team_id: team.id,
        },
        { withCredentials: true }
      );

      alert("Challenge request sent!");
    } catch (err) {
      alert(err.response?.data || "Failed to send challenge request.");
    }
  }

  // ───────────────────────────────────────────────
  // 🔸 UI BASE CHECKS
  // ───────────────────────────────────────────────
  if (error) return <p className="text-danger">{error}</p>;
  if (!team) return <p className="text-light">Loading team...</p>;

  // ───────────────────────────────────────────────
  // 🔥 CHALLENGE BUTTON VISIBILITY LOGIC
  // ───────────────────────────────────────────────
  const canChallenge =
    isCaptain &&
    myTeamID &&
    team.id !== myTeamID &&
    settings?.challenges_enabled === true &&
    team.status === "Active" &&                 // 🔥 target team must be active
    myTeam?.status === "Active" &&              // 🔥 requester team must be active
    teamSettings?.allow_challenges === true &&
    (myTeam?.weekly_challenges_used ?? 0) <
    (settings?.weekly_challenge_limit ?? 999);

  // ───────────────────────────────────────────────
  // 🔸 RENDER
  // ───────────────────────────────────────────────
  return (
    <div>
      <h2 className="text-light">{team.name}</h2>

      <p>
        Status:{" "}
        <span
          className={
            team.status === "Disbanded"
              ? "text-danger fw-bold"
              : team.status === "Active"
                ? "text-success fw-bold"
                : "text-warning fw-bold"
          }
        >
          {team.status}
        </span>
      </p>

      {/* ===========================
          ⚔️ CHALLENGE BUTTON SHOWS HERE
          =========================== */}
      {/* Challenge Button */}
      {settings?.challenges_enabled ? (
        team.status !== "Active" ? (
          <p className="text-warning small mt-2">
            🔒 This team is not active and cannot receive challenges.
          </p>
        ) : myTeam?.status !== "Active" ? (
          <p className="text-warning small mt-2">
            🔒 Your team must be active to issue challenges.
          </p>
        ) : canChallenge ? (
          <button
            className="btn btn-warning mt-3"
            onClick={handleChallengeRequest}
          >
            ⚔️ Challenge This Team
          </button>
        ) : (
          <p className="text-secondary small mt-2">
            {teamSettings?.allow_challenges !== true
              ? "🚫 This team is not accepting challenges."
              : (myTeam?.weekly_challenges_used ?? 0) >=
                (settings?.weekly_challenge_limit ?? 999)
                ? "⚠️ Your team has used all weekly challenge attempts."
                : !isCaptain
                  ? "⛔ Only the captain or co-captain may issue challenges."
                  : ""}
          </p>
        )
      ) : (
        <p className="text-warning small mt-2">
          ⛔ Challenge matches are currently disabled league-wide.
        </p>
      )}

      <h3>Roster</h3>
      {roster.some(p => p.on_cooldown) && (
        <p className="text-warning small mt-2 mb-1">
          ⏳ Players on cooldown cannot participate in matches until the next matchup generation.
        </p>
      )}
      {roster.length ? (
        <ul className="list-group roster-list">
          {roster.map((p) => (
            <li
              key={p.id ?? Math.random()}
              className="list-group-item d-flex justify-content-between align-items-center"
            >
              <div className="d-flex align-items-center gap-2">
                <strong>{p.display_name || p.username || "Unknown"}</strong>
                <span className={`roster-role ${p.role?.toLowerCase() || ""}`}>
                  {p.role || "-"}
                </span>

                {/* ⭐ Cooldown Badge */}
                {p.on_cooldown && (
                  <span className="badge bg-warning text-dark">
                    ⏳ Cooldown
                  </span>
                )}
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-light">No roster found.</p>
      )}

      <h3 className="mt-4 mb-3 text-light">📜 Match History</h3>

      {allSeasons.length > 1 && (
        <div className="d-flex justify-content-end mb-2">
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
      )}

      {filteredMatches.length ? (
        <div className="table-responsive">
          <table className="table table-dark table-striped align-middle text-center table-hover">
            <thead className="table-secondary">
              <tr>
                <th>#</th>
                <th>Opponent</th>
                <th>Date</th>
                <th>Result</th>
                <th>Match ID</th>
              </tr>
            </thead>
            <tbody>
              {filteredMatches.map((m, idx) => (
                <tr
                  key={m.id || idx}
                  className="match-row"
                  style={{ cursor: "pointer" }}
                  onClick={() => navigate(`/match/${m.id}`)}
                >
                  <td>{idx + 1}</td>
                  <td className="fw-semibold">
                    {m.opponent || "Unknown"}
                  </td>
                  <td>
                    {m.date
                      ? new Date(m.date).toLocaleDateString()
                      : "-"}
                  </td>
                  <td
                    className={
                      m.result === "Win"
                        ? "text-success fw-bold"
                        : m.result === "Loss"
                          ? "text-danger fw-bold"
                          : "text-warning fw-bold"
                    }
                  >
                    {m.result || "Pending"}
                  </td>
                  <td>{m.match_code || m.id}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="text-light">No matches found for {selectedSeason}.</p>
      )}
      {archive.length > 0 && (
        <div className="team-stats-card">
          <h3>📦 Archived Team Stats</h3>
          <table className="table table-dark table-striped">
            <thead>
              <tr>
                <th>Season</th>
                <th>Rating</th>
                <th>Wins</th>
                <th>Losses</th>
                <th>Matches</th>
              </tr>
            </thead>
            <tbody>
              {archive.map(a => (
                <tr key={a.id}>
                  <td>{a.season}</td>
                  <td>{a.rating}</td>
                  <td>{a.wins}</td>
                  <td>{a.losses}</td>
                  <td>{a.matches}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}