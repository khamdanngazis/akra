# T157 — Kerangka modul ujian: sub-navigasi, breadcrumb, dan judul

**Fase:** 21 Rapikan
**Dependensi:** T156
**Estimasi:** 2 hari

## Latar belakang
Halaman modul ujian tidak punya kerangka yang sama:
- **Tidak ada sub-navigasi.** Telaah, Pengaturan, Sesi saya, Ruang, TP, dan Bantuan hanya bisa dicapai dari kartu beranda, tombol tertentu, atau menu samping.
- **Breadcrumb tidak konsisten:**
  - bank soal, TP, dan soal tidak berakar di "Ujian" ("Bank soal / TP", "{bank} / Soal / Ubah");
  - label "Ujian" kadang menuju beranda modul (`/ujian`), kadang ke daftar ujian (`/ujian/paket/`);
  - koreksi esai punya breadcrumb "Beranda / Koreksi esai";
  - halaman hasil melompati "Daftar ujian".
- **Judul tidak konsisten:**
  - beranda modul dan daftar ujian sama-sama berjudul "Ujian";
  - huruf kapital campur ("Bank Soal", "Agenda Ujian" vs "Telaah soal", "Pengaturan ujian", "Sesi saya hari ini");
  - halaman hasil dan analisis tidak punya `<h1>` halaman.

## Keputusan
1. **Sub-navigasi modul** di bawah header setiap halaman staf `/ujian/...` (kecuali cetak dan layar penuh). Isinya sesuai hak akses:

   | Item | Path | Tampil untuk |
   |---|---|---|
   | Ringkasan | `/ujian` | semua staf modul |
   | Bank soal | `/ujian/bank/` (termasuk TP, soal, impor) | ActBank |
   | Ujian | `/ujian/paket/` | ActClassExam, ActAdmissionTest |
   | Agenda | `/ujian/agenda` (termasuk ruang, pusat kendali) | ActAgenda |
   | Sesi saya | `/ujian/sesi-saya` | guru pengawas, koordinator, admin TU |
   | Telaah | `/ujian/telaah` | bila telaah aktif dan pengguna penelaah |
   | Pengaturan | `/ujian/pengaturan` | admin TU, koordinator |
   | Bantuan | `/ujian/bantuan` | semua staf modul, di ujung kanan |

   Di HP, sub-navigasi bisa digulir horizontal dan item aktif digulir ke tengah. Telaah Soal dan Pengaturan Ujian dihapus dari menu samping (T156).
2. **Breadcrumb:** selalu dimulai "Ujian" → `/ujian`, lalu bagian sub-navigasi, lalu objek. Contoh:
   - Ujian › Bank soal › {bank} › Tambah soal
   - Ujian › Bank soal › Tujuan pembelajaran
   - Ujian › Daftar ujian › {ujian} › Hasil › Koreksi esai
   - Ujian › Agenda › {agenda} › Pusat kendali
3. **Judul:**
   - halaman tujuan menu atau sub-navigasi memakai label navigasinya ("Bank Soal", "Daftar Ujian", "Agenda Ujian", "Sesi Saya", "Telaah Soal", "Pengaturan Ujian");
   - halaman objek memakai nama objeknya;
   - halaman aksi memakai kalimat biasa ("Buat ujian", "Tambah soal").

   Beranda modul berjudul "Ujian". Aturan judul ini dicatat di `doc/brand-identity.md` untuk modul lain.

## Langkah
1. **View dasar `examPage`** di paket exam: `User`, `Nav` (item sub-navigasi beserta yang aktif), dan `Crumbs`. Disematkan di semua view halaman staf modul ujian. Item navigasi dihitung dari `Actor` yang sudah dimuat handler (tanpa query tambahan per halaman, kecuali status telaah yang di-cache per request).
2. **Partial `exam_subnav`** dipasang lewat blok `header` modul. Halaman cetak, token layar penuh, dan halaman peserta tidak memakainya.
3. **Perbaiki breadcrumb** semua template `web/templates/exam/*.html` dan `partials/exam.html` sesuai keputusan 2. Breadcrumb disusun di handler, bukan string literal di setiap template.
4. **Perbaiki `<h1>`** sesuai keputusan 3, termasuk hasil, analisis butir, analisis TP, kirim ke rapor, dan koreksi esai.
5. **Uji registry:** setiap route GET halaman staf `/ujian` di `internal/testutil/routes.go` dirender dengan sub-navigasi dan breadcrumb berakar "Ujian" (kecuali daftar pengecualian cetak/PDF/fragmen).

## File yang disentuh
- `internal/exam/*_handler.go` (view), `internal/exam/nav.go` (baru)
- `web/templates/exam/*.html`, `web/templates/partials/exam.html`, `web/templates/partials/back_nav.html`
- `internal/platform/nav/menu.go`, `doc/brand-identity.md`

## Catatan implementasi
- **Sub-navigasi lewat context, bukan view dasar.** Paket generik `internal/platform/subnav` menyimpan item di context. Middleware modul ujian (`withNav`) memuat `Actor` sekali per request, dan `RequireAccess` serta handler memakai ulang `Actor` itu (sebelumnya dimuat ulang di beberapa tempat). `app_header` merender sub-navigasi bila ada. Akibatnya tidak ada view atau template yang perlu disematkan `examPage`, dan modul lain bisa memakai pola yang sama.
- **Breadcrumb tetap ditulis di template**, tetapi semuanya berakar di "Ujian" → `/ujian` dan dijaga uji registry. Penyusunan di handler ditunda; T160 merombak kerangka halaman satu ujian.
- **Item "Telaah Soal" keluar dari menu samping.** Sekarang hanya di sub-navigasi, dan tampil bila telaah aktif.
- **Judul diganti:** "Daftar Ujian", "Sesi Saya", "Telaah Soal", "Pengaturan Ujian", "Bantuan". Halaman hasil dan analisis sudah punya `<h1>` "Hasil ujian" dari partial bersama.
- **Item aktif di HP** digulir ke tengah oleh `nav.js`.

## Kriteria selesai
- [x] Sub-navigasi tampil di semua halaman staf modul ujian sesuai hak akses (uji per peran: admin TU, guru, kepala sekolah)
- [x] Uji: semua halaman staf `/ujian` (registry) punya breadcrumb berakar "Ujian" → `/ujian` dan tepat satu `<h1>` (52 halaman admin TU, 42 guru, 34 kepala sekolah)
- [x] Tidak ada lagi label "Ujian" yang menuju `/ujian/paket/` (diuji)
- [x] Sub-navigasi bisa digulir di HP (overflow horizontal, item aktif digulir ke tengah); perlu dicek sekali di HP sungguhan
