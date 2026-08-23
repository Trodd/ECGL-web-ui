import { getApiUrl } from "./config";

/**
 * Build an API URL. Dev-mode impersonation is now handled entirely by the
 * server session (POST /api/dev/impersonate), so no extra query params are
 * needed here.
 */
export function devUrl(path) {
    return `${getApiUrl()}${path}`;
}
