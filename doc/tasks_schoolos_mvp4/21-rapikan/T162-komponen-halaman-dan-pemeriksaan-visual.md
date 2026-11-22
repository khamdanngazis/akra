# T162 — Komponen halaman bersama dan pemeriksaan visual

**Fase:** 21 Rapikan
**Dependensi:** T156–T161
**Estimasi:** 2 hari

## Latar belakang
Setiap halaman menyusun header, tombol, dan kartu sendiri-sendiri:
- **Jumlah tombol utama tidak menentu.** Ada halaman tanpa tombol utama; ada yang punya beberapa.
- **Tombol kedua selalu bergaris biru**, sehingga baris aksi terlihat ramai.
- **Kartu bersarang di dalam kartu**, misalnya opsi "Buat ujian" di dalam kartu dan "Ringkasan" berisi kotak-kotak.
- **Lebar konten berbeda:** layar pengawas sempit di tengah, halaman lain penuh.
- **Belum ada pemeriksaan visual rutin**, jadi ketidakrapian baru muncul lagi tanpa terlihat.

## Keputusan
1. **Partial `page_header`** (judul, keterangan, satu tombol utama, tombol kedua, menu "Lainnya") dipakai semua halaman modul ujian. Halaman modul lain menyusul bertahap saat disentuh.
2. **Hierarki tombol:**
   - maksimal satu `btn-primary` per area;
   - tombol kedua memakai `btn-ghost`;
   - aksi ketiga dan seterusnya masuk ke menu "Lainnya";
   - tombol berbahaya hanya di dalam menu atau form konfirmasi.
3. **Tanpa kartu bersarang.** Pilihan di dalam kartu memakai daftar bergaris atau `option-cards` tanpa bayangan.
4. **Lebar konten:** halaman daftar dan detail penuh; formulir maksimal 48rem; layar kerja (pengawas, koreksi esai) penuh dengan kolom samping di desktop.
5. **Pemeriksaan visual:** tangkapan layar halaman utama modul ujian di desktop (1280 px) dan HP (360 px) dibuat dari harness visual, lalu dilampirkan di setiap PR yang mengubah tampilan modul ujian.

## Langkah
1. **Partial dan CSS:** buat `partials/page_header.html` dan `partials/action_menu.html` (dari T161). Rapikan kelas CSS yang dobel (`empty-state` vs `empty-guide`, `card-title` vs `h2`).
2. **Terapkan ke semua template staf `web/templates/exam/`.** Hapus kartu bersarang di daftar ujian, ringkasan hasil, latihan, dan agenda.
3. **Harness visual yang masuk repo:**
   - `scripts/visual` (atau perintah `make visual`) menyimpan HTML halaman modul ujian dengan data demo, lalu memotretnya dengan Chrome headless ke `VISUAL_OUT`;
   - menggantikan berkas lokal `cmd/server/zz_visual_test.go`;
   - hanya berjalan bila `VISUAL_OUT` diisi, jadi tidak membebani CI.
4. **Uji lint template:** gagal bila satu halaman modul ujian punya lebih dari satu `btn-primary` di header, atau `<section class="card">` di dalam kartu lain.
5. **Periksa semua halaman di HP 360 px:** tidak ada gulir horizontal halaman, tombol tidak terpotong, dan tabel lebar memakai `table-wrap`.

## File yang disentuh
- `web/templates/partials/page_header.html`, `action_menu.html`
- `web/templates/exam/*.html`, `web/tailwind/app.css`
- `scripts/visual/` atau `Makefile`, uji lint template di `internal/exam`

## Catatan implementasi
- **`page_header` dan `action_menu`** (partial baru) menerima aksi `render.Action` yang dibuat di template dengan `action`, `post`, `danger`, `when`, dan `actions`. Dipakai di 19 halaman staf modul ujian (beranda, daftar ujian, agenda, buat agenda, ruang, sesi saya, telaah, bantuan, coba halaman ujian, TP, impor, pratinjau impor, cek mutu, pilih soal, stimulus, form bank, form stimulus, pengaturan, dan menu "Lainnya" detail bank).
- **Header dengan keterangan kaya tetap markup sendiri:** detail bank, kerangka ujian (T160), detail agenda, lihat soal, wizard, form soal (status simpan), dan daftar bank (atribut tur). Template Go tidak bisa mengoper potongan HTML ke partial; aturan yang sama dijaga uji lint.
- **Uji lint** (`pageLint`, dipakai `TestExamPagesFrame` untuk 52/42/34 halaman per peran dan `TestVisual` untuk halaman demo):
  - satu header halaman berisi `<h1>` (`.page-header`, atau `.work-head` di layar kerja pengawas dan pusat kendali);
  - paling banyak satu tombol utama dan dua tombol di luar menu "Lainnya";
  - tidak ada kartu di dalam kartu;
  - tombol merah hanya di menu atau form dengan konfirmasi.
- **Kartu bersarang yang diperbaiki:** kelompok soal satu stimulus di bank (kini garis kiri, bukan kartu), urutan soal di halaman stimulus, kartu sesi di beranda dan Sesi Saya, soal kembar di cek mutu, dan form ralat di detail ujian. Semuanya memakai `.tile` (bergaris tanpa bayangan).
- **Tombol kedua (`btn-ghost`) kini abu-abu netral** di seluruh aplikasi, sehingga hanya tombol utama yang biru.
- **Konfirmasi `data-confirm`** pada form ditangani `js/nav.js` di semua halaman (sebelumnya hanya di layar pengawas).
- **Bar tab:** di HP, tab aktif digulir ke tengah (sebelumnya tab Hasil tidak terlihat).
- **Harness visual:** `cmd/server/visual_test.go` (sebelumnya berkas lokal) dan `scripts/visual.sh` / `make visual`:
  - 84 halaman (21 di modul ujian) dipotret di desktop 1280 px dan HP 360 px;
  - lebar halaman di HP diukur otomatis; skrip gagal bila ada gulir horizontal dan menyebut elemen penyebabnya;
  - `--panduan` menyalin tangkapan ke `doc/panduan-ujian/img`.
- **HP 360 px:** layar pengawas (405 px) dan pilihan kelas absensi guru (422 px) melebar; keduanya diperbaiki. Kini tidak ada halaman yang melebar.
- **CSS dobel:** `empty-state` dan `empty-guide` dipertahankan karena berbeda fungsi, `card-title` untuk judul di dalam kartu. Aturannya dicatat di `doc/brand-identity.md` bagian 6, bersama hierarki tombol dan lebar konten. Formulir tetap `.form-card` (42rem, di bawah batas 48rem).

## Kriteria selesai
- [x] Semua halaman staf modul ujian memakai `page_header` atau markup yang sama dan lulus uji lint (satu tombol utama di header, tanpa kartu bersarang); lihat catatan
- [x] Harness visual masuk repo dan menghasilkan tangkapan desktop dan HP untuk 21 halaman modul ujian (84 halaman seluruhnya)
- [x] Pemeriksaan HP 360 px: tidak ada gulir horizontal (diukur otomatis oleh `scripts/visual.sh`)
- [x] Panduan `doc/panduan-ujian/` memakai tangkapan layar baru
