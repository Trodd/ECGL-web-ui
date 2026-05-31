import { Link } from "react-router-dom";

/**
 * Compute Discord avatar URL from player id + avatar hash.
 */
export function getDiscordAvatarUrl(player) {
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
}

/**
 * Renders a player row with avatar, display name (as link), and @username in mono.
 * Props:
 *   player - { id, display_name, username, avatar }
 *   linkTo - optional custom link path (defaults to /players/{id})
 *   size   - avatar size in px (default 28)
 */
export default function PlayerIdentity({ player, linkTo, size = 28 }) {
    if (!player) return null;

    const name = player.display_name || player.username || "Unknown";
    const username = player.username || "unknown";
    const path = linkTo || (player.id ? `/players/${player.id}` : null);

    return (
        <div className="d-flex align-items-center gap-2" style={{ minWidth: 0 }}>
            <img
                src={getDiscordAvatarUrl(player)}
                alt=""
                style={{
                    width: size,
                    height: size,
                    borderRadius: "50%",
                    objectFit: "cover",
                    border: "1px solid var(--border-default, #1e2a3a)",
                    flexShrink: 0,
                }}
                loading="lazy"
                onError={(e) => {
                    e.currentTarget.onerror = null;
                    e.currentTarget.src = "https://cdn.discordapp.com/embed/avatars/0.png";
                }}
            />
            <div style={{ minWidth: 0 }}>
                {path ? (
                    <Link to={path} className="text-info text-decoration-none fw-semibold" style={{ display: "block" }}>
                        {name}
                    </Link>
                ) : (
                    <span className="fw-semibold text-info">{name}</span>
                )}
                <span className="players-discord-username" style={{ display: "block" }}>
                    @{username}
                </span>
            </div>
        </div>
    );
}
