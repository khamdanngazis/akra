# T134 — Pengawasan langsung

**Fase:** 16 Pelaksanaan
**Dependensi:** T128, T131, T133
**Estimasi:** 2 hari

## Tujuan
Pengawas di ruang lab atau kelas tahu kondisi semua peserta dan bisa menangani masalah tanpa menelepon TU.

## Langkah
1. Halaman `GET /ujian/sesi/{id}/awas`, bisa dibuka guru pengawas sesi itu, admin_tu, dan kepala_sekolah:
   - token sesi saat ini, ukuran besar, untuk ditulis di papan;
   - hitung mundur rotasi token.
2. Tabel peserta dengan status:
   - belum login, login, mengerjakan (terjawab X/Y), offline (> 1 menit tanpa sinyal), terkunci, selesai;
   - jumlah kejadian (T133);
   - sisa waktu.
3. Pembaruan tiap 10 detik dengan polling ringan (satu query agregat per sesi), bukan WebSocket, agar tetap sederhana di Railway.
4. Aksi per peserta (tercatat di audit):
   - izinkan pindah perangkat;
   - buka kunci;
   - reset login (kode akses baru);
   - tambah waktu;
   - kumpulkan paksa;
   - (admin_tu) ulang ujian dari awal, dengan alasan wajib.
5. Aksi per sesi: perpanjang waktu semua peserta (mis. listrik padam 10 menit), tutup sesi.
6. Berita acara: jumlah hadir/tidak hadir dan catatan kejadian, dicetak PDF dari halaman ini.

## File yang disentuh
- `internal/exam/proctor.go`, `proctor_test.go`, `proctor_handler.go`, `report_pdf.go`
- `web/templates/exam/proctor.html`

## Kriteria selesai
- [ ] Guru yang bukan pengawas sesi tidak bisa membuka halaman awas (403/404)
- [ ] Status "offline" muncul bila peserta tidak menyimpan > 1 menit
- [ ] Setiap aksi pengawas tercatat di audit log dengan pelaku dan alasan
- [ ] Query polling diuji dengan 500 peserta per sesi < 50 ms
