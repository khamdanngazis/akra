// Papan pembagian ruang agenda (T134): seret kelas ke ruang+gelombang lain.
// Memakai form "Pindahkan kelas" yang juga bisa dipakai tanpa JavaScript.
(function () {
  "use strict";
  document.addEventListener("DOMContentLoaded", function () {
    var form = document.querySelector("form[data-move-form]");
    if (!form) return;
    var from = null;
    document.querySelectorAll(".room-chip[draggable=true]").forEach(function (chip) {
      chip.addEventListener("dragstart", function (e) {
        from = chip.dataset.from;
        e.dataTransfer.effectAllowed = "move";
        e.dataTransfer.setData("text/plain", from);
      });
    });
    document.querySelectorAll(".room-unit[data-target]").forEach(function (unit) {
      unit.addEventListener("dragover", function (e) {
        if (!from) return;
        e.preventDefault();
        unit.classList.add("is-drop");
      });
      unit.addEventListener("dragleave", function () { unit.classList.remove("is-drop"); });
      unit.addEventListener("drop", function (e) {
        e.preventDefault();
        unit.classList.remove("is-drop");
        if (!from) return;
        form.elements.from.value = from;
        form.elements.to.value = unit.dataset.target;
        from = null;
        form.submit();
      });
    });
  });
})();
