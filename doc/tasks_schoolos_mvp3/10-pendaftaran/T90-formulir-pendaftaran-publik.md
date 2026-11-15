# T90 — Formulir pendaftaran publik

**Fase:** 10 Pendaftaran
**Dependensi:** T89, T85b, T86, T87
**Estimasi:** 3 hari

## Tujuan
Orang tua bisa mendaftarkan anak secara online lewat link sekolah, dan data yang diisi langsung siap untuk Dapodik.

## Keputusan
Orang tua mengisi data lengkap F-PD; bagian non-wajib boleh dilewati (2026-11-14). Pendaftar memilih jalur (T89).

## Langkah
1. Migrasi `applicants`:
   - `id`, `school_id`, `period_id`, `track_id`, `registration_number` (unik per sekolah), `status` (lihat T93), `full_name`, `gender`, `birth_date`, `profile jsonb` (field T86), `guardians jsonb` (field T87), `contact_phone`, `consent_at timestamptz NOT NULL`, `resume_code_hash`, `submitted_at`, timestamps.
   - Data disimpan sebagai jsonb selama masih pendaftar; baru dipecah ke tabel siswa saat diterima (T96).
   - Field sensitif (NIK siswa, No KK, no akta, NIK orang tua) **tidak boleh** masuk jsonb. Simpan di kolom terenkripsi terpisah: `nik_enc`/`nik_idx`, `kk_enc`/`kk_idx`, `birth_cert_enc`, dan `guardian_niks_enc` (T85b).
   - Cek pendaftar ganda di periode yang sama lewat `nik_idx`.
2. Nomor pendaftaran berurutan per periode, mis. `PPDB-2027-0001`, dengan sequence seperti nomor kuitansi (000023).
3. Route publik `GET /s/{kode-sekolah}/daftar` memakai kode sekolah dari fitur login per sekolah. Periode tutup menampilkan pesan, bukan form.
4. Form bertahap: (0) pilih gelombang dan jalur, (1) data calon siswa, (2) alamat, (3) orang tua/wali, (4) sekolah asal, (5) dokumen (T91), (6) ringkasan dan kirim. Field dan validasi memakai ulang T86/T87.
5. Draf: setelah langkah 1, sistem memberi kode lanjut (6 karakter). Lanjutkan dengan nomor HP + kode. Kode disimpan sebagai hash.
6. Persetujuan pengolahan data pribadi wajib dicentang; waktu persetujuan disimpan.
7. Anti-spam: rate limit per IP dan per nomor HP, field honeypot, batas draf per nomor HP.
8. Halaman publik tidak pernah menampilkan data pendaftar lain dan tidak menyebut apakah nomor HP sudah terdaftar.

## File yang disentuh
- `db/migrations/0000XX_applicants.up.sql` dan `.down.sql`
- `internal/admission/applicant.go`, `applicant_test.go`, `public_handler.go`
- `web/templates/admission/public_form*.html`

## Kriteria selesai
- [x] Pendaftaran di periode tutup ditolak di server, bukan hanya disembunyikan di UI
- [x] Nomor pendaftaran tidak ganda saat 50 kiriman bersamaan
- [x] Draf bisa dilanjutkan dengan HP + kode; kode salah 5 kali mengunci sementara
- [x] Kode sekolah salah menampilkan 404 tanpa membocorkan sekolah lain
- [x] Tanpa persetujuan data pribadi, form tidak bisa dikirim
- [x] Kolom `profile` dan `guardians` (jsonb) tidak memuat NIK, No KK, atau no akta

## Catatan implementasi
Nomor pendaftaran `SPMB-{tahun mulai TA}-{urut 4 digit}` per sekolah per tahun ajaran (tabel `admission_counters`). Rate limit disimpan di tabel `admission_attempts`.
