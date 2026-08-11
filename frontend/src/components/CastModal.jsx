import { useEffect, useState } from "react";
import axios from "axios";

export default function CastModal({
    show,
    onClose,
    matchID,
    existingCast,
    urlBase,
    onSaved,
    me
}) {
    const [players, setPlayers] = useState([]);
    const [casterSlots, setCasterSlots] = useState(["", "", ""]); // 3 caster slots
    const [cameraSlot, setCameraSlot] = useState("");
    const [streamURL, setStreamURL] = useState("");
    const [streamError, setStreamError] = useState("");
    const [assignOpen, setAssignOpen] = useState(null); // "caster_0", "caster_1", "caster_2", "camera"
    const [assignSearch, setAssignSearch] = useState("");

    useEffect(() => {
        if (!show) return;

        axios
            .get(`${urlBase}/api/players`, { withCredentials: true })
            .then((res) => {
                const list = Array.isArray(res.data) ? res.data : [];
                setPlayers(list);

                const savedCasters = (existingCast?.casters || []).map(String);
                // Fill up to 3 slots
                const slots = ["", "", ""];
                for (let i = 0; i < Math.min(savedCasters.length, 3); i++) {
                    slots[i] = savedCasters[i];
                }
                setCasterSlots(slots);
                setCameraSlot(existingCast?.camera ? String(existingCast.camera) : "");
                setStreamURL(existingCast?.stream_url || "");
            })
            .catch(() => {
                setPlayers([]);
                setCasterSlots(["", "", ""]);
                setCameraSlot("");
                setStreamURL("");
            });
    }, [show]);

    if (!show) return null;

    const canClaim = me?.is_caster || me?.is_mod;
    const myId = me?.id ? String(me.id) : "";

    function claimSlot(slotKey) {
        if (slotKey === "camera") {
            setCameraSlot(myId);
        } else {
            const idx = parseInt(slotKey.split("_")[1]);
            setCasterSlots((prev) => {
                const next = [...prev];
                next[idx] = myId;
                return next;
            });
        }
    }

    function unclaimSlot(slotKey) {
        if (slotKey === "camera") {
            setCameraSlot("");
        } else {
            const idx = parseInt(slotKey.split("_")[1]);
            setCasterSlots((prev) => {
                const next = [...prev];
                next[idx] = "";
                return next;
            });
        }
    }

    function assignPlayer(slotKey, playerId) {
        if (slotKey === "camera") {
            setCameraSlot(String(playerId));
        } else {
            const idx = parseInt(slotKey.split("_")[1]);
            setCasterSlots((prev) => {
                const next = [...prev];
                next[idx] = String(playerId);
                return next;
            });
        }
        setAssignOpen(null);
        setAssignSearch("");
    }

    function getPlayerName(id) {
        if (!id) return null;
        const p = players.find((x) => String(x.id) === String(id));
        return p ? (p.display_name || p.username) : "Unknown";
    }

    function isValidYouTube(url) {
        if (!url) return true;
        return /^https?:\/\/(www\.)?(youtube\.com|youtu\.be)\/.+/.test(url);
    }

    async function saveCast() {
        const filledCasters = casterSlots.filter(Boolean);
        if (filledCasters.length === 0) {
            alert("Pick at least one caster.");
            return;
        }
        if (!cameraSlot) {
            alert("Select a camera operator.");
            return;
        }
        if (!isValidYouTube(streamURL)) {
            setStreamError("Invalid YouTube URL.");
            return;
        }

        try {
            const isEditing = !!(existingCast?.casters?.length || existingCast?.camera || existingCast?.stream_url);

            if (!isEditing) {
                await axios.post(
                    `${urlBase}/api/match/cast/request`,
                    { match_id: Number(matchID) },
                    { withCredentials: true }
                );
            }

            await axios.post(
                `${urlBase}/api/match/cast`,
                {
                    match_id: Number(matchID),
                    casters: filledCasters.map(String),
                    camera_id: cameraSlot.toString(),
                    stream_url: streamURL.trim(),
                },
                { withCredentials: true }
            );

            onSaved?.();
            onClose();
        } catch (err) {
            console.error("Failed to save cast:", err);
            alert(err.response?.data || "Failed to save cast.");
        }
    }

    async function deleteCast() {
        if (!confirm("Remove this cast assignment?")) return;
        try {
            await axios.post(`${urlBase}/api/match/cast/delete`, { match_id: Number(matchID) }, { withCredentials: true });
            if (onSaved) onSaved();
            onClose();
        } catch (err) {
            console.error("Failed to remove cast:", err);
            alert("Failed to remove cast.");
        }
    }

    const filteredAssign = players.filter((p) => {
        if (!assignSearch) return true;
        const name = (p.display_name || p.username || "").toLowerCase();
        return name.includes(assignSearch.toLowerCase());
    });

    function renderSlot(label, slotKey, currentValue) {
        const playerName = getPlayerName(currentValue);
        const isMine = currentValue && String(currentValue) === myId;

        return (
            <div className="cast-slot-row">
                <span className="cast-slot-label">{label}</span>
                <div className="cast-slot-value">
                    {playerName ? (
                        <>
                            <span className="cast-slot-name">{playerName}</span>
                            {(isMine || me?.is_mod) && (
                                <button className="btn btn-outline-danger btn-sm py-0 px-2" onClick={() => unclaimSlot(slotKey)}>
                                    ✕
                                </button>
                            )}
                        </>
                    ) : (
                        <span className="text-muted small">Empty</span>
                    )}
                </div>
                <div className="cast-slot-actions">
                    {!playerName && canClaim && (
                        <button className="btn btn-outline-success btn-sm py-0 px-2" onClick={() => claimSlot(slotKey)}>
                            Claim
                        </button>
                    )}
                    {(me?.is_mod || me?.is_caster) && (
                        <div style={{ position: "relative" }}>
                            <button
                                className="btn btn-outline-secondary btn-sm py-0 px-2"
                                onClick={() => setAssignOpen(assignOpen === slotKey ? null : slotKey)}
                            >
                                Assign
                            </button>
                            {assignOpen === slotKey && (
                                <div className="cast-assign-dropdown">
                                    <input
                                        type="text"
                                        className="form-control form-control-sm bg-dark text-light mb-1"
                                        placeholder="Search..."
                                        value={assignSearch}
                                        onChange={(e) => setAssignSearch(e.target.value)}
                                        autoFocus
                                    />
                                    <div className="cast-assign-list">
                                        {filteredAssign.slice(0, 20).map((p) => (
                                            <button
                                                key={p.id}
                                                type="button"
                                                className="cast-assign-item"
                                                onClick={() => assignPlayer(slotKey, p.id)}
                                            >
                                                {p.display_name || p.username}
                                            </button>
                                        ))}
                                        {filteredAssign.length === 0 && (
                                            <div className="text-muted small p-1">No matches</div>
                                        )}
                                    </div>
                                </div>
                            )}
                        </div>
                    )}
                </div>
            </div>
        );
    }

    return (
        <div className="cast-overlay" onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
            <div className="cast-window card bg-dark border-secondary shadow-lg" onClick={(e) => e.stopPropagation()}>

                <div className="card-header d-flex justify-content-between align-items-center">
                    <h5 className="mb-0 text-info">
                        🎥 {existingCast ? "Edit Match Cast" : "Schedule Match Cast"}
                    </h5>
                    <button className="btn btn-sm btn-outline-light" onClick={onClose}>✕</button>
                </div>

                <div className="card-body">
                    {/* Caster slots */}
                    <div className="mb-3">
                        <label className="fw-bold mb-2">🎙 Casters</label>
                        {renderSlot("Caster 1", "caster_0", casterSlots[0])}
                        {renderSlot("Caster 2", "caster_1", casterSlots[1])}
                        {renderSlot("Caster 3", "caster_2", casterSlots[2])}
                    </div>

                    {/* Camera slot */}
                    <div className="mb-3">
                        <label className="fw-bold mb-2">🎮 Camera Operator</label>
                        {renderSlot("Cam Op", "camera", cameraSlot)}
                    </div>

                    {/* Stream URL */}
                    <div className="mb-3">
                        <label className="fw-bold mb-1">📺 YouTube Stream URL (optional)</label>
                        <input
                            type="text"
                            className="form-control form-control-sm bg-dark text-light"
                            placeholder="https://youtube.com/live/…"
                            value={streamURL}
                            onChange={(e) => { setStreamURL(e.target.value); setStreamError(""); }}
                        />
                        {streamError && <div className="text-danger small mt-1">{streamError}</div>}
                    </div>
                </div>

                <div className="card-footer d-flex justify-content-between align-items-center">
                    {existingCast ? (
                        <button className="btn btn-outline-danger btn-sm" onClick={deleteCast}>🗑 Remove Cast</button>
                    ) : <span />}
                    <div className="d-flex gap-2">
                        <button className="btn btn-success btn-sm" onClick={saveCast}>💾 Save</button>
                        <button className="btn btn-secondary btn-sm" onClick={onClose}>Cancel</button>
                    </div>
                </div>
            </div>

            <style>{`
            .cast-overlay {
                position: fixed; inset: 0; background: rgba(0,0,0,0.6);
                backdrop-filter: blur(4px); z-index: 5000;
                display: flex; align-items: center; justify-content: center;
            }
            .cast-window { width: 460px; max-width: 95%; border-radius: 12px; }
            .cast-slot-row {
                display: flex; align-items: center; gap: 8px;
                padding: 6px 8px; margin-bottom: 4px;
                background: #151515; border: 1px solid #333; border-radius: 8px;
            }
            .cast-slot-label { font-size: 0.78rem; color: #888; min-width: 55px; font-weight: 600; }
            .cast-slot-value { flex: 1; display: flex; align-items: center; gap: 6px; }
            .cast-slot-name { font-weight: 600; font-size: 0.85rem; }
            .cast-slot-actions { display: flex; gap: 4px; }
            .cast-assign-dropdown {
                position: absolute; top: 100%; right: 0; z-index: 10;
                background: #1a1a1a; border: 1px solid #444; border-radius: 8px;
                padding: 6px; width: 200px; margin-top: 4px;
            }
            .cast-assign-list { max-height: 160px; overflow-y: auto; }
            .cast-assign-item {
                display: block; width: 100%; text-align: left;
                background: none; border: none; color: #ddd;
                padding: 4px 8px; border-radius: 4px; font-size: 0.8rem;
                cursor: pointer;
            }
            .cast-assign-item:hover { background: rgba(255,255,255,0.06); }
            `}</style>
        </div>
    );
}
