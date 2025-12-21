import { useState, useEffect } from "react";
import axios from "axios";
import DiscordRequiredModal from "../components/DiscordRequiredModal";

export default function Register() {
  const urlBase = import.meta.env.VITE_API_URL;

  const [me, setMe] = useState(null);
  const [team, setTeam] = useState(null);
  const [loading, setLoading] = useState(true);
  const [showDiscordModal, setShowDiscordModal] = useState(false);

  const [role, setRole] = useState("");
  const [device, setDevice] = useState("");
  const [timezone, setTimezone] = useState("");

  const [teams, setTeams] = useState([]);
  const [teamQuery, setTeamQuery] = useState("");
  const [newTeamName, setNewTeamName] = useState("");
  const [rosterLocked, setRosterLocked] = useState(false);

  const query = new URLSearchParams(window.location.search);
  const asParam = query.get("as");

  // -----------------------------------------------------
  // Load roster lock
  // -----------------------------------------------------
  useEffect(() => {
    axios
      .get(`${urlBase}/api/mod/roster/status`, { withCredentials: true })
      .then((res) => setRosterLocked(Boolean(res.data?.locked)))
      .catch(() => setRosterLocked(false));
  }, []);

  // -----------------------------------------------------
  // Load user + team status
  // -----------------------------------------------------
  useEffect(() => {
    async function loadStatus() {
      try {
        const meURL = asParam
          ? `${urlBase}/api/me?as=${asParam}`
          : `${urlBase}/api/me`;

        const teamURL = asParam
          ? `${urlBase}/api/myteam?as=${asParam}`
          : `${urlBase}/api/myteam`;

        const [meRes, teamRes] = await Promise.all([
          axios.get(meURL, { withCredentials: true }),
          axios.get(teamURL, { withCredentials: true }),
        ]);

        setMe(meRes.data || null);
        setTeam(teamRes.data?.team || null);
      } catch {
        setMe(null);
        setTeam(null);
      } finally {
        setLoading(false);
      }
    }

    loadStatus();
  }, []);

  // -----------------------------------------------------
  // Load teams
  // -----------------------------------------------------
  useEffect(() => {
    axios
      .get(`${urlBase}/api/teams`)
      .then((res) => setTeams(Array.isArray(res.data) ? res.data : []))
      .catch(() => setTeams([]));
  }, []);

  // -----------------------------------------------------
  // Handlers
  // -----------------------------------------------------
  async function handleRegister(e) {
    e.preventDefault();

    try {
      // 1️⃣ Check server membership
      const check = await axios.get(
        `${urlBase}/api/check-discord`,
        { withCredentials: true }
      );

      if (!check.data?.in_guild) {
        setShowDiscordModal(true);
        return;
      }

      // 2️⃣ Register
      await axios.post(
        asParam
          ? `${urlBase}/api/register?as=${asParam}`
          : `${urlBase}/api/register`,
        {
          username: me?.username,
          role,
          device,
          timezone,
        },
        { withCredentials: true }
      );

      // 3️⃣ 🔑 RE-FETCH ME (WITH asParam SUPPORT)
      const updated = await axios.get(
        asParam
          ? `${urlBase}/api/me?as=${asParam}`
          : `${urlBase}/api/me`,
        { withCredentials: true }
      );

      const [meRes, teamRes] = await Promise.all([
        axios.get(
          asParam
            ? `${urlBase}/api/me?as=${asParam}`
            : `${urlBase}/api/me`,
          { withCredentials: true }
        ),
        axios.get(
          asParam
            ? `${urlBase}/api/myteam?as=${asParam}`
            : `${urlBase}/api/myteam`,
          { withCredentials: true }
        ),
      ]);

      setMe(meRes.data);
      setTeam(teamRes.data?.team || null);

      alert("✅ Registered successfully");
    } catch (err) {
      console.error("Register failed:", err);

      if (err.response?.data?.need_discord) {
        setShowDiscordModal(true);
        return;
      }

      alert(err.response?.data || "❌ Failed to register");
    }
  }

  async function handleUnregister() {
    try {
      await axios.post(
        asParam
          ? `${urlBase}/api/unregister?as=${asParam}`
          : `${urlBase}/api/unregister`,
        {},
        { withCredentials: true }
      );
      window.location.reload();
    } catch {
      alert("❌ Failed to unregister");
    }
  }

  async function handleCreateTeam() {
    if (!newTeamName.trim()) return alert("Enter a team name");

    try {
      await axios.post(
        `${urlBase}/api/team/create`,
        { name: newTeamName },
        { withCredentials: true }
      );

      // 🔑 refresh state
      const [meRes, teamRes] = await Promise.all([
        axios.get(
          asParam
            ? `${urlBase}/api/me?as=${asParam}`
            : `${urlBase}/api/me`,
          { withCredentials: true }
        ),
        axios.get(
          asParam
            ? `${urlBase}/api/myteam?as=${asParam}`
            : `${urlBase}/api/myteam`,
          { withCredentials: true }
        ),
      ]);

      setMe(meRes.data);
      setTeam(teamRes.data?.team || null);
      setNewTeamName("");
    } catch {
      alert("❌ Failed to create team");
    }
  }

  async function handleRequestJoin(name) {
    const t = teams.find(
      (x) => x.name.toLowerCase() === name.toLowerCase()
    );
    if (!t) return alert("Team not found");

    try {
      if (rosterLocked)
        return alert("⏳ Roster lock is active.");

      await axios.post(
        `${urlBase}/api/team/request`,
        { team_id: t.id },
        { withCredentials: true }
      );

      alert(`✅ Join request sent to ${t.name}`);
      setTeamQuery("");
    } catch {
      alert("❌ Failed to request join");
    }
  }

  const modal = (
    <DiscordRequiredModal
      show={showDiscordModal}
      onClose={() => setShowDiscordModal(false)}
    />
  );

  // =====================================================
  // UI STATES
  // =====================================================

  if (loading) {
    return (
      <>
        {modal}
        <div className="container text-light py-5 text-center">
          <h4>🔄 Checking registration status…</h4>
        </div>
      </>
    );
  }

  if (!me) {
    return (
      <>
        {modal}
        <div className="container text-light py-5 text-center">
          <h4>🔑 Login Required</h4>
          <p>Please log in with Discord to register.</p>
        </div>
      </>
    );
  }

  if (me.registered && team) {
    return (
      <>
        {modal}
        <div className="container text-light py-5" style={{ maxWidth: 720 }}>
          <div className="card bg-dark border-secondary p-4">
            <h3>✅ You’re Registered</h3>
            <p className="mt-2">
              Team: <strong>{team.name}</strong>
            </p>
            <p className="text-secondary">
              Manage matches and roster from <b>My Team</b>.
            </p>
            <button
              className="btn btn-outline-danger mt-3 w-auto"
              onClick={handleUnregister}
            >
              Unregister
            </button>
          </div>
        </div>
      </>
    );
  }

  if (me.registered && !team) {
    const isBanned = me.role?.toLowerCase() === "banned";
    const isLeagueSub = me.role?.toLowerCase() === "league sub";

    return (
      <>
        {modal}
        <div className="container text-light py-5" style={{ maxWidth: 720 }}>
          <div className="card bg-dark border-secondary p-4 mb-4">
            <h3>
              {isBanned ? "🚫 Account Banned" : "✅ Registration Complete"}
            </h3>

            {!isBanned && (
              <p className="text-secondary">
                Role: <strong>{me.role}</strong>
              </p>
            )}

            {rosterLocked && !isBanned && (
              <div className="alert alert-warning small">
                ⏳ Roster lock is active.
              </div>
            )}

            {!isBanned && (
              <button
                className="btn btn-outline-danger mt-2 w-auto"
                onClick={handleUnregister}
              >
                Unregister
              </button>
            )}
          </div>

          {!isLeagueSub && !isBanned && !rosterLocked && (
            <>
              <div className="card bg-dark border-secondary p-4 mb-4">
                <h5>👥 Join a Team</h5>
                <input
                  className="form-control bg-dark text-light"
                  placeholder="Search team…"
                  value={teamQuery}
                  onChange={(e) => setTeamQuery(e.target.value)}
                />
                {teamQuery && (
                  <ul className="list-group mt-1">
                    {teams
                      .filter((t) =>
                        t.name.toLowerCase().includes(teamQuery.toLowerCase())
                      )
                      .slice(0, 5)
                      .map((t) => (
                        <li
                          key={t.id}
                          className="list-group-item list-group-item-action bg-dark text-light"
                          onClick={() => handleRequestJoin(t.name)}
                        >
                          {t.name}
                        </li>
                      ))}
                  </ul>
                )}
              </div>

              <div className="card bg-dark border-secondary p-4">
                <h5>➕ Create a Team</h5>
                <div className="d-flex gap-2">
                  <input
                    className="form-control bg-dark text-light"
                    placeholder="Team name"
                    value={newTeamName}
                    onChange={(e) => setNewTeamName(e.target.value)}
                  />
                  <button className="btn btn-info" onClick={handleCreateTeam}>
                    Create
                  </button>
                </div>
              </div>
            </>
          )}
        </div>
      </>
    );
  }

  return (
    <>
      {modal}
      <div className="container text-muted py-5" style={{ maxWidth: 720 }}>
        <div className="card bg-dark border-secondary p-4">
          <h3>📝 League Registration</h3>

          <div className="alert ecgl-alert-warning small mb-3">
            ⚠️ <strong>PCVR ONLY</strong>
            <br />
            Quest <strong>standalone / native</strong> is <strong>NOT eligible</strong>.
            <br />
            SteamVR or Oculus PC is required to play.
          </div>

          <form onSubmit={handleRegister} className="d-grid gap-3">
            <select
              className="form-select bg-dark text-light"
              value={role}
              onChange={(e) => setRole(e.target.value)}
            >
              <option value="">Select role…</option>
              <option value="Player">Player</option>
              <option value="League Sub">League Sub</option>
            </select>

            <select
              className="form-select bg-dark text-light"
              value={device}
              onChange={(e) => setDevice(e.target.value)}
            >
              <option value="">Select device…</option>
              <option value="rift">Rift</option>
              <option value="quest_link">Quest + Link/AirLink</option>
              <option value="quest_native" disabled>
                Quest Native ❌
              </option>
            </select>

            <div>
              <label className="form-label">Timezone</label>
              <select
                className="form-select form-select-sm bg-dark text-light w-auto"
                value={timezone}
                onChange={(e) => setTimezone(e.target.value)}
              >
                <option value="">Select timezone...</option>
                <optgroup label="North America">
                  <option value="US/Eastern">US/Eastern (EST/EDT)</option>
                  <option value="US/Central">US/Central (CST/CDT)</option>
                  <option value="US/Mountain">US/Mountain (MST/MDT)</option>
                  <option value="US/Arizona">US/Arizona (no DST)</option>
                  <option value="US/Pacific">US/Pacific (PST/PDT)</option>
                  <option value="US/Alaska">US/Alaska</option>
                  <option value="US/Hawaii">US/Hawaii</option>
                  <option value="Canada/Atlantic">Canada/Atlantic</option>
                  <option value="Canada/Newfoundland">Canada/Newfoundland</option>
                </optgroup>
                <optgroup label="Europe">
                  <option value="Europe/London">Europe/London (UK)</option>
                  <option value="Europe/Dublin">Europe/Dublin (Ireland)</option>
                  <option value="Europe/Lisbon">Europe/Lisbon (Portugal)</option>
                  <option value="Europe/Paris">Europe/Paris (France)</option>
                  <option value="Europe/Berlin">Europe/Berlin (Germany)</option>
                  <option value="Europe/Rome">Europe/Rome (Italy)</option>
                  <option value="Europe/Madrid">Europe/Madrid (Spain)</option>
                  <option value="Europe/Amsterdam">Europe/Amsterdam (Netherlands)</option>
                  <option value="Europe/Brussels">Europe/Brussels (Belgium)</option>
                  <option value="Europe/Vienna">Europe/Vienna (Austria)</option>
                  <option value="Europe/Oslo">Europe/Oslo (Norway)</option>
                  <option value="Europe/Stockholm">Europe/Stockholm (Sweden)</option>
                  <option value="Europe/Copenhagen">Europe/Copenhagen (Denmark)</option>
                  <option value="Europe/Helsinki">Europe/Helsinki (Finland)</option>
                  <option value="Europe/Athens">Europe/Athens (Greece)</option>
                  <option value="Europe/Sofia">Europe/Sofia (Bulgaria)</option>
                  <option value="Europe/Warsaw">Europe/Warsaw (Poland)</option>
                  <option value="Europe/Budapest">Europe/Budapest (Hungary)</option>
                  <option value="Europe/Prague">Europe/Prague (Czechia)</option>
                  <option value="Europe/Bucharest">Europe/Bucharest (Romania)</option>
                  <option value="Europe/Moscow">Europe/Moscow (Russia)</option>
                </optgroup>
              </select>
            </div>

            <button type="submit" className="btn btn-primary w-auto">
              Register
            </button>
          </form>
        </div>
      </div>
    </>
  );
}