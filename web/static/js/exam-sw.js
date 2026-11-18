// Service worker halaman ujian (T139, ADR 0001 bagian 5). Disajikan sebagai
// /u/sw.js dengan scope /u/a/. Menyimpan salinan di perangkat agar halaman
// ujian tetap terbuka saat sinyal putus:
// - halaman ujian /u/a/{id}: jaringan dulu, salinan cache bila offline;
// - gambar soal /u/a/{id}/gambar/{aset}: cache dulu (isi tetap per aset);
// - aset statis berversi (/static/...?v=) dan font: cache dulu.
// POST (simpan, kumpul) tidak pernah lewat cache. Semua cache bernama
// "akra-ujian-..." dan dihapus setelah ujian dikumpulkan.
"use strict";

var STATIC = "akra-ujian-static";
var ID = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";
var PAGE = new RegExp("^/u/a/(" + ID + ")$");
var IMAGE = new RegExp("^/u/a/(" + ID + ")/gambar/" + ID + "$");

function attemptCache(id) { return "akra-ujian-a-" + id; }

// staticCacheable: aset berversi (?v=) dan font (KaTeX, Arab; isinya tetap).
function staticCacheable(u) {
  return u.pathname.indexOf("/static/") === 0 && (/(^|&|\?)v=/.test(u.search) || /\.woff2?$/.test(u.pathname));
}

self.addEventListener("install", function () { self.skipWaiting(); });
self.addEventListener("activate", function (e) { e.waitUntil(self.clients.claim()); });

var OFFLINE = '<!doctype html><html lang="id"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">' +
  '<title>Tidak ada sambungan</title><body style="font-family:system-ui,sans-serif;padding:24px;max-width:32rem;margin:auto">' +
  "<h1>Tidak ada sambungan internet</h1><p>Halaman ujian belum tersimpan di perangkat ini. Hubungkan internet lalu muat ulang halaman.</p>" +
  "<p>Jawaban yang sudah diisi tidak hilang.</p></body></html>";

// page: jaringan dulu. Halaman 200 disalin ke cache attempt; jawaban lain
// (dialihkan ke selesai, perangkat lain) menghapus salinan.
function page(req, id) {
  var name = attemptCache(id);
  var key = new URL(req.url).pathname;
  return fetch(req).then(function (res) {
    if (res.status === 200 && res.type === "basic" && !res.redirected) {
      var copy = res.clone();
      caches.open(name).then(function (c) { return c.put(key, copy); }).catch(function () {});
    } else if (res.type === "opaqueredirect" || res.redirected || res.status === 409 || res.status === 404) {
      caches.delete(name);
    }
    return res;
  }).catch(function () {
    return caches.open(name).then(function (c) { return c.match(key); }).then(function (r) {
      return r || new Response(OFFLINE, { status: 503, headers: { "Content-Type": "text/html; charset=utf-8" } });
    });
  });
}

// cacheFirst: salinan cache bila ada; selain itu jaringan lalu disalin.
// Gambar soal dialihkan ke URL bertanda tangan di domain lain, sehingga
// responsnya bisa opaque; tetap disalin karena kunci cache-nya path tetap.
function cacheFirst(req, name) {
  var key = req.url;
  return caches.open(name).then(function (c) {
    return c.match(key).then(function (hit) {
      if (hit) return hit;
      return fetch(req).then(function (res) {
        if (res.ok || res.type === "opaque") c.put(key, res.clone()).catch(function () {});
        return res;
      });
    });
  });
}

self.addEventListener("fetch", function (e) {
  var req = e.request;
  if (req.method !== "GET") return;
  var u = new URL(req.url);
  if (u.origin !== self.location.origin) return;
  var m = PAGE.exec(u.pathname);
  if (m && req.mode === "navigate") { e.respondWith(page(req, m[1])); return; }
  m = IMAGE.exec(u.pathname);
  if (m) { e.respondWith(cacheFirst(req, attemptCache(m[1]))); return; }
  if (staticCacheable(u)) { e.respondWith(cacheFirst(req, STATIC)); return; }
});

// Pesan dari halaman ujian:
// - warm: salin halaman (bila halaman dibuka sebelum service worker aktif),
//   aset statis, dan semua gambar soal agar tersedia saat offline;
// - drop: hapus salinan attempt.
self.addEventListener("message", function (e) {
  var d = e.data || {};
  if (d.type === "warm" && typeof d.attempt === "string" && new RegExp("^" + ID + "$").test(d.attempt)) {
    e.waitUntil(warm(d));
  } else if (d.type === "drop" && typeof d.attempt === "string") {
    e.waitUntil(caches.delete(attemptCache(d.attempt)));
  }
});

function warm(d) {
  var name = attemptCache(d.attempt);
  var jobs = [];
  if (d.page) {
    var key = "/u/a/" + d.attempt;
    jobs.push(fetch(key, { credentials: "same-origin" }).then(function (res) {
      if (res.status === 200 && !res.redirected) return caches.open(name).then(function (c) { return c.put(key, res); });
    }));
  }
  (d.urls || []).forEach(function (raw) {
    var u;
    try { u = new URL(raw, self.location.origin); } catch (err) { return; }
    if (u.origin !== self.location.origin) return;
    var cache = IMAGE.test(u.pathname) ? name : (staticCacheable(u) ? STATIC : null);
    if (!cache) return;
    var req = new Request(u.href, { mode: IMAGE.test(u.pathname) ? "no-cors" : "same-origin", credentials: "same-origin" });
    jobs.push(caches.open(cache).then(function (c) {
      return c.match(u.href).then(function (hit) {
        if (hit) return;
        return fetch(req).then(function (res) { if (res.ok || res.type === "opaque") return c.put(u.href, res); });
      });
    }));
  });
  return Promise.all(jobs.map(function (j) { return j.catch(function () {}); }));
}
