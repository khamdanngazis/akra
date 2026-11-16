# T134 — Agenda ujian sekolah (PTS/PAS)

**Fase:** 16 Persiapan Ujian
**Dependensi:** T125, T133
**Estimasi:** 5 hari

## Tujuan
Koordinator ujian mengatur PTS/PAS satu sekolah dari satu halaman: jadwal mapel per hari, ruang, sesi, peserta, dan pengawas. Pekerjaan berhari-hari dengan Excel menjadi hitungan jam.

## Langkah
1. **Agenda** (`exam_events`): nama ("PAS Ganjil 2026/2027"), tujuan, tanggal mulai–selesai, tingkat yang ikut.
2. **Ruang** (`exam_rooms`): nama, kapasitas, jenis (`lab` dengan komputer, atau `kelas` untuk HP). Disimpan per sekolah dan dipakai ulang di agenda berikutnya.
3. **Jadwal mapel:**
   - grid hari × jam × tingkat; isi sel dengan mapel dan durasi;
   - setiap sel menunjuk **ujian** milik guru mapel (T133), yang dibuat guru atau dibuatkan koordinator dengan meminta guru mengisi soal;
   - status per sel: soal belum ada / disusun / siap / ditelaah.
4. **Pembagian peserta ke ruang dan sesi otomatis:**
   - input: tingkat, jumlah siswa per kelas, ruang, dan kapasitasnya;
   - aturan: satu kelas tetap bersama bila muat; lab dibagi beberapa sesi bila siswa lebih banyak dari komputer; urut nomor peserta;
   - hasil bisa disesuaikan dengan seret (pindah siswa atau kelas antar-ruang/sesi);
   - nomor peserta dibuat (format diatur, mis. `{tahun}-{tingkat}-{urut:3}`).
5. **Pengawas:**
   - tetapkan per ruang per sesi, otomatis bergilir dari daftar guru;
   - aturan: guru tidak mengawasi mapelnya sendiri, beban merata, cek bentrok dengan jadwal lain;
   - koordinator bisa menukar pengawas.
6. **Ringkasan kesiapan agenda:** jumlah ujian siap per hari, peserta tanpa ruang, ruang tanpa pengawas, bentrok. Bisa dibuka kepala sekolah.
7. **Jadwal untuk siswa dan orang tua:** tampil di beranda siswa (`/u/beranda`) dan portal orang tua (T151) setelah diumumkan.

## File yang disentuh
- `db/migrations/0000XX_exam_events.up.sql` dan `.down.sql`
- `internal/exam/event.go`, `allocate.go` (pembagian ruang), `proctor_roster.go`, tes, `event_handler.go`
- `web/templates/exam/events.html`, `event_detail.html`, `event_schedule.html`, `event_rooms.html`

## Kriteria selesai
- [ ] 600 siswa, 12 kelas, 4 lab × 40 komputer, 8 ruang kelas → pembagian valid tanpa melebihi kapasitas (uji unit)
- [ ] Pengawas tidak mendapat mapelnya sendiri, dan bentrok dilaporkan
- [ ] Penyesuaian manual (seret) menjaga kapasitas dan memberi peringatan bila melebihi
- [ ] Koordinator menyusun agenda PAS 5 hari untuk 3 tingkat dalam ≤ 2 jam (uji dengan koordinator pilot)
