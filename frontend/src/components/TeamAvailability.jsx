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
  const [availData, setAvailData] = useState(null); // { dates: [...] }
  const [loading, setLoading] = useState(true);
  const [selectedDate, setSelectedDate] = useState(null);
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
  if (!availData || !availData.dates || availData.dates.length === 0) {
    return (
      <div className="card bg-dark border-secondary p-3 mb-3 shadow-sm">
        <h5 className="mb-2"><E n="calendar" /> Team Availability</h5>
        <p className="text-muted small mb-0">
          {availData?.message || "No overlapping availability found yet. Players need to set their availability in Player Settings."}
        </p>
      </div>
    );
  }

  // Build a set of dates that have availability
  const availDates = new Set(availData.dates.map((d) => d.date));

  // Find date entry for selectedDate
  const selectedEntry = selectedDate
    ? availData.dates.find((d) => d.date === selectedDate)
    : null;

  const goPrevMonth = () => {
    if (viewMonth === 0) { setViewYear(viewYear - 1); setViewMonth(11); }
    else setViewMonth(viewMonth - 1);
  };
  const goNextMonth = () => {
    if (viewMonth === 11) { setViewYear(viewYear + 1); setViewMonth(0); }
    else setViewMonth(viewMonth + 1);
  };

  // Build calendar cells
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
      <h5 className="mb-2"><E n="calendar" /> Team Availability <small className="text-muted">(3+ players)</small></h5>

      {/* Mini calendar nav */}
      <div className="d-flex align-items-center justify-content-between mb-2">
        <button className="btn btn-outline-light btn-sm py-0 px-2" onClick={goPrevMonth}>◀</button>
        <span className="fw-semibold small">{MONTHS[viewMonth]} {viewYear}</span>
        <button className="btn btn-outline-light btn-sm py-0 px-2" onClick={goNextMonth}>▶</button>
      </div>

      {/* Mini calendar grid */}
      <div className="ta-mini-cal">
        {DAY_HEADERS.map((dh) => (
          <div key={dh} className="ta-mini-header">{dh}</div>
        ))}
        {cells.map((cell, i) => {
          const dateKey = toDateKey(viewYear, viewMonth, cell.day);
          const hasAvail = !cell.isOther && availDates.has(dateKey);
          const isSel = dateKey === selectedDate;
          return (
            <button
              key={i}
              type="button"
              className={`ta-mini-cell ${cell.isOther ? "other" : ""} ${hasAvail ? "has-avail" : ""} ${isSel ? "selected" : ""}`}
              disabled={cell.isOther}
              onClick={() => setSelectedDate(isSel ? null : dateKey)}
              title={hasAvail ? dateKey : undefined}
            >
              {cell.day}
            </button>
          );
        })}
      </div>

      {/* Selected date details */}
      {selectedEntry && (
        <div className="ta-detail mt-2">
          <div className="ta-detail-header">
            <strong>{selectedDate}</strong>
            <button className="btn-close btn-close-white btn-sm" onClick={() => setSelectedDate(null)} style={{ fontSize: '0.5rem' }} />
          </div>
          {selectedEntry.ranges.map((r, i) => (
            <div key={i} className="ta-range-chip">
              <span>{formatTimeNice(r.start_time)} – {formatTimeNice(r.end_time)}</span>
              <span className="badge bg-secondary ms-1">{r.player_count}p</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
