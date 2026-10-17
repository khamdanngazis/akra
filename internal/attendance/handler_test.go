package attendance

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/render"
	"akra/web"
)

type hEnv struct {
	t       *testing.T
	tx      pgx.Tx
	router  http.Handler
	schoolA string
	class1A string // wali kelas: guru1, 30 siswa
	class1B string // wali kelas: guru2
	guru1   string
	guru2   string
	admin   string
	today   time.Time
	cookies map[string]*http.Cookie // dibawa antar-request, seperti browser
}

const idHeader = "X-Test-Identity" // "<user-id>|<role,role>"

func newHEnv(t *testing.T) *hEnv {
	t.Helper()
	pool := connect(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	e := &hEnv{t: t, tx: tx, today: clock.Today(), cookies: map[string]*http.Cookie{}}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	e.schoolA = one(`INSERT INTO schools (name, code) VALUES ('A', 'test-atth-a') RETURNING id`)
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, 'Aktif', $2, $3, true) RETURNING id`,
		e.schoolA, e.today.AddDate(0, -2, 0), e.today.AddDate(0, 9, 0))
	user := `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, $2, 'x', $2) RETURNING id`
	e.guru1 = one(user, e.schoolA, "guru1@a.id")
	e.guru2 = one(user, e.schoolA, "guru2@a.id")
	e.admin = one(user, e.schoolA, "admin@a.id")
	e.class1A = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level, homeroom_teacher_id) VALUES ($1, $2, '1A', 1, $3) RETURNING id`, e.schoolA, year, e.guru1)
	e.class1B = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level, homeroom_teacher_id) VALUES ($1, $2, '1B', 1, $3) RETURNING id`, e.schoolA, year, e.guru2)
	if _, err := tx.Exec(ctx, `
		WITH s AS (
			INSERT INTO students (school_id, nis, full_name)
			SELECT $1, lpad(g::text, 3, '0'), 'Siswa ' || lpad(g::text, 2, '0') FROM generate_series(1, 30) g
			RETURNING id)
		INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
		SELECT $1, s.id, $2, $3, $4 FROM s`, e.schoolA, e.class1A, year, e.today.AddDate(0, -2, 0)); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	h := NewHandler(NewService(tx), tx, sm, rd, logger)

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, roles, _ := strings.Cut(r.Header.Get(idHeader), "|")
			id := auth.Identity{UserID: userID, SchoolID: e.schoolA, Roles: strings.Split(roles, ","), FullName: "Uji", SchoolName: "A"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	h.Routes(r)
	e.router = r
	return e
}

func (e *hEnv) do(method, path, userID, roles string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set(idHeader, userID+"|"+roles)
	for _, c := range e.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		e.cookies[c.Name] = c
	}
	return rec
}

var (
	sessionAction = regexp.MustCompile(`action="/absensi/([0-9a-f-]{36})"`)
	statusField   = regexp.MustCompile(`name="s_([0-9a-f-]{36})" value="hadir"`)
)

// open membuka form kelas pada tanggal (YYYY-MM-DD) dan mengembalikan ID sesi
// serta ID enrollment yang tampil.
func (e *hEnv) open(classID, date, userID string) (string, []string) {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/absensi?kelas="+classID+"&tanggal="+date, userID, "guru", nil)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("open form: status = %d", rec.Code)
	}
	m := sessionAction.FindStringSubmatch(rec.Body.String())
	if m == nil {
		e.t.Fatal("form has no session action")
	}
	var ids []string
	for _, f := range statusField.FindAllStringSubmatch(rec.Body.String(), -1) {
		ids = append(ids, f[1])
	}
	return m[1], ids
}

func (e *hEnv) counts(sessionID string) map[string]int {
	rows, _ := e.tx.Query(context.Background(), `SELECT status, count(*) FROM attendance_records WHERE session_id = $1 GROUP BY status`, sessionID)
	got := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		_ = rows.Scan(&s, &n)
		got[s] = n
	}
	return got
}

func TestAttendanceHandler_HomeroomTeacherFillsAttendance(t *testing.T) {
	e := newHEnv(t)
	date := e.today.Format(dateLayout)

	// Tanpa ?kelas, wali kelas dengan satu kelas langsung diarahkan.
	if rec := e.do(http.MethodGet, "/absensi", e.guru1, "guru", nil); rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), e.class1A) {
		t.Errorf("GET /absensi: status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}

	sessionID, ids := e.open(e.class1A, date, e.guru1)
	if len(ids) != 30 {
		t.Fatalf("form shows %d students, want 30", len(ids))
	}

	// Simulasi guru mengisi 30 siswa: semua hadir kecuali 3 sakit, 2 izin, 1 alpa.
	form := url.Values{}
	for i, id := range ids {
		st := StatusPresent
		switch {
		case i < 3:
			st = StatusSick
		case i < 5:
			st = StatusExcused
		case i < 6:
			st = StatusAbsent
		}
		form.Set("s_"+id, st)
	}
	start := time.Now()
	rec := e.do(http.MethodPost, "/absensi/"+sessionID, e.guru1, "guru", form)
	elapsed := time.Since(start)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: status = %d\n%s", rec.Code, rec.Body.String())
	}
	t.Logf("saving 30 students took %v", elapsed)
	if elapsed > 2*time.Second {
		t.Errorf("saving took %v", elapsed)
	}
	if got := e.counts(sessionID); got["hadir"] != 24 || got["sakit"] != 3 || got["izin"] != 2 || got["alpa"] != 1 {
		t.Errorf("stored counts = %v", got)
	}
	var updatedBy string
	_ = e.tx.QueryRow(context.Background(), `SELECT updated_by::text FROM attendance_records WHERE session_id = $1 AND status = 'alpa'`, sessionID).Scan(&updatedBy)
	if updatedBy != e.guru1 {
		t.Errorf("updated_by = %q, want guru1", updatedBy)
	}

	// Setelah simpan: ringkasan tampil.
	body := e.do(http.MethodGet, rec.Header().Get("Location"), e.guru1, "guru", nil).Body.String()
	if !strings.Contains(body, "tersimpan: 24 hadir, 3 sakit, 2 izin, 1 alpa") || !strings.Contains(body, "24 hadir") {
		t.Error("summary after save is missing")
	}
	// Membuka ulang: sesi sama, status tetap.
	again, _ := e.open(e.class1A, date, e.guru1)
	if again != sessionID {
		t.Error("reopening created another session")
	}
}

func TestAttendanceHandler_Access(t *testing.T) {
	e := newHEnv(t)
	date := e.today.Format(dateLayout)
	sessionID, ids := e.open(e.class1A, date, e.guru1)
	form := url.Values{"s_" + ids[0]: {StatusAbsent}}

	cases := []struct {
		name, user, roles string
	}{
		{"other teacher", e.guru2, "guru"},
		{"admin", e.admin, "admin_tu"},
		{"principal", e.admin, "kepala_sekolah"},
		{"parent", e.admin, "orang_tua"},
	}
	for _, tc := range cases {
		if rec := e.do(http.MethodGet, "/absensi?kelas="+e.class1A, tc.user, tc.roles, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s GET: status = %d, want 403", tc.name, rec.Code)
		}
		if rec := e.do(http.MethodPost, "/absensi/"+sessionID, tc.user, tc.roles, form); rec.Code != http.StatusForbidden {
			t.Errorf("%s POST: status = %d, want 403", tc.name, rec.Code)
		}
	}
	// Admin yang juga guru tetap ditolak jika bukan wali kelas.
	if rec := e.do(http.MethodGet, "/absensi?kelas="+e.class1A, e.admin, "admin_tu,guru", nil); rec.Code != http.StatusForbidden {
		t.Errorf("admin+guru (not homeroom): status = %d, want 403", rec.Code)
	}
	if got := e.counts(sessionID); got["alpa"] != 0 {
		t.Error("forbidden POST changed data")
	}
	// Sesi tidak dikenal.
	if rec := e.do(http.MethodPost, "/absensi/00000000-0000-0000-0000-000000000000", e.guru1, "guru", form); rec.Code != http.StatusNotFound {
		t.Errorf("unknown session: status = %d, want 404", rec.Code)
	}
	// Guru tanpa kelas perwalian.
	e.tx.Exec(context.Background(), `UPDATE classes SET homeroom_teacher_id = NULL WHERE id = $1`, e.class1B)
	if body := e.do(http.MethodGet, "/absensi", e.guru2, "guru", nil).Body.String(); !strings.Contains(body, "belum menjadi wali kelas") {
		t.Error("teacher without homeroom class does not see the explanation")
	}
}

func TestAttendanceHandler_FutureDateRejected(t *testing.T) {
	e := newHEnv(t)
	tomorrow := e.today.AddDate(0, 0, 1)
	if rec := e.do(http.MethodGet, "/absensi?kelas="+e.class1A+"&tanggal="+tomorrow.Format(dateLayout), e.guru1, "guru", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("GET tomorrow: status = %d, want 400", rec.Code)
	}
	var n int
	_ = e.tx.QueryRow(context.Background(), `SELECT count(*) FROM attendance_sessions WHERE class_id = $1`, e.class1A).Scan(&n)
	if n != 0 {
		t.Error("a session was created for a future date")
	}
	// Sesi masa depan yang terlanjur ada (mis. dari data lama) juga tidak bisa disimpan.
	var sessionID string
	if err := e.tx.QueryRow(context.Background(), `INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by)
		SELECT school_id, academic_year_id, id, $2, $3 FROM classes WHERE id = $1 RETURNING id`, e.class1A, tomorrow, e.guru1).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(http.MethodPost, "/absensi/"+sessionID, e.guru1, "guru", url.Values{}); rec.Code != http.StatusBadRequest {
		t.Errorf("POST future session: status = %d, want 400", rec.Code)
	}
	// Tanggal tidak valid dan di luar tahun ajaran.
	if rec := e.do(http.MethodGet, "/absensi?kelas="+e.class1A+"&tanggal=17-10-2026", e.guru1, "guru", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad date format: status = %d", rec.Code)
	}
	old := e.today.AddDate(-1, 0, 0).Format(dateLayout)
	if rec := e.do(http.MethodGet, "/absensi?kelas="+e.class1A+"&tanggal="+old, e.guru1, "guru", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("date outside academic year: status = %d", rec.Code)
	}
}

func TestAttendanceHandler_InvalidInputSavesNothing(t *testing.T) {
	e := newHEnv(t)
	sessionID, ids := e.open(e.class1A, e.today.Format(dateLayout), e.guru1)

	// Satu siswa asing di antara isian yang valid.
	form := url.Values{"s_" + ids[0]: {StatusSick}, "s_" + ids[1]: {StatusSick}, "s_00000000-0000-0000-0000-000000000000": {StatusSick}}
	if rec := e.do(http.MethodPost, "/absensi/"+sessionID, e.guru1, "guru", form); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown enrollment: status = %d, want 400", rec.Code)
	}
	// Status tidak valid.
	form = url.Values{"s_" + ids[0]: {StatusSick}, "s_" + ids[1]: {"terlambat"}}
	if rec := e.do(http.MethodPost, "/absensi/"+sessionID, e.guru1, "guru", form); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid status: status = %d, want 400", rec.Code)
	}
	if got := e.counts(sessionID); got["hadir"] != 30 {
		t.Errorf("invalid submissions changed data: %v", got)
	}
}

func TestAttendanceHandler_PartialFailureRollsBack(t *testing.T) {
	e := newHEnv(t)
	ctx := context.Background()
	sessionID, ids := e.open(e.class1A, e.today.Format(dateLayout), e.guru1)

	// Trigger sementara: UPDATE untuk siswa terakhir gagal setelah baris lain
	// sudah diperbarui dalam perintah yang sama.
	if _, err := e.tx.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION pg_temp.fail_one() RETURNS trigger LANGUAGE plpgsql AS
		$$ BEGIN IF NEW.enrollment_id = '%s' THEN RAISE EXCEPTION 'injected failure'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER fail_one BEFORE UPDATE ON attendance_records FOR EACH ROW EXECUTE FUNCTION pg_temp.fail_one();`, ids[len(ids)-1])); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	for _, id := range ids {
		form.Set("s_"+id, StatusAbsent)
	}
	if rec := e.do(http.MethodPost, "/absensi/"+sessionID, e.guru1, "guru", form); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if got := e.counts(sessionID); got["hadir"] != 30 || got["alpa"] != 0 {
		t.Errorf("partial save left data: %v", got)
	}
}
