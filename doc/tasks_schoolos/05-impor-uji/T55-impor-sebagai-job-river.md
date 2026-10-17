# T55 — Impor sebagai job River

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T54, T33
**Estimasi:** 1–2 hari

## Tujuan
Setelah validasi, baris yang valid disimpan ke database. Impor besar dijalankan sebagai job, dan halaman menampilkan progres.

## Langkah
1. Buat migrasi `0000XX_import_batches`:
   - `import_batches`: `id uuid PK`, `school_id`, `uploaded_by`, `file_key text`, `status text` (`diunggah`, `divalidasi`, `berjalan`, `selesai`, `gagal`), `total_rows int`, `valid_rows int`, `error_rows int`, `processed_rows int`, `created_at`, `finished_at`.
2. Alur di handler:
   - Unggahan selesai → validasi (T54) → status `divalidasi`.
   - Pengguna menekan "Simpan". Jika jumlah valid di atas 50, buat job River `ImportStudentsJob`. Jika 50 atau kurang, jalankan langsung.
3. Job `ImportStudentsJob`:
   - Proses dalam batch 100 baris per transaksi.
   - Setiap batch: upsert `students` berdasarkan (`school_id`, `nis`), buat `guardians` dan `student_guardians` jika data wali ada, buat `enrollments` untuk kelas di tahun aktif.
   - Perbarui `processed_rows` setelah setiap batch.
   - Jika gagal di tengah, job bisa diulang. Batch yang sudah selesai tidak diproses dua kali (lihat T57).
4. Halaman `GET /impor/batch/{id}` menampilkan progres. Halaman di-refresh setiap 3 detik dengan Alpine.js, atau dengan `meta refresh` sederhana jika Alpine tidak dipakai.
5. Audit: `import.run` dengan ringkasan jumlah.

## File yang disentuh
- `db/migrations/0000XX_import_batches.up.sql` dan `.down.sql`
- `internal/importer/service.go`, `handler.go`
- `internal/platform/jobs/import_students_job.go`
- `web/templates/importer/batch.html`

## Kriteria selesai
- [ ] Impor 200 baris selesai dan jumlah siswa baru sama dengan jumlah baris valid
- [ ] Halaman batch menampilkan progres yang bertambah
- [ ] Halaman tidak macet selama impor berlangsung (dicek dengan membuka halaman di tengah proses)
- [ ] Impor 50 baris atau kurang selesai langsung tanpa job
- [ ] Setelah impor, `audit_logs` mencatat satu baris `import.run`

## Catatan
- Batch 100 baris adalah titik awal. Ukuran bisa disesuaikan setelah uji beban (T61).
