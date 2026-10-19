# T73 — Konfigurasi format rapor **[wawancara]**

**Fase:** 07 Nilai
**Dependensi:** T71
**Estimasi:** 1 hari

## Tujuan
Sekolah bisa mengatur hal-hal dasar rapor tanpa perubahan kode: skala huruf, KKM per mapel, dan nama kepala sekolah serta wali kelas yang tampil.

## Syarat mulai
Format rapor yang dipakai sekolah pilot sudah didapat dari wawancara. Jika formatnya Kurikulum Merdeka, task ini tidak cukup dan harus diputuskan terpisah (di luar MVP).

## Langkah
1. Simpan pengaturan di `school_settings` dengan kunci:
   - `report.scale`: JSON batas huruf, misalnya `[{"min":90,"letter":"A"}, ...]`
   - `report.kkm_default`: angka
   - `report.principal_name`, `report.signer_title`
2. Buat `internal/grades/report_config.go`:
   - Baca dan validasi JSON skala: batas tidak boleh tumpang tindih, harus mencakup 0 sampai 100.
3. Halaman `GET /pengaturan/rapor` dan `POST /pengaturan/rapor`. Akses: `admin_tu`.
4. KKM per mapel bisa diisi di halaman mapel (T64), default dari `report.kkm_default`.
5. Audit: `report_config.update`.

## File yang disentuh
- `internal/grades/report_config.go`, `report_config_test.go`
- `internal/platform/settings/settings.go` (tambahan kunci)
- `web/templates/settings/report.html`

## Kriteria selesai
- [ ] Skala dengan celah atau tumpang tindih ditolak
- [ ] Skala yang tidak mencakup 0–100 ditolak
- [ ] Perubahan skala memengaruhi huruf di rekap T72 tanpa deploy ulang
- [ ] Guru mendapat `403`

## Catatan
- Pengaturan ini sengaja terbatas. Jika sekolah butuh format yang berbeda, itu keputusan baru, bukan konfigurasi.
