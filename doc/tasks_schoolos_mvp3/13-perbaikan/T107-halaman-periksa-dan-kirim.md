# T107 — Navigasi langkah dan halaman "Periksa dan kirim" yang jelas

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
7. Navigasi bolak-balik antar langkah (saat ini hanya bisa maju lewat "Simpan dan lanjut"):
   - Tombol **"← Kembali"** di setiap langkah kecuali langkah pertama. Isian di halaman ikut disimpan sebelum kembali; jika ada isian dengan format salah, halaman tetap terbuka dengan tanda di field tersebut dan pilihan "Kembali tanpa menyimpan halaman ini".
   - Penunjuk langkah diganti daftar langkah yang bisa diklik (1 Data calon siswa · 2 Alamat · 3 Orang tua · 4 Sekolah asal · 5 Dokumen · 6 Kirim), dengan tanda ✓ untuk langkah yang lengkap.
   - Tombol **"Simpan draf"** di setiap langkah untuk berhenti dan melanjutkan nanti dengan kode lanjut.

## File yang disentuh
- `internal/admission/public_handler.go` (`buildStepView`: kekurangan per langkah)
- `internal/admission/applicant.go` (`Missing` dengan informasi langkah)
- `web/templates/admission/public_step.html`, `web/templates/partials/admission.html`

## Kriteria selesai
- [x] Setiap kekurangan menyebut langkahnya dan punya tombol langsung ke langkah itu
- [x] Dokumen disebut "belum diunggah", isian "belum diisi"
- [x] Dokumen wajib bisa diunggah langsung dari halaman "Periksa dan kirim"
- [x] Field yang kurang tersorot saat langkahnya dibuka dari tombol
- [x] Setiap langkah punya tombol Kembali yang menyimpan isian; daftar langkah bisa diklik
- [x] Simpan draf tersedia di setiap langkah
- [x] Uji handler untuk formulir publik dan input TU

## Catatan implementasi
- Sebagian sudah dikerjakan di T111: kekurangan per langkah dengan "belum diisi"/"belum diunggah", status ✓/kurang di penunjuk langkah, tombol Kembali dan daftar langkah yang bisa diklik (keduanya menyimpan isian dulu).
- **Sorot isian:** tombol "Lengkapi" membuka langkah dengan `?kurang=nik,religion,...`; isian itu ditandai "Wajib diisi agar pendaftaran bisa dikirim", halaman bergulir ke isian pertama dan memberi fokus. Nama ibu (`mother_name`) dipetakan ke `ibu.full_name`.
- **Unggah dari halaman periksa:** dokumen wajib yang belum ada punya form unggah langsung (`/dokumen/{jenis}?kembali=kirim`), setelah berhasil kembali ke halaman periksa.
- **Kembali tanpa menyimpan:** jika Kembali atau lompat langkah gagal karena format isian salah, halaman tetap terbuka dengan tanda error dan tautan "Kembali tanpa menyimpan halaman ini" (di atas dan di bar bawah).
- **Simpan draf** (`go=draf`): menyimpan lalu tetap di langkah yang sama dengan pesan cara melanjutkan (nomor HP + kode lanjut). Untuk TU pesannya mengarah ke detail pendaftar.
- Semua berlaku juga untuk input oleh TU.

