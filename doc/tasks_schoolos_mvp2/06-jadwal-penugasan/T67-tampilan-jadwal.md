# T67 — Tampilan jadwal per kelas, guru, dan portal

**Fase:** 06 Jadwal dan Penugasan
**Dependensi:** T66, T44
**Estimasi:** 1 hari

## Tujuan
Jadwal bisa dilihat dalam bentuk tabel mingguan oleh guru, admin, dan orang tua anak di kelas tersebut.

## Langkah
1. Buat `internal/schedule/view.go`:
   - `ClassWeek(ctx, schoolID, classID)` mengembalikan slot dikelompokkan per hari, diurutkan per jam.
   - `TeacherWeek(ctx, schoolID, teacherID)` untuk jadwal mengajar guru.
2. Route:
   - `GET /jadwal/saya` untuk guru: jadwal mengajarnya.
   - `GET /jadwal?kelas=` untuk admin dan kepala sekolah (baca).
   - `GET /portal/siswa/{id}/jadwal` untuk orang tua. Pemeriksaan kepemilikan memakai T44.
3. Tampilan mobile: satu kolom per hari, dengan tab hari di atas (Alpine.js).
4. Jam ditampilkan dalam zona Asia/Jakarta.

## File yang disentuh
- `internal/schedule/view.go`, `view_test.go`
- `internal/schedule/handler.go` (tambahan route)
- `internal/portal/schedule_handler.go`
- `web/templates/schedule/week.html`

## Kriteria selesai
- [ ] Jadwal kelas di layar sama dengan data slot untuk uji contoh
- [ ] Guru hanya melihat jadwal mengajarnya sendiri
- [ ] Orang tua mengakses jadwal anaknya, dan `404` untuk anak lain
- [ ] Layout tab hari berfungsi di layar 360px
- [ ] Uji otorisasi T58 dan T59 diperbarui

## Catatan
- Pencetakan jadwal ke PDF tidak termasuk task ini.
