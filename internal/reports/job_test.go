package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/xuri/excelize/v2"

	"akra/internal/platform/database"
	"akra/internal/platform/jobs"
	"akra/internal/platform/storage"
)

type jobFixture struct {
	*fixture
	dir    string
	store  *storage.Local
	logger *slog.Logger
}

func newJobFixture(t *testing.T) *jobFixture {
	t.Helper()
	f := &jobFixture{fixture: newFixture(t), dir: t.TempDir(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	f.arrearsData(t)
	pool, err := database.Connect(context.Background(), os.Getenv("TEST_DATABASE_URL"), 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	inserter, err := jobs.NewClient(pool, nil, jobs.Config{}, f.logger)
	if err != nil {
		t.Fatal(err)
	}
	if f.store, err = storage.NewLocal(f.dir); err != nil {
		t.Fatal(err)
	}
	f.h.jobs, f.h.store = inserter, f.store
	return f
}

func (f *jobFixture) work(t *testing.T, jobID string, attempt int) error {
	t.Helper()
	w := NewExportWorker(f.tx, f.store, f.logger)
	return w.Work(context.Background(), &river.Job[ExportArgs]{
		JobRow: &rivertype.JobRow{Attempt: attempt, MaxAttempts: exportMaxAttempts},
		Args:   ExportArgs{SchoolID: f.schoolA, JobID: jobID},
	})
}

func (f *jobFixture) user(t *testing.T, email string) string {
	t.Helper()
	var id string
	if err := f.tx.QueryRow(context.Background(), `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, $2, 'x', $2) RETURNING id`, f.schoolA, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

var jobURL = regexp.MustCompile(`^/laporan/jobs/([0-9a-f-]{36})$`)

func TestReportJob_LargeExportRunsOnServer(t *testing.T) {
	f := newJobFixture(t)
	ctx := context.Background()

	// Di bawah ambang tetap langsung.
	if rec := f.get("/laporan/tunggakan.xlsx", "bendahara"); rec.Code != http.StatusOK {
		t.Fatalf("small export: status = %d", rec.Code)
	}
	direct := f.get("/laporan/tunggakan.xlsx", "bendahara").Body.Bytes()

	f.h.maxRows = 2 // 3 tagihan menunggak > 2
	start := time.Now()
	rec := f.get("/laporan/tunggakan.xlsx", "bendahara")
	elapsed := time.Since(start)
	m := jobURL.FindStringSubmatch(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || m == nil {
		t.Fatalf("large export: status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if elapsed > 2*time.Second {
		t.Errorf("enqueue took %v, want under 2s", elapsed)
	}
	jobID := m[1]
	var status, kind string
	var rows int
	var params []byte
	if err := f.tx.QueryRow(ctx, `SELECT r.status, r.row_count, r.params, j.kind FROM report_jobs r JOIN river_job j ON j.id = r.river_job_id WHERE r.id = $1`, jobID).
		Scan(&status, &rows, &params, &kind); err != nil {
		t.Fatal(err)
	}
	var p JobParams
	_ = json.Unmarshal(params, &p)
	if status != JobQueued || rows != 3 || kind != "report_export" || p.AsOf != "2026-10-18" {
		t.Errorf("job = %s, %d rows, kind %s, params %+v", status, rows, kind, p)
	}
	page := f.get("/laporan/jobs/"+jobID, "bendahara").Body.String()
	if !strings.Contains(page, "Menunggu antrean") || !strings.Contains(page, `http-equiv="refresh"`) {
		t.Error("status page while queued")
	}
	if rec := f.get("/laporan/jobs/"+jobID+"/unduh", "bendahara"); rec.Code != http.StatusNotFound {
		t.Errorf("download before done: status = %d, want 404", rec.Code)
	}

	if err := f.work(t, jobID, 1); err != nil {
		t.Fatal(err)
	}
	var key, name string
	_ = f.tx.QueryRow(ctx, `SELECT status, file_key, file_name FROM report_jobs WHERE id = $1`, jobID).Scan(&status, &key, &name)
	if status != JobDone || key != "schools/"+f.schoolA+"/reports/"+jobID+".xlsx" || name != "tunggakan-2026-10-18.xlsx" {
		t.Errorf("done job = %s, %s, %s", status, key, name)
	}
	page = f.get("/laporan/jobs/"+jobID, "bendahara").Body.String()
	if !strings.Contains(page, "Selesai") || strings.Contains(page, `http-equiv="refresh"`) || !strings.Contains(page, "/laporan/jobs/"+jobID+"/unduh") {
		t.Error("status page when done")
	}
	rec = f.get("/laporan/jobs/"+jobID+"/unduh", "bendahara")
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/storage/"+key+"?") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("download: status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	file := httptest.NewRecorder()
	f.store.Handler().ServeHTTP(file, httptest.NewRequest(http.MethodGet, rec.Header().Get("Location"), nil))
	if file.Code != http.StatusOK || !strings.Contains(file.Header().Get("Content-Disposition"), name) {
		t.Fatalf("signed file: %d %v", file.Code, file.Header())
	}
	checkXML(t, file.Body.Bytes())
	if a, b := sheetRows(t, file.Body.Bytes()), sheetRows(t, direct); a != b {
		t.Errorf("job file differs from direct export:\n%s\nvs\n%s", a, b)
	}
}

func sheetRows(t *testing.T, data []byte) string {
	t.Helper()
	x, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	out := ""
	for _, sheet := range []string{"Tunggakan", "Ringkasan"} {
		rows, _ := x.GetRows(sheet)
		for _, r := range rows {
			out += strings.Join(r, "|") + "\n"
		}
	}
	return out
}

func TestReportJob_Access(t *testing.T) {
	f := newJobFixture(t)
	f.h.maxRows = 0
	jobID := jobURL.FindStringSubmatch(f.get("/laporan/tunggakan.pdf", "bendahara").Header().Get("Location"))[1]
	if err := f.work(t, jobID, 1); err != nil {
		t.Fatal(err)
	}
	other, principal := f.user(t, "lain@uji.id"), f.user(t, "kepala@uji.id")
	for _, p := range []string{"/laporan/jobs/" + jobID, "/laporan/jobs/" + jobID + "/unduh"} {
		if rec := f.getAs(other, p, "bendahara"); rec.Code != http.StatusNotFound {
			t.Errorf("other user %s: status = %d, want 404", p, rec.Code)
		}
		if rec := f.getAs(other, p, "admin_tu"); rec.Code != http.StatusNotFound {
			t.Errorf("other admin %s: status = %d, want 404", p, rec.Code)
		}
	}
	if rec := f.getAs(principal, "/laporan/jobs/"+jobID+"/unduh", "kepala_sekolah"); rec.Code != http.StatusFound {
		t.Errorf("principal download: status = %d, want 302", rec.Code)
	}
	if rec := f.get("/laporan/jobs/"+jobID, "guru"); rec.Code != http.StatusForbidden {
		t.Errorf("teacher: status = %d, want 403", rec.Code)
	}
	if rec := f.get("/laporan/jobs/00000000-0000-0000-0000-000000000000", "bendahara"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown job: status = %d", rec.Code)
	}
}

func TestReportJob_FailureShowsSafeMessage(t *testing.T) {
	f := newJobFixture(t)
	ctx := context.Background()
	var userID string
	_ = f.tx.QueryRow(ctx, `SELECT id FROM users WHERE school_id = $1 AND email = 'guru@uji.id'`, f.schoolA).Scan(&userID)
	var jobID string
	// Kelas yang tidak ada: job pasti gagal.
	if err := f.tx.QueryRow(ctx, `INSERT INTO report_jobs (school_id, requested_by, report_type, format, params)
		VALUES ($1, $2, 'attendance', 'xlsx', '{"class_id": "00000000-0000-0000-0000-000000000000", "from": "2026-07-13", "to": "2026-10-18", "as_of": "2026-10-18"}')
		RETURNING id`, f.schoolA, userID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err := f.work(t, jobID, 1); err == nil {
		t.Fatal("job with unknown class succeeded")
	}
	var status string
	_ = f.tx.QueryRow(ctx, `SELECT status FROM report_jobs WHERE id = $1`, jobID).Scan(&status)
	if status != JobRunning {
		t.Errorf("after a retryable failure: status = %s, want berjalan", status)
	}
	if err := f.work(t, jobID, exportMaxAttempts); err == nil {
		t.Fatal("last attempt succeeded")
	}
	page := f.get("/laporan/jobs/"+jobID, "admin_tu").Body.String()
	if !strings.Contains(page, "Gagal") || !strings.Contains(page, "Laporan gagal dibuat.") || strings.Contains(page, "class not found") || strings.Contains(page, "attendance:") {
		t.Error("failed job must show a safe message without internal details")
	}
	if entries, _ := os.ReadDir(f.dir); len(entries) != 0 {
		t.Errorf("failed job left files: %v", entries)
	}
}

func TestReportJob_CleanupDeletesOldFiles(t *testing.T) {
	f := newJobFixture(t)
	ctx := context.Background()
	f.h.maxRows = 0
	ids := []string{}
	for i := 0; i < 2; i++ {
		id := jobURL.FindStringSubmatch(f.get("/laporan/tunggakan.xlsx", "bendahara").Header().Get("Location"))[1]
		if err := f.work(t, id, 1); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := f.tx.Exec(ctx, `UPDATE report_jobs SET finished_at = now() - interval '8 days' WHERE id = $1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.Exec(ctx, `UPDATE report_jobs SET finished_at = now() - interval '6 days' WHERE id = $1`, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err := NewCleanupWorker(f.tx, f.store, f.logger).Work(ctx, &river.Job[CleanupArgs]{JobRow: &rivertype.JobRow{}}); err != nil {
		t.Fatal(err)
	}
	exists := func(id string) bool {
		_, err := os.Stat(filepath.Join(f.dir, "schools", f.schoolA, "reports", id+".xlsx"))
		return err == nil
	}
	if exists(ids[0]) || !exists(ids[1]) {
		t.Errorf("after cleanup: old exists = %v (want false), recent exists = %v (want true)", exists(ids[0]), exists(ids[1]))
	}
	page := f.get("/laporan/jobs/"+ids[0], "bendahara").Body.String()
	if !strings.Contains(page, "File sudah dihapus otomatis") {
		t.Error("status page of a cleaned job")
	}
	if rec := f.get("/laporan/jobs/"+ids[0]+"/unduh", "bendahara"); rec.Code != http.StatusNotFound {
		t.Errorf("download of a deleted file: status = %d, want 404", rec.Code)
	}
	if p := CleanupPeriodicJob(); p == nil {
		t.Error("cleanup periodic job not configured")
	}
}
