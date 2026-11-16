# T133 — Wizard buat ujian

**Fase:** 16 Persiapan Ujian
**Dependensi:** T127, T128
**Estimasi:** 4 hari

## Tujuan
Guru membuat ulangan untuk kelasnya, dan TU membuat tes seleksi, lewat satu alur bertahap yang tidak membingungkan. Polanya sama dengan wizard gelombang pendaftaran (T117).

## Langkah wizard
1. **Dasar:**
   - nama;
   - tujuan (`ulangan`, `pts`, `pas`, `seleksi`, `latihan`);
   - mapel dan tingkat (bank seleksi untuk tes seleksi);
   - durasi.

   Pilihan tujuan mengisi bawaan yang masuk akal. Contoh: `seleksi` → hasil tidak ditampilkan, satu kali kerja; `latihan` → hasil dan pembahasan langsung, boleh mengulang.
2. **Soal:** dua cara yang bisa digabung.
   - **Pilih soal:** daftar bank dengan filter dan centang; urutan bisa diseret.
   - **Ambil acak:** aturan "10 PG mudah TP 3.1 + 5 PG sedang + 2 esai". Ditampilkan berapa soal tersedia per aturan.
   - **Bagian** (mis. TPA, Bahasa Inggris), masing-masing dengan urutan dan instruksi sendiri.
   - **Varian paket A/B/C:** set soal berbeda dengan komposisi sama, dibagikan otomatis per peserta. Ini perlindungan utama di lab.
   - Panel ringkas yang selalu terlihat: jumlah soal, total poin, perkiraan waktu, sebaran TP/level/kesulitan, jumlah esai (perlu koreksi manual).
3. **Aturan:**
   - acak soal / acak opsi;
   - hasil untuk peserta (`tidak`, `nilai`, `nilai_dan_pembahasan`, dibuka setelah sesi terakhir);
   - jumlah percobaan;
   - KKTP/nilai minimal;
   - aturan integritas (T141): hanya catat, atau kunci setelah N kali keluar;
   - tombol kembali ke soal sebelumnya boleh/tidak.
4. **Jadwal & peserta:**
   - **Ulangan guru:** pilih kelas yang diampu dan waktu; satu sesi per kelas dibuat otomatis, dengan lokasi `di_ruang` (bawaan) atau `jarak_jauh`.
   - **Tes seleksi:** pilih gelombang dan jalur; peserta otomatis dari pendaftar terverifikasi (T149).
   - **PTS/PAS:** tidak dijadwalkan di sini, melainkan lewat agenda (T134).
5. **Cek kesiapan** (T136), lalu tombol **Jadwalkan**.

## Detail ujian setelah dibuat
- Tab: Ringkasan · Soal · Sesi & peserta · Hasil.
- Kartu "langkah berikutnya" (pola T116), misalnya "Cetak kartu peserta" → "Ujian berlangsung" → "12 esai menunggu koreksi" → "Kirim ke rapor".
- **Kunci ujian:** terjadi saat sesi pertama mulai. Snapshot soal (`exam_item_snapshots`) dibuat, dan soal di bank boleh berubah tanpa memengaruhi ujian ini.
- **Salin ujian** untuk kelas lain atau tahun depan.

## Model data
- `exams`: dasar dan aturan; `status` (`draf`, `terjadwal`, `berlangsung`, `selesai`, `arsip`); `locked_at`.
- `exam_sections`, `exam_items` (soal tetap / aturan acak), `exam_variants`, `exam_item_snapshots`.

## Stimulus bersama
1. Di langkah Soal, memilih stimulus menambahkan **seluruh kelompoknya**. Guru bisa melepas soal tertentu dari kelompok untuk ujian ini.
2. **Acak soal** memperlakukan kelompok stimulus sebagai satu blok: blok diacak di antara soal lain, dan soal di dalam blok tetap berurutan (bawaan) atau diacak di dalam blok (pilihan).
3. Aturan acak dari bank bisa menyebut stimulus, mis. "2 teks bacaan sedang beserta soalnya".
4. Panel ringkas menghitung soal di dalam stimulus. Varian A/B bisa memakai stimulus berbeda dengan komposisi sama.

## File yang disentuh
- `db/migrations/0000XX_exams.up.sql` dan `.down.sql`
- `internal/exam/exam.go`, `assemble.go` (acak dan varian), `wizard.go`, tes, `exam_handler.go`
- `web/templates/exam/wizard.html`, `exam_detail.html`, partial tab dan langkah berikutnya

## Kriteria selesai
- [ ] Guru membuat ulangan 20 soal dari bank dalam ≤ 5 menit (uji pengguna)
- [ ] Aturan acak yang melebihi isi bank ditolak di langkah soal, dengan jumlah tersedia ditampilkan
- [ ] Varian A/B punya komposisi TP/level/kesulitan sama (uji unit dengan seed)
- [ ] Setelah terkunci, menyunting soal di bank tidak mengubah snapshot
- [ ] Guru tidak bisa memilih kelas yang tidak ia ampu
- [ ] Acak soal tidak pernah memisahkan soal dari stimulusnya atau mengacak urutan di dalam blok bila tidak diizinkan
