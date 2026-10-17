package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
	"akra/internal/school"
)

// Aturan tagihan massal.
const (
	// SyncLimit: sampai jumlah ini tagihan dibuat langsung dalam satu
	// transaksi; di atasnya lewat job River.
	SyncLimit = 50
	// bulkBatchSize adalah jumlah siswa per transaksi di job River.
	bulkBatchSize = 25

	ActionBulkCreate = "billing.bulk_create"

	RunQueued  = "queued"
	RunRunning = "running"
	RunDone    = "done"
	RunFailed  = "failed"
)

var (
	// ErrClassNotFound dikembalikan jika kelas tidak ada, milik sekolah lain,
	// atau bukan kelas tahun ajaran aktif.
	ErrClassNotFound = errors.New("billing: class not found in the active academic year")
	// ErrNoActiveYear dikembalikan jika sekolah belum punya tahun ajaran aktif.
	ErrNoActiveYear = errors.New("billing: no active academic year")
	// ErrRunNotFound dikembalikan jika proses tagihan massal tidak ditemukan.
	ErrRunNotFound = errors.New("billing: bulk run not found")
)

// JobInserter memasukkan job River di dalam transaksi (dipenuhi
// *river.Client[pgx.Tx]).
type JobInserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// BulkForm adalah isian form tagihan massal apa adanya.
type BulkForm struct {
	FeeTypeID string
	Period    string // YYYY-MM
	ClassID   string // kosong = semua kelas tahun ajaran aktif
	DueDate   string // YYYY-MM-DD, kosong = jatuh tempo default jenis tagihan
	Amount    string // kosong = nominal default jenis tagihan
}

// bulkInput adalah isian yang sudah divalidasi.
type bulkInput struct {
	FeeType   FeeType
	Year      school.AcademicYear
	Period    string
	ClassID   string
	ClassName string
	DueDate   time.Time
	Amount    int64
	Override  bool // nominal diisi, bukan default
}

// ClassCount adalah rincian pratinjau per kelas.
type ClassCount struct {
	ClassName string
	ToCreate  int
	Skipped   int
}

// BulkPreview adalah ringkasan sebelum tagihan massal dibuat.
type BulkPreview struct {
	FeeTypeName string
	Period      string
	YearName    string
	ClassName   string // "Semua kelas" jika semua
	DueDate     time.Time
	Amount      int64
	Eligible    int // siswa aktif di kelas terpilih
	ToCreate    int
	Skipped     int // sudah punya tagihan jenis ini pada periode ini
	Total       int64
	PerClass    []ClassCount
	Async       bool // > SyncLimit: diproses job River
}

// BulkRun adalah satu proses tagihan massal.
type BulkRun struct {
	ID             string
	SchoolID       string
	AcademicYearID string
	FeeTypeID      string
	FeeTypeName    string
	ClassID        string
	ClassName      string
	Period         string
	DueDate        time.Time
	Amount         *int64
	Mode           string
	Status         string
	TotalCount     int
	ProcessedCount int
	CreatedCount   int
	SkippedCount   int
	Error          string
	RequestedBy    string
	CreatedAt      time.Time
	FinishedAt     *time.Time
}

// Finished melaporkan apakah proses sudah selesai (berhasil atau gagal).
func (r BulkRun) Finished() bool { return r.Status == RunDone || r.Status == RunFailed }

// BulkService membuat tagihan untuk satu kelas atau semua kelas sekaligus.
type BulkService struct {
	db   database.DB
	jobs JobInserter
}

// NewBulkService mengembalikan BulkService. jobs dipakai untuk proses besar.
func NewBulkService(db database.DB, jobs JobInserter) *BulkService {
	return &BulkService{db: db, jobs: jobs}
}

