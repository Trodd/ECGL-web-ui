import { useEffect, useState } from "react";
import axios from "axios";

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

    // --- Flatten filtered matches ---
    const filtered = [];
    Object.entries(matches || {}).forEach(([season, weeks]) => {
        if (selectedSeason !== "All" && selectedSeason !== season) return;
        Object.entries(weeks).forEach(([week, list]) => {
            if (selectedWeek !== "All" && selectedWeek !== week) return;
            filtered.push({ season, week, list });
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
            {!loading && !error && filtered.length === 0 && (
                <p className="text-light">No matches found for the selected filters.</p>
            )}

            {!loading &&
                !error &&
                filtered.map(({ season, week, list }) => (
                    <div key={`${season}-${week}`} className="mb-4">
                        <h5 className="text-info border-bottom pb-1 mb-2">
                            {season === "Preseason" ? "Preseason" : `Season ${season}`} — Week {week}
                        </h5>

                        {list.map((m) => {
                            const winner =
                                m.winner_id === null
                                    ? null
                                    : m.winner_id === m.team_a_id
                                        ? "A"
                                        : "B";
                            return (
                                <div
                                    key={m.id}
                                    className="p-2 mb-2 rounded"
                                    style={{
                                        background: "#181a1b", // darker card tone
                                        border: "1px solid #2a2d2f",
                                        transition: "background 0.2s, border-color 0.2s",
                                    }}
                                    onMouseEnter={(e) => {
                                        e.currentTarget.style.background = "#202325";
                                        e.currentTarget.style.borderColor = "#3a3f42";
                                    }}
                                    onMouseLeave={(e) => {
                                        e.currentTarget.style.background = "#181a1b";
                                        e.currentTarget.style.borderColor = "#2a2d2f";
                                    }}
                                >

                                    <div className="d-flex justify-content-between align-items-center">
                                        <div>
                                            <span
                                                className={
                                                    winner === "A"
                                                        ? "text-success fw-bold"
                                                        : winner === "B"
                                                            ? "text-danger fw-bold"
                                                            : "text-light"
                                                }
                                            >
                                                {m.team_a}
                                            </span>{" "}
                                            vs{" "}
                                            <span
                                                className={
                                                    winner === "B"
                                                        ? "text-success fw-bold"
                                                        : winner === "A"
                                                            ? "text-danger fw-bold"
                                                            : "text-light"
                                                }
                                            >
                                                {m.team_b}
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
                                                m.status === "Completed"
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
                            );
                        })}
                    </div>
                ))}
        </div>
    );
}
