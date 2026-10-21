package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/jobs"
	"akra/internal/platform/money"
	"akra/internal/platform/render"
	"akra/internal/platform/storage"
	"akra/internal/reports/builder"
	"akra/internal/reports/export"
	"akra/internal/school"
	"akra/internal/testutil"
	"akra/web"
)

type customEnv struct {
	t      *testing.T
	tx     pgx.Tx
	a, b   *testutil.School
	h      *Handler
	svc    *builder.ConfigService
	router http.Handler
	store  *storage.Local
	dir    string
	logger *slog.Logger
	today  time.Time
}

func newCustomEnv(t *testing.T) *customEnv {
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
	hash, err := auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1}).HashPassword(testutil.Password)
	if err != nil {
		t.Fatal(err)
	}
	e := &customEnv{t: t, tx: tx, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), today: time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)}
	e.a, e.b = testutil.SeedTwoSchools(t, tx, hash)
	e.dir = t.TempDir()
	if e.store, err = storage.NewLocal(e.dir); err != nil {
		t.Fatal(err)
	}
	inserter, err := jobs.NewClient(pool, nil, jobs.Config{}, e.logger)
	if err != nil {
		t.Fatal(err)
	}
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, e.logger)
	if err != nil {
		t.Fatal(err)
	}
	e.h = NewHandler(tx, inserter, e.store, rd, e.logger)
	e.h.today = func() time.Time { return e.today }
	e.svc = builder.NewConfigService(tx, builder.Default)
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, e.logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			id := auth.Identity{UserID: req.Header.Get("X-User"), SchoolID: e.a.ID, Roles: strings.Split(req.Header.Get("X-Roles"), ","),
				FullName: "Uji", SchoolName: "SD Uji " + e.a.Marker}
			next.ServeHTTP(w, req.WithContext(auth.ContextWithIdentity(req.Context(), id)))
		})
	})
	builder.NewConfigHandler(e.svc, e.h, sm, rd, e.logger).Routes(r)
	e.h.Routes(r)
	e.router = r
	return e
}

func (e *customEnv) get(path string, u testutil.User, role string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-User", u.ID)
	req.Header.Set("X-Roles", role)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *customEnv) save(u testutil.User, name string, c builder.Config, shared bool) builder.SavedConfig {
	e.t.Helper()
	saved, err := e.svc.Create(context.Background(), school.Actor{SchoolID: e.a.ID, UserID: u.ID},
		auth.Identity{SchoolID: e.a.ID, UserID: u.ID, Roles: []string{u.Role}}, builder.ConfigInput{Name: name, Config: c, Shared: shared})
	if err != nil {
		e.t.Fatal(err)
	}
	return saved
}

func readXLSX(t *testing.T, data []byte) (rows [][]string, info string) {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	defer f.Close()
	if rows, err = f.GetRows("Laporan"); err != nil {
		t.Fatalf("sheet Laporan: %v", err)
	}
	infoRows, _ := f.GetRows("Info")
	for _, r := range infoRows {
		info += strings.Join(r, " ") + "\n"
	}
	return rows, info
}

