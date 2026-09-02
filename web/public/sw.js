// VisitFlow service worker.
//
// The one screen that must survive a network outage is the evacuation roster:
// during an emergency the lobby needs the list of people currently inside even
// if the intranet or the server is unreachable. Everything else is served
// network-first so operators never act on stale visitor data.

const SHELL_CACHE = "visitflow-shell-v1";
const ROSTER_CACHE = "visitflow-roster-v1";
const ROSTER_PATH = "/api/v1/lobby/roster";

self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const names = await caches.keys();
    await Promise.all(names.filter((name) => name !== SHELL_CACHE && name !== ROSTER_CACHE).map((name) => caches.delete(name)));
    await self.clients.claim();
  })());
});

async function cacheRoster(request) {
  try {
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(ROSTER_CACHE);
      // Store under a fixed key so a query string does not fragment the cache.
      await cache.put(ROSTER_PATH, response.clone());
    }
    return response;
  } catch (error) {
    const cached = await caches.match(ROSTER_PATH);
    if (cached) {
      const body = await cached.json();
      return new Response(JSON.stringify({ ...body, offline: true }), {
        status: 200,
        headers: { "Content-Type": "application/json; charset=utf-8" },
      });
    }
    throw error;
  }
}

async function shellFirstNetwork(request) {
  try {
    const response = await fetch(request);
    if (response.ok && request.method === "GET") {
      const cache = await caches.open(SHELL_CACHE);
      await cache.put(request, response.clone());
    }
    return response;
  } catch (error) {
    const cached = await caches.match(request);
    if (cached) return cached;
    throw error;
  }
}

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET") return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  if (url.pathname === ROSTER_PATH) {
    event.respondWith(cacheRoster(request));
    return;
  }
  // Never cache passes, QR images or any other visitor data.
  if (url.pathname.startsWith("/api/") || url.pathname.startsWith("/img/") || url.pathname === "/metrics") return;

  if (request.mode === "navigate" || url.pathname.startsWith("/assets/") || url.pathname === "/manifest.webmanifest" || url.pathname.endsWith(".svg") || url.pathname.endsWith(".png")) {
    event.respondWith(shellFirstNetwork(request));
  }
});
