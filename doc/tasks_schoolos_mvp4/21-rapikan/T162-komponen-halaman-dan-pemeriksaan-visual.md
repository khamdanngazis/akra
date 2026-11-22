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

## Kriteria selesai
- [ ] Semua halaman staf modul ujian memakai `page_header`; uji lint lulus (satu tombol utama di header, tanpa kartu bersarang)
- [ ] Harness visual masuk repo dan menghasilkan tangkapan desktop dan HP untuk ±15 halaman utama
- [ ] Pemeriksaan HP 360 px: tidak ada gulir horizontal (diperiksa dari tangkapan layar)
- [ ] Panduan `doc/panduan-ujian/` memakai tangkapan layar baru
