import { useEffect, useState } from "react";
import axios from "axios";
import { getApiUrl } from "../config";
import { E } from "../components/CustomEmoji";

export default function Leaderboard() {
  const [view, setView] = useState("teams"); // default: teams
  const [teams, setTeams] = useState([]);
  const [players, setPlayers] = useState([]);

  useEffect(() => {
    if (view === "teams") {
      axios.get(`${getApiUrl()}/api/leaderboard/teams`)
        .then(res => setTeams(res.data || []));
    } else {
      axios.get(`${getApiUrl()}/api/leaderboard/players`)
        .then(res => setPlayers(res.data || []));
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
    <div
      className="card bg-dark border-secondary p-4 shadow-sm mx-auto"
      style={{ maxWidth: 900 }}
    >
      {/* ================= HEADER ================= */}
      <div className="d-flex justify-content-between align-items-center mb-3">
        <h2 className="mb-0 text-light"><E n="trophy" className="emoji-gold" /> Leaderboard</h2>

        {/* Toggle */}
        <div className="btn-group">
          <button
            className={`btn btn-sm ${view === "teams" ? "btn-primary" : "btn-outline-primary"
              }`}
            onClick={() => setView("teams")}
          >
            <E n="trophy" /> Teams
          </button>
          <button
            className={`btn btn-sm ${view === "players" ? "btn-primary" : "btn-outline-primary"
              }`}
            onClick={() => setView("players")}
          >
            <E n="gamepad" /> Players
          </button>
        </div>
      </div>

      {/* ================= CONTENT ================= */}
      {view === "teams" ? (
        <div className="table-responsive">
          <table className="table table-dark table-hover align-middle mb-0">
            <thead>
              <tr>
                <th style={{ width: 50 }}>#</th>
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
                  <td className="fw-bold text-secondary">{idx + 1}</td>

                  <td className="fw-semibold">
                    {t.name}
                  </td>

                  <td>
                    <span className={`rank-badge ${getRankClass(t.division)}`}>
                      {t.in_placement ? "Placement" : `${t.division} ${t.tier}`}
                    </span>
                  </td>

                  <td className="fw-bold">{t.in_placement ? "—" : t.rating}</td>
                  <td className="text-success fw-semibold">{t.wins}</td>
                  <td className="text-danger fw-semibold">{t.losses}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="table-responsive">
          <table className="table table-dark table-hover align-middle mb-0">
            <thead>
              <tr>
                <th style={{ width: 50 }}>#</th>
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
                  <td className="fw-bold text-secondary">{idx + 1}</td>

                  <td className="fw-semibold">
                    {p.display_name || p.username}
                  </td>

                  <td>
                    <span className={`rank-badge ${getRankClass(p.division)}`}>
                      {p.division} {p.tier}
                    </span>
                  </td>

                  <td className="fw-bold">{p.rating}</td>
                  <td className="text-success fw-semibold">{p.wins}</td>
                  <td className="text-danger fw-semibold">{p.losses}</td>
                  <td className="fw-semibold">{p.matches}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
