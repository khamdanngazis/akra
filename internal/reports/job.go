package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"akra/internal/billing"
	"akra/internal/platform/database"
	"akra/internal/platform/storage"
	"akra/internal/reports/export"
	"akra/internal/school"
)

// Status job ekspor (report_jobs.status).
const (
	JobQueued  = "antrean"
	JobRunning = "berjalan"
	JobDone    = "selesai"
	JobFailed  = "gagal"
)

// Jenis laporan job.
const (
	TypeAttendance = "attendance"
	TypeArrears    = "arrears"
)

const (
	exportMaxAttempts = 3
	// FileRetention adalah lama file hasil ekspor disimpan.
	FileRetention = 7 * 24 * time.Hour
	// failedMessage adalah pesan aman untuk pengguna; detail hanya di log.
	failedMessage = "Laporan gagal dibuat. Coba lagi, atau hubungi admin jika terus gagal."
)

// ErrJobNotFound dikembalikan jika job tidak ada, milik sekolah lain, atau
// bukan milik pengguna (selain kepala sekolah).
var ErrJobNotFound = errors.New("reports: job not found")

// JobInserter memasukkan job River di dalam transaksi.
type JobInserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// JobParams adalah filter laporan yang disimpan bersama job. AsOf adalah
// tanggal saat diminta (Asia/Jakarta): dipakai sebagai "per tanggal" dan
// tanggal cetak, sehingga isi file sesuai saat diminta.
type JobParams struct {
	ClassID   string `json:"class_id,omitempty"`
	FeeTypeID string `json:"fee_type_id,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	DueFrom   string `json:"due_from,omitempty"`
	DueTo     string `json:"due_to,omitempty"`
	AsOf      string `json:"as_of"`
}

// ReportJob adalah satu permintaan ekspor di server.
type ReportJob struct {
	ID          string
	RequestedBy string
	Type        string
	Format      string
	Params      JobParams
	Status      string
	RowCount    int
	FileKey     string
	FileName    string
	Error       string
	CreatedAt   time.Time
	FinishedAt  *time.Time
	FileDeleted bool
}

// ExportArgs adalah argumen job ekspor laporan.
type ExportArgs struct {
	SchoolID string `json:"school_id"`
	JobID    string `json:"job_id"`
}

// Kind adalah nama job di River.
func (ExportArgs) Kind() string { return "report_export" }

// CleanupArgs adalah argumen job periodik penghapusan file lama.
type CleanupArgs struct{}

// Kind adalah nama job di River.
func (CleanupArgs) Kind() string { return "report_cleanup" }

func parseDay(s string) time.Time {
	d, _ := time.Parse(dateLayout, s)
	return d
}

// buildTable menyusun tabel laporan dari parameter job. Dipakai ekspor
// langsung dan job agar isinya sama.
func buildTable(ctx context.Context, q database.DBTX, schoolID, schoolName, reportType string, p JobParams) (export.Table, int, error) {
	asOf := parseDay(p.AsOf)
	switch reportType {
	case TypeAttendance:
		rep, err := BuildAttendanceReport(ctx, q, schoolID, AttendanceFilter{ClassID: p.ClassID, From: parseDay(p.From), To: parseDay(p.To)})
		if err != nil {
			return export.Table{}, 0, err
		}
		return attendanceTable(rep, schoolName, asOf), len(rep.Rows), nil
	case TypeArrears:
		f := billing.ArrearsFilter{ClassID: p.ClassID, FeeTypeID: p.FeeTypeID, DueFrom: parseDay(p.DueFrom), DueTo: parseDay(p.DueTo)}
		labels, err := arrearsFilterLabels(ctx, q, schoolID, f)
		if err != nil {
			return export.Table{}, 0, err
		}
		t, a, err := ArrearsReport(ctx, q, schoolID, schoolName, f, labels, asOf)
		return t, len(a.Items), err
	default:
		return export.Table{}, 0, fmt.Errorf("reports: unknown report type %q", reportType)
	}
}

var reportSheets = map[string]string{TypeAttendance: "Rekap", TypeArrears: "Tunggakan"}

// renderTable menulis t dalam format xlsx atau pdf.
func renderTable(t export.Table, reportType, format string) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	if format == "xlsx" {
		err = export.WriteExcel(&buf, t, reportSheets[reportType])
	} else {
		err = export.WritePDF(&buf, t)
	}
	return buf.Bytes(), err
}

// enqueueExport mencatat report_jobs dan memasukkan job River dalam satu
// transaksi.
func enqueueExport(ctx context.Context, db database.DB, jobs JobInserter, actor school.Actor, reportType, format string, p JobParams, rows int) (string, error) {
	params, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	var id string
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO report_jobs (school_id, requested_by, report_type, format, params, row_count)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, actor.SchoolID, actor.UserID, reportType, format, params, rows).Scan(&id); err != nil {
			return fmt.Errorf("reports: insert job: %w", err)
		}
		res, err := jobs.InsertTx(ctx, tx, ExportArgs{SchoolID: actor.SchoolID, JobID: id}, &river.InsertOpts{MaxAttempts: exportMaxAttempts})
		if err != nil {
			return fmt.Errorf("reports: enqueue job: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE report_jobs SET river_job_id = $2 WHERE id = $1`, id, res.Job.ID)
		return err
	})
	return id, err
}

