# T75 — Nilai di portal orang tua

**Fase:** 07 Nilai
**Dependensi:** T72, T44, T74
**Estimasi:** 1 hari

## Tujuan
Orang tua melihat nilai anaknya per mapel dan bisa mengunduh rapor jika sudah lengkap.

## Langkah
1. Tambah halaman `GET /portal/siswa/{id}/nilai`. Pemeriksaan kepemilikan memakai T44.
2. Data: nilai akhir, huruf, KKM, dan status "belum lengkap" per mapel. Komponen per komponen ditampilkan jika sekolah mengaktifkannya di pengaturan.
3. Tautan unduh rapor hanya tampil jika semua mapel lengkap (T74).
4. Nilai yang belum lengkap tidak boleh tampil sebagai nilai akhir.

## File yang disentuh
- `internal/portal/grade_handler.go`, `grade_service.go`
- `web/templates/portal/grades.html`

## Kriteria selesai
- [ ] Nilai di portal sama dengan rekap T72 untuk siswa yang sama
- [ ] Nilai belum lengkap tidak tampil sebagai angka akhir
- [ ] Tautan rapor tidak muncul jika ada mapel kosong
- [ ] Anak orang lain mendapat `404`

## Catatan
- Tidak ada fitur komentar guru di portal pada task ini.
