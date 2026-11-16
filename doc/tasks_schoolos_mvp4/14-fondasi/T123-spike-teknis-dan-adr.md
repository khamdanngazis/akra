# T123 — Spike teknis dan keputusan arsitektur (ADR)

**Fase:** 14 Fondasi
**Dependensi:** —
**Estimasi:** 4,5 hari

## Tujuan
Membuktikan bagian paling berisiko **sebelum** membangun fitur, lalu mencatat keputusannya. Hasilnya menentukan desain T128, T129, dan T139.

## Spike
1. **Rendering rumus.**
   - Bandingkan dua cara:
     - (a) KaTeX di browser pada setiap tampilan;
     - (b) **rumus dirender di server saat soal disimpan** (KaTeX dijalankan di Go lewat `goja`), lalu HTML hasilnya disimpan.
   - Ukur: waktu render, ukuran HTML, kebutuhan JS di halaman ujian, keamanan (output dibuat server, bukan dikirim klien), dan hasil cetak.
   - Rekomendasi awal: (b). Halaman ujian cukup memuat CSS dan font KaTeX tanpa JS, dan ringan di HP.
2. **Cetak naskah dan dokumen bersoal rumus.** `fpdf` tidak bisa menggambar rumus. Coba halaman HTML khusus cetak (CSS `@media print`) yang dicetak dari browser ("Simpan sebagai PDF"). Dokumen tanpa rumus (kartu peserta, berita acara) tetap memakai `fpdf`.
3. **Impor Word (`.docx`).** Parse dokumen contoh dari 3–5 guru sungguhan:
   - teks, tabel, dan gambar;
   - **Equation bawaan Word (OMML)** → LaTeX: cakup subset umum (pecahan, pangkat, akar, indeks, matriks, simbol);
   - **MathType/Equation 3.0** (objek OLE): ambil gambar pratinjaunya. Bila hanya WMF/EMF, catat sebagai keterbatasan.
   - Ukur persentase soal yang terimpor sempurna.
4. **Protokol simpan jawaban.** Rancang endpoint simpan yang tidak membaca session `scs` di database pada setiap request. Calon desain:
   - token attempt bertanda tangan (HMAC, berisi `attempt_id`, `device_id`, kedaluwarsa);
   - satu upsert batch.
   - Ukur throughput dengan 5.000 klien simulasi di laptop.
5. **Mode offline.** Prototipe service worker yang menyimpan cangkang halaman ujian, payload soal, dan gambar, sehingga muat ulang saat offline tetap jalan. Uji di Chrome Android dan Safari iOS.
6. **Editor rumus untuk guru.** Bandingkan MathLive (editor visual) dengan toolbar sederhana ditambah pratinjau langsung: ukuran bundle, kemudahan di tablet, dan dukungan bahasa Indonesia. Editor hanya dimuat di halaman guru, tidak di halaman ujian.

## Spike tambahan: tulisan Arab
7. **Teks Arab (kanan-ke-kiri) bercampur teks Latin dan rumus:**
   - uji di editor, render server, halaman ujian (Chrome Android, Safari iOS), dan cetak HTML;
   - pilih font Arab dengan harakat jelas di layar kecil (mis. Noto Naskh Arabic, Amiri, atau Scheherazade New; dibandingkan ukuran subset dan keterbacaan);
   - pastikan arah tulisan per paragraf (`dir="auto"`/`dir="rtl"`) tidak merusak nomor soal, opsi A–E, dan tanda baca;
   - cek pembacaan teks Arab dari `.docx` (`w:bidi`, `w:rtl`).

## Hasil
- `doc/adr/0001-modul-ujian.md`: keputusan untuk keenam spike, beserta angka ukurnya.
- Prototipe dibuang setelah ADR disetujui. Kode final dibuat di task masing-masing.
- Bila impor Word < 70% soal sempurna pada dokumen contoh, T129 diubah menjadi "template Word resmi" saja dan risiko diperbarui di indeks.

## Kriteria selesai
- [ ] ADR berisi keputusan dan angka ukur untuk keenam spike
- [ ] Dokumen Word contoh dari guru sungguhan dipakai (data pribadi dihapus)
- [ ] Rekomendasi diterima pemilik produk sebelum T128/T129/T139 dimulai
- [ ] ADR memuat pilihan font Arab dan aturan arah tulisan, diuji di HP dan cetak