// TestCustomExport_MatchesPreview: isi Excel sama dengan pratinjau T78 untuk
// konfigurasi yang sama, dan file memuat nama sekolah serta tanggal cetak.
func TestCustomExport_MatchesPreview(t *testing.T) {
	e := newCustomEnv(t)
	cfg := builder.Config{Source: "payments", Columns: []string{"receipt_number", "student_name", "amount", "payment_date", "method", "status"},
		Order: []builder.Order{{Column: "amount", Desc: true}}}
	saved := e.save(e.a.Treasurer, "Pembayaran Oktober", cfg, false)

	rec := e.get("/laporan/konfigurasi/"+saved.ID+".xlsx", e.a.Treasurer, "bendahara")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "spreadsheetml") ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "laporan-pembayaran-oktober-2026-10-18.xlsx") {
		t.Fatalf("xlsx: status = %d, type %q, disposition %q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Content-Disposition"))
	}
	rows, info := readXLSX(t, rec.Body.Bytes())
	preview, err := e.svc.Preview(context.Background(), auth.Identity{SchoolID: e.a.ID, UserID: e.a.Treasurer.ID, Roles: []string{"bendahara"}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(preview.Rows)+1 || len(preview.Rows) == 0 {
		t.Fatalf("xlsx rows %d, preview rows %d", len(rows)-1, len(preview.Rows))
	}
	for i, col := range preview.Columns {
		if rows[0][i] != col.Label {
			t.Errorf("header %d: %q, want %q", i, rows[0][i], col.Label)
		}
	}
	book, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	for r, want := range preview.Rows {
		for c, cell := range rows[r+1] {
			got := cell
			if preview.Columns[c].Type == builder.TypeMoney {
				// Excel menyimpan angka (format "Rp"#,##0); bandingkan nominalnya.
				ref, _ := excelize.CoordinatesToCellName(c+1, r+2)
				raw, _ := book.GetCellValue("Laporan", ref, excelize.Options{RawCellValue: true})
				n, err := strconv.ParseInt(raw, 10, 64)
				if err != nil {
					t.Fatalf("money cell %s = %q", ref, raw)
				}
				got = money.FormatRupiah(n)
			}
			if got != want[c] {
				t.Errorf("row %d col %s: excel %q, preview %q", r, preview.Columns[c].Key, got, want[c])
			}
		}
	}
	if !strings.Contains(info, "SD Uji "+e.a.Marker) || !strings.Contains(info, "18 Oktober 2026") || !strings.Contains(info, "Pembayaran Oktober") {
		t.Errorf("info sheet: %s", info)
	}

	pdf := e.get("/laporan/konfigurasi/"+saved.ID+".pdf", e.a.Treasurer, "bendahara")
	if pdf.Code != http.StatusOK || pdf.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(pdf.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("pdf: status = %d", pdf.Code)
	}
	// PDF (tanpa kompresi) memuat judul, sekolah, tanggal cetak, dan isi tabel.
	table, _, err := builder.ExportTable(context.Background(), e.tx, builder.Default, e.a.ID, "SD Uji "+e.a.Marker, saved.Name, cfg, e.today, builder.Options{})
	if err != nil {
		t.Fatal(err)
	}
	table.Uncompressed = true
	var buf bytes.Buffer
	if err := export.WritePDF(&buf, table); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Pembayaran Oktober", "SD Uji " + e.a.Marker, "18 Oktober 2026", "Sumber: Pembayaran", "Rp100.000"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("pdf misses %q", want)
		}
	}
}

func TestCustomExport_Access(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	cfg := builder.Config{Source: "attendance", Columns: []string{"nis", "status"}}
	private := e.save(e.a.Admin, "Pribadi", cfg, false)
	shared := e.save(e.a.Admin, "Bersama", cfg, true)
	for _, ext := range []string{".xlsx", ".pdf"} {
		// Pemilik dan konfigurasi bersama boleh.
		if rec := e.get("/laporan/konfigurasi/"+private.ID+ext, e.a.Admin, "admin_tu"); rec.Code != http.StatusOK {
			t.Errorf("owner %s: %d", ext, rec.Code)
		}
		if rec := e.get("/laporan/konfigurasi/"+shared.ID+ext, e.a.Principal, "kepala_sekolah"); rec.Code != http.StatusOK {
			t.Errorf("shared %s for principal: %d", ext, rec.Code)
		}
		// Konfigurasi pribadi orang lain, sumber di luar hak peran, sekolah lain, ID rusak: 404.
		for _, tc := range []struct {
			path string
			u    testutil.User
			role string
		}{
			{"/laporan/konfigurasi/" + private.ID + ext, e.a.Principal, "kepala_sekolah"},
			{"/laporan/konfigurasi/" + shared.ID + ext, e.a.Treasurer, "bendahara"},
			{"/laporan/konfigurasi/" + e.b.ReportConfigID + ext, e.a.Admin, "admin_tu"},
			{"/laporan/konfigurasi/bukan" + ext, e.a.Admin, "admin_tu"},
		} {
			if rec := e.get(tc.path, tc.u, tc.role); rec.Code != http.StatusNotFound {
				t.Errorf("GET %s as %s: %d, want 404", tc.path, tc.role, rec.Code)
			}
		}
	}
	// Peran pemilik berubah (admin_tu -> bendahara): sumber absensi tidak lagi diizinkan.
	if rec := e.get("/laporan/konfigurasi/"+private.ID+".xlsx", e.a.Admin, "bendahara"); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "tidak lagi diizinkan") {
		t.Errorf("owner whose role changed: %d", rec.Code)
	}
	// Guru tidak punya sumber laporan.
	if rec := e.get("/laporan/konfigurasi/"+shared.ID+".xlsx", e.a.Teacher, "guru"); rec.Code != http.StatusForbidden {
		t.Errorf("teacher: %d", rec.Code)
	}
	// Konfigurasi yang kolomnya tidak terdaftar lagi: 422, tanpa file.
	if _, err := e.tx.Exec(ctx, `UPDATE report_configs SET columns = '["nis","kolom_lama"]' WHERE id = $1`, private.ID); err != nil {
		t.Fatal(err)
	}
	if rec := e.get("/laporan/konfigurasi/"+private.ID+".xlsx", e.a.Admin, "admin_tu"); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("stale config: %d", rec.Code)
	}
}

