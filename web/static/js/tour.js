// Tur singkat yang bisa dilewati (T154). Halaman memasang
// data-tour="nama" pada satu elemen dan data-tour-step="1..n" beserta
// data-tour-text (dan opsional data-tour-title) pada elemen yang ditunjuk.
// Tur tampil sekali per browser; tombol data-tour-start memutar ulang.
(function () {
  "use strict";

  function storageKey(name) { return "akra.tour." + name; }
  function seen(name) {
    try { return localStorage.getItem(storageKey(name)) === "1"; } catch (e) { return false; }
  }
  function markSeen(name) {
    try { localStorage.setItem(storageKey(name), "1"); } catch (e) { /* mode privat: tur tampil lagi */ }
  }

  // steps mengambil langkah yang elemennya terlihat, urut nomor.
  function steps() {
    var list = [];
    document.querySelectorAll("[data-tour-step]").forEach(function (el) {
      if (el.offsetParent === null && getComputedStyle(el).position !== "fixed") return;
      list.push({ n: parseInt(el.getAttribute("data-tour-step"), 10) || 0, el: el });
    });
    list.sort(function (a, b) { return a.n - b.n; });
    return list;
  }

  function start(name) {
    var list = steps();
    if (!list.length) return;
    var i = 0;
    var card = document.createElement("div");
    card.className = "tour-card";
    card.setAttribute("role", "dialog");
    card.setAttribute("aria-live", "polite");
    card.setAttribute("aria-label", "Tur singkat");
    card.innerHTML =
      '<p class="tour-count"></p><p class="tour-title"></p><p class="tour-text"></p>' +
      '<div class="tour-actions"><button type="button" class="btn btn-ghost btn-sm" data-tour-skip>Lewati</button>' +
      '<button type="button" class="btn btn-primary btn-sm" data-tour-next></button></div>';
    document.body.appendChild(card);
    var current = null;

    function clear() {
      if (current) current.classList.remove("tour-target");
      current = null;
    }
    function end() {
      clear();
      markSeen(name);
      card.remove();
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("resize", place);
    }
    function place() {
      if (!current) return;
      var r = current.getBoundingClientRect();
      var w = card.offsetWidth, h = card.offsetHeight;
      var top = r.bottom + 10;
      if (top + h > window.innerHeight - 8) top = Math.max(8, r.top - h - 10);
      var left = Math.min(Math.max(8, r.left), window.innerWidth - w - 8);
      card.style.top = top + "px";
      card.style.left = left + "px";
    }
    function show() {
      clear();
      // Elemen bisa diganti (mis. data langsung layar pengawas): cari ulang.
      var fresh = steps();
      if (i >= fresh.length) { end(); return; }
      current = fresh[i].el;
      current.classList.add("tour-target");
      current.scrollIntoView({ block: "center", behavior: "smooth" });
      card.querySelector(".tour-count").textContent = (i + 1) + " dari " + fresh.length;
      var title = current.getAttribute("data-tour-title") || "";
      card.querySelector(".tour-title").textContent = title;
      card.querySelector(".tour-title").hidden = !title;
      card.querySelector(".tour-text").textContent = current.getAttribute("data-tour-text") || "";
      card.querySelector("[data-tour-next]").textContent = i === fresh.length - 1 ? "Selesai" : "Berikutnya";
      setTimeout(place, 250);
      place();
    }
    function onKey(e) { if (e.key === "Escape") end(); }

    card.querySelector("[data-tour-skip]").addEventListener("click", end);
    card.querySelector("[data-tour-next]").addEventListener("click", function () { i++; show(); });
    document.addEventListener("keydown", onKey);
    window.addEventListener("resize", place);
    show();
    card.querySelector("[data-tour-next]").focus({ preventScroll: true });
  }

  function init() {
    var root = document.querySelector("[data-tour]");
    if (!root) return;
    var name = root.getAttribute("data-tour");
    document.querySelectorAll("[data-tour-start]").forEach(function (b) {
      b.hidden = false;
      b.addEventListener("click", function () { if (!document.querySelector(".tour-card")) start(name); });
    });
    if (!seen(name)) setTimeout(function () { start(name); }, 400);
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
