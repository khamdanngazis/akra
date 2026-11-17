// Wizard ujian (T133): seret untuk mengubah urutan soal di satu bagian.
// Tanpa JavaScript, tombol naik/turun tetap bisa dipakai.
(function () {
  "use strict";
  function init(form) {
    var list = form.querySelector(".exam-items");
    if (!list) return;
    var dragged = null;
    list.addEventListener("dragstart", function (e) {
      dragged = e.target.closest(".exam-item");
      if (!dragged) return;
      dragged.classList.add("is-dragging");
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", dragged.id);
    });
    list.addEventListener("dragover", function (e) {
      if (!dragged) return;
      e.preventDefault();
      var over = e.target.closest(".exam-item");
      if (!over || over === dragged) return;
      var box = over.getBoundingClientRect();
      var after = e.clientY > box.top + box.height / 2;
      list.insertBefore(dragged, after ? over.nextSibling : over);
    });
    list.addEventListener("dragend", function () {
      if (!dragged) return;
      dragged.classList.remove("is-dragging");
      dragged = null;
      save(form);
    });
  }
  function save(form) {
    var body = new URLSearchParams(new FormData(form));
    fetch(form.action, {
      method: "POST",
      body: body,
      headers: { "X-Requested-With": "fetch" },
      credentials: "same-origin",
    }).then(function () {
      // Muat ulang agar kelompok stimulus, aturan, dan panel ringkas segar.
      window.location.reload();
    });
  }
  document.addEventListener("DOMContentLoaded", function () {
    document.querySelectorAll("form[data-reorder]").forEach(init);
  });
})();
