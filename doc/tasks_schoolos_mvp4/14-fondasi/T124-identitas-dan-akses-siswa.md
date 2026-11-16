# T124 — Identitas dan akses siswa

**Fase:** 14 Fondasi
**Dependensi:** T123
**Estimasi:** 3 hari
**[keputusan]** Model gabungan (lihat indeks), menunggu konfirmasi

## Tujuan
Siswa dan calon siswa bisa masuk ke ujian dengan cara yang paling sedikit masalahnya di hari ujian. Desain datanya sudah siap untuk portal siswa ke depan.

## Desain
1. **Peran baru `siswa`**:
   - session terikat ke `student_id` dan `school_id`;
   - hanya bisa membuka route `/u/...`; semua route staf menolak.
   - Calon siswa memakai jenis identitas `pendaftar` yang terikat ke `applicant_id`, dengan aturan yang sama.
2. Migrasi `student_credentials`:
   - `id`, `school_id`, `student_id NULL`, `applicant_id NULL` (tepat satu terisi);
   - `kind` (`kode_ujian`, `password`);
   - `secret_hash` (argon2id);
   - `valid_from`, `valid_until`, `exam_session_id NULL` (untuk `kode_ujian`);
   - `failed_count`, `locked_until`, `created_by`, `revoked_at`.
   - MVP 4 hanya membuat `kode_ujian`. `password` disiapkan untuk portal siswa.
3. **Kode ujian:**
   - 8 karakter dari alfabet tanpa huruf mirip (`ABCDEFGHJKMNPQRSTUVWXYZ23456789`);
   - dibuat per peserta per sesi;
   - **berlaku untuk mengerjakan** dari 30 menit sebelum sesi sampai sesi berakhir;
   - setelah itu **tetap bisa dipakai untuk melihat hasil** (hanya baca) sampai 30 hari setelah hasil dibuka (T151), supaya siswa bisa membaca pembahasan tanpa akun permanen;
   - tampil di kartu peserta (T135) dan kartu tes pendaftaran (T149).
4. **Login peserta** `GET/POST /u/{kode-sekolah}`:
   - Satu kolom "NIS atau nomor pendaftaran" dan satu kolom "kode di kartu". Tipe input membedakan siswa dan pendaftar.
   - Bisa dari **QR di kartu**: QR berisi URL login dengan NIS terisi, sehingga peserta cukup mengetik kode.
   - Pesan error ramah dan spesifik:
     - "Kode belum berlaku, sesi mulai pukul 08.00";
     - "Kode sudah dipakai di perangkat lain, minta pengawas".
5. **Keamanan:**
   - rate limit per IP dan per peserta memakai limiter database yang ada (`admission.Limiter` dipindah ke paket platform agar dipakai bersama);
   - 5 kali gagal → peserta terkunci sampai pengawas membuka;
   - kode tidak tercatat di log.
6. Session peserta memakai cookie terpisah dari staf (nama dan path berbeda), sehingga staf yang meminjamkan laptop tidak tertukar session-nya. Masa berlaku sampai akhir sesi + 30 menit.
7. Layar beranda siswa `/u/beranda` yang minimal: ujian yang sedang atau akan dibuka untuknya, beserta status. Halaman ini menjadi titik awal portal siswa nanti.

## File yang disentuh
- `db/migrations/0000XX_student_credentials.up.sql` dan `.down.sql`
- `internal/auth` (peran `siswa`, identitas peserta), `internal/exam/participant_auth.go`, tes
- `internal/platform/ratelimit/` (pindahan limiter)
- `web/templates/exam/participant_login.html`, `participant_home.html`
- `cmd/server/routes.go`, `internal/testutil/routes.go`

## Kriteria selesai
- [ ] Session siswa/pendaftar ditolak di **semua** route staf (uji otomatis dari registry route)
- [ ] Kode di luar jendela berlaku ditolak dengan pesan yang menyebut jam mulai
- [ ] Setelah sesi, kode hanya membuka hasil dalam mode baca, tidak bisa memulai atau mengubah attempt
- [ ] Kode tersimpan sebagai hash, tidak muncul di log (uji pemindai log)
- [ ] Penguncian setelah 5 gagal dan buka kunci oleh pengawas berfungsi
- [ ] Login lewat QR di HP: 2 langkah (pindai, ketik kode)
