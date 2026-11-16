# T125 — Peran koordinator ujian, pengawas, dan aktivasi modul

**Fase:** 14 Fondasi
**Dependensi:** —
**Estimasi:** 2 hari

## Tujuan
Pekerjaan ujian tidak bertumpu pada TU saja. Wakasek kurikulum atau panitia (biasanya guru) bisa mengatur ujian sekolah. Modul bisa dinyalakan per sekolah secara bertahap.

## Langkah
1. **Tugas tambahan per tahun ajaran** (bukan peran login baru): `staff_duties` (`school_id`, `academic_year_id`, `user_id`, `duty`).
   - Tugas MVP 4: `koordinator_ujian`.
   - Diatur admin_tu di **Pengaturan → Tugas tambahan**.
   - Guru dengan tugas ini mendapat menu "Agenda ujian" dan hak koordinator.
2. **Pengawas** ditetapkan per sesi (T134). Hak pengawas hanya berlaku untuk sesi itu, dari 30 menit sebelum sampai 2 jam sesudah sesi.
3. **Matriks akses modul ujian.** Ditulis sebagai tabel di kode dan diuji:

   | Aksi | admin_tu | koordinator | guru mapel | pengawas sesi | kepala |
   |---|---|---|---|---|---|
   | Bank soal mapel | semua | semua | mapel yang diampu | – | baca |
   | Ujian kelas sendiri | ya | ya | ya | – | baca |
   | Agenda PTS/PAS | ya | ya | – | – | baca |
   | Tes seleksi | ya | – | – | – | baca |
   | Layar pengawas | ya | ya | – | sesi itu | baca |
   | Koreksi esai | ya | ya | ujian miliknya / ditugaskan | – | – |
   | Kirim ke rapor | – | – | ujian miliknya | – | – |

4. **Aktivasi modul per sekolah:**
   - setting `exam.enabled` dan `exam.release` (`r1`, `r2`, `r3`);
   - menu dan route ujian hanya aktif bila modul aktif. Route tetap terlindungi, menu hanya disembunyikan;
   - diatur pengelola platform, bukan sekolah.

## File yang disentuh
- `db/migrations/0000XX_staff_duties.up.sql` dan `.down.sql`
- `internal/auth` (cek tugas), `internal/exam/access.go`, `access_test.go`
- `internal/platform/settings`, `internal/platform/nav/menu.go`
- `web/templates/settings/duties.html`

## Kriteria selesai
- [ ] Matriks akses diuji tabel per sel (izin dan tolak)
- [ ] Hak pengawas kedaluwarsa setelah jendela sesi
- [ ] Sekolah dengan modul nonaktif: menu tidak tampil, route ujian 404
- [ ] Tugas koordinator per tahun ajaran; tahun baru tidak mewarisi otomatis