// Preview menghitung berapa tagihan akan dibuat dan dilewati, tanpa
// menyimpan apa pun.
func (s *BulkService) Preview(ctx context.Context, schoolID string, f BulkForm) (BulkPreview, error) {
	in, err := s.validate(ctx, s.db, schoolID, f)
	if err != nil {
		return BulkPreview{}, err
	}
	targets, err := eligibleEnrollments(ctx, s.db, schoolID, in.Year.ID, in.ClassID, in.FeeType.ID, in.Period)
	if err != nil {
		return BulkPreview{}, err
	}
	p := BulkPreview{
		FeeTypeName: in.FeeType.Name, Period: in.Period, YearName: in.Year.Name, ClassName: in.ClassName,
		DueDate: in.DueDate, Amount: in.Amount, Eligible: len(targets), Async: len(targets) > SyncLimit,
	}
	idx := map[string]int{}
	for _, t := range targets {
		i, ok := idx[t.ClassName]
		if !ok {
			i = len(p.PerClass)
			idx[t.ClassName] = i
			p.PerClass = append(p.PerClass, ClassCount{ClassName: t.ClassName})
		}
		if t.Billed {
			p.Skipped++
			p.PerClass[i].Skipped++
		} else {
			p.ToCreate++
			p.PerClass[i].ToCreate++
		}
	}
	p.Total = int64(p.ToCreate) * in.Amount
	return p, nil
}

// Run memulai tagihan massal. Untuk SyncLimit siswa atau kurang, semua
// tagihan dibuat langsung dalam satu transaksi. Untuk lebih dari itu, catatan
// proses dan job River dimasukkan dalam satu transaksi; tagihan dibuat oleh
// BulkInvoiceWorker. Daftar siswa dihitung ulang saat eksekusi, jadi bisa
// berbeda dari pratinjau.
func (s *BulkService) Run(ctx context.Context, actor school.Actor, f BulkForm) (BulkRun, error) {
	var runID string
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		in, err := s.validate(ctx, tx, actor.SchoolID, f)
		if err != nil {
			return err
		}
		targets, err := eligibleEnrollments(ctx, tx, actor.SchoolID, in.Year.ID, in.ClassID, in.FeeType.ID, in.Period)
		if err != nil {
			return err
		}
		mode := "sync"
		if len(targets) > SyncLimit {
			mode = "async"
		}
		var amount any
		if in.Override {
			amount = in.Amount
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO bulk_invoice_runs (school_id, academic_year_id, fee_type_id, class_id, period, due_date, amount, mode, total_count, requested_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			actor.SchoolID, in.Year.ID, in.FeeType.ID, nullIfEmpty(in.ClassID), in.Period, in.DueDate, amount, mode, len(targets), actor.UserID).Scan(&runID)
		if err != nil {
			return fmt.Errorf("billing: create bulk run: %w", err)
		}

		if mode == "async" {
			res, err := s.jobs.InsertTx(ctx, tx, BulkInvoiceArgs{SchoolID: actor.SchoolID, RunID: runID}, &river.InsertOpts{MaxAttempts: bulkMaxAttempts})
			if err != nil {
				return fmt.Errorf("billing: enqueue bulk invoice job: %w", err)
			}
			_, err = tx.Exec(ctx, `UPDATE bulk_invoice_runs SET river_job_id = $2 WHERE id = $1`, runID, res.Job.ID)
			return err
		}

		if _, err := tx.Exec(ctx, `UPDATE bulk_invoice_runs SET status = 'running', started_at = now() WHERE id = $1`, runID); err != nil {
			return err
		}
		run := runFromInput(runID, actor, in)
		if _, err := processTargets(ctx, tx, run, in, targets); err != nil {
			return err
		}
		return finishRun(ctx, tx, actor.SchoolID, runID)
	})
	if err != nil {
		return BulkRun{}, err
	}
	return GetBulkRun(ctx, s.db, actor.SchoolID, runID)
}

// Get mengembalikan satu proses tagihan massal.
func (s *BulkService) Get(ctx context.Context, schoolID, id string) (BulkRun, error) {
	return GetBulkRun(ctx, s.db, schoolID, id)
}

