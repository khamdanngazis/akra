# T160 — Detail dan hasil ujian dalam satu kerangka

**Fase:** 21 Rapikan
**Dependensi:** T157
**Estimasi:** 2 hari

## Latar belakang
Detail ujian dan hasilnya terasa seperti dua aplikasi berbeda:
- **Detail ujian** (`/ujian/paket/{id}`) punya header, langkah berikutnya, dan tab Ringkasan · Soal · Sesi & peserta · Dokumen · Tindak lanjut · Hasil.
- **Tab "Hasil"** justru pindah ke halaman lain (`/hasil`). Halaman itu punya header sendiri (tiga tombol), tab sendiri (Nilai · Analisis butir · Analisis TP & remedial), dan tanpa `<h1>` nama ujian. Breadcrumb-nya melompati daftar ujian.
- **Koreksi esai dan Kirim ke rapor** adalah halaman lain lagi dengan breadcrumb berbeda-beda.
- **Aksi tersebar:**
  - Salin dan Arsipkan di menu "Lainnya";
  - Cetak kartu di tab Dokumen;
  - Unduh Excel dan Cetak analisis di header halaman hasil;
  - Buka hasil untuk siswa di dalam tab Nilai.

## Keputusan
1. **Satu kerangka untuk semua halaman satu ujian:** detail, hasil, analisis, koreksi esai, kirim ke rapor, dan ubah kunci. Kerangkanya terdiri dari:
   - header (nama ujian, tujuan, mapel, status, tombol utama sesuai status, menu "Lainnya");
   - langkah berikutnya;
   - tab utama.
2. **Tab utama:** Ringkasan · Soal · Sesi & peserta · Dokumen · **Hasil** · Tindak lanjut.
   - Tab Hasil punya sub-tab bergaya segmen: Nilai · Analisis butir · Analisis TP · Koreksi esai (dengan jumlah menunggu) · Kirim ke rapor.
   - Breadcrumb: Ujian › Daftar ujian › {ujian} › Hasil › {sub-tab}.
3. **Tombol utama header** (satu saja) mengikuti status:

   | Status | Tombol utama |
   |---|---|
   | Draf | Lanjutkan menyusun |
   | Terjadwal | Cetak kartu |
   | Berlangsung | Pantau sesi |
   | Selesai dengan esai menunggu | Koreksi esai |
   | Selesai, hasil belum dibuka | Buka hasil untuk siswa |
   | Selesai, nilai belum dikirim | Kirim ke rapor |

4. **Menu "Lainnya"** memuat: Salin, Unduh hasil (Excel), Cetak analisis, Naskah dan kunci, Arsipkan.
5. URL lama tetap berlaku (`/hasil`, `/esai`, `/rapor`); hanya kerangka halamannya yang disatukan.

## Langkah
1. **Partial `exam_frame`** (header, langkah berikutnya, tab) dengan view dasar `examFrame`, yang disematkan di view detail, hasil, analisis, esai, rapor, dan kunci.
2. **Sub-tab hasil:** pindahkan tombol Koreksi esai, Unduh Excel, dan Cetak analisis dari header halaman hasil ke sub-tab dan menu "Lainnya".
3. **Fungsi `PrimaryAction(exam, summary)`** di Go dengan uji tabel per status.
4. **Halaman koreksi satu soal** (`/esai/{qid}`) tetap layar kerja penuh tanpa tab. Breadcrumb dan tombol kembali menuju sub-tab Koreksi esai.

## File yang disentuh
- `internal/exam/exam_handler.go`, `result_handler.go`, `essay_handler.go`, `sync_handler.go`, `key_fix` handler
- `web/templates/exam/exam_detail.html`, `results.html`, `item_analysis.html`, `objective_analysis.html`, `essay_queue.html`, `sync_grades.html`, `key_fix.html`, `partials/exam.html`

## Catatan implementasi
- **Kartu "langkah berikutnya" hanya di detail ujian.** Kartu ini butuh cek kesiapan dan daftar sesi; di halaman hasil, esai, dan rapor informasinya sudah diwakili tombol utama dan sub-tab.
- **Tombol utama selesai:** urutannya Koreksi esai → Buka hasil untuk siswa → Kirim ke rapor (belum dikirim atau nilai berubah) → Lihat hasil. Pengguna yang hanya membaca (kepala sekolah) mendapat Lihat jadwal / Pantau sesi / Lihat hasil.
- **"Atur"** tetap sebagai tombol kedua untuk ujian yang masih bisa diubah. Untuk draf, tombol utama "Lanjutkan menyusun" sudah menggantikannya.
- **Judul halaman hasil kini nama ujian** (sebelumnya "Hasil ujian"). Judul koreksi esai, kirim nilai ke rapor, dan betulkan kunci menjadi `<h2>`, sehingga setiap halaman tetap punya satu `<h1>`.
- **Korektor yang hanya ditugaskan** (bukan pemilik ujian) tetap melihat halaman koreksi esai tanpa kerangka, karena tidak boleh membuka tab ujian lainnya.
- **Sub-tab Koreksi esai** menampilkan jumlah peserta yang menunggu koreksi.

## Kriteria selesai
- [x] Semua halaman satu ujian memakai kerangka yang sama: header, tab utama, Hasil aktif saat di hasil, analisis, esai, atau rapor (langkah berikutnya hanya di detail, lihat catatan)
- [x] Uji tabel `PrimaryAction` untuk semua status
- [x] Tidak ada tombol yang hilang; setiap aksi lama tercapai dari header, tab, atau menu "Lainnya" (diuji)
- [x] Tautan dan URL lama tetap 200 (tidak ada route yang berubah; uji registry)
