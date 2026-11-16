// Service worker spike: cangkang + payload + gambar dari cache bila offline.
const C = 'ujian-spike-v1';
const SHELL = ['ujian.html', 'ujian.css', 'ujian.js'];
self.addEventListener('install', e => e.waitUntil(caches.open(C).then(c => c.addAll(SHELL)).then(() => self.skipWaiting())));
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));
self.addEventListener('fetch', e => {
  const u = new URL(e.request.url);
  if (e.request.method !== 'GET') return; // simpan jawaban tidak pernah dari cache
  if (u.pathname === '/api/payload' || u.pathname.startsWith('/img/')) {
    // network-first, simpan salinan untuk offline
    e.respondWith(fetch(e.request).then(r => { const cp = r.clone(); caches.open(C).then(c => c.put(e.request, cp)); return r; })
      .catch(() => caches.match(e.request)));
    return;
  }
  e.respondWith(caches.match(e.request, { ignoreSearch: true }).then(r => r || fetch(e.request)));
});
