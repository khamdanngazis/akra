# T155 — Pilot dan rollout bertahap

**Fase:** 20 Siap Produksi
**Dependensi:** T152–T154 untuk setiap rilis
**Estimasi:** 2–4 minggu per rilis (berjalan paralel dengan pengembangan rilis berikutnya)

## Tujuan
Modul ujian dipakai sungguhan di sekolah, masalahnya ditemukan dan diperbaiki, lalu dibuka bertahap ke semua sekolah dengan risiko terkendali.

## Langkah
1. **Pilot per rilis**, diaktifkan per sekolah (T125):
   - **R1:** 1–2 sekolah menjalankan tes seleksi online (satu sesi di sekolah, satu dari rumah).
   - **R2:** 3–5 guru dari sekolah pilot menjalankan ulangan harian dengan HP siswa, termasuk satu yang diimpor dari Word.
   - **R3:** satu sekolah menjalankan PTS/PAS penuh (lab dan HP), dengan koordinator memakai agenda dan pusat kendali.
2. **Pendampingan:**
   - tim AKRA hadir (langsung atau daring) di ujian pertama setiap sekolah pilot;
   - gladi pengawas H-2 (T136);
   - cek perangkat siswa H-7.
3. **Data yang dikumpulkan:** ukuran keberhasilan di indeks bagian 9, tiket bantuan, log kejadian, dan wawancara singkat (guru, pengawas, koordinator, 5 siswa).
4. **Perbandingan nilai:** 10 lembar acak dikoreksi manual dan dicocokkan dengan nilai otomatis.
5. **Kriteria lanjut ke rollout:**
   - 0 jawaban hilang;
   - tidak ada insiden kategori tinggi yang terbuka;
   - ≥ 95% attempt tanpa bantuan;
   - guru pilot mau memakai lagi.
6. **Rollout:**
   - dibuka bertahap: 5 → 20 → semua sekolah;
   - pengumuman dan webinar singkat;
   - musim PAS pertama dipantau khusus (jam dukungan diperluas, tim siaga).
7. Masalah dari pilot dicatat sebagai task di folder `22-perbaikan/` (pola `13-perbaikan/` MVP 3).

## Kriteria selesai
- [ ] Pilot setiap rilis selesai dengan laporan di `doc/uji-mvp4.md`
- [ ] Kriteria lanjut terpenuhi sebelum rollout
- [ ] Task perbaikan dari pilot tercatat dan diprioritaskan
