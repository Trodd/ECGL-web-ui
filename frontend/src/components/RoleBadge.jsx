/**
 * RoleBadge — one consistent rendering of a team_members role.
 * Coaches get their own colour + icon so they're clearly staff, not players.
 */
export default function RoleBadge({ role, className = "" }) {
  const label = String(role || "").trim();
  const lower = label.toLowerCase();

  if (!label) {
    return <span className={`badge bg-secondary ${className}`}>-</span>;
  }

  if (lower === "coach") {
    return (
      <span
        className={`badge ${className}`}
        style={{
          background: "#0b3540",
          color: "#67e8f9",
          border: "1px solid #0dcaf0",
          fontWeight: 700,
        }}
        title="Coach — does not play"
      >
        🧑‍🏫 Coach
      </span>
    );
  }

  if (lower === "captain") {
    return <span className={`badge bg-warning text-dark ${className}`}>👑 Captain</span>;
  }

  if (lower === "co-captain") {
    return <span className={`badge bg-info text-dark ${className}`}>🛡️ Co-Captain</span>;
  }

  return <span className={`badge bg-secondary ${className}`}>{label}</span>;
}
