import { useEffect, useState } from "react";
import axios from "axios";
import "./calendar.css";
import { getApiUrl } from "../config";

export default function FullSeasonCalendar() {
    const [data, setData] = useState(null);

    useEffect(() => {
        axios.get(`${getApiUrl()}/api/season/calendar`)
            .then(res => setData(res.data))
            .catch(() => setData(null));
    }, []);

    if (!data) return <p className="text-light">Loading calendar…</p>;

    const {
        season_start,
        season_end,
        roster_lock,
        breaks,
        finals
    } = data;

    // Parse date string as local midnight (avoids UTC offset issues)
    const parseLocal = (str) => {
        if (!str) return null;
        const [y, m, d] = str.split("-").map(Number);
        return new Date(y, m - 1, d);
    };

    // Convert main dates
    const seasonStart = parseLocal(season_start);
    const seasonEnd = parseLocal(season_end);
    const rosterLock = parseLocal(roster_lock);
    const rosterLockStr = rosterLock ? rosterLock.toDateString() : null;

    // Build break ranges array
    const breakRanges = [];
    if (breaks && breaks.length > 0) {
        for (const b of breaks) {
            breakRanges.push({ start: parseLocal(b.start), end: parseLocal(b.end) });
        }
    }

    // Build finals ranges array
    const finalsRanges = [];
    if (finals && finals.length > 0) {
        for (const f of finals) {
            finalsRanges.push({ start: parseLocal(f.start), end: parseLocal(f.end) });
        }
    }

    const seasonStartStr = seasonStart.toDateString();
    const seasonEndStr = seasonEnd.toDateString();

    const isInBreakRange = (dateObj) => {
        return breakRanges.some(b => dateObj >= b.start && dateObj <= b.end);
    };

    // Check if date is within any finals range (inclusive)
    const isInFinalsRange = (dateObj) => {
        return finalsRanges.some(f => dateObj >= f.start && dateObj <= f.end);
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
                isRosterLock: rosterLockStr && dateStr === rosterLockStr,
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
                                                (c.isRosterLock ? "roster-lock " : "") +
                                                (c.isBreak ? "break-week " : "") +
                                                (c.isFinals ? "finals-week " : "")
                                            }
                                            title={
                                                c.isStart ? "Season Start" :
                                                    c.isEnd ? "Season End" :
                                                        c.isRosterLock ? "Roster Lock Date" :
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
                <div><span className="legend-box roster-lock"></span>Roster Lock Date{rosterLock ? ` (${rosterLock.toLocaleDateString()})` : " (Not set)"}</div>
                <div><span className="legend-box break-week"></span>Break Range</div>
                <div><span className="legend-box finals-week"></span>Finals Range</div>
            </div>
        </div>
    );
}
