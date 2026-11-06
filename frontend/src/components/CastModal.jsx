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
    const [casterSlots, setCasterSlots] = useState(["", "", ""]); // preserved, managed on the Caster Board
    const [cameraSlot, setCameraSlot] = useState("");
    const [streamURL, setStreamURL] = useState("");
    const [streamError, setStreamError] = useState("");

    useEffect(() => {
        if (!show) return;

        const savedCasters = (existingCast?.casters || []).map(String);
        const slots = ["", "", ""];
        for (let i = 0; i < Math.min(savedCasters.length, 3); i++) {
            slots[i] = savedCasters[i];
        }
        setCasterSlots(slots);
        setCameraSlot(existingCast?.camera ? String(existingCast.camera) : "");
        setStreamURL(existingCast?.stream_url || "");
    }, [show]);

    if (!show) return null;

    function isValidYouTube(url) {
        if (!url) return true;
        return /^https?:\/\/(www\.)?(youtube\.com|youtu\.be)\/.+/.test(url);
    }

    async function saveCast() {
        const filledCasters = casterSlots.filter(Boolean);
        if (!isValidYouTube(streamURL)) {
            setStreamError("Invalid YouTube URL.");
            return;
        }

        try {
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
                    {/* Stream URL */}
                    <div className="mb-0">
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
            `}</style>
        </div>
    );
}
