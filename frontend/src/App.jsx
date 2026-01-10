import { useEffect, useState } from "react";
import { Routes, Route, NavLink, Link, useNavigate } from "react-router-dom";
import { Offcanvas } from "bootstrap";
import ErrorBoundary from "./ErrorBoundary";
import Home from "./pages/Home";
import Teams from "./pages/Teams";
import TeamDetail from "./pages/TeamDetail";
import Leaderboard from "./pages/Leaderboard";
import MyTeam from "./pages/MyTeam";
import Register from "./pages/Register";
import Players from "./pages/Players";
import Matchups from "./pages/Matchups";
import PlayerDetail from "./pages/PlayerDetail";
import MatchDetail from "./pages/MatchDetail";
import Finals from "./pages/Finals";
import LeagueMod from "./pages/LeagueMod";

import "bootstrap/dist/css/bootstrap.min.css";
import "bootstrap/dist/js/bootstrap.bundle.min.js";
import "./styles.css";

function App() {
  const navigate = useNavigate();
  const [user, setUser] = useState(null);
  const [season, setSeason] = useState("");
  const [showFinals, setShowFinals] = useState(false);
  const [loadingUser, setLoadingUser] = useState(true);

  function getMobileNavElement() {
    return document.getElementById("mobileMainNav");
  }

  function isMobileNavOpen() {
    const el = getMobileNavElement();
    return !!el && el.classList.contains("show");
  }

  function openMobileNav() {
    const el = getMobileNavElement();
    if (!el) return;
    const inst = Offcanvas.getOrCreateInstance(el);
    inst.show();
  }

  function closeMobileNav() {
    const el = getMobileNavElement();
    if (!el) return;
    const inst = Offcanvas.getOrCreateInstance(el);
    inst.hide();
  }

  function navigateFromMobile(path) {
    navigate(path);
    window.setTimeout(() => closeMobileNav(), 0);
  }

  useEffect(() => {
    // Restore scroll on load
    const savedScroll = sessionStorage.getItem("scroll-position");
    if (savedScroll !== null) {
      window.scrollTo(0, Number(savedScroll));
    }

    // Save scroll location on every scroll
    const onScroll = () => {
      sessionStorage.setItem("scroll-position", window.scrollY);
    };

    window.addEventListener("scroll", onScroll);

    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  // Mobile: swipe anywhere — right to open, left to close.
  // Guarded to avoid firing during normal vertical scrolling.
  useEffect(() => {
    let startX = 0;
    let startY = 0;
    let started = false;
    let ignore = false;
    let decided = false;
    let horizontal = false;

    function isInteractiveTarget(target) {
      if (!target || !(target instanceof Element)) return false;
      return !!target.closest(
        "input, textarea, select, button, a, [role='button'], [contenteditable='true']"
      );
    }

    function onTouchStart(e) {
      if (e.touches?.length !== 1) return;
      const t = e.touches[0];
      startX = t.clientX;
      startY = t.clientY;
      started = true;
      ignore = isInteractiveTarget(e.target);
      decided = false;
      horizontal = false;
    }

    function onTouchMove(e) {
      if (!started || ignore || decided) return;
      const t = e.touches?.[0];
      if (!t) return;

      const dx = t.clientX - startX;
      const dy = t.clientY - startY;

      // Decide direction after a small movement threshold.
      if (Math.abs(dx) < 18 && Math.abs(dy) < 18) return;
      decided = true;
      horizontal = Math.abs(dx) > Math.abs(dy) * 1.2;
    }

    function onTouchEnd(e) {
      if (!started) return;
      started = false;
      if (ignore) return;

      const t = e.changedTouches?.[0];
      if (!t) return;

      const dx = t.clientX - startX;
      const dy = t.clientY - startY;

      // Only treat as a nav gesture when strongly horizontal.
      const isHorizontalGesture = horizontal || Math.abs(dx) > Math.abs(dy) * 1.2;
      if (!isHorizontalGesture) return;
      if (Math.abs(dx) < 80) return;

      if (!isMobileNavOpen() && dx > 0) {
        openMobileNav();
      } else if (isMobileNavOpen() && dx < 0) {
        closeMobileNav();
      }
    }

    window.addEventListener("touchstart", onTouchStart, { passive: true, capture: true });
    window.addEventListener("touchmove", onTouchMove, { passive: true, capture: true });
    window.addEventListener("touchend", onTouchEnd, { passive: true, capture: true });
    return () => {
      window.removeEventListener("touchstart", onTouchStart, true);
      window.removeEventListener("touchmove", onTouchMove, true);
      window.removeEventListener("touchend", onTouchEnd, true);
    };
  }, []);

  useEffect(() => {
    // Fetch user (session-based)
    fetch(`${import.meta.env.VITE_API_URL}/api/me`, {
      credentials: "include",
    })
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => setUser(data))
      .catch(() => setUser(null))
      .finally(() => setLoadingUser(false));

    // Fetch season
    fetch(`${import.meta.env.VITE_API_URL}/api/season`)
      .then((res) => res.json())
      .then((data) => setSeason(data.season))
      .catch(() => setSeason("Unknown"));

    // Fetch finals visibility
    fetch(`${import.meta.env.VITE_API_URL}/api/finals/visible`)
      .then(res => res.json())
      .then(data => setShowFinals(data.visible))
      .catch(() => setShowFinals(false));

  }, []);

  useEffect(() => {
    function handleVisibilityUpdate() {
      fetch(`${import.meta.env.VITE_API_URL}/api/finals/visible`)
        .then(res => res.json())
        .then(data => setShowFinals(data.visible))
        .catch(() => setShowFinals(false));
    }

    window.addEventListener("finals-visibility-updated", handleVisibilityUpdate);

    return () => {
      window.removeEventListener("finals-visibility-updated", handleVisibilityUpdate);
    };
  }, []);

  return (
    <div className="app-wrapper">
      {/* === Header === */}
      <header className="ecgl-header text-center">
        <div className="d-flex align-items-center justify-content-between">
          <div style={{ width: 44 }} className="d-md-none" />
          <div className="flex-grow-1">
            <h1 className="m-0">⚡ Echo Combat George League</h1>
          </div>
          <button
            type="button"
            className="btn btn-outline-light btn-sm d-md-none"
            aria-controls="mobileMainNav"
            aria-label="Open navigation"
            onClick={openMobileNav}
          >
            ☰
          </button>
        </div>
        <p className="season-text">
          📅 {season !== "" ? `Season ${season}` : "Loading..."}
        </p>
      </header>

      {/* === Mobile slide-out nav === */}
      <div
        className="offcanvas offcanvas-start text-bg-dark d-md-none"
        tabIndex={-1}
        id="mobileMainNav"
        aria-labelledby="mobileMainNavLabel"
      >
        <div className="offcanvas-header">
          <h5 className="offcanvas-title" id="mobileMainNavLabel">
            Navigation
          </h5>
          <button
            type="button"
            className="btn-close btn-close-white"
            aria-label="Close"
            onClick={closeMobileNav}
          />
        </div>
        <div className="offcanvas-body">
          <div className="list-group list-group-flush">
            <button
              type="button"
              className="list-group-item list-group-item-action bg-dark text-light"
              onClick={() => navigateFromMobile("/")}
            >
              🏠 Home
            </button>

            {user && (
              <>
                <button
                  type="button"
                  className="list-group-item list-group-item-action bg-dark text-light"
                  onClick={() => navigateFromMobile("/register")}
                >
                  📝 Register
                </button>
                <button
                  type="button"
                  className="list-group-item list-group-item-action bg-dark text-light"
                  onClick={() => navigateFromMobile("/myteam")}
                >
                  🧑 My Team
                </button>
              </>
            )}

            <button
              type="button"
              className="list-group-item list-group-item-action bg-dark text-light"
              onClick={() => navigateFromMobile("/players")}
            >
              📋 Players
            </button>
            <button
              type="button"
              className="list-group-item list-group-item-action bg-dark text-light"
              onClick={() => navigateFromMobile("/teams")}
            >
              👥 Teams
            </button>

            {showFinals && (
              <button
                type="button"
                className="list-group-item list-group-item-action bg-dark text-light"
                onClick={() => navigateFromMobile("/finals")}
              >
                🏆 Finals
              </button>
            )}

            <button
              type="button"
              className="list-group-item list-group-item-action bg-dark text-light"
              onClick={() => navigateFromMobile("/matchups")}
            >
              📅 Matchups
            </button>
            <button
              type="button"
              className="list-group-item list-group-item-action bg-dark text-light"
              onClick={() => navigateFromMobile("/leaderboard")}
            >
              🏆 Leaderboard
            </button>

            {!loadingUser && user?.is_mod && (
              <button
                type="button"
                className="list-group-item list-group-item-action bg-dark text-light"
                onClick={() => navigateFromMobile("/modpanel")}
              >
                🛠️ League Mod
              </button>
            )}

            <div className="mt-3" />
            {user ? (
              <a
                className="btn btn-outline-light w-100"
                href={`${import.meta.env.VITE_API_URL}/logout`}
              >
                🚪 Logout {user.display_name || user.username}
              </a>
            ) : (
              <a
                className="btn btn-outline-light w-100"
                href={`${import.meta.env.VITE_API_URL}/login`}
              >
                🔑 Login
              </a>
            )}
          </div>
        </div>
      </div>

      {/* === Desktop navbar tabs === */}
      <ul className="ecgl-tabs nav nav-tabs d-none d-md-flex">
        <li className="nav-item">
          <NavLink to="/" end className="nav-link">
            🏠 Home
          </NavLink>
        </li>

        {user && (
          <>
            <li className="nav-item">
              <NavLink to="/register" className="nav-link">
                📝 Register
              </NavLink>
            </li>
            <li className="nav-item">
              <NavLink to="/myteam" className="nav-link">
                🧑 My Team
              </NavLink>
            </li>
          </>
        )}

        <li className="nav-item">
          <NavLink to="/players" className="nav-link">
            📋 Players
          </NavLink>
        </li>

        <li className="nav-item">
          <NavLink to="/teams" className="nav-link">
            👥 Teams
          </NavLink>
        </li>

        {showFinals && (
          <li className="nav-item">
            <NavLink to="/finals" className="nav-link">
              🏆 Finals
            </NavLink>
          </li>
        )}

        <li className="nav-item">
          <NavLink to="/matchups" className="nav-link">
            📅 Matchups
          </NavLink>
        </li>

        <li className="nav-item">
          <NavLink to="/leaderboard" className="nav-link">
            🏆 Leaderboard
          </NavLink>
        </li>

        {/*<li className="nav-item">
          <NavLink to="/finals" className="nav-link">
            🏁 Finals
          </NavLink>
        </li>*/}

        {/* 🔒 League Mod tab only visible to League Mods */}
        {!loadingUser && user?.is_mod && (
          <li className="nav-item">
            <NavLink to="/modpanel" className="nav-link">
              🛠️ League Mod
            </NavLink>
          </li>
        )}

        {/* 🚪 Login/Logout (always far right) */}
        <li className="nav-item ms-auto">
          {user ? (
            <a
              className="nav-link"
              href={`${import.meta.env.VITE_API_URL}/logout`}
            >
              🚪 Logout {user.display_name || user.username}
            </a>
          ) : (
            <a
              className="nav-link"
              href={`${import.meta.env.VITE_API_URL}/login`}
            >
              🔑 Login
            </a>
          )}
        </li>
      </ul>

      {/* === Page Content === */}
      <div className="page-content">
        <ErrorBoundary>
          <Routes>
            <Route path="/" element={<Home />} />
            <Route path="/register" element={<Register />} />
            <Route path="/teams" element={<Teams />} />
            <Route path="/teams/:id" element={<TeamDetail />} />
            <Route path="/matchups" element={<Matchups />} />
            <Route path="/match/:id" element={<MatchDetail />} />
            <Route path="/leaderboard" element={<Leaderboard />} />
            <Route path="/myteam" element={<MyTeam />} />
            <Route path="/players" element={<Players />} />
            <Route path="/players/:id" element={<PlayerDetail />} />
            <Route path="/finals" element={<Finals />} />

            {/* 🛠️ League Mod Route (extra protected inside component) */}
            <Route path="/modpanel" element={<LeagueMod />} />
          </Routes>
        </ErrorBoundary>
      </div>
    </div>
  );
}

export default App;
