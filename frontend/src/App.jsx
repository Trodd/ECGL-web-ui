import { useEffect, useState, useRef } from "react";
import { Routes, Route, NavLink, Link, useNavigate, useLocation } from "react-router-dom";
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
import RulesPage from "./pages/Rules";
import LeagueMod from "./pages/LeagueMod";
import LeagueSettings from "./pages/LeagueSettings";

import "bootstrap/dist/css/bootstrap.min.css";
import "bootstrap/dist/js/bootstrap.bundle.min.js";
import "./styles.css";
import { getApiUrl } from "./config";
import DevToolbar from "./components/DevToolbar";

function App() {
  const navigate = useNavigate();
  const location = useLocation();
  const [user, setUser] = useState(null);
  const [season, setSeason] = useState("");
  const [showFinals, setShowFinals] = useState(false);
  const [loadingUser, setLoadingUser] = useState(true);
  const [notifCount, setNotifCount] = useState(0);

  // Dev impersonation
  const [impersonateId, setImpersonateId] = useState(
    () => sessionStorage.getItem("dev_impersonate") || null
  );
  const updateImpersonate = (id) => {
    if (id) sessionStorage.setItem("dev_impersonate", id);
    else sessionStorage.removeItem("dev_impersonate");
    setImpersonateId(id);
  };

  // Pull-to-refresh
  const [pullDistance, setPullDistance] = useState(0);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const pullStartY = useRef(0);
  const isPulling = useRef(false);

  // Account dropdown
  const [accountDropdownOpen, setAccountDropdownOpen] = useState(false);
  const mobileDropdownRef = useRef(null);
  const desktopDropdownRef = useRef(null);

  // Close account dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(e) {
      // Don't close if clicking inside either dropdown wrapper
      if (mobileDropdownRef.current && mobileDropdownRef.current.contains(e.target)) {
        return;
      }
      if (desktopDropdownRef.current && desktopDropdownRef.current.contains(e.target)) {
        return;
      }
      setAccountDropdownOpen(false);
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, []);

  useEffect(() => {
    function updateHeaderHeightVar() {
      const headerEl = document.querySelector(".ecgl-header");
      if (!headerEl) return;
      const h = Math.ceil(headerEl.getBoundingClientRect().height);
      document.documentElement.style.setProperty("--ecgl-header-height", `${h}px`);
    }

    const raf = window.requestAnimationFrame(updateHeaderHeightVar);
    window.addEventListener("resize", updateHeaderHeightVar);
    return () => {
      window.cancelAnimationFrame(raf);
      window.removeEventListener("resize", updateHeaderHeightVar);
    };
  }, [season]);

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
      if (Math.abs(dx) < 14 && Math.abs(dy) < 14) return;
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
      // Shorter swipe distance for mobile comfort.
      if (Math.abs(dx) < 55) return;

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

  // Pull-to-refresh gesture handling
  useEffect(() => {
    const PULL_THRESHOLD = 80;

    function onPullStart(e) {
      // Only start pull if at top of page and not in mobile nav
      if (window.scrollY > 5 || isMobileNavOpen() || isRefreshing) return;
      if (e.touches?.length !== 1) return;

      pullStartY.current = e.touches[0].clientY;
      isPulling.current = true;
    }

    function onPullMove(e) {
      if (!isPulling.current || isRefreshing) return;
      if (window.scrollY > 5) {
        isPulling.current = false;
        setPullDistance(0);
        return;
      }

      const currentY = e.touches[0].clientY;
      const distance = Math.max(0, currentY - pullStartY.current);

      // Apply resistance for a more natural feel
      const resistedDistance = Math.min(distance * 0.5, 120);
      setPullDistance(resistedDistance);
    }

    function onPullEnd() {
      if (!isPulling.current) return;
      isPulling.current = false;

      if (pullDistance >= PULL_THRESHOLD && !isRefreshing) {
        setIsRefreshing(true);
        setPullDistance(50); // Keep indicator visible during refresh

        // Reload the page
        setTimeout(() => {
          window.location.reload();
        }, 300);
      } else {
        setPullDistance(0);
      }
    }

    document.addEventListener("touchstart", onPullStart, { passive: true });
    document.addEventListener("touchmove", onPullMove, { passive: true });
    document.addEventListener("touchend", onPullEnd, { passive: true });

    return () => {
      document.removeEventListener("touchstart", onPullStart);
      document.removeEventListener("touchmove", onPullMove);
      document.removeEventListener("touchend", onPullEnd);
    };
  }, [pullDistance, isRefreshing]);

  useEffect(() => {
    // Fetch user (session-based)
    if (impersonateId) {
      // When impersonating: fetch real user for dev flags, then impersonated user for view
      Promise.all([
        fetch(`${getApiUrl()}/api/me`, { credentials: "include" }).then(r => r.ok ? r.json() : null),
        fetch(`${getApiUrl()}/api/me?as=${impersonateId}`, { credentials: "include" }).then(r => r.ok ? r.json() : null),
      ])
        .then(([realUser, impUser]) => {
          if (impUser) {
            impUser.is_dev = realUser?.is_dev || false;
            impUser.is_mod = realUser?.is_mod || false;
            impUser.dev_mode = realUser?.dev_mode || false;
          }
          setUser(impUser);
        })
        .catch(() => setUser(null))
        .finally(() => setLoadingUser(false));
    } else {
      fetch(`${getApiUrl()}/api/me`, { credentials: "include" })
        .then((res) => (res.ok ? res.json() : null))
        .then((data) => setUser(data))
        .catch(() => setUser(null))
        .finally(() => setLoadingUser(false));
    }

    // Fetch season
    fetch(`${getApiUrl()}/api/season`)
      .then((res) => res.json())
      .then((data) => setSeason(data.season))
      .catch(() => setSeason("Unknown"));

    // Fetch finals visibility
    fetch(`${getApiUrl()}/api/finals/visible`)
      .then(res => res.json())
      .then(data => setShowFinals(data))
      .catch(() => setShowFinals(null));

  }, []);

  // Poll notification count
  useEffect(() => {
    if (!user) { setNotifCount(0); return; }
    const asQ = impersonateId ? `?as=${impersonateId}` : "";
    const fetchCount = () => {
      fetch(`${getApiUrl()}/api/notifications/count${asQ}`, { credentials: "include" })
        .then(res => res.ok ? res.json() : null)
        .then(data => { if (data) setNotifCount(data.unread_count || 0); })
        .catch(() => { });
    };
    fetchCount();
    const interval = setInterval(fetchCount, 30000);
    return () => clearInterval(interval);
  }, [user, impersonateId]);

  // Clear notifications when visiting My Team
  useEffect(() => {
    if (location.pathname === "/myteam" && notifCount > 0) {
      const asQ = impersonateId ? `?as=${impersonateId}` : "";
      fetch(`${getApiUrl()}/api/notifications/read-all${asQ}`, { method: "POST", credentials: "include" }).catch(() => { });
      setNotifCount(0);
    }
  }, [location.pathname]);

  useEffect(() => {
    function handleVisibilityUpdate() {
      fetch(`${getApiUrl()}/api/finals/visible`)
        .then(res => res.json())
        .then(data => setShowFinals(data))
        .catch(() => setShowFinals(null));
    }

    window.addEventListener("finals-visibility-updated", handleVisibilityUpdate);

    return () => {
      window.removeEventListener("finals-visibility-updated", handleVisibilityUpdate);
    };
  }, []);

  // Determine if Finals tab should show
  const finalsTabVisible = showFinals?.visible || (user?.is_mod && showFinals?.mod_visible);

  return (
    <div className="app-wrapper">
      {/* Pull-to-refresh indicator - slides down from top */}
      <div className={`pull-to-refresh-zone ${pullDistance > 20 || isRefreshing ? 'visible' : ''}`}>
        <div className={`ptr-spinner ${isRefreshing ? 'spinning' : ''}`}>
          {isRefreshing ? '↻' : '↓'}
        </div>
        <span className="ptr-text">
          {isRefreshing ? 'Refreshing...' : pullDistance >= 80 ? 'Release to refresh' : 'Pull to refresh'}
        </span>
      </div>

      {/* Main content */}
      <div className="app-content">
        {/* === Header === */}
        <header className="ecgl-header text-center">
          <div className="d-flex align-items-center justify-content-between">
            <button
              type="button"
              className="btn btn-outline-light btn-sm d-md-none ecgl-mobile-nav-trigger"
              aria-controls="mobileMainNav"
              aria-label="Open navigation"
              onClick={openMobileNav}
            >
              ☰
            </button>
            <div className="flex-grow-1">
              <h1 className="m-0">⚡ Echo Combat George League</h1>
            </div>
            <div style={{ width: 44 }} className="d-md-none" />
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
                className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/" ? "mobile-nav-active" : ""}`}
                onClick={() => navigateFromMobile("/")}
              >
                🏠 Home
              </button>
              <button
                type="button"
                className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/rules" ? "mobile-nav-active" : ""}`}
                onClick={() => navigateFromMobile("/rules")}
              >
                📜 Rules
              </button>

              {user && (
                <>
                  <button
                    type="button"
                    className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/register" ? "mobile-nav-active" : ""}`}
                    onClick={() => navigateFromMobile("/register")}
                  >
                    📝 Register
                  </button>
                  <button
                    type="button"
                    className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/myteam" ? "mobile-nav-active" : ""}`}
                    onClick={() => navigateFromMobile("/myteam")}
                  >
                    🧑 My Team
                    {notifCount > 0 && <span className="badge bg-danger rounded-pill ms-2">{notifCount}</span>}
                  </button>
                </>
              )}

              <button
                type="button"
                className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/players" ? "mobile-nav-active" : ""}`}
                onClick={() => navigateFromMobile("/players")}
              >
                📋 Players
              </button>
              <button
                type="button"
                className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/teams" ? "mobile-nav-active" : ""}`}
                onClick={() => navigateFromMobile("/teams")}
              >
                👥 Teams
              </button>

              {finalsTabVisible && (
                <button
                  type="button"
                  className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/finals" ? "mobile-nav-active" : ""}`}
                  onClick={() => navigateFromMobile("/finals")}
                >
                  🏆 Finals
                </button>
              )}

              <button
                type="button"
                className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/matchups" ? "mobile-nav-active" : ""}`}
                onClick={() => navigateFromMobile("/matchups")}
              >
                📅 Matchups
              </button>
              <button
                type="button"
                className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/leaderboard" ? "mobile-nav-active" : ""}`}
                onClick={() => navigateFromMobile("/leaderboard")}
              >
                🏆 Leaderboard
              </button>

              {!loadingUser && user?.is_mod && (
                <button
                  type="button"
                  className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/modpanel" ? "mobile-nav-active" : ""}`}
                  onClick={() => navigateFromMobile("/modpanel")}
                >
                  🛠️ League Mod
                </button>
              )}
              {!loadingUser && user?.is_dev && (
                <button
                  type="button"
                  className={`list-group-item list-group-item-action bg-dark text-light ${location.pathname === "/settings" ? "mobile-nav-active" : ""}`}
                  onClick={() => navigateFromMobile("/settings")}
                >
                  ⚙️ Settings
                </button>
              )}
            </div>

            {/* Auth section with divider */}
            <div className="mobile-auth-section mt-3 pt-3 border-top border-secondary">
              {user ? (
                <div className={`account-dropdown-wrapper ${accountDropdownOpen ? 'open' : ''}`} ref={mobileDropdownRef}>
                  {/* Discord Account Card - Clickable */}
                  <div
                    className="discord-account-card d-flex align-items-center clickable"
                    onClick={() => setAccountDropdownOpen(!accountDropdownOpen)}
                  >
                    <img
                      src={
                        user.avatar
                          ? `https://cdn.discordapp.com/avatars/${user.id}/${user.avatar}.png?size=64`
                          : `https://cdn.discordapp.com/embed/avatars/${(BigInt(user.id) >> 22n) % 6n}.png`
                      }
                      alt="Avatar"
                      className="discord-avatar rounded-circle me-3"
                      width="48"
                      height="48"
                    />
                    <div className="discord-user-info overflow-hidden flex-grow-1">
                      <div className="discord-display-name text-light fw-semibold text-truncate">
                        {user.display_name || user.username}
                      </div>
                      <div className="discord-username text-secondary small text-truncate">
                        @{user.username}
                      </div>
                    </div>
                    <span className={`dropdown-chevron ${accountDropdownOpen ? 'open' : ''}`}>▼</span>
                  </div>
                  {/* Dropdown menu */}
                  {accountDropdownOpen && (
                    <div className="account-dropdown-menu">
                      <button
                        type="button"
                        className="account-dropdown-item"
                        onClick={() => { window.location.href = `${getApiUrl()}/logout`; }}
                      >
                        🚪 Logout
                      </button>
                    </div>
                  )}
                </div>
              ) : (
                <a
                  className="list-group-item list-group-item-action bg-dark text-light"
                  style={{ borderRadius: '8px' }}
                  href={`${getApiUrl()}/login`}
                >
                  🔑 Login with Discord
                </a>
              )}
            </div>
          </div>
        </div>

        <div className="ecgl-shell">
          {/* === Desktop sidebar nav === */}
          <aside className="ecgl-side-nav d-none d-md-flex flex-column">
            <div className="list-group list-group-flush">
              <NavLink
                to="/"
                end
                className={({ isActive }) =>
                  `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                  }`
                }
              >
                🏠 Home
              </NavLink>

              <NavLink
                to="/rules"
                className={({ isActive }) =>
                  `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                  }`
                }
              >
                📜 Rules
              </NavLink>

              {user && (
                <>
                  <NavLink
                    to="/register"
                    className={({ isActive }) =>
                      `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                      }`
                    }
                  >
                    📝 Register
                  </NavLink>
                  <NavLink
                    to="/myteam"
                    className={({ isActive }) =>
                      `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                      }`
                    }
                  >
                    🧑 My Team
                    {notifCount > 0 && <span className="badge bg-danger rounded-pill ms-2" style={{ fontSize: 10 }}>{notifCount}</span>}
                  </NavLink>
                </>
              )}

              <NavLink
                to="/players"
                className={({ isActive }) =>
                  `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                  }`
                }
              >
                📋 Players
              </NavLink>

              <NavLink
                to="/teams"
                className={({ isActive }) =>
                  `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                  }`
                }
              >
                👥 Teams
              </NavLink>

              {finalsTabVisible && (
                <NavLink
                  to="/finals"
                  className={({ isActive }) =>
                    `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                    }`
                  }
                >
                  🏆 Finals
                </NavLink>
              )}

              <NavLink
                to="/matchups"
                className={({ isActive }) =>
                  `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                  }`
                }
              >
                📅 Matchups
              </NavLink>

              <NavLink
                to="/leaderboard"
                className={({ isActive }) =>
                  `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                  }`
                }
              >
                🏆 Leaderboard
              </NavLink>

              {!loadingUser && user?.is_mod && (
                <NavLink
                  to="/modpanel"
                  className={({ isActive }) =>
                    `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                    }`
                  }
                >
                  🛠️ League Mod
                </NavLink>
              )}
              {!loadingUser && user?.is_dev && (
                <NavLink
                  to="/settings"
                  className={({ isActive }) =>
                    `list-group-item list-group-item-action ecgl-side-link${isActive ? " active" : ""
                    }`
                  }
                >
                  ⚙️ Settings
                </NavLink>
              )}
            </div>

            {/* Auth section - right under nav tabs */}
            <div className="ecgl-side-auth-section mt-3 pt-3 border-top border-secondary">
              {user ? (
                <div className={`account-dropdown-wrapper ${accountDropdownOpen ? 'open' : ''}`} ref={desktopDropdownRef}>
                  {/* Discord Account Card - Clickable */}
                  <div
                    className="discord-account-card d-flex align-items-center px-2 clickable"
                    onClick={() => setAccountDropdownOpen(!accountDropdownOpen)}
                  >
                    <img
                      src={
                        user.avatar
                          ? `https://cdn.discordapp.com/avatars/${user.id}/${user.avatar}.png?size=64`
                          : `https://cdn.discordapp.com/embed/avatars/${(BigInt(user.id) >> 22n) % 6n}.png`
                      }
                      alt="Avatar"
                      className="discord-avatar rounded-circle me-2"
                      width="40"
                      height="40"
                    />
                    <div className="discord-user-info overflow-hidden flex-grow-1">
                      <div className="discord-display-name text-light fw-semibold text-truncate">
                        {user.display_name || user.username}
                      </div>
                      <div className="discord-username text-secondary small text-truncate">
                        @{user.username}
                      </div>
                    </div>
                    <span className={`dropdown-chevron ${accountDropdownOpen ? 'open' : ''}`}>▼</span>
                  </div>
                  {/* Dropdown menu */}
                  {accountDropdownOpen && (
                    <div className="account-dropdown-menu">
                      <button
                        type="button"
                        className="account-dropdown-item"
                        onClick={() => { window.location.href = `${getApiUrl()}/logout`; }}
                      >
                        🚪 Logout
                      </button>
                    </div>
                  )}
                </div>
              ) : (
                <a
                  className="list-group-item list-group-item-action ecgl-side-link ecgl-side-auth"
                  href={`${getApiUrl()}/login`}
                >
                  🔑 Login with Discord
                </a>
              )}
            </div>
          </aside>

          {/* === Page Content === */}
          <div className="ecgl-content">
            <div className="page-content">
              <ErrorBoundary>
                <Routes>
                  <Route path="/" element={<Home user={user} />} />
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
                  <Route path="/rules" element={<RulesPage />} />

                  {/* 🛠️ League Mod Route (extra protected inside component) */}
                  <Route path="/modpanel" element={<LeagueMod />} />
                  <Route path="/settings" element={<LeagueSettings />} />
                </Routes>
              </ErrorBoundary>
            </div>
          </div>
        </div>
      </div>{/* end app-content */}

      {/* Dev impersonation toolbar */}
      {user?.is_dev && user?.dev_mode && (
        <DevToolbar
          impersonateId={impersonateId}
          setImpersonateId={updateImpersonate}
        />
      )}
    </div>
  );
}

export default App;
