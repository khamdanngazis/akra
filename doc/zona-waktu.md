# Zona Waktu Sekolah (T126)

Setiap sekolah punya zona waktu: WIB (`Asia/Jakarta`), WITA (`Asia/Makassar`), atau WIT (`Asia/Jayapura`). Diatur admin/TU di **Pengaturan → Profil Sekolah** (`/pengaturan/sekolah`). Bawaan: WIB.

## Aturan kode
- **Penyimpanan tetap `timestamptz` (UTC).** Zona hanya dipakai di tepi: saat membaca input jam/tanggal dan saat memformat tampilan.
- **Halaman staf:** pakai `auth.Identity.Location()` dan `auth.Identity.Today()`. Zona waktu dibaca saat login dan disimpan di sesi. Sesi lama tanpa zona dianggap WIB.
- **Halaman publik dan job latar belakang:** pakai `clock.ForSchool(ctx, db, schoolID)`.
- **SQL:** `(now() AT TIME ZONE (SELECT timezone FROM schools WHERE id = …))::date`, bukan `'Asia/Jakarta'`.
- Format jam lengkap: `clock.FormatDateTimeID(t, loc)` → "10 Februari 2027 08.00 WIT".
- **Modul ujian (`internal/exam`) tidak boleh memakai `clock.Jakarta`, `clock.Today()`, atau `clock.DateOf()`.** Dicek oleh `TestNoJakartaInExam` di `internal/platform/clock`.
- Admin yang mengganti zona langsung mendapat zona baru di sesinya. Pengguna lain mendapat zona baru saat login berikutnya (maks. 12 jam).

## Sudah memakai zona sekolah (T126)
- "Hari ini" absensi harian dan per mapel, termasuk batas tanggal yang boleh diisi dan rekap absensi.
- Jatuh tempo dan status terlambat tagihan: halaman pembayaran siswa, tunggakan, portal orang tua, beranda, dan sumber "Tunggakan" di laporan kustom.
- Jadwal tes pendaftaran: input jadwal, daftar tes, lembar penilaian PDF, status publik, dan bukti pendaftaran.
- Cap waktu dokumen: bukti pendaftaran, daftar pendaftar, peringkat, kuitansi biaya formulir, kuitansi pembayaran, rapor dan rapor di portal, F-PD dan ekspor Dapodik.
- Beranda: pembayaran hari ini dan jam pembayaran terbaru.

## Daftar periksa: masih memakai WIB
Belum diubah karena tidak memengaruhi jadwal ujian atau batas tanggal. Ubah bertahap saat modulnya disentuh.

| Lokasi | Pemakaian | Dampak bila sekolah WITA/WIT |
|---|---|---|
| `internal/payments/service.go`, `form.go` | `PaidOn` tidak boleh di masa depan; tahun nomor kuitansi | Batas "tanggal bayar" bergeser 1–2 jam di pergantian hari/tahun |
| `internal/payments/void_service.go` | pembatalan hanya di bulan berjalan | Batas bulan bergeser 1–2 jam |
| `internal/students/service.go`, `enrollment_service.go`, `profile.go`, `guardian_details.go` | validasi tanggal lahir/tanggal mutasi tidak di masa depan | Tanggal "hari ini" bergeser 1–2 jam |
| `internal/admission/period.go`, `applicant.go` | periode pendaftaran sedang dibuka; validasi tanggal | Periode dibuka/ditutup 1–2 jam lebih lambat |
| `internal/admission/fee.go` (`FeeService.today`) | tanggal bayar biaya formulir | Seperti pembayaran |
| `internal/admission/public_view.go`, `applicant_handler.go`, `convert_handler.go`, `settings_handler.go`, `summary.go` | tampilan tanggal kirim/anonimisasi/daftar ulang; rekap per hari | Label "WIB"; tanggal bisa beda di dekat tengah malam |
| `internal/announcements/handler.go` | "18 Oktober 2026 09.30 WIB" | Label jam |
| `internal/audit/view/handler.go` | filter dan tampilan jejak audit | Label jam; filter tanggal |
| `internal/importer/handler.go` | waktu unggah impor; hari ini | Label jam |
| `internal/reports/job_handler.go`, `reports/handler.go`, `reports/job.go` | waktu job laporan; "per tanggal" laporan | Label jam |
| `internal/dapodik/handler.go` | tanggal masuk Dapodik | Tanggal di dekat tengah malam |
| `internal/schedule/view.go`, `web/templates/partials/schedule_week.html` | tab hari ini di jadwal; teks "Jam dalam WIB" | Tab hari bergeser 1–2 jam |
| `internal/grades/report_pdf.go` | tebakan semester dari tanggal | Tidak berarti (hanya di akhir Juni) |
| `internal/billing/bulk_handler.go`, `payments/void_handler.go`, `home/handler.go` (pengumuman) | tanggal dibuat | Tanggal di dekat tengah malam |
| `cmd/server/workers.go` (retensi pendaftar) | hari ini untuk masa simpan | Hapus otomatis bergeser beberapa jam |
| `internal/backup/backup.go` | nama folder backup | Tidak berdampak ke sekolah (operasional platform) |
| `cmd/demoseed`, `internal/demo` | data contoh | Tidak berdampak |
