import { useState, useEffect } from "react";
import axios from "axios";
import DiscordRequiredModal from "../components/DiscordRequiredModal";

export default function Register() {
  const [me, setMe] = useState(null);
  const [team, setTeam] = useState(null);
  const [loading, setLoading] = useState(true);
  const [showDiscordModal, setShowDiscordModal] = useState(false);
  const query = new URLSearchParams(window.location.search);
  const asParam = query.get("as");

  const [role, setRole] = useState("");
  const [device, setDevice] = useState("");
  const [timezone, setTimezone] = useState("");
  const [teams, setTeams] = useState([]);
  const [teamQuery, setTeamQuery] = useState("");
  const [newTeamName, setNewTeamName] = useState("");
  const [settings, setSettings] = useState({ min_team_players: 3, max_team_players: 6 });
  useEffect(() => {
    axios
      .get(`${urlBase}/api/settings`)
      .then((res) => {
        if (res.data) setSettings(res.data);
      })
      .catch(() => setSettings({ min_team_players: 3, max_team_players: 6 }));
  }, []);

  const urlBase = import.meta.env.VITE_API_URL;

  const [rosterLocked, setRosterLocked] = useState(false); // ✅ Global roster lock flag

  // --- Load roster lock status ---
  useEffect(() => {
    axios
      .get(`${urlBase}/api/mod/roster/status`, { withCredentials: true })
      .then((res) => {
        if (res.data?.locked !== undefined) {
          setRosterLocked(Boolean(res.data.locked));
        }
      })
      .catch(() => setRosterLocked(false));
  }, []);

  // --- Load player + team status ---
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
      } catch (err) {
        console.error("❌ Failed to load status:", err);
        setMe(null);
        setTeam(null);
      } finally {
        setLoading(false);
      }
    }
    loadStatus();
  }, []);

  // --- Load teams for join requests ---
  useEffect(() => {
    axios
      .get(`${urlBase}/api/teams`)
      .then((res) => setTeams(Array.isArray(res.data) ? res.data : []))
      .catch(() => setTeams([]));
  }, []);

  // --- Sync localStorage playerID ---
  useEffect(() => {
    if (me?.id) localStorage.setItem("playerID", me.id);
    else localStorage.removeItem("playerID");
  }, [me]);

  // Render modal globally
  const discordModalElement = (
    <DiscordRequiredModal
      show={showDiscordModal}
      onClose={() => setShowDiscordModal(false)}
    />
  );

  // --- Register handler ---
  async function handleRegister(e) {
    e.preventDefault();

    try {
      // 1️⃣ Check server membership BEFORE registering
      const check = await axios.get(`${urlBase}/api/check-discord`, { withCredentials: true });

      if (!check.data?.in_guild) {
        // ❌ Not in server → show modal & block registration
        setShowDiscordModal(true);
        return;
      }

      // 2️⃣ Proceed with actual registration
      const res = await axios.post(
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

      const updated = await axios.get(`${urlBase}/api/me`, { withCredentials: true });
      setMe(updated.data);

      alert("✅ Registered successfully");

    } catch (err) {
      console.error("Register failed:", err);

      // Backend may also return need_discord
      if (err.response?.data?.need_discord) {
        setShowDiscordModal(true);
        return;
      }

      alert("Failed to register");
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
      const res = await axios.get(
        asParam
          ? `${urlBase}/api/me?as=${asParam}`
          : `${urlBase}/api/me`,
        { withCredentials: true }
      );
      setMe(res.data);
      setTeam(null);
      alert("✅ Unregistered successfully");
    } catch {
      alert("Failed to unregister");
    }
  }

  async function handleCreateTeam() {
    if (!newTeamName.trim()) {
      alert("Enter a team name");
      return;
    }
    try {
      const res = await axios.post(
        `${urlBase}/api/team/create`,
        { name: newTeamName },
        { withCredentials: true }
      );
      setNewTeamName("");
      alert(`✅ Team "${res.data.team.name}" created!`);
      setTeam(res.data.team);
    } catch {
      alert("Failed to create team");
    }
  }

  async function handleRequestJoin(teamName) {
    const team = teams.find((t) => t.name.toLowerCase() === teamName.toLowerCase());
    if (!team) {
      alert("❌ Team not found");
      return;
    }

    try {
      // 🚫 Check for roster lock before anything
      if (rosterLocked) {
        alert("⏳ Roster lock is active — you cannot join or create teams right now.");
        return;
      }

      // 🧾 Fetch settings for limits (min/max players)
      const settingsRes = await axios.get(`${urlBase}/api/settings`);
      const maxPlayers = settingsRes.data?.max_team_players ?? 6;

      // 🧮 Check current team size
      const teamRes = await axios.get(`${urlBase}/api/team/${team.id}`);
      const currentCount = teamRes.data?.roster?.length ?? 0;

      // 🚫 Prevent joining full teams
      if (currentCount >= maxPlayers) {
        alert(
          `🚫 "${team.name}" already has the maximum number of players (${maxPlayers}). You cannot join this team.`
        );
        return;
      }

      // ✅ Proceed normally
      await axios.post(
        `${urlBase}/api/team/request`,
        { team_id: team.id },
        { withCredentials: true }
      );

      alert(`✅ Join request submitted to "${team.name}"!`);
      setTeamQuery("");
    } catch (err) {
      console.error("Join request failed:", err);
      if (err.response?.status === 403) {
        alert(
          err.response.data ||
          "🚫 You cannot join this team right now (roster may be locked or team is full)."
        );
      } else {
        alert("⚠️ Failed to submit join request. Please try again later.");
      }
    }
  }

  // --- Loading state ---
  if (loading)
    return (
      <>
        {discordModalElement}
        <p className="text-light">Checking registration status...</p>
      </>
    );
  if (!me)
    return (
      <>
        {discordModalElement}
        <p>🔑 Please log in with Discord to register.</p>
      </>
    );

  // --- Already registered & on a team ---
  if (me?.registered && team) {
    return (
      <>
        {discordModalElement}
        <div className="text-light">
          <h2>✅ You’re already registered!</h2>
          <p>
            You’re on team <strong>{team.name}</strong> ({team.status}).
          </p>
          <p>Use the <em>My Team</em> tab to manage your roster or matches.</p>
          <button className="btn btn-danger mt-3" onClick={handleUnregister}>
            Unregister
          </button>
        </div>
      </>
    );
  }

  // --- Registered but no team ---
  if (me?.registered && !team) {
    const isBanned = me?.role?.toLowerCase() === "banned";
    const isLeagueSub = me?.role?.toLowerCase() === "league sub";

    return (
      <>
        {discordModalElement}
        <div className="text-light">
          <h2 className="mb-3">
            {isBanned ? (
              <span className="text-danger">🚫 Your account has been banned</span>
            ) : (
              <>✅ You’re registered as a {me.role}</>
            )}
          </h2>

          {/* 🚫 Show roster lock warning */}
          {!isBanned && rosterLocked && (
            <div className="alert alert-warning small mb-3" style={{ maxWidth: 500 }}>
              <strong>🚫 Roster Lock Active:</strong> Joining or creating teams is temporarily disabled.
            </div>
          )}

          {/* ⭐ ALWAYS show unregister button unless banned */}
          {!isBanned && (
            <button className="btn btn-danger mb-4" onClick={handleUnregister}>
              Unregister
            </button>
          )}

          {isBanned ? (
            <p className="text-light fw-bold mt-2">
              🚫 Contact a Mod for more info.
            </p>
          ) : (
            <>
              {/* 💬 League Sub message */}
              {isLeagueSub && (
                <div className="text-warning fw-bold mt-2">
                  ⚠️ League Subs cannot join or create teams.
                  <ul className="mt-2">
                    <li>League Subs are stand-in players who fill in for teams when needed.</li>
                    <li>Look for teams requesting subs in the <strong>looking-for-subs</strong> channel.</li>
                    <li>League Subs do not join teams or participate as full rostered players.</li>
                    <li>To become a full player, please unregister and re-register as a Player.</li>
                  </ul>
                </div>
              )}

              {/* 👥 Team Join/Create section — ONLY for non-League Subs */}
              {!isLeagueSub && !rosterLocked && (
                <>
                  <h5>👥 Request to Join a Team</h5>
                  <div className="mb-3 position-relative" style={{ maxWidth: 300 }}>
                    <input
                      type="text"
                      className="form-control bg-dark text-light"
                      placeholder="Type team name..."
                      value={teamQuery}
                      onChange={(e) => setTeamQuery(e.target.value)}
                    />
                    {teamQuery && (
                      <ul
                        className="list-group position-absolute w-100"
                        style={{ zIndex: 1000 }}
                      >
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
                              style={{ cursor: "pointer" }}
                            >
                              {t.name}
                            </li>
                          ))}
                      </ul>
                    )}
                  </div>

                  <h5 className="text-light fw-light">➕ Create a Team</h5>
                  <div className="d-flex gap-2 mb-3" style={{ maxWidth: 300 }}>
                    <input
                      type="text"
                      className="form-control bg-dark text-light"
                      placeholder="Team name"
                      value={newTeamName}
                      onChange={(e) => setNewTeamName(e.target.value)}
                    />
                    <button className="btn btn-info" onClick={handleCreateTeam}>
                      Create
                    </button>
                  </div>
                </>
              )}
            </>
          )}
        </div>
      </>
    );
  }

  // --- Default: not registered ---
  return (
    <>
      {discordModalElement}
      <div className="text-light">
        <h2>📝 Register</h2>

        <form onSubmit={handleRegister} className="d-flex flex-column gap-3">
          <div
            className="alert alert-warning small mb-2"
            style={{ maxWidth: "500px" }}
          >
            <strong>⚠️ Platform Requirement Notice</strong><br />
            Echo Combat is <b>only available on PCVR</b>.<br />
            ❌ Quest-native players are <b>not eligible</b>.<br />
            ✅ You must play via <b>SteamVR</b> or <b>Oculus PC</b>.
          </div>

          <div>
            <label className="form-label">Role</label>
            <select
              className="form-select form-select-sm bg-dark text-light w-auto"
              value={role}
              onChange={(e) => setRole(e.target.value)}
            >
              <option value="">Select role...</option>
              <option value="Player">Player</option>
              <option value="League Sub">League Sub</option>
            </select>
          </div>

          <div>
            <label className="form-label">Device</label>
            <select
              className="form-select form-select-sm bg-dark text-light w-auto"
              value={device}
              onChange={(e) => setDevice(e.target.value)}
            >
              <option value="">Select device...</option>
              <option value="rift">Rift</option>
              <option value="quest_link">Quest + Link/AirLink</option>
              <option value="quest_native" disabled>
                Quest Native ❌
              </option>
            </select>
          </div>

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

          <button
            type="submit"
            className="btn btn-primary w-auto align-self-start"
            disabled={showDiscordModal}
          >
            Register
          </button>
        </form>
      </div>
    </>
  );
}