const jobColumns = `id, requested_by, report_type, format, params, status, coalesce(row_count, 0), coalesce(file_key, ''),
	coalesce(file_name, ''), coalesce(error, ''), created_at, finished_at, file_deleted_at IS NOT NULL`

func scanJob(row pgx.Row) (ReportJob, error) {
	var j ReportJob
	var params []byte
	err := row.Scan(&j.ID, &j.RequestedBy, &j.Type, &j.Format, &params, &j.Status, &j.RowCount, &j.FileKey, &j.FileName,
		&j.Error, &j.CreatedAt, &j.FinishedAt, &j.FileDeleted)
	if err != nil {
		return j, err
	}
	return j, json.Unmarshal(params, &j.Params)
}

// GetJob mengembalikan job milik sekolah schoolID yang boleh dilihat
// pengguna userID: pemiliknya, atau semua job sekolah untuk kepala sekolah.
func GetJob(ctx context.Context, q database.DBTX, schoolID, userID string, isPrincipal bool, jobID string) (ReportJob, error) {
	if !database.IsUUID(jobID) {
		return ReportJob{}, ErrJobNotFound
	}
	j, err := scanJob(q.QueryRow(ctx, `SELECT `+jobColumns+` FROM report_jobs
		WHERE school_id = $1 AND id = $2 AND (requested_by = $3 OR $4)`, schoolID, jobID, userID, isPrincipal))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReportJob{}, ErrJobNotFound
	}
	if err != nil {
		return ReportJob{}, fmt.Errorf("reports: get job: %w", err)
	}
	return j, nil
}

// ExportWorker membuat file laporan, mengunggahnya ke storage privat, lalu
// menandai job selesai. Gagal di percobaan terakhir: status gagal dengan
// pesan aman.
type ExportWorker struct {
	river.WorkerDefaults[ExportArgs]
	db     database.DB
	store  storage.Store
	logger *slog.Logger
}

// NewExportWorker mengembalikan ExportWorker.
func NewExportWorker(db database.DB, store storage.Store, logger *slog.Logger) *ExportWorker {
	return &ExportWorker{db: db, store: store, logger: logger}
}

// Work menjalankan satu ekspor.
func (w *ExportWorker) Work(ctx context.Context, job *river.Job[ExportArgs]) error {
	err := w.export(ctx, job.Args)
	if err == nil {
		return nil
	}
	w.logger.ErrorContext(ctx, "report export failed", "job_id", job.Args.JobID, "attempt", job.Attempt, "error", err)
	if job.Attempt >= job.MaxAttempts {
		if _, uerr := w.db.Exec(context.WithoutCancel(ctx), `UPDATE report_jobs SET status = $3, error = $4, finished_at = now()
			WHERE school_id = $1 AND id = $2`, job.Args.SchoolID, job.Args.JobID, JobFailed, failedMessage); uerr != nil {
			w.logger.ErrorContext(ctx, "mark report job failed", "job_id", job.Args.JobID, "error", uerr)
		}
	}
	return err
}

