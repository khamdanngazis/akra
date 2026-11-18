// Setelah kumpul (T138, T139): hapus salinan ujian di perangkat — cache
// service worker (halaman, gambar soal, aset) dan salinan jawaban di
// IndexedDB. Jawaban sudah diterima server.
(function () {
  "use strict";
  if (window.caches && caches.keys) {
    caches.keys().then(function (ks) {
      ks.forEach(function (k) { if (k.indexOf("akra-ujian") === 0) caches.delete(k); });
    }).catch(function () {});
  }
  if (!window.indexedDB) return;
  var m = /^\/u\/a\/([0-9a-f-]{36})\/selesai$/.exec(location.pathname);
  try {
    if (m) indexedDB.deleteDatabase("akra-ujian-" + m[1]);
  } catch (e) { /* penyimpanan dimatikan */ }
})();
