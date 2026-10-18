# T50 — Pengumuman dasar

**Fase:** 04 Portal dan Laporan
**Dependensi:** T43
**Estimasi:** 1 hari

## Tujuan
Admin atau kepala sekolah bisa membuat pengumuman untuk satu kelas atau semua kelas. Pengumuman tampil di portal orang tua yang terkait.

## Langkah
1. Buat migrasi `0000XX_announcements`:
   - `id uuid PK`, `school_id uuid NOT NULL`, `title text NOT NULL` (maks 150 karakter), `body text NOT NULL`, `class_id uuid NULL REFERENCES classes(id)` (NULL berarti semua kelas), `academic_year_id uuid NOT NULL`, `published_at timestamptz NOT NULL DEFAULT now()`, `created_by uuid NOT NULL`.
2. Query untuk portal: pengumuman dengan `school_id` sama dan (`class_id IS NULL` atau `class_id` adalah kelas dari anak yang terhubung di tahun ajaran aktif).
3. Buat halaman admin `GET /pengumuman`, `GET /pengumuman/baru`, `POST /pengumuman`.
4. Akses tulis: `admin_tu` dan `kepala_sekolah`. Bendahara dan guru hanya membaca.
5. Halaman portal `GET /portal/pengumuman` menampilkan pengumuman yang relevan, terbaru di atas.
6. Batasi body maksimal 5.000 karakter. Tidak ada HTML. Output di-escape oleh `html/template`.
7. Audit: `announcement.create`.

## File yang disentuh
- `db/migrations/0000XX_announcements.up.sql` dan `.down.sql`
- `internal/announcements/service.go`, `handler.go`
- `internal/portal/announcement_handler.go`
- `web/templates/announcements/*.html`, `web/templates/portal/announcements.html`

## Kriteria selesai
- [x] Pengumuman untuk kelas A tidak tampil di portal orang tua kelas B
- [x] Pengumuman untuk semua kelas tampil di semua portal orang tua sekolah itu
- [x] Pengumuman sekolah lain tidak tampil
- [x] Judul dengan tag `<script>` ditampilkan sebagai teks biasa
- [x] Bendahara mendapat `403` pada form pengumuman

## Catatan
- Notifikasi WhatsApp dan email tidak dikerjakan di sini. Pengumuman hanya muncul ketika orang tua membuka portal.
