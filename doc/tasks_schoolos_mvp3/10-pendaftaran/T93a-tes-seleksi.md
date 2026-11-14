# T93a — Tes seleksi dan nilai

**Fase:** 10 Pendaftaran
**Dependensi:** T89, T93
**Estimasi:** 2 hari

## Tujuan
Sekolah bisa mengatur tes seleksi (mis. tes tertulis, wawancara, baca Al-Qur'an), menjadwalkannya, dan mengisi nilai pendaftar, untuk jalur yang memakai tes.

## Langkah
1. Migrasi `admission_tests`: `id`, `school_id`, `period_id`, `name`, `weight int` (bobot persen), `max_score` (bawaan 100), `scheduled_at timestamptz NULL`, `location text NULL`, `sort_order`. Jumlah bobot per periode harus 100 sebelum nilai bisa dihitung.
2. Migrasi `applicant_test_scores`: `applicant_id`, `test_id`, `score numeric(5,2) NULL` (NULL = belum dinilai / tidak hadir), `recorded_by`, `updated_at`. `PRIMARY KEY (applicant_id, test_id)`.
3. Hanya pendaftar berstatus `diverifikasi` di jalur yang `uses_test` yang bisa dinilai.
4. Halaman `GET/POST /pendaftaran/periode/{id}/tes`: kelola tes dan jadwal.
5. Halaman input nilai per tes: grid pendaftar × nilai, simpan dalam satu transaksi (seperti input nilai T70). Validasi 0–`max_score`.
6. Skor akhir = Σ(score/max_score × weight); pendaftar dengan nilai kosong di salah satu tes ditandai "belum lengkap", tidak dihitung 0.
7. Jadwal tes tampil di halaman cek status pendaftar (T95) dan bukti pendaftaran.
8. Akses: `admin_tu`. Peran penguji terpisah di luar cakupan.

## File yang disentuh
- `db/migrations/0000XX_admission_tests.up.sql` dan `.down.sql`
- `internal/admission/test.go`, `test_test.go`, `test_handler.go`
- `web/templates/admission/tests.html`, `scores.html`

## Kriteria selesai
- [ ] Bobot yang jumlahnya bukan 100 ditolak saat menghitung skor
- [ ] Nilai di luar 0–max ditolak per baris
- [ ] Skor akhir dihitung dengan uji unit (termasuk nilai kosong)
- [ ] Pendaftar yang belum diverifikasi tidak muncul di grid nilai
- [ ] Route baru masuk daftar uji T58 dan T59
