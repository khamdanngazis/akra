# T66 — Jadwal pelajaran

**Fase:** 06 Jadwal dan Penugasan
**Dependensi:** T65
**Estimasi:** 2 hari

## Tujuan
Setiap kelas punya jadwal mingguan. Sistem menolak jadwal yang bentrok untuk kelas yang sama atau guru yang sama.

## Langkah
1. Migrasi `schedule_slots`:
   - `id uuid PK`, `school_id uuid NOT NULL`, `academic_year_id uuid NOT NULL`, `class_id uuid NOT NULL REFERENCES classes(id)`, `subject_id uuid NOT NULL REFERENCES subjects(id)`, `teacher_id uuid NOT NULL REFERENCES users(id)`, `day_of_week smallint NOT NULL` (1 = Senin sampai 6 = Sabtu), `start_time time NOT NULL`, `end_time time NOT NULL`.
2. Constraint:
   - `CHECK (day_of_week BETWEEN 1 AND 6)`
   - `CHECK (end_time > start_time)`
3. Validasi bentrok di transaksi, dengan `SELECT ... FOR UPDATE` pada baris kelas dan guru:
   - Kelas yang sama tidak boleh punya dua slot yang waktunya berpotongan pada hari yang sama.
   - Guru yang sama tidak boleh mengajar dua kelas dengan waktu berpotongan.
4. Validasi di service: guru harus punya penugasan untuk kelas dan mapel tersebut (T65).
5. Halaman `GET /jadwal?kelas=` dengan form tambah slot. Akses tulis: `admin_tu`.
6. Audit: `schedule.create`, `schedule.update`, `schedule.delete`.
7. Hapus slot diizinkan karena jadwal bukan data keuangan.

## File yang disentuh
- `db/migrations/0000XX_schedule_slots.up.sql` dan `.down.sql`
- `internal/schedule/repo.go`, `service.go`, `handler.go`, `conflict.go`
- `web/templates/schedule/class.html`, `form.html`

## Kriteria selesai
- [ ] Dua slot kelas yang sama pada hari dan jam yang bertabrakan ditolak
- [ ] Guru yang mengajar di dua kelas pada jam bertabrakan ditolak
- [ ] Slot yang bersebelahan (selesai 08.00, mulai 08.00) diterima
- [ ] Uji konkurensi: dua permintaan bentrok bersamaan, hanya satu yang berhasil
- [ ] Slot tanpa penugasan guru ditolak

## Catatan
- Jadwal ujian dan jadwal pengganti belum termasuk. Keduanya masuk daftar keputusan wawancara.
