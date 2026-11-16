# T128 — Jadwal sesi dan peserta

**Fase:** 15 Paket dan Jadwal
**Dependensi:** T127
**Estimasi:** 2,5 hari

## Tujuan
Ujian dijadwalkan per sesi dengan peserta dan pengawas yang jelas, karena lab tidak muat semua peserta sekaligus.

## Langkah
1. Migrasi `exam_sessions`:
   - `id`, `school_id`, `exam_id`, `name` (mis. "Sesi 1 · Lab A");
   - `starts_at`, `ends_at`: jendela waktu login, bukan durasi;
   - `room NULL`, `proctor_ids uuid[]` (guru pengawas);
   - `token_required bool`, `token`, `token_rotated_at`;
   - `late_entry_minutes`: batas terlambat masuk;
   - `location_mode`: `di_ruang` (lab atau HP di kelas, ada pengawas) atau `jarak_jauh` (dari rumah). Menentukan bunyi tanda keluar halaman (T133);
   - `alert_sound bool`: bawaan aktif untuk `di_ruang` dan mati untuk `jarak_jauh`, bisa diubah per sesi;
   - `seb_required bool` (opsional, T133).
2. Migrasi `exam_participants`:
   - `session_id`, `school_id`;
   - `student_id NULL` atau `applicant_id NULL`: tepat satu terisi;
   - `access_code_hash` (T129), `extra_minutes` (mis. untuk peserta berkebutuhan khusus).
3. Menambah peserta:
   - **Siswa:** pilih kelas/rombel (tahun ajaran aktif), lalu bisa dikurangi atau ditambah per siswa.
   - **Calon siswa:** otomatis dari pendaftar `diverifikasi` di jalur bertes gelombang terkait (T137). Pendaftar yang terverifikasi belakangan ditambahkan ke sesi yang dipilih TU.
   - Bagi rata ke beberapa sesi/ruang (mis. 300 peserta → 10 sesi × 30 kursi lab).
4. Satu peserta hanya boleh berada di satu sesi per paket. Pindah sesi dilakukan lewat tombol.
5. **Token sesi:** 6 huruf, diganti otomatis tiap 15 menit (bisa diatur), ditampilkan di halaman pengawas (T134). Mirip praktik ANBK yang sudah dikenal sekolah.
6. Daftar hadir dan berita acara sesi bisa dicetak PDF.

## File yang disentuh
- `db/migrations/0000XX_exam_sessions.up.sql` dan `.down.sql`
- `internal/exam/session.go`, `session_test.go`, `session_handler.go`, `attendance_pdf.go`
- `web/templates/exam/sessions.html`, `session_form.html`

## Kriteria selesai
- [ ] Siswa/pendaftar sekolah lain tidak bisa ditambahkan sebagai peserta (FK `school_id` dan uji lintas sekolah)
- [ ] Peserta ganda dalam satu paket ditolak
- [ ] Token lama tidak berlaku setelah rotasi, kecuali masa tenggang 1 menit
- [ ] Bagi rata 300 peserta ke 10 sesi menghasilkan 30 per sesi
