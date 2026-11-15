# T93b — Perankingan per jalur dan penetapan hasil

**Fase:** 10 Pendaftaran
**Dependensi:** T93, T93a
**Estimasi:** 1,5 hari

## Tujuan
TU bisa melihat peringkat pendaftar per jalur, lalu menetapkan diterima, cadangan, atau ditolak secara massal sesuai kuota.

## Langkah
1. Halaman `GET /pendaftaran/periode/{id}/hasil`: per jalur, pendaftar `diverifikasi` diurutkan:
   - jalur dengan tes: skor akhir (T93a) menurun, lalu waktu kirim paling awal;
   - jalur tanpa tes: waktu kirim paling awal.
2. Usulan otomatis: peringkat 1..kuota = diterima, sisanya = cadangan. Pendaftar dengan nilai belum lengkap tidak diusulkan.
3. TU bisa mengubah usulan per baris sebelum menetapkan.
4. Tombol "Tetapkan hasil": satu transaksi, memakai perpindahan status T93 dan mencatat `applicant_events`. Kuota terlampaui → peringatan, tidak diblok.
5. Hasil tampil di halaman cek status (T95). Pengumuman massal (WhatsApp/email) di luar cakupan.
6. Ekspor peringkat ke Excel (pakai ekspor laporan yang ada).

## File yang disentuh
- `internal/admission/ranking.go`, `ranking_test.go`, `result_handler.go`
- `web/templates/admission/results.html`

## Kriteria selesai
- [x] Urutan peringkat diuji unit, termasuk skor sama (waktu kirim menentukan)
- [x] Penetapan hasil dua kali tidak membuat event ganda untuk pendaftar yang statusnya tidak berubah
- [x] Pendaftar dengan nilai belum lengkap tidak masuk usulan diterima
