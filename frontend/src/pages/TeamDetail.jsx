import { useEffect, useMemo, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import axios from "axios";

export default function TeamDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [team, setTeam] = useState(null);
  const [selectedSeason, setSelectedSeason] = useState("All");
  const [currentSeason, setCurrentSeason] = useState("Preseason");
  const [error, setError] = useState(null);

  // --- Fetch team details ---
  useEffect(() => {
    setError(null);
    axios
      .get(`${import.meta.env.VITE_API_URL}/api/team/${id}`)
      .then((res) => setTeam(res.data))
      .catch(() => setError("Failed to load team details"));
  }, [id]);

  // --- Fetch current season from backend (.env-driven) ---
  useEffect(() => {
    axios
      .get(`${import.meta.env.VITE_API_URL}/api/season`)
      .then((res) => {
        if (res.data?.season) {
          let s = res.data.season.toString().trim();
          if (/^\d+$/.test(s)) s = `Season ${s}`;
          setCurrentSeason(s);
        }
      })
      .catch(() => setCurrentSeason("Preseason"));
  }, []);

  const matches = Array.isArray(team?.matches) ? team.matches : [];
  const roster = Array.isArray(team?.roster) ? team.roster : [];

  // --- Build dynamic season filter ---
  const allSeasons = useMemo(() => {
    const base = ["Preseason"];
    if (currentSeason && !base.includes(currentSeason))
      base.push(currentSeason);
    return ["All", ...base];
  }, [currentSeason]);

  // --- Filter matches ---
  const filteredMatches = useMemo(() => {
    if (!matches.length) return [];

    if (selectedSeason === "All") return matches;

    return matches.filter((m) => {
      const code = (m.match_code || "").toLowerCase();
      if (selectedSeason === "Preseason") {
        // ✅ Any match that doesn’t specify S1/S2/etc = Preseason
        return (
          code.includes("week") ||
          (!code.includes("s1") &&
            !code.includes("s2") &&
            !code.includes("s3") &&
            !code.includes("season"))
        );
      }

      // ✅ Otherwise match against season keyword
      const keyword = selectedSeason.toLowerCase().replace(" ", "");
      return code.includes(keyword);
    });
  }, [matches, selectedSeason]);

  if (error) return <p className="text-danger">{error}</p>;
  if (!team) return <p className="text-muted">Loading team...</p>;

  return (
    <div>
      <h2 className="text-light">{team.name}</h2>
      <p>
        Status:{' '}
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

      <h3>Roster</h3>
      {roster.length ? (
        <ul className="list-group roster-list">
          {roster.map((p) => (
            <li
              key={p.id ?? Math.random()}
              className="list-group-item d-flex justify-content-between align-items-center"
            >
              <strong>{p.display_name || p.username || "Unknown"}</strong>
              <span className={`roster-role ${p.role?.toLowerCase() || ""}`}>
                {p.role || "-"}
              </span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-muted">No roster found.</p>
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
                  title="View match details"
                >
                  <td>{idx + 1}</td>
                  <td className="fw-semibold">
                    {m.opponent ? (
                      <a
                        href={`/teams/${m.opponent_id}`}
                        className={`text-decoration-none ${m.result === "Win"
                          ? "text-success"
                          : m.result === "Loss"
                            ? "text-danger"
                            : "text-warning"
                          }`}
                        onClick={(e) => e.stopPropagation()}
                      >
                        {m.opponent}
                      </a>
                    ) : (
                      "Unknown"
                    )}
                  </td>
                  <td style={{ whiteSpace: "nowrap" }}>
                    {m.date ? new Date(m.date).toLocaleDateString() : "-"}
                  </td>
                  <td
                    className={`fw-bold ${m.result === "Win"
                      ? "text-success"
                      : m.result === "Loss"
                        ? "text-danger"
                        : "text-warning"
                      }`}
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
        <p className="text-light">
          No matches found for {selectedSeason}.
        </p>
      )}
    </div>
  );
}
