import { useEffect, useState } from "react";
import { useParams, Link } from "react-router-dom";
import axios from "axios";

export default function PlayerDetail() {
  const { id } = useParams();
  const [player, setPlayer] = useState(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function load() {
      try {
        const res = await axios.get(
          `${import.meta.env.VITE_API_URL}/api/player/${id}`
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
  }, [id]);

  if (loading) return <p>Loading player...</p>;
  if (error) return <div className="alert alert-danger">{error}</div>;
  if (!player) return <p>No player data.</p>;

  return (
    <div className="container text-light py-4" style={{ maxWidth: 960 }}>
      {/* ================= PLAYER HEADER ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <h2 className="mb-1">
          🎮 {player.display_name || "Unknown Player"}
        </h2>

        <div className="text-secondary small">
          <div><b>Username:</b> {player.username || "-"}</div>
          <div><b>Role:</b> {player.role || "-"}</div>
          <div><b>Timezone:</b> {player.timezone || "-"}</div>
        </div>
      </div>

      {/* ================= PLAYER STATS ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <h4 className="mb-3">🏆 Player Stats</h4>

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

      {/* ================= CURRENT TEAM ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <h4 className="mb-2">👥 Current Team</h4>

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
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <h4 className="mb-3">📜 Team History</h4>

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
        <h4 className="mb-3">📦 Archived Season Stats</h4>

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

      {/* ================= BACK ================= */}
      <Link to="/players" className="btn btn-secondary">
        ← Back to Players
      </Link>
    </div>
  );
}