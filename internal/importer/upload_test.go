package importer

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/web"
)

type uploadFixture struct {
	tx               pgx.Tx
	h                http.Handler
	schoolA, schoolB string
	adminA, adminB   string
}

func newUploadFixture(t *testing.T) *uploadFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f := &uploadFixture{tx: tx}
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('SD A', 'test-imp-a') RETURNING id`)
	f.schoolB = one(`INSERT INTO schools (name, code) VALUES ('SD B', 'test-imp-b') RETURNING id`)
	f.adminA = one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'admin@a.id', 'x', 'Admin A') RETURNING id`, f.schoolA)
	f.adminB = one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'admin@b.id', 'x', 'Admin B') RETURNING id`, f.schoolB)
	// Sekolah A punya tahun ajaran aktif dengan kelas 1A dan 1B; sekolah B belum.
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, f.schoolA)
	old := one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2025/2026', '2025-07-14', '2026-06-26') RETURNING id`, f.schoolA)
	for _, c := range []struct{ year, name string }{{year, "1A"}, {year, "1B"}, {old, "6A"}} {
		one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 1) RETURNING id`, f.schoolA, c.year, c.name)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, static, err := web.Load("")
	if err != nil {
		t.Fatal(err)
	}
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	h := NewHandler(NewService(tx), sm, rd, logger)
	h.today = func() time.Time { return testToday }
	h.Routes(r)
	f.h = r
	return f
}

func (f *uploadFixture) do(req *http.Request, school, user string, roles ...string) *httptest.ResponseRecorder {
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), auth.Identity{UserID: user, SchoolID: school, FullName: "Admin", Roles: roles}))
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func uploadRequest(t *testing.T, filename string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if filename != "" {
		w, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/impor/siswa", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func (f *uploadFixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.tx.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUpload_CreatesBatchWithoutTouchingStudents(t *testing.T) {
	f := newUploadFixture(t)
	students := f.count(t, `SELECT count(*) FROM students`)
	data := buildXLSX(t, [][]any{header(), student("001", "Ani"), {}, student("002", "Budi")})
	rec := f.do(uploadRequest(t, `C:\Users\tu\Data Siswa.xlsx`, data), f.schoolA, f.adminA, auth.RoleAdminTU)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/impor/batch/") {
		t.Fatalf("upload: status = %d, location %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	batchID := strings.TrimPrefix(rec.Header().Get("Location"), "/impor/batch/")

	b, err := NewService(f.tx).GetBatch(context.Background(), f.schoolA, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusValidated || b.ValidRows != 2 || b.ErrorRows != 0 || b.TotalRows != 2 || b.SkippedRows != 1 || b.FileName != "Data Siswa.xlsx" || b.UploadedBy != "Admin A" {
		t.Errorf("batch = %+v", b)
	}
	if len(b.Skipped) != 1 || b.Skipped[0] != (Skip{Line: 3, Reason: SkipEmpty}) || len(b.Preview) != 2 || b.Preview[1].Values[ColNIS] != "002" {
		t.Errorf("skipped %v, preview %+v", b.Skipped, b.Preview)
	}
	// Setelah validasi, data baris ada di import_rows, bukan parsed_rows.
	if rows, err := NewService(f.tx).ParsedRows(context.Background(), f.schoolA, batchID); err != nil || len(rows) != 0 {
		t.Errorf("parsed rows after validation = %+v, %v", rows, err)
	}
	if n := f.count(t, `SELECT count(*) FROM import_rows WHERE batch_id = $1`, batchID); n != 3 {
		t.Errorf("import_rows = %d, want 2 valid + 1 skipped", n)
	}
	if n := f.count(t, `SELECT count(*) FROM students`); n != students {
		t.Errorf("students changed: %d -> %d", students, n)
	}

	page := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID, nil), f.schoolA, f.adminA, auth.RoleAdminTU)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "Data Siswa.xlsx") || !strings.Contains(body, "Budi") || !strings.Contains(body, "Baris 3 · kosong") {
		t.Errorf("batch page: status = %d", page.Code)
	}

	// Sekolah lain tidak bisa melihat batch ini.
	if rec := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID, nil), f.schoolB, f.adminB, auth.RoleAdminTU); rec.Code != http.StatusNotFound {
		t.Errorf("other school: status = %d", rec.Code)
	}
	if _, err := NewService(f.tx).ParsedRows(context.Background(), f.schoolB, batchID); err != ErrBatchNotFound {
		t.Errorf("other school parsed rows: %v", err)
	}
	if rec := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/bukan-uuid", nil), f.schoolA, f.adminA, auth.RoleAdminTU); rec.Code != http.StatusNotFound {
		t.Errorf("invalid id: status = %d", rec.Code)
	}
}

func TestUpload_RejectedFiles(t *testing.T) {
	f := newUploadFixture(t)
	valid := buildXLSX(t, [][]any{header(), student("001", "Ani")})
	noNIS := buildXLSX(t, [][]any{header()[1:], student("001", "Ani")[1:]})
	csv := []byte("NIS,Nama lengkap\n001,Ani\n")
	cases := []struct {
		name, filename string
		data           []byte
		want           string
	}{
		{"no file", "", nil, "Pilih file Excel"},
		{"csv renamed to xlsx", "siswa.xlsx", csv, "harus berformat Excel .xlsx"},
		{"xlsx renamed to csv", "siswa.csv", valid, "harus berformat Excel .xlsx"},
		{"missing NIS column", "siswa.xlsx", noNIS, "Kolom &#34;NIS&#34; tidak ditemukan"},
		{"6 MB", "besar.xlsx", padXLSX(t, valid, 6<<20), "Maksimal 5 MB"},
	}
	for _, c := range cases {
		rec := f.do(uploadRequest(t, c.filename, c.data), f.schoolA, f.adminA, auth.RoleAdminTU)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: status = %d, want message %q", c.name, rec.Code, c.want)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM import_batches WHERE school_id = $1`, f.schoolA); n != 0 {
		t.Errorf("rejected uploads created %d batches", n)
	}
	// File 4 MB diterima.
	if rec := f.do(uploadRequest(t, "empat.xlsx", padXLSX(t, valid, 4<<20)), f.schoolA, f.adminA, auth.RoleAdminTU); rec.Code != http.StatusSeeOther {
		t.Errorf("4 MB: status = %d", rec.Code)
	}
}

func TestUpload_Access(t *testing.T) {
	h := newRouter(t)
	for _, role := range []string{auth.RoleGuru, auth.RoleBendahara, auth.RoleKepalaSekolah, auth.RoleOrangTua} {
		req := uploadRequest(t, "a.xlsx", []byte("x"))
		req = req.WithContext(auth.ContextWithIdentity(req.Context(), auth.Identity{UserID: "u1", SchoolID: "s1", Roles: []string{role}}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("upload as %s: status = %d", role, rec.Code)
		}
		if rec := get(h, "/impor/batch/00000000-0000-0000-0000-000000000000", role); rec.Code != http.StatusForbidden {
			t.Errorf("batch as %s: status = %d", role, rec.Code)
		}
	}
}
