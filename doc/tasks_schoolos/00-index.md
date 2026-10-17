# Daftar Task MVP 1 SchoolOS

Acuan: `rancangan_arsitektur_schoolos.md` dan `rencana_mvp_schoolos.md`.

## Aturan Task
- Setiap task dibuat cukup kecil untuk selesai dalam beberapa jam sampai satu hari.
- Setiap task punya dependensi dan kriteria selesai yang bisa dicek.
- Task dikerjakan sesuai urutan nomor, kecuali dependensi menyatakan lain.
- Setiap route baru wajib punya uji otorisasi (lihat T58 dan T59).
- Setiap aturan keuangan wajib punya uji unit (lihat T31, T39, T60).

## Daftar Fase

| Folder | Task | Isi | Estimasi |
|---|---|---|---|
| `01-fondasi/` | T01–T15 | Setup proyek, database, login, sesi, CSRF, RBAC, embed aset, reset password, Railway, audit | 2 minggu |
| `02-master-data-absensi/` | T16–T27 | Tahun ajaran, kelas, siswa, enrollment, orang tua, akun orang tua, absensi, rekap | 2 minggu |
| `03-keuangan/` | T28–T42 | Jenis tagihan, tagihan massal, pembayaran, bukti transfer, pembatalan, kuitansi, tunggakan | 3–4 minggu |
| `04-portal-laporan/` | T43–T51 | Portal orang tua, laporan kehadiran dan tunggakan, ekspor besar, pengumuman, beranda per peran | 2 minggu |
| `05-impor-uji/` | T52–T63 | Impor Excel, validasi, impor idempoten, uji lintas sekolah, uji keuangan, uji beban, backup, uji internal | 1–2 minggu |

## Urutan Kritis
1. T01 → T05 → T06 → T07 → T08 → T10 (fondasi login dan peran)
2. T16 → T18 → T20 → T21 (struktur sekolah dan siswa)
3. T28 → T30 → T31 → T34 → T38 (keuangan inti)
4. T58 dan T59 harus lulus sebelum T63

## Keputusan yang Masih Terbuka
- Nilai ambang persetujuan pembatalan pembayaran (T38)
- Aturan selisih transfer (T34, T35)
- Satu bukti transfer untuk beberapa pembayaran (T36)
- Layanan email untuk reset password dan aktivasi di production (T13, T24)
- Cara menjalankan migrasi di Railway (T14)
- Definisi persentase kehadiran (T27)
- Hak akses guru mata pelajaran (T26)
- Batas tanggal edit absensi (T26)
- Pengaturan rekening tujuan transfer (T35)
