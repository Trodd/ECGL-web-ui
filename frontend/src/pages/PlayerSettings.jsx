import { useState, useEffect, useCallback } from "react";
import { getApiUrl } from "../config";
import { E } from "../components/CustomEmoji";
import { ROLE_OPTIONS, DEVICE_OPTIONS, TIMEZONE_GROUPS } from "../profileOptions";

const MONTHS = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December"
];
const DAY_HEADERS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

function generateTimeOptions() {
  const options = [];
  for (let h = 8; h < 26; h++) {
    for (let m = 0; m < 60; m += 30) {
      const hour = h % 24;
      const ampm = hour >= 12 ? "PM" : "AM";
      const displayHour = hour === 0 ? 12 : hour > 12 ? hour - 12 : hour;
      options.push({
        label: `${displayHour}:${String(m).padStart(2, "0")} ${ampm}`,
        value: `${String(hour).padStart(2, "0")}:${String(m).padStart(2, "0")}`,
      });
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
  const [availabilityMap, setAvailabilityMap] = useState({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  const [selectedDate, setSelectedDate] = useState(null);
  const [editStartTime, setEditStartTime] = useState("18:00");
  const [editEndTime, setEditEndTime] = useState("22:00");
  const [editError, setEditError] = useState("");

  // ── Tabs / profile / preferences ──
  const [tab, setTab] = useState("availability");
  const [profile, setProfile] = useState(null);
  const [prefs, setPrefs] = useState(null);
  const [teams, setTeams] = useState([]);
  const [notifyTypes, setNotifyTypes] = useState([]);
  const [cooldown, setCooldown] = useState(null);
  const [profileMsg, setProfileMsg] = useState("");
  const [prefsMsg, setPrefsMsg] = useState("");
  const [overlayToken, setOverlayToken] = useState("");
  const [busy, setBusy] = useState("");

  const urlBase = getApiUrl();

  function loadSettings() {
    fetch(`${urlBase}/api/player/settings`, { credentials: "include" })
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (!data) return;
        setProfile(data.profile || null);
        setPrefs(data.prefs || null);
        setTeams(Array.isArray(data.teams) ? data.teams : []);
        setNotifyTypes(Array.isArray(data.notification_types) ? data.notification_types : []);
        setCooldown(data.cooldown || null);
        setOverlayToken(data.prefs?.overlay_token || "");
        if (data.prefs?.default_start_time) setEditStartTime(data.prefs.default_start_time);
        if (data.prefs?.default_end_time) setEditEndTime(data.prefs.default_end_time);
      })
      .catch(() => {});
  }

  useEffect(() => {
    fetch(`${getApiUrl()}/api/player/availability`, { credentials: "include" })
      .then((r) => (r.ok ? r.json() : []))
      .then((slots) => {
        const map = {};
        slots.forEach((s) => {
          if (!map[s.date]) map[s.date] = [];
          map[s.date].push({ start_time: s.start_time, end_time: s.end_time });
        });
        setAvailabilityMap(map);
      })
      .catch(() => {})
      .finally(() => setLoading(false));

    loadSettings();
  }, []);

  // Cache the UI preferences locally so they apply on the next page load.
  function cachePrefs(p) {
    try {
      localStorage.setItem("ecgl_prefs", JSON.stringify({
        default_league: p.default_league,
        landing_page: p.landing_page,
        compact_cards: p.compact_cards,
        reduce_motion: p.reduce_motion,
        notify_sound: p.notify_sound,
      }));
    } catch { /* ignore */ }
    window.dispatchEvent(new CustomEvent("ecgl-prefs-changed"));
  }

  async function saveProfile() {
    if (!profile) return;
    setBusy("profile");
    setProfileMsg("");
    try {
      const res = await fetch(`${urlBase}/api/player/settings`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({
          role: profile.role || "",
          device: profile.device || "",
          timezone: profile.timezone || "",
        }),
      });
      if (!res.ok) throw new Error(await res.text());
      setProfileMsg("✅ Profile saved.");
      loadSettings();
    } catch (err) {
      setProfileMsg("❌ " + (err.message || "Failed to save profile"));
    } finally {
      setBusy("");
    }
  }

  async function savePrefs(patch) {
    const next = { ...(prefs || {}), ...patch };
    setPrefs(next);
    setBusy("prefs");
    setPrefsMsg("");
    try {
      const res = await fetch(`${urlBase}/api/player/settings`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify(patch),
      });
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      setPrefs(data.prefs);
      cachePrefs(data.prefs);
      setPrefsMsg("✅ Saved.");
      setTimeout(() => setPrefsMsg(""), 2500);
    } catch (err) {
      setPrefsMsg("❌ " + (err.message || "Failed to save"));
    } finally {
      setBusy("");
    }
  }

  function toggleMute(type) {
    const current = String(prefs?.muted_notification_types || "")
      .split(",").map((t) => t.trim()).filter(Boolean);
    const next = current.includes(type)
      ? current.filter((t) => t !== type)
      : [...current, type];
    savePrefs({ muted_notification_types: next.join(",") });
  }

  async function rotateToken() {
    if (!confirm("Generate a new overlay token? The old one stops working.")) return;
    setBusy("token");
    try {
      const res = await fetch(`${urlBase}/api/player/overlay-token`, {
        method: "POST",
        credentials: "include",
      });
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      setOverlayToken(data.overlay_token || "");
      setPrefsMsg("✅ New token generated.");
    } catch (err) {
      setPrefsMsg("❌ " + (err.message || "Failed to rotate token"));
    } finally {
      setBusy("");
    }
  }

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
      if (res.ok) { setSaved(true); setTimeout(() => setSaved(false), 3000); }
    } catch (err) { console.error("Failed to save", err); }
    finally { setSaving(false); }
  }, [availabilityMap]);

  const openDateModal = (dateStr) => {
    const existing = availabilityMap[dateStr];
    if (existing && existing.length > 0) {
      setEditStartTime(existing[0].start_time);
      setEditEndTime(existing[0].end_time);
    } else {
      setEditStartTime(prefs?.default_start_time || "18:00");
      setEditEndTime(prefs?.default_end_time || "22:00");
    }
    setEditError("");
    setSelectedDate(dateStr);
  };

  // Copy the visible month's slots onto the same weekdays next month.
  const copyPreviousWeek = () => {
    setAvailabilityMap((prev) => {
      const next = { ...prev };
      let copied = 0;
      // For every date with slots, mirror it one week later when empty.
      Object.keys(prev).forEach((dateStr) => {
        const [y, m, d] = dateStr.split("-").map(Number);
        if (y !== viewYear || m - 1 !== viewMonth) return;
        const target = new Date(y, m - 1, d + 7);
        const targetStr = toDateStr(target.getFullYear(), target.getMonth(), target.getDate());
        if (next[targetStr] && next[targetStr].length > 0) return;
        next[targetStr] = prev[dateStr].map((s) => ({ ...s }));
        copied++;
      });
      if (copied === 0) return prev;
      return next;
    });
  };

  // Clear every slot in the visible month.
  const clearMonth = () => {
    if (!confirm(`Remove all availability in ${MONTHS[viewMonth]} ${viewYear}?`)) return;
    setAvailabilityMap((prev) => {
      const next = { ...prev };
      Object.keys(next).forEach((dateStr) => {
        const [y, m] = dateStr.split("-").map(Number);
        if (y === viewYear && m - 1 === viewMonth) delete next[dateStr];
      });
      return next;
    });
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
      next[selectedDate] = [...next[selectedDate], { start_time: editStartTime, end_time: editEndTime }];
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
  for (let i = firstDay - 1; i >= 0; i--) cells.push({ day: prevMonthDays - i, other: true });
  for (let d = 1; d <= daysInMonth; d++) cells.push({ day: d, other: false });
  const rem = 7 - (cells.length % 7);
  if (rem < 7) for (let d = 1; d <= rem; d++) cells.push({ day: d, other: true });

  if (loading) {
    return <div className="text-center py-5"><div className="spinner-border text-light" role="status" /></div>;
  }

  return (
    <div className="player-settings-root">
      <h2><E n="gear" /> Player Settings</h2>

      <div className="d-flex flex-wrap gap-2 mb-3">
        {[
          { key: "profile", label: "👤 Profile" },
          { key: "availability", label: "🗓 Availability" },
          { key: "notifications", label: "🔔 Notifications" },
          { key: "appearance", label: "🎨 Appearance" },
          { key: "privacy", label: "🔒 Privacy & Data" },
        ].map((t) => (
          <button
            key={t.key}
            type="button"
            className={`btn btn-sm ${tab === t.key ? "btn-info" : "btn-outline-secondary"}`}
            onClick={() => setTab(t.key)}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* ══════════════════ PROFILE ══════════════════ */}
      {tab === "profile" && profile && (
        <div className="card bg-dark border-secondary p-3 mb-3" style={{ maxWidth: 620 }}>
          <h5 className="mb-3">👤 Profile</h5>

          <div className="mb-3">
            <div className="small text-secondary mb-1">Name</div>
            <div className="text-light">
              {profile.display_name || profile.username}
              <span className="text-secondary small ms-2">@{profile.username}</span>
            </div>
            <div className="small text-secondary mt-1">Set by your Discord account.</div>
          </div>

          <label className="form-label text-secondary small mb-1">Role</label>
          <select
            className="form-select form-select-sm bg-dark text-light mb-3"
            value={profile.role || ""}
            onChange={(e) => setProfile({ ...profile, role: e.target.value })}
          >
            <option value="">Select role…</option>
            {ROLE_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>

          <label className="form-label text-secondary small mb-1">Device</label>
          <select
            className="form-select form-select-sm bg-dark text-light mb-3"
            value={profile.device || ""}
            onChange={(e) => setProfile({ ...profile, device: e.target.value })}
          >
            <option value="">Select device…</option>
            {DEVICE_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>

          <label className="form-label text-secondary small mb-1">Timezone</label>
          <select
            className="form-select form-select-sm bg-dark text-light mb-1"
            value={profile.timezone || ""}
            onChange={(e) => setProfile({ ...profile, timezone: e.target.value })}
          >
            <option value="">Select timezone…</option>
            {TIMEZONE_GROUPS.map((g) => (
              <optgroup key={g.label} label={g.label}>
                {g.options.map((o) => (
                  <option key={o.value} value={o.value}>{o.label}</option>
                ))}
              </optgroup>
            ))}
          </select>
          <p className="text-secondary small mb-3">
            Used to show availability in your local time.
          </p>

          <div className="d-flex align-items-center gap-2 flex-wrap">
            <button className="btn btn-success btn-sm" onClick={saveProfile} disabled={busy === "profile"}>
              {busy === "profile" ? "Saving…" : "Save profile"}
            </button>
            {profileMsg && <span className="small text-info">{profileMsg}</span>}
          </div>

          {teams.length > 0 && (
            <div className="mt-3 pt-3 border-top border-secondary">
              <div className="small text-secondary mb-1">My teams</div>
              {teams.map((t) => (
                <div key={`${t.league}-${t.team_id}`} className="small text-light">
                  <span className="badge bg-secondary me-1">{t.league}</span>
                  {t.team_name}
                  <span className="text-secondary"> · {t.role}</span>
                </div>
              ))}
            </div>
          )}

          {cooldown?.on_cooldown && cooldown?.last_left_team_at && (
            <p className="small text-warning mt-2 mb-0">
              ⏳ Last left a team on {new Date(cooldown.last_left_team_at).toLocaleDateString()} — a join
              cooldown may still apply.
            </p>
          )}

          <div className="mt-3 pt-3 border-top border-secondary">
            <a className="btn btn-outline-warning btn-sm" href="/register">Unregister / manage account</a>
            <a className="btn btn-outline-info btn-sm ms-2" href="/myteam">My Team</a>
          </div>
        </div>
      )}

      {/* ══════════════════ NOTIFICATIONS ══════════════════ */}
      {tab === "notifications" && prefs && (
        <div className="card bg-dark border-secondary p-3 mb-3" style={{ maxWidth: 620 }}>
          <h5 className="mb-1">🔔 Notifications</h5>
          <p className="text-secondary small">Turn off the categories you don't want to be notified about.</p>

          <div className="form-check form-switch mb-3">
            <input
              className="form-check-input"
              type="checkbox"
              checked={Boolean(prefs.notify_sound)}
              onChange={(e) => savePrefs({ notify_sound: e.target.checked })}
            />
            <label className="form-check-label small text-light">Play a sound for new notifications</label>
          </div>

          <div className="d-flex flex-column gap-1">
            {notifyTypes.map((t) => {
              const muted = String(prefs.muted_notification_types || "")
                .split(",").map((x) => x.trim()).includes(t);
              return (
                <label key={t} className="d-flex align-items-center justify-content-between small">
                  <span className="text-light">{t.replace(/_/g, " ")}</span>
                  <input
                    type="checkbox"
                    className="form-check-input mt-0"
                    checked={!muted}
                    onChange={() => toggleMute(t)}
                    title={muted ? "Muted — click to enable" : "Enabled — click to mute"}
                  />
                </label>
              );
            })}
          </div>
          {prefsMsg && <div className="small text-info mt-2">{prefsMsg}</div>}
        </div>
      )}

      {/* ══════════════════ APPEARANCE ══════════════════ */}
      {tab === "appearance" && prefs && (
        <div className="card bg-dark border-secondary p-3 mb-3" style={{ maxWidth: 620 }}>
          <h5 className="mb-3">🎨 Appearance & Defaults</h5>

          <label className="form-label text-secondary small mb-1">Default league on load</label>
          <select
            className="form-select form-select-sm bg-dark text-light mb-3"
            value={prefs.default_league || "main"}
            onChange={(e) => savePrefs({ default_league: e.target.value })}
          >
            <option value="main">Varsity League</option>
            <option value="rookie">Rookie League</option>
          </select>

          <label className="form-label text-secondary small mb-1">Landing page</label>
          <select
            className="form-select form-select-sm bg-dark text-light mb-3"
            value={prefs.landing_page || ""}
            onChange={(e) => savePrefs({ landing_page: e.target.value })}
          >
            <option value="">Home</option>
            <option value="/myteam">My Team</option>
            <option value="/matchups">Matchups</option>
            <option value="/leaderboard">Leaderboard</option>
            <option value="/players">Players</option>
            <option value="/teams">Teams</option>
          </select>

          <div className="form-check form-switch mb-2">
            <input
              className="form-check-input"
              type="checkbox"
              checked={Boolean(prefs.compact_cards)}
              onChange={(e) => savePrefs({ compact_cards: e.target.checked })}
            />
            <label className="form-check-label small text-light">Compact cards (tighter spacing)</label>
          </div>

          <div className="form-check form-switch mb-3">
            <input
              className="form-check-input"
              type="checkbox"
              checked={Boolean(prefs.reduce_motion)}
              onChange={(e) => savePrefs({ reduce_motion: e.target.checked })}
            />
            <label className="form-check-label small text-light">Reduce motion & glow effects</label>
          </div>

          {prefsMsg && <div className="small text-info">{prefsMsg}</div>}
        </div>
      )}

      {/* ══════════════════ PRIVACY & DATA ══════════════════ */}
      {tab === "privacy" && prefs && (
        <div className="card bg-dark border-secondary p-3 mb-3" style={{ maxWidth: 620 }}>
          <h5 className="mb-3">🔒 Privacy & Data</h5>

          <div className="form-check form-switch mb-2">
            <input
              className="form-check-input"
              type="checkbox"
              checked={Boolean(prefs.available_as_sub)}
              onChange={(e) => savePrefs({ available_as_sub: e.target.checked })}
            />
            <label className="form-check-label small text-light">
              Available as a sub for other teams
            </label>
          </div>
          <p className="text-secondary small mb-3">
            Flags you as willing to fill in for short-handed teams.
          </p>

          <div className="form-check form-switch mb-2">
            <input
              className="form-check-input"
              type="checkbox"
              checked={Boolean(prefs.hide_availability)}
              onChange={(e) => savePrefs({ hide_availability: e.target.checked })}
            />
            <label className="form-check-label small text-light">
              Hide my availability from other teams
            </label>
          </div>
          <p className="text-secondary small mb-3">
            Your own team (and mods) still see it; other teams' availability views skip you.
          </p>

          <label className="form-label text-secondary small mb-1">Overlay token</label>
          <div className="input-group input-group-sm mb-1">
            <input
              className="form-control bg-dark text-light"
              readOnly
              value={overlayToken || "No token yet"}
            />
            <button
              className="btn btn-outline-info"
              disabled={busy === "token"}
              onClick={rotateToken}
            >
              {overlayToken ? "Rotate" : "Generate"}
            </button>
          </div>
          <p className="text-secondary small mb-3">
            Use for personal stream overlays. Rotating invalidates the previous token.
          </p>

          <a
            className="btn btn-outline-light btn-sm"
            href={`${urlBase}/api/player/export`}
            download
          >
            ⬇ Download my data (JSON)
          </a>

          {prefsMsg && <div className="small text-info mt-2">{prefsMsg}</div>}
        </div>
      )}

      {/* ══════════════════ AVAILABILITY ══════════════════ */}
      {tab === "availability" && (
        <>
          <h4 className="mb-1"><E n="clock" /> My Availability</h4>
          <p className="text-secondary mb-3">
            Tap a date to add or remove your available time slots.
            {profile?.timezone ? ` Times are shown in ${profile.timezone}.` : ""}
          </p>

          <div className="d-flex flex-wrap gap-2 mb-3 align-items-end">
            <div>
              <label className="form-label text-secondary small mb-1 d-block">Default slot when adding</label>
              <div className="d-flex align-items-center gap-1">
                <select
                  className="form-select form-select-sm bg-dark text-light"
                  style={{ width: 110 }}
                  value={prefs?.default_start_time || "18:00"}
                  onChange={(e) => savePrefs({ default_start_time: e.target.value })}
                >
                  {TIME_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
                <span className="text-secondary small">to</span>
                <select
                  className="form-select form-select-sm bg-dark text-light"
                  style={{ width: 110 }}
                  value={prefs?.default_end_time || "22:00"}
                  onChange={(e) => savePrefs({ default_end_time: e.target.value })}
                >
                  {TIME_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
              </div>
            </div>
            <button className="btn btn-outline-light btn-sm" onClick={copyPreviousWeek}>
              ⟳ Copy week forward
            </button>
            <button className="btn btn-outline-danger btn-sm" onClick={clearMonth}>
              🗑 Clear this month
            </button>
          </div>

          <div className="avail-month-nav">
            <button className="btn btn-outline-light btn-sm" onClick={goPrevMonth}>◀</button>
            <h4 className="m-0">{MONTHS[viewMonth]} {viewYear}</h4>
            <button className="btn btn-outline-light btn-sm" onClick={goNextMonth}>▶</button>
          </div>

          {/* Same grid style as TeamAvailability */}
          <div className="ta-month-grid">
            {DAY_HEADERS.map((dh) => (
              <div key={dh} className="ta-month-header">{dh}</div>
            ))}
            {cells.map((cell, i) => {
              const dateKey = toDateStr(viewYear, viewMonth, cell.day);
              const slots = availabilityMap[dateKey];
              const hasData = !cell.other && slots && slots.length > 0;
              return (
                <button
                  key={i}
                  type="button"
                  className={`ta-month-cell clickable ${cell.other ? "other" : ""} ${hasData ? "has-overlap" : ""}`}
                  disabled={cell.other}
                  onClick={() => openDateModal(dateKey)}
                >
                  {!cell.other && (
                    <>
                      <span className="ta-month-day">{cell.day}</span>
                      {hasData && slots.map((s, si) => (
                        <div key={si} className="ta-cell-overlap">
                          <span className="ta-cell-time">{formatTimeNice(s.start_time)}–{formatTimeNice(s.end_time)}</span>
                        </div>
                      ))}
                    </>
                  )}
                </button>
              );
            })}
          </div>

          {/* Save bar */}
          <div className="avail-save-bar mt-3">
            <button className="btn btn-success" onClick={saveAll} disabled={saving}>
              {saving ? "Saving..." : saved ? (<><E n="check" /> Saved!</>) : "Save Availability"}
            </button>
          </div>
        </>
      )}

      {/* Date modal — same tap-to-add as before */}
      {selectedDate && (
        <div className="avail-modal-backdrop" onClick={() => setSelectedDate(null)}>
          <div className="avail-modal" onClick={(e) => e.stopPropagation()}>
            <div className="avail-modal-header">
              <h5 className="m-0">{formatDateNice(selectedDate)}</h5>
              <button className="btn-close btn-close-white" onClick={() => setSelectedDate(null)} />
            </div>
            <div className="avail-modal-body">
              {availabilityMap[selectedDate] && availabilityMap[selectedDate].length > 0 && (
                <div className="mb-3">
                  <label className="form-label text-secondary small">Current time slots:</label>
                  {availabilityMap[selectedDate].map((slot, i) => (
                    <div key={i} className="avail-slot-chip">
                      <span>{formatTimeNice(slot.start_time)} – {formatTimeNice(slot.end_time)}</span>
                      <button className="avail-slot-remove" onClick={() => removeTimeSlot(selectedDate, i)} title="Remove">✕</button>
                    </div>
                  ))}
                </div>
              )}
              <label className="form-label text-secondary small">Add time slot:</label>
              <div className="avail-time-picker-row">
                <select className="form-select form-select-sm" value={editStartTime} onChange={(e) => setEditStartTime(e.target.value)}>
                  {TIME_OPTIONS.map((opt) => (<option key={opt.value} value={opt.value}>{opt.label}</option>))}
                </select>
                <span className="text-secondary">to</span>
                <select className="form-select form-select-sm" value={editEndTime} onChange={(e) => setEditEndTime(e.target.value)}>
                  {TIME_OPTIONS.map((opt) => (<option key={opt.value} value={opt.value}>{opt.label}</option>))}
                </select>
                <button className="btn btn-outline-primary btn-sm" onClick={addTimeSlot}><E n="plus" /> Add</button>
              </div>
              {editError && <p className="text-danger small mt-1 mb-0">{editError}</p>}
            </div>
            <div className="avail-modal-footer">
              <button className="btn btn-secondary btn-sm" onClick={() => setSelectedDate(null)}>Done</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}


