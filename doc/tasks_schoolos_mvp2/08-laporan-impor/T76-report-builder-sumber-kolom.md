# T76 — Report builder: sumber dan kolom yang diizinkan

**Fase:** 08 Laporan dan Impor
**Dependensi:** T47
**Estimasi:** 2 hari

## Tujuan
Laporan yang bisa disesuaikan dibangun dari daftar sumber dan kolom yang sudah ditetapkan di kode. Pengguna memilih dari daftar itu, tidak pernah menulis SQL. Ini mitigasi R7.

## Langkah
1. Buat `internal/reports/builder/registry.go`:
   - Struct `Source` berisi nama sumber, query dasar yang sudah ditulis, dan daftar `Column`.
   - `Column` berisi kunci, label, tipe (`text`, `int`, `money`, `date`), dan ekspresi SQL tetap.
   - Sumber awal: `attendance` (T27), `arrears` (T42), `payments` (T34), `grades` (T72).
2. Filter dan urutan hanya boleh memakai kunci kolom yang terdaftar. Operator dibatasi: `=`, `!=`, `>=`, `<=`, `contains`, `in`.
3. Builder merakit SQL dari potongan tetap. Nilai filter selalu diteruskan sebagai parameter, tidak pernah disambungkan ke string.
4. Kolom yang memuat data sekolah selalu ditambah filter `school_id` dari identitas.
5. Batas: maksimal 15 kolom, maksimal 5 filter, baris maksimal 5.000 (di atasnya dipakai job T49).

## File yang disentuh
- `internal/reports/builder/registry.go`, `sources.go`
- `internal/reports/builder/compile.go`, `compile_test.go`

## Kriteria selesai
- [ ] Kunci kolom yang tidak terdaftar ditolak
- [ ] Operator di luar daftar ditolak
- [ ] Nilai filter berisi `'; DROP TABLE` diperlakukan sebagai teks biasa (diuji)
- [ ] Setiap query yang dihasilkan memuat `school_id = $1`
- [ ] Uji unit menghasilkan SQL yang sama untuk konfigurasi yang sama

## Catatan
- Semua query dibuat lewat builder. Tidak ada jalur lain untuk laporan kustom.
