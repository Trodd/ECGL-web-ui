import { useEffect, useState } from "react";
import axios from "axios";

export default function CastModal({
    show,
    onClose,
    matchID,
    existingCast,
    urlBase,
    onSaved
}) {
    const [players, setPlayers] = useState([]);
    const [casters, setCasters] = useState([]);
    const [camera, setCamera] = useState("");

    const [casterSearch, setCasterSearch] = useState("");
    const [cameraSearch, setCameraSearch] = useState("");
    const [streamURL, setStreamURL] = useState("");
    const [streamError, setStreamError] = useState("");

    useEffect(() => {
        if (!show) return;

        axios
            .get(`${urlBase}/api/players`, { withCredentials: true })
            .then((res) => {
                const list = Array.isArray(res.data) ? res.data : [];
                setPlayers(list);

                // Normalize existing cast
                const savedCasters = (existingCast?.casters || []).map(String);
                const savedCamera = existingCast?.camera ? String(existingCast.camera) : "";
                const savedStream = existingCast?.stream_url || "";

                setCasters(savedCasters);
                setCamera(savedCamera);
                setStreamURL(savedStream);
            })
            .catch(() => {
                setPlayers([]);
                setCasters([]);
                setCamera("");
                setStreamURL("");
            });
    }, [show]);

    // Load players ONLY when modal is open
    useEffect(() => {
        if (!show) return;

        axios
            .get(`${urlBase}/api/players`, { withCredentials: true })
            .then((res) => {
                const list = Array.isArray(res.data) ? res.data : [];
                setPlayers(list);

                // Normalize existing cast IDs (numbers → strings)
                const savedCasters = (existingCast?.casters || []).map(String);
                const savedCamera = existingCast?.camera ? String(existingCast.camera) : "";

                setCasters(savedCasters);
                setCamera(savedCamera);
            })
            .catch(() => {
                setPlayers([]);
                setCasters([]);
                setCamera("");
            });
    }, [show]);

    if (!show) return null;

    function toggleCaster(id) {
        id = String(id);
        setCasters((prev) =>
            prev.includes(id)
                ? prev.filter((x) => x !== id)
                : [...prev, id]
        );
    }

    function isValidYouTube(url) {
        if (!url) return true; // optional field
        return /^https?:\/\/(www\.)?(youtube\.com|youtu\.be)\/.+/.test(url);
    }

    async function saveCast() {
        if (casters.length === 0) {
            alert("Pick at least one caster.");
            return;
        }
        if (!camera) {
            alert("Select a camera operator.");
            return;
        }

        // YouTube URL validation
        if (!isValidYouTube(streamURL)) {
            setStreamError("Invalid YouTube URL. Example: https://youtube.com/live/xxxx");
            return;
        }

        try {
            // Detect whether this is an edit (not a new cast)
            const isEditing =
                !!(existingCast?.casters?.length ||
                    existingCast?.camera ||
                    existingCast?.stream_url);

            // Step 1 — Only create Discord channel if this is NOT an edit
            if (!isEditing) {
                await axios.post(
                    `${urlBase}/api/match/cast/request`,
                    { match_id: Number(matchID) },
                    { withCredentials: true }
                );
            }

            // Step 2 — Save cast + stream URL to DB
            await axios.post(
                `${urlBase}/api/match/cast`,
                {
                    match_id: Number(matchID),
                    casters: casters.map(String),
                    camera_id: camera.toString(),
                    stream_url: streamURL.trim(),
                },
                { withCredentials: true }
            );

            alert("🎥 Cast saved!");
            onSaved?.();
            onClose();

        } catch (err) {
            console.error("❌ Failed to save cast:", err);
            alert(err.response?.data || "Failed to save cast.");
        }
    }

    async function deleteCast() {
        if (!confirm("Remove this cast assignment?")) return;

        try {
            await axios.post(
                `${urlBase}/api/match/cast/delete`,
                { match_id: Number(matchID) },
                { withCredentials: true }
            );

            alert("🗑 Cast removed.");
            if (onSaved) onSaved();
            onClose();
        } catch (err) {
            console.error("❌ Failed to remove cast:", err);
            alert("Failed to remove cast.");
        }
    }

    // CLIENT-SIDE FILTERING
    const filteredCasters = players.filter((p) => {
        const name = (p.display_name || p.username || "").toLowerCase();
        return name.includes(casterSearch.toLowerCase());
    });

    const filteredCameraPlayers = players.filter((p) => {
        const name = (p.display_name || p.username || "").toLowerCase();
        return name.includes(cameraSearch.toLowerCase());
    });

    return (
        <div className="cast-overlay">
            <div className="cast-window card bg-dark border-secondary shadow-lg">

                {/* ================= HEADER ================= */}
                <div className="card-header d-flex justify-content-between align-items-center">
                    <h5 className="mb-0 text-info">
                        🎥 {existingCast ? "Edit Match Cast" : "Schedule Match Cast"}
                    </h5>

                    <button
                        className="btn btn-sm btn-outline-light"
                        onClick={onClose}
                        title="Close"
                    >
                        ✕
                    </button>
                </div>

                <div className="card-body">

                    {/* ================= CASTERS ================= */}
                    <div className="mb-4">
                        <label className="fw-bold mb-1">🎙 Casters</label>

                        <input
                            type="text"
                            className="form-control form-control-sm bg-dark text-light mb-2"
                            placeholder="Search players..."
                            value={casterSearch}
                            onChange={(e) => setCasterSearch(e.target.value)}
                        />

                        <div className="cast-scroll">
                            {filteredCasters.length === 0 ? (
                                <div className="text-warning small text-center py-2">
                                    No matching players
                                </div>
                            ) : (
                                filteredCasters.map((p) => (
                                    <label
                                        key={p.id}
                                        className="d-flex align-items-center gap-2 cast-row"
                                    >
                                        <input
                                            type="checkbox"
                                            className="form-check-input"
                                            checked={casters.includes(String(p.id))}
                                            onChange={() => toggleCaster(p.id)}
                                        />
                                        <span>
                                            {p.display_name || p.username}
                                        </span>
                                    </label>
                                ))
                            )}
                        </div>

                        {casters.length > 0 && (
                            <div className="small text-secondary mt-1">
                                Selected: {casters.length}
                            </div>
                        )}
                    </div>

                    {/* ================= CAMERA ================= */}
                    <div className="mb-4">
                        <label className="fw-bold mb-1">🎮 Camera Operator</label>

                        <input
                            type="text"
                            className="form-control form-control-sm bg-dark text-light mb-2"
                            placeholder="Search camera operator..."
                            value={cameraSearch}
                            onChange={(e) => setCameraSearch(e.target.value)}
                        />

                        <select
                            className="form-select bg-dark text-light"
                            value={camera}
                            onChange={(e) => setCamera(e.target.value)}
                        >
                            <option value="">Select operator…</option>
                            {filteredCameraPlayers.map((p) => (
                                <option key={p.id} value={String(p.id)}>
                                    {p.display_name || p.username}
                                </option>
                            ))}
                        </select>
                    </div>

                    {/* ================= STREAM ================= */}
                    <div className="mb-3">
                        <label className="fw-bold mb-1">📺 YouTube Stream URL (optional)</label>

                        <input
                            type="text"
                            className="form-control form-control-sm bg-dark text-light"
                            placeholder="https://youtube.com/live/…"
                            value={streamURL}
                            onChange={(e) => {
                                setStreamURL(e.target.value);
                                setStreamError("");
                            }}
                        />

                        {streamError && (
                            <div className="text-danger small mt-1">
                                {streamError}
                            </div>
                        )}
                    </div>
                </div>

                {/* ================= FOOTER ================= */}
                <div className="card-footer d-flex justify-content-between align-items-center">

                    {existingCast ? (
                        <button
                            className="btn btn-outline-danger btn-sm"
                            onClick={deleteCast}
                        >
                            🗑 Remove Cast
                        </button>
                    ) : (
                        <span />
                    )}

                    <div className="d-flex gap-2">
                        <button
                            className="btn btn-success btn-sm"
                            onClick={saveCast}
                        >
                            💾 Save
                        </button>

                        <button
                            className="btn btn-secondary btn-sm"
                            onClick={onClose}
                        >
                            Cancel
                        </button>
                    </div>
                </div>
            </div>

            <style>{`
            .cast-overlay {
                position: fixed;
                inset: 0;
                background: rgba(0,0,0,0.6);
                backdrop-filter: blur(4px);
                z-index: 5000;
                display: flex;
                align-items: center;
                justify-content: center;
            }

            .cast-window {
                width: 460px;
                max-width: 95%;
                border-radius: 12px;
            }

            .cast-scroll {
                max-height: 180px;
                overflow-y: auto;
                border: 1px solid #333;
                border-radius: 8px;
                padding: 6px;
                background: #151515;
            }

            .cast-row {
                padding: 6px 8px;
                border-radius: 6px;
                cursor: pointer;
            }

            .cast-row:hover {
                background: rgba(255,255,255,0.06);
            }

            .cast-row input {
                margin-top: 0;
            }
            `}</style>
        </div>
    );
}
