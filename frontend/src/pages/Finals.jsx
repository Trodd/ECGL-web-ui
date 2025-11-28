
import { useState, useEffect } from "react";
import axios from "axios";

export default function Finals() {
    const [bracket, setBracket] = useState(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const urlBase = import.meta.env.VITE_API_URL;
    const [me, setMe] = useState(null);

    async function loadMe() {
        try {
            const res = await axios.get(`${urlBase}/api/me`, { withCredentials: true });
            setMe(res.data);
        } catch (err) {
            console.error("Failed to load me:", err);
        }
    }

    useEffect(() => {
        loadMe();
    }, []);

    // --- load bracket ---
    async function loadBracket() {
        try {
            const res = await axios.get(`${urlBase}/api/finals/bracket`, { withCredentials: true });
            setBracket(res.data);
        } catch (err) {
            console.error("Failed to load finals bracket:", err);
            setError("Could not load finals bracket.");
        } finally {
            setLoading(false);
        }
    }

    useEffect(() => {
        loadBracket();
    }, []);

    const onWinnerSelect = async ({ bracket, round, slot, winner }) => {
        try {
            await axios.post(
                `${urlBase}/api/mod/finals/update-match`,
                { bracket, round, slot, winner },
                { withCredentials: true }
            );
            loadBracket();
        } catch (err) {
            console.error("Failed to set winner:", err);
        }
    };

    if (loading) return <p className="text-light">Loading finals bracket...</p>;
    if (error) return <p className="text-danger">{error}</p>;

    if (!bracket) return <p className="text-light">No finals bracket generated.</p>;

    // shortcut helpers
    const WB = bracket.winners || [];      // winners[roundIndex][matchIndex]
    const LB = bracket.losers || [];
    const GF = bracket.grand_final || null;

    const renderMatch = (m, bracketName, round, slot) => (
        <div className="fb-match">
            <div className="fb-teams">
                <div className="fb-team">{m.team_a}</div>
                <div className="fb-team">{m.team_b}</div>
            </div>

            {/* Winner selector (mods only) */}
            {me?.is_mod && (
                <select
                    className="fb-winner-select"
                    onChange={(e) => {
                        const winnerTeamID = Number(e.target.value);
                        if (!winnerTeamID) return;

                        onWinnerSelect({
                            bracket: bracketName,
                            round,
                            slot,
                            winner: winnerTeamID
                        });
                    }}
                >
                    <option value="">Set Winner…</option>
                    <option value={m.team_a_id}>{m.team_a}</option>
                    <option value={m.team_b_id}>{m.team_b}</option>
                </select>
            )}
        </div>
    );

    return (
        <div className="fb-container">

            {/* ================= WINNERS BRACKET ================= */}
            <div className="fb-section">
                <h5 className="fb-section-title text-success">Winners Bracket</h5>

                <div className="fb-columns">

                    {/* Round 1 */}
                    <div className="fb-column">
                        <div className="fb-column-title">Round 1</div>
                        {WB[0]?.map((m, i) =>
                            renderMatch(m, "winners", 1, i + 1)
                        )}
                    </div>

                    {/* Semifinals */}
                    <div className="fb-column">
                        <div className="fb-column-title">Semifinals</div>
                        {WB[1]?.map((m, i) =>
                            renderMatch(m, "winners", 2, i + 1)
                        )}
                    </div>

                    {/* Finals */}
                    <div className="fb-column">
                        <div className="fb-column-title">Finals</div>
                        {GF ? (
                            <div className="fb-match">
                                <div className="fb-teams">
                                    <div className="fb-team">{GF.team_a}</div>
                                    <div className="fb-team">{GF.team_b}</div>
                                </div>

                                {/* Winner selector (mods only) */}
                                {me?.is_mod && (
                                    <select
                                        className="fb-winner-select"
                                        onChange={(e) => {
                                            const winnerTeamID = Number(e.target.value);
                                            if (!winnerTeamID) return;

                                            onWinnerSelect({
                                                bracket: "grand_final",
                                                round: 1,
                                                slot: 1,
                                                winner: winnerTeamID
                                            });
                                        }}
                                    >
                                        <option value="">Set Winner…</option>
                                        <option value={GF.team_a_id}>{GF.team_a}</option>
                                        <option value={GF.team_b_id}>{GF.team_b}</option>
                                    </select>
                                )}
                            </div>
                        ) : (
                            <p className="text-light">No finals match</p>
                        )}
                    </div>
                </div>
            </div>

            {/* ================= LOSERS BRACKET ================= */}
            <div className="fb-section" style={{ marginTop: "40px" }}>
                <h5 className="fb-section-title text-danger">Losers Bracket</h5>

                <div className="fb-columns">

                    {/* Losers Round 1 */}
                    <div className="fb-column">
                        <div className="fb-column-title">Losers Round 1</div>
                        {LB[0]?.map((m, i) =>
                            renderMatch(m, "losers", 1, i + 1)
                        )}
                    </div>

                    {/* Losers Round 2 */}
                    <div className="fb-column">
                        <div className="fb-column-title">Losers Round 2</div>
                        {LB[1]?.map((m, i) =>
                            renderMatch(m, "losers", 2, i + 1)
                        )}
                    </div>
                </div>
            </div>
        </div>
    );
}
