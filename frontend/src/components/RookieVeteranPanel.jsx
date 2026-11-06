import { useEffect, useMemo, useState } from "react";
import axios from "axios";
import { toast } from "../toast";
import PlayerIdentity from "./PlayerIdentity";
import { leagueLink } from "../league";

/**
 * Rookie League veteran management.
 *
 * Veterans are Coach-only in the Rookie League — they sit on a roster but never
 * play. Two mod actions:
 *   • Mark a new player as a veteran (they never played Varsity but are clearly
 *     too strong for the league).
 *   • Allow a veteran to play as a normal rookie (the reverse override).
 */
export default function RookieVeteranPanel({ urlBase }) {
  const [rows, setRows] = useState([]);
  const [ruleOn, setRuleOn] = useState(true);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState("");
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState("all");

  async function load() {
    try {
      const res = await axios.get(`${urlBase}/api/mod/rookie/veterans`, {
        withCredentials: true,
        params: { league: "rookie" },
      });
      setRows(
        Array.isArray(res.data?.players)
          ? res.data.players
          : Array.isArray(res.data?.veterans)
            ? res.data.veterans
            : []
      );
      setRuleOn(res.data?.rule_on !== false);
      toast("");
    } catch (err) {
      const detail =
        typeof err.response?.data === "string"
          ? err.response.data.trim()
          : err.response?.data?.error || "";
      toast(
        `❌ Failed to load players${detail ? `: ${detail}` : " — is the backend up to date?"}`
      );
      setRows([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function callAction(row, path, body, okMessage) {
    if (!row) return;
    setBusyId(row.player_id);
    try {
      await axios.post(
        `${urlBase}${path}`,
        { ...body, league: "rookie" },
        { withCredentials: true }
      );
      toast(okMessage);
      await load();
    } catch (err) {
      const detail =
        typeof err.response?.data === "string"
          ? err.response.data.trim()
          : err.response?.data?.error || "";
      toast("❌ " + (detail || "Action failed"));
    } finally {
      setBusyId("");
    }
  }

  const setVeteran = (row, veteran) =>
    callAction(
      row,
      "/api/mod/rookie/veteran-flag",
      { player_id: row.player_id, veteran },
      `✅ ${row.display_name || row.username} ${
        veteran ? "marked as a veteran — Coach only" : "is a new player again"
      }.`
    );

  const setPlayOverride = (row, allow) =>
    callAction(
      row,
      "/api/mod/rookie/play-override",
      { player_id: row.player_id, allow },
      `✅ ${row.display_name || row.username} ${
        allow ? "can now play as a rookie" : "is Coach-only again"
      }.`
    );

  const visible = useMemo(() => {
    const q = search.trim().toLowerCase();
    return rows.filter((r) => {
      if (filter === "veterans" && !r.is_veteran) return false;
      if (filter === "new" && r.is_veteran) return false;
      if (filter === "coachonly" && !r.locked_to_coach) return false;
      if (!q) return true;
      return (
        (r.display_name || "").toLowerCase().includes(q) ||
        (r.username || "").toLowerCase().includes(q) ||
        (r.team_name || "").toLowerCase().includes(q)
      );
    });
  }, [rows, search, filter]);

  const vetCount = rows.filter((r) => r.is_veteran).length;
  const allowedCount = rows.filter((r) => r.play_override).length;
  const forcedCount = rows.filter((r) => r.force_veteran).length;

  return (
    <div>
      <h5 className="text-info mb-1">🧑‍🏫 Rookie Eligibility</h5>
      <p className="text-secondary small mb-3">
        Veterans are <b>Coach only</b> in the Rookie League — they can sit on a
        roster but never play. Mark a seasoned player as a veteran even if they
        never played Varsity, or let a veteran play as a normal rookie.
      </p>

      {!ruleOn && (
        <div className="alert ecgl-alert-warning small py-2">
          The veteran rule is <b>disabled</b> in League Settings, so nobody is
          Coach-only right now and these marks have no effect.
        </div>
      )}

      <div className="d-flex flex-wrap gap-2 align-items-center mb-3">
        <input
          className="form-control form-control-sm bg-dark text-light"
          style={{ maxWidth: 240 }}
          placeholder="🔍 Search player or team"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <div className="btn-group btn-group-sm flex-wrap">
          {[
            { key: "all", label: `All (${rows.length})` },
            { key: "veterans", label: `Veterans (${vetCount})` },
            { key: "new", label: "New players" },
            { key: "coachonly", label: "Coach only" },
          ].map((f) => (
            <button
              key={f.key}
              type="button"
              className={`btn btn-outline-secondary ${filter === f.key ? "active" : ""}`}
              onClick={() => setFilter(f.key)}
            >
              {f.label}
            </button>
          ))}
        </div>
        <span className="text-secondary small ms-auto">
          {allowedCount} allowed to play
          {forcedCount > 0 ? ` • ${forcedCount} marked by a mod` : ""}
        </span>
        <button className="btn btn-outline-secondary btn-sm" onClick={load}>
          ↻ Refresh
        </button>
      </div>

      {loading ? (
        <p className="text-secondary small mb-0">Loading players…</p>
      ) : visible.length === 0 ? (
        <p className="text-secondary small mb-0">No players match your filter.</p>
      ) : (
        <div className="d-flex flex-column gap-2">
          {visible.map((r) => (
            <div
              key={r.player_id}
              className="d-flex flex-wrap align-items-center gap-2 p-2 rounded"
              style={{
                background: "#0f0f0f",
                border: `1px solid ${
                  r.play_override ? "#198754" : r.is_veteran ? "#8a6d3b" : "#2c2c2c"
                }`,
              }}
            >
              <PlayerIdentity
                player={{
                  id: r.player_id,
                  display_name: r.display_name,
                  username: r.username,
                  avatar: r.avatar,
                }}
                linkTo={leagueLink(`/players/${r.player_id}`)}
              />

              {r.is_veteran ? (
                <span
                  className={`badge ${r.play_override ? "bg-success" : "bg-warning text-dark"}`}
                >
                  {r.play_override ? "Veteran — play allowed" : "Veteran — Coach only"}
                </span>
              ) : (
                <span className="badge bg-success">New player</span>
              )}

              {r.force_veteran && (
                <span
                  className="badge bg-secondary"
                  title="Marked as a veteran by a league mod"
                >
                  🏷️ Marked by mod
                </span>
              )}
              {r.varsity_veteran && (
                <span
                  className="badge bg-dark border border-secondary"
                  title="Played in Varsity"
                >
                  Varsity
                </span>
              )}
              {r.is_coach && (
                <span
                  className="badge"
                  style={{
                    background: "#0b3540",
                    color: "#67e8f9",
                    border: "1px solid #0dcaf0",
                  }}
                >
                  🧑‍🏫 Coach
                </span>
              )}

              <span className="text-secondary small">
                {r.team_name
                  ? `On ${r.team_name}${r.team_role ? ` (${r.team_role})` : ""}`
                  : "No team yet"}
              </span>

              <div className="ms-auto d-flex gap-2">
                {r.is_veteran && (
                  <button
                    className={`btn btn-sm ${r.play_override ? "btn-outline-secondary" : "btn-outline-info"}`}
                    disabled={busyId === r.player_id || (!r.play_override && r.explicit_coach)}
                    title={
                      !r.play_override && r.explicit_coach
                        ? "They registered as a coach on purpose — remove that first"
                        : "Toggle the Rookie League veteran rule for this player"
                    }
                    onClick={() => setPlayOverride(r, !r.play_override)}
                  >
                    {busyId === r.player_id
                      ? "Saving…"
                      : r.play_override
                        ? "↩ Coach only"
                        : "✅ Allow play as rookie"}
                  </button>
                )}

                {!r.varsity_veteran && (
                  <button
                    className={`btn btn-sm ${r.force_veteran ? "btn-outline-secondary" : "btn-outline-warning"}`}
                    disabled={busyId === r.player_id}
                    title={
                      r.force_veteran
                        ? "Undo the veteran mark — back to a normal new player"
                        : "Never played Varsity but too strong for the league? Mark them as a veteran"
                    }
                    onClick={() => setVeteran(r, !r.force_veteran)}
                  >
                    {busyId === r.player_id
                      ? "Saving…"
                      : r.force_veteran
                        ? "↩ Unmark veteran"
                        : "🏷️ Mark as veteran"}
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
