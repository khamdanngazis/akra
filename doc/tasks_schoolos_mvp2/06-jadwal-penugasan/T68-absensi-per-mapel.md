# T68 — Absensi per mapel **[wawancara]**

**Fase:** 06 Jadwal dan Penugasan
**Dependensi:** T65, T25, T26
**Estimasi:** 1–2 hari

## Tujuan
Jika sekolah membutuhkannya, absensi bisa dicatat per mata pelajaran, bukan hanya harian per kelas.

## Syarat mulai
Hanya dikerjakan jika wawancara menunjukkan sekolah ingin absensi per mapel. Jika tidak, task ini ditutup dan tidak dikerjakan.

## Langkah
1. Migrasi:
   - Tambah kolom `subject_id uuid NULL REFERENCES subjects(id)` ke `attendance_sessions`.
   - Ubah unique constraint dari `(class_id, session_date)` menjadi `(class_id, session_date, COALESCE(subject_id, '00000000-0000-0000-0000-000000000000'))`. Gunakan unique index, bukan constraint biasa, karena ekspresi `COALESCE` perlu index.
2. Mode absensi harian tetap berlaku. Sesi dengan `subject_id` NULL adalah absensi harian.
3. Form absensi (T26) menampilkan pilihan mapel jika sekolah mengaktifkannya lewat `school_settings`.
4. Guru hanya bisa membuat sesi untuk mapel yang ditugaskan kepadanya (T65).
5. Rekap T27 diperbarui agar bisa difilter per mapel.

## File yang disentuh
- `db/migrations/0000XX_attendance_per_subject.up.sql` dan `.down.sql`
- `internal/attendance/service.go`, `repo.go`, `report.go`
- `web/templates/attendance/form.html`

## Kriteria selesai
- [ ] Dua sesi untuk kelas, tanggal, dan mapel yang sama ditolak
- [ ] Sesi harian dan sesi per mapel untuk tanggal yang sama bisa berdampingan
- [ ] Guru tanpa penugasan mapel tidak bisa membuat sesi mapel tersebut
- [ ] Rekap T27 lama tetap menghasilkan angka yang sama untuk data harian

## Catatan
- Jika sekolah tidak membutuhkan task ini, daftar task dikurangi satu dan tidak ada perubahan skema.
