# T146 — Penilaian otomatis dan nilai ulang

**Fase:** 19 Penilaian
**Dependensi:** T140
**Estimasi:** 2 hari

## Tujuan
Soal objektif dinilai seketika saat dikumpulkan. Kesalahan kunci bisa diperbaiki untuk semua peserta dengan satu tindakan yang tercatat.

## Langkah
1. Saat attempt dikumpulkan (manual, otomatis, atau oleh job), soal objektif dinilai dari **snapshot** sesuai aturan jenis (T128). Hasilnya disimpan di `exam_answers.points_awarded`.
2. **Nilai attempt** = Σ poin / total poin × 100. Disimpan sebagai integer seperseratus (pola `track_score` T118) agar tidak ada selisih pembulatan.
   - Status nilai: `final`, atau `menunggu_esai` bila ada esai yang belum dikoreksi.
   - Nilai `menunggu_esai` tidak dikirim ke modul lain.
3. **Soal dibatalkan:** poin dikeluarkan dari total untuk semua peserta.
4. **Nilai ulang:** bila kunci salah, pengelola membetulkan kunci di snapshot lalu menilai ulang.
   - Tampil pratinjau dampak: "41 peserta naik, 3 turun".
   - Konfirmasi dengan alasan; tercatat di audit.
   - Nilai yang sudah dikirim ke seleksi (T149) atau rapor (T150) ikut diperbarui, dengan pemberitahuan ke guru.
5. Penilaian idempoten: memanggil ulang tidak mengubah hasil kecuali kunci atau aturan berubah.

## File yang disentuh
- `internal/exam/grading.go`, `grading_test.go`, `regrade.go`

## Kriteria selesai
- [ ] Uji tabel penilaian per jenis, termasuk kosong, berlebih, dan format angka 0,5 / 0.5
- [ ] Nilai ulang menampilkan dampak sebelum diterapkan dan tercatat di audit
- [ ] Nilai `menunggu_esai` tidak bocor ke seleksi atau rapor
