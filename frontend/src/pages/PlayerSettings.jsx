import { useState, useEffect, useCallback } from "react";
import { getApiUrl } from "../config";
import { E } from "../components/CustomEmoji";

const MONTHS = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December"
];
const DAY_HEADERS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

// Generate time options in 30-min increments from 8 AM to 2 AM
function generateTimeOptions() {
  const options = [];
  for (let h = 8; h < 26; h++) {
    for (let m = 0; m < 60; m += 30) {
      const hour = h % 24;
      const ampm = hour >= 12 ? "PM" : "AM";
      const displayHour = hour === 0 ? 12 : hour > 12 ? hour - 12 : hour;
      const label = `${displayHour}:${String(m).padStart(2, "0")} ${ampm}`;
      const value = `${String(hour).padStart(2, "0")}:${String(m).padStart(2, "0")}`;
      options.push({ label, value });
    }
  }
  return options;
}

const TIME_OPTIONS = generateTimeOptions();

function toDateStr(year, month, day) {
  return `${year}-${String(month + 1).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

function formatDateNice(dateStr) {
  const [y, m, d] = dateStr.split("-").map(Number);
  return `${MONTHS[m - 1]} ${d}, ${y}`;
}

function formatTimeNice(timeStr) {
  const [h, m] = timeStr.split(":").map(Number);
  const ampm = h >= 12 ? "PM" : "AM";
  const displayH = h === 0 ? 12 : h > 12 ? h - 12 : h;
  return `${displayH}:${String(m).padStart(2, "0")} ${ampm}`;
}

export default function PlayerSettings() {
  const today = new Date();
  const [viewYear, setViewYear] = useState(today.getFullYear());
  const [viewMonth, setViewMonth] = useState(today.getMonth());

  const [availabilityMap, setAvailabilityMap] = useState({}); // date -> [{start_time, end_time, id}]
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  // Modal state for adding/editing a date
  const [selectedDate, setSelectedDate] = useState(null); // "YYYY-MM-DD"
  const [editStartTime, setEditStartTime] = useState("18:00");
  const [editEndTime, setEditEndTime] = useState("22:00");
  const [editError, setEditError] = useState("");

  // Load existing availability
  useEffect(() => {
    fetch(`${getApiUrl()}/api/player/availability`, { credentials: "include" })
      .then((r) => (r.ok ? r.json() : []))
      .then((slots) => {
        const map = {};
        slots.forEach((s) => {
          if (!map[s.date]) map[s.date] = [];
          map[s.date].push({ id: s.id, start_time: s.start_time, end_time: s.end_time });
        });
        setAvailabilityMap(map);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const saveAll = useCallback(async () => {
    setSaving(true);
    setSaved(false);
    const slots = [];
    for (const date in availabilityMap) {
      for (const s of availabilityMap[date]) {
        slots.push({ date, start_time: s.start_time, end_time: s.end_time });
      }
    }
    try {
      const res = await fetch(`${getApiUrl()}/api/player/availability`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify(slots),
      });
      if (res.ok) {
        setSaved(true);
        setTimeout(() => setSaved(false), 3000);
      }
    } catch (err) {
      console.error("Failed to save", err);
    } finally {
      setSaving(false);
    }
  }, [availabilityMap]);

  const openDateModal = (dateStr) => {
    const existing = availabilityMap[dateStr];
    if (existing && existing.length > 0) {
      setEditStartTime(existing[0].start_time);
      setEditEndTime(existing[0].end_time);
    } else {
      setEditStartTime("18:00");
      setEditEndTime("22:00");
    }
    setEditError("");
    setSelectedDate(dateStr);
  };

  const addTimeSlot = () => {
    if (!selectedDate) return;
    if (editStartTime >= editEndTime) {
      setEditError("End time must be after start time.");
      return;
    }
    setEditError("");
    setAvailabilityMap((prev) => {
      const next = { ...prev };
      if (!next[selectedDate]) next[selectedDate] = [];
      next[selectedDate] = [
        ...next[selectedDate],
        { id: 0, start_time: editStartTime, end_time: editEndTime },
      ];
      return next;
    });
  };

  const removeTimeSlot = (dateStr, idx) => {
    setAvailabilityMap((prev) => {
      const next = { ...prev };
      next[dateStr] = next[dateStr].filter((_, i) => i !== idx);
      if (next[dateStr].length === 0) delete next[dateStr];
      return next;
    });
  };

  const goPrevMonth = () => {
    if (viewMonth === 0) {
      setViewYear(viewYear - 1);
      setViewMonth(11);
    } else {
      setViewMonth(viewMonth - 1);
    }
  };

  const goNextMonth = () => {
    if (viewMonth === 11) {
      setViewYear(viewYear + 1);
      setViewMonth(0);
    } else {
      setViewMonth(viewMonth + 1);
    }
  };

  // Build calendar grid
  const firstDay = new Date(viewYear, viewMonth, 1).getDay(); // 0=Sun
  const daysInMonth = new Date(viewYear, viewMonth + 1, 0).getDate();
  const prevMonthDays = new Date(viewYear, viewMonth, 0).getDate();

  const calendarCells = [];
  // Previous month tail
  for (let i = firstDay - 1; i >= 0; i--) {
    const d = prevMonthDays - i;
    const pm = viewMonth === 0 ? 11 : viewMonth - 1;
    const py = viewMonth === 0 ? viewYear - 1 : viewYear;
    calendarCells.push({ day: d, month: pm, year: py, isOtherMonth: true });
  }
  // Current month
  for (let d = 1; d <= daysInMonth; d++) {
    calendarCells.push({ day: d, month: viewMonth, year: viewYear, isOtherMonth: false });
  }
  // Next month head (fill to complete last row)
  const remaining = 7 - (calendarCells.length % 7);
  if (remaining < 7) {
    for (let d = 1; d <= remaining; d++) {
      const nm = viewMonth === 11 ? 0 : viewMonth + 1;
      const ny = viewMonth === 11 ? viewYear + 1 : viewYear;
      calendarCells.push({ day: d, month: nm, year: ny, isOtherMonth: true });
    }
  }

  if (loading) {
    return (
      <div className="text-center py-5">
        <div className="spinner-border text-light" role="status" />
      </div>
    );
  }

  return (
    <div className="player-settings-root">
      <h2><E n="clock" /> My Availability</h2>
      <p className="text-secondary mb-3">
        Tap a date on the calendar, then add your available time slots. Your availability helps captains schedule matches.
      </p>

      {/* Month navigation */}
      <div className="avail-month-nav">
        <button className="btn btn-outline-light btn-sm" onClick={goPrevMonth}>
          ◀
        </button>
        <h4 className="m-0">{MONTHS[viewMonth]} {viewYear}</h4>
        <button className="btn btn-outline-light btn-sm" onClick={goNextMonth}>
          ▶
        </button>
      </div>

      {/* Calendar grid */}
      <div className="avail-calendar">
        {DAY_HEADERS.map((dh) => (
          <div key={dh} className="avail-cal-header">{dh}</div>
        ))}
        {calendarCells.map((cell, i) => {
          const dateStr = toDateStr(cell.year, cell.month, cell.day);
          const hasSlots = !!availabilityMap[dateStr] && availabilityMap[dateStr].length > 0;
          const isToday = dateStr === toDateStr(today.getFullYear(), today.getMonth(), today.getDate());
          return (
            <button
              key={i}
              type="button"
              className={`avail-cal-cell ${cell.isOtherMonth ? "other-month" : ""} ${isToday ? "today" : ""} ${hasSlots ? "has-slots" : ""}`}
              onClick={() => openDateModal(dateStr)}
              title={formatDateNice(dateStr)}
            >
              <span className="avail-cal-day">{cell.day}</span>
              {hasSlots && <span className="avail-cal-dot" />}
            </button>
          );
        })}
      </div>

      {/* Date modal */}
      {selectedDate && (
        <div className="avail-modal-backdrop" onClick={() => setSelectedDate(null)}>
          <div className="avail-modal" onClick={(e) => e.stopPropagation()}>
            <div className="avail-modal-header">
              <h5 className="m-0">{formatDateNice(selectedDate)}</h5>
              <button className="btn-close btn-close-white" onClick={() => setSelectedDate(null)} />
            </div>
            <div className="avail-modal-body">
              {/* Existing slots for this date */}
              {availabilityMap[selectedDate] && availabilityMap[selectedDate].length > 0 && (
                <div className="mb-3">
                  <label className="form-label text-secondary small">Current time slots:</label>
                  {availabilityMap[selectedDate].map((slot, i) => (
                    <div key={i} className="avail-slot-chip">
                      <span>{formatTimeNice(slot.start_time)} – {formatTimeNice(slot.end_time)}</span>
                      <button
                        className="avail-slot-remove"
                        onClick={() => removeTimeSlot(selectedDate, i)}
                        title="Remove"
                      >
                        ✕
                      </button>
                    </div>
                  ))}
                </div>
              )}

              {/* Add new slot */}
              <label className="form-label text-secondary small">Add time slot:</label>
              <div className="avail-time-picker-row">
                <select
                  className="form-select form-select-sm"
                  value={editStartTime}
                  onChange={(e) => setEditStartTime(e.target.value)}
                >
                  {TIME_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>{opt.label}</option>
                  ))}
                </select>
                <span className="text-secondary">to</span>
                <select
                  className="form-select form-select-sm"
                  value={editEndTime}
                  onChange={(e) => setEditEndTime(e.target.value)}
                >
                  {TIME_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>{opt.label}</option>
                  ))}
                </select>
                <button className="btn btn-outline-primary btn-sm" onClick={addTimeSlot}>
                  <E n="plus" /> Add
                </button>
              </div>
              {editError && <p className="text-danger small mt-1 mb-0">{editError}</p>}
            </div>
            <div className="avail-modal-footer">
              <button className="btn btn-secondary btn-sm" onClick={() => setSelectedDate(null)}>
                Done
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Save */}
      <div className="avail-save-bar mt-3">
        <button className="btn btn-success" onClick={saveAll} disabled={saving}>
          {saving ? "Saving..." : saved ? (<><E n="check" /> Saved!</>) : "Save Availability"}
        </button>
      </div>

      {/* Summary of all availability */}
      {Object.keys(availabilityMap).length > 0 && (
        <div className="availability-summary mt-4">
          <h5><E n="check" /> Your Availability</h5>
          <div className="row">
            {Object.keys(availabilityMap)
              .sort()
              .map((dateStr) => (
                <div key={dateStr} className="col-12 col-sm-6 col-md-4 mb-2">
                  <div className="avail-day-summary">
                    <strong>{formatDateNice(dateStr)}</strong>
                    <ul className="list-unstyled mb-0 mt-1">
                      {availabilityMap[dateStr].map((s, i) => (
                        <li key={i} className="text-secondary small d-flex justify-content-between align-items-center">
                          <span>{formatTimeNice(s.start_time)} – {formatTimeNice(s.end_time)}</span>
                          <button
                            className="avail-slot-remove"
                            onClick={() => removeTimeSlot(dateStr, i)}
                            title="Remove"
                          >
                            ✕
                          </button>
                        </li>
                      ))}
                    </ul>
                  </div>
                </div>
              ))}
          </div>
        </div>
      )}
    </div>
  );
}

