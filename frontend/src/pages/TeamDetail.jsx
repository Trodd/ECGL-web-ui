import { useEffect, useMemo, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import axios from "axios";
import { getApiUrl } from "../config";
import { E } from "../components/CustomEmoji";
import TeamLogo from "../components/TeamLogo";
import PlayerIdentity from "../components/PlayerIdentity";
import TeamAvailability from "../components/TeamAvailability";

export default function TeamDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const urlBase = getApiUrl();

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
  const [me, setMe] = useState(null);
  const [copiedLogo, setCopiedLogo] = useState(false);

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

    // Me (caster/mod/etc)
    axios
      .get(`${urlBase}/api/me`, { withCredentials: true })
      .then((res) => setMe(res.data))
      .catch(() => setMe(null));

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

  const effectiveLogoUrl = team?.logo_url || (team?.id ? `/api/team/logo/${team.id}` : "");
  const canCopyLogoUrl = !!me?.is_caster || !!me?.is_mod;

  async function handleCopyLogoUrl() {
    if (!effectiveLogoUrl) return;
    const absolute = effectiveLogoUrl.startsWith("http://") || effectiveLogoUrl.startsWith("https://")
      ? effectiveLogoUrl
      : `${urlBase}${effectiveLogoUrl}`;
    if (!absolute) return;

    try {
      if (navigator?.clipboard?.writeText) {
        await navigator.clipboard.writeText(absolute);
      } else {
        window.prompt("Copy team logo URL:", absolute);
      }
      setCopiedLogo(true);
      window.setTimeout(() => setCopiedLogo(false), 1500);
    } catch (e) {
      console.error("Failed to copy logo URL", e);
      window.prompt("Copy team logo URL:", absolute);
    }
  }

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
    <div
      className="container text-light py-4"
      style={{ maxWidth: 960 }}
    >
      {/* ================= TEAM HEADER ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <div className="d-flex flex-wrap justify-content-between align-items-center gap-3">
          <div className="d-flex align-items-center gap-3">
            <TeamLogo
              name={team.name}
              logoUrl={effectiveLogoUrl}
              size={80}
            />

            <div>
              <h2 className="mb-1">{team.name}</h2>
              <span
                className={`badge px-3 py-2 ${team.status === "Active"
                  ? "bg-success"
                  : team.status === "Disbanded"
                    ? "bg-danger"
                    : "bg-warning text-dark"
                  }`}
              >
                {team.status}
              </span>
            </div>
          </div>

          {/* ⚔️ CHALLENGE CTA */}
          <div className="text-end">
            {canCopyLogoUrl ? (
              <div className="mb-2">
                <button
                  className="btn btn-outline-light btn-sm"
                  onClick={handleCopyLogoUrl}
                  type="button"
                >
                  {copiedLogo ? <><E n="check" className="emoji-success" /> Copied</> : "Copy Logo URL"}
                </button>
              </div>
            ) : null}
            {settings?.challenges_enabled ? (
              team.status !== "Active" ? (
                <span className="text-warning small">
                  <E n="lock" /> Team inactive
                </span>
              ) : myTeam?.status !== "Active" ? (
                <span className="text-warning small">
                  <E n="lock" /> Your team inactive
                </span>
              ) : canChallenge ? (
                <button
                  className="btn btn-warning"
                  onClick={handleChallengeRequest}
                >
                  <E n="swords" /> Challenge Team
                </button>
              ) : (
                <span className="text-secondary small">
                  {teamSettings?.allow_challenges !== true
                    ? <><E n="banned" /> Not accepting challenges</>
                    : (myTeam?.weekly_challenges_used ?? 0) >=
                      (settings?.weekly_challenge_limit ?? 999)
                      ? <><E n="warning" className="emoji-warning" /> Weekly challenge limit reached</>
                      : !isCaptain
                        ? <><E n="stop" /> Captain only</>
                        : ""}
                </span>
              )
            ) : (
              <span className="text-warning small">
                <E n="stop" /> Challenges disabled league-wide
              </span>
            )}
          </div>
        </div>
      </div>

      {/* ================= ROSTER ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <h4 className="mb-3">👥 Roster</h4>

        {roster.some(p => p.on_cooldown) && (
          <div className="alert alert-warning text-dark small py-2">
            ⏳ Players on cooldown cannot participate in matches yet.
          </div>
        )}

        {roster.length ? (
          <ul className="list-group">
            {roster.map(p => (
              <li
                key={p.id}
                className="list-group-item bg-black text-light d-flex justify-content-between align-items-center"
                style={{ borderColor: "#333" }}
              >
                <div className="d-flex align-items-center gap-2">
                  <PlayerIdentity player={p} size={26} />
                  <span className={`badge bg-secondary`}>
                    {p.role || "-"}
                  </span>
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
          <p className="text-secondary mb-0">No roster found.</p>
        )}
      </div>

      {/* ================= TEAM AVAILABILITY ================= */}
      <TeamAvailability teamId={team?.id} />

      {/* ================= MATCH HISTORY ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <div className="d-flex justify-content-between align-items-center mb-3">
          <h4 className="mb-0">📜 Match History</h4>

          {allSeasons.length > 1 && (
            <select
              className="form-select form-select-sm bg-black text-light"
              style={{ maxWidth: 200 }}
              value={selectedSeason}
              onChange={e => setSelectedSeason(e.target.value)}
            >
              {allSeasons.map(s => (
                <option key={s}>{s}</option>
              ))}
            </select>
          )}
        </div>

        {filteredMatches.length ? (
          <div className="table-responsive">
            <table className="table table-dark table-striped table-hover align-middle text-center">
              <thead className="table-secondary">
                <tr>
                  <th>#</th>
                  <th>Opponent</th>
                  <th>Date</th>
                  <th>Result</th>
                  <th>Match</th>
                </tr>
              </thead>
              <tbody>
                {filteredMatches.map((m, idx) => (
                  <tr
                    key={m.id}
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
          <p className="text-secondary mb-0">
            No matches found for {selectedSeason}.
          </p>
        )}
      </div>

      {/* ================= ARCHIVE ================= */}
      {archive.length > 0 && (
        <div className="card bg-dark border-secondary p-4 shadow-sm">
          <h4 className="mb-3">📦 Archived Team Stats</h4>

          <div className="table-responsive">
            <table className="table table-dark table-striped text-center">
              <thead className="table-secondary">
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
        </div>
      )}
    </div>
  );
}