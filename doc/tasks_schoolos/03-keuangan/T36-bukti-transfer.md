# T36 — Bukti transfer

**Fase:** 03 Keuangan
**Dependensi:** T34, T12
**Estimasi:** 1 hari

## Tujuan
Bukti transfer (foto atau PDF) bisa dilampirkan ke pembayaran, disimpan di R2, dan tidak bisa dipakai dua kali.

## Langkah
1. Buat migrasi `000017_payment_proofs`:
   - `id uuid PK`, `payment_id uuid NOT NULL REFERENCES payments(id)`, `storage_key text NOT NULL UNIQUE`, `sha256 bytea NOT NULL`, `size_bytes integer NOT NULL`, `content_type text NOT NULL`, `uploaded_by uuid NOT NULL`, `uploaded_at timestamptz NOT NULL DEFAULT now()`.
   - `UNIQUE (sha256)` agar file yang sama tidak dipakai di dua pembayaran. Ini ketat, jadi cek juga apakah sekolah ingin mengizinkan satu bukti untuk beberapa pembayaran sebelum finalisasi.
   - Constraint `CHECK (size_bytes > 0 AND size_bytes <= 2097152)` (2 MB).
2. Buat `internal/platform/storage/r2.go`:
   - Client dari AWS SDK for Go v2 dengan endpoint R2.
   - Fungsi `Put(ctx, key, reader, contentType)` dan `Presign(ctx, key, ttl)`.
   - Konfigurasi dari environment: `R2_ACCOUNT_ID`, `R2_BUCKET`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`.
3. Handler upload:
   - Terima `multipart/form-data`, baca maksimal 2 MB (`http.MaxBytesReader`).
   - Deteksi tipe dari isi file (`http.DetectContentType`), bukan dari ekstensi. Izinkan `image/jpeg`, `image/png`, `application/pdf`.
   - Hitung SHA-256 sambil membaca.
   - Jika hash sudah ada di `payment_proofs`, tolak dengan pesan "bukti ini sudah dipakai".
   - Kunci R2: `schools/{school_id}/proofs/{uuid}.{ext}`.
4. Kompres gambar besar sebelum disimpan (opsional di task ini, bisa ditunda ke catatan).
5. Audit: `payment.proof_upload`.

## File yang disentuh
- `db/migrations/000017_payment_proofs.up.sql` dan `.down.sql`
- `internal/platform/storage/r2.go`
- `internal/payments/proof_handler.go`, `proof_service.go`

## Kriteria selesai
- [x] File 3 MB ditolak sebelum disimpan ke R2
- [x] File `.exe` yang di-rename menjadi `.jpg` ditolak (deteksi tipe dari isi)
- [x] Unggahan file yang sama dua kali kedua ditolak
- [x] File tersimpan di R2 dengan kunci sesuai pola di atas
- [x] Objek di R2 tidak bisa diakses lewat URL publik

## Catatan
- Layanan R2 perlu bucket privat. Pastikan public access dimatikan di dashboard.
- Keputusan apakah satu bukti boleh dipakai untuk beberapa pembayaran belum ditetapkan. Default di task ini: tidak boleh.
