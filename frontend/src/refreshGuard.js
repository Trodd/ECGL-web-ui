import axios from "axios";
import { getApiUrl } from "./config";

/**
 * refreshGuard — blocks an action only when data *relevant to that action*
 * has changed since this tab loaded, so unrelated updates elsewhere no longer
 * force a full reload.
 *
 * How it works:
 *  - The backend keeps a version per resource scope (matches, teams, players,
 *    settings, media) — see GET /api/version.
 *  - Each mutating request is mapped to the scope(s) it affects (from its URL).
 *  - Before that request we compare only those scopes against the baselines we
 *    recorded, and refresh only if one of *those* scopes moved.
 *  - After the user's own mutation we advance the baseline for just the scopes
 *    it touched, so our own write never triggers a self-reload.
 */

let baselines = null; // { scope: version }
let staleHandled = false;
let overlayEl = null;
let mutationGraceUntil = 0;
let scopesCache = { data: null, at: 0 };

async function fetchScopes() {
  const res = await fetch(`${getApiUrl()}/api/version`, {
    credentials: "include",
    cache: "no-store",
  });
  if (!res.ok) throw new Error("version fetch failed");
  const data = await res.json();
  if (data && data.scopes && typeof data.scopes === "object") return data.scopes;
  // Backward compatibility: fall back to the single global version.
  return { global: Number(data && data.version) };
}

async function fetchScopesCached() {
  if (scopesCache.data && Date.now() - scopesCache.at < 1500) {
    return scopesCache.data;
  }
  const data = await fetchScopes();
  scopesCache = { data, at: Date.now() };
  return data;
}

async function syncBaselines() {
  try {
    baselines = { ...(await fetchScopes()) };
    scopesCache = { data: baselines, at: Date.now() };
  } catch {
    /* network hiccup — retry on next tick */
  }
}

/**
 * Map a request URL to the resource scopes it can affect. Returning [] means
 * "this action touches no versioned data" and skips the check entirely.
 */
function scopesForRequest(url) {
  let p = String(url || "");
  p = p.replace(/^https?:\/\/[^/]+/i, "").split("?")[0];
  if (!p.startsWith("/api/") || p === "/api/version") return [];

  const has = (re) => re.test(p);
  const s = new Set();

  if (has(/\/(match|matches|matchmaker|cast|finals)\b/)) s.add("matches");
  if (has(/\/(team|teams|challenge|challenges)\b/)) s.add("teams");
  if (has(/\/(player|players|register|unregister)\b/)) s.add("players");
  if (has(/\/(settings|roster|season|config|rules|calendar)\b/)) s.add("settings");
  if (has(/\/(clips|playlist)\b/)) s.add("media");

  // Actions that rewrite leaderboards / archives affect teams and players.
  if (has(/\/(leaderboard|leaderboards|adjust-stats|reset_player_leaderboard|sync-roles|archive)\b/)) {
    s.add("teams");
    s.add("players");
  }
  // Global challenge toggles + finals visibility are stored in league_settings.
  if (has(/challenges\/(enable|disable)\b/) || has(/\/finals\/toggle-visible\b/)) {
    s.add("settings");
  }
  // Drafting a player changes rosters and player rows.
  if (has(/\/mod\/draft\b/)) {
    s.add("teams");
    s.add("players");
  }

  return [...s];
}

function showRefreshOverlay() {
  if (staleHandled) return;
  staleHandled = true;

  if (!document.body) return;

  if (!overlayEl) {
    overlayEl = document.createElement("div");
    overlayEl.id = "ecgl-refresh-overlay";
    overlayEl.style.cssText = [
      "position:fixed",
      "inset:0",
      "z-index:2147483647",
      "display:flex",
      "flex-direction:column",
      "align-items:center",
      "justify-content:center",
      "gap:16px",
      "background:rgba(0,0,0,0.88)",
      "color:#fff",
      "font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif",
      "text-align:center",
      "padding:24px",
    ].join(";");

    const spinner = document.createElement("div");
    spinner.style.cssText = [
      "width:44px",
      "height:44px",
      "border:4px solid rgba(255,255,255,0.25)",
      "border-top-color:#fff",
      "border-radius:50%",
      "animation:ecgl-spin 0.9s linear infinite",
    ].join(";");

    const text = document.createElement("div");
    text.style.cssText = "font-size:18px;font-weight:600;";
    text.textContent = "The site has been updated. Refreshing…";

    const sub = document.createElement("div");
    sub.style.cssText = "font-size:14px;color:#bbb;";
    sub.textContent = "Your changes (if any) are safe. One moment…";

    const style = document.createElement("style");
    style.textContent =
      "@keyframes ecgl-spin{to{transform:rotate(360deg)}}";

    overlayEl.appendChild(spinner);
    overlayEl.appendChild(text);
    overlayEl.appendChild(sub);
    overlayEl.appendChild(style);
    document.body.appendChild(overlayEl);
  } else {
    overlayEl.style.display = "flex";
  }

  // Give the user a beat to see the message, then reload.
  setTimeout(() => window.location.reload(), 900);
}

