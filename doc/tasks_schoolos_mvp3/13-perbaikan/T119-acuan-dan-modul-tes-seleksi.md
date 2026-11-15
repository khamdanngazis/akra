# T119 — Acuan hasil tes seleksi dan pertimbangan modul tes

**Fase:** 13 Perbaikan
**Dependensi:** T93a, T118
**Estimasi:** tahap 1: 1 hari; tahap 2 (tes online): 5–8 hari, belum diputuskan
**Status:** catatan dan analisa, belum dikerjakan

## Masukan pengguna (2026-11-15)
Di jalur tes seleksi, TU tidak punya acuan hasil tes. Perlu dipertimbangkan pembuatan modul tes.

## Analisa kondisi sekarang
- **Yang sudah ada (T93a):**
  - Daftar tes per periode (nama, bobot %, nilai maksimal, jadwal, lokasi).
  - Grid isi nilai per tes.
  - Skor akhir = Σ (nilai ÷ maks × bobot), dipakai di peringkat.
- **Kenapa terasa "tidak ada acuan":**
  - Tes dilaksanakan **di luar aplikasi** (kertas, wawancara, baca Al-Qur'an, dsb.). Aplikasi hanya menerima angka akhir.
  - Tidak ada **rubrik/kriteria** per tes, misalnya "Membaca: 0–100; lancar ≥ 80".
  - Tidak ada **nilai minimal lulus**, sehingga pendaftar dengan nilai sangat rendah tetap diusulkan diterima jika kuota masih ada.
  - Tidak ada **lembar penilaian** yang bisa dicetak untuk penguji.
  - Tidak ada **kartu tes** (nomor peserta, jadwal, ruang) untuk calon siswa.
  - Halaman tes hanya bisa dicapai lewat tombol kecil "Tes seleksi" di detail periode, dan nilai tidak tampil di detail pendaftar.

## Usulan bertahap
**Tahap 1: acuan dan pelaksanaan tes offline** (disarankan sekarang)
1. Per tes:
   - **deskripsi/rubrik** (teks bebas, mis. aspek yang dinilai dan rentang nilai);
   - **nilai minimal lulus** (opsional).
2. **Lembar penilaian PDF** per tes: daftar peserta dengan kolom nilai dan rubrik, untuk penguji.
3. **Kartu tes** di halaman status pendaftaran: jadwal, lokasi, dan nomor pendaftaran.
4. **Nilai di detail pendaftar:** nilai per tes, skor akhir, lulus/tidak.
5. **Peringkat** menandai yang di bawah nilai minimal dan tidak mengusulkannya diterima.

**Tahap 2: tes online (CBT)**, hanya jika banyak sekolah membutuhkannya
- Bank soal pilihan ganda per tes, jadwal/durasi, calon siswa mengerjakan di halaman status, nilai otomatis masuk grid.
- Risikonya besar:
  - kecurangan (tes dari rumah);
  - perangkat orang tua;
  - beban server saat tes serentak;
  - soal esai tetap perlu dinilai manual.

  Untuk SD, tes biasanya berupa observasi atau wawancara, jadi CBT kurang cocok.

## Perlu keputusan
- Setuju tahap 1 dulu dan tahap 2 ditunda sampai ada permintaan sekolah?
- Apakah tes untuk sekolah Anda lebih banyak **tertulis** (cocok CBT) atau **wawancara/praktik** (cukup tahap 1)?

## Kriteria selesai (tahap 1)
- [ ] Setiap tes bisa diberi rubrik dan nilai minimal
- [ ] Lembar penilaian dan kartu tes bisa dicetak
- [ ] Nilai dan status lulus terlihat di detail pendaftar dan di peringkat
