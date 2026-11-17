// Cek perangkat peserta (T136): fitur browser, koneksi, rumus, dan suara.
// Tidak mengirim apa pun ke server selain permintaan uji kecepatan.
(function () {
  "use strict";
  var root, results = {};
  var rank = { ok: 0, warn: 1, fail: 2, checking: 1 };

  function item(name) { return root.querySelector('[data-item="' + name + '"]'); }
  function set(name, level, text) {
    var el = item(name);
    el.className = "is-" + level;
    if (text) el.querySelector("[data-detail]").textContent = text;
    results[name] = level;
    summarize();
  }
  function summarize() {
    var worst = root.dataset.serverLevel || "ok";
    var pending = false;
    ["features", "network", "math", "sound"].forEach(function (k) {
      if (!results[k]) { pending = true; return; }
      if (rank[results[k]] > rank[worst]) worst = results[k];
    });
    var box = root.querySelector("[data-verdict]");
    var title = root.querySelector("[data-verdict-title]");
    var text = root.querySelector("[data-verdict-text]");
    if (pending && worst !== "fail") {
      box.className = "device-verdict is-checking";
      title.textContent = "Memeriksa…";
      text.textContent = "Selesaikan tes suara di bawah.";
      return;
    }
    box.className = "device-verdict is-" + worst;
    title.textContent = { ok: "✓ Siap dipakai ujian", warn: "⚠ Bisa dipakai, ada saran", fail: "✗ Tidak didukung" }[worst];
    text.textContent = {
      ok: "Perangkat dan koneksi ini siap untuk ujian online.",
      warn: "Ikuti saran di bawah agar ujian lancar.",
      fail: "Gunakan perangkat atau browser lain, atau pakai komputer lab sekolah.",
    }[worst];
  }

  function checkFeatures() {
    var missing = [], advice = [];
    if (!window.fetch || !window.Promise) missing.push("fetch");
    try { window.localStorage.setItem("akra-cek", "1"); window.localStorage.removeItem("akra-cek"); } catch (e) { missing.push("penyimpanan lokal"); }
    if (!window.indexedDB) missing.push("IndexedDB");
    if (!("serviceWorker" in navigator)) advice.push("mode offline tidak tersedia");
    var el = document.documentElement;
    if (!(el.requestFullscreen || el.webkitRequestFullscreen)) advice.push("layar penuh tidak tersedia");
    if (!(window.AudioContext || window.webkitAudioContext)) advice.push("bunyi tanda tidak tersedia");
    if (missing.length) return set("features", "fail", "Tidak tersedia: " + missing.join(", ") + ". Gunakan Chrome versi terbaru.");
    if (advice.length) return set("features", "warn", "Bisa dipakai, tetapi " + advice.join(", ") + ". Chrome lebih disarankan.");
    set("features", "ok", "Simpan otomatis, mode offline, layar penuh, dan suara tersedia.");
  }

  function timed(url) {
    var t0 = performance.now();
    return fetch(url + (url.indexOf("?") < 0 ? "?" : "&") + "t=" + Date.now(), { cache: "no-store" })
      .then(function (r) { return r.arrayBuffer(); })
      .then(function (b) { return { ms: performance.now() - t0, bytes: b.byteLength }; });
  }

  function checkNetwork() {
    var pings = [];
    var p = Promise.resolve();
    for (var i = 0; i < 3; i++) p = p.then(function () { return timed("/u/cek/ping").then(function (r) { pings.push(r.ms); }); });
    p.then(function () { return timed("/u/cek/contoh.jpg"); })
      .then(function (img) {
        pings.sort(function (a, b) { return a - b; });
        var ping = Math.round(pings[1]);
        var kbps = Math.round((img.bytes * 8) / img.ms);
        var load = (img.ms / 1000).toFixed(1);
        var text = "Waktu tempuh " + ping + " ms · gambar soal dimuat " + load + " detik (± " + kbps + " kbps).";
        if (img.ms > 8000 || ping > 1500) return set("network", "fail", text + " Terlalu lambat: cari sinyal lebih baik atau pakai WiFi sekolah.");
        if (img.ms > 3000 || ping > 600) return set("network", "warn", text + " Agak lambat; jawaban tetap aman, tetapi soal bergambar butuh waktu.");
        set("network", "ok", text);
      })
      .catch(function () { set("network", "fail", "Tidak bisa terhubung ke server. Periksa internet."); });
  }

  function checkMath() {
    var m = root.querySelector("[data-math] .katex");
    if (!m) return set("math", "warn", "Contoh rumus tidak tersedia.");
    var ready = document.fonts && document.fonts.ready ? document.fonts.ready : Promise.resolve();
    ready.then(function () {
      var w = m.getBoundingClientRect().width;
      var font = !document.fonts || document.fonts.check("16px KaTeX_Main");
      var frac = m.querySelector(".mfrac") && m.querySelector(".sqrt");
      if (w > 0 && frac && font) return set("math", "ok", "Rumus tampil benar. Pastikan contoh di bawah terlihat sebagai pecahan dan akar.");
      set("math", "warn", "Rumus mungkin tidak tampil sempurna. Gunakan Chrome versi terbaru.");
    });
  }

  function setupSound() {
    var play = root.querySelector("[data-sound-play]");
    var yes = root.querySelector("[data-sound-yes]");
    var no = root.querySelector("[data-sound-no]");
    var Ctx = window.AudioContext || window.webkitAudioContext;
    if (!Ctx) {
      play.hidden = true;
      return set("sound", "warn", "Browser tidak bisa membunyikan tanda. Gunakan Chrome.");
    }
    play.addEventListener("click", function () {
      var ctx = new Ctx();
      var osc = ctx.createOscillator();
      var gain = ctx.createGain();
      osc.type = "square";
      osc.frequency.value = 880;
      gain.gain.value = 0.2;
      osc.connect(gain).connect(ctx.destination);
      osc.start();
      osc.stop(ctx.currentTime + 0.6);
      yes.hidden = no.hidden = false;
    });
    yes.addEventListener("click", function () { set("sound", "ok", "Bunyi terdengar."); });
    no.addEventListener("click", function () { set("sound", "warn", "Bunyi tidak terdengar: naikkan volume dan matikan mode senyap."); });
  }

  document.addEventListener("DOMContentLoaded", function () {
    root = document.querySelector("[data-device-check]");
    if (!root) return;
    checkFeatures();
    checkMath();
    setupSound();
    checkNetwork();
    summarize();
  });
})();
