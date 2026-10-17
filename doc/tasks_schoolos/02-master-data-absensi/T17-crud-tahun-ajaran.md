# T17 — CRUD tahun ajaran

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T16, T10
**Estimasi:** 1 hari

## Tujuan
Admin/TU bisa membuat, melihat, dan mengubah tahun ajaran dari antarmuka. Peran lain tidak bisa mengakses halaman ini.

## Langkah
1. Buat `internal/school/academic_year_service.go`:
   - `List(ctx, schoolID)`, `Create(ctx, schoolID, input)`, `Update(ctx, schoolID, id, input)`, `Activate(ctx, schoolID, id)`.
   - Validasi input di service: nama tidak kosong (maks 20 karakter), tanggal valid, `end_date > start_date`.
   - Setiap fungsi menerima `schoolID` dari `Identity`, bukan dari form.
2. Buat `internal/school/academic_year_handler.go`:
   - `GET /pengaturan/tahun-ajaran`: daftar tahun ajaran, tanda yang aktif.
   - `GET /pengaturan/tahun-ajaran/baru` dan `POST /pengaturan/tahun-ajaran`.
   - `GET /pengaturan/tahun-ajaran/{id}/ubah` dan `POST /pengaturan/tahun-ajaran/{id}`.
   - `POST /pengaturan/tahun-ajaran/{id}/aktifkan`.
3. Pasang `RequireRole(RoleAdminTU)` (dan `RoleKepalaSekolah` hanya untuk `GET` daftar, jika diperlukan) pada grup route.
4. Setiap perubahan dicatat lewat `audit.Record` (T15) dengan `action` seperti `academic_year.create`.
5. Buat template `web/templates/school/academic_years.html` dan `academic_year_form.html`.
6. Tombol "Aktifkan" memakai konfirmasi sederhana (Alpine.js) sebelum submit, karena mengubah tahun aktif berpengaruh ke seluruh sekolah.

## File yang disentuh
- `internal/school/academic_year_service.go`
- `internal/school/academic_year_handler.go`
- `web/templates/school/academic_years.html`
- `web/templates/school/academic_year_form.html`

## Kriteria selesai
- [ ] Admin/TU bisa membuat, mengubah, dan mengaktifkan tahun ajaran
- [ ] Guru yang membuka `/pengaturan/tahun-ajaran` mendapat `403`
- [ ] Orang tua yang membuka halaman yang sama mendapat `403`
- [ ] ID tahun ajaran milik sekolah lain menghasilkan `404`, bukan data
- [ ] Setiap create, update, dan activate tercatat di `audit_logs`
- [ ] Form tanpa token CSRF ditolak (dari T09)

## Catatan
- Tahun ajaran yang sudah punya data absensi tidak boleh dihapus. Di task ini cukup tidak menyediakan tombol hapus.
