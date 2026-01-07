import { useEffect, useState, useMemo } from "react";
import { Link } from "react-router-dom";
import axios from "axios";

export default function Teams() {
  const [teams, setTeams] = useState([]);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [settings, setSettings] = useState(null);
  const urlBase = import.meta.env.VITE_API_URL || "http://localhost:8080";

  function buildLogoSrc(logoUrl) {
    if (!logoUrl) return "";
    const base = String(logoUrl);
    const absolute = base.startsWith("http://") || base.startsWith("https://");
    return absolute ? base : `${urlBase}${base}`;
  }

  useEffect(() => {
    axios
      .get(`${urlBase}/api/settings`)
      .then(res => setSettings(res.data))
      .catch(() => setSettings({ challenges_enabled: true }));
  }, [urlBase]);

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
  }, [urlBase]);

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
    <div className="d-flex justify-content-center">
      <div style={{ width: "100%", maxWidth: 680 }}>
        <div className="card bg-dark border-secondary p-4 shadow-sm">
          <h2 className="mb-3">👥 Teams</h2>

          {error && (
            <div className="alert alert-danger small mb-3">
              {error}
            </div>
          )}

          {/* ================= SEARCH ================= */}
          <div className="mb-4" style={{ maxWidth: 320 }}>
            <input
              type="text"
              className="form-control bg-dark text-light"
              placeholder="🔍 Search team name…"
              value={search}
              onChange={(e) => setSearch(e.target.value || "")}
            />
          </div>

          {/* ================= TEAM LIST ================= */}
          {filteredTeams.length === 0 ? (
            <p className="text-secondary">No teams found.</p>
          ) : (
            <div className="d-flex flex-column gap-2">
              {filteredTeams.map((t) => (
                <Link
                  key={t.id}
                  to={`/teams/${t.id}`}
                  className="text-decoration-none"
                >
                  <div
                    className="border rounded p-3 d-flex justify-content-between align-items-center"
                    style={{
                      backgroundColor: "#1c1c1c",
                      borderColor: "#3a3a3a",
                    }}
                  >
                    {/* LEFT — Team Name */}
                    <div className="d-flex align-items-center gap-2 fw-semibold text-light">
                      <img
                        src={buildLogoSrc(t?.logo_url || (t?.id ? `/api/team/logo/${t.id}` : ""))}
                        alt=""
                        className="rounded border border-secondary"
                        style={{ width: 40, height: 40, objectFit: "cover" }}
                        loading="lazy"
                        onError={(e) => {
                          e.currentTarget.style.display = "none";
                        }}
                      />
                      <span>{t.name}</span>

                      {/* 🏆 Accepting Challenges */}
                      {t.status === "Active" &&
                        settings?.challenges_enabled &&
                        t.allow_challenges && (
                          <span
                            className="badge bg-warning text-dark"
                            title="Accepting challenge matches"
                          >
                            🏆
                          </span>
                        )}

                      {/* 🔒 Inactive / Disbanded */}
                      {t.status !== "Active" && (
                        <span
                          className="badge bg-danger"
                          title="This team cannot receive or issue challenges"
                        >
                          🔒
                        </span>
                      )}
                    </div>

                    {/* RIGHT — Status */}
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
                  </div>
                </Link>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
