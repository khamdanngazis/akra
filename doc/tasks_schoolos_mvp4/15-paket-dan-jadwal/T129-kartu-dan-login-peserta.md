# T129 — Kartu peserta dan login peserta

**Fase:** 15 Paket dan Jadwal
**Dependensi:** T128
**Estimasi:** 2 hari
**[keputusan]** Cara login siswa (lihat indeks)

## Tujuan
Peserta bisa masuk ke ujian dengan mudah dari HP atau komputer lab, tanpa membuka akses ke data lain.

## Langkah (mengikuti saran keputusan (a): kartu peserta per ujian)
1. Halaman login peserta `GET/POST /u/{kode-sekolah}`, terpisah dari login staf dan orang tua:
   - **Siswa:** NIS + kode akses.
   - **Calon siswa:** nomor pendaftaran + kode akses.
2. **Kode akses:** 8 karakter tanpa huruf mirip (0/O, 1/l), dibuat per peserta per jadwal. Disimpan sebagai hash (argon2id, seperti password). Hanya tampil di kartu peserta.
3. **Kartu peserta** PDF per sesi (8 kartu per A4, siap potong): nama, kelas/nomor pendaftaran, sesi, ruang, waktu, kode akses, QR menuju halaman login.
   - Calon siswa: kode juga tampil di **kartu tes** halaman status pendaftaran (T119), setelah sesi dibuat.
4. Session peserta:
   - cookie terpisah dengan masa berlaku sampai `ends_at` sesi + 30 menit;
   - hanya bisa membuka halaman ujian miliknya;
   - tidak bisa membuka halaman staf.
5. Rate limit login per IP dan per peserta. Setelah 5 kali gagal, peserta harus minta pengawas mereset (T134).
6. Pengawas bisa membuat ulang kode akses peserta yang kartunya hilang.

## File yang disentuh
- `internal/exam/participant_auth.go`, `participant_auth_test.go`, `card_pdf.go`
- `web/templates/exam/participant_login.html`
- `internal/admission` (kode di kartu tes)
- `cmd/server/routes.go`, `internal/testutil/routes.go`

## Kriteria selesai
- [ ] Peserta tidak bisa membuka ujian peserta lain atau sesi lain (uji akses)
- [ ] Kode akses tidak tersimpan dalam teks biasa dan tidak muncul di log
- [ ] Kode tidak berlaku di luar jendela sesi
- [ ] Session peserta ditolak di semua route staf (uji: daftar route staf × session peserta → 403/redirect)
- [ ] Kartu peserta bisa dipindai QR dan login dari HP
