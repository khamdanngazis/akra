# T43 — Portal: daftar anak

**Fase:** 04 Portal dan Laporan
**Dependensi:** T23, T24
**Estimasi:** 3 jam

## Tujuan
Orang tua yang login melihat daftar anak yang terhubung dengannya, dan tidak melihat siswa lain.

## Langkah
1. Buat `internal/portal/handler.go` dengan prefix route `/portal`.
2. Pasang `RequireRole(RoleOrangTua)` pada grup `/portal`.
3. Buat `internal/portal/service.go`:
   - `ListChildren(ctx, guardianID)`: ambil siswa lewat `student_guardians`, lalu join ke `students` dan enrollment aktif tahun ajaran aktif.
   - `guardianID` diambil dari identitas (`users.guardian_id`), bukan dari parameter.
   - Filter `school_id` dari identitas.
4. Handler `GET /portal`:
   - Jika hanya satu anak, langsung tampilkan ringkasan anak tersebut.
   - Jika lebih dari satu, tampilkan daftar kartu anak dengan tautan ke detail.
5. Template `web/templates/portal/children.html`. Tampilkan nama, NIS, dan kelas.
6. Jika tidak ada anak terhubung, tampilkan pesan "Belum ada data anak yang terhubung. Hubungi sekolah."

## File yang disentuh
- `internal/portal/handler.go`, `service.go`
- `web/templates/portal/children.html`

## Kriteria selesai
- [ ] Orang tua dengan dua anak melihat dua kartu
- [ ] Orang tua tanpa anak terhubung melihat pesan kosong, bukan error
- [ ] Anak dari keluarga lain tidak muncul, walaupun berada di sekolah yang sama
- [ ] Peran admin atau bendahara yang membuka `/portal` mendapat `403`
- [ ] Hanya enrollment tahun ajaran aktif yang ditampilkan kelasnya

## Catatan
- Satu anak bisa punya dua wali (T23). Keduanya melihat anak yang sama, dan itu sesuai rancangan.
