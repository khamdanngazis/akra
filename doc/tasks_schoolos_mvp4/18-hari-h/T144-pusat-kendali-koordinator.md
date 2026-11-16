# T144 — Pusat kendali koordinator

**Fase:** 18 Hari-H
**Dependensi:** T134, T143
**Estimasi:** 3 hari

## Tujuan
Koordinator ujian dan kepala sekolah melihat kondisi **semua ruang** saat PTS/PAS berlangsung, dan bisa bertindak cepat bila ada gangguan.

## Langkah
1. Halaman `/ujian/agenda/{id}/kendali` untuk hari ini:
   - **kartu per ruang/sesi:** pengawas (sudah membuka layar atau belum), hadir/total, offline, perlu perhatian, sisa waktu;
   - warna kartu: hijau normal, kuning ada masalah, merah ruang tanpa pengawas atau > 20% offline.
2. **Indikator kesehatan sistem:** latensi simpan jawaban dan error rate untuk sekolah ini (dari T145). Koordinator langsung tahu apakah masalahnya di jaringan sekolah atau di server.
3. **Aksi lintas ruang:**
   - jeda atau tambah waktu untuk beberapa ruang (mis. listrik padam di satu gedung);
   - pindahkan peserta ke ruang/sesi lain (komputer rusak);
   - ganti pengawas.
4. **Pesan ke layar peserta:** pesan singkat ke satu ruang, satu sesi, atau semua, mis. "Soal no. 12 opsi C dibaca 'hasil'". Muncul sebagai banner di halaman ujian lewat respons simpan atau ping. Tercatat di berita acara.
5. **Ralat soal saat ujian berlangsung:**
   - koordinator atau pembuat soal menandai soal "dibatalkan" (poin dikeluarkan dari total) atau mengirim pesan ralat;
   - kunci dibetulkan lewat nilai ulang setelah ujian (T146).
6. **Rekap harian:** kehadiran per mapel/tingkat, ujian yang belum selesai dikoreksi, berita acara yang belum ditandatangani.

## File yang disentuh
- `internal/exam/control.go`, tes, `control_handler.go`, `broadcast.go`
- `web/templates/exam/control.html`

## Kriteria selesai
- [ ] Kartu ruang mencerminkan status ≤ 15 detik
- [ ] Pesan ke peserta muncul di layar peserta ≤ 15 detik dan tercatat di berita acara
- [ ] Memindahkan peserta mempertahankan jawaban dan sisa waktunya
- [ ] Hanya koordinator, admin_tu (aksi), dan kepala (baca) yang bisa membuka
