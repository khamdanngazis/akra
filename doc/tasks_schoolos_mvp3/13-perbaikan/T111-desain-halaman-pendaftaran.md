# T111 — Desain halaman pendaftaran publik yang menarik dan jelas

**Fase:** 13 Perbaikan
**Dependensi:** T90, T95, T107, T108
**Estimasi:** 2–3 hari

## Latar belakang
Halaman publik pendaftaran (`/s/{kode}/daftar` dan `/s/{kode}/status`) masih polos: tanpa identitas sekolah, penunjuk langkah hanya teks ("Langkah 6 dari 6"), dan halaman "Periksa dan kirim" tidak menampilkan ringkasan data yang akan dikirim. Halaman ini adalah kesan pertama orang tua terhadap sekolah.

## Langkah
1. **Kepala halaman bernuansa sekolah:** logo sekolah (diunggah TU di pengaturan sekolah, disimpan di storage), nama sekolah, dan label "Pendaftaran Siswa Baru TA …". Teks kecil "didukung AKRA" di kaki halaman.
2. **Halaman awal informatif:** kartu periode (gelombang, tanggal tutup, biaya formulir), daftar dokumen yang perlu disiapkan dengan ikon, perkiraan waktu mengisi, tombol besar "Mulai daftar" dan "Lanjutkan draf", tautan cek status.
3. **Penunjuk langkah visual:** di layar lebar berupa lingkaran bernomor dengan garis penghubung (selesai / aktif / belum); di HP berupa batang progres + "Langkah N dari 6 · nama langkah". Terhubung dengan daftar langkah yang bisa diklik (T107).
4. **Form ringan dibaca:** satu kolom di HP, isian dikelompokkan dalam kartu berjudul; isian opsional ditandai "(opsional)" di label, menggantikan badge "Wajib" di setiap field.
5. **Tombol aksi menempel di bawah layar HP** ("← Kembali" dan "Simpan dan lanjut →").
6. **"Periksa dan kirim" sebagai ringkasan:** kartu per langkah berisi data utama (nama, tanggal lahir, alamat, nama ibu, dokumen dengan pratinjau) dan tombol "Ubah"; kotak centang "Saya menyatakan data di atas benar"; tombol besar "Kirim pendaftaran". Kekurangan tampil sesuai T107.
7. **Halaman selesai:** ikon centang besar, nomor pendaftaran besar dengan tombol salin, tombol unduh bukti pendaftaran, timeline langkah berikutnya (diperiksa sekolah → tes jika ada → pengumuman) dan tautan cek status.
8. **Halaman cek status** memakai gaya yang sama: kartu status dengan warna sesuai status dan timeline riwayat yang ramah orang tua.
9. Ikuti warna dan tipografi `doc/brand-identity.md`; pakai komponen CSS yang ada, tambahkan komponen baru di `web/tailwind/app.css` bila perlu (lalu `make css`).
10. Buat mockup dulu (halaman awal, satu langkah form, periksa dan kirim, selesai) untuk disetujui sebelum implementasi.

## File yang disentuh
- `web/templates/admission/public_*.html`, `web/templates/partials/admission.html`
- `web/tailwind/app.css`, `web/static/css/app.css`
- Pengaturan logo sekolah (handler dan template pengaturan; storage)

## Kriteria selesai
- [ ] Mockup disetujui sebelum implementasi
- [ ] Halaman awal, langkah form, periksa dan kirim, selesai, dan cek status memakai desain baru
- [ ] Nyaman dipakai di HP (lebar 360 px): satu kolom, tombol aksi menempel di bawah, tanpa gulir horizontal
- [ ] Logo sekolah tampil jika diunggah; tanpa logo tetap rapi
- [ ] `make css-check` lulus; uji handler halaman publik tetap lulus
