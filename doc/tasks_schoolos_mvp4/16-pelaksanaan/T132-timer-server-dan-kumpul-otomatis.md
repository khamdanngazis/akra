# T132 — Timer server dan kumpul otomatis

**Fase:** 16 Pelaksanaan
**Dependensi:** T131
**Estimasi:** 1,5 hari

## Tujuan
Waktu ujian adil dan tidak bisa diakali dengan mengubah jam perangkat atau memuat ulang halaman.

## Langkah
1. `deadline_at` dihitung dengan:
   - `min(started_at + durasi + extra_minutes, sesi.ends_at + toleransi)`.
   - Toleransi diatur per sesi: peserta yang mulai terlambat bisa dipotong waktunya atau tetap mendapat durasi penuh.
2. Klien menerima `deadline_at` dan **selisih jam server** saat halaman dimuat. Hitung mundur memakai jam server, bukan jam perangkat. Selisih diperbarui di setiap respons simpan jawaban.
3. Peringatan saat sisa waktu 10 menit dan 1 menit.
4. Saat waktu habis, klien mengirim antrian lalu mengumpulkan otomatis.
5. Job River terjadwal (tiap menit) mengumpulkan semua attempt yang lewat `deadline_at` + tenggang dan belum dikumpulkan. Ini untuk peserta yang HP-nya mati atau browser tertutup. Jawaban yang sudah tersimpan tetap dinilai.
6. Tambah waktu oleh pengawas (T134) memperbarui `deadline_at` dan langsung terlihat di perangkat peserta pada respons simpan berikutnya.

## File yang disentuh
- `internal/exam/timer.go`, `timer_test.go`, `jobs.go`
- `web/static/js/exam-take.js`

## Kriteria selesai
- [ ] Mengubah jam perangkat tidak mengubah sisa waktu (uji browser dengan jam palsu)
- [ ] Attempt yang ditinggal dikumpulkan otomatis oleh job dan jawabannya dinilai
- [ ] Sisa waktu setelah muat ulang sama dengan sebelumnya (± 2 detik)
- [ ] Perhitungan tenggat dengan `extra_minutes` dan batas sesi diuji unit
