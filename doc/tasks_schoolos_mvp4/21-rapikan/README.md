# 21 — Rapikan Navigasi dan Tampilan

Dikerjakan sebelum pilot (T155), agar sekolah pilot memakai tampilan yang sudah rapi. Hasil audit tampilan 2026-11-22:
- menu seluruh aplikasi datar 24 item ([analisis menu](../../analisis-menu.md));
- modul ujian tanpa sub-navigasi;
- breadcrumb dan judul tidak konsisten;
- detail dan hasil ujian terpisah;
- tombol aksi menumpuk.

| Task | Isi | Estimasi |
|---|---|---|
| [T156](T156-menu-seluruh-aplikasi.md) | Menu seluruh aplikasi berkelompok (Akademik, Ujian, Pendaftaran, Keuangan, Dapodik, Informasi), pusat Pengaturan menggantikan 8 item pengaturan, label diperjelas, bottom nav dan pintasan beranda per peran, titik masuk impor nilai | 3 hari |
| [T157](T157-kerangka-modul-ujian.md) | Sub-navigasi modul ujian, breadcrumb berakar "Ujian", aturan judul halaman | 2 hari |
| [T158](T158-beranda-modul-ujian.md) | Beranda modul: Hari ini, Perlu tindakan, Mulai cepat | 1,5 hari |
| [T159](T159-daftar-dan-buat-ujian.md) | Daftar ujian dengan saringan dan paginasi; satu tombol Buat ujian; satu nama untuk latihan halaman ujian | 1,5 hari |
| [T160](T160-detail-dan-hasil-ujian.md) | Detail, hasil, analisis, koreksi, dan kirim rapor dalam satu kerangka; satu tombol utama per status | 2 hari |
| [T161](T161-bank-soal-rapi.md) | Aksi bank soal dikelompokkan; bank bawaan berlabel Contoh; saringan mapel | 1 hari |
| [T162](T162-komponen-halaman-dan-pemeriksaan-visual.md) | Komponen header dan menu bersama, hierarki tombol, tanpa kartu bersarang, harness visual di repo | 2 hari |

**Urutan:** T156 → T157 → (T158, T159, T160, T161 bisa paralel) → T162. T156 juga menjadi dasar modul per sekolah (`22-modul-sekolah/`).

**Prinsip:**
- Tidak ada fitur baru dan tidak ada perubahan hak akses. URL lama tetap berlaku.
- Setiap task disertai tangkapan layar sebelum dan sesudah (desktop dan HP).
