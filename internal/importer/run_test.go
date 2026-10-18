package importer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"akra/internal/auth"
)

// fakeJobs mencatat job yang dimasukkan tanpa menjalankannya.
type fakeJobs struct {
	args []river.JobArgs
}

func (f *fakeJobs) InsertTx(_ context.Context, _ pgx.Tx, args river.JobArgs, _ *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	f.args = append(f.args, args)
	return &rivertype.JobInsertResult{Job: &rivertype.JobRow{ID: int64(len(f.args))}}, nil
}

// uploadAndValidate mengunggah rows (tanpa header) dan mengembalikan id batch.
func (f *uploadFixture) uploadAndValidate(t *testing.T, rows ...[]any) string {
	t.Helper()
	data := buildXLSX(t, append([][]any{header()}, rows...))
	rec := f.do(uploadRequest(t, "impor.xlsx", data), f.schoolA, f.adminA, auth.RoleAdminTU)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload: status = %d", rec.Code)
	}
	return strings.TrimPrefix(rec.Header().Get("Location"), "/impor/batch/")
}

func (f *uploadFixture) runBatch(t *testing.T, batchID string, roles ...string) *httptest.ResponseRecorder {
	t.Helper()
	if roles == nil {
		roles = []string{auth.RoleAdminTU}
	}
	return f.do(httptest.NewRequest(http.MethodPost, "/impor/batch/"+batchID+"/simpan", nil), f.schoolA, f.adminA, roles...)
}

