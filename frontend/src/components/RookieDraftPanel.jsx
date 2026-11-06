import { Fragment, useState, useEffect } from "react";
import axios from "axios";
import PlayerIdentity from "./PlayerIdentity";
import { leagueLink } from "../league";

/**
 * Rookie League draft panel.
 *
 * Lists players who signed up for the rookie draft from the Register tab.
 * Mods can assign each of them to an existing rookie team, or create a new
 * team for them (they become captain).
 */
export default function RookieDraftPanel({ urlBase }) {
    const [rows, setRows] = useState([]);
    const [selected, setSelected] = useState({}); // signup_id -> selected for a new team
    const [groupName, setGroupName] = useState("");
    const [captain, setCaptain] = useState("");
    const [groupForce, setGroupForce] = useState(false);
    const [busyId, setBusyId] = useState(0);
    const [loading, setLoading] = useState(true);
    const [msg, setMsg] = useState("");
    const [rosters, setRosters] = useState([]);
    const [maxSlots, setMaxSlots] = useState(0);
    const [draggingId, setDraggingId] = useState(0);
    const [draggingMember, setDraggingMember] = useState("");
    const [draggingFrom, setDraggingFrom] = useState(0);
    const [dragOverTeam, setDragOverTeam] = useState(0);
    const [dragOverDraft, setDragOverDraft] = useState(false);

    async function load() {
        try {
            const draftRes = await axios.get(`${urlBase}/api/mod/draft`, {
                withCredentials: true,
            });
            setRows(Array.isArray(draftRes.data?.draft) ? draftRes.data.draft : []);
            setMsg("");
        } catch (err) {
            const status = err.response?.status;
            const detail =
                typeof err.response?.data === "string"
                    ? err.response.data.trim()
                    : err.response?.data?.error || "";
            setMsg(
                `❌ Failed to load the draft${status ? ` (${status})` : ""}${detail ? `: ${detail}` : " — is the backend up to date?"}`
            );
            setRows([]);
        }

        try {
            const rosterRes = await axios.get(`${urlBase}/api/mod/draft/teams`, {
                withCredentials: true,
            });
            setRosters(Array.isArray(rosterRes.data?.teams) ? rosterRes.data.teams : []);
            setMaxSlots(Number(rosterRes.data?.max_slots) || 0);
        } catch {
            setRosters([]);
        } finally {
            setLoading(false);
        }
    }

    useEffect(() => {
        load();
    }, []);

    async function assignToTeam(row, teamID) {
        if (!row || !teamID) return;
        setBusyId(row.id);
        setMsg("");
        try {
            await axios.post(
                `${urlBase}/api/mod/draft/assign`,
                { signup_id: row.id, team_id: teamID, override: groupForce },
                { withCredentials: true }
            );
            const teamName = rosters.find((t) => t.id === teamID)?.name || "a team";
            setMsg(`✅ Assigned ${row.display_name || row.username} to ${teamName}.`);
            await load();
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to assign player"));
        } finally {
            setBusyId(0);
        }
    }

    async function transferPlayer(playerID, toTeamID) {
        if (!playerID || !toTeamID) return;
        setBusyId(-2);
        setMsg("");
        try {
            const res = await axios.post(
                `${urlBase}/api/mod/draft/transfer`,
                { player_id: String(playerID), to_team_id: toTeamID, override: groupForce },
                { withCredentials: true }
            );
            setMsg("✅ " + (res.data?.message || "Player moved."));
            await load();
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to move player"));
        } finally {
            setBusyId(0);
        }
    }

    async function releaseToDraft(playerID) {
        if (!playerID) return;
        setBusyId(-2);
        setMsg("");
        try {
            const res = await axios.post(
                `${urlBase}/api/mod/draft/release`,
                { player_id: String(playerID) },
                { withCredentials: true }
            );
            setMsg("✅ " + (res.data?.message || "Player returned to the draft."));
            await load();
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to return player to the draft"));
        } finally {
            setBusyId(0);
        }
    }

    async function handleCreateGroup() {
        const ids = rows.filter((r) => selected[r.id]).map((r) => r.id);
        if (ids.length === 0) {
            setMsg("⚠️ Select at least one player first.");
            return;
        }
        // Blank name → the server generates a unique adjective + noun name.
        const name = groupName.trim();
        setBusyId(-1);
        try {
            const res = await axios.post(
                `${urlBase}/api/mod/draft/create-team`,
                {
                    signup_ids: ids,
                    name,
                    captain_player_id: captain || "",
                    override: groupForce,
                },
                { withCredentials: true }
            );
            const created = res.data?.team || name || "new team";
            setMsg(`✅ Created team ${created} with ${ids.length} drafted player(s).`);
            setGroupName("");
            setCaptain("");
            setSelected({});
            setGroupForce(false);
            await load();
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Failed to create team"));
        } finally {
            setBusyId(0);
        }
    }

    async function suggestName() {
        try {
            const res = await axios.get(`${urlBase}/api/mod/draft/team-name`, {
                withCredentials: true,
            });
            if (res.data?.name) {
                setGroupName(res.data.name);
                setMsg("");
            }
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Could not generate a name"));
        }
    }

    async function handleAutoAssign(kind = "player") {
        const isCoachRun = kind === "coach";
        const pool = isCoachRun
            ? rows.filter((r) => r.kind === "coach")
            : rows.filter((r) => r.kind !== "coach");

        if (pool.length === 0) {
            setMsg(`⚠️ There are no pending ${isCoachRun ? "coach " : ""}signups to assign.`);
            return;
        }

        const ok = confirm(
            isCoachRun
                ? `Auto-assign ${pool.length} coach(es) to the teams that don't have one yet?`
                : `Auto-assign ${pool.length} pending player(s)?\n\n` +
                  "Existing teams get topped up first, then new teams are created. " +
                  "The veteran ratio, team size limits and Force setting are respected."
        );
        if (!ok) return;

        setBusyId(-1);
        try {
            const res = await axios.post(
                `${urlBase}/api/mod/draft/auto-assign`,
                { override: groupForce, kind: isCoachRun ? "coach" : "player" },
                { withCredentials: true }
            );
            setMsg("✅ " + (res.data?.message || "Auto-assign complete."));
            setSelected({});
            await load();
        } catch (err) {
            setMsg("❌ " + (err.response?.data || "Auto-assign failed"));
        } finally {
            setBusyId(0);
        }
    }

    const isCoach = (row) => row.kind === "coach";
    const coachRows = rows.filter(isCoach);
    const playerRows = rows.filter((row) => !isCoach(row));
    const draftOrdered = [...playerRows, ...coachRows];

    return (
        <div className="rdp">
            <div
                className="d-flex align-items-center justify-content-between flex-wrap gap-2 mb-3 p-2 px-3 rounded"
                style={{ background: "#141414", border: "1px solid #7a3f10" }}
            >
                <div className="d-flex align-items-center gap-2 flex-wrap">
                    <span className="fw-bold text-light">📝 Rookie Draft Signups</span>
                    <span
                        className="badge"
                        style={{ background: "#242424", color: "#e6e6e6", border: "1px solid #3d3d3d" }}
                    >
                        {playerRows.length} player{playerRows.length === 1 ? "" : "s"}
                    </span>
                    {coachRows.length > 0 && (
                        <span
                            className="badge"
                            style={{ background: "#0b3540", color: "#67e8f9", border: "1px solid #0dcaf0" }}
                        >
                            🧑‍🏫 {coachRows.length} coach{coachRows.length === 1 ? "" : "es"}
                        </span>
                    )}
                </div>
                <div className="d-flex gap-2 flex-wrap">
                    <button
                        className="btn btn-warning btn-sm fw-semibold"
                        onClick={() => handleAutoAssign("player")}
                        disabled={busyId !== 0 || playerRows.length === 0}
                        title="Fill existing teams with players, then create new teams using the veteran rules"
                    >
                        {busyId === -1 ? "Working…" : "⚡ Auto-assign players"}
                    </button>
                    <button
                        className="btn btn-info btn-sm fw-semibold"
                        onClick={() => handleAutoAssign("coach")}
                        disabled={busyId !== 0 || coachRows.length === 0}
                        title="Give each coach signup a team that doesn't have a coach yet"
                    >
                        {busyId === -1 ? "Working…" : "🧑‍🏫 Auto-assign coaches"}
                    </button>
                    <button
                        className="btn btn-outline-light btn-sm"
                        onClick={load}
                        disabled={busyId !== 0}
                    >
                        ↻ Refresh
                    </button>
                </div>
            </div>

            <div className="rdp-note text-secondary small mb-3">
                Players below registered but don't have a Rookie League team. Assign them to an
                existing team, or select several and group them into a new one. A captain is
                optional — you can promote one later from Team Tools.
                <br />
                <span className="text-info">💡 On desktop you can drag a player card onto a team
                    on the right to assign them instantly.</span>
                {coachRows.length > 0 && (
                    <>
                        <br />
                        <span className="text-info">🧑‍🏫 Coach signups are listed below the players —
                            place each one on the team they'll coach.</span>
                    </>
                )}
            </div>

            {msg && (
                <div
                    className={`alert ${msg.startsWith("✅")
                            ? "ecgl-alert-success"
                            : msg.startsWith("⚠️")
                                ? "ecgl-alert-warning"
                                : "alert-danger"
                        } small`}
                >
                    {msg}
                </div>
            )}

            {loading && <p className="text-secondary small">Loading draft…</p>}

            {!loading && rows.length === 0 && (
                <p className="text-secondary small">No pending draft signups.</p>
            )}
            <div className="row g-3">
                <div
                    className="col-12 col-lg-7"
                    onDragOver={(e) => {
                        if (!draggingMember) return;
                        e.preventDefault();
                        e.dataTransfer.dropEffect = "move";
                    }}
                    onDragEnter={() => {
                        if (draggingMember) setDragOverDraft(true);
                    }}
                    onDragLeave={(e) => {
                        if (!e.currentTarget.contains(e.relatedTarget)) {
                            setDragOverDraft(false);
                        }
                    }}
                    onDrop={(e) => {
                        if (!draggingMember) return;
                        e.preventDefault();
                        const memberID =
                            e.dataTransfer.getData("application/x-ecgl-member") ||
                            (e.dataTransfer.getData("text/plain") || "").replace("member:", "");
                        setDragOverDraft(false);
                        setDragOverTeam(0);
                        setDraggingMember("");
                        setDraggingFrom(0);
                        if (memberID) releaseToDraft(memberID);
                    }}
                    style={
                        dragOverDraft && draggingMember
                            ? {
                                outline: "2px dashed #4dabf7",
                                outlineOffset: 4,
                                borderRadius: 10,
                                backgroundColor: "rgba(77,171,247,0.07)",
                            }
                            : { outline: "2px dashed transparent", outlineOffset: 4, borderRadius: 10 }
                    }
                >
            {rows.length > 0 && (
                <div
                    className="p-3 rounded mb-3"
                    style={{ backgroundColor: "#141414", border: "1px solid #333" }}
                >
                    <h6 className="text-info mb-2">🏗️ Group selected players into a new team</h6>
                    <div className="d-flex flex-column flex-md-row gap-2 align-items-md-center">
                        <input
                            className="form-control bg-dark text-light"
                            style={{ maxWidth: 240 }}
                            placeholder="Team name (blank = random)"
                            value={groupName}
                            onChange={(e) => setGroupName(e.target.value)}
                        />
                        <button
                            className="btn btn-outline-light btn-sm"
                            onClick={suggestName}
                            title="Generate a random adjective + noun name"
                        >
                            🎲
                        </button>
                        <select
                            className="form-select bg-dark text-light"
                            style={{ maxWidth: 220 }}
                            value={captain}
                            onChange={(e) => setCaptain(e.target.value)}
                        >
                            <option value="">No captain (assign later)</option>
                            {rows
                                .filter((r) => selected[r.id])
                                .map((r) => (
                                    <option key={r.player_id_str} value={r.player_id_str}>
                                        {r.display_name || r.username}
                                    </option>
                                ))}
                        </select>
                        <label
                            className="text-secondary small d-flex align-items-center gap-1"
                            title="Bypass the rookie veteran ratio rule for this team"
                        >
                            <input
                                type="checkbox"
                                className="form-check-input mt-0"
                                checked={groupForce}
                                onChange={(e) => setGroupForce(e.target.checked)}
                            />
                            Force
                        </label>
                        <button
                            className="btn btn-info btn-sm"
                            onClick={handleCreateGroup}
                            disabled={busyId !== 0}
                        >
                            {busyId === -1
                                ? "Creating…"
                                : `Create team (${rows.filter((r) => selected[r.id]).length})`}
                        </button>
                    </div>
                </div>
            )}

            <div className="d-flex flex-column gap-3">
                {draftOrdered.map((row, rowIdx) => (
                    <Fragment key={row.id}>
                    {isCoach(row) && !isCoach(draftOrdered[rowIdx - 1] || {}) && (
                        <div className="rdp-coach-row">
                            🧑‍🏫 Coach signups ({coachRows.length}) — assign to the team they'll coach
                        </div>
                    )}
                    <div
                        className={`p-3 rounded rdp-card${isCoach(row) ? " rdp-card--coach" : ""}`}
                        draggable={busyId === 0}
                        onDragStart={(e) => {
                            e.dataTransfer.setData("text/plain", String(row.id));
                            e.dataTransfer.effectAllowed = "move";
                            setDraggingId(row.id);
                        }}
                        onDragEnd={() => {
                            setDraggingId(0);
                            setDragOverTeam(0);
                        }}
                        style={{
                            cursor: "grab",
                            opacity: draggingId === row.id ? 0.5 : 1,
                        }}
                    >
                        <div className="d-flex flex-wrap align-items-center gap-2 mb-2">
                            <PlayerIdentity
                                player={{
                                    id: row.player_id_str,
                                    display_name: row.display_name,
                                    username: row.username,
                                    avatar: row.avatar,
                                }}
                                linkTo={leagueLink(`/players/${row.player_id_str}`)}
                            />
                            <span className="badge bg-secondary">{row.role || "Player"}</span>
                            {row.kind === "coach" && (
                                <span className="badge bg-info text-dark">🧑‍🏫 Coach</span>
                            )}
                            <span className="badge bg-dark border border-secondary">
                                {row.device || "device n/a"}
                            </span>
                            <span className="badge bg-dark border border-secondary">
                                {row.timezone || "tz n/a"}
                            </span>
                            {row.is_veteran ? (
                                <span className="badge bg-warning text-dark">Veteran (Varsity)</span>
                            ) : (
                                <span className="badge bg-success">New player</span>
                            )}
                            {row.varsity_matches > 0 && (
                                <span className="text-secondary small">
                                    Varsity: {row.varsity_rating} rating • {row.varsity_matches} games
                                </span>
                            )}
                        </div>

                        {row.note && (
                            <p className="text-secondary small mb-2">“{row.note}”</p>
                        )}

                        <div className="d-flex flex-wrap gap-2 align-items-center">
                            <label className="text-secondary small d-flex align-items-center gap-1 me-2">
                                <input
                                    type="checkbox"
                                    className="form-check-input mt-0"
                                    checked={!!selected[row.id]}
                                    onChange={(e) =>
                                        setSelected((p) => ({ ...p, [row.id]: e.target.checked }))
                                    }
                                />
                                Select
                            </label>
                            {busyId === row.id && (
                                <span className="text-info small">Working…</span>
                            )}
                        </div>
                    </div>
                    </Fragment>
                ))}
            </div>
                </div>

                <div className="col-12 col-lg-5">
                    <div
                        className="p-2 rounded h-100"
                        style={{ backgroundColor: "#141414", border: "1px solid #333" }}
                    >
                        <h6 className="text-info mb-2">🧩 Team rosters ({rosters.length})</h6>

                        {rosters.length === 0 ? (
                            <p className="text-secondary small mb-0">No teams in this league yet.</p>
                        ) : (
                            <div className="d-flex flex-column gap-2">
                                {rosters.map((t) => (
                                    <div
                                        key={t.id}
                                        className="p-2 rounded"
                                        onDragOver={(e) => {
                                            if (!draggingId && !draggingMember) return;
                                            e.preventDefault();
                                            e.dataTransfer.dropEffect = "move";
                                        }}
                                        onDragEnter={() => {
                                            if (draggingId || draggingMember) setDragOverTeam(t.id);
                                        }}
                                        onDragLeave={(e) =>
                                            !e.currentTarget.contains(e.relatedTarget) &&
                                            setDragOverTeam((cur) => (cur === t.id ? 0 : cur))
                                        }
                                        onDrop={(e) => {
                                            e.preventDefault();
                                            const memberID = e.dataTransfer.getData(
                                                "application/x-ecgl-member"
                                            );
                                            const plain = e.dataTransfer.getData("text/plain") || "";
                                            const fromTeam = draggingFrom;
                                            setDragOverTeam(0);
                                            setDraggingId(0);
                                            setDraggingMember("");
                                            setDraggingFrom(0);

                                            // Dropping a member back on their own team is a no-op.
                                            if (fromTeam && fromTeam === t.id) return;

                                            if (memberID) {
                                                transferPlayer(memberID, t.id);
                                                return;
                                            }
                                            if (plain.startsWith("member:")) {
                                                transferPlayer(plain.slice(7), t.id);
                                                return;
                                            }

                                            const id = Number(plain) || draggingId;
                                            const row = rows.find((r) => r.id === id);
                                            if (row) assignToTeam(row, t.id);
                                        }}
                                        style={{
                                            backgroundColor:
                                                dragOverTeam === t.id ? "#173042" : "#1a1a1a",
                                            border:
                                                dragOverTeam === t.id
                                                    ? "1px solid #4dabf7"
                                                    : "1px solid #333",
                                            boxShadow:
                                                dragOverTeam === t.id
                                                    ? "0 0 0 2px rgba(77,171,247,0.25)"
                                                    : "none",
                                            transition: "background 120ms, border-color 120ms",
                                        }}
                                    >
                                        <div className="d-flex justify-content-between align-items-center mb-1">
                                            <strong className="small">{t.name}</strong>
                                            <span
                                                className={`badge ${t.members.length >= (t.max_slots || maxSlots)
                                                        ? "bg-danger"
                                                        : "bg-secondary"
                                                    }`}
                                                title="Players in team / max slots"
                                            >
                                                {t.members.length}/{t.max_slots || maxSlots || "?"}
                                            </span>
                                        </div>

                                        <div className="text-secondary" style={{ fontSize: "0.72rem" }}>
                                            {t.status || "Active"} • {t.rating} rating • {t.veterans} vet /{" "}
                                            {t.rookies} new
                                        </div>

                                        {t.members.length === 0 ? (
                                            <div className="text-secondary small mt-1">No players</div>
                                        ) : (
                                            <ul className="list-unstyled mb-0 mt-1">
                                                {t.members.map((m) => (
                                                    <li
                                                        key={m.id}
                                                        className="d-flex align-items-center gap-1 flex-wrap"
                                                        draggable={busyId === 0}
                                                        onDragStart={(e) => {
                                                            e.dataTransfer.setData(
                                                                "application/x-ecgl-member",
                                                                m.id
                                                            );
                                                            e.dataTransfer.setData(
                                                                "text/plain",
                                                                "member:" + m.id
                                                            );
                                                            e.dataTransfer.effectAllowed = "move";
                                                            setDraggingMember(m.id);
                                                            setDraggingFrom(t.id);
                                                        }}
                                                        onDragEnd={() => {
                                                            setDraggingMember("");
                                                            setDraggingFrom(0);
                                                            setDragOverTeam(0);
                                                            setDragOverDraft(false);
                                                        }}
                                                        style={{
                                                            fontSize: "0.8rem",
                                                            cursor: "grab",
                                                            opacity: draggingMember === m.id ? 0.45 : 1,
                                                        }}
                                                        title="Drag onto another team to move this player"
                                                    >
                                                        <span className="text-light text-truncate">
                                                            {m.display_name || m.username}
                                                        </span>
                                                        {m.role && (
                                                            <span className="badge bg-dark border border-secondary">
                                                                {m.role}
                                                            </span>
                                                        )}
                                                        {m.is_veteran ? (
                                                            <span className="badge bg-warning text-dark">Vet</span>
                                                        ) : (
                                                            <span className="badge bg-success">New</span>
                                                        )}
                                                    </li>
                                                ))}
                                            </ul>
                                        )}
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                </div>
            </div>

            <style>{`
            .rdp .rdp-card {
                background: #161616;
                border: 1px solid #343434;
                border-radius: 10px;
                transition: border-color .15s ease, box-shadow .15s ease, transform .15s ease;
            }
            .rdp .rdp-card:hover {
                border-color: #ff8c42;
                box-shadow: 0 4px 16px rgba(255,140,66,0.16);
            }
            .rdp .rdp-card--coach {
                background: #10262b;
                border-color: #0dcaf0;
            }
            .rdp .rdp-card--coach:hover {
                box-shadow: 0 4px 16px rgba(13,202,240,0.18);
            }
            .rdp .rdp-note {
                background: #131313;
                border: 1px dashed #3a3a3a;
                border-radius: 8px;
                padding: 8px 10px;
            }
            .rdp .rdp-coach-row {
                display: flex;
                align-items: center;
                gap: 8px;
                margin: 6px 0 0;
                padding-top: 10px;
                border-top: 1px solid #2f3a3d;
                color: #67e8f9;
                font-size: 0.78rem;
                font-weight: 700;
                letter-spacing: 0.3px;
                text-transform: uppercase;
            }
            `}</style>
        </div>
    );
}
