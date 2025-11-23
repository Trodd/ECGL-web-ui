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

    async function saveCast() {
        if (casters.length === 0) {
            alert("Pick at least one caster.");
            return;
        }
        if (!camera) {
            alert("Select a camera operator.");
            return;
        }

        try {
            // Step 1 — Create Discord cast channel
            await axios.post(
                `${urlBase}/api/match/cast/request`,
                { match_id: Number(matchID) },
                { withCredentials: true }
            );

            // Step 2 — Save to DB
            await axios.post(
                `${urlBase}/api/match/cast`,
                {
                    match_id: Number(matchID),
                    casters: casters.map(String),
                    camera_id: camera.toString(),
                },
                { withCredentials: true }
            );

            alert("🎥 Cast saved!");
            if (onSaved) onSaved();
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
            <div className="cast-window bg-dark text-light p-3 rounded shadow">
                <h4 className="text-info mb-3">
                    {existingCast ? "🎥 Edit Cast" : "🎥 Schedule Cast"}
                </h4>

                {/* === CASTER SEARCH + MULTI SELECT === */}
                <label className="fw-bold mb-1">Casters</label>
                <input
                    type="text"
                    className="form-control form-control-sm bg-dark text-light mb-2"
                    placeholder="Search casters..."
                    value={casterSearch}
                    onChange={(e) => setCasterSearch(e.target.value)}
                />

                <div className="mb-3" style={{ maxHeight: 170, overflowY: "auto" }}>
                    {filteredCasters.length === 0 ? (
                        <p className="text-warning small">No matching players found.</p>
                    ) : (
                        filteredCasters.map((p) => (
                            <div key={p.id} className="form-check text-light">
                                <input
                                    type="checkbox"
                                    className="form-check-input"
                                    checked={casters.includes(String(p.id))}
                                    onChange={() => toggleCaster(p.id)}
                                />
                                <label className="form-check-label">
                                    {p.display_name || p.username}
                                </label>
                            </div>
                        ))
                    )}
                </div>

                {/* === CAMERA OPERATOR SEARCH + SELECT === */}
                <label className="fw-bold">Camera Operator</label>
                <input
                    type="text"
                    className="form-control form-control-sm bg-dark text-light mb-2"
                    placeholder="Search camera operator..."
                    value={cameraSearch}
                    onChange={(e) => setCameraSearch(e.target.value)}
                />

                <select
                    className="form-select bg-dark text-light mb-3"
                    value={camera}
                    onChange={(e) => setCamera(e.target.value)}
                >
                    <option value="">Select camera operator…</option>
                    {filteredCameraPlayers.map((p) => (
                        <option key={p.id} value={String(p.id)}>
                            {p.display_name || p.username}
                        </option>
                    ))}
                </select>

                {/* === BUTTONS === */}
                <div className="d-flex justify-content-between gap-2 mt-3">
                    {existingCast && (
                        <button className="btn btn-danger" onClick={deleteCast}>
                            🗑 Remove
                        </button>
                    )}

                    <div className="d-flex gap-2">
                        <button className="btn btn-success" onClick={saveCast}>
                            Save
                        </button>
                        <button className="btn btn-secondary" onClick={onClose}>
                            Cancel
                        </button>
                    </div>
                </div>
            </div>

            <style>{`
                .cast-overlay {
                    position: fixed;
                    inset: 0;
                    background: rgba(0,0,0,0.55);
                    z-index: 5000;
                    display: flex;
                    align-items: center;
                    justify-content: center;
                }
                .cast-window {
                    width: 400px;
                    max-width: 95%;
                    border: 1px solid #444;
                }
            `}</style>
        </div>
    );
}
