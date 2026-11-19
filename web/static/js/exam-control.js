// Pusat kendali koordinator (T144): kartu ruang diperbarui tiap 10 detik,
// centang kartu bertahan saat diperbarui, tujuan pindah disaring per
// ujian, dan konfirmasi aksi jeda.
(function () {
  "use strict";

  var root = document.querySelector("[data-control]");
  if (!root) return;
  var live = root.querySelector("[data-live]");
  var updated = root.querySelector("[data-updated]");
  var bulk = document.getElementById("bulk");
  var picked = root.querySelector("[data-picked]");

  // ---- Centang kartu: disimpan per nilai (id sesi) agar bertahan. ----
  var chosen = {};
  function readPicks() {
    chosen = {};
    live.querySelectorAll("[data-pick]").forEach(function (c) { if (c.checked) chosen[c.value] = true; });
    showPicked();
  }
  function applyPicks() {
    live.querySelectorAll("[data-pick]").forEach(function (c) { c.checked = !!chosen[c.value]; });
    // Kartu yang hilang (sesi selesai) tidak lagi dipilih.
    var present = {};
    live.querySelectorAll("[data-pick]").forEach(function (c) { present[c.value] = true; });
    Object.keys(chosen).forEach(function (k) { if (!present[k]) delete chosen[k]; });
    showPicked();
  }
  function showPicked() {
    if (!picked) return;
    var n = Object.keys(chosen).length;
    picked.textContent = n ? "(" + n + " ruang)" : "(belum ada)";
  }
  live.addEventListener("change", function (e) {
    if (e.target.matches("[data-pick]")) {
      readPicks();
      var radio = bulk && bulk.querySelector('input[name="target"][value="pilih"]');
      if (radio && Object.keys(chosen).length) radio.checked = true;
    }
  });

  // ---- Pembaruan tiap 10 detik. ----
  var lastAt = Date.now();
  var inflight = false;
  function poll() {
    if (inflight) return;
    inflight = true;
    fetch(root.getAttribute("data-data-url"), { credentials: "same-origin", cache: "no-store", headers: { "Accept": "text/html" } })
      .then(function (r) {
        if (r.status === 404 || r.status === 403) { window.location.reload(); throw new Error("gone"); }
        if (!r.ok) throw new Error("poll " + r.status);
        return r.text();
      })
      .then(function (html) {
        var y = window.scrollY;
        live.innerHTML = html;
        applyPicks();
        window.scrollTo(0, y);
        lastAt = Date.now();
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

  // ---- Konfirmasi aksi massal. ----
  if (bulk) {
    bulk.addEventListener("submit", function (e) {
      var btn = e.submitter;
      if (!btn) return;
      var all = bulk.querySelector('input[name="target"][value="semua"]').checked;
      if (!all && !Object.keys(chosen).length) {
        e.preventDefault();
        window.alert("Centang kartu ruang dulu, atau pilih “Semua sesi yang sedang berlangsung”.");
        return;
      }
      var msg = btn.getAttribute("data-confirm");
      if (btn.value === "pesan") {
        var text = bulk.querySelector('[name="pesan"]').value.trim();
        if (!text) { e.preventDefault(); bulk.querySelector('[name="pesan"]').focus(); return; }
        msg = "Kirim pesan ke " + (all ? "semua sesi yang berlangsung" : Object.keys(chosen).length + " ruang") + "?\n\n" + text;
      }
      if (msg && !window.confirm(msg)) e.preventDefault();
    });
  }

  // ---- Pindah peserta: tujuan hanya sesi ujian yang sama. ----
  var move = root.querySelector("[data-move]");
  if (move) {
    var who = move.querySelector('[name="peserta"]');
    var to = move.querySelector('[name="tujuan"]');
    var filterTargets = function () {
      var opt = who.options[who.selectedIndex];
      var exam = opt && opt.getAttribute("data-exam");
      var from = opt && opt.getAttribute("data-session");
      Array.prototype.forEach.call(to.options, function (o) {
        if (!o.value) return;
        var ok = !exam || (o.getAttribute("data-exam") === exam && o.value !== from);
        o.hidden = !ok;
        o.disabled = !ok;
        if (!ok && o.selected) to.value = "";
      });
    };
    who.addEventListener("change", filterTargets);
    filterTargets();
  }
})();
