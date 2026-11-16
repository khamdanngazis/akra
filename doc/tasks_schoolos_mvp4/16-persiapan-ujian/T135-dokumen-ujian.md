# T135 — Dokumen ujian

**Fase:** 16 Persiapan Ujian
**Dependensi:** T124, T133 (dan T134 untuk dokumen agenda)
**Estimasi:** 2,5 hari

## Tujuan
Semua dokumen yang biasa dibuat panitia keluar dari AKRA dengan sekali klik, dalam format siap cetak.

## Dokumen
| Dokumen | Isi | Format |
|---|---|---|
| **Kartu peserta** | Nama, NIS/no. pendaftaran, nomor peserta, ruang, sesi, jadwal, **kode akses**, QR login. 8–10 per A4, ada garis potong | PDF (fpdf) |
| **Kartu tes pendaftar** | Seperti di atas, di halaman status pendaftaran dan bukti pendaftaran (T95, T119) | Halaman + PDF |
| **Daftar hadir** | Per ruang per sesi, kolom tanda tangan | PDF |
| **Denah tempat duduk** | Per ruang, posisi peserta; siswa sekelas berselang bila diatur | PDF |
| **Tempelan pintu ruang** | Ruang, sesi, rentang nomor peserta | PDF |
| **Berita acara** | Isi otomatis dari data pelaksanaan (T143): hadir/tidak hadir, kejadian, catatan pengawas | PDF |
| **Naskah cadangan** | Soal per varian tanpa kunci, untuk keadaan darurat; lembar jawaban terpisah | Halaman cetak (HTML print, memuat rumus) |
| **Kunci jawaban** | Hanya untuk pengelola ujian; tercatat di audit | Halaman cetak |

## Langkah
1. Semua dokumen memakai kop sekolah dari pengaturan rapor dan zona waktu sekolah (T126).
2. **Kode akses tampil di kartu:**
   - dibuat saat kartu pertama kali dicetak dan disimpan sebagai hash (T124);
   - mencetak ulang membuat kode baru dan membatalkan yang lama, dengan konfirmasi bahwa kartu lama tidak berlaku.
3. Cetak per sesi, per ruang, per kelas, atau semua; nama file yang jelas.
4. Kartu tersedia **paling cepat H-3** sebelum sesi, agar kode tidak beredar terlalu lama.

## Stimulus dan tulisan Arab di cetak
- Naskah cadangan dan kartu soal mencetak stimulus satu kali di atas kelompok soalnya; kelompok diusahakan tidak terpotong halaman (CSS `break-inside`).
- Teks Arab dicetak dengan font dan arah tulisan yang benar.

## File yang disentuh
- `internal/exam/docs_pdf.go`, `card_pdf.go`, `seating.go`, tes
- `web/templates/exam/paper_print.html`, `key_print.html`

## Kriteria selesai
- [ ] Kartu terpindai QR dan login berhasil di HP
- [ ] Cetak ulang kartu membatalkan kode lama
- [ ] Naskah cadangan tidak memuat kunci; rumus tampil benar saat dicetak dari Chrome
- [ ] Mencetak kunci tercatat di audit
- [ ] Naskah cetak berisi stimulus dan teks Arab tampil benar dari Chrome desktop
