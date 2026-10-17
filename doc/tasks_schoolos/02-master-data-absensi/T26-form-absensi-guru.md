# T26 — Form absensi guru

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T25, T10
**Estimasi:** 1–2 hari

## Tujuan
Guru bisa mengisi absensi kelas yang diampunya dengan cepat. Guru yang tidak berhak tidak bisa membuka atau mengubah absensi kelas tersebut.

## Keputusan yang perlu dikonfirmasi
Di MVP 1 belum ada tabel penugasan guru mata pelajaran. Untuk task ini, hak akses guru dibatasi ke **kelas yang wali kelasnya adalah guru tersebut** (`classes.homeroom_teacher_id`). Jika sekolah membutuhkan guru mapel mengisi absensi, tambahkan tabel `teaching_assignments` sebagai task terpisah sebelum MVP 2.

## Langkah
1. Buat `internal/attendance/access.go`:
   - `CanEditClass(identity, classID) bool`: true jika peran `guru` dan `classes.homeroom_teacher_id = identity.UserID` dan kelas milik `school_id` yang sama.
   - Admin/TU tidak mengisi absensi di task ini.
2. Buat handler:
   - `GET /absensi?kelas={id}&tanggal={yyyy-mm-dd}`: buat atau ambil sesi (`GetOrCreateSession`), tampilkan daftar siswa.
   - `POST /absensi/{sessionID}`: simpan semua status sekaligus dalam satu transaksi.
3. Form berisi satu baris per siswa dengan radio button empat status. Default `hadir`.
4. Tombol "Tandai semua hadir" memakai Alpine.js (set semua radio ke `hadir` di sisi browser), lalu tetap perlu submit untuk menyimpan.
5. Simpan dengan `POST` tunggal, bukan satu request per siswa. Ini penting agar 30 siswa tidak menjadi 30 request.
6. Setelah simpan, tampilkan ringkasan jumlah hadir, sakit, izin, alpa.
7. Template: `web/templates/attendance/form.html`. Tampilan dibuat untuk layar mobile (tombol besar, jarak cukup).

## File yang disentuh
- `internal/attendance/access.go`, `handler.go`, `service.go`
- `web/templates/attendance/form.html`

## Kriteria selesai
- [x] Wali kelas bisa mengisi absensi kelasnya
- [x] Guru lain yang membuka `/absensi?kelas={id}` mendapat `403`
- [x] Admin/TU mendapat `403` pada form absensi
- [x] Mengisi 30 siswa dan menyimpan selesai dalam kurang dari 2 menit (diuji manual oleh guru atau dengan simulasi)
- [x] Simpan sebagian gagal tidak meninggalkan data setengah jadi (transaksi)
- [x] Tanggal di masa depan ditolak
- [x] Form berfungsi di layar 360px

## Catatan
- Keputusan batas tanggal edit (misalnya hanya bisa mengubah absensi dalam 7 hari terakhir) belum ditetapkan. Catat sebagai pertanyaan untuk sekolah.
