package billing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"akra/internal/platform/database"
	"akra/internal/platform/jobs"
)

type bulkFixture struct {
	*fixture
	svc              *BulkService
	year             string
	class1A, class1B string // 1A: 20 siswa, 1B: 200 siswa
	otherYearClass   string
	classB           string // kelas sekolah B
	spp, kegiatan    FeeType
	inactive         FeeType
}

func newBulkFixture(t *testing.T) *bulkFixture {
	t.Helper()
	f := &bulkFixture{fixture: newFixture(t)}
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := f.tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.year = one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, f.schoolA)
	oldYear := one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2025/2026', '2025-07-14', '2026-06-26') RETURNING id`, f.schoolA)
	yearB := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, f.schoolB)
	class := `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, $4) RETURNING id`
	f.class1A = one(class, f.schoolA, f.year, "1A", 1)
	f.class1B = one(class, f.schoolA, f.year, "1B", 1)
	f.otherYearClass = one(class, f.schoolA, oldYear, "9Z", 9)
	f.classB = one(class, f.schoolB, yearB, "1A", 1)
	seed := func(classID string, n int, prefix string) {
		if _, err := f.tx.Exec(ctx, `
			WITH s AS (INSERT INTO students (school_id, nis, full_name)
			           SELECT $1, $4 || g, 'Siswa ' || $4 || g FROM generate_series(1, $3) g RETURNING id)
			INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
			SELECT $1, s.id, $2, $5, '2026-07-13' FROM s`, f.schoolA, classID, n, prefix, f.year); err != nil {
			t.Fatal(err)
		}
	}
	seed(f.class1A, 20, "A")
	seed(f.class1B, 200, "B")
	// Siswa nonaktif di 1A: tidak ikut ditagih.
	if _, err := f.tx.Exec(ctx, `UPDATE students SET status = 'keluar' WHERE school_id = $1 AND nis = 'A20'`, f.schoolA); err != nil {
		t.Fatal(err)
	}

	mk := func(name string, amount int64, day int, active bool) FeeType {
		ft, err := CreateFeeType(ctx, f.tx, f.schoolA, FeeTypeInput{Name: name, DefaultAmount: amount, DefaultDueDay: day})
		if err != nil {
			t.Fatal(err)
		}
		if !active {
			ft, _ = SetFeeTypeActive(ctx, f.tx, f.schoolA, ft.ID, false)
		}
		return ft
	}
	f.spp = mk("SPP", 150000, 10, true)
	f.kegiatan = mk("Kegiatan", 75000, 0, true)
	f.inactive = mk("Seragam Lama", 450000, 10, false)

	dsn := os.Getenv("TEST_DATABASE_URL")
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	inserter, err := jobs.NewClient(pool, nil, jobs.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	f.svc = NewBulkService(f.tx, inserter)
	return f
}

func (f *bulkFixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.tx.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *bulkFixture) invoices(t *testing.T) int {
	return f.count(t, `SELECT count(*) FROM invoices WHERE school_id = $1`, f.schoolA)
}

func sppForm(f *bulkFixture, classID string) BulkForm {
	return BulkForm{FeeTypeID: f.spp.ID, Period: "2026-09", ClassID: classID}
}

func TestBulk_PreviewChangesNothing(t *testing.T) {
	f := newBulkFixture(t)
	ctx := context.Background()
	// 5 siswa 1A sudah punya SPP September.
	enr, _ := f.tx.Query(ctx, `SELECT id FROM enrollments WHERE class_id = $1 ORDER BY id LIMIT 5`, f.class1A)
	ids, _ := pgx.CollectRows(enr, pgx.RowTo[string])
	for _, id := range ids {
		if _, err := CreateInvoice(ctx, f.tx, f.actor, InvoiceRequest{EnrollmentID: id, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.spp.ID}}}); err != nil {
			t.Fatal(err)
		}
	}
	beforeInv, beforeRuns := f.invoices(t), f.count(t, `SELECT count(*) FROM bulk_invoice_runs`)

	p, err := f.svc.Preview(ctx, f.schoolA, sppForm(f, f.class1A))
	if err != nil {
		t.Fatal(err)
	}
	// 20 siswa, 1 keluar → 19 aktif; 5 sudah ditagih (kecuali jika salah satunya yang keluar).
	if p.Eligible+p.Skipped == 0 || p.ToCreate+p.Skipped != p.Eligible || p.Eligible != 19 {
		t.Errorf("preview = %+v", p)
	}
	if p.Skipped < 4 || p.Skipped > 5 || p.Total != int64(p.ToCreate)*150000 || p.Async {
		t.Errorf("preview counts = to create %d, skipped %d, total %d", p.ToCreate, p.Skipped, p.Total)
	}
	if f.invoices(t) != beforeInv || f.count(t, `SELECT count(*) FROM bulk_invoice_runs`) != beforeRuns {
		t.Error("preview changed the database")
	}
	all, err := f.svc.Preview(ctx, f.schoolA, sppForm(f, ""))
	if err != nil || all.Eligible != 219 || len(all.PerClass) != 2 || !all.Async {
		t.Errorf("all classes preview = %+v, %v", all, err)
	}
}

