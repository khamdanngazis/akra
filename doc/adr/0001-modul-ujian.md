# ADR 0001 — Arsitektur modul ujian online (hasil spike T123)

- **Status:** diusulkan, menunggu persetujuan pemilik produk
- **Tanggal:** 2026-11-17
- **Task:** [T123](../tasks_schoolos_mvp4/14-fondasi/T123-spike-teknis-dan-adr.md)
- **Prototipe:** [`spike/mvp4/`](../../spike/mvp4/README.md), modul Go terpisah. Dihapus setelah ADR ini disetujui.

## Ringkasan keputusan

| # | Spike | Keputusan |
|---|---|---|
| 1 | Rendering rumus | **Render di server saat soal disimpan** (KaTeX lewat `goja`). HTML hasilnya disimpan. Halaman ujian hanya memuat CSS + font KaTeX, **tanpa JS rumus** |
| 2 | Cetak dokumen bersoal rumus | **Halaman HTML khusus cetak** (`@media print`), dicetak dari browser. `fpdf` tetap untuk dokumen tanpa rumus |
| 3 | Impor Word | Lanjut T129 seperti rencana: OMML → LaTeX, gambar, tabel, format pangkat/indeks, teks Arab. MathType lama: pakai pratinjau PNG; **WMF/EMF dilaporkan per soal** untuk diperbaiki di editor |
| 4 | Protokol simpan jawaban | **Token attempt HMAC + satu pernyataan SQL** (cek attempt dan upsert batch sekaligus). Tidak membaca sesi `scs` |
| 5 | Mode offline | **Service worker** untuk cangkang + payload + gambar, **outbox IndexedDB** untuk jawaban, kirim ulang saat `online` dan berkala |
| 6 | Editor rumus guru | **Toolbar templat + pratinjau langsung** (KaTeX di browser, hanya di halaman guru). MathLive tidak dipakai di MVP 4 |
| 7 | Tulisan Arab | Font **Noto Naskh Arabic** (subset Arab, dimuat lewat `unicode-range`), arah per paragraf, rumus diisolasi dari bidi |

Lingkungan ukur: laptop 8 core (WSL2), Go 1.22.2, PostgreSQL 16 di Docker, Chrome 135 headless. Angka laptop adalah batas atas kasar, bukan angka produksi. Uji beban produksi tetap di T152.

---

## 1. Rendering rumus

### Pilihan
- (a) KaTeX di browser pada setiap tampilan.
- (b) KaTeX dijalankan di Go (`github.com/dop251/goja`) saat soal disimpan; HTML hasilnya disimpan di kolom soal.

### Angka ukur

| Ukuran | (a) browser | (b) server |
|---|---|---|
| JS di halaman ujian | `katex.min.js` 267 KB (**76 KB gzip**) | **0 KB** |
| CSS | 24 KB (3,5 KB gzip) | sama |
| Font (woff2, 20 berkas) | total 254 KB; praktis 3–5 berkas dipakai (± 60–80 KB), diunduh hanya bila ada rumus | sama |
| Waktu render | ± 0,15 ms/rumus (V8 desktop); HP murah ± 5–10× lebih lambat, di setiap tampilan | **2,6 ms rerata, p95 5,2 ms** per rumus, sekali saat simpan |
| Inisialisasi | — | 50–70 ms per VM (sekali per VM) |
| Throughput paralel | — | pool 8 VM: 836 render/detik, heap 56 MB |
| Ukuran HTML | — | rerata 1,75 KB per rumus |

Anggaran halaman ujian 150 KB (indeks bagian 4). Pilihan (a) menghabiskan separuhnya hanya untuk JS rumus. Pilihan (b) tidak memakai JS sama sekali.

### Keamanan
- Output dibuat server dari LaTeX, bukan HTML kiriman klien.
- Opsi wajib: `trust:false`, `throwOnError:true`, `maxExpand:200`, `maxSize:20`.
- Diuji: `\href{javascript:…}` tidak menghasilkan tautan; `<script>` di-escape; makro rekursif `\def\a{\a}\a` ditolak dengan error, bukan hang.
- HTML hasil KaTeX tetap disanitasi dengan allowlist tag/atribut KaTeX sebelum disimpan (pertahanan berlapis).

