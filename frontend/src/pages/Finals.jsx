import { useState, useEffect, useRef, useLayoutEffect, } from "react";
import axios from "axios";

export default function Finals() {
    const urlBase = import.meta.env.VITE_API_URL;
    const currentSeason = import.meta.env.VITE_CURRENT_SEASON;

    const [visible, setVisible] = useState(true);
    const [me, setMe] = useState(null);
    const [loading, setLoading] = useState(true);

    const [season, setSeason] = useState(currentSeason);
    const readOnly = season !== currentSeason;

    const [winnerRounds, setWinnerRounds] = useState([]);
    const [loserRounds, setLoserRounds] = useState([]);
    const [grandFinals, setGrandFinals] = useState([]);
    const [resetPossible, setResetPossible] = useState(false);

    const matchRefs = useRef({});
    const [connections, setConnections] = useState([]);

    const registerMatchRef = (id, el) => {
        if (el) matchRefs.current[id] = el;
    };

    const buildConnections = () => {
        const paths = [];

        const container = document.querySelector(".fb-bracket");
        if (!container) return;

        const containerRect = container.getBoundingClientRect();

        const getAnchor = (el, side = "right") => {
            const r = el.getBoundingClientRect();
            return {
                x:
                    (side === "right" ? r.right : r.left) -
                    containerRect.left,
                y:
                    r.top +
                    r.height / 2 -
                    containerRect.top,
            };
        };

        winnerRounds.forEach((round) => {
            round.forEach((m) => {
                if (!m.next_match_id) return;

                const fromEl = matchRefs.current[m.id];
                const toEl = matchRefs.current[m.next_match_id];
                if (!fromEl || !toEl) return;

                paths.push({
                    from: getAnchor(fromEl, "right"),
                    to: getAnchor(toEl, "left"),
                });
            });
        });

        loserRounds.forEach((round) => {
            round.forEach((m) => {
                if (!m.next_match_id) return;

                const fromEl = matchRefs.current[m.id];
                const toEl = matchRefs.current[m.next_match_id];
                if (!fromEl || !toEl) return;

                paths.push({
                    from: getAnchor(fromEl, "right"),
                    to: getAnchor(toEl, "left"),
                });
            });
        });

        setConnections(paths);
    };

    useEffect(() => {
        const handler = () => buildConnections();
        window.addEventListener("resize", handler);
        window.addEventListener("scroll", handler, true);

        return () => {
            window.removeEventListener("resize", handler);
            window.removeEventListener("scroll", handler, true);
        };
    }, []);

    // -------------------------------
    // Finals visibility
    // -------------------------------
    useEffect(() => {
        axios
            .get(`${urlBase}/api/finals/visible`)
            .then((res) => setVisible(!!res.data?.visible))
            .catch(() => setVisible(false));
    }, [urlBase]);

    // -------------------------------
    // Load user (mod perms)
    // -------------------------------
    useEffect(() => {
        axios
            .get(`${urlBase}/api/me`, { withCredentials: true })
            .then((res) => setMe(res.data))
            .catch(() => setMe(null));
    }, [urlBase]);

    // -------------------------------
    // Unified finals loader
    // -------------------------------
    const loadFinals = async (targetSeason) => {
        setLoading(true);

        try {
            if (targetSeason === currentSeason) {
                // 🔥 LIVE FINALS
                const res = await axios.get(`${urlBase}/api/finals/bracket`);
                setWinnerRounds(res.data?.winners || []);
                setLoserRounds(res.data?.losers || []);
                setGrandFinals(res.data?.grand_finals || []);
                setResetPossible(!!res.data?.reset_possible);
            } else {
                // 🧊 ARCHIVED FINALS (READ-ONLY)
                const res = await axios.get(
                    `${urlBase}/api/finals/archive?season=${targetSeason}`
                );

                const matches = Array.isArray(res.data?.matches)
                    ? res.data.matches
                    : [];

                // Group archived matches back into rounds
                const winners = {};
                const losers = {};
                const grand = [];

                for (const m of matches) {
                    if (m.bracket === "W") {
                        winners[m.bracket_round] ??= [];
                        winners[m.bracket_round].push(m);
                    } else if (m.bracket === "L") {
                        losers[m.bracket_round] ??= [];
                        losers[m.bracket_round].push(m);
                    } else if (m.bracket === "G") {
                        grand.push(m);
                    }
                }

                setWinnerRounds(Object.values(winners));
                setLoserRounds(Object.values(losers));
                setGrandFinals(grand);
                setResetPossible(false);
            }
        } catch (err) {
            console.error("❌ Failed to load finals:", err);
            setWinnerRounds([]);
            setLoserRounds([]);
            setGrandFinals([]);
            setResetPossible(false);
        } finally {
            setLoading(false);
        }
    };

    // -------------------------------
    // Reload on season change
    // -------------------------------
    useEffect(() => {
        loadFinals(season);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [season]);

    if (!visible) {
        return <p className="text-light">🏆 Finals bracket is not available yet.</p>;
    }

    if (loading) {
        return <p className="text-light">Loading finals…</p>;
    }

    const hasBracket =
        winnerRounds.length > 0 ||
        loserRounds.length > 0 ||
        grandFinals.length > 0;

    if (!hasBracket) {
        return (
            <div className="d-flex justify-content-center align-items-center py-5">
                <div
                    className="text-center p-5 rounded-4"
                    style={{
                        maxWidth: 520,
                        background: "linear-gradient(180deg, #151515, #0f0f0f)",
                        border: "1px solid #2c2c2c",
                        boxShadow: "0 15px 40px rgba(0,0,0,0.6)",
                    }}
                >
                    <h2
                        className="mb-3"
                        style={{
                            color: "#9ecbff",
                            textShadow: "0 0 14px rgba(59,110,165,0.6)",
                            fontWeight: 700,
                        }}
                    >
                        🏆 Finals Bracket
                    </h2>

                    <p
                        className="mb-0"
                        style={{
                            fontSize: "1.05rem",
                            color: "#9aa0a6",
                            lineHeight: 1.6,
                        }}
                    >
                        The finals bracket hasn’t been generated yet.
                        <br />
                        Check back once playoffs begin.
                    </p>
                </div>
            </div>
        );
    }

    // -------------------------------
    // Mod: set winner (disabled if read-only)
    // -------------------------------
    const setWinner = async (match, winnerID) => {
        if (readOnly) return;

        const idNum = Number(winnerID);
        if (!idNum || !match?.id) return;

        try {
            await axios.post(
                `${urlBase}/api/mod/finals/update-match`,
                {
                    match_id: match.id,
                    winner: idNum,
                },
                { withCredentials: true }
            );
            await loadFinals(season);
        } catch (err) {
            console.error("❌ Failed to set winner:", err);
        }
    };

    const renderMatch = (m, { connectRight = false } = {}) => {
        if (!m) return null;

        const aWin = m.winner_id === m.team_a_id;
        const bWin = m.winner_id === m.team_b_id;

        return (
            <div className="fb-match-wrapper" key={m.id}>
                <div
                    className="fb-match rounded p-2 mb-2"
                    ref={(el) => registerMatchRef(m.id, el)}
                    style={{ backgroundColor: "#1a1a1a", border: "1px solid #333" }}
                >
                    <div className="fb-teams">
                        <div className={`fb-team ${aWin ? "fb-team-winner" : ""}`}>
                            {m.team_a || "TBD"}
                        </div>
                        <div className={`fb-team ${bWin ? "fb-team-winner" : ""}`}>
                            {m.team_b || "TBD"}
                        </div>
                    </div>

                    {me?.is_mod && !readOnly && (
                        <select
                            className="form-select bg-dark text-light mt-1"
                            onChange={(e) => setWinner(m, e.target.value)}
                            defaultValue=""
                        >
                            <option value="">Pick winner…</option>
                            {m.team_a_id && (
                                <option value={m.team_a_id}>{m.team_a}</option>
                            )}
                            {m.team_b_id && (
                                <option value={m.team_b_id}>{m.team_b}</option>
                            )}
                        </select>
                    )}
                </div>

                {connectRight && <div className="fb-connector-next" />}
            </div>
        );
    };

    const filterRealMatches = (round) =>
        Array.isArray(round)
            ? round.filter(
                (m) => m.team_a_id || m.team_b_id
            )
            : [];

    return (
        <div className="fb-bracket finals-root text-light">
            {/* SVG CONNECTORS */}
            <svg
                className="fb-svg"
                width="100%"
                height="100%"
                preserveAspectRatio="none"
            >
                {connections.map((c, i) => {
                    const midX = (c.from.x + c.to.x) / 2;

                    return (
                        <g key={i}>
                            {/* Horizontal from source */}
                            <line
                                x1={c.from.x}
                                y1={c.from.y}
                                x2={midX}
                                y2={c.from.y}
                                stroke="rgba(200,200,200,0.6)"
                                strokeWidth="2"
                                strokeLinecap="square"
                            />

                            {/* Vertical merge */}
                            <line
                                x1={midX}
                                y1={c.from.y}
                                x2={midX}
                                y2={c.to.y}
                                stroke="rgba(200,200,200,0.6)"
                                strokeWidth="2"
                                strokeLinecap="square"
                            />

                            {/* Horizontal into target */}
                            <line
                                x1={midX}
                                y1={c.to.y}
                                x2={c.to.x}
                                y2={c.to.y}
                                stroke="rgba(200,200,200,0.6)"
                                strokeWidth="2"
                                strokeLinecap="square"
                            />
                        </g>
                    );
                })}
            </svg>

            {/* ================= WINNERS ================= */}
            {winnerRounds.length > 0 && (
                <section className="finals-section winners">
                    <div className="finals-header">
                        <h3>🥇 Winners Bracket</h3>
                        <span className="finals-sub">Road to Grand Finals</span>
                    </div>

                    <div className="finals-lanes">
                        {winnerRounds.map((round, i) => {
                            const matches = filterRealMatches(round);
                            if (!matches.length) return null;

                            return (
                                <div key={i} className="fb-column finals-round">
                                    <div className="round-label">
                                        WB Round {i + 1}
                                    </div>

                                    {matches.map((m) =>
                                        renderMatch(m, {
                                            connectRight: i < winnerRounds.length - 1,
                                        })
                                    )}
                                </div>
                            );
                        })}
                    </div>
                </section>
            )}

            {/* ================= LOSERS ================= */}
            {loserRounds.length > 0 && (
                <section className="finals-section losers">
                    <div className="finals-header">
                        <h3>🥉 Losers Bracket</h3>
                        <span className="finals-sub">Second chance gauntlet</span>
                    </div>

                    <div className="finals-lanes">
                        {loserRounds.map((round, i) => {
                            const matches = filterRealMatches(round);
                            if (!matches.length) return null;

                            return (
                                <div key={i} className="fb-column finals-round">
                                    <div className="round-label">
                                        LB Round {i + 1}
                                    </div>

                                    {matches.map((m) =>
                                        renderMatch(m, {
                                            connectRight: i < loserRounds.length - 1,
                                        })
                                    )}
                                </div>
                            );
                        })}
                    </div>
                </section>
            )}

            {/* ================= GRAND FINALS ================= */}
            {grandFinals.length > 0 && (
                <section className="finals-section grand-finals">
                    <div className="finals-header grand">
                        <h3>🏁 Grand Finals</h3>
                        <span className="finals-sub">Championship Match</span>
                    </div>

                    <div className="grand-finals-stage">
                        <div className="fb-column">
                            {renderMatch(grandFinals[0])}
                        </div>
                    </div>
                </section>
            )}
        </div>
    );
}
