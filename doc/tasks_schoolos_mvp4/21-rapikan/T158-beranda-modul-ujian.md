# T158 — Beranda modul ujian: "Hari ini" dan "Perlu tindakan"

**Fase:** 21 Rapikan
**Dependensi:** T157
**Estimasi:** 1,5 hari

## Latar belakang
Beranda modul (`/ujian`) hanya berisi lima kartu dengan tombol "Buka":
- Kartu **Tes seleksi** dan **Ulangan kelas** menuju halaman yang sama (`/ujian/paket/`).
- Kartu **Ujian latihan** tumpang tindih dengan jenis ujian "Ujian latihan" di daftar ujian.
- Setelah T157, semua kartu ini sudah tersedia di sub-navigasi, sehingga beranda tidak memberi informasi baru.
- Guru tidak melihat apa yang perlu dikerjakan hari ini.

## Keputusan
Beranda modul menjadi ringkasan kerja pengguna, dengan tiga bagian.

1. **Hari ini:**
   - sesi yang diawasi pengguna hari ini, dengan tombol Layar pengawas;
   - untuk koordinator dan admin TU: agenda yang berjalan hari ini, dengan tombol Pusat kendali.
2. **Perlu tindakan**, sesuai hak akses, maksimal 8 baris dan diurutkan dari yang paling mendesak:

   | Hal | Tautan |
   |---|---|
   | Ujian draf yang belum dijadwalkan | wizard langkah terakhir |
   | Jawaban esai menunggu koreksi | Koreksi esai |
   | Ujian selesai yang hasilnya belum dibuka untuk siswa | Hasil |
   | Nilai belum atau perlu dikirim ulang ke rapor | Kirim ke rapor |
   | Soal menunggu telaah (penelaah) | Telaah |
   | Agenda: kesiapan yang masih merah (koordinator) | Agenda |

   Bila semua beres: "Tidak ada yang perlu ditindaklanjuti."
3. **Mulai cepat:** Buat ujian, Tambah soal / Impor soal, Buka ujian latihan untuk kelas (lihat penamaan di T159). Ditambah tautan "Butuh bantuan?" yang sudah ada.

## Langkah
1. `internal/exam/home.go`: query ringkasan per pengguna, maksimal satu query per bagian, dibatasi ke ujian yang boleh dilihat `Actor`.
2. Template `exam/home.html` ditulis ulang. Kartu bagian lama dihapus.
3. **Empty state** untuk sekolah baru: tampilkan langkah mulai (bank contoh, ujian latihan, buat ujian), dengan pola T154.

## File yang disentuh
- `internal/exam/handler.go` (home), `internal/exam/home.go` (baru), `home_test.go`
- `web/templates/exam/home.html`

## Catatan implementasi
- **"Perlu tindakan" hanya untuk ujian milik pengguna** (pemilik ujian), ditambah esai yang ditugaskan kepadanya dan, untuk pengelola agenda, agenda dekat.
- **Ujian yang sesi terakhirnya berakhir lebih dari 60 hari lalu** tidak lagi diingatkan (hasil belum dibuka, nilai belum dikirim).
- **"Kesiapan agenda merah"** dipersempit menjadi dua hal yang bisa dihitung murah: jadwal belum diumumkan, dan sesi tanpa pengawas, untuk agenda yang berjalan atau mulai dalam 14 hari.
- **"Kirim ulang ke rapor"** memakai `GradesStale` yang sama dengan detail ujian, hanya untuk ujian yang dikirim dalam 60 hari terakhir.
- **Mulai cepat** ada di header beranda modul: Buat ujian, Tambah atau impor soal, Buka ujian latihan. Kartu bagian lama dihapus; tujuannya kini ada di sub-navigasi (T157).
- **Jumlah query tidak dibatasi 4.** Beranda memakai sekitar 8 query ringan: sesi pengawas, agenda hari ini, pengaturan telaah, satu query gabungan untuk draf/hasil/rapor, esai, telaah, agenda, dan cek sekolah baru. Ditambah `GradesStale` per ujian yang baru dikirim. Semua dibatasi per pengguna dan rentang waktu, jadi tidak tumbuh dengan jumlah ujian sekolah.

## Kriteria selesai
- [x] Uji per peran: guru melihat draf miliknya dan tombol Buat ujian; kepala sekolah hanya melihat (tanpa tombol ubah dan tanpa draf guru lain); pengelola agenda melihat agenda hari ini dan "jadwal belum diumumkan" paling atas
- [x] Setiap baris "Perlu tindakan" menuju halaman yang tepat dan hilang setelah ditindaklanjuti (diuji untuk hasil dibuka dan batas 60 hari)
- [ ] Beranda tidak menambah lebih dari 4 query: tidak dipenuhi, lihat catatan
- [x] Tampilan desktop diperiksa; HP perlu dicek sekali di perangkat sungguhan