// FormOptions mengembalikan pilihan form: jenis tagihan aktif dan kelas tahun
// ajaran aktif.
func (s *BulkService) FormOptions(ctx context.Context, schoolID string) ([]FeeType, school.AcademicYear, []ClassOption, error) {
	fees, err := ListActiveFeeTypes(ctx, s.db, schoolID)
	if err != nil {
		return nil, school.AcademicYear{}, nil, err
	}
	year, err := school.GetActiveAcademicYear(ctx, s.db, schoolID)
	if errors.Is(err, school.ErrNotFound) {
		return fees, school.AcademicYear{}, nil, ErrNoActiveYear
	}
	if err != nil {
		return nil, school.AcademicYear{}, nil, err
	}
	classes, err := listClassOptions(ctx, s.db, schoolID, year.ID)
	return fees, year, classes, err
}

// ClassOption adalah kelas untuk pilihan form.
type ClassOption struct {
	ID, Name string
}

func listClassOptions(ctx context.Context, q database.DBTX, schoolID, yearID string) ([]ClassOption, error) {
	rows, err := q.Query(ctx, `SELECT id, name FROM classes WHERE school_id = $1 AND academic_year_id = $2 ORDER BY grade_level, lower(name)`, schoolID, yearID)
	if err != nil {
		return nil, fmt.Errorf("billing: list classes: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ClassOption, error) {
		var c ClassOption
		err := row.Scan(&c.ID, &c.Name)
		return c, err
	})
}

// validate memeriksa form tagihan massal terhadap data sekolah.
func (s *BulkService) validate(ctx context.Context, q database.DBTX, schoolID string, f BulkForm) (bulkInput, error) {
	fields := map[string]string{}
	var in bulkInput

	year, err := school.GetActiveAcademicYear(ctx, q, schoolID)
	if errors.Is(err, school.ErrNotFound) {
		return bulkInput{}, ErrNoActiveYear
	}
	if err != nil {
		return bulkInput{}, err
	}
	in.Year = year

	ft, err := GetFeeType(ctx, q, schoolID, f.FeeTypeID)
	switch {
	case errors.Is(err, ErrFeeTypeNotFound):
		fields["fee_type_id"] = "Pilih jenis tagihan."
	case err != nil:
		return bulkInput{}, err
	case !ft.IsActive:
		fields["fee_type_id"] = "Jenis tagihan ini nonaktif dan tidak bisa dipakai untuk tagihan baru."
	default:
		in.FeeType = ft
	}

	in.Period = strings.TrimSpace(f.Period)
	if !periodPattern.MatchString(in.Period) {
		fields["period"] = "Pilih periode (bulan dan tahun)."
	}

	in.ClassName = "Semua kelas"
	if f.ClassID != "" {
		if !database.IsUUID(f.ClassID) {
			return bulkInput{}, ErrClassNotFound
		}
		err := q.QueryRow(ctx, `SELECT name FROM classes WHERE school_id = $1 AND id = $2 AND academic_year_id = $3`,
			schoolID, f.ClassID, year.ID).Scan(&in.ClassName)
		if errors.Is(err, pgx.ErrNoRows) {
			return bulkInput{}, ErrClassNotFound
		}
		if err != nil {
			return bulkInput{}, err
		}
		in.ClassID = f.ClassID
	}

	in.Amount = in.FeeType.DefaultAmount
	if a := strings.TrimSpace(f.Amount); a != "" {
		amount, err := money.ParseRupiah(a)
		switch {
		case err != nil:
			fields["amount"] = "Isi nominal dalam rupiah bulat, mis. 150.000."
		case amount > MaxAmount:
			fields["amount"] = "Nominal terlalu besar."
		default:
			in.Amount, in.Override = amount, true
		}
	}

	if d := strings.TrimSpace(f.DueDate); d != "" {
		due, err := time.Parse("2006-01-02", d)
		if err != nil {
			fields["due_date"] = "Tanggal jatuh tempo tidak valid."
		}
		in.DueDate = due
	} else if fields["period"] == "" && in.FeeType.ID != "" {
		if in.FeeType.DefaultDueDay == 0 {
			fields["due_date"] = "Jenis tagihan ini tidak punya jatuh tempo default. Isi tanggal jatuh tempo."
		} else {
			in.DueDate, _ = DueDateFor(in.Period, in.FeeType.DefaultDueDay)
		}
	}

	if len(fields) > 0 {
		return bulkInput{}, &ValidationError{Fields: fields}
	}
	return in, nil
}

