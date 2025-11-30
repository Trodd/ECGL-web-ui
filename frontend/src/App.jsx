import { useEffect, useState } from "react";
import { Routes, Route, NavLink, Link } from "react-router-dom";
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
  const [user, setUser] = useState(null);
  const [season, setSeason] = useState("");
  const [showFinals, setShowFinals] = useState(false);
  const [loadingUser, setLoadingUser] = useState(true);

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
        <h1>⚡ Echo Combat George League</h1>
        <p className="season-text">
          📅 {season !== "" ? `Season ${season}` : "Loading..."}
        </p>
      </header>

      {/* === Navbar === */}
      <ul className="ecgl-tabs nav nav-tabs">
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
