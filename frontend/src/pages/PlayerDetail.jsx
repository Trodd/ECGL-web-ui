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
    <div>
      <h2>🎮 {player.display_name || "Unknown Player"}</h2>
      <p>
        <b>Username:</b> {player.username} <br />
        <b>Role:</b> {player.role || "-"} <br />
        <b>Timezone:</b> {player.timezone || "-"}
      </p>

      <h4>🏆 Stats</h4>
      <ul>
        <li>Rating: {player.rating}</li>
        <li>Wins: {player.wins}</li>
        <li>Losses: {player.losses}</li>
        <li>Games Played: {player.matches}</li>
      </ul>

      <h4>👥 Current Team</h4>
      {player.current_team ? (
        <p>
          <Link to={`/teams/${player.current_team_id}`}>
            {player.current_team}
          </Link>
        </p>
      ) : (
        <p>None</p>
      )}

      <h4>📜 Team History</h4>
      <div className="table-section">
        {Array.isArray(player.history) && player.history.length > 0 ? (
          <table className="table table-dark table-striped">
            <thead>
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
                        className="text-info text-decoration-none"
                      >
                        <>
                          {h.team}
                          {h.role === "League Sub" && (
                            <span className="text-warning ms-2">(League Sub)</span>
                          )}
                        </>
                      </Link>
                    ) : (
                      h.team
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p>No history found.</p>
        )}
      </div>

      {/* ARCHIVED STATS */}
      <h4 className="mt-4">📦 Season Stats history</h4>
      <div className="table-section">
        {Array.isArray(player.archived_stats) && player.archived_stats.length > 0 ? (
          <table className="table table-dark table-striped mt-2">
            <thead>
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
        ) : (
          <p className="text-light">No archived stats.</p>
        )}
      </div>

      <Link to="/players" className="btn btn-secondary mt-3">
        ← Back to Players
      </Link>
    </div>
  );
}