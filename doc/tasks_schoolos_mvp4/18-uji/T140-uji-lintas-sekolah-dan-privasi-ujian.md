# T140 — Uji lintas sekolah, akses peserta, dan kebocoran soal

**Fase:** 18 Uji
**Dependensi:** semua task 14–17
**Estimasi:** 1,5 hari

## Tujuan
Memastikan ujian tidak membuka data sekolah lain, peserta tidak bisa melihat milik peserta lain, dan kunci jawaban tidak bocor.

## Langkah
1. Semua route `/ujian/...` dan `/u/...` masuk registry `internal/testutil/routes.go` dengan jenis yang benar (KindByID, KindFormRef, dsb.). Uji lintas sekolah T58 lulus.
2. Uji akses peserta:
   - session peserta × semua route staf → ditolak;
   - peserta A × attempt/jawaban peserta B → 404;
   - peserta sesi 1 × sesi 2 → ditolak.
3. Uji kebocoran kunci: setiap respons route peserta (HTML dan JSON) dipindai, dan tidak boleh memuat nilai dari `answer_key` atau `explanation` sebelum waktunya.
4. Uji peran: guru non-pengawas, guru mapel lain, bendahara, orang tua (T59) terhadap bank soal, paket, hasil, dan halaman awas.
5. Uji rate limit login peserta dan simpan jawaban.

## Kriteria selesai
- [ ] Semua uji di atas otomatis dan lulus di CI
- [ ] Pemindai kebocoran kunci mencakup semua jenis soal
