# T93 — Daftar dan verifikasi pendaftar **[keputusan]**

**Fase:** 10 Pendaftaran
**Dependensi:** T90, T91
**Estimasi:** 2 hari

## Tujuan
TU bisa memeriksa pendaftar, meminta perbaikan, dan menetapkan hasilnya dengan jejak yang jelas.

## Langkah
1. Status dan perpindahan yang diizinkan (fungsi murni, diuji unit):
   - `draf` → `dikirim` (oleh pendaftar)
   - `dikirim` → `perlu_perbaikan` | `diverifikasi`
   - `perlu_perbaikan` → `dikirim`
   - `diverifikasi` → `diterima` | `cadangan` | `ditolak`
   - `cadangan` → `diterima` | `ditolak`
   - semua kecuali `diterima` yang sudah jadi siswa → `mundur`
2. Migrasi `applicant_events`: `applicant_id`, `from_status`, `to_status`, `note`, `actor_id NULL` (NULL = pendaftar), `created_at`.
3. Halaman `GET /pendaftaran`: filter periode, status, cari nama/nomor. Kolom: nomor, nama, asal sekolah, status, kelengkapan dokumen, status bayar (T94).
4. Halaman detail: data, dokumen, checklist dokumen, riwayat status, tombol aksi sesuai status. `perlu_perbaikan` wajib dengan catatan.
5. Kuota (T89): `diterima` melebihi kuota diberi peringatan, tidak diblok.
6. Masa simpan data pendaftar `ditolak`/`mundur` mengikuti keputusan sekolah; job pembersihan menjadi catatan untuk task terpisah.

## File yang disentuh
- `db/migrations/0000XX_applicant_events.up.sql` dan `.down.sql`
- `internal/admission/status.go`, `status_test.go`, `admin_handler.go`
- `web/templates/admission/list.html`, `detail.html`

## Kriteria selesai
- [ ] Perpindahan status yang tidak diizinkan ditolak di service
- [ ] Setiap perubahan status tercatat di `applicant_events` dan audit
- [ ] Dua petugas yang mengubah status bersamaan tidak menghasilkan status ganda (cek versi atau `SELECT ... FOR UPDATE`)
- [ ] Route baru masuk daftar uji T58 dan T59
