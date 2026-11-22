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