func TestBulk_SyncRunTwiceCreatesNoDuplicates(t *testing.T) {
	f := newBulkFixture(t)
	ctx := context.Background()

	run, err := f.svc.Run(ctx, f.actor, sppForm(f, f.class1A))
	if err != nil {
		t.Fatal(err)
	}
	if run.Mode != "sync" || run.Status != RunDone || run.CreatedCount != 19 || run.SkippedCount != 0 {
		t.Errorf("first run = %+v", run)
	}
	again, err := f.svc.Run(ctx, f.actor, sppForm(f, f.class1A))
	if err != nil {
		t.Fatal(err)
	}
	if again.CreatedCount != 0 || again.SkippedCount != 19 {
		t.Errorf("second run = created %d, skipped %d; want 0 and 19", again.CreatedCount, again.SkippedCount)
	}
	if n := f.count(t, `SELECT count(*) FROM invoice_items WHERE fee_type_id = $1 AND period = '2026-09'`, f.spp.ID); n != 19 {
		t.Errorf("SPP September items = %d, want 19", n)
	}
	// Satu audit ringkasan per proses, tanpa audit per tagihan.
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'billing.bulk_create'`, f.schoolA); n != 2 {
		t.Errorf("bulk_create audits = %d, want 2", n)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'invoice.create'`, f.schoolA); n != 0 {
		t.Errorf("per-invoice audits = %d, want 0", n)
	}
	// Nominal khusus dan jatuh tempo eksplisit.
	run, err = f.svc.Run(ctx, f.actor, BulkForm{FeeTypeID: f.kegiatan.ID, Period: "2026-09", ClassID: f.class1A, DueDate: "2026-09-25", Amount: "80.000"})
	if err != nil || run.CreatedCount != 19 {
		t.Fatalf("kegiatan run = %+v, %v", run, err)
	}
	var amount int64
	var due time.Time
	_ = f.tx.QueryRow(ctx, `SELECT i.amount, v.due_date FROM invoice_items i JOIN invoices v ON v.id = i.invoice_id WHERE i.fee_type_id = $1 LIMIT 1`, f.kegiatan.ID).Scan(&amount, &due)
	if amount != 80000 || due.Format("2006-01-02") != "2026-09-25" {
		t.Errorf("custom amount/due = %d / %v", amount, due)
	}
}

func TestBulk_Validation(t *testing.T) {
	f := newBulkFixture(t)
	ctx := context.Background()
	for name, form := range map[string]BulkForm{
		"inactive fee type": {FeeTypeID: f.inactive.ID, Period: "2026-09", ClassID: f.class1A},
		"no period":         {FeeTypeID: f.spp.ID, Period: "", ClassID: f.class1A},
		"no due date":       {FeeTypeID: f.kegiatan.ID, Period: "2026-09", ClassID: f.class1A},
		"bad amount":        {FeeTypeID: f.spp.ID, Period: "2026-09", ClassID: f.class1A, Amount: "-5"},
	} {
		_, err := f.svc.Preview(ctx, f.schoolA, form)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: preview error = %v, want validation error", name, err)
		}
		if _, err := f.svc.Run(ctx, f.actor, form); !errors.As(err, &ve) {
			t.Errorf("%s: run error = %v, want validation error", name, err)
		}
	}
	for name, classID := range map[string]string{"other school": f.classB, "other year": f.otherYearClass, "invalid": "bukan-uuid"} {
		if _, err := f.svc.Preview(ctx, f.schoolA, sppForm(f, classID)); !errors.Is(err, ErrClassNotFound) {
			t.Errorf("%s class: error = %v, want ErrClassNotFound", name, err)
		}
		if _, err := f.svc.Run(ctx, f.actor, sppForm(f, classID)); !errors.Is(err, ErrClassNotFound) {
			t.Errorf("%s class run: error = %v", name, err)
		}
	}
	if f.invoices(t) != 0 {
		t.Error("invalid requests created invoices")
	}
}

