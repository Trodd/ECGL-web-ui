import { useEffect, useMemo, useRef, useState, useCallback } from "react";
import axios from "axios";
import { getApiUrl } from "../config";
import { E } from "../components/CustomEmoji";

export default function Finals() {
    const API = getApiUrl();

    /* ═══════════ STATE ═══════════ */
    const [visible, setVisible] = useState(true);
    const [modVisible, setModVisible] = useState(false);
    const [me, setMe] = useState(null);
    const [meLoaded, setMeLoaded] = useState(false);
    const [loading, setLoading] = useState(true);
    const [wbRaw, setWbRaw] = useState([]);
    const [lbRaw, setLbRaw] = useState([]);
    const [gf, setGf] = useState([]);
    const [allTeams, setAllTeams] = useState([]);
    const [selectedMatch, setSelectedMatch] = useState(null);
    const [modalTeamA, setModalTeamA] = useState("");
    const [modalTeamB, setModalTeamB] = useState("");
    const [modalWinner, setModalWinner] = useState("");
    const [saving, setSaving] = useState(false);
    const [containerWidth, setContainerWidth] = useState(window.innerWidth);
    const containerRef = useRef(null);

    /* ═══════════ RESPONSIVE ═══════════ */
    useEffect(() => {
        const measure = () => {
            if (containerRef.current) {
                setContainerWidth(containerRef.current.clientWidth);
            } else {
                setContainerWidth(window.innerWidth);
            }
        };
        measure();
        window.addEventListener("resize", measure);
        return () => window.removeEventListener("resize", measure);
    }, [loading, meLoaded]);

    const isMobile = containerWidth < 500;

    /* ═══════════ DESIGN TOKENS ═══════════ */
    const COL = {
        wbAccent: "#5a9fd4",
        wbDim: "rgba(59,110,165,0.4)",
        lbAccent: "#a855f7",
        lbDim: "rgba(168,85,247,0.35)",
        gfAccent: "#fbbf24",
        gfDim: "rgba(251,191,36,0.5)",
        winText: "#4ade80",
        loseText: "#f87171",
        teamText: "#e8ecf0",
        tbdText: "#5c6b7a",
        boxBg: "rgba(13,17,23,0.92)",
        boxBgDim: "rgba(13,17,23,0.6)",
        connWb: "rgba(90,159,212,0.4)",
        connLb: "rgba(168,85,247,0.35)",
        connGf: "rgba(251,191,36,0.35)",
        labelWb: "#5a9fd4",
        labelLb: "#a855f7",
        labelGf: "#fbbf24",
    };

    // Responsive layout constants
    const MW = isMobile ? 110 : 200;
    const MH = isMobile ? 44 : 60;
    const PX = isMobile ? 16 : 50;
    const PY = isMobile ? 12 : 22;
    const TOP_PAD = isMobile ? 14 : 24;
    const FONT_TEAM = isMobile ? 9 : 12;
    const FONT_LABEL = isMobile ? 7 : 9;
    const FONT_SECTION = isMobile ? 10 : 14;
    const TRUNC_LEN = isMobile ? 10 : 18;

    /* ═══════════ DATA LOAD ═══════════ */
    useEffect(() => {
        axios.get(`${API}/api/finals/visible`).then(r => {
            setVisible(!!r.data?.visible);
            setModVisible(!!r.data?.mod_visible);
        }).catch(() => setVisible(false));
        axios.get(`${API}/api/me`, { withCredentials: true }).then(r => { setMe(r.data); setMeLoaded(true); }).catch(() => { setMe(null); setMeLoaded(true); });
    }, [API]);

    useEffect(() => {
        if (me?.is_mod) {
            axios.get(`${API}/api/teams`, { withCredentials: true })
                .then(r => setAllTeams(Array.isArray(r.data) ? r.data : []))
                .catch(() => { });
        }
    }, [me, API]);

    const sortRound = (r) => Array.isArray(r) ? [...r].sort((a, b) => (a.bracket_slot || 0) - (b.bracket_slot || 0)) : [];

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

    useEffect(() => { load(); }, [API]);

    /* ═══════════ DERIVED ═══════════ */
    const trimLosersRounds = (wbR, lbR) => {
        const wc = wbR.length;
        if (!wc) return lbR;
        const want = Math.max(0, wc * 2 - 2);
        return lbR.slice(0, Math.min(lbR.length, want));
    };

    const wb = useMemo(() => (wbRaw || []).map(r => (r || []).map((m, i) => ({ ...m, __slot: i }))), [wbRaw]);
    const lb = useMemo(() => {
        const trimmed = trimLosersRounds(wbRaw || [], lbRaw || []);
        return (trimmed || []).map(r => (r || []).map((m, i) => ({ ...m, __slot: i })));
    }, [wbRaw, lbRaw]);

    /* ═══════════ SVG LAYOUT ═══════════ */
    const layout = useMemo(() => {
        const positions = new Map();
        const connections = [];

        let matchNum = 1; // global match counter

        const wbMaxCount = Math.max(1, ...wb.map(r => r.length));
        const wbTotalH = wbMaxCount * (MH + PY) - PY;

        wb.forEach((round, ri) => {
            const count = round.length;
            const spacing = MH + PY;
            const totalH = count * spacing - PY;
            const startY = TOP_PAD + 30 + (wbTotalH - totalH) / 2;

            round.forEach((m, i) => {
                positions.set(m.id, {
                    x: ri * (MW + PX),
                    y: startY + i * spacing,
                    m,
                    lane: "wb",
                    matchNum: matchNum++,
                });
            });
        });

        const lbTop = TOP_PAD + 30 + wbTotalH + 50;
        const lbMaxCount = Math.max(1, ...lb.map(r => r.length));
        const lbTotalH = lbMaxCount * (MH + PY) - PY;

        lb.forEach((round, ri) => {
            const count = round.length;
            const spacing = MH + PY;
            const totalH = count * spacing - PY;
            const startY = lbTop + 30 + (lbTotalH - totalH) / 2;

            round.forEach((m, i) => {
                positions.set(m.id, {
                    x: ri * (MW + PX),
                    y: startY + i * spacing,
                    m,
                    lane: "lb",
                    matchNum: matchNum++,
                });
            });
        });

        const roundsMax = Math.max(wb.length, lb.length);
        const gfX = roundsMax * (MW + PX);
        const midY = (TOP_PAD + 30 + wbTotalH / 2 + lbTop + 30 + lbTotalH / 2) / 2 - MH / 2;

        if (gf?.[0]) {
            positions.set(gf[0].id, {
                x: gfX,
                y: midY - (MH / 2 + PY / 2),
                m: gf[0],
                lane: "gf",
                matchNum: matchNum++,
            });
        }
        if (gf?.[1]) {
            positions.set(gf[1].id, {
                x: gfX,
                y: midY + (MH / 2 + PY / 2),
                m: gf[1],
                lane: "gf",
                matchNum: matchNum++,
            });
        }

        const allRounds = [...wb, ...lb];
        allRounds.forEach(round => {
            round.forEach(m => {
                if (!m?.next_match_id) return;
                const from = positions.get(m.id);
                const to = positions.get(m.next_match_id);
                if (!from || !to) return;

                const x1 = from.x + MW;
                const y1 = from.y + MH / 2;
                const x2 = to.x;
                const y2 = to.y + MH / 2;
                const mx = (x1 + x2) / 2;

                connections.push({
                    path: `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`,
                    lane: from.lane,
                });
            });
        });

        let maxX = 0, maxY = 0;
        for (const p of positions.values()) {
            if (p.x + MW > maxX) maxX = p.x + MW;
            if (p.y + MH > maxY) maxY = p.y + MH;
        }

        return {
            positions,
            connections,
            width: maxX + 40,
            height: maxY + 40,
            wbLabelY: TOP_PAD,
            lbLabelY: lbTop,
            gfLabelX: gfX,
            gfLabelY: midY - 20,
        };
    }, [wb, lb, gf]);

    /* ═══════════ MODAL ACTIONS ═══════════ */
    const openModal = (m) => {
        if (!me?.is_mod) return;
        setSelectedMatch(m);
        setModalTeamA(m.team_a_id ? String(m.team_a_id) : "");
        setModalTeamB(m.team_b_id ? String(m.team_b_id) : "");
        setModalWinner(m.winner_id ? String(m.winner_id) : "");
    };

    const closeModal = () => {
        setSelectedMatch(null);
        setModalTeamA("");
        setModalTeamB("");
        setModalWinner("");
    };

    const saveMatch = async () => {
        if (!selectedMatch) return;
        setSaving(true);
        try {
            const mid = selectedMatch.id;
            // Assign teams if changed
            const oldA = selectedMatch.team_a_id ? String(selectedMatch.team_a_id) : "";
            const oldB = selectedMatch.team_b_id ? String(selectedMatch.team_b_id) : "";
            if (modalTeamA !== oldA) {
                await axios.post(`${API}/api/mod/finals/assign-slot`, {
                    match_id: mid,
                    slot: "team_a",
                    team_id: modalTeamA ? Number(modalTeamA) : 0,
                }, { withCredentials: true });
            }
            if (modalTeamB !== oldB) {
                await axios.post(`${API}/api/mod/finals/assign-slot`, {
                    match_id: mid,
                    slot: "team_b",
                    team_id: modalTeamB ? Number(modalTeamB) : 0,
                }, { withCredentials: true });
            }
            // Set winner if changed
            const oldW = selectedMatch.winner_id ? String(selectedMatch.winner_id) : "";
            if (modalWinner !== oldW) {
                await axios.post(`${API}/api/mod/finals/set-winner`, {
                    match_id: mid,
                    winner: modalWinner ? Number(modalWinner) : 0,
                }, { withCredentials: true });
            }
            await load();
            closeModal();
        } finally {
            setSaving(false);
        }
    };

    const clearMatch = async () => {
        if (!selectedMatch) return;
        setSaving(true);
        try {
            const mid = selectedMatch.id;
            await axios.post(`${API}/api/mod/finals/assign-slot`, {
                match_id: mid, slot: "team_a", team_id: 0,
            }, { withCredentials: true });
            await axios.post(`${API}/api/mod/finals/assign-slot`, {
                match_id: mid, slot: "team_b", team_id: 0,
            }, { withCredentials: true });
            await axios.post(`${API}/api/mod/finals/set-winner`, {
                match_id: mid, winner: 0,
            }, { withCredentials: true });
            await load();
            closeModal();
        } finally {
            setSaving(false);
        }
    };

    /* ═══════════ RENDER ═══════════ */
    if (loading || !meLoaded)
        return (
            <div className="d-flex justify-content-center align-items-center" style={{ minHeight: 300 }}>
                <div className="spinner-border text-info" />
            </div>
        );

    if (!visible && !(me?.is_mod && modVisible))
        return <p className="text-secondary text-center mt-5">Finals bracket is not available yet.</p>;

    const escSvg = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    const trunc = (s, max) => s.length > max ? s.substring(0, max - 1) + "…" : s;

    const renderMatchBox = (pos) => {
        const m = pos.m;
        const teamA = m.team_a || "TBD";
        const teamB = m.team_b || "TBD";
        const aWin = m.winner_id && m.winner_id === m.team_a_id;
        const bWin = m.winner_id && m.winner_id === m.team_b_id;
        const hasData = m.team_a || m.team_b;

        const isGf = pos.lane === "gf";
        const borderColor = isGf ? COL.gfAccent : pos.lane === "lb" ? COL.lbAccent : COL.wbAccent;
        const dimBorder = isGf ? COL.gfDim : pos.lane === "lb" ? COL.lbDim : COL.wbDim;
        const border = hasData ? borderColor : dimBorder;
        const bg = hasData ? COL.boxBg : COL.boxBgDim;
        const strokeW = isGf ? 2 : 1.5;

        const aColor = aWin ? COL.winText : (m.winner_id && !aWin && m.team_a_id) ? COL.loseText : m.team_a ? COL.teamText : COL.tbdText;
        const bColor = bWin ? COL.winText : (m.winner_id && !bWin && m.team_b_id) ? COL.loseText : m.team_b ? COL.teamText : COL.tbdText;
        const aWeight = aWin ? "bold" : "normal";
        const bWeight = bWin ? "bold" : "normal";
        const aIcon = aWin ? "✓ " : (m.winner_id && !aWin && m.team_a_id) ? "✗ " : "";
        const bIcon = bWin ? "✓ " : (m.winner_id && !bWin && m.team_b_id) ? "✗ " : "";

        const labelColor = isGf ? COL.labelGf : pos.lane === "lb" ? COL.labelLb : COL.labelWb;
        const label = isGf
            ? (gf?.[1] && m.id === gf[1].id ? "GF Reset" : "Grand Final")
            : `Match ${pos.matchNum}`;

        return (
            <g
                key={`box-${m.id}`}
                onClick={() => openModal(m)}
                style={{ cursor: me?.is_mod ? "pointer" : "default" }}
            >
                {/* Glow for GF */}
                {isGf && hasData && (
                    <rect
                        x={pos.x - 2} y={pos.y - 2} width={MW + 4} height={MH + 4}
                        rx="8" fill="none" stroke={COL.gfAccent} strokeWidth="1" opacity="0.3"
                        filter="url(#glow)"
                    />
                )}
                {/* Box */}
                <rect
                    x={pos.x} y={pos.y} width={MW} height={MH}
                    rx="6" fill={bg} stroke={border} strokeWidth={strokeW}
                />
                {/* Label above */}
                {label && (
                    <text
                        x={pos.x + MW / 2} y={pos.y - (isMobile ? 3 : 5)}
                        textAnchor="middle" fill={labelColor}
                        fontSize={FONT_LABEL} fontFamily="'Inter', sans-serif" opacity="0.8"
                    >
                        {label}
                    </text>
                )}
                {/* Team A */}
                <text
                    x={pos.x + (isMobile ? 6 : 10)} y={pos.y + MH * 0.38}
                    fill={aColor} fontSize={FONT_TEAM} fontWeight={aWeight}
                    fontFamily="'Inter', sans-serif"
                >
                    {aIcon}{escSvg(trunc(teamA, TRUNC_LEN))}
                </text>
                {/* Divider */}
                <line
                    x1={pos.x + 6} y1={pos.y + MH * 0.5}
                    x2={pos.x + MW - 6} y2={pos.y + MH * 0.5}
                    stroke="rgba(255,255,255,0.08)" strokeWidth="1"
                />
                {/* Team B */}
                <text
                    x={pos.x + (isMobile ? 6 : 10)} y={pos.y + MH * 0.78}
                    fill={bColor} fontSize={FONT_TEAM} fontWeight={bWeight}
                    fontFamily="'Inter', sans-serif"
                >
                    {bIcon}{escSvg(trunc(teamB, TRUNC_LEN))}
                </text>
                {/* Mod clickable indicator */}
                {me?.is_mod && (
                    <text
                        x={pos.x + MW - 8} y={pos.y + 12}
                        textAnchor="end" fill="rgba(255,255,255,0.3)"
                        fontSize="8" fontFamily="'Inter', sans-serif"
                    >
                        ✎
                    </text>
                )}
            </g>
        );
    };

    /* ═══════════ MODAL ═══════════ */
    const renderModal = () => {
        if (!selectedMatch) return null;
        const m = selectedMatch;
        const lane = (() => {
            for (const pos of layout.positions.values()) {
                if (pos.m.id === m.id) return pos.lane;
            }
            return "wb";
        })();
        const laneLabel = lane === "gf" ? "Grand Final" : lane === "lb" ? "Losers Bracket" : "Winners Bracket";
        const accentColor = lane === "gf" ? COL.gfAccent : lane === "lb" ? COL.lbAccent : COL.wbAccent;

        // Build winner options based on currently selected teams
        const teamAName = allTeams.find(t => String(t.id) === modalTeamA)?.name;
        const teamBName = allTeams.find(t => String(t.id) === modalTeamB)?.name;

        return (
            <div
                onClick={closeModal}
                style={{
                    position: "fixed",
                    inset: 0,
                    background: "rgba(0,0,0,0.7)",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    zIndex: 9999,
                }}
            >
                <div
                    onClick={(e) => e.stopPropagation()}
                    style={{
                        background: "#1a1d23",
                        border: `1px solid ${accentColor}`,
                        borderRadius: 12,
                        padding: 24,
                        minWidth: 340,
                        maxWidth: 420,
                        boxShadow: `0 0 30px ${accentColor}33`,
                    }}
                >
                    <h5 style={{ color: accentColor, marginBottom: 4 }}>
                        Edit Match {layout.positions.get(m.id)?.matchNum || m.id}
                    </h5>
                    <p style={{ color: "#8b99a8", fontSize: 13, marginBottom: 16 }}>
                        {laneLabel}
                    </p>

                    {/* Team A */}
                    <label style={{ color: "#ccc", fontSize: 12, marginBottom: 4, display: "block" }}>Team A</label>
                    <select
                        className="form-select form-select-sm bg-dark text-light mb-3"
                        value={modalTeamA}
                        onChange={(e) => setModalTeamA(e.target.value)}
                    >
                        <option value="">— Empty —</option>
                        {allTeams.map(t => (
                            <option key={t.id} value={String(t.id)}>{t.name}</option>
                        ))}
                    </select>

                    {/* Team B */}
                    <label style={{ color: "#ccc", fontSize: 12, marginBottom: 4, display: "block" }}>Team B</label>
                    <select
                        className="form-select form-select-sm bg-dark text-light mb-3"
                        value={modalTeamB}
                        onChange={(e) => setModalTeamB(e.target.value)}
                    >
                        <option value="">— Empty —</option>
                        {allTeams.map(t => (
                            <option key={t.id} value={String(t.id)}>{t.name}</option>
                        ))}
                    </select>

                    {/* Winner */}
                    <label style={{ color: "#ccc", fontSize: 12, marginBottom: 4, display: "block" }}>Winner</label>
                    <select
                        className="form-select form-select-sm bg-dark text-light mb-3"
                        value={modalWinner}
                        onChange={(e) => setModalWinner(e.target.value)}
                    >
                        <option value="">— No Winner —</option>
                        {modalTeamA && teamAName && <option value={modalTeamA}>{teamAName}</option>}
                        {modalTeamB && teamBName && <option value={modalTeamB}>{teamBName}</option>}
                    </select>

                    {/* Buttons */}
                    <div style={{ display: "flex", gap: 8, marginTop: 16 }}>
                        <button
                            className="btn btn-success btn-sm"
                            onClick={saveMatch}
                            disabled={saving}
                        >
                            {saving ? "Saving…" : "Save"}
                        </button>
                        <button
                            className="btn btn-outline-danger btn-sm"
                            onClick={clearMatch}
                            disabled={saving}
                        >
                            Clear
                        </button>
                        <button
                            className="btn btn-outline-secondary btn-sm ms-auto"
                            onClick={closeModal}
                        >
                            Close
                        </button>
                    </div>
                </div>
            </div>
        );
    };

    return (
        <div style={{ width: "100%", padding: isMobile ? "8px 0" : "16px 0", maxWidth: "100vw", overflowX: "hidden" }}>
            {/* Page header */}
            <div
                style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: isMobile ? "0 10px 10px" : "0 16px 16px",
                    maxWidth: 1200,
                    margin: "0 auto",
                }}
            >
                <h2 style={{ fontWeight: 900, fontSize: isMobile ? 16 : 22, color: "#e8ecf0", margin: 0 }}>
                    <span style={{ marginRight: 6 }}><E n="trophy" className="emoji-gold" /></span>Finals Bracket
                </h2>
                <button
                    onClick={load}
                    style={{
                        borderRadius: 8,
                        padding: isMobile ? "4px 10px" : "6px 14px",
                        fontWeight: 700,
                        fontSize: isMobile ? 11 : 13,
                        border: "1px solid rgba(255,255,255,0.15)",
                        background: "rgba(59, 110, 165, 0.15)",
                        color: "#6fa8dc",
                        cursor: "pointer",
                    }}
                >
                    ↻ Refresh
                </button>
            </div>

            {/* Legend */}
            <div
                style={{
                    display: "flex",
                    gap: isMobile ? 10 : 20,
                    justifyContent: "center",
                    marginBottom: isMobile ? 8 : 16,
                    flexWrap: "wrap",
                    fontSize: isMobile ? 11 : 13,
                    color: "#8b99a8",
                    padding: isMobile ? "0 8px" : 0,
                }}
            >
                <span style={{ display: "flex", alignItems: "center", gap: 6 }}>
                    <span style={{ width: 12, height: 12, borderRadius: 2, background: COL.wbAccent, display: "inline-block" }} />
                    Winners
                </span>
                <span style={{ display: "flex", alignItems: "center", gap: 6 }}>
                    <span style={{ width: 12, height: 12, borderRadius: 2, background: COL.lbAccent, display: "inline-block" }} />
                    Losers
                </span>
                <span style={{ display: "flex", alignItems: "center", gap: 6 }}>
                    <span style={{ width: 12, height: 12, borderRadius: 2, background: COL.gfAccent, display: "inline-block" }} />
                    Grand Final
                </span>
                <span style={{ display: "flex", alignItems: "center", gap: 6 }}>
                    <span style={{ color: COL.winText, fontWeight: 700 }}>✓</span> Winner
                </span>
                <span style={{ display: "flex", alignItems: "center", gap: 6 }}>
                    <span style={{ color: COL.loseText, fontWeight: 700 }}>✗</span> Eliminated
                </span>
            </div>

            {me?.is_mod && (
                <p style={{ textAlign: "center", color: "#6b7280", fontSize: 12, marginBottom: 12 }}>
                    Click any match to assign teams or set a winner
                </p>
            )}

            {/* SVG Bracket */}
            <div
                ref={containerRef}
                style={{
                    overflowX: isMobile ? "hidden" : "auto",
                    width: "100%",
                    padding: isMobile ? "0 4px 12px" : "0 16px 16px",
                }}
            >
                <svg
                    viewBox={`0 0 ${layout.width} ${layout.height}`}
                    width="100%"
                    preserveAspectRatio="xMidYMin meet"
                    style={{ display: "block" }}
                >
                    <defs>
                        <filter id="glow">
                            <feGaussianBlur stdDeviation="3" result="blur" />
                            <feMerge>
                                <feMergeNode in="blur" />
                                <feMergeNode in="SourceGraphic" />
                            </feMerge>
                        </filter>
                    </defs>

                    {/* Connector lines */}
                    {layout.connections.map((c, i) => (
                        <path
                            key={`conn-${i}`}
                            d={c.path}
                            fill="none"
                            stroke={c.lane === "lb" ? COL.connLb : c.lane === "gf" ? COL.connGf : COL.connWb}
                            strokeWidth="2"
                        />
                    ))}

                    {/* Section labels */}
                    {wb.length > 0 && (
                        <text
                            x={0} y={layout.wbLabelY + 14}
                            fill={COL.labelWb} fontSize={FONT_SECTION} fontWeight="bold"
                            fontFamily="'Inter', sans-serif" opacity="0.85"
                        >
                            WINNERS BRACKET
                        </text>
                    )}
                    {lb.length > 0 && (
                        <text
                            x={0} y={layout.lbLabelY + 14}
                            fill={COL.labelLb} fontSize={FONT_SECTION} fontWeight="bold"
                            fontFamily="'Inter', sans-serif" opacity="0.85"
                        >
                            LOSERS BRACKET
                        </text>
                    )}
                    {gf?.length > 0 && (
                        <text
                            x={layout.gfLabelX} y={layout.gfLabelY + 10}
                            fill={COL.labelGf} fontSize={FONT_SECTION} fontWeight="bold"
                            fontFamily="'Inter', sans-serif" opacity="0.85"
                        >
                            GRAND FINALS
                        </text>
                    )}

                    {/* Match boxes */}
                    {Array.from(layout.positions.values()).map(renderMatchBox)}
                </svg>
            </div>

            {/* Edit Modal */}
            {renderModal()}
        </div>
    );
}
