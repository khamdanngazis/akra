# T169 — Uji kombinasi modul dan dokumentasi

**Fase:** 22 Modul per Sekolah
**Dependensi:** T163–T168
**Estimasi:** 1,5 hari

## Latar belakang
Dengan 9 modul opsional, kesalahan biasanya muncul di sambungan antarmodul, bukan di modul itu sendiri. Contohnya tautan ke modul yang mati atau query modul mati di halaman lain.

## Langkah
1. **Uji kombinasi** di `cmd/server` (memakai registry route dan klien uji):
   - sekolah **inti saja**;
   - setiap **paket templat**;
   - **semua kecuali X** untuk setiap modul X.

   Untuk setiap kombinasi:
   - semua route GET yang boleh dibuka peran memberi 200 atau 404 "modul tidak aktif", tanpa 500;
   - tidak ada tautan di halaman yang menuju route modul mati (pemindai tautan);
   - menu dan beranda setiap peran sesuai.
2. **Harness visual** (T162): opsi `-modules` untuk memotret menu dan beranda paket Dasar dan Keuangan.
3. **Dokumentasi:**
   - `doc/panduan-ujian/README.md` dan panduan lain menyebut modul;
   - runbook: cara menangani "modul tidak aktif" dan permintaan aktivasi;
   - ADR 0002 menjadi "diterima" dengan keputusan final.

## Catatan implementasi
- **`TestModuleCombinations`** (`cmd/server/combo_test.go`): 14 kombinasi (inti saja, 4 paket, semua kecuali X untuk 9 modul).
  - Di setiap kombinasi, semua route GET di registry dibuka oleh setiap peran yang berhak, dengan ID data sekolah sendiri; halaman publik pendaftaran (`/s/{kode}/...`) dibuka tanpa login. Sekitar 480 request per kombinasi.
  - Syaratnya: tidak ada 5xx, dan halaman HTML tidak memuat `href`/`action`/`src` ke route yang butuh modul mati (sekitar 63 ribu tautan diperiksa).
  - Menu dan beranda setiap peran termasuk di dalamnya, karena keduanya route GET.
  - Waktu uji sekitar 65 detik.
- **Dikecualikan dari uji kombinasi:**
  - halaman publik yang tidak terikat sekolah (`/u/`, cek perangkat, panduan PDF);
  - dokumen pendaftar (`/pendaftaran/{id}/dokumen/{docID}`), karena fixture hanya mencatat kunci berkas tanpa mengunggah berkas terenkripsinya ke storage uji (500 di lingkungan uji, bukan karena modul).
- **Harness visual:** `scripts/visual.sh --modules` (env `VISUAL_MODULES`) menyimpan halaman `pkg-dasar-*` dan `pkg-keuangan-*`: beranda admin, bendahara, guru, kepala sekolah, portal orang tua, pusat Pengaturan, dan Modul & Paket. Semuanya diperiksa, tanpa gulir horizontal di HP 360 px.
- **Temuan visual:** beranda guru di paket tanpa modul akademik (paket Keuangan) menyisakan kolom kiri kosong. Kini beranda menjadi satu kolom bila tidak ada kartu utama.
- **Dokumentasi:**
  - `doc/panduan-ujian/README.md`: modul Ujian diaktifkan tim AKRA, halaman "Modul tidak aktif", dan fitur yang butuh modul lain; `guru.md` dan `koordinator.md` menyebut modul Nilai dan Pendaftaran;
  - runbook ujian bagian 7: menangani "modul tidak aktif" dan permintaan aktivasi;
  - ADR 0002 bagian 9: keputusan final implementasi.
- **Menyusul (di luar fase 22):**
  - migrasi penghapusan kunci lama `exam.enabled`/`exam.release` di rilis berikutnya;
  - UI implementator.

## Kriteria selesai
- [x] Uji kombinasi lulus untuk semua kombinasi di atas (tanpa 500, tanpa tautan ke modul mati)
- [x] Tangkapan layar menu dan beranda paket Dasar dan Keuangan diperiksa
- [x] ADR 0002 diperbarui dengan keputusan final
