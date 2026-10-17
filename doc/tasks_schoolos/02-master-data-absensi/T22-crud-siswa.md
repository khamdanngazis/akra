# T22 — CRUD siswa

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T21
**Estimasi:** 1–2 hari

## Tujuan
Admin/TU bisa mencari, menambah, dan mengubah data siswa, termasuk penempatan kelas. Pencarian tetap cepat untuk ratusan data.

## Langkah
1. Buat `internal/students/service.go`:
   - `List(ctx, schoolID, filter)` dengan filter nama, kelas, tahun ajaran, dan status. Hasil dipaginasi 25 per halaman.
   - `Create(ctx, schoolID, input)`: buat siswa dan enrollment awal dalam satu transaksi.
   - `Update(ctx, schoolID, id, input)`: ubah data pribadi. Perpindahan kelas lewat `Place` (T21), bukan lewat form ubah data.
2. Buat `internal/students/handler.go`:
   - `GET /siswa?q=&kelas=&tahun=&page=`
   - `GET /siswa/baru`, `POST /siswa`
   - `GET /siswa/{id}`, `GET /siswa/{id}/ubah`, `POST /siswa/{id}`
   - `POST /siswa/{id}/pindah-kelas`
3. Pencarian nama memakai `ILIKE` dengan index dari T20. Jika lambat, pertimbangkan `pg_trgm` sebagai langkah berikutnya, bukan sekarang.
4. Form tambah memakai `<datalist>` untuk kelas, diisi dari daftar kelas tahun aktif.
5. Batasi akses: `admin_tu` penuh. `kepala_sekolah` hanya baca. Peran lain `403`.
6. Audit untuk `student.create`, `student.update`, `student.place`.
7. Template: `web/templates/students/index.html`, `form.html`, `detail.html`.

## File yang disentuh
- `internal/students/service.go`, `handler.go`
- `web/templates/students/index.html`, `form.html`, `detail.html`

## Kriteria selesai
- [ ] Pencarian dengan 500 siswa dummy selesai di bawah 1 detik (diukur di data uji)
- [ ] Hasil pencarian berhalaman 25 per halaman
- [ ] Membuat siswa tanpa kelas ditolak
- [ ] Pindah kelas tercatat sebagai enrollment baru, bukan edit kolom
- [ ] Guru yang membuka `/siswa` mendapat `403` (guru hanya lewat absensi)
- [ ] ID siswa sekolah lain menghasilkan `404`

## Catatan
- Guru dan orang tua tidak punya akses ke daftar siswa di task ini. Akses guru ke data siswa kelasnya ditangani di T26, dan akses orang tua di T43.
