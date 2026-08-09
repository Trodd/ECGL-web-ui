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

export default function MatchAvailability({ matchId, compact = false }) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [selectedDate, setSelectedDate] = useState(null);
  const [viewMonth, setViewMonth] = useState(new Date().getMonth());
  const [viewYear, setViewYear] = useState(new Date().getFullYear());

  useEffect(() => {
    if (!matchId) { setLoading(false); return; }
    fetch(`${getApiUrl()}/api/match/${matchId}/availability`, { credentials: "include" })
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => setData(d))
      .catch(() => setData(null))
      .finally(() => setLoading(false));
  }, [matchId]);

  if (loading) return null;

  const dates = data?.dates || [];
  const hasDataA = data?.team_a_has_data;
  const hasDataB = data?.team_b_has_data;
  const noOverlap = dates.length === 0;

  if (compact) {
    // Compact inline version for MatchCard
    if (noOverlap) {
      return (
        <div className="small text-muted mt-2">
          <E n="calendar" />{" "}
          {(!hasDataA || !hasDataB)
            ? "Not enough availability data to find overlapping times."
            : "No date/time overlap found between teams."}
        </div>
      );
    }
    return (
      <div className="mt-2">
        <div className="small text-success fw-semibold mb-1">
          <E n="calendar" /> Both teams available on:
        </div>
        <div className="d-flex flex-wrap gap-1">
          {dates.slice(0, 3).map((d) => (
            <span key={d.date} className="badge bg-success bg-opacity-25 text-success border border-success">
              {d.date.slice(5)}
            </span>
          ))}
          {dates.length > 3 && (
            <span className="badge bg-secondary">+{dates.length - 3} more</span>
          )}
        </div>
      </div>
    );
  }

  // Full version for MatchDetail
  // Build set of overlapping dates
  const overlapDates = new Set(dates.map((d) => d.date));
  const selectedEntry = selectedDate ? dates.find((d) => d.date === selectedDate) : null;

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
      <h5 className="mb-2">
        <E n="calendar" /> Match Availability{" "}
        <small className="text-muted">(3+ per team)</small>
      </h5>

      {!hasDataA || !hasDataB ? (
        <p className="text-muted small mb-0">
          {!hasDataA && !hasDataB
            ? "Neither team has enough players with availability set."
            : !hasDataA
              ? "Team A doesn't have enough availability data."
              : "Team B doesn't have enough availability data."}
        </p>
      ) : noOverlap ? (
        <p className="text-muted small mb-0">
          No overlapping availability found between the two teams. Encourage your players to set their availability in Player Settings.
        </p>
      ) : (
        <>
          {/* Mini calendar */}
          <div className="d-flex align-items-center justify-content-between mb-2">
            <button className="btn btn-outline-light btn-sm py-0 px-2" onClick={goPrevMonth}>◀</button>
            <span className="fw-semibold small">{MONTHS[viewMonth]} {viewYear}</span>
            <button className="btn btn-outline-light btn-sm py-0 px-2" onClick={goNextMonth}>▶</button>
          </div>

          <div className="ta-mini-cal">
            {DAY_HEADERS.map((dh) => (
              <div key={dh} className="ta-mini-header">{dh}</div>
            ))}
            {cells.map((cell, i) => {
              const dateKey = toDateKey(viewYear, viewMonth, cell.day);
              const hasOverlap = !cell.isOther && overlapDates.has(dateKey);
              const isSel = dateKey === selectedDate;
              return (
                <button
                  key={i}
                  type="button"
                  className={`ta-mini-cell ${cell.isOther ? "other" : ""} ${hasOverlap ? "has-avail" : ""} ${isSel ? "selected" : ""}`}
                  disabled={cell.isOther}
                  onClick={() => setSelectedDate(isSel ? null : dateKey)}
                  title={hasOverlap ? dateKey : undefined}
                >
                  {cell.day}
                </button>
              );
            })}
          </div>

          {/* Selected date detail */}
          {selectedEntry && (
            <div className="ta-detail mt-2">
              <div className="ta-detail-header">
                <strong>{selectedDate}</strong>
                <button className="btn-close btn-close-white btn-sm" onClick={() => setSelectedDate(null)} style={{ fontSize: '0.5rem' }} />
              </div>
              {selectedEntry.ranges.map((r, i) => (
                <div key={i} className="ta-range-chip">
                  <span>{formatTimeNice(r.start_time)} – {formatTimeNice(r.end_time)}</span>
                  <span className="badge bg-secondary ms-1">A:{r.team_a_players}p B:{r.team_b_players}p</span>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