func runJob(t *testing.T, w *BulkInvoiceWorker, schoolID, runID string, attempt int) error {
	t.Helper()
	return w.Work(context.Background(), &river.Job[BulkInvoiceArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: bulkMaxAttempts},
		Args:   BulkInvoiceArgs{SchoolID: schoolID, RunID: runID},
	})
}

func TestBulk_LargeRunUsesJobAndRetriesIdempotently(t *testing.T) {
	f := newBulkFixture(t)
	ctx := context.Background()

	start := time.Now()
	run, err := f.svc.Run(ctx, f.actor, sppForm(f, f.class1B))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("enqueueing 200 students took %v", elapsed)
	if run.Mode != "async" || run.Status != RunQueued || run.TotalCount != 200 || f.invoices(t) != 0 {
		t.Fatalf("run = %+v, invoices = %d; want queued async with no invoices yet", run, f.invoices(t))
	}
	var jobKind string
	_ = f.tx.QueryRow(ctx, `SELECT kind FROM river_job WHERE id = (SELECT river_job_id FROM bulk_invoice_runs WHERE id = $1)`, run.ID).Scan(&jobKind)
	if jobKind != "billing_bulk_invoice" {
		t.Errorf("river job kind = %q", jobKind)
	}

	worker := NewBulkInvoiceWorker(f.tx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Percobaan 1 gagal setelah 2 batch (50 siswa).
	worker.failAfterBatches = 2
	if err := runJob(t, worker, f.schoolA, run.ID, 1); err == nil {
		t.Fatal("first attempt should fail")
	}
	mid, _ := f.svc.Get(ctx, f.schoolA, run.ID)
	if f.invoices(t) != 50 || mid.Status != RunRunning || mid.ProcessedCount != 50 {
		t.Errorf("after failed attempt: invoices %d, run %+v", f.invoices(t), mid)
	}
	// Percobaan 2 (diulang River) menyelesaikan sisanya tanpa duplikat.
	worker.failAfterBatches = 0
	if err := runJob(t, worker, f.schoolA, run.ID, 2); err != nil {
		t.Fatal(err)
	}
	done, _ := f.svc.Get(ctx, f.schoolA, run.ID)
	if done.Status != RunDone || done.CreatedCount != 200 || done.SkippedCount != 0 || done.ProcessedCount != 200 {
		t.Errorf("finished run = %+v", done)
	}
	if n := f.count(t, `SELECT count(*) FROM invoice_items WHERE fee_type_id = $1 AND period = '2026-09'`, f.spp.ID); n != 200 {
		t.Errorf("items = %d, want 200 (no duplicates)", n)
	}
	if n := f.count(t, `SELECT count(DISTINCT enrollment_id) FROM invoices WHERE bulk_run_id = $1`, run.ID); n != 200 {
		t.Errorf("distinct invoiced enrollments = %d, want 200", n)
	}
	// Job yang terulang lagi setelah selesai tidak membuat apa pun.
	if err := runJob(t, worker, f.schoolA, run.ID, 3); err != nil || f.invoices(t) != 200 {
		t.Errorf("rerun after done: err %v, invoices %d", err, f.invoices(t))
	}
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE action = 'billing.bulk_create' AND entity_id = $1`, run.ID); n != 1 {
		t.Errorf("bulk_create audits = %d, want 1", n)
	}
}

func TestBulk_JobMarkedFailedAfterLastAttempt(t *testing.T) {
	f := newBulkFixture(t)
	ctx := context.Background()
	run, err := f.svc.Run(ctx, f.actor, sppForm(f, f.class1B))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewBulkInvoiceWorker(f.tx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.failAfterBatches = 1
	if err := runJob(t, worker, f.schoolA, run.ID, bulkMaxAttempts); err == nil {
		t.Fatal("expected failure")
	}
	got, _ := f.svc.Get(ctx, f.schoolA, run.ID)
	if got.Status != RunFailed || got.Error == "" {
		t.Errorf("run after last attempt = %+v", got)
	}
}
