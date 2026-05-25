import { useEffect, useRef, useState } from "react";
import axios from "axios";
import { useNavigate } from "react-router-dom";
import { getApiUrl } from "../config";

export default function NotificationBell() {
    const [open, setOpen] = useState(false);
    const [notifications, setNotifications] = useState([]);
    const [unreadCount, setUnreadCount] = useState(0);
    const dropdownRef = useRef(null);
    const navigate = useNavigate();
    const API = getApiUrl();

    // Poll for unread count every 30s
    useEffect(() => {
        const fetchCount = () => {
            axios
                .get(`${API}/api/notifications/count`, { withCredentials: true })
                .then((res) => setUnreadCount(res.data?.unread_count || 0))
                .catch(() => { });
        };
        fetchCount();
        const interval = setInterval(fetchCount, 30000);
        return () => clearInterval(interval);
    }, [API]);

    // Fetch full list when dropdown opens
    useEffect(() => {
        if (open) {
            axios
                .get(`${API}/api/notifications`, { withCredentials: true })
                .then((res) => {
                    setNotifications(res.data?.notifications || []);
                    setUnreadCount(res.data?.unread_count || 0);
                })
                .catch(() => { });
        }
    }, [open, API]);

    // Close on outside click
    useEffect(() => {
        const handler = (e) => {
            if (dropdownRef.current && !dropdownRef.current.contains(e.target)) {
                setOpen(false);
            }
        };
        document.addEventListener("mousedown", handler);
        return () => document.removeEventListener("mousedown", handler);
    }, []);

    const markRead = (id) => {
        axios.post(`${API}/api/notifications/read`, { id }, { withCredentials: true }).catch(() => { });
        setNotifications((prev) => prev.map((n) => (n.id === id ? { ...n, read: true } : n)));
        setUnreadCount((c) => Math.max(0, c - 1));
    };

    const markAllRead = () => {
        axios.post(`${API}/api/notifications/read-all`, {}, { withCredentials: true }).catch(() => { });
        setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
        setUnreadCount(0);
    };

    const handleClick = (notif) => {
        if (!notif.read) markRead(notif.id);
        if (notif.link) {
            navigate(notif.link);
            setOpen(false);
        }
    };

    const getIcon = (type) => {
        switch (type) {
            case "join_request": return "👤";
            case "matchup_posted": return "📅";
            case "schedule_proposed": return "🕐";
            case "score_submitted": return "📝";
            case "challenge_received": return "⚔️";
            case "match_added": return "➕";
            case "match_deleted": return "🗑️";
            case "mod_scores_set": return "🛠️";
            default: return "🔔";
        }
    };

    const timeAgo = (dateStr) => {
        const diff = Date.now() - new Date(dateStr).getTime();
        const mins = Math.floor(diff / 60000);
        if (mins < 1) return "just now";
        if (mins < 60) return `${mins}m ago`;
        const hrs = Math.floor(mins / 60);
        if (hrs < 24) return `${hrs}h ago`;
        const days = Math.floor(hrs / 24);
        return `${days}d ago`;
    };

    return (
        <div ref={dropdownRef} style={{ position: "relative", display: "inline-block" }}>
            {/* Bell button */}
            <button
                onClick={() => setOpen(!open)}
                style={{
                    background: "transparent",
                    border: "none",
                    cursor: "pointer",
                    position: "relative",
                    padding: "6px 8px",
                    fontSize: 20,
                    lineHeight: 1,
                    color: "#e8ecf0",
                }}
                aria-label="Notifications"
            >
                🔔
                {unreadCount > 0 && (
                    <span
                        style={{
                            position: "absolute",
                            top: 2,
                            right: 2,
                            background: "#ef4444",
                            color: "#fff",
                            borderRadius: "50%",
                            width: 18,
                            height: 18,
                            fontSize: 11,
                            fontWeight: 800,
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                            border: "2px solid var(--bg-base, #080a0f)",
                        }}
                    >
                        {unreadCount > 9 ? "9+" : unreadCount}
                    </span>
                )}
            </button>

            {/* Dropdown */}
            {open && (
                <div
                    style={{
                        position: "absolute",
                        top: "calc(100% + 8px)",
                        right: 0,
                        width: 340,
                        maxHeight: 420,
                        overflowY: "auto",
                        background: "rgba(15, 20, 30, 0.95)",
                        backdropFilter: "blur(12px)",
                        WebkitBackdropFilter: "blur(12px)",
                        border: "1px solid rgba(255,255,255,0.12)",
                        borderRadius: 12,
                        boxShadow: "0 12px 40px rgba(0,0,0,0.5)",
                        zIndex: 9999,
                    }}
                >
                    {/* Header */}
                    <div
                        style={{
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "space-between",
                            padding: "12px 14px",
                            borderBottom: "1px solid rgba(255,255,255,0.08)",
                        }}
                    >
                        <span style={{ fontWeight: 800, fontSize: 14, color: "#e8ecf0" }}>
                            Notifications
                        </span>
                        {unreadCount > 0 && (
                            <button
                                onClick={markAllRead}
                                style={{
                                    background: "none",
                                    border: "none",
                                    color: "#6fa8dc",
                                    fontSize: 12,
                                    fontWeight: 700,
                                    cursor: "pointer",
                                }}
                            >
                                Mark all read
                            </button>
                        )}
                    </div>

                    {/* List */}
                    {notifications.length === 0 ? (
                        <div style={{ padding: "24px 14px", textAlign: "center", color: "#5c6b7a", fontSize: 13 }}>
                            No notifications yet
                        </div>
                    ) : (
                        notifications.map((n) => (
                            <div
                                key={n.id}
                                onClick={() => handleClick(n)}
                                style={{
                                    display: "flex",
                                    alignItems: "flex-start",
                                    gap: 10,
                                    padding: "10px 14px",
                                    cursor: n.link ? "pointer" : "default",
                                    background: n.read ? "transparent" : "rgba(59, 110, 165, 0.08)",
                                    borderBottom: "1px solid rgba(255,255,255,0.05)",
                                    transition: "background 0.15s",
                                }}
                                onMouseEnter={(e) => (e.currentTarget.style.background = "rgba(255,255,255,0.05)")}
                                onMouseLeave={(e) =>
                                (e.currentTarget.style.background = n.read
                                    ? "transparent"
                                    : "rgba(59, 110, 165, 0.08)")
                                }
                            >
                                <span style={{ fontSize: 18, flexShrink: 0, marginTop: 2 }}>
                                    {getIcon(n.type)}
                                </span>
                                <div style={{ flex: 1, minWidth: 0 }}>
                                    <div
                                        style={{
                                            fontWeight: n.read ? 600 : 800,
                                            fontSize: 13,
                                            color: n.read ? "#8b99a8" : "#e8ecf0",
                                            whiteSpace: "nowrap",
                                            overflow: "hidden",
                                            textOverflow: "ellipsis",
                                        }}
                                    >
                                        {n.title}
                                    </div>
                                    <div
                                        style={{
                                            fontSize: 12,
                                            color: "#5c6b7a",
                                            marginTop: 2,
                                            whiteSpace: "nowrap",
                                            overflow: "hidden",
                                            textOverflow: "ellipsis",
                                        }}
                                    >
                                        {n.message}
                                    </div>
                                    <div style={{ fontSize: 11, color: "#3d4f5f", marginTop: 3 }}>
                                        {timeAgo(n.created_at)}
                                    </div>
                                </div>
                                {!n.read && (
                                    <span
                                        style={{
                                            width: 8,
                                            height: 8,
                                            borderRadius: "50%",
                                            background: "#3b82f6",
                                            flexShrink: 0,
                                            marginTop: 8,
                                        }}
                                    />
                                )}
                            </div>
                        ))
                    )}
                </div>
            )}
        </div>
    );
}
