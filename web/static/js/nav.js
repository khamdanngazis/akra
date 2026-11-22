// Navigasi kembali (T121): setiap halaman mencatat query terakhirnya per
// path di sessionStorage, lalu tautan bertanda data-restore (tombol kembali
// dan jejak navigasi) yang tidak memuat query diarahkan ke daftar dengan
// filter dan halaman terakhir yang dibuka di tab ini.
(function () {
  const key = (path) => 'akra.query:' + path;
  try {
    sessionStorage.setItem(key(location.pathname), location.search);
  } catch (e) {
    return;
  }
  function restore() {
    document.querySelectorAll('a[data-restore]').forEach((a) => {
      const url = new URL(a.getAttribute('href'), location.href);
      if (url.origin !== location.origin || url.search) return;
      let q = '';
      try {
        q = sessionStorage.getItem(key(url.pathname)) || '';
      } catch (e) {
        return;
      }
      if (q) a.setAttribute('href', url.pathname + q);
    });
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', restore);
  else restore();
})();

// Sub-navigasi modul (T157): di HP, item aktif digulir ke tengah agar
// tidak terpotong.
(function () {
  function center() {
    var a = document.querySelector('.module-nav [aria-current="page"]');
    if (a && a.parentElement.scrollWidth > a.parentElement.clientWidth) {
      a.parentElement.scrollLeft = a.offsetLeft - (a.parentElement.clientWidth - a.offsetWidth) / 2;
    }
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', center);
  else center();
})();

// Menu aksi (T161): <details class="menu-details"> tertutup dengan Esc
// (fokus kembali ke tombolnya), klik di luar menu, atau saat menu lain
// dibuka, sehingga hanya satu menu terbuka.
(function () {
  function closeAll(except) {
    document.querySelectorAll('details.menu-details[open]').forEach(function (d) {
      if (d !== except && !d.contains(except)) d.removeAttribute('open');
    });
  }
  document.addEventListener('click', function (e) {
    var d = e.target.closest && e.target.closest('details.menu-details');
    closeAll(d);
  });
  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Escape') return;
    var d = document.activeElement && document.activeElement.closest && document.activeElement.closest('details.menu-details[open]');
    if (!d) d = document.querySelector('details.menu-details[open]');
    if (!d) return;
    d.removeAttribute('open');
    var s = d.querySelector('summary');
    if (s) s.focus();
  });
})();