### Catatan implementasi (T128)
- `goja.Runtime` **tidak aman dipakai bersamaan**: pakai pool VM (`sync.Pool` atau kanal) dengan ukuran ≤ `GOMAXPROCS`.
- **Versi goja dikunci** ke `v0.0.0-20250125213203-5ef83b82af17`. Versi terbaru menuntut toolchain Go 1.26, sedangkan proyek di Go 1.22.2.
- `katex.min.js` di-embed ke binary (+ ± 270 KB). Versi KaTeX di server dan CSS/font di klien **harus sama** (dikunci 0.19.1).
- LaTeX sumber tetap disimpan agar bisa dirender ulang saat KaTeX diperbarui (job migrasi).

## 2. Cetak naskah dan dokumen bersoal rumus

- Halaman HTML yang sama dengan halaman soal, ditambah `@media print` (`break-inside: avoid` per soal, tanpa navigasi). Dicetak dari browser ("Simpan sebagai PDF").
- Diuji di Chrome headless: rumus (pecahan, akar, matriks, integral) dan teks Arab tercetak benar. PDF 3 halaman contoh: 90 KB. Bukti: [`printmedia-naskh.png`](../../spike/mvp4/arabic/printmedia-naskh.png).
- `fpdf` tetap dipakai untuk dokumen tanpa rumus: kartu peserta, daftar hadir, berita acara (T135).
- **Keterbatasan:** hasil cetak bergantung pada browser pemakai. Panduan cetak (margin, "grafik latar") masuk T154. Chrome/Edge desktop menjadi acuan.

## 3. Impor Word (`.docx`)

### Prototipe
Parser `encoding/xml` tanpa dependensi baru ([`spike/mvp4/docx`](../../spike/mvp4/docx)):
- paragraf, tabel, gambar (`w:drawing`/`a:blip`);
- **OMML → LaTeX**: pecahan (`m:f`, termasuk linear dan binom), pangkat/indeks (`m:sSup`, `m:sSub`, `m:sSubSup`), akar (`m:rad`, dengan derajat), kurung (`m:d`), matriks (`m:m`), sigma/integral/produk (`m:nary`), fungsi (`m:func`, termasuk `lim` dengan `m:limLow`), aksen/vektor (`m:acc`), garis atas/bawah (`m:bar`), sistem persamaan (`m:eqArr`), dan 43 simbol Unicode (×, ÷, ±, ≤, π, α, °, →, …);
- **format pangkat/indeks pada huruf** (`w:vertAlign`), misalnya H₂O dan 10⁸ yang diketik guru tanpa Equation → `<sub>`/`<sup>`;
- **MathType/Equation 3.0** (`w:object`): gambar pratinjau dari `v:imagedata`;
- **arah tulisan**: `w:bidi` (paragraf) dan `w:rtl` (run) terbaca;
- segmentasi soal: `1.` memulai soal, `A.`/`a.` opsi, opsi Arab `أ ب ج د هـ`, baris `Kunci: B`.
- Setiap LaTeX hasil konversi divalidasi dengan KaTeX. Rumus yang gagal tidak menggagalkan impor; soalnya ditandai.

### Hasil pada dokumen contoh

| Dokumen | Soal | Sempurna |
|---|---|---|
| Matematika, Equation bawaan Word (OMML) | 18 | 18 (100%) |
| IPA, pangkat/indeks dengan format huruf | 5 | 5 (100%) |
| Matematika, MathType lama (5 pratinjau PNG, 5 hanya WMF) | 10 | 5 (50%) |
| PAI/Bahasa Arab (teks berharakat, opsi kanan-ke-kiri) | 5 | 5 (100%) |
| **Total** | **38** | **33 (87%)** |

Putaran pertama: 94% pada dokumen OMML. `lim` dan `m:eqArr` belum didukung; keduanya ditambahkan lalu diukur ulang.

