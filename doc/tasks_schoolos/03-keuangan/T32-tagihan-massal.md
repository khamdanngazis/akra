# T32 — Tagihan massal

**Fase:** 03 Keuangan
**Dependensi:** T30, T31, T33
**Estimasi:** 1–2 hari

## Tujuan
Bendahara bisa membuat tagihan untuk satu kelas atau semua kelas dalam satu aksi. Pembuatan berulang untuk periode yang sama tidak menghasilkan tagihan ganda.

## Langkah
1. Buat `internal/billing/bulk_service.go`:
   - `PreviewBulk(ctx, schoolID, input)`: hitung berapa tagihan akan dibuat, berapa yang dilewati (sudah ada), dan total nominalnya. Tidak menyimpan apa pun.
   - `RunBulk(ctx, schoolID, input, userID)`: membuat tagihan.
   - Input: jenis tagihan (hanya yang aktif), periode `YYYY-MM`, kelas (satu atau semua), jatuh tempo.
2. Alur:
   - Ambil enrollment aktif untuk kelas yang dipilih di tahun ajaran aktif.
   - Untuk setiap enrollment, cek apakah item dengan `fee_type_id` dan `period` yang sama sudah ada. Jika ada, lewati.
   - Buat tagihan dengan `CreateInvoice` (T30).
3. Jika jumlah enrollment lebih dari 50, jalankan sebagai job River (T33) dan simpan `bulk_job_id`. Halaman menampilkan progres.
4. Jika 50 atau kurang, jalankan langsung dalam satu transaksi.
5. Halaman dua langkah: "Pratinjau" lalu "Buat tagihan". Tombol kedua hanya aktif setelah pratinjau.
6. Audit: satu baris `billing.bulk_create` dengan ringkasan jumlah, bukan satu baris per tagihan.

## File yang disentuh
- `internal/billing/bulk_service.go`, `bulk_handler.go`
- `internal/billing/jobs/bulk_invoice_job.go`
- `web/templates/billing/bulk_form.html`, `bulk_preview.html`

## Kriteria selesai
- [x] Pratinjau menampilkan jumlah tagihan yang akan dibuat dan yang dilewati, tanpa mengubah database
- [x] Menjalankan dua kali untuk periode dan jenis yang sama tidak menghasilkan tagihan ganda
- [x] Jenis tagihan nonaktif tidak bisa dipilih
- [x] Kelas dari sekolah lain tidak bisa dipilih (`403` atau `404`)
- [x] Tagihan untuk 200 siswa selesai tanpa membuat halaman macet (job River)
- [x] Kegagalan di tengah job tidak meninggalkan setengah tagihan: job diulang dan hasilnya tetap idempoten

## Catatan
- Siswa yang enrollment-nya baru ditambahkan setelah pratinjau tidak ikut. Pratinjau dan eksekusi bisa berbeda; tampilkan jumlah saat eksekusi di hasil.
