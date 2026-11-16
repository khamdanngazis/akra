package admission

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/platform/storage"
	"akra/internal/school"
)

// Masa simpan data pendaftar yang tidak diterima (T113, UU PDP). Setelah
// tujuan pemrosesan (seleksi, pengumuman, sanggahan, rekap) selesai, data
// pribadi dihapus; baris pendaftar dianonimkan agar rekap tetap benar.
const (
	// RetentionDays adalah masa simpan setelah tanggal tutup periode.
	RetentionDays = 90
	// ReserveGraceDays: cadangan baru dihapus paling cepat sekian hari
	// setelah tahun ajaran dimulai (masih mungkin dipanggil).
	ReserveGraceDays = 30
	// attemptRetention adalah masa simpan riwayat percobaan formulir publik
	// (hash nomor HP dan IP untuk pembatas percobaan).
	attemptRetention = 30 * 24 * time.Hour
)

// Alasan anonimisasi.
const (
	ReasonRetention = "masa_simpan"
	ReasonRequest   = "permintaan"
)

// ActionApplicantAnonymize dicatat di audit saat data pribadi pendaftar
// dihapus.
const ActionApplicantAnonymize = "admission.applicant_anonymize"

// anonymizedName menggantikan nama pendaftar yang datanya sudah dihapus.
const anonymizedName = "Data dihapus"

// ErrNotErasable dikembalikan jika status pendaftar tidak boleh
// dianonimkan (masih diproses atau sudah diterima).
var ErrNotErasable = errors.New("admission: applicant data cannot be erased in this status")

// erasableStatuses adalah status yang data pribadinya boleh/akan dihapus.
var erasableStatuses = []string{ApplicantDraft, ApplicantRejected, ApplicantWithdrawn, ApplicantReserve}

// Erasable melaporkan apakah data pribadi pendaftar boleh dihapus.
func (a Applicant) Erasable() bool {
	if a.AnonymizedAt != nil {
		return false
	}
	for _, s := range erasableStatuses {
		if a.Status == s {
			return true
		}
	}
	return false
}

// RetentionDue mengembalikan tanggal data pribadi pendaftar dihapus
// otomatis. ok false jika status tidak dihapus otomatis.
func RetentionDue(status string, periodCloses, yearStart time.Time) (time.Time, bool) {
	switch status {
	case ApplicantDraft, ApplicantRejected, ApplicantWithdrawn:
		return periodCloses.AddDate(0, 0, RetentionDays), true
	case ApplicantReserve:
		due := periodCloses.AddDate(0, 0, RetentionDays)
		if min := yearStart.AddDate(0, 0, ReserveGraceDays); due.Before(min) {
			due = min
		}
		return due, true
	}
	return time.Time{}, false
}

// retentionDue mengembalikan tanggal penghapusan otomatis untuk pendaftar.
func (s *ApplicantService) retentionDue(ctx context.Context, q database.DBTX, a Applicant) (time.Time, bool, error) {
	if a.AnonymizedAt != nil {
		return time.Time{}, false, nil
	}
	var closes, yearStart time.Time
	if err := q.QueryRow(ctx, `SELECT p.closes_on, y.start_date FROM admission_periods p JOIN academic_years y ON y.id = p.academic_year_id
		WHERE p.school_id = $1 AND p.id = $2`, a.SchoolID, a.PeriodID).Scan(&closes, &yearStart); err != nil {
		return time.Time{}, false, mapApplicantError(err)
	}
	due, ok := RetentionDue(a.Status, closes, yearStart)
	return due, ok, nil
}

// Anonymize menghapus data pribadi pendaftar: identitas, data rinci, orang
// tua, kontak, catatan status, dan dokumen. Yang tersisa: status, jalur,
// periode, jenis kelamin, tahun lahir, nomor pendaftaran, nilai tes, dan
// catatan pembayaran. Mengembalikan key storage dokumen yang harus dihapus
// pemanggil setelah transaksi selesai. actorID kosong untuk job sistem.
func (s *ApplicantService) Anonymize(ctx context.Context, q database.DB, schoolID, id, actorID, reason string) ([]string, error) {
	var keys []string
	err := pgx.BeginFunc(ctx, q, func(tx pgx.Tx) error {
		if !database.IsUUID(id) {
			return ErrNotFound
		}
		// Cukup status; tidak perlu mendekripsi data yang akan dihapus.
		a := Applicant{ID: id, SchoolID: schoolID}
		if err := tx.QueryRow(ctx, `SELECT status, anonymized_at FROM applicants WHERE school_id = $1 AND id = $2 FOR UPDATE`,
			schoolID, id).Scan(&a.Status, &a.AnonymizedAt); err != nil {
			return err
		}
		if !a.Erasable() {
			return ErrNotErasable
		}
		rows, err := tx.Query(ctx, `DELETE FROM applicant_documents WHERE school_id = $1 AND applicant_id = $2 RETURNING storage_key`, schoolID, id)
		if err != nil {
			return err
		}
		if keys, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE applicant_events SET note = NULL WHERE school_id = $1 AND applicant_id = $2`, schoolID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE applicants SET full_name = $3, birth_date = make_date(extract(year FROM birth_date)::int, 1, 1),
			       profile = '{}', guardians = '{}', transfer = '{}',
			       nik_enc = NULL, nik_idx = NULL, kk_enc = NULL, birth_cert_enc = NULL, kip_enc = NULL, kks_enc = NULL,
			       guardian_niks_enc = NULL, contact_phone = '+00000000', resume_code_hash = NULL, resume_failures = 0,
			       resume_locked_until = NULL, anonymized_at = now(), anonymized_reason = $4, updated_at = now()
			WHERE school_id = $1 AND id = $2`, schoolID, id, anonymizedName, reason); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: schoolID, UserID: actorID, Action: ActionApplicantAnonymize,
			EntityType: "applicant", EntityID: id,
			After: map[string]any{"reason": reason, "status": a.Status, "documents_deleted": len(keys)}})
	})
	if err != nil {
		return nil, mapApplicantError(err)
	}
	return keys, nil
}

