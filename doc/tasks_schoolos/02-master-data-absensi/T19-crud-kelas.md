# T19 — CRUD kelas

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T18
**Estimasi:** 1 hari

## Tujuan
Admin/TU bisa mengelola kelas dan menugaskan wali kelas dari daftar guru sekolah.

## Langkah
1. Buat `internal/classes/service.go`:
   - `Create`, `Update`, `List`, `AssignHomeroomTeacher`.
   - Saat menugaskan wali kelas, cek bahwa pengguna yang dipilih:
     - berasal dari sekolah yang sama,
     - aktif (`is_active = true`),
     - memiliki peran `guru`.
2. Buat `internal/classes/handler.go`:
   - `GET /kelas?tahun={id}`: daftar kelas untuk tahun ajaran yang dipilih. Default: tahun aktif.
   - `GET /kelas/baru`, `POST /kelas`
   - `GET /kelas/{id}/ubah`, `POST /kelas/{id}`
3. Form kelas memakai `<select>` untuk wali kelas, diisi dari `ListTeachers(schoolID)`.
4. Pembatasan akses: hanya `admin_tu`. `kepala_sekolah` boleh melihat daftar.
5. Setiap perubahan dicatat ke `audit_logs` dengan `action` `class.create`, `class.update`, `class.assign_teacher`.
6. Template: `web/templates/classes/index.html` dan `form.html`.

## File yang disentuh
- `internal/classes/service.go`, `handler.go`
- `web/templates/classes/index.html`, `form.html`

## Kriteria selesai
- [x] Admin/TU bisa membuat kelas dan memilih wali kelas
- [x] Wali kelas yang dipilih tampil di daftar kelas
- [x] Pengguna dengan peran orang tua yang dicoba dijadikan wali kelas ditolak service
- [x] Pengguna nonaktif tidak muncul di pilihan wali kelas
- [x] Guru tidak bisa membuka form ubah kelas (`403`)
- [x] Setiap perubahan tercatat di audit log

## Catatan
- Kelas dan penempatan siswa (T21) adalah dua hal berbeda. Menghapus kelas tidak dibuat di task ini.
