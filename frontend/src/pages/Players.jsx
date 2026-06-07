import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import axios from "axios";
import { getApiUrl } from "../config";

export default function Players() {
  const [players, setPlayers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [roleFilter, setRoleFilter] = useState("all");
  const [me, setMe] = useState(null);
  const [copiedAvatar, setCopiedAvatar] = useState(null);

  useEffect(() => {
    let canceled = false;

    async function load() {
      try {
        setLoading(true);
        setError("");

        const urlBase = getApiUrl();
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
            username: typeof p.username === "string" ? p.username : "",
            avatar: typeof p.avatar === "string" ? p.avatar : "",
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
    axios
      .get(`${getApiUrl()}/api/me`, { withCredentials: true })
      .then((res) => setMe(res.data))
      .catch(() => setMe(null));
    return () => {
      canceled = true;
    };
  }, []);

  const canCopyAvatar = !!me?.is_caster || !!me?.is_mod;

  async function handleCopyAvatarUrl(p) {
    const url = getDiscordAvatarUrl(p);
    try {
      if (navigator?.clipboard?.writeText) {
        await navigator.clipboard.writeText(url);
      } else {
        window.prompt("Copy avatar URL:", url);
      }
      setCopiedAvatar(p.id);
      window.setTimeout(() => setCopiedAvatar(null), 1500);
    } catch {
      window.prompt("Copy avatar URL:", url);
    }
  }

  const safeLower = (v) => (typeof v === "string" ? v.toLowerCase() : "");
  const safeIncludes = (haystack, needle) =>
    safeLower(haystack).includes(safeLower(needle));

  const getDiscordAvatarUrl = (player) => {
    if (!player?.id) return "https://cdn.discordapp.com/embed/avatars/0.png";
    if (player.avatar) {
      return `https://cdn.discordapp.com/avatars/${player.id}/${player.avatar}.png?size=64`;
    }
    try {
      const idx = Number((BigInt(player.id) >> 22n) % 6n);
      return `https://cdn.discordapp.com/embed/avatars/${idx}.png`;
    } catch {
      return "https://cdn.discordapp.com/embed/avatars/0.png";
    }
  };

  const filteredPlayers = useMemo(() => {
    const srch = safeLower(search);
    const rf = safeLower(roleFilter);

    return (Array.isArray(players) ? players : []).filter((p) => {
      const matchesSearch =
        !srch ||
        safeIncludes(p.display_name || "", srch) ||
        safeIncludes(p.username || "", srch);
      const matchesRole =
        rf === "all" || safeLower(p.role || "") === rf;
      return matchesSearch && matchesRole;
    });
  }, [players, search, roleFilter]);

  return (
    <div className="d-flex justify-content-center">
      <div style={{ width: "100%", maxWidth: 760 }}>
        <div className="card bg-dark border-secondary p-3 shadow-sm">
          <h2 className="text-light mb-2">👥 Registered Players</h2>

          {error && (
            <div className="alert alert-danger small py-2 px-3 mb-2">
              {error}
            </div>
          )}

          {/* ================= FILTER BAR ================= */}
          <div className="d-flex flex-wrap gap-2 mb-3 align-items-center">
            <input
              type="text"
              className="form-control form-control-sm bg-dark text-light"
              style={{ flex: "1 1 200px" }}
              placeholder="🔍 Search player"
              value={search}
              onChange={(e) => setSearch(e.target.value || "")}
            />

            <select
              className="form-select form-select-sm bg-dark text-light"
              style={{ flex: "0 0 160px" }}
              value={roleFilter}
              onChange={(e) => setRoleFilter(e.target.value || "all")}
            >
              <option value="all">All Roles</option>
              <option value="player">Player</option>
              <option value="league sub">League Sub</option>
              <option value="banned">Banned</option>
            </select>
          </div>

          {/* ================= PLAYER LIST ================= */}
          {loading ? (
            <p className="text-secondary small mb-0">Loading players…</p>
          ) : filteredPlayers.length === 0 ? (
            <p className="text-secondary small mb-0">No players found.</p>
          ) : (
            <div className="d-flex flex-column gap-1">
              {filteredPlayers.map((p, idx) => (
                <div
                  key={p.id ?? idx}
                  className="border rounded px-3 py-2 d-flex justify-content-between align-items-center"
                  style={{
                    borderColor: "#3a3a3a",
                    backgroundColor: "#1c1c1c",
                  }}
                >
                  {/* LEFT */}
                  <div className="d-flex align-items-center gap-2 lh-sm">
                    <img
                      src={getDiscordAvatarUrl(p)}
                      alt=""
                      className="players-discord-avatar"
                      loading="lazy"
                      style={canCopyAvatar ? {
                        cursor: "pointer",
                        border: copiedAvatar === p.id ? "2px solid #28a745" : "2px solid transparent",
                        borderRadius: "50%",
                        transition: "border-color 0.2s, box-shadow 0.2s",
                      } : undefined}
                      title={canCopyAvatar ? "Copy avatar URL" : undefined}
                      onClick={canCopyAvatar ? () => handleCopyAvatarUrl(p) : undefined}
                      onMouseEnter={canCopyAvatar ? (e) => { e.currentTarget.style.boxShadow = "0 0 0 3px rgba(13,202,240,0.45)"; } : undefined}
                      onMouseLeave={canCopyAvatar ? (e) => { e.currentTarget.style.boxShadow = "none"; } : undefined}
                      onError={(e) => {
                        e.currentTarget.onerror = null;
                        e.currentTarget.src = "https://cdn.discordapp.com/embed/avatars/0.png";
                      }}
                    />

                    <div className="d-flex flex-column players-name-block">
                      <span className="fw-semibold">
                        {p.id ? (
                          <Link
                            to={`/players/${p.id}`}
                            className="text-info text-decoration-none"
                          >
                            {p.display_name || "Unknown"}
                          </Link>
                        ) : (
                          p.display_name || "Unknown"
                        )}
                      </span>

                      <span className="players-discord-username">
                        @{p.username || "unknown"}
                      </span>

                      <span className="text-secondary" style={{ fontSize: "0.8rem" }}>
                        {p.timezone || "No timezone"}
                      </span>
                    </div>
                  </div>

                  {/* RIGHT */}
                  <span
                    className={`rank-badge ${p.role === "Banned"
                      ? "rank-bronze"
                      : p.role === "League Sub"
                        ? "rank-silver"
                        : "rank-gold"
                      }`}
                    style={{ fontSize: "0.75rem" }}
                  >
                    {p.role || "Player"}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