// target adalah satu enrollment calon tagihan.
type target struct {
	EnrollmentID string
	ClassName    string
	Billed       bool // sudah punya item jenis tagihan ini pada periode ini
}

// eligibleEnrollments mengembalikan enrollment aktif (siswa berstatus aktif)
// di kelas terpilih pada tahun ajaran aktif, beserta tanda sudah ditagih.
func eligibleEnrollments(ctx context.Context, q database.DBTX, schoolID, yearID, classID, feeTypeID, period string) ([]target, error) {
	rows, err := q.Query(ctx, `
		SELECT e.id, c.name,
		       EXISTS (SELECT 1 FROM invoice_items i
		               WHERE i.enrollment_id = e.id AND i.fee_type_id = $4 AND i.period = $5)
		FROM enrollments e
		JOIN classes c ON c.id = e.class_id AND c.school_id = e.school_id
		JOIN students s ON s.id = e.student_id AND s.school_id = e.school_id
		WHERE e.school_id = $1 AND e.academic_year_id = $2 AND e.end_date IS NULL AND s.status = 'aktif'
		  AND ($3::uuid IS NULL OR e.class_id = $3::uuid)
		ORDER BY c.grade_level, lower(c.name), lower(s.full_name), e.id`,
		schoolID, yearID, nullIfEmpty(classID), feeTypeID, period)
	if err != nil {
		return nil, fmt.Errorf("billing: list eligible enrollments: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (target, error) {
		var t target
		err := row.Scan(&t.EnrollmentID, &t.ClassName, &t.Billed)
		return t, err
	})
}

