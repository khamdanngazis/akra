# T140 — Timer server, kumpul otomatis, dan jeda

**Fase:** 17 Pelaksanaan
**Dependensi:** T139
**Estimasi:** 2 hari

## Tujuan
Waktu ujian adil, tidak bisa diakali, dan tetap benar saat ada gangguan seperti listrik padam atau server sempat lambat.

## Langkah
1. `deadline_at = min(started_at + durasi + extra_minutes + jeda_total, batas_sesi)`.
   - Aturan terlambat masuk diatur per sesi: durasi penuh, atau dipotong sesuai keterlambatan.
2. Klien menghitung mundur dengan **offset jam server** yang diperbarui di setiap respons simpan. Mengubah jam HP tidak berpengaruh.
3. Peringatan pada sisa 10 menit dan 1 menit (banner dan getar di HP).
4. Waktu habis: klien mengirim antrian lalu kumpul otomatis, dan menampilkan "Waktu habis — jawaban dikumpulkan".
5. **Job River tiap menit:** mengumpulkan attempt yang lewat `deadline_at` + tenggang dan belum dikumpulkan (HP mati, browser ditutup). Jawaban yang tersimpan tetap dinilai.
6. **Jeda** (oleh pengawas atau koordinator, T143/T144):
   - pause per peserta atau per sesi (mis. listrik padam);
   - selama jeda, layar peserta terkunci dengan pesan;
   - durasi jeda ditambahkan ke `deadline_at`.
7. **Tambah waktu** per peserta atau per sesi langsung terlihat di perangkat pada respons simpan berikutnya (≤ 10 detik; klien melakukan ping ringan tiap 15 detik bila tidak ada perubahan).

## File yang disentuh
- `internal/exam/timer.go`, `timer_test.go`, `jobs.go`
- `web/static/js/exam-take.js`

## Kriteria selesai
- [ ] Jam perangkat dimajukan 1 jam: sisa waktu tetap benar (uji Playwright dengan jam palsu)
- [ ] Attempt yang ditinggal dikumpulkan job dan dinilai
- [ ] Jeda 10 menit memperpanjang tenggat tepat 10 menit
- [ ] Rumus tenggat (terlambat, waktu tambahan, batas sesi, jeda) diuji tabel
