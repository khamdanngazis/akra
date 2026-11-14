# T108 — Unggah dokumen otomatis dan foto dikecilkan di HP

**Fase:** 13 Perbaikan
**Dependensi:** T91, T92
**Estimasi:** 1–1,5 hari

## Latar belakang
Di langkah Dokumen, setiap dokumen punya dua aksi terpisah: pilih file, lalu klik "Unggah". Pengguna (termasuk yang paham IT) memilih file lalu langsung klik "Lanjut ke pemeriksaan", sehingga file tidak pernah terkirim dan halaman akhir menyebut dokumen belum ada.

Selain itu foto kamera HP umumnya 3–8 MB, sedangkan batas dokumen 2 MB, jadi banyak unggahan akan ditolak tanpa orang tua tahu harus berbuat apa.

## Langkah
1. Unggah otomatis saat file dipilih (event `change`, Alpine.js + `fetch` multipart ke route unggah yang sudah ada). Tombol "Unggah" dihapus; tetap berfungsi tanpa JavaScript lewat form biasa (fallback).
2. Status per dokumen: Belum ada → Mengunggah… (progres) → ✓ Terunggah (pratinjau kecil untuk foto atau nama file PDF, tombol Ganti) atau ✗ Gagal (alasan dalam bahasa Indonesia, tombol Coba lagi).
   - Endpoint unggah mengembalikan JSON untuk permintaan `fetch` (status, pesan) tanpa mengubah perilaku form biasa.
3. Foto (JPEG/PNG) dikecilkan di browser sebelum dikirim: sisi terpanjang sekitar 1600 px, JPEG kualitas ~0,8, target < 2 MB. PDF tidak diubah; PDF > 2 MB tetap ditolak dengan pesan yang menyarankan foto.
4. Tombol "Lanjut ke pemeriksaan": jika masih ada unggahan yang berjalan atau file dipilih tetapi gagal, tampilkan peringatan dan jangan lanjut sebelum pengguna memilih.
5. Input file menerima `image/*` dan `application/pdf`; di HP tersedia pilihan kamera. Tambah petunjuk per dokumen (mis. "Foto KK utuh, tidak terpotong, tidak buram").
6. Berlaku untuk formulir publik dan input oleh TU (halaman yang sama).
7. Validasi server tidak berubah (tipe dari isi file, maks 2 MB, enkripsi T91).

## File yang disentuh
- `web/templates/admission/public_step.html` (langkah Dokumen)
- `internal/admission/public_handler.go`, `staff_entry.go` (respons JSON untuk unggah via `fetch`)
- `web/static/js/` jika skrip kompresi dipisah dari template

## Kriteria selesai
- [x] Memilih file langsung mengunggah tanpa tombol tambahan, dengan status yang jelas
- [x] Foto HP 5 MB berhasil terunggah (dikecilkan) dan tulisan tetap terbaca
- [x] Unggah gagal menampilkan alasan dan tombol Coba lagi
- [x] "Lanjut" memperingatkan jika ada unggahan yang belum selesai/gagal
- [x] Tanpa JavaScript, unggah lewat form biasa tetap bisa
- [x] Uji handler untuk respons JSON dan form biasa (publik dan TU)

## Catatan implementasi
- Skrip `web/static/js/admission-upload.js` (komponen Alpine `docUpload`), dimuat hanya di langkah Dokumen. Unggah lewat XHR (agar ada progres) ke route yang sama dengan header `Accept: application/json` dan `X-CSRF-Token`; server membalas JSON `{ok, message}` (200/400/409/500) untuk publik dan TU, sedangkan form biasa tetap redirect/halaman seperti sebelumnya.
- Setiap kartu dokumen punya tombol **Ambil foto** (kamera HP, `capture`) dan **Pilih file** (foto/PDF), status Belum diunggah → Mengecilkan foto…/Mengunggah… (batang progres) → Terunggah (pratinjau foto) atau Gagal + **Coba lagi**. Petunjuk foto per jenis dokumen.
- Foto di atas 1 MB (atau selain JPG/PNG yang bisa dibaca browser) dikecilkan ke sisi terpanjang 1600 px, JPEG 0,8 (turun ke 0,6 jika masih terlalu besar). Diuji di Chrome: foto 4000×3000 sebesar 5,3 MB terunggah sebagai sekitar 235 KB. PDF tidak diubah; PDF di atas 2 MB ditolak di browser dengan saran memotret dokumennya.
- "Lanjut periksa" menahan jika ada unggahan yang berjalan atau gagal, dengan pilihan **Tetap lanjut**. Tombol Kembali tidak ditahan.
- Tanpa JavaScript, form unggah biasa dengan tombol Unggah tetap tampil dan bekerja.
- Surat keterangan pindah dan rapor tidak ditawarkan di formulir siswa baru kecuali diwajibkan periode.
- Unggah dari halaman periksa (T107) masih memakai form biasa.

