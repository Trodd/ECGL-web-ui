import { useEffect, useState, useMemo } from "react";
import { Link } from "react-router-dom";
import axios from "axios";

export default function Teams() {
  const [teams, setTeams] = useState([]);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");

  useEffect(() => {
    let canceled = false;

    async function loadTeams() {
      try {
        setError("");
        const res = await axios.get(`${import.meta.env.VITE_API_URL}/api/teams`, {
          timeout: 15000,
        });

        if (!Array.isArray(res.data)) {
          setTeams([]);
          setError("Unexpected response format.");
          return;
        }

        if (!canceled) setTeams(res.data);
      } catch (err) {
        console.error("❌ Failed to load teams:", err);
        if (!canceled) {
          setError("Failed to load teams.");
          setTeams([]);
        }
      }
    }

    loadTeams();
    return () => {
      canceled = true;
    };
  }, []);

  // --- Helpers ---
  const safeLower = (v) => (typeof v === "string" ? v.toLowerCase() : "");
  const safeIncludes = (haystack, needle) =>
    safeLower(haystack).includes(safeLower(needle));

  // --- Filter teams by search ---
  const filteredTeams = useMemo(() => {
    const query = safeLower(search);
    return (Array.isArray(teams) ? teams : []).filter(
      (t) => !query || safeIncludes(t.name, query)
    );
  }, [teams, search]);

  return (
    <div>
      <h2>👥 Teams</h2>

      {error && <div className="alert alert-danger">{error}</div>}

      {/* 🔍 Search bar */}
      <div className="d-flex mb-3">
        <input
          type="text"
          className="form-control"
          style={{ maxWidth: 300 }}
          placeholder="🔍 Search team name..."
          value={search}
          onChange={(e) => setSearch(e.target.value || "")}
        />
      </div>

      {filteredTeams.length === 0 ? (
        <p>No teams found.</p>
      ) : (
        <ul className="list-group">
          {filteredTeams.map((t) => (
            <li key={t.id} className="list-group-item">
              <Link to={`/teams/${t.id}`}>
                <b>{t.name}</b>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}