### Keterbatasan dan risiko
- **Dokumen di atas sintetis**, disusun meniru format soal guru. Dokumen guru sungguhan belum tersedia. Kriteria "dokumen Word contoh dari guru sungguhan" **belum terpenuhi** (lihat bagian Tindak lanjut). Angka 87% belum boleh dipakai untuk gerbang 70%.
- **MathType dengan pratinjau WMF/EMF tidak bisa dikonversi** tanpa pustaka grafis eksternal. Soal seperti ini dilaporkan per soal ("rumus belum terbaca, ketik ulang di editor"). Template Word resmi memakai Equation bawaan Word.
- Penomoran otomatis Word (`w:numPr`) tidak menghasilkan teks "1." di XML. T129 harus membaca `numbering.xml` atau meminta guru memakai template.

### Gerbang T129
Bila pada 3–5 dokumen guru sungguhan hasilnya < 70% soal sempurna, T129 diubah menjadi "template Word resmi saja" dan risiko di indeks diperbarui.

## 4. Protokol simpan jawaban

### Desain
- **Token attempt** dibuat saat attempt dimulai: `base64url(attempt_id ‖ device_id ‖ exp) "." base64url(HMAC-SHA256)`. Dikirim di header `X-Attempt-Token`. Verifikasi murni CPU, tanpa query.
- **Satu pernyataan SQL** per simpan. Pernyataan itu sekaligus mengecek attempt masih `berjalan`, perangkat cocok, dan belum lewat batas waktu:

  ```sql
  INSERT INTO exam_answers (attempt_id, question_id, value, client_seq)
  SELECT a.id, u.q, u.v, u.s
  FROM exam_attempts a, unnest($3, $4, $5) AS u(q, v, s)
  WHERE a.id = $1 AND a.device_id = $2 AND a.status = 'berjalan' AND a.deadline > now()
  ON CONFLICT (attempt_id, question_id) DO UPDATE
  SET value = excluded.value, client_seq = excluded.client_seq, saved_at = now()
  WHERE exam_answers.client_seq < excluded.client_seq
  ```

- `client_seq` naik per perubahan di perangkat. Jawaban lama yang terkirim ulang dari antrean offline tidak menimpa jawaban baru.
- Respons berisi `seq` tertinggi yang diterima. Klien hanya menghapus antrean sampai `seq` itu.
- Attempt yang dikunci/dikumpulkan/dipindah perangkat langsung ditolak oleh `WHERE` (0 baris) tanpa menunggu token kedaluwarsa. Server membedakan "0 baris" dari sukses dan mengembalikan 409 agar klien menampilkan pesan.
- Endpoint simpan berada **di luar** middleware `scs.LoadAndSave` dan CSRF; perlindungannya token di header (tidak dikirim otomatis oleh browser).

### Angka ukur
5.000 klien simulasi dalam satu proses dengan server (Go `net/http`), pool DB 10 koneksi (sama dengan produksi), 2 jawaban per simpan.

| Skenario | Throughput | p50 | p95 | p99 | Gagal |
|---|---|---|---|---|---|
| **Token**, simpan tiap 10 dtk/peserta | 500 req/dtk | 3,8 ms | **18 ms** | 118 ms | 0 |
| Token, tiap 6 dtk | 832 req/dtk | 6,2 ms | 67 ms | 197 ms | 0 |
| Token, tiap 4 dtk | 1.029 req/dtk | 200 ms | 5,7 dtk | 6,2 dtk | 727 |
| Token, tiap 4 dtk, pool 20 | 1.200 req/dtk | 9,4 ms | 1,0 dtk | 1,5 dtk | 0 |
| **Sesi `scs`** (SELECT + UPDATE sesi), tiap 10 dtk | 297 req/dtk | 934 ms | **23 dtk** | 24 dtk | 909 |