func (f *uploadFixture) batch(t *testing.T, id string) Batch {
	t.Helper()
	b, err := NewService(f.tx, nil).GetBatch(context.Background(), f.schoolA, id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRun_SyncSavesStudentsClassesAndGuardians(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	// Siswa lama 003 di kelas 1A; impor memindahkannya ke 1B.
	var oldID string
	if err := f.tx.QueryRow(ctx, `INSERT INTO students (school_id, nis, full_name, birth_date) VALUES ($1, '003', 'Nama Lama', '2018-01-01') RETURNING id`, f.schoolA).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.Exec(ctx, `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
		SELECT $1, $2, id, academic_year_id, '2026-07-13' FROM classes WHERE id = $3`, f.schoolA, oldID, f.class1A); err != nil {
		t.Fatal(err)
	}
	students := f.count(t, `SELECT count(*) FROM students WHERE school_id = $1`, f.schoolA)

	moved := student("003", "Citra Baru")
	moved[ColClass], moved[ColBirthDate], moved[ColGuardianName], moved[ColGuardianPhone], moved[ColGuardianEmail] = "1b", "", "", "", ""
	sibling := student("002", "Budi")
	sibling[ColGuardianName], sibling[ColGuardianPhone] = "Wali Ani", "081234567890" // wali yang sama dengan Ani
	bad := student("004", "Dodi")
	bad[ColClass] = "9Z"
	batchID := f.uploadAndValidate(t, student("001", "Ani"), sibling, moved, bad)

	rec := f.runBatch(t, batchID)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("run: status = %d, body %s", rec.Code, rec.Body.String())
	}
	b := f.batch(t, batchID)
	if b.Status != StatusDone || b.Mode != "sync" || b.CreatedCount != 2 || b.UpdatedCount != 1 || b.FailedCount != 0 || b.ProcessedRows != 3 {
		t.Errorf("batch = %+v", b)
	}
	if len(f.jobs.args) != 0 {
		t.Errorf("sync run enqueued %d jobs", len(f.jobs.args))
	}
	if n := f.count(t, `SELECT count(*) FROM students WHERE school_id = $1`, f.schoolA); n != students+2 {
		t.Errorf("students = %d, want %d", n, students+2)
	}

	// Siswa yang diperbarui: nama berubah, tanggal lahir lama tidak terhapus,
	// pindah ke 1B dengan satu enrollment aktif.
	var name, birth string
	if err := f.tx.QueryRow(ctx, `SELECT full_name, birth_date::text FROM students WHERE id = $1`, oldID).Scan(&name, &birth); err != nil || name != "Citra Baru" || birth != "2018-01-01" {
		t.Errorf("updated student = %q %q, %v", name, birth, err)
	}
	var activeClass string
	if n := f.count(t, `SELECT count(*) FROM enrollments WHERE student_id = $1 AND end_date IS NULL`, oldID); n != 1 {
		t.Errorf("active enrollments = %d", n)
	}
	if err := f.tx.QueryRow(ctx, `SELECT class_id FROM enrollments WHERE student_id = $1 AND end_date IS NULL`, oldID).Scan(&activeClass); err != nil || activeClass != f.class1B {
		t.Errorf("active class = %q, %v", activeClass, err)
	}
	// Siswa baru masuk kelas tahun aktif.
	if n := f.count(t, `SELECT count(*) FROM enrollments e JOIN students s ON s.id = e.student_id
		WHERE s.school_id = $1 AND s.nis IN ('001', '002') AND e.class_id = $2 AND e.end_date IS NULL`, f.schoolA, f.class1A); n != 2 {
		t.Errorf("new students in 1A = %d", n)
	}
	// Ani dan Budi berbagi satu wali (nama dan telepon sama), wali utama.
	if n := f.count(t, `SELECT count(*) FROM guardians WHERE school_id = $1`, f.schoolA); n != 1 {
		t.Errorf("guardians = %d, want 1", n)
	}
	if n := f.count(t, `SELECT count(*) FROM student_guardians WHERE school_id = $1 AND is_primary AND relationship = 'wali'`, f.schoolA); n != 2 {
		t.Errorf("primary guardian links = %d", n)
	}
	var phone, email string
	if err := f.tx.QueryRow(ctx, `SELECT phone, email FROM guardians WHERE school_id = $1`, f.schoolA).Scan(&phone, &email); err != nil || phone != "+6281234567890" || email != "wali@contoh.id" {
		t.Errorf("guardian = %q %q, %v", phone, email, err)
	}

	// Satu audit import.run berisi ringkasan, tanpa data pribadi.
	var after string
	if err := f.tx.QueryRow(ctx, `SELECT after::text FROM audit_logs WHERE school_id = $1 AND action = 'import.run' AND entity_id = $2`, f.schoolA, batchID).Scan(&after); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if !strings.Contains(after, `"created": 2`) || !strings.Contains(after, `"updated": 1`) || strings.Contains(after, "Ani") {
		t.Errorf("audit after = %s", after)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'import.run'`, f.schoolA); n != 1 {
		t.Errorf("import.run audits = %d", n)
	}

	page := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID, nil), f.schoolA, f.adminA, auth.RoleAdminTU).Body.String()
	if !strings.Contains(page, "2 siswa baru, 1 diperbarui, 0 dilewati.") || !strings.Contains(page, `badge-success">Selesai`) {
		t.Error("batch page misses the result")
	}

	// Batch yang sudah selesai tidak bisa dijalankan ulang.
	if rec := f.runBatch(t, batchID); rec.Code != http.StatusConflict {
		t.Errorf("rerun: status = %d", rec.Code)
	}
}

// Impor file yang sama dua kali: tidak ada siswa atau wali ganda.
func TestRun_SameFileTwice(t *testing.T) {
	f := newUploadFixture(t)
	rows := [][]any{student("001", "Ani"), student("002", "Budi")}
	for i := range 2 {
		id := f.uploadAndValidate(t, rows...)
		if rec := f.runBatch(t, id); rec.Code != http.StatusSeeOther {
			t.Fatalf("run %d: status = %d", i, rec.Code)
		}
		b := f.batch(t, id)
		if i == 1 && (b.CreatedCount != 0 || b.UpdatedCount != 2) {
			t.Errorf("second import = %+v", b)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM students WHERE school_id = $1`, f.schoolA); n != 2 {
		t.Errorf("students = %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM guardians WHERE school_id = $1`, f.schoolA); n != 2 {
		t.Errorf("guardians = %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM enrollments WHERE school_id = $1 AND end_date IS NULL`, f.schoolA); n != 2 {
		t.Errorf("active enrollments = %d", n)
	}
}

// Kelas yang dihapus setelah validasi: baris itu gagal, baris lain tetap
// tersimpan.
func TestRun_RowFailureDoesNotStopOthers(t *testing.T) {
	f := newUploadFixture(t)
	r2 := student("002", "Budi")
	r2[ColClass] = "1B"
	batchID := f.uploadAndValidate(t, student("001", "Ani"), r2)
	if _, err := f.tx.Exec(context.Background(), `DELETE FROM classes WHERE id = $1`, f.class1B); err != nil {
		t.Fatal(err)
	}
	if rec := f.runBatch(t, batchID); rec.Code != http.StatusSeeOther {
		t.Fatalf("run: status = %d", rec.Code)
	}
	b := f.batch(t, batchID)
	if b.Status != StatusDone || b.CreatedCount != 1 || b.FailedCount != 1 {
		t.Errorf("batch = %+v", b)
	}
	// Savepoint dibatalkan: siswa Budi tidak tersimpan tanpa kelas.
	if n := f.count(t, `SELECT count(*) FROM students WHERE school_id = $1 AND nis = '002'`, f.schoolA); n != 0 {
		t.Errorf("failed row left a student behind")
	}
	failed, err := NewService(f.tx, nil).FailedRows(context.Background(), f.schoolA, batchID, 10)
	if err != nil || len(failed) != 1 || failed[0].Line != 3 || !strings.Contains(failed[0].Errors[0].Message, "Kelas 1B sudah tidak ada") {
		t.Errorf("failed rows = %+v, %v", failed, err)
	}
	page := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID, nil), f.schoolA, f.adminA, auth.RoleAdminTU).Body.String()
	if !strings.Contains(page, "Baris yang gagal disimpan") || !strings.Contains(page, "1 siswa baru, 0 diperbarui, 0 dilewati, 1 gagal.") {
		t.Error("batch page misses failed rows")
	}
}

// Lebih dari 50 baris valid: job River. Job yang gagal setelah transaksi
// pertama lalu diulang tidak membuat siswa ganda.
func TestRun_AsyncJobResumesAfterFailure(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	var rows [][]any
	for i := range 120 {
		rows = append(rows, []any{fmt.Sprintf("%04d", i), fmt.Sprintf("Siswa %d", i), "", "", "1A"})
	}
	batchID := f.uploadAndValidate(t, rows...)
	if rec := f.runBatch(t, batchID); rec.Code != http.StatusSeeOther {
		t.Fatalf("run: status = %d", rec.Code)
	}
	b := f.batch(t, batchID)
	if b.Status != StatusRunning || b.Mode != "async" || len(f.jobs.args) != 1 {
		t.Fatalf("batch = %+v, jobs %d", b, len(f.jobs.args))
	}
	args := f.jobs.args[0].(ImportStudentsArgs)
	if args.BatchID != batchID || args.SchoolID != f.schoolA {
		t.Errorf("job args = %+v", args)
	}
	// Halaman progres memperbarui diri sendiri.
	page := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID, nil), f.schoolA, f.adminA, auth.RoleAdminTU).Body.String()
	if !strings.Contains(page, `http-equiv="refresh"`) || !strings.Contains(page, "0 dari 120 baris diproses") {
		t.Error("progress page")
	}

	w := NewImportStudentsWorker(f.tx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.failAfterChunks = 1
	if err := w.run(ctx, args); err == nil {
		t.Fatal("injected failure did not stop the job")
	}
	b = f.batch(t, batchID)
	if b.Status != StatusRunning || b.ProcessedRows != runChunkSize || b.CreatedCount != runChunkSize {
		t.Errorf("after failure = %+v", b)
	}
	page = f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID, nil), f.schoolA, f.adminA, auth.RoleAdminTU).Body.String()
	if !strings.Contains(page, "100 dari 120 baris diproses (83%)") {
		t.Error("progress page does not show progress")
	}

	w.failAfterChunks = 0
	if err := w.run(ctx, args); err != nil {
		t.Fatal(err)
	}
	b = f.batch(t, batchID)
	if b.Status != StatusDone || b.ProcessedRows != 120 || b.CreatedCount != 120 || b.UpdatedCount != 0 {
		t.Errorf("after retry = %+v", b)
	}
	if n := f.count(t, `SELECT count(*) FROM students WHERE school_id = $1`, f.schoolA); n != 120 {
		t.Errorf("students = %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'import.run'`, f.schoolA); n != 1 {
		t.Errorf("import.run audits = %d", n)
	}
	// Job yang terulang setelah selesai tidak melakukan apa-apa.
	if err := w.run(ctx, args); err != nil {
		t.Errorf("run after done: %v", err)
	}
}

