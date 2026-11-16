# T137 — Integrasi dengan tes seleksi pendaftaran

**Fase:** 17 Penilaian
**Dependensi:** T135, T93a, T119
**Estimasi:** 1,5 hari

## Tujuan
Tes seleksi pendaftaran bisa dikerjakan online, dan nilainya langsung masuk ke peringkat tanpa diketik TU.

## Langkah
1. Di tab "Tes seleksi" gelombang (T119), setiap tes punya pilihan **cara tes**: "Di kertas / wawancara" (seperti sekarang) atau **"Ujian online"**. Untuk ujian online, pilih paket ujian (`purpose = seleksi`).
   - Migrasi: `admission_tests.exam_id NULL`.
   - `max_score` mengikuti skala nilai ujian (100).
2. Tombol **"Buat jadwal ujian"**: membuat sesi (T128) dengan peserta pendaftar `diverifikasi` di jalur bertes.
3. Jadwal, ruang, dan **kode akses** tampil di kartu tes halaman status pendaftaran dan bukti pendaftaran (T95, T119).
4. Saat nilai attempt final (setelah esai dinilai), nilai ditulis ke `applicant_test_scores` dengan `recorded_by` = sistem/penilai.
   - Pendaftar yang tidak hadir tetap NULL (belum lengkap), bukan 0.
   - Nilai minimal tes (T119) berlaku seperti biasa.
5. Nilai dari ujian online **tidak bisa disunting** di grid "Isi nilai". Ditandai ikon "dari ujian online" dengan tautan ke detail attempt. Koreksi lewat nilai ulang (T135).
6. Kartu "Nilai tes" di detail pendaftar menampilkan sumber nilai dan tautan ke hasil ujian.

## File yang disentuh
- `db/migrations/0000XX_admission_test_exam.up.sql` dan `.down.sql`
- `internal/admission/selection.go`, `selection_handler.go`
- `internal/exam/sync_admission.go`, `sync_admission_test.go`
- `web/templates/admission/tests.html`, `public_status.html`

## Kriteria selesai
- [ ] Nilai ujian online masuk ke peringkat (T93b, T118) tanpa input manual
- [ ] Tidak hadir = NULL, bukan 0
- [ ] Nilai ulang di T135 memperbarui `applicant_test_scores`
- [ ] Ujian milik sekolah lain tidak bisa dipilih sebagai tes (uji lintas sekolah)
