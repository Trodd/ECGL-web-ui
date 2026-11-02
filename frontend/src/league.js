import axios from "axios";

/**
 * league — tracks which league the app is currently viewing ("main" or
 * "rookie") and attaches it to every API request via the X-ECGL-League header.
 *
 * Switching leagues performs a full reload so every page refetches its data
 * under the new league (the backend filters all reads by this header).
 */

const STORAGE_KEY = "ecgl_league";

export const LEAGUES = [
  { id: "main", label: "Main League" },
  { id: "rookie", label: "Rookie League" },
];

function normalize(v) {
  return String(v || "").toLowerCase() === "rookie" ? "rookie" : "main";
}

let current = "main";

export function getLeague() {
  return current;
}

export function setLeague(v) {
  current = normalize(v);
  try {
    localStorage.setItem(STORAGE_KEY, current);
  } catch {
    /* ignore storage errors (private mode, etc.) */
  }
  axios.defaults.headers.common["X-ECGL-League"] = current;
  window.dispatchEvent(new CustomEvent("ecgl-league-changed", { detail: current }));
}

export function leagueLabel(id = current) {
  const found = LEAGUES.find((l) => l.id === id);
  return found ? found.label : "Main League";
}

export function initLeague() {
  try {
    current = normalize(localStorage.getItem(STORAGE_KEY) || "main");
  } catch {
    current = "main";
  }

  // Attach the league to every axios request.
  axios.defaults.headers.common["X-ECGL-League"] = current;

  // Attach it to native fetch calls too (some components use fetch directly).
  const origFetch = window.fetch.bind(window);
  window.fetch = (input, init) => {
    try {
      const baseHeaders =
        (init && init.headers) ||
        (input && typeof input !== "string" && input.headers) ||
        undefined;
      const headers = new Headers(baseHeaders || undefined);
      headers.set("X-ECGL-League", current);
      return origFetch(input, { ...(init || {}), headers });
    } catch {
      return origFetch(input, init);
    }
  };
}