func TestRun_FinalFailureMarksBatch(t *testing.T) {
	f := newUploadFixture(t)
	var rows [][]any
	for i := range 60 {
		rows = append(rows, []any{fmt.Sprintf("%04d", i), "Siswa", "", "", "1A"})
	}
	batchID := f.uploadAndValidate(t, rows...)
	f.runBatch(t, batchID)
	w := NewImportStudentsWorker(f.tx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.markFailed(ImportStudentsArgs{SchoolID: f.schoolA, BatchID: batchID}, fmt.Errorf("boom"))
	b := f.batch(t, batchID)
	if b.Status != StatusFailed || !strings.Contains(b.Error, "gangguan sistem") || strings.Contains(b.Error, "boom") {
		t.Errorf("batch = %+v", b)
	}
	page := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID, nil), f.schoolA, f.adminA, auth.RoleAdminTU).Body.String()
	if !strings.Contains(page, "alert-error") || strings.Contains(page, `http-equiv="refresh"`) {
		t.Error("failed batch page")
	}
}

func TestRun_AccessAndNothingToImport(t *testing.T) {
	f := newUploadFixture(t)
	bad := student("001", "Ani")
	bad[ColClass] = "9Z"
	batchID := f.uploadAndValidate(t, bad)
	if rec := f.runBatch(t, batchID); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Tidak ada baris yang siap disimpan") {
		t.Errorf("nothing to import: status = %d", rec.Code)
	}
	ok := f.uploadAndValidate(t, student("002", "Budi"))
	for _, role := range []string{auth.RoleGuru, auth.RoleBendahara, auth.RoleKepalaSekolah, auth.RoleOrangTua} {
		if rec := f.runBatch(t, ok, role); rec.Code != http.StatusForbidden {
			t.Errorf("run as %s: status = %d", role, rec.Code)
		}
	}
	// Admin sekolah lain.
	rec := f.do(httptest.NewRequest(http.MethodPost, "/impor/batch/"+ok+"/simpan", nil), f.schoolB, f.adminB, auth.RoleAdminTU)
	if rec.Code != http.StatusNotFound {
		t.Errorf("other school: status = %d", rec.Code)
	}
	if b := f.batch(t, ok); b.Status != StatusValidated {
		t.Errorf("batch changed: %+v", b)
	}
}
