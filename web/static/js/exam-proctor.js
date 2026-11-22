// Layar pengawas (T143): pembaruan daftar peserta tiap 10 detik, saring
// per status, tanda kejadian baru (getar + lencana), lembar aksi per
// peserta, dan hitung mundur token. Juga halaman token layar penuh.
(function () {
  "use strict";

  function pad(n) { return (n < 10 ? "0" : "") + n; }
  function mmss(ms) {
    var s = Math.max(0, Math.round(ms / 1000));
    return Math.floor(s / 60) + ":" + pad(s % 60);
  }

  // Hitung mundur rotasi token: offset jam server dari atribut data-now
  // tidak tersedia, jadi dipakai jam perangkat (cukup untuk tampilan).
  function tokenCountdown(box, reload) {
    var left = box.querySelector("[data-token-left]");
    if (!left) return;
    function tick() {
      var next = parseInt(box.getAttribute("data-next"), 10) || 0;
      var ms = next - Date.now();
      left.textContent = mmss(ms);
      if (ms <= 0 && reload) reload();
    }
    tick();
    setInterval(tick, 1000);
  }

  // ---- Halaman token layar penuh. ----
  var screen = document.querySelector("[data-token-screen]");
  if (screen) {
    var busy = false;
    var refresh = function () {
      if (busy) return;
      busy = true;
      fetch(screen.getAttribute("data-json"), { credentials: "same-origin", cache: "no-store" })
        .then(function (r) { return r.json(); })
        .then(function (d) {
          if (d.token) screen.querySelector("[data-token-code]").textContent = d.token;
          // Selisih jam perangkat dan server ikut dihitung.
          if (d.next && d.now) screen.setAttribute("data-next", String(d.next - d.now + Date.now()));
        })
        .catch(function () { /* coba lagi pada putaran berikutnya */ })
        .then(function () { busy = false; });
    };
    tokenCountdown(screen, refresh);
    setInterval(refresh, 10000);
    var fsBtn = screen.querySelector("[data-fullscreen]");
    if (fsBtn) fsBtn.addEventListener("click", function () {
      var d = document.documentElement;
      var fs = d.requestFullscreen || d.webkitRequestFullscreen;
      if (fs) fs.call(d);
    });
    return;
  }

  var root = document.querySelector("[data-proctor]");
  if (!root) return;
  var live = root.querySelector("[data-live]");
  var updated = root.querySelector("[data-updated]");
  var tokenBox = root.querySelector("[data-token]");
  if (tokenBox) tokenCountdown(tokenBox, function () { if (!tokenBox.dataset.reloading) { tokenBox.dataset.reloading = "1"; poll(); } });

  // ---- Saring per status. ----
  var filter = "";
  function applyFilter() {
    var shown = 0;
    live.querySelectorAll("[data-row]").forEach(function (row) {
      var on = !filter || (" " + row.getAttribute("data-filters") + " ").indexOf(" " + filter + " ") >= 0;
      row.hidden = !on;
      if (on) shown++;
    });
    live.querySelectorAll("[data-filter]").forEach(function (b) { b.setAttribute("aria-pressed", b.getAttribute("data-filter") === filter ? "true" : "false"); });
    var empty = live.querySelector("[data-empty]");
    if (empty) empty.hidden = shown > 0 || live.querySelectorAll("[data-row]").length === 0;
  }
  live.addEventListener("click", function (e) {
    var b = e.target.closest("[data-filter]");
    if (b) { filter = b.getAttribute("data-filter"); applyFilter(); return; }
    var row = e.target.closest("[data-open]");
    if (row) openSheet(row);
  });

  // ---- Kejadian baru: id kejadian terakhir per peserta. ----
  var seen = {};
  var fresh = {}; // peserta dengan kejadian baru yang belum dibuka
  function markEvents(first) {
    var any = false;
    live.querySelectorAll("[data-row]").forEach(function (row) {
      var pid = row.getAttribute("data-pid");
      var ev = parseInt(row.getAttribute("data-event"), 10) || 0;
      if (!first && ev > (seen[pid] || 0)) { fresh[pid] = true; any = true; }
      seen[pid] = Math.max(seen[pid] || 0, ev);
      var badge = row.querySelector(".proctor-new");
      if (badge) badge.hidden = !fresh[pid];
      row.classList.toggle("is-new", !!fresh[pid]);
    });
    if (any && navigator.vibrate) navigator.vibrate([200, 100, 200]);
  }

  // ---- Pembaruan tiap 10 detik. ----
  var lastAt = Date.now();
  var inflight = false;
  function poll() {
    if (inflight || sheetOpen()) return;
    inflight = true;
    fetch(root.getAttribute("data-data-url"), { credentials: "same-origin", cache: "no-store", headers: { "Accept": "text/html" } })
      .then(function (r) {
        if (r.status === 404 || r.status === 403) { window.location.reload(); throw new Error("gone"); }
        if (!r.ok) throw new Error("poll " + r.status);
        return r.text();
      })
      .then(function (html) {
        var scrollY = window.scrollY;
        live.innerHTML = html;
        applyFilter();
        markEvents(false);
        window.scrollTo(0, scrollY);
        lastAt = Date.now();
        // Token berganti: muat ulang header token (jarang, tiap 15 menit).
        if (tokenBox && tokenBox.dataset.reloading) window.location.reload();
      })
      .catch(function () { /* indikator menunjukkan data lama */ })
      .then(function () { inflight = false; });
  }
  setInterval(poll, 10000);
  setInterval(function () {
    var s = Math.round((Date.now() - lastAt) / 1000);
    updated.textContent = s < 3 ? "Diperbarui baru saja" : "Diperbarui " + s + " detik lalu";
    updated.parentElement.classList.toggle("is-stale", s > 30);
  }, 1000);
  document.addEventListener("visibilitychange", function () { if (document.visibilityState === "visible") poll(); });
  markEvents(true);
  applyFilter();

  // ---- Lembar aksi per peserta. ----
  var sheet = root.querySelector("[data-sheet]");
  function sheetOpen() { return sheet && sheet.open; }
  function openSheet(btn) {
    if (!sheet) return;
    var row = btn.closest("[data-row]");
    var pid = row.getAttribute("data-pid");
    fresh[pid] = false;
    var badge = row.querySelector(".proctor-new");
    if (badge) badge.hidden = true;
    row.classList.remove("is-new");
    var state = btn.getAttribute("data-state");
    var locked = btn.getAttribute("data-locked") === "true";
    var paused = btn.getAttribute("data-paused") === "true";
    var running = state === "mengerjakan" || state === "offline";
    sheet.querySelector("[data-sheet-name]").textContent = btn.getAttribute("data-name");
    sheet.querySelector("[data-sheet-status]").textContent = btn.getAttribute("data-status");
    var show = {
      all: true,
      notdone: state !== "selesai",
      locked: locked,
      running: running,
      "running-unpaused": running && !paused,
      paused: running && paused,
    };
    sheet.querySelectorAll("[data-act]").forEach(function (f) {
      f.action = root.getAttribute("data-action-url") + pid;
      f.hidden = !show[f.getAttribute("data-show")];
    });
    sheet.querySelector("[data-reason-input]").value = "";
    sheet.showModal();
  }
  if (sheet) {
    sheet.querySelector("[data-sheet-close]").addEventListener("click", function () { sheet.close(); });
    sheet.addEventListener("click", function (e) { if (e.target === sheet) sheet.close(); });
    sheet.addEventListener("submit", function (e) {
      var reason = sheet.querySelector("[data-reason-input]").value;
      var f = e.target;
      var r = f.querySelector("[data-reason]");
      if (r) r.value = reason;
    });
  }

  // Konfirmasi form[data-confirm] ditangani js/nav.js (T162).
})();
