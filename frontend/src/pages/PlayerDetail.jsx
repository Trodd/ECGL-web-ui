import { useEffect, useState } from "react";
import { useParams, Link } from "react-router-dom";
import axios from "axios";
import { getApiUrl } from "../config";
import { E } from "../components/CustomEmoji";
import { getDiscordAvatarUrl } from "../components/PlayerIdentity";
import { getLeague, leagueLabel } from "../league";

export default function PlayerDetail() {
  const { id } = useParams();
  const [player, setPlayer] = useState(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [me, setMe] = useState(null);
  const [copiedAvatar, setCopiedAvatar] = useState(false);
  const [echo, setEcho] = useState({ loading: true });

  useEffect(() => {
    async function load() {
      try {
        const res = await axios.get(
          `${getApiUrl()}/api/player/${id}`
        );
        setPlayer(res.data);
      } catch (err) {
        console.error("❌ Failed to load player:", err);
        setError("Failed to load player details.");
      } finally {
        setLoading(false);
      }
    }
    load();

    // Echo Combat stats (echocombat.com) — public data, proxied by our backend
    // so the API key stays server-side. Cached server-side for 6 hours.
    setEcho({ loading: true });
    axios
      .get(`${getApiUrl()}/api/player/${id}/echo-stats`, { withCredentials: true })
      .then((res) =>
        setEcho(
          res.data?.success
            ? { stats: res.data.stats }
            : { message: res.data?.message || "no stats" }
        )
      )
      .catch((err) =>
        setEcho({
          message:
            err.response?.status === 401
              ? "log in to see Echo Combat stats"
              : "no stats",
        })
      );

    axios
      .get(`${getApiUrl()}/api/me`, { withCredentials: true })
      .then((res) => setMe(res.data))
      .catch(() => setMe(null));
  }, [id]);

  if (loading) return <p>Loading player...</p>;
  if (error) return <div className="alert alert-danger">{error}</div>;
  if (!player) return <p>No player data.</p>;

  const canCopyAvatarUrl = !!me?.is_caster || !!me?.is_mod;

  async function handleCopyAvatarUrl() {
    const url = getDiscordAvatarUrl(player);
    try {
      if (navigator?.clipboard?.writeText) {
        await navigator.clipboard.writeText(url);
      } else {
        window.prompt("Copy avatar URL:", url);
      }
      setCopiedAvatar(true);
      window.setTimeout(() => setCopiedAvatar(false), 1500);
    } catch (e) {
      console.error("Failed to copy avatar URL", e);
      window.prompt("Copy avatar URL:", url);
    }
  }

  return (
    <div className="container text-light py-4 ecgl-dash">
      {/* ================= BACK (top) ================= */}
      <div className="ecgl-span-all">
        <Link to="/players" className="btn btn-secondary btn-sm">
          ← Back to Players
        </Link>
      </div>

      {/* ================= PLAYER HEADER ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm ecgl-span-2">
        <div className="d-flex align-items-center gap-3 mb-2">
          <img
            src={getDiscordAvatarUrl(player)}
            alt=""
            style={{
              width: 48,
              height: 48,
              borderRadius: "50%",
              border: copiedAvatar ? "2px solid #28a745" : "2px solid var(--border-default, #1e2a3a)",
              flexShrink: 0,
              cursor: canCopyAvatarUrl ? "pointer" : "default",
              transition: "border-color 0.2s, box-shadow 0.2s",
            }}
            title={canCopyAvatarUrl ? "Copy avatar URL" : undefined}
            onClick={canCopyAvatarUrl ? handleCopyAvatarUrl : undefined}
            onMouseEnter={canCopyAvatarUrl ? (e) => { e.currentTarget.style.boxShadow = "0 0 0 3px rgba(13,202,240,0.45)"; } : undefined}
            onMouseLeave={canCopyAvatarUrl ? (e) => { e.currentTarget.style.boxShadow = "none"; } : undefined}
            onError={(e) => { e.currentTarget.onerror = null; e.currentTarget.src = "https://cdn.discordapp.com/embed/avatars/0.png"; }}
          />
          <div>
            <h2 className="mb-0">
              {player.display_name || "Unknown Player"}
            </h2>
            <span className="players-discord-username" style={{ fontSize: "0.82rem" }}>
              @{player.username || "unknown"}
            </span>
          </div>
        </div>

        <div className="text-secondary small">
          <div className="d-flex align-items-center gap-2">
            <b>Role:</b> {player.role || "-"}
            {player.is_rookie_coach && getLeague() === "rookie" && (
              <span
                className="badge"
                style={{
                  background: "#0b3540",
                  color: "#67e8f9",
                  border: "1px solid #0dcaf0",
                  fontWeight: 700,
                }}
                title="Rookie League coach — does not play"
              >
                🧑‍🏫 Rookie Coach
              </span>
            )}
          </div>
          {player.is_rookie_coach && player.coach_team && getLeague() === "rookie" && (
            <div><b>Coaching:</b> {player.coach_team}</div>
          )}
          <div><b>Timezone:</b> {player.timezone || "-"}</div>

          {/* Discord User ID */}
          {player.id && (
            <div
              className="d-inline-flex align-items-center gap-2 mt-2 px-3 py-2 rounded"
              style={{
                background: "rgba(255,255,255,0.04)",
                border: "1px solid #3b6ea5",
              }}
            >
              {/* Clickable Discord profile */}
              <a
                href={`https://discord.com/users/${player.id}`}
                target="_blank"
                rel="noreferrer"
                className="text-decoration-none"
                title="Open Discord Profile"
                style={{
                  cursor: "pointer",
                  transition: "all 0.2s ease",
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.textShadow =
                    "0 0 10px rgba(158,203,255,0.7)";
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.textShadow = "none";
                }}
              >
                <span className="fw-semibold text-light">Discord ID:</span>{" "}
                <span
                  style={{
                    fontFamily: "monospace",
                    color: "#9ecbff",
                  }}
                >
                  {player.id}
                </span>{" "}
                <span style={{ opacity: 0.6 }}>↗</span>
              </a>

              {/* Copy button */}
              <button
                className="btn btn-sm btn-outline-info"
                title="Copy Discord ID"
                onClick={() => {
                  navigator.clipboard.writeText(player.id);
                }}
                style={{
                  borderRadius: "6px",
                  padding: "2px 8px",
                }}
              >
                <E n="clipboard" size={16} />
              </button>
            </div>
          )}
        </div>
      </div>

      {/* ================= LEAGUE STATS (this league only) ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <div className="d-flex flex-wrap align-items-center gap-2 mb-3">
          <h4 className="mb-0">
            <E n="trophy" className="emoji-gold" /> League Stats
          </h4>
          <span className="badge bg-secondary">{leagueLabel()}</span>
          <span className="text-secondary small">
            ECGL league play only — separate from Echo Combat stats below
          </span>
        </div>

        <div className="row text-center">
          <div className="col-6 col-md-3">
            <div className="fw-bold fs-4">{player.rating}</div>
            <div className="text-secondary small">Rating</div>
          </div>
          <div className="col-6 col-md-3">
            <div className="fw-bold fs-4 text-success">{player.wins}</div>
            <div className="text-secondary small">Wins</div>
          </div>
          <div className="col-6 col-md-3 mt-3 mt-md-0">
            <div className="fw-bold fs-4 text-danger">{player.losses}</div>
            <div className="text-secondary small">Losses</div>
          </div>
          <div className="col-6 col-md-3 mt-3 mt-md-0">
            <div className="fw-bold fs-4">{player.matches}</div>
            <div className="text-secondary small">Games Played</div>
          </div>
        </div>
      </div>

      {/* ================= ECHO COMBAT STATS ================= */}
      {(() => {
        // Tell the user we're talking to the stats API — the first lookup for a
        // player can take a few seconds (it may search by name upstream).
        if (echo.loading) {
          return (
            <div className="card bg-dark border-secondary p-3 mb-4 shadow-sm">
              <div className="d-flex flex-wrap align-items-center gap-2">
                <h6 className="mb-0">📊 Echo Combat Stats</h6>
                <span className="d-inline-flex align-items-center gap-2 text-info small">
                  <span className="spinner-border spinner-border-sm" role="status" aria-hidden="true" />
                  Fetching from echocombat.com…
                </span>
              </div>
            </div>
          );
        }

        // Surface *why* there's no data instead of silently hiding the card.
        if (!echo.stats) {
          return (
            <div className="card bg-dark border-secondary p-3 mb-4 shadow-sm">
              <div className="d-flex flex-wrap align-items-center gap-2">
                <h6 className="mb-0 text-secondary">📊 Echo Combat Stats</h6>
                <span className="text-secondary small">
                  {echo.message || "no recorded games for this player"}
                </span>
              </div>
            </div>
          );
        }

        const st = echo.stats;
        const tierColor =
          st.tier === "Veteran"
            ? "bg-danger"
            : st.tier === "Seasoned"
              ? "bg-warning text-dark"
              : st.tier === "Developing"
                ? "bg-info text-dark"
                : "bg-success";
        return (
          <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
            <div className="d-flex flex-wrap align-items-center gap-2 mb-2">
              <h4 className="mb-0">📊 Echo Combat Stats</h4>
              <span className={`badge ${tierColor}`} title={st.verdict || ""}>
                {st.tier} · {st.eligibility}/100
              </span>
              <span className="badge bg-dark border border-secondary">
                overall · all play
              </span>
              {st.matched_name && (st.match_score ?? 100) < 100 && (
                <span
                  className="badge bg-warning text-dark"
                  title={`Matched to the echocombat.com player "${st.matched_name}" by name (${st.match_score}% confidence)`}
                >
                  matched as {st.matched_name}
                </span>
              )}
              <a
                href={`https://echocombat.com/players/${encodeURIComponent(st.name || "")}/${st.user_id}`}
                target="_blank"
                rel="noreferrer"
                className="text-info small text-decoration-none ms-auto"
              >
                echocombat.com ↗
              </a>
            </div>

            <div className="row text-center">
              <div className="col-4 col-md-2">
                <div className="fw-bold fs-5">{st.games ?? 0}</div>
                <div className="text-secondary small">Games</div>
              </div>
              <div className="col-4 col-md-2">
                <div className="fw-bold fs-5">{Number(st.hours_played ?? 0).toFixed(1)}h</div>
                <div className="text-secondary small">Played</div>
              </div>
              <div className="col-4 col-md-2">
                <div className="fw-bold fs-5 text-success">
                  {st.win_rate_known ? `${Math.round((st.win_rate ?? 0) * 100)}%` : "—"}
                </div>
                <div className="text-secondary small">
                  Win rate
                  {st.win_rate_known ? (
                    <span className="d-block" style={{ fontSize: "0.68rem" }}>
                      {st.decided_games} decided
                    </span>
                  ) : (
                    <span className="d-block" style={{ fontSize: "0.68rem" }}>
                      no results
                    </span>
                  )}
                </div>
              </div>
              <div className="col-4 col-md-2">
                <div className="fw-bold fs-5">{st.level ?? 0}</div>
                <div className="text-secondary small">Level</div>
              </div>
              <div className="col-4 col-md-2">
                <div className="fw-bold fs-5">{Number(st.deaths_per_game ?? 0).toFixed(1)}</div>
                <div className="text-secondary small">Deaths / game</div>
              </div>
              <div className="col-4 col-md-2">
                <div className="fw-bold fs-5">{Number(st.top_speed ?? 0).toFixed(1)}</div>
                <div className="text-secondary small">Top m/s</div>
              </div>
            </div>

            {st.verdict && (
              <p className="text-secondary small mt-3 mb-0">{st.verdict}</p>
            )}
            <p className="text-secondary small mb-0" style={{ fontSize: "0.72rem" }}>
              Every recorded Echo Combat game (not just ECGL league play) ·{" "}
              {st.games ?? 0} games · {st.wins ?? 0}W–{st.losses ?? 0}L · first seen{" "}
              {(st.first_seen_at || "").slice(0, 10) || "unknown"} · last seen{" "}
              {(st.last_seen_at || "").slice(0, 10) || "unknown"}
            </p>
          </div>
        );
      })()}

      {/* ================= CURRENT TEAM ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <h4 className="mb-2"><E n="team" /> Current Team</h4>

        {player.current_team ? (
          <Link
            to={`/teams/${player.current_team_id}`}
            className="btn btn-outline-info"
          >
            {player.current_team}
          </Link>
        ) : (
          <p className="text-secondary mb-0">No current team</p>
        )}
      </div>

      {/* ================= TEAM HISTORY ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm ecgl-span-all">
        <h4 className="mb-3"><E n="scroll" /> Team History</h4>

        {Array.isArray(player.history) && player.history.length > 0 ? (
          <div className="table-responsive">
            <table className="table table-dark table-striped align-middle">
              <thead className="table-secondary">
                <tr>
                  <th>Season</th>
                  <th>Team</th>
                </tr>
              </thead>
              <tbody>
                {player.history.map((h, i) => (
                  <tr key={i}>
                    <td>{h.season}</td>
                    <td>
                      {h.team ? (
                        <Link
                          to={`/teams/${h.team_id}`}
                          className="text-info text-decoration-none fw-semibold"
                        >
                          {h.team}
                          {h.role === "League Sub" && (
                            <span className="badge bg-warning text-dark ms-2">
                              League Sub
                            </span>
                          )}
                        </Link>
                      ) : (
                        <span className="text-secondary">Unknown</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="text-secondary mb-0">No team history found.</p>
        )}
      </div>

      {/* ================= ARCHIVED STATS ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <h4 className="mb-3"><E n="medal" /> Archived Season Stats</h4>

        {Array.isArray(player.archived_stats) &&
          player.archived_stats.length > 0 ? (
          <div className="table-responsive">
            <table className="table table-dark table-striped text-center">
              <thead className="table-secondary">
                <tr>
                  <th>Season</th>
                  <th>Rating</th>
                  <th>W</th>
                  <th>L</th>
                  <th>GP</th>
                </tr>
              </thead>
              <tbody>
                {player.archived_stats.map((s, i) => (
                  <tr key={i}>
                    <td>{s.season}</td>
                    <td>{s.archive_rating}</td>
                    <td>{s.archive_wins}</td>
                    <td>{s.archive_losses}</td>
                    <td>{s.archive_matches}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="text-secondary mb-0">No archived stats.</p>
        )}
      </div>

      {/* ================= BACK (bottom) ================= */}
      <div className="ecgl-span-all">
        <Link to="/players" className="btn btn-secondary">
          ← Back to Players
        </Link>
      </div>
    </div>
  );
}