// After the client's own mutation completes, advance the baselines for only
// the scopes that mutation touched, so we never reload on our own write while
// still catching changes made by other users in other scopes.
function markMutationSettled(scopes) {
  mutationGraceUntil = Date.now() + 2000;
  fetchScopes()
    .then((current) => {
      scopesCache = { data: current, at: Date.now() };
      if (!baselines) {
        baselines = { ...current };
        return;
      }
      for (const sc of scopes || []) {
        if (current[sc] !== undefined) baselines[sc] = current[sc];
      }
    })
    .catch(() => {});
}

// Returns true when the relevant scopes are current, false when a refresh is
// required. Only the scopes touched by this action are compared.
async function ensureFresh(scopes) {
  if (!scopes || scopes.length === 0) return true;
  try {
    const current = await fetchScopesCached();

    if (!baselines) {
      baselines = { ...current };
      return true;
    }

    let stale = false;
    for (const sc of scopes) {
      if (current[sc] === undefined) continue;
      if (baselines[sc] === undefined) {
        baselines[sc] = current[sc];
        continue;
      }
      if (current[sc] !== baselines[sc]) stale = true;
    }

    if (!stale) return true;

    // The user's own write just bumped these scopes — adopt and proceed.
    if (Date.now() < mutationGraceUntil) {
      for (const sc of scopes) {
        if (current[sc] !== undefined) baselines[sc] = current[sc];
      }
      return true;
    }
    return false;
  } catch {
    // Can't verify (e.g. offline) — let the request proceed and fail normally.
    return true;
  }
}

export function initRefreshGuard() {
  syncBaselines();

  // Block stale commands: before a mutating request, verify only the scopes it
  // affects are current. Unrelated changes are ignored.
  axios.interceptors.request.use(async (config) => {
    const method = String(config.method || "").toLowerCase();
    if (["post", "put", "patch", "delete"].includes(method)) {
      const scopes = scopesForRequest(config.url);
      const fresh = await ensureFresh(scopes);
      if (!fresh) {
        showRefreshOverlay();
        const err = new Error("Stale client state — refresh required");
        err.isECGLStale = true;
        return Promise.reject(err);
      }
      config.__ecglScopes = scopes;
    }
    return config;
  });

  // Advance only the scopes our own mutation touched.
  axios.interceptors.response.use(
    (res) => {
      const method = (res.config && res.config.method) || "";
      if (["post", "put", "patch", "delete"].includes(String(method).toLowerCase())) {
        markMutationSettled(res.config && res.config.__ecglScopes);
      }
      return res;
    },
    (err) => Promise.reject(err)
  );

  // Pre-check stale state + advance scopes after native fetch mutations.
  const origFetch = window.fetch.bind(window);
  window.fetch = (...args) => {
    const opts = args[1] || {};
    const method = String(opts.method || "GET").toUpperCase();
    const isMutation = ["POST", "PUT", "PATCH", "DELETE"].includes(method);
    const url = typeof args[0] === "string" ? args[0] : (args[0] && args[0].url) || "";

    if (!isMutation) return origFetch(...args);

    const scopes = scopesForRequest(url);
    return ensureFresh(scopes).then((fresh) => {
      if (!fresh) {
        showRefreshOverlay();
        return Promise.reject(new Error("Stale client state — refresh required"));
      }
      return origFetch(...args).then((res) => {
        markMutationSettled(scopes);
        return res;
      });
    });
  };
}
