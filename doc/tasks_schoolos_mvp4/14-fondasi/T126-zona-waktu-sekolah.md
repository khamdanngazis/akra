# T126 — Zona waktu sekolah

**Fase:** 14 Fondasi
**Dependensi:** —
**Estimasi:** 2 hari

## Tujuan
Jadwal ujian tepat untuk sekolah di WIB, WITA, maupun WIT. Saat ini seluruh aplikasi memakai `clock.Jakarta` (28 tempat), sehingga sekolah di Makassar atau Jayapura melihat jam yang salah.

## Langkah
1. Kolom `schools.timezone` (`Asia/Jakarta`, `Asia/Makassar`, `Asia/Jayapura`), bawaan `Asia/Jakarta`. Diatur di profil sekolah.
2. Fungsi `clock.For(school)` atau lokasi di `auth.Identity` (`id.Location`). Semua tampilan dan input jam/tanggal di modul ujian memakainya.
3. Ganti pemakaian `clock.Jakarta` di modul lain secara bertahap:
   - **wajib di task ini:** tanggal "hari ini" untuk absensi, jatuh tempo tagihan, jadwal tes pendaftaran, dan cap waktu dokumen;
   - sisanya dicatat sebagai daftar periksa.
4. Penyimpanan tetap `timestamptz` (UTC). Konversi hanya di tepi (parse input, format tampilan).
5. Uji dengan sekolah di `Asia/Jayapura`: jadwal ujian 08.00 WIT dibuka pukul 06.00 WIB di server.

## File yang disentuh
- `db/migrations/0000XX_school_timezone.up.sql` dan `.down.sql`
- `internal/platform/clock`, `internal/auth` (identity), `internal/school` (profil)
- Modul yang memakai `clock.Jakarta` (bertahap)

## Kriteria selesai
- [ ] Sekolah WIT melihat dan memasukkan jam ujian dalam WIT
- [ ] "Hari ini" absensi sekolah WIT berganti pada tengah malam WIT
- [ ] Tidak ada pemakaian `clock.Jakarta` tersisa di `internal/exam` (uji grep di CI)