// processTargets membuat tagihan untuk target yang belum ditagih. Setiap
// tagihan di savepoint sendiri; tagihan yang ternyata sudah ada (mis. dibuat
// bersamaan) dilewati. Mengembalikan jumlah tagihan baru.
func processTargets(ctx context.Context, tx pgx.Tx, run BulkRun, in bulkInput, targets []target) (int, error) {
	actor := school.Actor{SchoolID: run.SchoolID, UserID: run.RequestedBy}
	created := 0
	for _, t := range targets {
		if t.Billed {
			continue
		}
		amount := in.Amount
		err := pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
			_, err := createInvoice(ctx, sp, actor, InvoiceRequest{
				EnrollmentID: t.EnrollmentID, Period: in.Period, DueDate: in.DueDate, BulkRunID: run.ID,
				Items: []ItemRequest{{FeeTypeID: in.FeeType.ID, Amount: &amount}},
			}, false)
			return err
		})
		switch {
		case errors.Is(err, ErrDuplicateItem):
			// Sudah ditagih di antara pengecekan dan pembuatan.
		case err != nil:
			return created, err
		default:
			created++
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE bulk_invoice_runs SET processed_count = processed_count + $2 WHERE id = $1`, run.ID, len(targets)); err != nil {
		return created, err
	}
	return created, nil
}

// finishRun menandai proses selesai dengan jumlah dari database (tagihan
// dengan bulk_run_id ini), lalu menulis satu audit ringkasan.
func finishRun(ctx context.Context, tx pgx.Tx, schoolID, runID string) error {
	var run BulkRun
	err := tx.QueryRow(ctx, `
		UPDATE bulk_invoice_runs r
		SET created_count = c.n, skipped_count = greatest(r.total_count - c.n, 0), processed_count = r.total_count,
		    status = 'done', finished_at = now(), error = NULL
		FROM (SELECT count(*)::int AS n FROM invoices WHERE bulk_run_id = $2) c
		WHERE r.school_id = $1 AND r.id = $2
		RETURNING r.requested_by, r.fee_type_id, coalesce(r.class_id::text, ''), r.period, r.total_count, r.created_count, r.skipped_count, r.mode`,
		schoolID, runID).Scan(&run.RequestedBy, &run.FeeTypeID, &run.ClassID, &run.Period, &run.TotalCount, &run.CreatedCount, &run.SkippedCount, &run.Mode)
	if err != nil {
		return fmt.Errorf("billing: finish bulk run: %w", err)
	}
	entry := audit.Entry{
		SchoolID: schoolID, UserID: run.RequestedBy,
		Action: ActionBulkCreate, EntityType: "bulk_invoice_run", EntityID: runID,
		After: map[string]any{
			"fee_type_id": run.FeeTypeID, "class_id": nullIfEmpty(run.ClassID), "period": run.Period, "mode": run.Mode,
			"eligible": run.TotalCount, "created": run.CreatedCount, "skipped": run.SkippedCount,
		},
	}
	return audit.Record(ctx, tx, entry)
}

func runFromInput(id string, actor school.Actor, in bulkInput) BulkRun {
	return BulkRun{ID: id, SchoolID: actor.SchoolID, RequestedBy: actor.UserID, FeeTypeID: in.FeeType.ID, Period: in.Period}
}

// GetBulkRun mengembalikan satu proses tagihan massal milik schoolID.
func GetBulkRun(ctx context.Context, q database.DBTX, schoolID, id string) (BulkRun, error) {
	if !database.IsUUID(id) {
		return BulkRun{}, ErrRunNotFound
	}
	var r BulkRun
	err := q.QueryRow(ctx, `
		SELECT r.id, r.school_id, r.academic_year_id, r.fee_type_id, f.name, coalesce(r.class_id::text, ''), coalesce(c.name, ''),
		       r.period, r.due_date, r.amount, r.mode, r.status, r.total_count, r.processed_count, r.created_count, r.skipped_count,
		       coalesce(r.error, ''), r.requested_by, r.created_at, r.finished_at
		FROM bulk_invoice_runs r
		JOIN fee_types f ON f.id = r.fee_type_id AND f.school_id = r.school_id
		LEFT JOIN classes c ON c.id = r.class_id AND c.school_id = r.school_id
		WHERE r.school_id = $1 AND r.id = $2`, schoolID, id).Scan(
		&r.ID, &r.SchoolID, &r.AcademicYearID, &r.FeeTypeID, &r.FeeTypeName, &r.ClassID, &r.ClassName,
		&r.Period, &r.DueDate, &r.Amount, &r.Mode, &r.Status, &r.TotalCount, &r.ProcessedCount, &r.CreatedCount, &r.SkippedCount,
		&r.Error, &r.RequestedBy, &r.CreatedAt, &r.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BulkRun{}, ErrRunNotFound
	}
	if err != nil {
		return BulkRun{}, fmt.Errorf("billing: get bulk run: %w", err)
	}
	if r.ClassName == "" {
		r.ClassName = "Semua kelas"
	}
	return r, nil
}

// ListRecent mengembalikan proses tagihan massal terbaru sekolah.
func (s *BulkService) ListRecent(ctx context.Context, schoolID string, limit int) ([]BulkRun, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM bulk_invoice_runs WHERE school_id = $1 ORDER BY created_at DESC LIMIT $2`, schoolID, limit)
	if err != nil {
		return nil, fmt.Errorf("billing: list bulk runs: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	runs := make([]BulkRun, 0, len(ids))
	for _, id := range ids {
		r, err := GetBulkRun(ctx, s.db, schoolID, id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, nil
}
