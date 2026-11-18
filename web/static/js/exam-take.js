// Halaman mengerjakan ujian (T138): satu soal per layar, grid nomor,
// ragu-ragu, simpan otomatis, hitung mundur, ringkasan dan kumpul dua
// langkah. Tanpa pustaka; jalan di Chrome Android lama.
(function () {
  "use strict";

  var root = document.querySelector("[data-take]");
  if (!root) return;
  var state = JSON.parse(document.getElementById("take-state").textContent);
  var csrf = (document.querySelector('meta[name="csrf-token"]') || {}).content || "";
  var saveURL = root.getAttribute("data-save-url");
  var qs = Array.prototype.slice.call(root.querySelectorAll("[data-q]"));
  var cells = Array.prototype.slice.call(root.querySelectorAll("[data-goto]"));
  var stage = root.querySelector("[data-stage]");
  var stimCol = root.querySelector("[data-stim-col]");
  var divider = root.querySelector("[data-divider]");
  var fab = root.querySelector("[data-text-fab]");
  var flagBtn = root.querySelector("[data-flag]");
  var prevBtn = root.querySelector("[data-prev]");
  var nextBtn = root.querySelector("[data-next]");
  var panel = root.querySelector("[data-panel]");
  var answers = state.answers || {};
  var current = -1;
  var furthest = 0;
  var offset = state.now - Date.now(); // jam server − jam perangkat
  var deadline = state.deadline;
  var submitting = false;

  // ---- Urutan (seq) perubahan: selalu naik, juga setelah muat ulang. ----
  var lastSeq = 0;
  Object.keys(answers).forEach(function (k) { if (answers[k].s > lastSeq) lastSeq = answers[k].s; });
  function nextSeq() {
    var t = (Date.now() + offset) * 1000;
    lastSeq = Math.max(lastSeq + 1, t);
    return lastSeq;
  }

  function qIndex(el) { return qs.indexOf(el); }
  function qid(i) { return qs[i].getAttribute("data-q"); }

  // ---- Membaca dan menulis jawaban per jenis soal. ----
  function readValue(el) {
    var kind = el.getAttribute("data-kind");
    var id = el.getAttribute("data-q");
    switch (kind) {
      case "pg": {
        var c = el.querySelector('input[name="a-' + id + '"]:checked');
        return c ? c.value : null;
      }
      case "pg_kompleks": {
        var vs = Array.prototype.map.call(el.querySelectorAll('input[name="a-' + id + '"]:checked'), function (x) { return x.value; });
        return vs.length ? vs : null;
      }
      case "benar_salah": {
        var o = {}, n = 0;
        el.querySelectorAll("[data-stmt]").forEach(function (row) {
          var c = row.querySelector("input:checked");
          if (c) { o[row.getAttribute("data-stmt")] = c.value === "B"; n++; }
        });
        return n ? o : null;
      }
      case "menjodohkan": {
        var m = {}, k = 0;
        el.querySelectorAll("[data-left]").forEach(function (b) {
          var r = b.getAttribute("data-paired");
          if (r) { m[b.getAttribute("data-left")] = r; k++; }
        });
        return k ? m : null;
      }
      case "isian":
      case "esai": {
        var f = el.querySelector("input.take-input, textarea");
        var v = f ? f.value : "";
        return v.trim() === "" ? null : v;
      }
    }
    return null;
  }

  function applyValue(el, v) {
    var kind = el.getAttribute("data-kind");
    var id = el.getAttribute("data-q");
    if (v === null || v === undefined) return;
    switch (kind) {
      case "pg":
      case "pg_kompleks": {
        var set = Array.isArray(v) ? v : [v];
        el.querySelectorAll('input[name="a-' + id + '"]').forEach(function (x) { x.checked = set.indexOf(x.value) >= 0; });
        break;
      }
      case "benar_salah":
        el.querySelectorAll("[data-stmt]").forEach(function (row) {
          var s = v[row.getAttribute("data-stmt")];
          if (s === true || s === false) {
            var x = row.querySelector('input[value="' + (s ? "B" : "S") + '"]');
            if (x) x.checked = true;
          }
        });
        break;
      case "menjodohkan":
        el.querySelectorAll("[data-left]").forEach(function (b) { setPair(el, b, v[b.getAttribute("data-left")] || ""); });
        break;
      case "isian":
      case "esai": {
        var f = el.querySelector("input.take-input, textarea");
        if (f) { f.value = v; if (f.tagName === "TEXTAREA") grow(f); }
        break;
      }
    }
  }

  // complete: jawaban lengkap (B/S dan menjodohkan: semua baris terisi).
  function complete(el, v) {
    if (v === null || v === undefined) return false;
    var need = parseInt(el.getAttribute("data-need") || "0", 10);
    if (need && typeof v === "object" && !Array.isArray(v)) return Object.keys(v).length >= need;
    return true;
  }

  // ---- Menjodohkan: ketuk kiri lalu kanan. ----
  function setPair(el, left, right) {
    var badge = left.querySelector("[data-pair]");
    if (right) {
      var r = el.querySelector('[data-right="' + right + '"]');
      left.setAttribute("data-paired", right);
      left.classList.add("is-paired");
      badge.textContent = r ? r.getAttribute("data-letter") : "?";
    } else {
      left.removeAttribute("data-paired");
      left.classList.remove("is-paired");
      badge.textContent = "–";
    }
    var used = {};
    el.querySelectorAll("[data-left]").forEach(function (b) { var p = b.getAttribute("data-paired"); if (p) used[p] = true; });
    el.querySelectorAll("[data-right]").forEach(function (b) { b.classList.toggle("is-used", !!used[b.getAttribute("data-right")]); });
  }

  qs.forEach(function (el) {
    if (el.getAttribute("data-kind") !== "menjodohkan") return;
    var box = el.querySelector(".take-match");
    var active = null;
    function pick(b) {
      el.querySelectorAll("[data-left]").forEach(function (x) { x.setAttribute("aria-pressed", x === b ? "true" : "false"); });
      active = b;
      box.classList.toggle("is-picking", !!b);
    }
    el.querySelectorAll("[data-left]").forEach(function (b) {
      b.addEventListener("click", function () { pick(active === b ? null : b); });
    });
    el.querySelectorAll("[data-right]").forEach(function (b) {
      b.addEventListener("click", function () {
        var left = active;
        if (!left) {
          // Belum memilih kiri: pilih kiri pertama yang belum berpasangan.
          left = el.querySelector("[data-left]:not(.is-paired)");
          if (!left) return;
        }
        var r = b.getAttribute("data-right");
        setPair(el, left, left.getAttribute("data-paired") === r ? "" : r);
        pick(null);
        changed(el);
        var nextLeft = el.querySelector("[data-left]:not(.is-paired)");
        if (nextLeft) nextLeft.focus();
      });
    });
  });

  // ---- Esai: tumbuh mengikuti isi, hitung kata. ----
  function grow(t) {
    t.style.height = "auto";
    t.style.height = t.scrollHeight + 2 + "px";
    var w = t.parentNode.querySelector("[data-words]");
    if (w) {
      var n = t.value.trim() ? t.value.trim().split(/\s+/).length : 0;
      w.textContent = n + " kata";
    }
  }

  // ---- Perubahan jawaban → antrian simpan. ----
  var pending = {};
  var saveTimer = null;
  var posDirty = false;

  function changed(el) {
    var id = el.getAttribute("data-q");
    var prev = answers[id] || { v: null, f: false, s: 0 };
    var a = { v: readValue(el), f: prev.f, s: nextSeq() };
    answers[id] = a;
    pending[id] = a;
    paintCell(qIndex(el));
    schedule(1000);
  }

  qs.forEach(function (el) {
    el.addEventListener("change", function (ev) {
      if (ev.target.matches("input[type=radio], input[type=checkbox]")) changed(el);
    });
    el.addEventListener("input", function (ev) {
      if (ev.target.matches("textarea")) grow(ev.target);
      if (ev.target.matches("input.take-input, textarea")) changed(el);
    });
  });

  function setStatus(kind, text) {
    var s = root.querySelector("[data-save-status]");
    s.className = "take-save is-" + kind;
    s.querySelector("[data-save-text]").textContent = text;
  }

  function hasPending() { return Object.keys(pending).length > 0; }

  function schedule(ms) {
    if (saveTimer) clearTimeout(saveTimer);
    saveTimer = setTimeout(flush, ms);
  }

  var backoff = 0;
  var inflight = null;

  // flush mengirim antrian; mengembalikan Promise yang selesai saat antrian
  // terkirim (true) atau gagal (false).
  function flush() {
    if (saveTimer) { clearTimeout(saveTimer); saveTimer = null; }
    if (inflight) return inflight.then(function () { return hasPending() || posDirty ? flush() : true; });
    if (!hasPending() && !posDirty) return Promise.resolve(true);
    var batch = pending;
    pending = {};
    var sentPos = posDirty;
    posDirty = false;
    var changes = Object.keys(batch).map(function (k) { return { q: k, v: batch[k].v, f: batch[k].f, s: batch[k].s }; });
    setStatus("busy", "Menyimpan…");
    inflight = fetch(saveURL, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
      body: JSON.stringify({ changes: changes, pos: current })
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (body) { return { res: res, body: body }; });
    }).then(function (r) {
      inflight = null;
      if (r.body && r.body.now) offset = r.body.now - Date.now();
      if (r.res.status === 409 && r.body.closed) {
        submitting = true;
        window.location.href = r.body.redirect;
        return false;
      }
      if (!r.res.ok) throw new Error("save " + r.res.status);
      if (r.body.deadline) deadline = r.body.deadline;
      backoff = 0;
      if (hasPending()) { schedule(300); return true; }
      setStatus("ok", "Tersimpan");
      return true;
    }).catch(function () {
      inflight = null;
      // Kembalikan ke antrian kecuali sudah ada perubahan lebih baru.
      Object.keys(batch).forEach(function (k) { if (!pending[k] || pending[k].s < batch[k].s) pending[k] = batch[k]; });
      if (sentPos) posDirty = true;
      backoff = Math.min(backoff ? backoff * 2 : 2000, 30000);
      setStatus("offline", "Offline — jawaban aman di perangkat ini");
      schedule(backoff);
      return false;
    });
    return inflight;
  }

  window.addEventListener("online", function () { if (hasPending()) flush(); });
  window.addEventListener("beforeunload", function (ev) {
    if (!submitting && hasPending()) { ev.preventDefault(); ev.returnValue = ""; }
  });
  document.addEventListener("visibilitychange", function () { if (document.visibilityState === "hidden" && hasPending()) flush(); });

  // ---- Grid nomor dan tombol. ----
  function paintCell(i) {
    var a = answers[qid(i)];
    var cell = cells[i];
    var flagged = !!(a && a.f);
    var done = !!(a && complete(qs[i], a.v));
    cell.classList.toggle("is-flagged", flagged);
    cell.classList.toggle("is-answered", done && !flagged);
    var label = "Soal " + (i + 1) + (flagged ? ", ragu-ragu" : done ? ", terjawab" : ", kosong");
    cell.setAttribute("aria-label", label);
    if (i === current) {
      flagBtn.setAttribute("aria-pressed", flagged ? "true" : "false");
      qs[i].querySelector("[data-flag-badge]").hidden = !flagged;
    }
  }

  function canGo(i) { return state.back || i >= current; }

  function show(i, focus) {
    if (i < 0 || i >= qs.length || i === current) return;
    if (current >= 0 && !canGo(i)) return;
    var prevStim = current >= 0 ? qs[current].getAttribute("data-stim") : "0";
    if (current >= 0) {
      qs[current].hidden = true;
      cells[current].classList.remove("is-current");
      cells[current].removeAttribute("aria-current");
    }
    current = i;
    if (i > furthest) furthest = i;
    var el = qs[i];
    el.hidden = false;
    cells[i].classList.add("is-current");
    cells[i].setAttribute("aria-current", "step");
    var stim = el.getAttribute("data-stim");
    showStim(stim, stim !== prevStim);
    paintCell(i);
    prevBtn.disabled = i === 0 || !state.back;
    nextBtn.querySelector("span").textContent = i === qs.length - 1 ? "Selesai" : "Berikutnya";
    var qcol = root.querySelector("[data-qcol]");
    if (stage.classList.contains("has-stim") && window.innerWidth >= 1024) qcol.scrollTop = 0;
    else if (stim === "0" || stim !== prevStim) stage.scrollTop = 0;
    else {
      var top = el.offsetTop - 8;
      if (stage.scrollTop > top) stage.scrollTop = top;
    }
    el.querySelectorAll("textarea").forEach(grow);
    if (focus) {
      var h = el.querySelector(".take-qno");
      h.setAttribute("tabindex", "-1");
      h.focus({ preventScroll: true });
    }
    posDirty = true;
    schedule(hasPending() ? 1000 : 3000);
  }

  // ---- Teks bacaan. ----
  function showStim(n, changedGroup) {
    var has = n && n !== "0";
    stimCol.hidden = !has;
    stage.classList.toggle("has-stim", !!has);
    divider.hidden = !has;
    fab.hidden = !has;
    if (!changedGroup) return;
    stimCol.querySelectorAll("[data-stim]").forEach(function (s) {
      var on = s.getAttribute("data-stim") === n;
      s.hidden = !on;
      if (!on) closeSheet(s);
    });
  }

  function closeSheet(s) {
    if (!s.classList.contains("is-sheet")) return;
    s.classList.remove("is-sheet");
    document.body.style.overflow = "";
  }

  stimCol.querySelectorAll("[data-stim]").forEach(function (s) {
    var t = s.querySelector("[data-stim-toggle]");
    t.addEventListener("click", function () {
      var open = s.classList.toggle("is-open");
      t.setAttribute("aria-expanded", open ? "true" : "false");
      t.querySelector("[data-stim-toggle-text]").textContent = open ? "ketuk untuk melipat" : "ketuk untuk membaca";
    });
    s.querySelector("[data-stim-close]").addEventListener("click", function () { closeSheet(s); fab.focus(); });
  });

  fab.addEventListener("click", function () {
    var s = stimCol.querySelector("[data-stim]:not([hidden])");
    if (!s) return;
    s.classList.add("is-sheet");
    s.scrollTop = 0;
    s.querySelector("[data-stim-close]").focus();
  });

  // Pembatas kolom bisa digeser (mouse, sentuh, atau panah keyboard).
  (function () {
    var dragging = false;
    function setWidth(px) {
      var max = stage.clientWidth * 0.7, min = 288;
      px = Math.max(min, Math.min(max, px));
      stage.style.setProperty("--stim-w", px + "px");
      savePrefs({ stim: px / stage.clientWidth });
    }
    divider.addEventListener("pointerdown", function (e) { dragging = true; divider.setPointerCapture(e.pointerId); });
    divider.addEventListener("pointermove", function (e) {
      if (dragging) setWidth(e.clientX - stage.getBoundingClientRect().left);
    });
    divider.addEventListener("pointerup", function () { dragging = false; });
    divider.addEventListener("keydown", function (e) {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      e.preventDefault();
      e.stopPropagation();
      setWidth(stimCol.getBoundingClientRect().width + (e.key === "ArrowLeft" ? -40 : 40));
    });
  })();

  // ---- Navigasi. ----
  prevBtn.addEventListener("click", function () { show(current - 1, true); });
  nextBtn.addEventListener("click", function () {
    if (current === qs.length - 1) openSummary();
    else show(current + 1, true);
  });
  flagBtn.addEventListener("click", toggleFlag);
  function toggleFlag() {
    var id = qid(current);
    var a = answers[id] || { v: readValue(qs[current]), f: false, s: 0 };
    a = { v: a.v, f: !a.f, s: nextSeq() };
    answers[id] = a;
    pending[id] = a;
    paintCell(current);
    schedule(1000);
  }
  cells.forEach(function (c, i) {
    c.addEventListener("click", function () {
      show(i, true);
      panel.classList.remove("is-open");
    });
  });
  root.querySelector("[data-panel-open]").addEventListener("click", function () {
    panel.classList.add("is-open");
    var c = cells[current] || cells[0];
    c.focus();
  });
  root.querySelector("[data-panel-close]").addEventListener("click", function () { panel.classList.remove("is-open"); });

  // ---- Keyboard: 1–6 pilih jawaban, ←/→ pindah soal, R ragu-ragu. ----
  document.addEventListener("keydown", function (e) {
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    var t = e.target;
    if (t && (t.tagName === "TEXTAREA" || (t.tagName === "INPUT" && t.type !== "radio" && t.type !== "checkbox") || t.isContentEditable)) return;
    if (document.querySelector("dialog[open]") || !cover.hidden) return;
    var el = qs[current];
    if (e.key === "ArrowRight") { e.preventDefault(); if (current < qs.length - 1) show(current + 1, true); }
    else if (e.key === "ArrowLeft") { e.preventDefault(); show(current - 1, true); }
    else if (e.key === "r" || e.key === "R") { e.preventDefault(); toggleFlag(); }
    else if (/^[1-6]$/.test(e.key)) {
      var opts = el.querySelectorAll(".take-opt input");
      var x = opts[parseInt(e.key, 10) - 1];
      if (!x) return;
      e.preventDefault();
      x.checked = x.type === "checkbox" ? !x.checked : true;
      x.focus();
      changed(el);
    }
  });

  // ---- Ringkasan dan kumpul dua langkah. ----
  var dialog = root.querySelector("[data-summary]");
  function openSummary() {
    var answered = 0, flagged = [], empty = [];
    qs.forEach(function (el, i) {
      var a = answers[qid(i)];
      if (a && a.f) flagged.push(i);
      if (a && complete(el, a.v)) answered++;
      else empty.push(i);
    });
    dialog.querySelector("[data-sum-line]").textContent =
      "Terjawab " + answered + " dari " + qs.length + " · Ragu-ragu " + flagged.length + " · Kosong " + empty.length;
    var lists = dialog.querySelector("[data-sum-lists]");
    lists.textContent = "";
    [["Kosong atau belum lengkap", empty], ["Ragu-ragu", flagged]].forEach(function (p) {
      if (!p[1].length) return;
      var h = document.createElement("p");
      h.className = "mb-1 text-sm font-semibold";
      h.textContent = p[0] + ":";
      var box = document.createElement("div");
      box.className = "take-sum-list";
      p[1].forEach(function (i) {
        var b = document.createElement("button");
        b.type = "button";
        b.textContent = String(i + 1);
        b.setAttribute("aria-label", "Buka soal " + (i + 1));
        b.disabled = !canGo(i);
        b.addEventListener("click", function () { dialog.close(); show(i, true); });
        box.appendChild(b);
      });
      lists.appendChild(h);
      lists.appendChild(box);
    });
    dialog.querySelector('[data-step="1"]').hidden = false;
    dialog.querySelector('[data-step="2"]').hidden = true;
    dialog.querySelector("[data-sum-offline]").hidden = true;
    dialog.showModal();
    panel.classList.remove("is-open");
  }
  root.querySelector("[data-open-summary]").addEventListener("click", openSummary);
  dialog.querySelectorAll("[data-close]").forEach(function (b) { b.addEventListener("click", function () { dialog.close(); }); });
  dialog.querySelector("[data-to-step2]").addEventListener("click", function () {
    dialog.querySelector('[data-step="1"]').hidden = true;
    dialog.querySelector('[data-step="2"]').hidden = false;
    dialog.querySelector("[data-final-submit]").focus();
  });
  dialog.querySelector("[data-final-submit]").addEventListener("click", function () { submit("peserta"); });

  // submit mengirim antrian dulu, lalu mengumpulkan. Saat offline peserta
  // diberi tahu; waktu habis tetap mengumpulkan jawaban yang sudah sampai.
  function submit(reason) {
    if (submitting) return;
    var btn = dialog.querySelector("[data-final-submit]");
    btn.disabled = true;
    flush().then(function (ok) {
      if (!ok && reason !== "waktu") {
        btn.disabled = false;
        dialog.querySelector('[data-step="1"]').hidden = false;
        dialog.querySelector('[data-step="2"]').hidden = true;
        dialog.querySelector("[data-sum-offline]").hidden = false;
        return;
      }
      submitting = true;
      var f = root.querySelector("[data-submit-form]");
      f.querySelector('[name="alasan"]').value = reason;
      f.submit();
    });
  }

  // ---- Hitung mundur dengan jam server. ----
  var timer = root.querySelector("[data-timer]");
  var timerText = root.querySelector("[data-timer-text]");
  var banner = root.querySelector("[data-banner]");
  var warned = {};
  function pad(n) { return (n < 10 ? "0" : "") + n; }
  function tick() {
    var left = Math.max(0, Math.floor((deadline - (Date.now() + offset)) / 1000));
    var h = Math.floor(left / 3600), m = Math.floor((left % 3600) / 60), s = left % 60;
    timerText.textContent = (h ? h + ":" + pad(m) : pad(m)) + ":" + pad(s);
    timer.classList.toggle("is-warn", left <= 600 && left > 60);
    timer.classList.toggle("is-danger", left <= 60);
    [[600, "Sisa waktu 10 menit."], [60, "Sisa waktu 1 menit. Periksa jawaban Anda."]].forEach(function (w) {
      if (left <= w[0] && left > 0 && !warned[w[0]]) {
        warned[w[0]] = true;
        banner.textContent = w[1];
        banner.hidden = false;
        if (navigator.vibrate) navigator.vibrate(300);
        setTimeout(function () { banner.hidden = true; }, 6000);
      }
    });
    if (left === 0 && !submitting) {
      banner.textContent = "Waktu habis — jawaban dikumpulkan.";
      banner.hidden = false;
      submit("waktu");
    }
  }
  setInterval(tick, 500);
  tick();

  // ---- Preferensi tampilan per peserta: ukuran huruf, kontras. ----
  var prefs = {};
  try { prefs = JSON.parse(localStorage.getItem(state.key) || "{}") || {}; } catch (e) { prefs = {}; }
  function savePrefs(p) {
    Object.keys(p).forEach(function (k) { prefs[k] = p[k]; });
    try { localStorage.setItem(state.key, JSON.stringify(prefs)); } catch (e) { /* penyimpanan penuh/dimatikan */ }
  }
  function applySize(sz) {
    root.setAttribute("data-size", sz);
    root.querySelectorAll("[data-size-btn]").forEach(function (b) { b.setAttribute("aria-pressed", b.getAttribute("data-size-btn") === sz ? "true" : "false"); });
  }
  var contrast = root.querySelector("[data-contrast]");
  applySize(prefs.size || "m");
  contrast.checked = !!prefs.contrast;
  root.classList.toggle("is-contrast", contrast.checked);
  if (prefs.stim) stage.style.setProperty("--stim-w", Math.round(prefs.stim * 100) + "%");
  root.querySelectorAll("[data-size-btn]").forEach(function (b) {
    b.addEventListener("click", function () { applySize(b.getAttribute("data-size-btn")); savePrefs({ size: b.getAttribute("data-size-btn") }); });
  });
  contrast.addEventListener("change", function () { root.classList.toggle("is-contrast", contrast.checked); savePrefs({ contrast: contrast.checked }); });
  var setBtn = root.querySelector("[data-settings-btn]");
  setBtn.addEventListener("click", function () {
    var box = root.querySelector("[data-settings]");
    box.hidden = !box.hidden;
    setBtn.setAttribute("aria-expanded", box.hidden ? "false" : "true");
  });

  // ---- Perbesar gambar. ----
  var zoom = root.querySelector("[data-zoom]");
  root.addEventListener("click", function (e) {
    var img = e.target.closest && e.target.closest(".exam-content img");
    if (!img || img.closest(".take-opt, .take-left, .take-right")) return;
    zoom.querySelector("[data-zoom-img]").src = img.currentSrc || img.src;
    zoom.hidden = false;
    zoom.querySelector("[data-zoom-close]").focus();
  });
  zoom.addEventListener("click", function (e) { if (e.target === zoom || e.target.closest("[data-zoom-close]")) zoom.hidden = true; });

  // ---- Layar awal: layar penuh (butuh sentuhan pengguna). ----
  var cover = root.querySelector("[data-cover]");
  root.querySelector("[data-cover-go]").addEventListener("click", function () {
    var d = document.documentElement;
    var fs = d.requestFullscreen || d.webkitRequestFullscreen;
    if (fs) { try { var p = fs.call(d); if (p && p.catch) p.catch(function () {}); } catch (e) { /* tidak didukung */ } }
    cover.hidden = true;
    var h = qs[current].querySelector(".take-qno");
    h.setAttribute("tabindex", "-1");
    h.focus({ preventScroll: true });
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") {
      if (!zoom.hidden) zoom.hidden = true;
      panel.classList.remove("is-open");
      var s = stimCol.querySelector(".is-sheet");
      if (s) closeSheet(s);
    }
  });

  // ---- Isi awal dari server. ----
  qs.forEach(function (el, i) {
    var a = answers[qid(i)];
    if (a) applyValue(el, a.v);
    paintCell(i);
  });
  var start = Math.min(Math.max(0, state.pos || 0), qs.length - 1);
  furthest = start;
  show(start, false);
  posDirty = false;
  if (saveTimer) { clearTimeout(saveTimer); saveTimer = null; }
})();
