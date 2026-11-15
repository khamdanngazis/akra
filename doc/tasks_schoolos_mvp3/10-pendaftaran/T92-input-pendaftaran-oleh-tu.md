# T92 — Input pendaftaran oleh TU

**Fase:** 10 Pendaftaran
**Dependensi:** T90, T91
**Estimasi:** 0,5 hari

## Tujuan
TU bisa memasukkan pendaftar yang datang langsung ke sekolah dengan form yang sama.

## Langkah
1. Route `GET/POST /pendaftaran/baru`. Akses: `admin_tu`.
2. Memakai komponen form T90 (satu halaman, tanpa kode lanjut). Persetujuan data pribadi dicatat dengan keterangan "diisi oleh TU" dan nama petugas.
3. TU boleh mendaftarkan di periode yang sudah tutup (pendaftar susulan), dengan peringatan.

## File yang disentuh
- `internal/admission/admin_handler.go`
- `web/templates/admission/admin_form.html`

## Kriteria selesai
- [x] Pendaftar dari TU mendapat nomor pendaftaran dari sequence yang sama dengan pendaftar online
- [x] Audit mencatat petugas yang menginput
