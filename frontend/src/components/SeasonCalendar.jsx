import { useEffect, useState } from "react";
import axios from "axios";
import "./calendar.css";

export default function FullSeasonCalendar() {
    const [data, setData] = useState(null);

    useEffect(() => {
        axios.get(`${import.meta.env.VITE_API_URL}/api/season/calendar`)
            .then(res => setData(res.data))
            .catch(() => setData(null));
    }, []);

    if (!data) return <p className="text-light">Loading calendar…</p>;

    const {
        season_start,
        season_end,
        break_start,
        break_end,
        finals_start,
        finals_end
    } = data;

    // Convert main dates
    const seasonStart = new Date(season_start);
    const seasonEnd = new Date(season_end);

    const breakStart = break_start ? new Date(break_start) : null;
    const breakEnd = break_end ? new Date(break_end) : null;

    const finalsStart = finals_start ? new Date(finals_start) : null;
    const finalsEnd = finals_end ? new Date(finals_end) : null;

    const seasonStartStr = seasonStart.toDateString();
    const seasonEndStr = seasonEnd.toDateString();

    // Check if date is within break range
    const isInBreakRange = (dateObj) => {
        if (!breakStart || !breakEnd) return false;
        return dateObj >= breakStart && dateObj <= breakEnd;
    };

    // Check if date is within finals range
    const isInFinalsRange = (dateObj) => {
        if (!finalsStart || !finalsEnd) return false;
        return dateObj >= finalsStart && dateObj <= finalsEnd;
    };

    // Build list of all months in the season
    const months = [];
    const cursor = new Date(seasonStart.getFullYear(), seasonStart.getMonth(), 1);

    while (cursor <= seasonEnd) {
        months.push(new Date(cursor));
        cursor.setMonth(cursor.getMonth() + 1);
    }

    // Build one month's day grid
    const buildMonthDays = (monthObj) => {
        const year = monthObj.getFullYear();
        const month = monthObj.getMonth();

        const firstDay = new Date(year, month, 1);
        const lastDay = new Date(year, month + 1, 0);

        const startOffset = firstDay.getDay();
        const totalDays = lastDay.getDate();

        const cells = [];

        // Empty leading squares
        for (let i = 0; i < startOffset; i++) {
            cells.push({ empty: true });
        }

        // Actual calendar days
        for (let d = 1; d <= totalDays; d++) {
            const dateObj = new Date(year, month, d);
            const dateStr = dateObj.toDateString();

            cells.push({
                day: d,
                date: dateStr,
                inRange: dateObj >= seasonStart && dateObj <= seasonEnd,
                isStart: dateStr === seasonStartStr,
                isEnd: dateStr === seasonEndStr,
                isBreak: isInBreakRange(dateObj),
                isFinals: isInFinalsRange(dateObj)
            });
        }

        return cells;
    };

    return (
        <div className="season-grid-container text-light">
            <h2 className="text-info mb-4 text-center">📅 Season Calendar</h2>

            <div className="season-calendar-grid">
                {months.map((monthObj, idx) => {
                    const monthName = monthObj.toLocaleString("default", { month: "long" });
                    const year = monthObj.getFullYear();
                    const cells = buildMonthDays(monthObj);

                    return (
                        <div key={idx} className="month-block bg-dark rounded p-2">
                            <h5 className="text-center text-info mb-2">
                                {monthName} {year}
                            </h5>

                            <div className="month-grid">
                                {["S", "M", "T", "W", "T", "F", "S"].map((d, i) => (
                                    <div key={i} className="dow">{d}</div>
                                ))}

                                {cells.map((c, i) =>
                                    c.empty ? (
                                        <div key={i} className="day empty"></div>
                                    ) : (
                                        <div
                                            key={i}
                                            className={
                                                "day " +
                                                (c.inRange ? "in-range " : "out-range ") +
                                                (c.isStart ? "season-start " : "") +
                                                (c.isEnd ? "season-end " : "") +
                                                (c.isBreak ? "break-week " : "") +
                                                (c.isFinals ? "finals-week " : "")
                                            }
                                            title={
                                                c.isStart ? "Season Start" :
                                                    c.isEnd ? "Season End" :
                                                        c.isBreak ? "Break Period" :
                                                            c.isFinals ? "Finals Period" :
                                                                ""
                                            }
                                        >
                                            {c.day}
                                        </div>
                                    )
                                )}
                            </div>
                        </div>
                    );
                })}
            </div>

            {/* Legend */}
            <div className="legend mt-4">
                <h5 className="text-info">Legend</h5>
                <div><span className="legend-box season-start"></span>Season Start</div>
                <div><span className="legend-box season-end"></span>Season End</div>
                <div><span className="legend-box break-week"></span>Break Range</div>
                <div><span className="legend-box finals-week"></span>Finals Range</div>
            </div>
        </div>
    );
}
