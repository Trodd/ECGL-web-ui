/*
  ECGL service worker
  Goal: avoid "stale UI" issues by keeping caching minimal and ensuring
  new versions activate immediately.
*/

const CACHE_NAME = "ecgl-sw-v1";

self.addEventListener("install", (event) => {
    // Activate new SW immediately.
    self.skipWaiting();
});

self.addEventListener("activate", (event) => {
    event.waitUntil(
        (async () => {
            // Take control of all clients right away.
            await self.clients.claim();

            // Clean up old caches.
            const keys = await caches.keys();
            await Promise.all(
                keys
                    .filter((k) => k !== CACHE_NAME)
                    .map((k) => caches.delete(k))
            );
        })()
    );
});

self.addEventListener("fetch", (event) => {
    const req = event.request;

    // Don't cache POST/PUT/etc.
    if (req.method !== "GET") return;

    // For navigations (HTML), prefer network so the latest bundle hashes load.
    if (req.mode === "navigate") {
        event.respondWith(
            (async () => {
                try {
                    return await fetch(req, { cache: "no-store" });
                } catch {
                    return caches.match(req);
                }
            })()
        );
        return;
    }

    // For all other GETs, just pass-through (no app-shell caching).
    // This keeps behavior predictable and avoids update problems.
    event.respondWith(fetch(req));
});
