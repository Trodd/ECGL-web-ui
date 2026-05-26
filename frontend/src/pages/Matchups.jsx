import { useEffect, useState } from "react";
import axios from "axios";
import { Link } from "react-router-dom";
import { getApiUrl } from "../config";
import { E } from "../components/CustomEmoji";

export default function Matchups() {
    const [flatMatches, setFlatMatches] = useState([]);
    const [selectedSeason, setSelectedSeason] = useState("All");
    const [selectedWeek, setSelectedWeek] = useState("All");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);

    // -----------------------------
    // Fetch + FLATTEN (source of truth)
    // -----------------------------
    useEffect(() => {
        setLoading(true);
        axios
            .get(`${getApiUrl()}/api/matches/public`)
            .then((res) => {
                if (!res.data.success) throw new Error("Invalid response");

                const raw = res.data.matches || {};
                const flat = [];

                Object.entries(raw).forEach(([seasonKey, weeksObj]) => {
                    Object.entries(weeksObj).forEach(([weekKey, list]) => {
                        list.forEach(m => {
                            flat.push({
                                ...m,
                                season: seasonKey,
                                week: weekKey,
                            });
                        });
                    });
                });

                setFlatMatches(flat);
            })
            .catch((err) => setError(err.message))
            .finally(() => setLoading(false));
    }, []);

    // -----------------------------
    // SEASON FILTER OPTIONS
    // -----------------------------
    const seasonOptions = [
        "All",
        ...Array.from(new Set(flatMatches.map(m => m.season))).sort((a, b) => {
            if (a === "Preseason") return 1;
            if (b === "Preseason") return -1;
            return Number(b) - Number(a);
        }),
    ];

    // -----------------------------
    // WEEK FILTER OPTIONS
    // -----------------------------
    const weekOptions = [
        "All",
        ...Array.from(
            new Set(
                flatMatches
                    .filter(m => selectedSeason === "All" || m.season === selectedSeason)
                    .map(m => m.week)
            )
        ).sort((a, b) => {
            if (a === "Finals") return -1;
            if (b === "Finals") return 1;
            return Number(b) - Number(a);
        }),
    ];

    // -----------------------------
    // APPLY FILTERS
    // -----------------------------
    const filtered = flatMatches.filter(m =>
        (selectedSeason === "All" || m.season === selectedSeason) &&
        (selectedWeek === "All" || m.week === selectedWeek)
    );

    // -----------------------------
    // GROUP FOR DISPLAY
    // -----------------------------
    const grouped = {};
    filtered.forEach(m => {
        grouped[m.season] ??= {};
        grouped[m.season][m.week] ??= [];
        grouped[m.season][m.week].push(m);
    });

    // -----------------------------
    // SORT SEASONS
    // -----------------------------
    const sortedSeasons = Object.keys(grouped).sort((a, b) => {
        if (a === "Preseason") return 1;
        if (b === "Preseason") return -1;
        return Number(b) - Number(a);
    });

    return (
        <div className="d-flex justify-content-center">
            <div style={{ width: "100%", maxWidth: 820 }}>
                <div className="card bg-dark border-secondary p-4 shadow-sm">
                    <h2 className="mb-3"><E n="calendar" /> Matchups</h2>

                    {loading && <p className="text-secondary"><E n="refresh" /> Loading matchups…</p>}
                    {error && <p className="text-danger"><E n="warning" className="emoji-warning" /> {error}</p>}

                    {/* ================= FILTER BAR ================= */}
                    {!loading && !error && (
                        <div className="d-flex flex-wrap gap-2 mb-4">
                            <select
                                className="form-select form-select-sm bg-dark text-light"
                                style={{ width: 160 }}
                                value={selectedSeason}
                                onChange={(e) => {
                                    setSelectedSeason(e.target.value);
                                    setSelectedWeek("All");
                                }}
                            >
                                {seasonOptions.map((s) => (
                                    <option key={s} value={s}>
                                        {s === "All"
                                            ? "All Seasons"
                                            : s === "Preseason"
                                                ? "Preseason"
                                                : `Season ${s}`}
                                    </option>
                                ))}
                            </select>

                            <select
                                className="form-select form-select-sm bg-dark text-light"
                                style={{ width: 140 }}
                                value={selectedWeek}
                                onChange={(e) => setSelectedWeek(e.target.value)}
                            >
                                {weekOptions.map((w) => (
                                    <option key={w} value={w}>
                                        {w === "All"
                                            ? "All Weeks"
                                            : w === "Finals"
                                                ? "Finals 🏁"
                                                : `Week ${w}`}
                                    </option>
                                ))}
                            </select>
                        </div>
                    )}

                    {!loading && !error && filtered.length === 0 && (
                        <p className="text-secondary">No matches found.</p>
                    )}

                    {/* ================= MATCHUPS ================= */}
                    {!loading &&
                        !error &&
                        sortedSeasons.map((season) => {
                            const weeks = grouped[season];
                            const sortedWeeks = Object.keys(weeks).sort((a, b) => {
                                if (a === "Finals") return -1;
                                if (b === "Finals") return 1;
                                return Number(b) - Number(a);
                            });

                            return sortedWeeks.map((week) => (
                                <div key={`${season}-${week}`} className="mb-4">
                                    {/* Season / Week Header */}
                                    <h5 className="text-info border-bottom pb-1 mb-3">
                                        {season === "Preseason"
                                            ? "Preseason"
                                            : `Season ${season}`}{" "}
                                        — {week === "Finals" ? <><E n="flag" /> Finals</> : `Week ${week}`}
                                    </h5>

                                    {weeks[week].map((m) => {
                                        const winnerA = m.winner_id === m.team_a_id;
                                        const winnerB = m.winner_id === m.team_b_id;

                                        return (
                                            <Link
                                                to={`/match/${m.id}`}
                                                key={m.id}
                                                className="text-decoration-none"
                                            >
                                                <div
                                                    className="border rounded p-3 mb-2 bg-dark shadow-sm"
                                                    style={{ borderColor: "#3a3a3a" }}
                                                >
                                                    {/* Teams */}
                                                    <div className="d-flex justify-content-between align-items-center mb-1">
                                                        <div className="fw-semibold">
                                                            <span className={winnerA ? "text-success" : ""}>
                                                                {m.team_a} {winnerA && <E n="trophy" className="emoji-gold" />}
                                                            </span>{" "}
                                                            <span className="text-secondary mx-1">vs</span>
                                                            <span className={winnerB ? "text-success" : ""}>
                                                                {m.team_b} {winnerB && <E n="trophy" className="emoji-gold" />}
                                                            </span>

                                                            {m.cast_active && (
                                                                <span
                                                                    className="badge bg-danger ms-2"
                                                                    title="Live / Casted Match"
                                                                >
                                                                    LIVE
                                                                </span>
                                                            )}
                                                        </div>

                                                        {/* Date */}
                                                        <span className="text-secondary small">
                                                            {m.scheduled_date
                                                                ? new Date(
                                                                    m.scheduled_date
                                                                ).toLocaleString([], {
                                                                    dateStyle: "medium",
                                                                    timeStyle: "short",
                                                                })
                                                                : "TBD"}
                                                        </span>
                                                    </div>

                                                    {/* Meta */}
                                                    <div className="small text-secondary">
                                                        <span className="me-2">
                                                            Match ID: <b>{m.match_code}</b>
                                                        </span>
                                                        <span
                                                            className={`badge ${m.status === "Completed"
                                                                ? "bg-success"
                                                                : m.status === "Scheduled"
                                                                    ? "bg-warning text-dark"
                                                                    : "bg-secondary"
                                                                }`}
                                                        >
                                                            {m.status}
                                                        </span>
                                                    </div>
                                                </div>
                                            </Link>
                                        );
                                    })}
                                </div>
                            ));
                        })}
                </div>
            </div>
        </div>
    );
}
