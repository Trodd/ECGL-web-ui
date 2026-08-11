import { useEffect, useState } from "react";
import { getApiUrl } from "../config";
import { E } from "./CustomEmoji";

const MONTHS = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December"
];

function formatTimeNice(timeStr) {
  const [h, m] = timeStr.split(":").map(Number);
  const ampm = h >= 12 ? "PM" : "AM";
  const displayH = h === 0 ? 12 : h > 12 ? h - 12 : h;
  return `${displayH}:${String(m).padStart(2, "0")} ${ampm}`;
}

function toDateKey(y, m, d) {
  return `${y}-${String(m + 1).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
}

export default function TeamAvailability({ teamId }) {
  const [availData, setAvailData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [viewMonth, setViewMonth] = useState(new Date().getMonth());
  const [viewYear, setViewYear] = useState(new Date().getFullYear());

  useEffect(() => {
    if (!teamId) { setLoading(false); return; }
    fetch(`${getApiUrl()}/api/team/${teamId}/availability`, { credentials: "include" })
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => setAvailData(d))
      .catch(() => setAvailData(null))
      .finally(() => setLoading(false));
  }, [teamId]);

  if (loading) return null;

  const dates = availData?.dates || [];
  const datesMap = {};
  dates.forEach((d) => { datesMap[d.date] = d; });

  if (!availData || dates.length === 0) {
    return (
      <div className="card bg-dark border-secondary p-3 mb-3 shadow-sm">
        <h5 className="mb-2"><E n="calendar" /> Team Availability</h5>
        <p className="text-muted small mb-0">
          No availability set yet. Players can set theirs in Player Settings (account dropdown).
        </p>
      </div>
    );
  }

  const goPrevMonth = () => {
    if (viewMonth === 0) { setViewYear(viewYear - 1); setViewMonth(11); }
    else setViewMonth(viewMonth - 1);
  };
  const goNextMonth = () => {
    if (viewMonth === 11) { setViewYear(viewYear + 1); setViewMonth(0); }
    else setViewMonth(viewMonth + 1);
  };

  const firstDay = new Date(viewYear, viewMonth, 1).getDay();
  const daysInMonth = new Date(viewYear, viewMonth + 1, 0).getDate();
  const prevMonthDays = new Date(viewYear, viewMonth, 0).getDate();

  const cells = [];
  for (let i = firstDay - 1; i >= 0; i--) {
    cells.push({ day: prevMonthDays - i, isOther: true });
  }
  for (let d = 1; d <= daysInMonth; d++) {
    cells.push({ day: d, isOther: false });
  }
  const rem = 7 - (cells.length % 7);
  if (rem < 7) {
    for (let d = 1; d <= rem; d++) {
      cells.push({ day: d, isOther: true });
    }
  }

  const DAY_HEADERS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

  return (
    <div className="card bg-dark border-secondary p-3 mb-3 shadow-sm">
      <h5 className="mb-2"><E n="calendar" /> Team Availability</h5>

      <div className="d-flex align-items-center justify-content-between mb-2">
        <button className="btn btn-outline-light btn-sm py-0 px-2" onClick={goPrevMonth}>◀</button>
        <span className="fw-semibold small">{MONTHS[viewMonth]} {viewYear}</span>
        <button className="btn btn-outline-light btn-sm py-0 px-2" onClick={goNextMonth}>▶</button>
      </div>

      <div className="ta-month-grid">
        {DAY_HEADERS.map((dh) => (
          <div key={dh} className="ta-month-header">{dh}</div>
        ))}
        {cells.map((cell, i) => {
          const dateKey = toDateKey(viewYear, viewMonth, cell.day);
          const entry = datesMap[dateKey];
          const hasData = !cell.isOther && !!entry;
          const hasOverlap = hasData && entry.overlaps && entry.overlaps.length > 0;

          return (
            <div
              key={i}
              className={`ta-month-cell ${cell.isOther ? "other" : ""} ${hasOverlap ? "has-overlap" : hasData ? "has-data" : ""}`}
            >
              {!cell.isOther && (
                <>
                  <span className="ta-month-day">{cell.day}</span>
                  {hasOverlap && entry.overlaps.map((o, oi) => (
                    <div key={oi} className="ta-cell-overlap">
                      <span className="ta-cell-time">{formatTimeNice(o.start_time)}–{formatTimeNice(o.end_time)}</span>
                      {o.players.map((p, pi) => (
                        <span key={pi} className="ta-cell-player">{p}</span>
                      ))}
                    </div>
                  ))}
                  {hasData && !hasOverlap && entry.players.map((p, pi) => (
                    <span key={pi} className="ta-cell-player dim">{p}</span>
                  ))}
                </>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