func (w *ExportWorker) export(ctx context.Context, a ExportArgs) error {
	j, err := scanJob(w.db.QueryRow(ctx, `SELECT `+jobColumns+` FROM report_jobs WHERE school_id = $1 AND id = $2`, a.SchoolID, a.JobID))
	if err != nil {
		return fmt.Errorf("load job: %w", err)
	}
	if j.Status == JobDone {
		return nil
	}
	if _, err := w.db.Exec(ctx, `UPDATE report_jobs SET status = $3, started_at = coalesce(started_at, now()) WHERE school_id = $1 AND id = $2`,
		a.SchoolID, a.JobID, JobRunning); err != nil {
		return fmt.Errorf("mark running: %w", err)
	}
	var schoolName string
	if err := w.db.QueryRow(ctx, `SELECT name FROM schools WHERE id = $1`, a.SchoolID).Scan(&schoolName); err != nil {
		return fmt.Errorf("load school: %w", err)
	}
	t, rows, err := buildTable(ctx, w.db, a.SchoolID, schoolName, j.Type, j.Params)
	if err != nil {
		return fmt.Errorf("build report: %w", err)
	}
	data, err := renderTable(t, j.Type, j.Format)
	if err != nil {
		return fmt.Errorf("render report: %w", err)
	}
	key := "schools/" + a.SchoolID + "/reports/" + a.JobID + "." + j.Format
	ct := "application/pdf"
	if j.Format == "xlsx" {
		ct = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	if err := w.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), ct); err != nil {
		return fmt.Errorf("upload report: %w", err)
	}
	_, err = w.db.Exec(ctx, `UPDATE report_jobs SET status = $3, file_key = $4, file_name = $5, row_count = $6, error = NULL, finished_at = now()
		WHERE school_id = $1 AND id = $2`, a.SchoolID, a.JobID, JobDone, key, exportFilename(j, t.PrintedAt), rows)
	return err
}

func exportFilename(j ReportJob, printed time.Time) string {
	if j.Type == TypeAttendance {
		return fmt.Sprintf("rekap-kehadiran-%s-%s.%s", j.Params.From, j.Params.To, j.Format)
	}
	return fmt.Sprintf("tunggakan-%s.%s", printed.Format(dateLayout), j.Format)
}

// CleanupWorker menghapus file hasil ekspor yang lebih lama dari
// FileRetention. Baris job tetap ada (riwayat), dengan file_deleted_at.
type CleanupWorker struct {
	river.WorkerDefaults[CleanupArgs]
	db     database.DB
	store  storage.Store
	logger *slog.Logger
	now    func() time.Time
}

// NewCleanupWorker mengembalikan CleanupWorker.
func NewCleanupWorker(db database.DB, store storage.Store, logger *slog.Logger) *CleanupWorker {
	return &CleanupWorker{db: db, store: store, logger: logger, now: time.Now}
}

// CleanupPeriodicJob menjalankan CleanupWorker setiap jam (dan saat start).
func CleanupPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
		func() (river.JobArgs, *river.InsertOpts) { return CleanupArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// Work menghapus file lama satu per satu; satu file gagal tidak menghentikan
// yang lain.
func (w *CleanupWorker) Work(ctx context.Context, _ *river.Job[CleanupArgs]) error {
	rows, err := w.db.Query(ctx, `SELECT school_id, id, file_key FROM report_jobs
		WHERE file_key IS NOT NULL AND finished_at < $1 ORDER BY finished_at LIMIT 500`, w.now().Add(-FileRetention))
	if err != nil {
		return fmt.Errorf("reports: list old files: %w", err)
	}
	type old struct{ school, id, key string }
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (old, error) {
		var o old
		err := row.Scan(&o.school, &o.id, &o.key)
		return o, err
	})
	if err != nil {
		return fmt.Errorf("reports: list old files: %w", err)
	}
	var failed int
	for _, o := range list {
		if err := w.store.Delete(ctx, o.key); err != nil {
			failed++
			w.logger.ErrorContext(ctx, "delete report file", "job_id", o.id, "error", err)
			continue
		}
		if _, err := w.db.Exec(ctx, `UPDATE report_jobs SET file_key = NULL, file_deleted_at = now() WHERE school_id = $1 AND id = $2`, o.school, o.id); err != nil {
			return fmt.Errorf("reports: mark file deleted: %w", err)
		}
	}
	if len(list) > 0 {
		w.logger.InfoContext(ctx, "report files cleaned up", "deleted", len(list)-failed, "failed", failed)
	}
	if failed > 0 {
		return fmt.Errorf("reports: %d report files could not be deleted", failed)
	}
	return nil
}

// jobParamsFromAttendance mengubah filter rekap kehadiran ke JobParams.
func jobParamsFromAttendance(f AttendanceFilter, asOf time.Time) JobParams {
	return JobParams{ClassID: f.ClassID, From: f.From.Format(dateLayout), To: f.To.Format(dateLayout), AsOf: asOf.Format(dateLayout)}
}

// jobParamsFromArrears mengubah filter tunggakan ke JobParams.
func jobParamsFromArrears(f billing.ArrearsFilter, asOf time.Time) JobParams {
	p := JobParams{ClassID: f.ClassID, FeeTypeID: f.FeeTypeID, AsOf: asOf.Format(dateLayout)}
	if !f.DueFrom.IsZero() {
		p.DueFrom = f.DueFrom.Format(dateLayout)
	}
	if !f.DueTo.IsZero() {
		p.DueTo = f.DueTo.Format(dateLayout)
	}
	return p
}