- Membaca dan memperbarui sesi `scs` di setiap simpan **tidak layak**: gagal pada beban yang dengan token masih p95 18 ms.
- Satu replika dengan pool 10 nyaman sampai ± 800 req/dtk di laptop. Jenuh di sekitar 1.000 req/dtk.

### Aturan klien (T139)
- Simpan saat jawaban berubah, di-*debounce* 2 detik, dan digabung dalam satu batch. Paling sering satu request per 5 detik per peserta, kecuali saat pindah soal terakhir dan kumpul.
- Tanpa perubahan, tidak ada request simpan. Detak "masih online" dibatasi satu per 30 detik dan tidak menulis ke DB setiap kali.
- Perkiraan beban nyata 5.000 peserta (± 1 jawaban/menit/peserta): ± 100–200 req/dtk, jauh di bawah titik jenuh.

## 5. Mode offline

### Prototipe
[`spike/mvp4/offline`](../../spike/mvp4/offline):
- **Service worker**: cangkang (HTML, CSS, JS) di-cache saat install; payload soal dan gambar *network-first* dengan salinan cache; POST simpan tidak pernah lewat cache.
- **Outbox IndexedDB**: setiap jawaban ditulis ke perangkat dulu, baru dikirim. Dikirim lagi pada event `online`, setiap 10 detik, dan saat halaman dibuka lagi.
- Status selalu terlihat: "Tersimpan", "Offline — 2 jawaban menunggu dikirim".

### Uji (Chrome 135 headless, emulasi Galaxy S8)
- [x] Service worker mengendalikan halaman setelah muat pertama
- [x] Muat ulang saat offline: 5 soal tampil dari cache
- [x] Gambar soal tampil saat offline
- [x] Jawaban saat offline masuk antrean
- [x] Tab ditutup saat offline, dibuka lagi saat online: antrean terkirim, server menerima ketiga jawaban

Ukuran prototipe (gzip): HTML 0,3 KB, CSS 0,3 KB, JS 1,4 KB, SW 0,5 KB.

### Catatan
- **Belum diuji di HP sungguhan** (Chrome Android, Safari iOS). Masuk matriks perangkat T142.
- Safari iOS tidak punya Background Sync. Desain ini tidak memakainya; pengiriman dipicu `online`, interval, dan buka halaman.
- Safari menghapus data situs yang tidak dibuka 7 hari. Tidak berpengaruh selama ujian berlangsung.
- Gambar soal memakai URL bertanda tangan yang berlaku selama attempt (indeks 4.1). Cache kunci gambar harus memakai path tanpa query tanda tangan.
- Kunci jawaban tidak pernah ada di payload, jadi cache perangkat tidak membocorkan kunci.

## 6. Editor rumus untuk guru

| | MathLive 0.111.4 | Toolbar templat + pratinjau |
|---|---|---|
| JS | 828 KB (**223 KB gzip**) + CSS/font | KaTeX 76 KB gzip (atau 0 KB bila pratinjau dari server) + toolbar ± 2 KB |
| Bahasa Indonesia | **tidak ada** (tersedia ar, de, el, es, fr, he, it, ja, ko, pl, pt, uk) | label sendiri, Indonesia |
| Tablet | papan ketik virtual bagus | tombol templat besar; tetap mengetik di kolom teks |
| Hasil | LaTeX | LaTeX |

**Keputusan:** toolbar templat (pecahan, pangkat, indeks, akar, matriks, sigma, integral, simbol umum, huruf Yunani) yang menyisipkan LaTeX dengan penanda isian, ditambah pratinjau langsung dengan KaTeX di browser.
- Editor hanya dimuat di halaman guru; halaman ujian tidak terpengaruh.
- Jalur utama guru tetap impor Word (T129). Editor untuk memperbaiki dan menulis soal pendek.
- MathLive dievaluasi ulang setelah pilot bila guru kesulitan dengan toolbar.

## 7. Tulisan Arab

### Font (subset Arab, woff2, regular)

