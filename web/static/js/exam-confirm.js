// Konfirmasi sebelum ujian (T138): tes suara untuk sesi berbunyi tanda.
// Tombol Mulai aktif setelah peserta menyatakan bunyi terdengar (atau
// melaporkan tidak terdengar; pengawas tetap melihat tanda di layarnya).
(function () {
  "use strict";
  document.addEventListener("DOMContentLoaded", function () {
    var form = document.querySelector("[data-start-form]");
    if (!form || !form.hasAttribute("data-need-sound")) return;
    var start = form.querySelector("[data-start]");
    var play = form.querySelector("[data-sound-play]");
    var yes = form.querySelector("[data-sound-yes]");
    var no = form.querySelector("[data-sound-no]");
    var note = form.querySelector("[data-sound-note]");
    var Ctx = window.AudioContext || window.webkitAudioContext;
    if (!Ctx) {
      play.hidden = true;
      note.textContent = "Browser ini tidak bisa membunyikan tanda. Beri tahu pengawas.";
      return;
    }
    start.disabled = true;
    note.textContent = "Tekan Tes bunyi dulu sebelum mulai.";
    play.addEventListener("click", function () {
      var ctx = new Ctx();
      // Nada naik-turun khas, berbeda dari nada notifikasi.
      [0, 0.25, 0.5].forEach(function (t, i) {
        var osc = ctx.createOscillator();
        var gain = ctx.createGain();
        osc.type = "square";
        osc.frequency.value = i % 2 ? 660 : 990;
        gain.gain.value = 0.25;
        osc.connect(gain).connect(ctx.destination);
        osc.start(ctx.currentTime + t);
        osc.stop(ctx.currentTime + t + 0.2);
      });
      yes.hidden = no.hidden = false;
    });
    yes.addEventListener("click", function () {
      note.textContent = "Bunyi terdengar. Silakan mulai.";
      start.disabled = false;
      start.focus();
    });
    no.addEventListener("click", function () {
      note.textContent = "Naikkan volume dan matikan mode senyap, lalu tes lagi. Bila tetap tidak terdengar, beri tahu pengawas.";
      start.disabled = false;
    });
  });
})();
