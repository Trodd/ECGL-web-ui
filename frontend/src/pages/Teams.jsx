import { useEffect, useState, useMemo } from "react";
import { Link } from "react-router-dom";
import axios from "axios";

export default function Teams() {
  const [teams, setTeams] = useState([]);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [settings, setSettings] = useState(null);

  useEffect(() => {
    axios
      .get(`${import.meta.env.VITE_API_URL}/api/settings`)
      .then(res => setSettings(res.data))
      .catch(() => setSettings({ challenges_enabled: true }));
  }, []);

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

  const safeLower = (v) => (typeof v === "string" ? v.toLowerCase() : "");
  const safeIncludes = (haystack, needle) =>
    safeLower(haystack).includes(safeLower(needle));

  const filteredTeams = useMemo(() => {
    const query = safeLower(search);
    return (Array.isArray(teams) ? teams : []).filter(
      (t) => !query || safeIncludes(t.name, query)
    );
  }, [teams, search]);

  return (
    <div className="text-light mx-auto" style={{ maxWidth: "600px" }}>
      <h2 className="mb-3">👥 Teams</h2>

      {error && <div className="alert alert-danger">{error}</div>}

      {/* 🔍 Search bar */}
      <div className="d-flex mb-3">
        <input
          type="text"
          className="form-control bg-dark text-light"
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
            <li
              key={t.id}
              className="list-group-item d-flex justify-content-between align-items-center px-3 py-2 mb-2"
              style={{
                backgroundColor: "#1c1c1c",
                border: "1px solid #2a2a2a",
                borderRadius: "0.5rem",
              }}
            >
              <Link
                to={`/teams/${t.id}`}
                className="text-decoration-none text-light fw-semibold d-flex align-items-center"
              >
                {t.name}

                {/* 🏆 Only show trophy if team is Active AND allows challenges */}
                {t.status === "Active" && settings?.challenges_enabled && t.allow_challenges && (
                  <span
                    style={{
                      marginLeft: "8px",
                      color: "#ffd700",
                      fontSize: "1.2rem",
                    }}
                    title="This team is accepting challenge matches"
                  >
                    🏆
                  </span>
                )}

                {/* 🚫 Indicate inactive/disbanded */}
                {t.status !== "Active" && (
                  <span
                    style={{ marginLeft: "8px", color: "#ff4444", fontSize: "1.1rem" }}
                    title="This team cannot receive or issue challenges"
                  >
                    🔒
                  </span>
                )}
              </Link>

              <span
                className={`badge ${t.status === "Active"
                  ? "bg-success"
                  : t.status === "Disbanded"
                    ? "bg-danger"
                    : "bg-secondary"
                  }`}
              >
                {t.status || "Unknown"}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
