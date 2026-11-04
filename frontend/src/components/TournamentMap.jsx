import { Link } from "react-router-dom";

/**
 * TournamentMap
 *
 * Visual "map" of matchup matches for bracket / group-stage leagues (anything
 * that isn't a regular weekly season). Matches are laid out in round columns so
 * you can see the shape of the tournament at a glance.
 *
 * Rounds are inferred, in order of preference, from:
 *   bracket_round  →  week number  →  "RN" in the match code  →  1
 */
export default function TournamentMap({
    matches = [],
    formatLabel = "Tournament",
    format = "season",
    teamCount = 0,
    isMod = false,
    onAddMatch = null,
}) {
    function roundOf(m) {
        if (Number(m.bracket_round) > 0) return Number(m.bracket_round);
        const w = Number(m.week);
        if (Number.isFinite(w) && w > 0) return w;
        const mm = String(m.match_code || "").match(/[-\s]R(\d+)/i);
        if (mm) return Number(mm[1]);
        return 1;
    }

    function bracketOf(m) {
        const b = String(m.bracket || "").toLowerCase();
        if (b === "winners" || b === "wb" || b === "upper") return "winners";
        if (b === "losers" || b === "lb" || b === "lower") return "losers";
        if (b === "grand_final" || b === "gf") return "grand_final";
        if (b.startsWith("group")) return b;
        return "";
    }

    const BRACKET_LABELS = {
        winners: "Winners Bracket",
        losers: "Losers Bracket",
        grand_final: "Grand Final",
        "": "Matchups",
    };

    function bracketLabel(b) {
        if (BRACKET_LABELS[b]) return BRACKET_LABELS[b];
        if (b.startsWith("group_")) {
            return `Group ${b.slice(6).toUpperCase()}`;
        }
        return "Matchups";
    }

    // --- group matches: bracket -> round -> matches
    const byBracket = {};
    matches.forEach((m) => {
        const b = bracketOf(m);
        byBracket[b] ??= {};
        const r = roundOf(m);
        byBracket[b][r] ??= [];
        byBracket[b][r].push(m);
    });

    const bracketOrder = [
        "winners",
        "losers",
        "grand_final",
        ...Object.keys(byBracket)
            .filter(
                (b) => b && !["winners", "losers", "grand_final"].includes(b)
            )
            .sort(),
        "",
    ].filter((b) => byBracket[b]);

    const isKnockout = format === "single_elim" || format === "double_elim";

    // Skeleton rounds for a knockout with nothing scheduled yet.
    function skeletonRounds(count) {
        let size = 2;
        while (size < Math.max(count, 2)) size *= 2;
        if (size > 128) size = 128;
        const out = [];
        let perRound = size / 2;
        let r = 1;
        while (perRound >= 1) {
            out.push({ round: r, count: perRound });
            perRound = Math.floor(perRound / 2);
            r += 1;
        }
        return out;
    }

    if (matches.length === 0) {
        if (teamCount === 0) {
            return (
                <p className="text-secondary">
                    No teams in this league yet — the tournament map appears once teams
                    exist and matches are scheduled.
                </p>
            );
        }

        if (!isKnockout) {
            return (
                <div>
                    <p className="text-secondary small mb-2">
                        {formatLabel} — no matches scheduled yet.
                    </p>
                    <div
                        className="rounded p-3 text-center text-secondary"
                        style={{ border: "1px dashed var(--border-default, #444)" }}
                    >
                        {teamCount} team{teamCount === 1 ? "" : "s"} ready — add matches and
                        they will show up here round by round.
                        {isMod && onAddMatch && (
                            <div className="mt-2">
                                <button
                                    className="btn btn-outline-info btn-sm"
                                    onClick={() => onAddMatch({ bracket: "", round: 1 })}
                                >
                                    + Add match
                                </button>
                            </div>
                        )}
                    </div>
                </div>
            );
        }

        const rounds = skeletonRounds(teamCount);
        return (
            <div className="mb-3">
                <p className="text-secondary small mb-2">
                    {formatLabel} — empty bracket ({teamCount} team
                    {teamCount === 1 ? "" : "s"}). Slots fill in as matches are created.
                </p>
                <div className="d-flex gap-3 align-items-start pb-2" style={{ overflowX: "auto" }}>
                    {rounds.map(({ round, count }) => (
                        <div key={round} style={{ minWidth: 220, flex: "0 0 auto" }}>
                            <div className="text-center text-secondary small fw-semibold mb-2">
                                {count === 1 ? "Final" : `Round ${round}`}
                            </div>
                            {Array.from({ length: count }).map((_, i) => (
                                <div
                                    key={i}
                                    className="rounded p-2 mb-2"
                                    style={{
                                        background: "var(--bg-elevated, #1b1b1b)",
                                        border: "1px dashed var(--border-default, #444)",
                                        minWidth: 200,
                                    }}
                                >
                                    <div className="small text-secondary">TBD</div>
                                    <div className="small text-secondary">TBD</div>
                                </div>
                            ))}
                        </div>
                    ))}
                </div>
            </div>
        );
    }

    function MatchNode({ m }) {
        const winnerA = m.winner_id && m.winner_id === m.team_a_id;
        const winnerB = m.winner_id && m.winner_id === m.team_b_id;
        const teamA = m.team_a || "TBD";
        const teamB = m.team_b || "TBD";
        const isBye = !m.team_a_id || !m.team_b_id;

        const statusClass =
            m.status === "Completed" || m.status === "Finished"
                ? "bg-success"
                : m.status === "Forfeit" || m.status === "Forfeited"
                    ? "bg-danger"
                    : m.status === "Scheduled"
                        ? "bg-warning text-dark"
                        : "bg-secondary";

        return (
            <Link to={`/match/${m.id}`} className="text-decoration-none">
                <div
                    className="rounded p-2 mb-2"
                    style={{
                        background: "var(--bg-elevated, #1b1b1b)",
                        border: "1px solid var(--border-default, #333)",
                        minWidth: 200,
                    }}
                >
                    <div
                        className={`d-flex justify-content-between align-items-center small ${winnerA ? "text-success fw-bold" : "text-light"
                            }`}
                    >
                        <span className="text-truncate">{isBye ? "BYE" : teamA}</span>
                        <span className="ms-2">{winnerA ? "✓" : ""}</span>
                    </div>
                    <div
                        className={`d-flex justify-content-between align-items-center small ${winnerB ? "text-success fw-bold" : "text-light"
                            }`}
                    >
                        <span className="text-truncate">{isBye ? "BYE" : teamB}</span>
                        <span className="ms-2">{winnerB ? "✓" : ""}</span>
                    </div>

                    <div className="d-flex justify-content-between align-items-center mt-1">
                        <span className="text-secondary" style={{ fontSize: "0.7rem" }}>
                            {m.match_code}
                        </span>
                        {isBye ? (
                            <span className="badge bg-dark border border-secondary">BYE</span>
                        ) : (
                            <span className={`badge ${statusClass}`}>{m.status}</span>
                        )}
                    </div>

                    {m.scheduled_date && (
                        <div className="text-secondary" style={{ fontSize: "0.7rem" }}>
                            {new Date(m.scheduled_date).toLocaleString([], {
                                dateStyle: "medium",
                                timeStyle: "short",
                            })}
                        </div>
                    )}
                </div>
            </Link>
        );
    }

    return (
        <div className="mb-3">
            <p className="text-secondary small mb-2">
                {formatLabel} — matchup map. Matches are grouped by round; follow a column
                left to right as the tournament progresses.
            </p>

            {bracketOrder.map((b) => {
                const rounds = byBracket[b];
                const roundKeys = Object.keys(rounds)
                    .map(Number)
                    .sort((x, y) => x - y);

                return (
                    <div key={b || "all"} className="mb-4">
                        <h6 className="text-info border-bottom pb-1 mb-3">
                            {bracketLabel(b)}
                        </h6>

                        <div
                            className="d-flex gap-3 align-items-start pb-2"
                            style={{ overflowX: "auto" }}
                        >
                            {roundKeys.map((r) => (
                                <div key={r} style={{ minWidth: 220, flex: "0 0 auto" }}>
                                    <div className="d-flex align-items-center justify-content-between mb-2">
                                        <span className="text-secondary small fw-semibold">
                                            Round {r}
                                        </span>
                                        {isMod && onAddMatch && (
                                            <button
                                                className="btn btn-outline-info btn-sm py-0 px-2"
                                                title={`Add a match to round ${r}`}
                                                onClick={() =>
                                                    onAddMatch({ bracket: b, round: r })
                                                }
                                            >
                                                +
                                            </button>
                                        )}
                                    </div>
                                    <div className="d-flex flex-column">
                                        {rounds[r].map((m) => (
                                            <MatchNode key={m.id} m={m} />
                                        ))}
                                    </div>
                                </div>
                            ))}
                        </div>
                    </div>
                );
            })}
        </div>
    );
}
