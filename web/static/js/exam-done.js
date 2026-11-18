// Setelah kumpul (T138): hapus salinan halaman ujian di perangkat.
(function () {
  "use strict";
  if (window.caches && caches.keys) {
    caches.keys().then(function (ks) {
      ks.forEach(function (k) { if (k.indexOf("akra-ujian") === 0) caches.delete(k); });
    }).catch(function () {});
  }
})();