// AnonymizeBy menghapus data pribadi atas permintaan (TU), lalu file
// dokumennya.
func (s *ApplicantService) AnonymizeBy(ctx context.Context, actor school.Actor, store storage.Store, id string) error {
	keys, err := s.Anonymize(ctx, s.db, actor.SchoolID, id, actor.UserID, ReasonRequest)
	if err != nil {
		return err
	}
	deleteObjects(ctx, store, keys, nil)
	return nil
}

func deleteObjects(ctx context.Context, store storage.Store, keys []string, logger *slog.Logger) {
	if store == nil {
		return
	}
	for _, k := range keys {
		if err := store.Delete(context.WithoutCancel(ctx), k); err != nil && logger != nil {
			logger.ErrorContext(ctx, "delete applicant document object", "error", err)
		}
	}
}

// dueApplicant adalah pendaftar yang sudah lewat masa simpan.
type dueApplicant struct{ school, id string }

// dueForRetention mengembalikan pendaftar yang sudah lewat masa simpan pada
// tanggal today (paling banyak limit).
func dueForRetention(ctx context.Context, q database.DBTX, today time.Time, limit int) ([]dueApplicant, error) {
	rows, err := q.Query(ctx, `
		SELECT a.school_id, a.id FROM applicants a
		JOIN admission_periods p ON p.id = a.period_id
		JOIN academic_years y ON y.id = p.academic_year_id
		WHERE a.anonymized_at IS NULL AND (
		      (a.status IN ('draf', 'ditolak', 'mundur') AND p.closes_on + $2::int <= $1::date)
		   OR (a.status = 'cadangan' AND p.closes_on + $2::int <= $1::date AND y.start_date + $3::int <= $1::date))
		ORDER BY p.closes_on, a.created_at LIMIT $4`, today, RetentionDays, ReserveGraceDays, limit)
	if err != nil {
		return nil, fmt.Errorf("admission: list due applicants: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (dueApplicant, error) {
		var d dueApplicant
		return d, r.Scan(&d.school, &d.id)
	})
}

// RetentionArgs adalah job harian masa simpan data pendaftar.
type RetentionArgs struct{}

// Kind mengembalikan nama job.
func (RetentionArgs) Kind() string { return "admission_retention" }

// RetentionWorker menganonimkan pendaftar yang lewat masa simpan dan
// membersihkan riwayat percobaan lama.
type RetentionWorker struct {
	river.WorkerDefaults[RetentionArgs]
	db     database.DB
	store  storage.Store
	logger *slog.Logger
	today  func() time.Time
	now    func() time.Time
}

// NewRetentionWorker mengembalikan RetentionWorker.
func NewRetentionWorker(db database.DB, store storage.Store, today func() time.Time, logger *slog.Logger) *RetentionWorker {
	return &RetentionWorker{db: db, store: store, logger: logger, today: today, now: time.Now}
}

// RetentionPeriodicJob menjalankan RetentionWorker setiap hari (dan saat
// start).
func RetentionPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(24*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) { return RetentionArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// Work memproses per batch; satu pendaftar gagal tidak menghentikan yang
// lain. Aman dijalankan ulang (pendaftar yang sudah dianonimkan dilewati).
func (w *RetentionWorker) Work(ctx context.Context, _ *river.Job[RetentionArgs]) error {
	_, err := w.Run(ctx)
	return err
}

// Run menjalankan satu putaran dan mengembalikan jumlah pendaftar yang
// dianonimkan.
func (w *RetentionWorker) Run(ctx context.Context) (int, error) {
	svc := &ApplicantService{db: w.db}
	done := 0
	for {
		due, err := dueForRetention(ctx, w.db, w.today(), 200)
		if err != nil {
			return done, err
		}
		if len(due) == 0 {
			break
		}
		progressed := false
		for _, d := range due {
			keys, err := svc.Anonymize(ctx, w.db, d.school, d.id, "", ReasonRetention)
			if err != nil {
				w.logger.ErrorContext(ctx, "anonymize applicant", "applicant_id", d.id, "error", err)
				continue
			}
			deleteObjects(ctx, w.store, keys, w.logger)
			done++
			progressed = true
		}
		if !progressed || len(due) < 200 {
			break
		}
	}
	if _, err := w.db.Exec(ctx, `DELETE FROM rate_limit_attempts WHERE attempted_at < $1`, w.now().Add(-attemptRetention)); err != nil {
		return done, fmt.Errorf("admission: delete old attempts: %w", err)
	}
	if done > 0 {
		w.logger.InfoContext(ctx, "applicant data anonymized after retention period", "count", done)
	}
	return done, nil
}
