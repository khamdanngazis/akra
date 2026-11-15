# T91 — Unggah dokumen pendaftaran

**Fase:** 10 Pendaftaran
**Dependensi:** T90
**Estimasi:** 1,5 hari

## Tujuan
Pendaftar bisa mengunggah dokumen persyaratan, dan hanya TU yang bisa melihatnya.

## Langkah
1. Migrasi `applicant_documents`: `id`, `school_id`, `applicant_id`, `doc_type` (`kk`, `akta`, `ijazah`, `foto`, `kip`, `lainnya`), `storage_key`, `content_type`, `size_bytes`, `sha256`, `uploaded_at`.
2. Memakai ulang `internal/platform/storage` dan pola dari bukti bayar (`payments/proof_service.go`): cek tipe (JPG, PNG, PDF), batas ukuran (2 MB), hash SHA-256, hapus objek jika simpan ke DB gagal.
3. Satu dokumen per jenis; unggah ulang menggantikan yang lama (objek lama dihapus).
4. Lihat dokumen lewat URL presign berumur pendek, hanya untuk `admin_tu` dan `kepala_sekolah`.
5. Pendaftar hanya bisa mengunggah atau mengganti selama status `draf` atau `perlu_perbaikan`.

## File yang disentuh
- `db/migrations/0000XX_applicant_documents.up.sql` dan `.down.sql`
- `internal/admission/document.go`, `document_test.go`

## Kriteria selesai
- [x] File .exe yang diganti nama menjadi .pdf ditolak (cek isi, bukan ekstensi)
- [x] URL dokumen tidak bisa ditebak dan kedaluwarsa
- [x] `guru` dan `orang_tua` mendapat 403 saat membuka dokumen pendaftar

## Catatan implementasi
Isi file dienkripsi (AES-GCM, kunci PII) sebelum masuk storage; dibuka staf lewat handler yang mendekripsi dan mencatat audit, bukan URL presign. `storage.Store` mendapat metode `Get`.
