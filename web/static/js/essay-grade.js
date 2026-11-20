// Layar koreksi esai (T147): tombol skor cepat dan pintasan keyboard.
// Angka 0–9 mengisi skor, Enter menyimpan dan membuka jawaban berikutnya
// (Ctrl+Enter dari kolom komentar), panah kiri/kanan pindah jawaban.
(function () {
  "use strict";
  var form = document.querySelector("[data-essay-form]");
  if (!form) return;
  var input = form.querySelector('[name="skor"]');
  var comment = form.querySelector('[name="komentar"]');
  var buttons = Array.prototype.slice.call(form.querySelectorAll("[data-score]"));

  function mark() {
    buttons.forEach(function (b) { b.setAttribute("aria-pressed", b.getAttribute("data-score") === input.value.trim() ? "true" : "false"); });
  }
  buttons.forEach(function (b) {
    b.addEventListener("click", function () { input.value = b.getAttribute("data-score"); mark(); input.focus(); });
  });
  input.addEventListener("input", mark);
  mark();
  if (!input.value) input.focus();

  function go(sel) { var a = document.querySelector(sel); if (a) window.location.href = a.href; }

  document.addEventListener("keydown", function (e) {
    if (e.altKey || e.metaKey) return;
    var inComment = e.target === comment;
    if (e.key === "Enter" && (e.ctrlKey || !inComment)) {
      if (e.target.tagName === "A" || (e.target.tagName === "BUTTON" && e.target.type !== "button")) return;
      e.preventDefault();
      if (form.requestSubmit) form.requestSubmit(); else form.submit();
      return;
    }
    if (inComment || e.ctrlKey) return;
    var typing = e.target === input;
    if (/^[0-9]$/.test(e.key) && !typing) {
      var b = buttons.filter(function (x) { return x.getAttribute("data-score") === e.key; })[0];
      if (b) { e.preventDefault(); b.click(); }
      return;
    }
    if (!typing && e.key === "ArrowLeft") go("[data-prev]");
    if (!typing && e.key === "ArrowRight") go("[data-next]");
  });
})();
