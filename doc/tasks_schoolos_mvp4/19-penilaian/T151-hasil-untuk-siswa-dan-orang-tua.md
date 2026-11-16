# T151 — Hasil dan jadwal untuk siswa dan orang tua

**Fase:** 19 Penilaian
**Dependensi:** T124, T134, T148, T75
**Estimasi:** 2 hari

## Tujuan
Siswa dan orang tua mendapat informasi yang berguna (jadwal ujian, nilai, pembahasan bila diizinkan) tanpa membuka data yang tidak semestinya.

## Langkah
1. **Beranda siswa** (`/u/beranda`, login dengan kode kartu ujian; setelah sesi berakhir kode tetap berlaku hanya untuk melihat hasil sampai 30 hari, lihat T124):
   - jadwal ujian yang akan datang;
   - hasil ujian yang sudah dibuka: nilai, dan pembahasan bila `show_result = nilai_dan_pembahasan`.
   - Hasil dibuka setelah sesi terakhir ujian itu (termasuk susulan yang terjadwal) agar soal tidak bocor.
2. **Pembahasan:** soal, jawaban siswa, kunci, dan pembahasan; per TP ditampilkan "sudah dikuasai / perlu belajar lagi".
3. **Calon siswa:** tidak melihat nilai maupun pembahasan (keputusan). Hasil seleksi tetap lewat halaman status pendaftaran (T95).
4. **Portal orang tua** (T75):
   - jadwal ujian anak (dari agenda T134 yang sudah diumumkan);
   - nilai ujian setelah dibuka guru (tanpa detail jawaban);
   - status "tidak hadir" dan jadwal susulan bila ada.
5. Orang tua tidak pernah melihat soal atau kunci (menjaga bank soal sekolah).

## File yang disentuh
- `internal/exam/student_results.go`, tes, `internal/portal/exam_handler.go`
- `web/templates/exam/participant_home.html`, `participant_review.html`, `web/templates/portal/exams.html`

## Kriteria selesai
- [ ] Pembahasan tidak terbuka sebelum sesi terakhir (termasuk susulan)
- [ ] Calon siswa tidak bisa membuka nilai atau pembahasan lewat URL apa pun
- [ ] Orang tua hanya melihat anaknya sendiri, tanpa soal atau kunci (uji T59)
