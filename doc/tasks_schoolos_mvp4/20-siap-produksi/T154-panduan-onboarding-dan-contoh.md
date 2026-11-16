# T154 — Panduan, onboarding, dan konten contoh

**Fase:** 20 Siap Produksi
**Dependensi:** fitur rilis terkait
**Estimasi:** 3 hari

## Tujuan
Guru, pengawas, koordinator, dan siswa bisa memakai modul ujian tanpa pelatihan panjang. Keberhasilan adopsi sama pentingnya dengan fitur.

## Langkah
1. **Konten contoh untuk setiap sekolah baru:**
   - bank soal contoh per jenjang (SD/SMP/SMA), semua jenis soal termasuk rumus dan gambar, ditandai "Contoh";
   - ujian latihan siap pakai (T136);
   - template Word dan Excel resmi (T129, T131).
2. **Panduan dalam aplikasi:**
   - halaman kosong berisi langkah dan tombol aksi;
   - tooltip "Apa ini?" pada istilah (varian, KKTP, daya beda, lokasi di ruang/jarak jauh);
   - tur singkat yang bisa dilewati saat pertama kali membuka bank soal, wizard, dan layar pengawas.
3. **Panduan cetak 1 halaman** (PDF, bahasa sederhana, bergambar):
   - **Pengawas:** sebelum, saat, dan sesudah sesi; arti warna; cara reset dan izin pindah perangkat; keterbatasan bunyi.
   - **Siswa/peserta:** login, ragu-ragu, offline, kumpul. Bisa dicetak di balik kartu peserta.
   - **Orang tua calon siswa:** cek perangkat, login tes dari rumah.
4. **Panduan lengkap** di `doc/panduan-ujian/` untuk guru dan koordinator, dengan tangkapan layar terbaru dan video pendek opsional (≤ 2 menit per topik).
5. **Pusat bantuan saat ujian:** tautan "Butuh bantuan?" di layar pengawas menuju langkah pemecahan masalah umum dan kontak dukungan AKRA (jam layanan diperluas di musim PAS).

## Kriteria selesai
- [ ] Sekolah baru langsung memiliki bank contoh dan ujian latihan
- [ ] Panduan pengawas dan siswa 1 halaman tersedia dan diuji dengan 3 guru serta 5 siswa (dipahami tanpa penjelasan tambahan)
- [ ] Semua halaman kosong modul ujian punya panduan dan aksi
