import { useState, useEffect } from "react";
import axios from "axios";

export default function Finals() {
    const [visible, setVisible] = useState(true);
    const [me, setMe] = useState(null);
    const [loading, setLoading] = useState(true);

    const [winnerRounds, setWinnerRounds] = useState([]);
    const [loserRounds, setLoserRounds] = useState([]);
    const [grandFinals, setGrandFinals] = useState([]);
    const [resetPossible, setResetPossible] = useState(false);

    const urlBase = import.meta.env.VITE_API_URL;

    // --- Load finals visibility ---
    useEffect(() => {
        axios
            .get(`${urlBase}/api/finals/visible`)
            .then((res) => setVisible(!!res.data?.visible))
            .catch(() => setVisible(false));
    }, [urlBase]);

    if (!visible) {
        return (
            <p className="text-light">
                🏆 Finals bracket is not available yet.
            </p>
        );
    }

    // --- Load user (for mod perms) ---
    useEffect(() => {
        axios
            .get(`${urlBase}/api/me`, { withCredentials: true })
            .then((res) => setMe(res.data))
            .catch(() => setMe(null));
    }, [urlBase]);

    // --- Main load of bracket data ---
    const loadBracket = async () => {
        setLoading(true);
        try {
            const res = await axios.get(`${urlBase}/api/finals/bracket`);
            setWinnerRounds(res.data?.winners || []);
            setLoserRounds(res.data?.losers || []);
            setGrandFinals(res.data?.grand_finals || []);
            setResetPossible(!!res.data?.reset_possible);
        } catch (err) {
            console.error("❌ Failed loading finals bracket:", err);
            setWinnerRounds([]);
            setLoserRounds([]);
            setGrandFinals([]);
            setResetPossible(false);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        loadBracket();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // --- Mod: set winner for a match ---
    const setWinner = async (match, winnerID) => {
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
            await loadBracket();
        } catch (error) {
            console.error("❌ Failed to set winner:", error);
        }
    };

    // --- Match card with optional connector line ---
    const renderMatch = (m, { connectRight = false } = {}) => {
        if (!m) return null;

        const aWin = m.winner_id && m.winner_id === m.team_a_id;
        const bWin = m.winner_id && m.winner_id === m.team_b_id;

        return (
            <div className="fb-match-wrapper" key={m.id}>
                <div
                    className="fb-match rounded p-2 mb-2"
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

                    {me?.is_mod && (
                        <select
                            className="form-select bg-dark text-light mt-1 fb-winner-select"
                            onChange={(e) => setWinner(m, e.target.value)}
                            defaultValue=""
                        >
                            <option value="">Pick winner…</option>
                            {m.team_a_id ? (
                                <option value={m.team_a_id}>{m.team_a}</option>
                            ) : null}
                            {m.team_b_id ? (
                                <option value={m.team_b_id}>{m.team_b}</option>
                            ) : null}
                        </select>
                    )}
                </div>

                {/* connector to next round */}
                {connectRight && <div className="fb-connector-next" />}
            </div>
        );
    };

    // Helper: filter out pure placeholder matches (0 vs 0 / TBD vs TBD)
    const filterRealMatches = (round) => {
        if (!Array.isArray(round)) return [];
        return round.filter((m) => {
            const aReal = m.team_a_id && m.team_a_id !== 0;
            const bReal = m.team_b_id && m.team_b_id !== 0;
            return aReal || bReal;
        });
    };

    if (loading) return <p className="text-light">Loading finals…</p>;

    const hasBracket =
        (winnerRounds && winnerRounds.length > 0) ||
        (loserRounds && loserRounds.length > 0) ||
        (grandFinals && grandFinals.length > 0);

    if (!hasBracket) return <p className="text-light">No finals bracket generated.</p>;

    return (
        <div className="text-light fb-bracket">

            {/* WINNERS BRACKET */}
            {winnerRounds && winnerRounds.length > 0 && (
                <section className="mb-5">
                    <h3 className="text-success mb-4">🥇 Winners Bracket</h3>

                    {/* Build list of non-empty winner rounds */}
                    <div className="d-flex flex-wrap gap-4">
                        {winnerRounds
                            .map((round, idx) => {
                                const realMatches = filterRealMatches(round);
                                if (realMatches.length === 0) return null;
                                return { idx, matches: realMatches };
                            })
                            .filter(Boolean)
                            .map((col, colIndex, arr) => {
                                const isLast = colIndex === arr.length - 1;
                                return (
                                    <div key={`wb-${col.idx}`} className="fb-column">
                                        <h5 className="text-info mb-2">
                                            WB Round {colIndex + 1}
                                        </h5>
                                        {col.matches.map((m) =>
                                            renderMatch(m, { connectRight: !isLast })
                                        )}
                                    </div>
                                );
                            })}
                    </div>
                </section>
            )}

            {/* LOSERS BRACKET */}
            {loserRounds && loserRounds.length > 0 && (
                <section className="mb-5">
                    <h3 className="text-warning mb-4">🥉 Losers Bracket</h3>

                    <div className="d-flex flex-wrap gap-4">
                        {loserRounds
                            .map((round, idx) => {
                                const realMatches = filterRealMatches(round);
                                if (realMatches.length === 0) return null;
                                return { idx, matches: realMatches };
                            })
                            .filter(Boolean)
                            .map((col, colIndex, arr) => {
                                const isLast = colIndex === arr.length - 1;
                                return (
                                    <div key={`lb-${col.idx}`} className="fb-column">
                                        <h5 className="text-info mb-2">
                                            LB Round {colIndex + 1}
                                        </h5>
                                        {col.matches.map((m) =>
                                            renderMatch(m, { connectRight: !isLast })
                                        )}
                                    </div>
                                );
                            })}
                    </div>
                </section>
            )}

            {/* GRAND FINALS */}
            {grandFinals && grandFinals.length > 0 && (
                <section>
                    <h3 className="text-light mb-4">🏁 Grand Finals</h3>

                    <div className="d-flex flex-wrap gap-4">

                        {/* Only show the FIRST Grand Final */}
                        <div className="fb-column">
                            <h5 className="text-info mb-2">Grand Final</h5>
                            {renderMatch(grandFinals[0], { connectRight: false })}
                        </div>

                    </div>
                </section>
            )}
        </div>
    );
}
