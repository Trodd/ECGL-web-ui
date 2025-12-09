import { useEffect, useState } from "react";
import axios from "axios";

export default function Leaderboard() {
  const [view, setView] = useState("teams"); // default: teams
  const [teams, setTeams] = useState([]);
  const [players, setPlayers] = useState([]);

  useEffect(() => {
    if (view === "teams") {
      axios.get(`${import.meta.env.VITE_API_URL}/api/leaderboard/teams`)
        .then(res => setTeams(res.data));
    } else {
      axios.get(`${import.meta.env.VITE_API_URL}/api/leaderboard/players`)
        .then(res => setPlayers(res.data));
    }
  }, [view]);

  // ✅ sort teams
  const sortedTeams = [...teams].sort((a, b) => {
    if (b.rating !== a.rating) return b.rating - a.rating;
    if (b.wins !== a.wins) return b.wins - a.wins;
    return a.losses - b.losses;
  });

  // ✅ sort players
  const sortedPlayers = [...players].sort((a, b) => {
    if (b.rating !== a.rating) return b.rating - a.rating;
    if (b.wins !== a.wins) return b.wins - a.wins;
    return a.losses - b.losses;
  });

  function getRankClass(division) {
    if (!division) return "rank-badge rank-unranked";
    return "rank-badge rank-" + division.toLowerCase();
  }

  return (
    <div>
      <h2>Leaderboard</h2>

      {/* Filter toggle */}
      <div className="btn-group mb-3">
        <button
          className={`btn ${view === "teams" ? "btn-primary" : "btn-outline-primary"}`}
          onClick={() => setView("teams")}
        >
          🏆 Teams
        </button>
        <button
          className={`btn ${view === "players" ? "btn-primary" : "btn-outline-primary"}`}
          onClick={() => setView("players")}
        >
          🎮 Players
        </button>
      </div>

      {view === "teams" ? (
        <table className="table table-dark table-striped">
          <thead>
            <tr>
              <th>#</th>
              <th>Team</th>
              <th>Rank</th>
              <th>Rating</th>
              <th>W</th>
              <th>L</th>
            </tr>
          </thead>
          <tbody>
            {sortedTeams.map((t, idx) => (
              <tr key={t.id}>
                <td>{idx + 1}</td>
                <td>{t.name}</td>

                {/* ⭐ DIVISION + TIER */}
                <td>
                  <span className={getRankClass(t.division)}>
                    {t.division} {t.tier}
                  </span>
                </td>

                <td>{t.rating}</td>
                <td>{t.wins}</td>
                <td>{t.losses}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <table className="table table-dark table-striped">
          <thead>
            <tr>
              <th>#</th>
              <th>Player</th>
              <th>Rank</th>
              <th>Rating</th>
              <th>W</th>
              <th>L</th>
              <th>GP</th>
            </tr>
          </thead>
          <tbody>
            {sortedPlayers.map((p, idx) => (
              <tr key={p.id}>
                <td>{idx + 1}</td>
                <td>{p.display_name || p.username}</td>

                {/* ⭐ DIVISION + TIER */}
                <td>
                  <span className={getRankClass(p.division)}>
                    {p.division} {p.tier}
                  </span>
                </td>

                <td>{p.rating}</td>
                <td>{p.wins}</td>
                <td>{p.losses}</td>
                <td>{p.matches}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
