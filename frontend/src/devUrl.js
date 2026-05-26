import { getApiUrl } from "./config";

/**
 * Build an API URL with optional dev-mode impersonation.
 * Reads from sessionStorage so all pages stay in sync.
 *
 * Usage: devUrl("/api/myteam") → "http://host/api/myteam?as=12345" (or without ?as= if not impersonating)
 */
export function devUrl(path) {
    const base = `${getApiUrl()}${path}`;
    const impId = sessionStorage.getItem("dev_impersonate");
    if (!impId) return base;
    const sep = base.includes("?") ? "&" : "?";
    return `${base}${sep}as=${impId}`;
}
