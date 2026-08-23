import axios from "axios";
import { getApiUrl } from "./config";

/**
 * refreshGuard — force a full page refresh when backend data changes while a
 * user keeps the tab open, so nobody can interact with a stale frontend.
 *
 * How it works:
 *  - The backend keeps a global "data version" that increments on every
 *    meaningful write (GET /api/version).
 *  - On load we record a baseline.
 *  - Before ANY mutating command (axios or fetch), we re-check the version and
 *    block + refresh if the client has gone stale.
 *  - We re-sync the baseline after the user's own mutations (axios + fetch)
 *    so their own actions never trigger a self-reload.
 */

let baseline = null;
let staleHandled = false;
let overlayEl = null;
let mutationGraceUntil = 0;

async function fetchVersion() {
  const res = await fetch(`${getApiUrl()}/api/version`, {
    credentials: "include",
    cache: "no-store",
  });
  if (!res.ok) throw new Error("version fetch failed");
  const data = await res.json();
  return Number(data && data.version);
}

async function syncBaseline() {
  try {
    baseline = await fetchVersion();
  } catch {
    /* network hiccup — retry on next tick */
  }
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

// After the client's own mutation completes, the backend has already bumped
// the version, so re-sync the baseline to avoid self-reloading.
function markMutationSettled() {
  mutationGraceUntil = Date.now() + 2000;
  syncBaseline();
}

let versionCache = { value: null, at: 0 };

// Returns true when the client is current, false when a refresh is required.
// Called right before every mutating command so a stale user cannot submit an
// action against outdated data.
async function ensureFresh() {
  try {
    let v;
    if (versionCache.value !== null && Date.now() - versionCache.at < 2000) {
      v = versionCache.value;
    } else {
      v = await fetchVersion();
      versionCache.value = v;
      versionCache.at = Date.now();
    }

    if (baseline === null) {
      baseline = v;
      return true;
    }
    if (v !== baseline) {
      // The user just performed a mutation; their own write bumped the
      // version, so re-sync and allow it through.
      if (Date.now() < mutationGraceUntil) {
        baseline = v;
        return true;
      }
      return false;
    }
    return true;
  } catch {
    // Can't verify (e.g. offline) — let the request proceed and fail normally.
    return true;
  }
}

export function initRefreshGuard() {
  syncBaseline();

  // Block stale commands: before any mutating request, verify the client is
  // up to date. If not, force a refresh and cancel the request.
  axios.interceptors.request.use(async (config) => {
    const method = String(config.method || "").toLowerCase();
    if (["post", "put", "patch", "delete"].includes(method)) {
      const fresh = await ensureFresh();
      if (!fresh) {
        showRefreshOverlay();
        const err = new Error("Stale client state — refresh required");
        err.isECGLStale = true;
        return Promise.reject(err);
      }
    }
    return config;
  });

  // Re-sync baseline after axios mutations.
  axios.interceptors.response.use(
    (res) => {
      const method = (res.config && res.config.method) || "";
      if (["post", "put", "patch", "delete"].includes(String(method).toLowerCase())) {
        markMutationSettled();
      }
      return res;
    },
    (err) => Promise.reject(err)
  );

  // Pre-check stale state + re-sync baseline after native fetch mutations.
  const origFetch = window.fetch.bind(window);
  window.fetch = (...args) => {
    const opts = args[1] || {};
    const method = String(opts.method || "GET").toUpperCase();
    const isMutation = ["POST", "PUT", "PATCH", "DELETE"].includes(method);

    const doFetch = () =>
      origFetch(...args).then((res) => {
        if (isMutation) markMutationSettled();
        return res;
      });

    if (!isMutation) return doFetch();

    return ensureFresh().then((fresh) => {
      if (!fresh) {
        showRefreshOverlay();
        return Promise.reject(new Error("Stale client state — refresh required"));
      }
      return doFetch();
    });
  };
}