| Font | Ukuran | Catatan uji di 360 px |
|---|---|---|
| **Noto Naskh Arabic** | **52 KB** | Harakat jelas dan tidak bertabrakan; serasi dengan teks Latin |
| Scheherazade New | 79 KB | Harakat paling besar dan terbaca; huruf lebih lebar, baris lebih panjang |
| Amiri | 109 KB | Kaligrafis; tasydid + fathah rapat di ukuran kecil |

**Keputusan:** Noto Naskh Arabic 400 (dan 700 bila perlu tebal, 53 KB).
- Dimuat dengan `@font-face` + `unicode-range` blok Arab. Ujian tanpa teks Arab **tidak mengunduh** font ini.
- Ukuran teks Arab 1,5× teks Latin, `line-height` ± 2,1 agar harakat tidak terpotong.

### Aturan arah tulisan
1. Arah ditentukan **per blok** (paragraf, opsi), bukan per halaman. Blok berbahasa Arab mendapat `lang="ar" dir="rtl"`. Blok campuran memakai `dir="auto"`.
2. **Nomor soal dan huruf opsi (A–E) berada di luar blok teks**, di kolom grid sendiri, dan selalu LTR. Arah teks soal tidak bisa menggeser nomor atau huruf opsi.
3. **Rumus diisolasi:** `.katex { direction: ltr; unicode-bidi: isolate }`. Temuan spike: tanpa aturan ini, `x+2=5` di dalam paragraf Arab tampil terbalik menjadi `5 = 2 + x`. Dengan aturan ini, tampil benar.
4. Tanda baca Arab (`،` `؟`) mengikuti arah blok. Ini sudah benar dengan aturan 1.
5. Impor Word: `w:bidi` atau `w:rtl` → blok RTL.
6. Editor (T128): tombol "Teks Arab" membungkus pilihan dalam blok RTL. Kolom teks memakai `dir="auto"`.

Bukti: [`shot-naskh.png`](../../spike/mvp4/arabic/shot-naskh.png) (360 px, Chrome Android diemulasi) dan [`printmedia-naskh.png`](../../spike/mvp4/arabic/printmedia-naskh.png) (cetak A4).

---

## Konsekuensi
- **Dependensi baru di T128:** `github.com/dop251/goja` (versi dikunci) dan `katex.min.js` 0.19.1 yang di-embed. Binary bertambah **± 7 MB** (diukur dengan `-ldflags="-s -w"`; produksi 11,7 MB → ± 19 MB). Masih wajar untuk image Railway.
- **Aset statis baru:** CSS + font KaTeX (± 280 KB total, diunduh sesuai kebutuhan) dan Noto Naskh Arabic (52 KB, hanya bila ada teks Arab).
- **Endpoint peserta di luar `scs`:** route simpan jawaban dan payload memakai token attempt. Registry route (`internal/testutil/routes.go`) perlu jenis route baru untuk endpoint bertoken (T138/T139).
- **Kunci HMAC** baru di konfigurasi (`EXAM_TOKEN_KEY`), dengan rotasi dua kunci aktif seperti keyring PII.

## Tindak lanjut sebelum ADR disetujui
- [ ] **Pemilik produk** mengumpulkan 3–5 dokumen Word soal dari guru sungguhan (data pribadi dihapus), lalu `go test -v ./docx` dijalankan ulang dengan dokumen itu. Hasilnya ditulis di bagian 3 dan menentukan gerbang T129.
- [ ] Uji halaman Arab dan prototipe offline di **HP Android sungguhan** (Chrome ≥ 90, RAM 2 GB) dan **iPhone** (Safari ≥ 15).
- [ ] Persetujuan pemilik produk atas tujuh keputusan di atas.

## Status kriteria selesai T123
- [x] ADR berisi keputusan dan angka ukur untuk keenam spike
- [ ] Dokumen Word contoh dari guru sungguhan dipakai — **belum**, memakai dokumen sintetis
- [ ] Rekomendasi diterima pemilik produk sebelum T128/T129/T139 dimulai
- [~] ADR memuat pilihan font Arab dan aturan arah tulisan; diuji di cetak dan emulasi HP, **belum di HP sungguhan**
