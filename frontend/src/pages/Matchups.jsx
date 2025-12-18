import { useEffect, useState } from "react";
import axios from "axios";
import { Link } from "react-router-dom";

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
            .get(`${import.meta.env.VITE_API_URL}/api/matches/public`)
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
        <div className="container text-light" style={{ maxWidth: 800 }}>
            <h2 className="mb-3">📅 Matchups</h2>

            {loading && <p>⏳ Loading matchups...</p>}
            {error && <p className="text-danger">⚠️ {error}</p>}

            {/* Filters */}
            {!loading && !error && (
                <div className="d-flex flex-wrap gap-2 mb-3">
                    <select
                        className="form-select bg-dark text-light"
                        style={{ width: "auto" }}
                        value={selectedSeason}
                        onChange={(e) => {
                            setSelectedSeason(e.target.value);
                            setSelectedWeek("All");
                        }}
                    >
                        {seasonOptions.map(s => (
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
                        className="form-select bg-dark text-light"
                        style={{ width: "auto" }}
                        value={selectedWeek}
                        onChange={(e) => setSelectedWeek(e.target.value)}
                    >
                        {weekOptions.map(w => (
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
                <p className="text-light">No matches found.</p>
            )}

            {!loading && !error && sortedSeasons.map(season => {
                const weeks = grouped[season];
                const sortedWeeks = Object.keys(weeks).sort((a, b) => {
                    if (a === "Finals") return -1;
                    if (b === "Finals") return 1;
                    return Number(b) - Number(a);
                });

                return sortedWeeks.map(week => (
                    <div key={`${season}-${week}`} className="mb-4">
                        <h5 className="text-info border-bottom pb-1 mb-2">
                            {season === "Preseason"
                                ? "Preseason"
                                : `Season ${season}`} —{" "}
                            {week === "Finals" ? "Finals 🏁" : `Week ${week}`}
                        </h5>

                        {weeks[week].map(m => {
                            const winnerA = m.winner_id === m.team_a_id;
                            const winnerB = m.winner_id === m.team_b_id;

                            return (
                                <Link
                                    to={`/match/${m.id}`}
                                    key={m.id}
                                    className="text-decoration-none text-light"
                                >
                                    <div
                                        className="p-2 mb-2 rounded"
                                        style={{
                                            background: "#181a1b",
                                            border: "1px solid #2a2d2f",
                                        }}
                                    >
                                        <div className="d-flex justify-content-between">
                                            <div className="d-flex align-items-center gap-2">
                                                <span className={`fw-bold ${winnerA ? "text-success" : ""}`}>
                                                    {m.team_a} {winnerA && "🏆"}
                                                </span>

                                                <span>vs</span>

                                                <span className={`fw-bold ${winnerB ? "text-success" : ""}`}>
                                                    {m.team_b} {winnerB && "🏆"}
                                                </span>

                                                {m.cast_active && (
                                                    <span
                                                        className="ms-2"
                                                        title="This match was casted"
                                                        style={{ color: "#4ea1ff" }}
                                                    >
                                                        🔴 Casted
                                                    </span>
                                                )}
                                            </div>

                                            <div className="small">
                                                {m.scheduled_date
                                                    ? new Date(m.scheduled_date).toLocaleString()
                                                    : "TBD"}
                                            </div>
                                        </div>

                                        <div className="small text-secondary">
                                            Match ID: <b>{m.match_code}</b> | Status:{" "}
                                            <span className={
                                                m.status === "Completed"
                                                    ? "text-success"
                                                    : m.status === "Scheduled"
                                                        ? "text-warning"
                                                        : "text-danger"
                                            }>
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
    );
}
