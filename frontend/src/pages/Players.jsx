import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import axios from "axios";

export default function Players() {
  const [players, setPlayers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [roleFilter, setRoleFilter] = useState("all");

  useEffect(() => {
    let canceled = false;

    async function load() {
      try {
        setLoading(true);
        setError("");

        const urlBase = import.meta?.env?.VITE_API_URL || "";
        const res = await axios.get(`${urlBase}/api/players`, {
          withCredentials: true,
          timeout: 15000,
        });

        if (!Array.isArray(res.data)) {
          setPlayers([]);
          setError("Unexpected API response format.");
          return;
        }

        // ✅ Normalize data and prefer DisplayName
        const normalized = res.data
          .filter((x) => x && typeof x === "object")
          .map((p) => ({
            id:
              typeof p.id === "string" || typeof p.id === "number"
                ? String(p.id)
                : undefined,
            display_name:
              typeof p.display_name === "string"
                ? p.display_name
                : p.username || "",
            role: typeof p.role === "string" ? p.role : "",
            timezone: typeof p.timezone === "string" ? p.timezone : "",
          }));

        if (!canceled) setPlayers(normalized);
      } catch (err) {
        console.error("❌ Failed to load players:", err);
        if (!canceled) {
          setPlayers([]);
          setError("Failed to load players. Please try again.");
        }
      } finally {
        if (!canceled) setLoading(false);
      }
    }

    load();
    return () => {
      canceled = true;
    };
  }, []);

  const safeLower = (v) => (typeof v === "string" ? v.toLowerCase() : "");
  const safeIncludes = (haystack, needle) =>
    safeLower(haystack).includes(safeLower(needle));

  const filteredPlayers = useMemo(() => {
    const srch = safeLower(search);
    const rf = safeLower(roleFilter);

    return (Array.isArray(players) ? players : []).filter((p) => {
      const matchesSearch = !srch || safeIncludes(p.display_name || "", srch);
      const matchesRole =
        rf === "all" || safeLower(p.role || "") === rf;
      return matchesSearch && matchesRole;
    });
  }, [players, search, roleFilter]);

  return (
    <div>
      <h2>👥 Registered Players</h2>

      {error && <div className="alert alert-danger">{error}</div>}

      {/* Filters */}
      <div className="d-flex flex-wrap gap-2 mb-3">
        <input
          type="text"
          className="form-control"
          style={{ maxWidth: 240, minWidth: 180 }}
          placeholder="🔍 Search name"
          value={search}
          onChange={(e) => setSearch(e.target.value || "")}
        />

        <select
          className="form-select"
          style={{ maxWidth: 200, minWidth: 160 }}
          value={roleFilter}
          onChange={(e) => setRoleFilter(e.target.value || "all")}
        >
          <option value="all">All Roles</option>
          <option value="player">Player</option>
          <option value="league sub">League Sub</option>
        </select>
      </div>

      {loading ? (
        <p>Loading players…</p>
      ) : filteredPlayers.length === 0 ? (
        <p>No players found.</p>
      ) : (
        <table className="table table-dark table-striped">
          <thead>
            <tr>
              <th>#</th>
              <th>Display Name</th>
              <th>Role</th>
              <th>Timezone</th>
            </tr>
          </thead>
          <tbody>
            {filteredPlayers.map((p, idx) => {
              const key = p?.id ?? `row-${idx}`;
              return (
                <tr key={key}>
                  <td>{idx + 1}</td>
                  <td>
                    {p.id ? (
                      <Link
                        to={`/players/${p.id}`}
                        className="text-info text-decoration-none"
                        style={{ fontWeight: "bold" }}
                      >
                        {p.display_name || "Unknown"}
                      </Link>
                    ) : (
                      p.display_name || "Unknown"
                    )}
                  </td>
                  <td>{p.role || "-"}</td>
                  <td>{p.timezone || "-"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}