import { useEffect, useState } from "react";
import axios from "axios";
import { Link } from "react-router-dom";

export default function Matchups() {
    const [matches, setMatches] = useState({});
    const [selectedSeason, setSelectedSeason] = useState("All");
    const [selectedWeek, setSelectedWeek] = useState("All");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);

    // --- Fetch all matchups ---
    useEffect(() => {
        setLoading(true);
        axios
            .get(`${import.meta.env.VITE_API_URL}/api/matches/public`)
            .then((res) => {
                if (res.data.success) setMatches(res.data.matches);
                else throw new Error("Invalid response");
            })
            .catch((err) => setError(err.message))
            .finally(() => setLoading(false));
    }, []);

    // --- Build season + week options dynamically and reactively ---
    const [availableWeeks, setAvailableWeeks] = useState(["All"]);

    useEffect(() => {
        if (!matches) return;

        const seasons = Object.keys(matches);
        const currentWeeks =
            selectedSeason !== "All" && matches[selectedSeason]
                ? ["All", ...Object.keys(matches[selectedSeason])]
                : ["All"];

        setAvailableWeeks(currentWeeks);
    }, [matches, selectedSeason]);

    const allSeasons = ["All", ...Object.keys(matches || {})];

    // --------------------------------------
    // 🔥 SORT SEASONS AND WEEKS (DESC)
    // --------------------------------------
    const sortedSeasons = Object.keys(matches || {}).sort((a, b) => {
        const A = a === "Preseason" ? 0 : Number(a);
        const B = b === "Preseason" ? 0 : Number(b);
        return B - A; // newest → oldest
    });

    const sortedFiltered = [];

    sortedSeasons.forEach((season) => {
        if (selectedSeason !== "All" && selectedSeason !== season) return;

        const weeks = matches[season] || {};

        const sortedWeeks = Object.keys(weeks).sort(
            (a, b) => Number(b) - Number(a) // newest → oldest
        );

        sortedWeeks.forEach((week) => {
            if (selectedWeek !== "All" && selectedWeek !== week) return;

            sortedFiltered.push({ season, week, list: weeks[week] });
        });
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
                        {allSeasons.map((s) => (
                            <option key={s} value={s}>
                                {s === "Preseason"
                                    ? "Preseason"
                                    : s === "All"
                                        ? "All Seasons"
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
                        {availableWeeks.map((w) => (
                            <option key={w} value={w}>
                                {w === "All" ? "All Weeks" : `Week ${w}`}
                            </option>
                        ))}
                    </select>
                </div>
            )}

            {/* Results */}
            {!loading && !error && sortedFiltered.length === 0 && (
                <p className="text-light">No matches found for the selected filters.</p>
            )}

            {!loading &&
                !error &&
                sortedFiltered.map(({ season, week, list }) => (
                    <div key={`${season}-${week}`} className="mb-4">
                        <h5 className="text-info border-bottom pb-1 mb-2">
                            {season === "Preseason" ? "Preseason" : `Season ${season}`} — Week {week}
                        </h5>

                        {list.map((m) => {
                            const winnerId = Number(m.winner_id);
                            const teamAId = Number(m.team_a_id);
                            const teamBId = Number(m.team_b_id);

                            let winner = null;
                            if (winnerId === teamAId) winner = "A";
                            else if (winnerId === teamBId) winner = "B";

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
                                            cursor: "pointer",
                                        }}
                                    >
                                        <div className="d-flex justify-content-between">
                                            <div>
                                                <span
                                                    className={`fw-bold ${winner === "A"
                                                        ? "text-success"
                                                        : winner === "B"
                                                            ? "text-danger"
                                                            : "text-light"
                                                        }`}
                                                >
                                                    {m.team_a} {winner === "A" && "🏆"}
                                                </span>{" "}
                                                vs{" "}
                                                <span
                                                    className={`fw-bold ${winner === "B"
                                                        ? "text-success"
                                                        : winner === "A"
                                                            ? "text-danger"
                                                            : "text-light"
                                                        }`}
                                                >
                                                    {m.team_b} {winner === "B" && "🏆"}
                                                </span>
                                            </div>

                                            <div className="text-end small text-light">
                                                {m.scheduled_date
                                                    ? new Date(m.scheduled_date).toLocaleString([], {
                                                        dateStyle: "short",
                                                        timeStyle: "short",
                                                    })
                                                    : "TBD"}
                                            </div>
                                        </div>

                                        <div className="small text-secondary">
                                            Match ID: <b>{m.match_code}</b> | Status:{" "}
                                            <span
                                                className={
                                                    ["Completed", "Finished"].includes(m.status)
                                                        ? "text-success"
                                                        : m.status === "Scheduled"
                                                            ? "text-warning"
                                                            : "text-light"
                                                }
                                            >
                                                {m.status}
                                            </span>
                                        </div>
                                    </div>
                                </Link>
                            );
                        })}
                    </div>
                ))}
        </div>
    );
}
