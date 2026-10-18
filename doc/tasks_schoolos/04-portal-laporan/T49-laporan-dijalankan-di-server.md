# T49 — Laporan dijalankan di server

**Fase:** 04 Portal dan Laporan
**Dependensi:** T47, T33
**Estimasi:** 1 hari

## Tujuan
Ekspor besar tidak membuat halaman menunggu lama. Ekspor di atas 5.000 baris dijalankan sebagai job, dan hasilnya bisa diunduh setelah selesai.

## Langkah
1. Buat tabel `report_jobs`:
   - `id uuid PK`, `school_id uuid`, `requested_by uuid`, `report_type text`, `params jsonb`, `status text` (`antrean`, `berjalan`, `selesai`, `gagal`), `file_key text NULL`, `error text NULL`, `created_at`, `finished_at`.
   - Migrasi `0000XX_report_jobs`.
2. Di handler ekspor:
   - Hitung jumlah baris dulu (`COUNT` dengan filter yang sama).
   - Jika di bawah 5.000, buat file langsung seperti T47 dan T48.
   - Jika 5.000 atau lebih, buat baris `report_jobs` dengan status `antrean`, masukkan job River, dan tampilkan halaman status.
3. Job River `ReportExportJob`:
   - Buat file, unggah ke R2 dengan kunci `schools/{school_id}/reports/{job_id}.xlsx`.
   - Ubah status menjadi `selesai` dan isi `file_key`.
   - Jika gagal, status `gagal` dan `error` ditulis dengan pesan yang aman.
4. Halaman `GET /laporan/jobs/{id}`: tampilkan status. Saat selesai, tombol unduh memakai presigned URL 5 menit (sama dengan T37).
5. Hanya pemilik job (`requested_by`) atau kepala sekolah yang bisa melihat dan mengunduh hasilnya.
6. Hapus file hasil ekspor setelah 7 hari dengan job terjadwal (cukup satu job periodik).

## File yang disentuh
- `db/migrations/0000XX_report_jobs.up.sql` dan `.down.sql`
- `internal/reports/job.go`, `job_handler.go`
- `internal/platform/jobs/report_export_job.go`

## Kriteria selesai
- [x] Ekspor di bawah 5.000 baris tetap langsung
- [x] Ekspor 5.000 baris atau lebih membuat job dan halaman merespons dalam 2 detik
- [x] Job selesai menghasilkan file yang bisa diunduh
- [x] Pengguna lain tidak bisa mengunduh hasil job orang lain (`404`)
- [x] Job yang gagal menampilkan status gagal tanpa detail internal
- [x] Job terjadwal menghapus file yang lebih dari 7 hari

## Catatan
- Ambang 5.000 baris dicoba dulu di staging. Jika ternyata lambat di bawah itu, turunkan.
