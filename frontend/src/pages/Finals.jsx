import { useState, useEffect } from "react";
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
        return <p className="text-light">No finals bracket available.</p>;
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
                <div className="fb-match rounded p-2 mb-2"
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
        <div className="text-light fb-bracket">

            {/* WINNERS */}
            {winnerRounds.length > 0 && (
                <section className="mb-5">
                    <h3 className="text-success mb-4">🥇 Winners Bracket</h3>
                    <div className="d-flex flex-wrap gap-4">
                        {winnerRounds.map((round, i) => {
                            const matches = filterRealMatches(round);
                            if (!matches.length) return null;
                            return (
                                <div key={i} className="fb-column">
                                    <h5 className="text-info mb-2">
                                        WB Round {i + 1}
                                    </h5>
                                    {matches.map((m) =>
                                        renderMatch(m, { connectRight: i < winnerRounds.length - 1 })
                                    )}
                                </div>
                            );
                        })}
                    </div>
                </section>
            )}

            {/* LOSERS */}
            {loserRounds.length > 0 && (
                <section className="mb-5">
                    <h3 className="text-warning mb-4">🥉 Losers Bracket</h3>
                    <div className="d-flex flex-wrap gap-4">
                        {loserRounds.map((round, i) => {
                            const matches = filterRealMatches(round);
                            if (!matches.length) return null;
                            return (
                                <div key={i} className="fb-column">
                                    <h5 className="text-info mb-2">
                                        LB Round {i + 1}
                                    </h5>
                                    {matches.map((m) =>
                                        renderMatch(m, { connectRight: i < loserRounds.length - 1 })
                                    )}
                                </div>
                            );
                        })}
                    </div>
                </section>
            )}

            {/* GRAND FINAL */}
            {grandFinals.length > 0 && (
                <section>
                    <h3 className="text-light mb-4">🏁 Grand Finals</h3>
                    <div className="fb-column">
                        {renderMatch(grandFinals[0])}
                    </div>
                </section>
            )}
        </div>
    );
}