// TestCustomExport_LargeRunsAsJob: di atas batas ekspor langsung, laporan
// dibuat sebagai job T49 dengan salinan konfigurasi di params.
func TestCustomExport_LargeRunsAsJob(t *testing.T) {
	e := newCustomEnv(t)
	ctx := context.Background()
	if _, err := e.tx.Exec(ctx, `WITH s AS (
		INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by)
		SELECT $1, $2, $3, date '2026-07-14' + g, $5 FROM generate_series(0, 39) g RETURNING id)
		INSERT INTO attendance_records (school_id, session_id, enrollment_id, status) SELECT $1, s.id, $4, 'izin' FROM s`,
		e.a.ID, e.a.YearID, e.a.ClassID, e.a.EnrollmentIDs[0], e.a.Teacher.ID); err != nil {
		t.Fatal(err)
	}
	cfg := builder.Config{Source: "attendance", Columns: []string{"nis", "session_date", "status"},
		Filters: []builder.Filter{{Column: "status", Op: builder.OpEq, Value: "izin"}}}
	saved := e.save(e.a.Admin, "Izin semester", cfg, false)

	// Di bawah batas: langsung.
	if rec := e.get("/laporan/konfigurasi/"+saved.ID+".xlsx", e.a.Admin, "admin_tu"); rec.Code != http.StatusOK {
		t.Fatalf("small export: %d", rec.Code)
	}
	e.h.maxRows = 10 // 40 baris > 10
	rec := e.get("/laporan/konfigurasi/"+saved.ID+".xlsx", e.a.Admin, "admin_tu")
	m := jobURL.FindStringSubmatch(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || m == nil {
		t.Fatalf("large export: status = %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	var typ string
	var rows int
	var params []byte
	if err := e.tx.QueryRow(ctx, `SELECT report_type, row_count, params FROM report_jobs WHERE id = $1`, m[1]).Scan(&typ, &rows, &params); err != nil {
		t.Fatal(err)
	}
	var p JobParams
	if err := json.Unmarshal(params, &p); err != nil {
		t.Fatal(err)
	}
	if typ != TypeCustom || rows != 40 || p.Title != "Izin semester" || p.Config == nil || p.Config.Source != "attendance" || len(p.Config.Filters) != 1 || p.AsOf != "2026-10-18" {
		t.Fatalf("job: %s %d %+v", typ, rows, p)
	}
	// Konfigurasi diubah setelah diminta: job memakai salinan saat diminta.
	if _, err := e.tx.Exec(ctx, `UPDATE report_configs SET columns = '["nis"]' WHERE id = $1`, saved.ID); err != nil {
		t.Fatal(err)
	}
	w := NewExportWorker(e.tx, e.store, e.logger)
	if err := w.export(ctx, ExportArgs{SchoolID: e.a.ID, JobID: m[1]}); err != nil {
		t.Fatal(err)
	}
	var key, name, status string
	if err := e.tx.QueryRow(ctx, `SELECT status, file_key, file_name FROM report_jobs WHERE id = $1`, m[1]).Scan(&status, &key, &name); err != nil {
		t.Fatal(err)
	}
	if status != JobDone || name != "laporan-izin-semester-2026-10-18.xlsx" {
		t.Fatalf("job done: %s %s", status, name)
	}
	data, err := os.ReadFile(filepath.Join(e.dir, key))
	if err != nil {
		t.Fatal(err)
	}
	got, info := readXLSX(t, data)
	if len(got) != 41 || len(got[0]) != 3 || got[0][1] != "Tanggal" || got[1][1] != clock.FormatDateID(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("job file: %d rows, header %v, first %v", len(got), got[0], got[1])
	}
	if !strings.Contains(info, "18 Oktober 2026") {
		t.Errorf("job info sheet: %s", info)
	}
}
