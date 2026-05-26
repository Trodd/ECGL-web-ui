import { useState, useEffect } from "react";
import { getApiUrl } from "../config";

export default function DevToolbar({ impersonateId, setImpersonateId }) {
    const [players, setPlayers] = useState([]);
    const [search, setSearch] = useState("");
    const [expanded, setExpanded] = useState(false);

    useEffect(() => {
        fetch(`${getApiUrl()}/api/players`, { credentials: "include" })
            .then((res) => res.json())
            .then((data) => setPlayers(Array.isArray(data) ? data : []))
            .catch(() => setPlayers([]));
    }, []);

    const filtered = search.trim()
        ? players.filter(
            (p) =>
                (p.display_name || p.username || "")
                    .toLowerCase()
                    .includes(search.toLowerCase()) ||
                String(p.id).includes(search)
        )
        : players;

    return (
        <div
            style={{
                position: "fixed",
                bottom: 12,
                right: 12,
                zIndex: 99999,
                background: "rgba(30, 30, 30, 0.95)",
                border: "2px solid #f0ad4e",
                borderRadius: 10,
                padding: expanded ? "12px 16px" : "8px 14px",
                color: "#fff",
                fontSize: 13,
                maxWidth: expanded ? 320 : "auto",
                boxShadow: "0 4px 20px rgba(0,0,0,0.5)",
            }}
        >
            <div
                style={{ cursor: "pointer", fontWeight: 700, color: "#f0ad4e" }}
                onClick={() => setExpanded(!expanded)}
            >
                🛠️ DEV {impersonateId && `(as ${impersonateId})`}
            </div>

            {impersonateId && (
                <button
                    className="btn btn-sm btn-warning mt-1 w-100"
                    onClick={() => {
                        setImpersonateId(null);
                        window.location.reload();
                    }}
                >
                    ✕ Stop Impersonating
                </button>
            )}

            {expanded && (
                <div style={{ marginTop: 8 }}>
                    <input
                        type="text"
                        className="form-control form-control-sm mb-2"
                        placeholder="Search player or paste Discord ID..."
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        style={{ background: "#222", color: "#fff", border: "1px solid #555" }}
                    />

                    <div style={{ maxHeight: 200, overflowY: "auto" }}>
                        {filtered.slice(0, 30).map((p) => (
                            <div
                                key={p.id}
                                style={{
                                    padding: "4px 8px",
                                    cursor: "pointer",
                                    borderRadius: 4,
                                    background:
                                        String(p.id) === impersonateId ? "#f0ad4e33" : "transparent",
                                }}
                                onClick={() => {
                                    setImpersonateId(String(p.id));
                                    window.location.reload();
                                }}
                            >
                                <strong>{p.display_name || p.username}</strong>{" "}
                                <span style={{ opacity: 0.6 }}>({p.id})</span>
                            </div>
                        ))}
                    </div>
                </div>
            )}
        </div>
    );
}
