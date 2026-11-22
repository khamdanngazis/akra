# 22 — Modul dan Paket per Sekolah

Setiap sekolah memakai paket modul sesuai kebutuhan (mis. tanpa keuangan, tanpa pendaftaran, tanpa ujian). Modul yang tidak dipakai tidak tampil dan tidak bisa dibuka, tanpa merusak modul lain. Rancangan dan keputusan yang dibutuhkan: [ADR 0002](../../adr/0002-modul-per-sekolah.md). Analisis menu: [doc/analisis-menu.md](../../analisis-menu.md).

| Task | Isi | Estimasi |
|---|---|---|
| [T163](T163-katalog-dan-status-modul.md) | Katalog modul di kode, tabel `school_modules`, migrasi, status per request tanpa login ulang | 2 hari |
| [T164](T164-penegakan-route.md) | Semua route modul tertutup bila modulnya mati; registry route bermodul | 2 hari |
| [T165](T165-beranda-portal-laporan.md) | Beranda, portal orang tua, laporan, impor, dan pencarian mengikuti modul | 2 hari |
| [T166](T166-fitur-lintas-modul-dan-peran.md) | Fitur lintas modul dan peran (bendahara, koordinator ujian) mengikuti modul | 1,5 hari |
| [T167](T167-modul-dan-paket-untuk-sekolah.md) | Halaman "Modul & paket" untuk sekolah, permintaan aktivasi, pusat pengaturan per modul | 1 hari |
| [T168](T168-pengelola-modul-dan-paket.md) | Pengelola untuk tim AKRA: layanan bersama, paket, penjaga, `akra-modul`, dasbor internal | 2 hari |
| [T169](T169-uji-kombinasi-modul.md) | Uji kombinasi (inti saja, per paket, semua kecuali satu) dan dokumentasi | 1,5 hari |

**Urutan:** T156 (menu, folder 21) → T163 → T164 → (T165, T166, T168 paralel) → T167 → T169.

**Sebelum mulai:** keputusan pemilik produk di ADR 0002 bagian 8, terutama siapa yang mengatur modul dan isi paket.
