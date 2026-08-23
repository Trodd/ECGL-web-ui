import { useState, useEffect } from "react";
import { getApiUrl } from "../config";

export default function DevToolbar({ user }) {
    const [players, setPlayers] = useState([]);
    const [search, setSearch] = useState("");
    const [expanded, setExpanded] = useState(false);
    const [busy, setBusy] = useState(false);

    useEffect(() => {
        fetch(`${getApiUrl()}/api/players`, { credentials: "include" })
            .then((res) => res.json())
            .then((data) => setPlayers(Array.isArray(data) ? data : []))
            .catch(() => setPlayers([]));
    }, []);

    const impersonating = !!user?.impersonating;
    const realUser = user?.real_user;

    function startImpersonate(id) {
        if (busy) return;
        setBusy(true);
        fetch(`${getApiUrl()}/api/dev/impersonate`, {
            method: "POST",
            credentials: "include",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ discord_id: String(id) }),
        })
            .then((res) => {
                if (!res.ok) throw new Error("impersonate failed");
                window.location.reload();
            })
            .catch(() => {
                setBusy(false);
                alert("❌ Failed to impersonate");
            });
    }

    function stopImpersonating() {
        if (busy) return;
        setBusy(true);
        fetch(`${getApiUrl()}/api/dev/stop-impersonating`, {
            method: "POST",
            credentials: "include",
        })
            .then((res) => {
                if (!res.ok) throw new Error("stop failed");
                window.location.reload();
            })
            .catch(() => {
                setBusy(false);
                alert("❌ Failed to stop impersonating");
            });
    }

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
                border: `2px solid ${impersonating ? "#e74c3c" : "#f0ad4e"}`,
                borderRadius: 10,
                padding: expanded ? "12px 16px" : "8px 14px",
                color: "#fff",
                fontSize: 13,
                maxWidth: expanded ? 320 : "auto",
                boxShadow: "0 4px 20px rgba(0,0,0,0.5)",
            }}
        >
            <div
                style={{ cursor: "pointer", fontWeight: 700, color: impersonating ? "#e74c3c" : "#f0ad4e" }}
                onClick={() => setExpanded(!expanded)}
            >
                🛠️ DEV {impersonating && "— IMPERSONATING"}
            </div>

            {impersonating && (
                <div style={{ marginTop: 6, fontSize: 12, lineHeight: 1.4 }}>
                    <div>
                        Now acting as <strong>{user.display_name || user.username}</strong>{" "}
                        <span style={{ opacity: 0.6 }}>@{user.username}</span>
                    </div>
                    {realUser && (
                        <div style={{ opacity: 0.7 }}>
                            Real login:{" "}
                            <strong>{realUser.display_name || realUser.username}</strong>{" "}
                            <span style={{ opacity: 0.6 }}>@{realUser.username}</span>
                        </div>
                    )}
                    <button
                        className="btn btn-sm btn-warning mt-1 w-100"
                        disabled={busy}
                        onClick={stopImpersonating}
                    >
                        ✕ Stop Impersonating
                    </button>
                </div>
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
                                        String(p.id) === String(user?.id) ? "#f0ad4e33" : "transparent",
                                }}
                                onClick={() => startImpersonate(p.id)}
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
