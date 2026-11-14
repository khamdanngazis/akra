# T107 — Halaman "Periksa dan kirim" yang menunjukkan apa yang harus dilengkapi

**Fase:** 13 Perbaikan
**Dependensi:** T90, T91, T92
**Estimasi:** 1 hari

## Latar belakang
Di langkah terakhir formulir pendaftaran, pesan kekurangan membingungkan orang tua:

> Masih ada isian wajib yang kosong: Kartu Keluarga, Akta kelahiran, Pas foto.
> Lengkapi lewat langkah sebelumnya, lalu kembali ke halaman ini.

Masalahnya:
- Yang kurang adalah dokumen, tetapi judulnya "isian wajib".
- Tidak disebutkan langkah mana yang harus dibuka, dan tidak ada tautan langsung.
- Orang tua baru tahu ada kekurangan di akhir.

## Langkah
1. Kelompokkan kekurangan per langkah formulir (Data calon siswa, Alamat dan keluarga, Orang tua/wali, Sekolah asal dan kontak, Dokumen). Setiap kelompok: judul langkah, daftar yang kurang dengan bahasa yang tepat ("belum diisi" untuk isian, "belum diunggah" untuk dokumen), dan tombol langsung ke langkah itu.
   - Pemetaan isian wajib ke langkah dari `students.ProfileField.Section` dan langkah orang tua untuk nama ibu kandung; dokumen ke langkah Dokumen.
2. Tampilkan daftar periksa semua langkah dengan tanda lengkap (✓) atau belum (✗).
3. Tombol menuju langkah membawa penanda field yang kurang (mis. `#nik` atau `?kurang=nik,religion`); halaman langkah menggulir ke field pertama yang kurang dan menyorotnya.
4. Di halaman "Periksa dan kirim", dokumen wajib yang belum ada bisa langsung diunggah (form unggah yang sama dengan langkah Dokumen), lalu kembali ke halaman ini.
5. Penanda langkah ("Langkah N dari 6") menampilkan status tiap langkah, agar kekurangan terlihat sebelum langkah terakhir.
6. Berlaku juga untuk input oleh TU (T92), yang memakai halaman yang sama.

## File yang disentuh
- `internal/admission/public_handler.go` (`buildStepView`: kekurangan per langkah)
- `internal/admission/applicant.go` (`Missing` dengan informasi langkah)
- `web/templates/admission/public_step.html`, `web/templates/partials/admission.html`

## Kriteria selesai
- [ ] Setiap kekurangan menyebut langkahnya dan punya tombol langsung ke langkah itu
- [ ] Dokumen disebut "belum diunggah", isian "belum diisi"
- [ ] Dokumen wajib bisa diunggah langsung dari halaman "Periksa dan kirim"
- [ ] Field yang kurang tersorot saat langkahnya dibuka dari tombol
- [ ] Uji handler untuk formulir publik dan input TU
