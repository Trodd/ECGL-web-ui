import { useEffect, useMemo, useState } from "react";
import { toast } from "../toast";
import { useParams, useNavigate, Link } from "react-router-dom";
import axios from "axios";
import { getApiUrl } from "../config";
import { E } from "../components/CustomEmoji";
import TeamLogo from "../components/TeamLogo";
import PlayerIdentity from "../components/PlayerIdentity";
import TeamAvailability from "../components/TeamAvailability";
import RoleBadge from "../components/RoleBadge";

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
  const [teamStats, setTeamStats] = useState(null);

  // Team rank / rating / division / tier come from the league team board, so
  // the numbers here always match the Leaderboard page.
  useEffect(() => {
    if (!id) return;
    axios
      .get(`${urlBase}/api/leaderboard/teams`, { withCredentials: true })
      .then((res) => {
        const list = Array.isArray(res.data) ? res.data : res.data?.teams || [];
        const idx = list.findIndex((t) => Number(t.id) === Number(id));
        setTeamStats(idx >= 0 ? { ...list[idx], rank: idx + 1, total: list.length } : null);
      })
      .catch(() => setTeamStats(null));
  }, [id, urlBase]);

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
  const formerRoster = Array.isArray(team?.former_roster) ? team.former_roster : [];

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

  // Current streak of decided games (matches arrive newest first).
  const streak = useMemo(() => {
    let kind = null;
    let count = 0;
    for (const m of matches) {
      if (m.result !== "Win" && m.result !== "Loss") continue;
      if (kind === null) kind = m.result;
      if (m.result !== kind) break;
      count += 1;
    }
    return kind ? { kind, count } : null;
  }, [matches]);

  // ───────────────────────────────────────────────
  // 🔸 SEND CHALLENGE REQUEST
  // ───────────────────────────────────────────────
  async function handleChallengeRequest() {
    if (!myTeamID || !team?.id) return toast("Invalid team selection.");

    try {
      await axios.post(
        `${urlBase}/api/challenge/request`,
        {
          requester_team_id: myTeamID,
          target_team_id: team.id,
        },
        { withCredentials: true }
      );

      toast("Challenge request sent!");
    } catch (err) {
      toast(err.response?.data || "Failed to send challenge request.");
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
      className="container text-light py-4 ecgl-dash"
    >
      {/* ================= BACK (top) ================= */}
      <div className="ecgl-span-all">
        <Link to="/teams" className="btn btn-secondary btn-sm">
          ← Back to Teams
        </Link>
      </div>

      {/* ================= TEAM HEADER ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm ecgl-span-2">
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
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm ecgl-roster">
        <h4 className="mb-3">👥 Roster</h4>

        {roster.some(p => p.on_cooldown) && (
          <div className="alert alert-warning text-dark small py-2">
            ⏳ Players on cooldown cannot participate in matches yet.
          </div>
        )}

        {roster.length ? (
          <ul className="list-group">
            {[...roster]
              .sort((a, b) => {
                // Playing members first, coaches at the bottom of the roster.
                const rank = (r) => (String(r.role || "").toLowerCase() === "coach" ? 1 : 0);
                return rank(a) - rank(b);
              })
              .map(p => (
              <li
                key={p.id}
                className="list-group-item bg-black text-light d-flex justify-content-between align-items-center"
                style={{ borderColor: "#333" }}
              >
                <div className="d-flex align-items-center gap-2">
                  <PlayerIdentity player={p} size={26} />
                  <RoleBadge role={p.role} />
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
          <p className="text-secondary mb-0">
            {team.status === "Disbanded"
              ? "This team is disbanded — its former members are listed at the bottom of the page."
              : "No roster found."}
          </p>
        )}
      </div>

      {/* ================= TEAM STATS ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <div className="d-flex flex-wrap align-items-center gap-2 mb-3">
          <h4 className="mb-0"><E n="trophy" className="emoji-gold" /> Team Stats</h4>
          {teamStats?.division ? (
            <span className="badge bg-secondary">
              {teamStats.division}
              {teamStats.tier ? ` ${teamStats.tier}` : ""}
            </span>
          ) : null}
          {teamStats?.in_placement ? (
            <span className="badge bg-info text-dark">In placement</span>
          ) : null}
        </div>

        {(() => {
          const wins = teamStats?.wins;
          const losses = teamStats?.losses;
          const games = teamStats?.matches;
          const decided = (wins ?? 0) + (losses ?? 0);
          const pct = decided > 0 ? Math.round((wins / decided) * 100) : null;

          return (
            <>
              <div className="row text-center">
                <div className="col-6 col-md-3">
                  <div className="fw-bold fs-4">
                    {teamStats?.rank ? `#${teamStats.rank}` : "—"}
                  </div>
                  <div className="text-secondary small">
                    Rank{teamStats?.total ? ` (of ${teamStats.total})` : ""}
                  </div>
                </div>
                <div className="col-6 col-md-3">
                  <div className="fw-bold fs-4">{teamStats?.rating ?? "—"}</div>
                  <div className="text-secondary small">Rating</div>
                </div>
                <div className="col-6 col-md-3 mt-3 mt-md-0">
                  <div className="fw-bold fs-4">
                    <span className="text-success">{wins ?? "—"}</span>
                    <span className="text-secondary"> – </span>
                    <span className="text-danger">{losses ?? "—"}</span>
                  </div>
                  <div className="text-secondary small">Wins – Losses</div>
                </div>
                <div className="col-6 col-md-3 mt-3 mt-md-0">
                  <div className="fw-bold fs-4">{pct === null ? "—" : `${pct}%`}</div>
                  <div className="text-secondary small">
                    Win rate · {games ?? 0} game{games === 1 ? "" : "s"}
                  </div>
                </div>
              </div>

              {streak && (
                <p className="text-secondary small mb-0 mt-3">
                  🔥 Current streak: {streak.count} {streak.kind === "Win" ? "win" : "loss"}
                  {streak.count === 1 ? "" : "es"}
                  {team.status ? ` · ${team.status}` : ""}
                </p>
              )}
            </>
          );
        })()}
      </div>

      {/* ================= TEAM AVAILABILITY ================= */}
      <TeamAvailability teamId={team?.id} />

      {/* ================= MATCH HISTORY ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm ecgl-span-all">
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
        <div className="card bg-dark border-secondary p-4 shadow-sm ecgl-span-all">
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

      {/* ================= FORMER MEMBERS (bottom) ================= */}
      {(formerRoster.length > 0 || team.status === "Disbanded") && (
        <div className="card bg-dark border-secondary p-4 shadow-sm ecgl-span-all">
          <h4 className="mb-3">🕓 Former Members</h4>

          {formerRoster.length === 0 ? (
            <p className="text-secondary mb-0">
              No former members on record for this team.
            </p>
          ) : (
            <ul className="list-group">
              {formerRoster.map(p => (
                <li
                  key={p.id}
                  className="list-group-item bg-black text-light d-flex justify-content-between align-items-center"
                  style={{ borderColor: "#333" }}
                >
                  <div className="d-flex align-items-center gap-2">
                    <PlayerIdentity player={p} size={26} />
                    <span className="badge bg-secondary">{p.role || "-"}</span>
                    {p.season && (
                      <span className="text-secondary small">
                        {/^\d+$/.test(String(p.season)) ? `Season ${p.season}` : p.season}
                      </span>
                    )}
                  </div>
                  {p.left_at && (
                    <span className="text-secondary small">
                      {new Date(p.left_at).toLocaleDateString()}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {/* ================= BACK (bottom) ================= */}
      <div className="ecgl-span-all">
        <Link to="/teams" className="btn btn-secondary">
          ← Back to Teams
        </Link>
      </div>
    </div>
  );
}
