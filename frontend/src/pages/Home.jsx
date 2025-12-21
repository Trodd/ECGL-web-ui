import { useEffect, useState } from "react";
import axios from "axios";
import { Link } from "react-router-dom";
import FullSeasonCalendar from "../components/SeasonCalendar";
import Rules from "../components/Rules";

export default function Home() {
  const urlBase = import.meta.env.VITE_API_URL;
  const [upcoming, setUpcoming] = useState([]);

  // =====================================================
  // Load upcoming matches (same source as Matchups tab)
  // =====================================================
  useEffect(() => {
    let cancelled = false;

    async function loadUpcoming() {
      try {
        const res = await axios.get(
          `${urlBase}/api/matches/public`,
          { withCredentials: true }
        );

        const raw = res.data?.matches;
        if (!raw || typeof raw !== "object") {
          setUpcoming([]);
          return;
        }

        const now = Date.now();
        const collected = [];

        Object.values(raw).forEach((weeks) => {
          if (!weeks || typeof weeks !== "object") return;

          Object.values(weeks).forEach((list) => {
            if (!Array.isArray(list)) return;

            list.forEach((m) => {
              if (!m || typeof m !== "object") return;

              // ✅ accept multiple date fields
              const dateStr =
                m.date ||
                m.scheduled_date ||
                m.scheduled_time ||
                m.match_time ||
                m.start_time;

              if (!dateStr) return;

              const ts = new Date(dateStr).getTime();
              if (isNaN(ts) || ts < now) return;

              const hasCasters = m.cast_active === true;

              collected.push({
                id: m.id,
                date: dateStr,

                team_a:
                  m.team_a_name ||
                  m.team_a ||
                  m.teamA ||
                  "TBD",

                team_b:
                  m.team_b_name ||
                  m.team_b ||
                  m.teamB ||
                  "TBD",

                isFinals: !!m.is_finals || !!m.finals,
                isLive: hasCasters,
              });
            });
          });
        });

        collected.sort((a, b) => new Date(a.date) - new Date(b.date));

        setUpcoming(collected.slice(0, 6));
      } catch (err) {
        console.error("Failed to load upcoming matches", err);
        setUpcoming([]);
      }
    }

    loadUpcoming();
    return () => { cancelled = true; };
  }, [urlBase]);

  // =====================================================
  // Helpers
  // =====================================================
  function relativeTime(dateStr) {
    const diff = new Date(dateStr) - Date.now();
    if (diff <= 0) return "Starting soon";

    const mins = Math.floor(diff / 60000);
    const hrs = Math.floor(mins / 60);
    const days = Math.floor(hrs / 24);

    if (days > 0) return `Starts in ${days}d`;
    if (hrs > 0) return `Starts in ${hrs}h ${mins % 60}m`;
    return `Starts in ${mins}m`;
  }

  return (
    <div className="container text-light py-4" style={{ maxWidth: "1100px" }}>

      {/* =====================================================
          HERO
      ====================================================== */}
      <div
        className="p-4 mb-4 text-center rounded"
        style={{
          background: "linear-gradient(180deg, #1b1b1b, #111)",
          border: "1px solid #2a2a2a",
        }}
      >
        <h1 className="mb-2">📢 Welcome to the Echo Combat George League!</h1>
        <p className="text-secondary mb-3">
          Your guide to format, rules, and expectations for ECGL.
        </p>

        <div className="d-flex flex-wrap justify-content-center gap-3">
          {/* 🔑 Discord Login */}
          <a
            href={`/login`}
            className="btn d-flex align-items-center gap-2"
            style={{
              backgroundColor: "#5865F2",
              color: "white",
              fontWeight: 600,
              border: "none",
              padding: "10px 16px",
            }}
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              width="20"
              height="20"
              fill="currentColor"
              viewBox="0 0 24 24"
            >
              <path d="M20.317 4.369a19.791 19.791 0 0 0-4.885-1.515.074.074 0 0 0-.078.037c-.211.375-.444.864-.608 1.249a18.27 18.27 0 0 0-5.487 0 12.64 12.64 0 0 0-.617-1.249.077.077 0 0 0-.078-.037 19.736 19.736 0 0 0-4.885 1.515.07.07 0 0 0-.032.027C.533 9.045-.319 13.58.099 18.057a.082.082 0 0 0 .031.056 19.9 19.9 0 0 0 5.993 3.03.077.077 0 0 0 .084-.027c.461-.63.873-1.295 1.226-1.994a.076.076 0 0 0-.041-.105 13.201 13.201 0 0 1-1.872-.9.077.077 0 0 1-.008-.128c.126-.095.252-.192.371-.291a.074.074 0 0 1 .077-.01c3.927 1.793 8.18 1.793 12.061 0a.074.074 0 0 1 .078.01c.12.099.245.196.372.291a.077.077 0 0 1-.006.128 12.299 12.299 0 0 1-1.873.899.076.076 0 0 0-.04.106c.36.698.772 1.363 1.225 1.993a.076.076 0 0 0 .084.028 19.876 19.876 0 0 0 6.002-3.03.077.077 0 0 0 .031-.055c.5-5.177-.838-9.673-3.548-13.66a.061.061 0 0 0-.031-.03ZM8.02 15.331c-1.183 0-2.156-1.085-2.156-2.419 0-1.333.955-2.418 2.156-2.418 1.21 0 2.175 1.095 2.156 2.418 0 1.334-.955 2.419-2.156 2.419Zm7.974 0c-1.183 0-2.156-1.085-2.156-2.419 0-1.333.955-2.418 2.156-2.418 1.21 0 2.175 1.095 2.156 2.418 0 1.334-.946 2.419-2.156 2.419Z" />
            </svg>

            Login with Discord
          </a>

          {/* 📝 Sign Up */}
          <a href="/register" className="btn ecgl-btn btn-primary">
            📝 Sign Up
          </a>

          {/* 👥 Teams */}
          <a href="/teams" className="btn ecgl-btn btn-outline-light">
            👥 Teams
          </a>

          {/* 📊 Leaderboard */}
          <a href="/leaderboard" className="btn ecgl-btn btn-outline-info">
            📊 Leaderboard
          </a>
        </div>
      </div>

      {/* =====================================================
          MAIN GRID
      ====================================================== */}
      <div className="row g-4">

        {/* LEFT COLUMN */}
        <div className="col-lg-7">

          {/* UPCOMING MATCHES */}
          <div
            className="p-3 rounded mb-4"
            style={{ background: "#151515", border: "1px solid #2a2a2a" }}
          >
            <h4>🗓️ Upcoming Matches</h4>

            {upcoming.length === 0 ? (
              <p className="text-secondary mt-2">
                No upcoming matches scheduled.
              </p>
            ) : (
              <div className="mt-3">
                {upcoming.map((m) => {
                  const isFinals = !!m.isFinals;

                  return (
                    <Link
                      key={m.id}
                      to={`/match/${m.id}`}
                      className="d-block text-decoration-none text-light mb-2"
                    >
                      <div
                        className="p-2 rounded d-flex justify-content-between align-items-center"
                        style={{
                          background: "#1c1c1c",
                          border: "1px solid #333",
                        }}
                      >
                        <div>
                          <div className="fw-semibold">
                            {m.isFinals && "🏆 FINALS — "}
                            {m.team_a} vs {m.team_b}

                            {m.isLive && (
                              <span
                                className="badge bg-danger ms-2"
                                style={{ fontSize: "0.7rem" }}
                              >
                                LIVE
                              </span>
                            )}
                          </div>
                          <small className="text-secondary">
                            {relativeTime(m.date)}
                          </small>
                        </div>

                        <small className="text-secondary">
                          {new Date(m.date).toLocaleString()}
                        </small>
                      </div>
                    </Link>
                  );
                })}
              </div>
            )}

            <a href="/matchups" className="d-inline-block mt-2 text-info">
              View full matchups →
            </a>
          </div>

        </div>

        {/* RIGHT COLUMN */}
        <div className="col-lg-5">

          {/* LEAGUE SNAPSHOT */}
          <div
            className="p-3 rounded mb-4"
            style={{ background: "#151515", border: "1px solid #2a2a2a" }}
          >
            <h4>📊 League Snapshot</h4>
            <ul className="mt-3">
              <li>🎮 Format: <b>3v3</b> (4v4 optional)</li>
              <li>🗺️ Best-of-3 maps</li>
              <li>📈 ELO-based rankings</li>
              <li>👥 Teams: 3–5 players</li>
              <li>⚔️ 2 matches per week</li>
            </ul>
          </div>

          {/* PLATFORM NOTICE */}
          <div className="alert alert-warning small">
            ⚠️ PCVR ONLY — SteamVR, Oculus PC, or Quest via Link/Air Link.<br />
            ❌ Quest-native is not supported.
          </div>
        </div>
      </div>

      {/* =====================================================
          RULES (COLLAPSED — FULL CONTENT)
      ====================================================== */}
      <details
        className="mt-5 p-3 rounded"
        style={{ background: "#121212", border: "1px solid #2a2a2a" }}
      >
        <summary style={{ cursor: "pointer", fontSize: "1.1rem" }}>
          📜 League Rules & Format (click to expand)
        </summary>

        <div className="mt-4">
          <Rules />
        </div>
      </details>

      {/* =====================================================
          CALENDAR
      ====================================================== */}
      <div className="mt-5">
        <h3>🗓️ Season Timeline</h3>
        <FullSeasonCalendar />
      </div>

    </div>
  );
}
