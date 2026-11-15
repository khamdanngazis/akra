# T116 — Tindak lanjut pendaftar lebih menonjol (terutama tablet dan HP)

**Fase:** 13 Perbaikan
**Dependensi:** T93, T114
**Estimasi:** 0,5–1 hari
**Status:** catatan dan analisa, belum dikerjakan

## Masukan pengguna (2026-11-15)
Bagian tindak lanjut di detail pendaftar kurang menonjol. Pengguna sempat mencari cara menerima siswa di menu lain, terutama di tablet dan HP.

## Analisa kondisi sekarang
- **Letak di layar kecil:** di `applicant_detail.html`, tindak lanjut ada di kolom kanan (`.detail-side`). Di bawah lebar `lg` (1024 px), kolom kanan turun ke **bawah** seluruh accordion data pendaftar. Di tablet dan HP, tombol Verifikasi/Terima baru terlihat setelah menggulir semua data.
- **Bentuknya seperti form biasa:** kartu berjudul "Tindak lanjut" berisi textarea catatan lebih dulu, lalu tombol kecil (`btn-sm`). Tidak ada penjelasan langkah apa yang sedang ditunggu.
- **Tombol tidak selalu ada:**
  - Tombol hanya muncul jika status punya langkah berikutnya (`staffTransitions`).
  - "Diterima" hanya bisa dari status Diverifikasi atau Cadangan. Untuk pendaftar berstatus Dikirim, tombol Terima tidak ada sama sekali, jadi pengguna mencari di tempat lain.
  - Tidak ada petunjuk bahwa urutannya Dikirim → Diverifikasi → Diterima.
- **Dua tempat menerima siswa:** detail pendaftar (satu per satu) dan **Peringkat dan hasil** di halaman periode (massal). Keduanya tidak saling menunjuk.
- **Setelah Diterima,** langkah berikutnya ("Jadikan siswa") ada di halaman lain dan tidak ditautkan dari detail (lihat T120).

## Usulan
1. **Kartu "Langkah berikutnya" di bagian atas halaman,** tepat di bawah judul, di semua ukuran layar:
   - Menampilkan status sekarang, langkah yang ditunggu, dan tombol utama berukuran normal. Contoh: "Dikirim → periksa data dan dokumen, lalu **Verifikasi**".
   - Catatan perbaikan hanya muncul saat memilih "Perlu perbaikan" atau "Tolak", tidak selalu tampil.
2. **Garis alur status** (Dikirim → Diverifikasi → Diterima → Jadi siswa) dengan posisi pendaftar sekarang.
3. **Petunjuk jika langkah tidak bisa dilakukan,** misalnya "Belum bisa diterima: verifikasi dulu" atau "Biaya formulir wajib lunas sebelum verifikasi".
4. **Bar aksi menempel di bawah layar** di HP dan tablet, berisi tombol utama, seperti bar lembar salin T114.
5. **Penghubung ke halaman lain:**
   - Untuk jalur dengan tes atau kuota: tautan "Lihat peringkat jalur ini".
   - Setelah Diterima: tombol "Jadikan siswa" (T120).

## Perlu keputusan
- Bolehkah TU langsung **menerima dari status Dikirim** (verifikasi dan terima sekaligus) untuk jalur tanpa tes dan tanpa kuota? Ini memangkas satu langkah untuk sekolah kecil.

## Kriteria selesai
- [ ] Di 390 px dan 768 px, tombol tindak lanjut terlihat tanpa menggulir
- [ ] Setiap status menampilkan langkah berikutnya, atau alasan jika belum bisa
- [ ] Ada tautan ke peringkat jalur dan ke Jadikan siswa sesuai status
