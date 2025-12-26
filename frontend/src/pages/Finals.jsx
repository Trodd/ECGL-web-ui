import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import axios from "axios";

export default function Finals() {
    const API = import.meta.env.VITE_API_URL;

    /* ================= STATE ================= */
    const [visible, setVisible] = useState(true);
    const [me, setMe] = useState(null);
    const [loading, setLoading] = useState(true);

    const [wbRaw, setWbRaw] = useState([]);
    const [lbRaw, setLbRaw] = useState([]);
    const [gf, setGf] = useState([]);

    /* ================= VIEWPORT / FIT ================= */
    const pageRef = useRef(null);
    const fitRef = useRef(null);
    const [scale, setScale] = useState(1);

    /* ================= DESIGN CONSTANTS ================= */
    const HEADER_H = 70;
    const PAD = 16;

    // “Natural” design size (we’ll scale this to fit)
    const ROUND_W = 260;
    const ROUND_GAP = 46;
    const CARD_H = me?.is_mod ? 132 : 96;
    const CARD_INNER_GAP = 8;
    const CARD_RADIUS = 16;

    // Lanes: WB on top, LB bottom
    const LANE_GAP = 18;
    const LANE_TITLE_H = 40;
    const computeLaneHeight = (rounds) => {
        const maxMatches = Math.max(
            1,
            ...rounds.map((r) => r.length)
        );
        return maxMatches * CARD_H + (maxMatches - 1) * 24;
    };

    // LB trim rule (removes extra LB round)
    // A common DE shape is LB rounds = 2*WB - 2 (not counting GF).
    const trimLosersRounds = (wbRounds, lbRounds) => {
        const wbCount = wbRounds.length;
        if (!wbCount) return lbRounds;
        const want = Math.max(0, wbCount * 2 - 2);
        return lbRounds.slice(0, Math.min(lbRounds.length, want));
    };

    const GF_W = 260;
    const GF_H = me?.is_mod ? 160 : 120;
    const GF_GAP = 24;

    /* ================= LOAD ================= */
    useEffect(() => {
        axios
            .get(`${API}/api/finals/visible`)
            .then((r) => setVisible(!!r.data?.visible))
            .catch(() => setVisible(false));

        axios
            .get(`${API}/api/me`, { withCredentials: true })
            .then((r) => setMe(r.data))
            .catch(() => setMe(null));
    }, [API]);

    const sortRound = (r) =>
        Array.isArray(r)
            ? [...r].sort((a, b) => (a.bracket_slot || 0) - (b.bracket_slot || 0))
            : [];

    const load = async () => {
        setLoading(true);
        try {
            const res = await axios.get(`${API}/api/finals/bracket`);
            setWbRaw((res.data?.winners || []).map(sortRound));
            setLbRaw((res.data?.losers || []).map(sortRound));
            setGf(res.data?.grand_finals || []);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        load();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [API]);

    /* ================= DERIVED ROUNDS (TRIM + REINDEX SLOTS) ================= */
    const wb = useMemo(() => {
        // reindex per round so layout is tight and stable
        return (wbRaw || []).map((round) =>
            (round || []).map((m, i) => ({ ...m, __slot: i }))
        );
    }, [wbRaw]);

    const lb = useMemo(() => {
        const trimmed = trimLosersRounds(wbRaw || [], lbRaw || []);
        return (trimmed || []).map((round) =>
            (round || []).map((m, i) => ({ ...m, __slot: i }))
        );
    }, [wbRaw, lbRaw]);

    /* ================= LAYOUT ENGINE (COORDS) =================
       We compute x/y for every match card, and then draw connectors
       directly from those coords. No DOM getBoundingClientRect needed.
    */
    const layoutLane = (rounds, laneTop) => {
        const positions = new Map();
        const roundX = (ri) => PAD + ri * (ROUND_W + ROUND_GAP);

        const laneHeight = computeLaneHeight(rounds);

        rounds.forEach((round, ri) => {
            const count = Math.max(1, round.length);
            const totalCardsH = count * CARD_H;
            const totalGapsH = (count - 1) * 24;
            const contentH = totalCardsH + totalGapsH;

            const startY =
                laneTop +
                LANE_TITLE_H +
                Math.max(0, (laneHeight - contentH) / 2);

            round.forEach((m, i) => {
                const x = roundX(ri);
                const y = startY + i * (CARD_H + 24);

                positions.set(m.id, {
                    x,
                    y,
                    w: ROUND_W,
                    h: CARD_H,
                    m,
                });
            });
        });

        return { positions, laneHeight };
    };

    const buildPaths = (positions, rounds) => {
        const paths = [];
        rounds.forEach((round) => {
            round.forEach((m) => {
                if (!m?.next_match_id) return;
                const a = positions.get(m.id);
                const b = positions.get(m.next_match_id);
                if (!a || !b) return;

                const ax = a.x + a.w;
                const ay = a.y + a.h / 2;

                const bx = b.x;
                const by = b.y + b.h / 2;

                const mid = ax + (bx - ax) * 0.5;

                // classic orthogonal connector
                paths.push(`M ${ax} ${ay} H ${mid} V ${by} H ${bx}`);
            });
        });
        return paths;
    };

    const natural = useMemo(() => {
        const wbTop = PAD + HEADER_H;
        const { positions: wbPos, laneHeight: wbH } = layoutLane(wb, wbTop);

        const lbTop = wbTop + LANE_TITLE_H + wbH + LANE_GAP;
        const { positions: lbPos, laneHeight: lbH } = layoutLane(lb, lbTop);

        const wbPaths = buildPaths(wbPos, wb);
        const lbPaths = buildPaths(lbPos, lb);

        const roundsMax = Math.max(wb.length, lb.length);
        const baseWidth =
            PAD * 2 +
            roundsMax * ROUND_W +
            Math.max(0, roundsMax - 1) * ROUND_GAP;

        const gfTop = lbTop + LANE_TITLE_H + lbH + GF_GAP;
        const gfLeft = (baseWidth - GF_W) / 2;

        const totalHeight =
            gfTop +
            GF_H +
            PAD;

        return {
            wbTop,
            lbTop,
            gfTop,
            gfLeft,
            wbPos,
            lbPos,
            wbPaths,
            lbPaths,
            width: baseWidth,
            height: totalHeight,
        };

    }, [wb, lb, me?.is_mod]);

    /* ================= ZOOM TO FIT ================= */
    const recomputeScale = () => {
        const page = pageRef.current;
        const fit = fitRef.current;
        if (!page || !fit) return;

        const vw = page.clientWidth;
        const vh = page.clientHeight;

        const maxW = Math.max(320, vw - 8);
        const maxH = Math.max(320, vh - 8);

        const sW = maxW / natural.width;
        const sH = maxH / natural.height;

        // scale down to fit, but don’t scale up beyond 1
        const s = Math.min(1, sW, sH);
        setScale(s);
    };

    useLayoutEffect(() => {
        recomputeScale();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [natural.width, natural.height]);

    useEffect(() => {
        const onResize = () => recomputeScale();
        window.addEventListener("resize", onResize);
        return () => window.removeEventListener("resize", onResize);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [natural.width, natural.height]);

    /* ================= ACTION ================= */
    const setWinner = async (matchId, winnerId) => {
        if (!me?.is_mod) return;
        const idNum = Number(winnerId);
        if (!idNum) return;

        await axios.post(
            `${API}/api/mod/finals/update-match`,
            { match_id: matchId, winner: idNum },
            { withCredentials: true }
        );
        await load();
    };

    /* ================= UI ================= */
    if (!visible) return <p className="text-light">Finals not visible.</p>;
    if (loading) return <p className="text-light">Loading finals…</p>;

    const TeamRow = ({ name, isWinner }) => (
        <div
            title={name || "TBD"}
            style={{
                width: "100%",
                padding: "7px 10px",
                borderRadius: 10,
                fontWeight: 900,
                letterSpacing: 0.2,
                color: "#f8fafc",
                background: isWinner
                    ? "linear-gradient(180deg, rgba(34,197,94,0.28), rgba(34,197,94,0.16))"
                    : "linear-gradient(180deg, rgba(255,255,255,0.12), rgba(255,255,255,0.06))",
                border: isWinner
                    ? "1px solid rgba(34,197,94,0.55)"
                    : "1px solid rgba(255,255,255,0.25)",
                whiteSpace: "nowrap",
                overflow: "visible",
                textOverflow: "ellipsis",
            }}
        >
            {name || "TBD"}
        </div>
    );

    const MatchCard = ({ pos }) => {
        const m = pos.m;
        const aWin = m?.winner_id && m.winner_id === m.team_a_id;
        const bWin = m?.winner_id && m.winner_id === m.team_b_id;

        return (
            <div
                style={{
                    position: "absolute",
                    left: pos.x,
                    top: pos.y,
                    width: pos.w,
                    height: pos.h,
                    borderRadius: CARD_RADIUS,
                    padding: 10,
                    background: "rgba(0,0,0,0.55)",
                    border: "1px solid rgba(255,255,255,0.22)",
                    boxShadow: "0 12px 30px rgba(0,0,0,0.40)",
                    display: "flex",
                    flexDirection: "column",
                    justifyContent: "space-between",
                    overflow: "visible",
                }}
            >
                <div style={{ display: "grid", gap: CARD_INNER_GAP }}>
                    <TeamRow name={m.team_a} isWinner={aWin} />
                    <TeamRow name={m.team_b} isWinner={bWin} />
                </div>

                {me?.is_mod && (
                    <select
                        className="form-select bg-dark text-light"
                        value={m.winner_id || ""}
                        onChange={(e) => setWinner(m.id, e.target.value)}
                        style={{
                            marginTop: 8,
                            borderRadius: 10,
                            border: "1px solid rgba(255,255,255,0.22)",
                            backgroundColor: "rgba(0,0,0,0.45)",
                            fontWeight: 800,
                            fontSize: 12,
                            paddingTop: 6,
                            paddingBottom: 6,
                        }}
                    >
                        <option value="">Pick winner…</option>
                        {m.team_a_id ? <option value={m.team_a_id}>{m.team_a}</option> : null}
                        {m.team_b_id ? <option value={m.team_b_id}>{m.team_b}</option> : null}
                    </select>
                )}
            </div>
        );
    };

    const LaneTitle = ({ top, label, rounds }) => (
        <div
            style={{
                position: "absolute",
                left: PAD,
                top,
                height: LANE_TITLE_H,
                width: natural.width - PAD * 2,
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: 12,
                padding: "0 12px",
                borderRadius: 14,
                background: "rgba(0,0,0,0.45)",
                border: "1px solid rgba(255,255,255,0.18)",
                color: "#e8eaed",
                fontWeight: 950,
                letterSpacing: 0.3,
            }}
        >
            <div>{label}</div>
            <div style={{ fontSize: 12, opacity: 0.75 }}>{rounds} rounds</div>
        </div>
    );

    return (
        <div
            ref={pageRef}
            style={{
                height: "calc(100vh - 120px)", // tweak if your site has different header height
                width: "100%",
                position: "relative",
                overflow: "visible",
                color: "#e8eaed",
            }}
        >
            {/* FIT WRAPPER */}
            <div
                ref={fitRef}
                style={{
                    position: "absolute",
                    left: "50%",
                    top: "50%",
                    transform: `translate(-50%, -50%) scale(${scale})`,
                    transformOrigin: "center",
                    width: natural.width,
                    height: natural.height,
                    borderRadius: 18,
                    background:
                        "radial-gradient(1200px 600px at 15% 15%, rgba(59,110,165,0.18), transparent 60%), radial-gradient(900px 500px at 85% 25%, rgba(160,80,200,0.14), transparent 60%), rgba(0,0,0,0.35)",
                    border: "1px solid rgba(255,255,255,0.12)",
                    boxShadow: "0 20px 70px rgba(0,0,0,0.55)",
                    overflow: "visible",
                }}
            >
                {/* HEADER */}
                <div
                    style={{
                        position: "absolute",
                        inset: 0,
                        height: HEADER_H,
                        padding: "14px 16px",
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        gap: 12,
                        background: "rgba(0,0,0,0.55)",
                        borderBottom: "1px solid rgba(255,255,255,0.12)",
                    }}
                >
                    <div style={{ fontWeight: 950, letterSpacing: 0.4, fontSize: 18 }}>
                        🏆 Finals Bracket
                    </div>

                    <button
                        className="btn btn-sm"
                        onClick={load}
                        style={{
                            borderRadius: 999,
                            padding: "6px 10px",
                            fontWeight: 900,
                            border: "1px solid rgba(255,255,255,0.18)",
                            background: "rgba(255,255,255,0.08)",
                            color: "#e8eaed",
                        }}
                    >
                        ↻ Refresh
                    </button>
                </div>

                {/* LANE TITLES */}
                <LaneTitle top={natural.wbTop} label="🥇 Winners Bracket" rounds={wb.length} />
                <LaneTitle top={natural.lbTop} label="🥉 Losers Bracket" rounds={lb.length} />

                {/* CONNECTORS (WB + LB) */}
                <svg
                    width={natural.width}
                    height={natural.height}
                    style={{ position: "absolute", inset: 0, pointerEvents: "none" }}
                >
                    {natural.wbPaths.map((d, i) => (
                        <path key={`wb-${i}`} d={d} stroke="rgba(220,220,220,0.55)" strokeWidth="2" fill="none" />
                    ))}
                    {natural.lbPaths.map((d, i) => (
                        <path key={`lb-${i}`} d={d} stroke="rgba(220,220,220,0.55)" strokeWidth="2" fill="none" />
                    ))}
                </svg>

                {/* MATCH CARDS */}
                {Array.from(natural.wbPos.values()).map((pos) => (
                    <MatchCard key={`m-${pos.m.id}`} pos={pos} />
                ))}
                {Array.from(natural.lbPos.values()).map((pos) => (
                    <MatchCard key={`m-${pos.m.id}`} pos={pos} />
                ))}

                {/* GRAND FINALS (OPTIONAL SIMPLE PANEL, STILL FITS) */}
                {gf?.[0] ? (
                    <div
                        style={{
                            position: "absolute",
                            top: natural.gfTop,
                            left: natural.gfLeft,
                            width: GF_W,
                            minHeight: GF_H,
                            borderRadius: 16,
                            padding: 12,
                            background: "rgba(0,0,0,0.55)",
                            border: "1px solid rgba(255,215,0,0.30)",
                            boxShadow: "0 14px 40px rgba(0,0,0,0.45)",
                        }}
                    >
                        <div style={{ fontWeight: 950, marginBottom: 10, textAlign: "center" }}>
                            🏁 Grand Finals
                        </div>

                        <div style={{ display: "grid", gap: 8 }}>
                            <TeamRow
                                name={gf[0].team_a}
                                isWinner={gf[0]?.winner_id === gf[0]?.team_a_id}
                            />
                            <TeamRow
                                name={gf[0].team_b}
                                isWinner={gf[0]?.winner_id === gf[0]?.team_b_id}
                            />
                        </div>

                        {me?.is_mod && (
                            <select
                                className="form-select bg-dark text-light"
                                value={gf[0].winner_id || ""}
                                onChange={(e) => setWinner(gf[0].id, e.target.value)}
                                style={{
                                    marginTop: 10,
                                    borderRadius: 10,
                                    border: "1px solid rgba(255,255,255,0.22)",
                                    backgroundColor: "rgba(0,0,0,0.45)",
                                    fontWeight: 800,
                                    fontSize: 12,
                                }}
                            >
                                <option value="">Pick winner…</option>
                                {gf[0].team_a_id && (
                                    <option value={gf[0].team_a_id}>{gf[0].team_a}</option>
                                )}
                                {gf[0].team_b_id && (
                                    <option value={gf[0].team_b_id}>{gf[0].team_b}</option>
                                )}
                            </select>
                        )}
                    </div>
                ) : null}
            </div>
        </div>
    );
}
