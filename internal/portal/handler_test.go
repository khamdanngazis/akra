package portal

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/web"
)

type fixture struct {
	tx                                 pgx.Tx
	schoolA, schoolB                   string
	ani, budi, citra, dewi             string // siswa
	userSari, userAgus, userRina, none string // akun orang tua
	h                                  http.Handler
}

func newFixture(t *testing.T) *fixture {
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
	f := &fixture{tx: tx}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('SD Portal A', 'test-portal-a') RETURNING id`)
	f.schoolB = one(`INSERT INTO schools (name, code) VALUES ('SD Portal B', 'test-portal-b') RETURNING id`)
	oldYear := one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2025/2026', '2025-07-14', '2026-06-26') RETURNING id`, f.schoolA)
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, f.schoolA)
	class := func(y, name string) string {
		return one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 1) RETURNING id`, f.schoolA, y, name)
	}
	c1A, c2B, oldC := class(year, "1A"), class(year, "2B"), class(oldYear, "6A")
	student := func(school, nis, name string) string {
		return one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id`, school, nis, name)
	}
	enroll := func(st, y, c string, ended bool) {
		end := any(nil)
		if ended {
			end = "2026-08-01"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date, end_date)
			VALUES ($1, $2, $3, $4, '2026-07-13', $5)`, f.schoolA, st, c, y, end); err != nil {
			t.Fatal(err)
		}
	}
	f.ani, f.budi = student(f.schoolA, "001", "Ani Lestari"), student(f.schoolA, "002", "Budi Lestari")
	f.citra = student(f.schoolA, "003", "Citra Keluarga Lain")
	f.dewi = student(f.schoolB, "001", "Dewi Sekolah B")
	enroll(f.ani, year, c1A, false)
	enroll(f.budi, year, c1A, true) // dipindah: enrollment lama ditutup
	enroll(f.budi, year, c2B, false)
	enroll(f.citra, oldYear, oldC, false) // hanya tahun lalu
	if _, err := tx.Exec(ctx, `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
		VALUES ($1, $2, $3, $4, '2025-07-14')`, f.schoolA, f.ani, oldC, oldYear); err != nil {
		t.Fatal(err)
	}

	guardian := func(school, name string) string {
		return one(`INSERT INTO guardians (school_id, full_name) VALUES ($1, $2) RETURNING id`, school, name)
	}
	link := func(school, st, g, rel string) {
		if _, err := tx.Exec(ctx, `INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship) VALUES ($1, $2, $3, $4)`, school, st, g, rel); err != nil {
			t.Fatal(err)
		}
	}
	account := func(school, email, g string) string {
		var gid any
		if g != "" {
			gid = g
		}
		return one(`INSERT INTO users (school_id, email, password_hash, full_name, guardian_id) VALUES ($1, $2, 'x', $2, $3) RETURNING id`, school, email, gid)
	}
	sari, agus, rina := guardian(f.schoolA, "Sari"), guardian(f.schoolA, "Agus"), guardian(f.schoolA, "Rina")
	gB := guardian(f.schoolB, "Wali B")
	link(f.schoolA, f.ani, sari, "ibu")
	link(f.schoolA, f.budi, sari, "ibu")
	link(f.schoolA, f.ani, agus, "ayah") // dua wali untuk satu anak
	link(f.schoolA, f.citra, rina, "wali")
	link(f.schoolB, f.dewi, gB, "ibu")
	f.userSari, f.userAgus, f.userRina = account(f.schoolA, "sari@uji.id", sari), account(f.schoolA, "agus@uji.id", agus), account(f.schoolA, "rina@uji.id", rina)
	f.none = account(f.schoolA, "kosong@uji.id", guardian(f.schoolA, "Tanpa Anak"))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.Identity{UserID: r.Header.Get("X-User"), SchoolID: r.Header.Get("X-School"), Roles: strings.Split(r.Header.Get("X-Roles"), ","), FullName: "Ibu Uji", SchoolName: "SD Portal A"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	NewHandler(NewService(tx), sm, rd, logger).Routes(r)
	f.h = r
	return f
}

func (f *fixture) get(user, school, roles string) *httptest.ResponseRecorder {
	return f.getPath("/portal", user, school, roles)
}

func (f *fixture) getPath(path, user, school, roles string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-User", user)
	req.Header.Set("X-School", school)
	req.Header.Set("X-Roles", roles)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func TestListChildren(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := NewService(f.tx)

	kids, err := svc.ListChildren(ctx, f.schoolA, f.userSari)
	if err != nil || len(kids) != 2 {
		t.Fatalf("Sari: %+v, %v; want 2 children", kids, err)
	}
	if kids[0].FullName != "Ani Lestari" || kids[0].ClassName != "1A" || kids[0].YearName != "2026/2027" || kids[0].Relationship != "ibu" {
		t.Errorf("Ani = %+v; want class 1A of the active year (not 6A of last year)", kids[0])
	}
	if kids[1].FullName != "Budi Lestari" || kids[1].ClassName != "2B" {
		t.Errorf("Budi = %+v; want current class 2B, not the closed 1A", kids[1])
	}
	if kids, _ := svc.ListChildren(ctx, f.schoolA, f.userAgus); len(kids) != 1 || kids[0].ID != f.ani || kids[0].Relationship != "ayah" {
		t.Errorf("second guardian of Ani: %+v", kids)
	}
	if kids, _ := svc.ListChildren(ctx, f.schoolA, f.userRina); len(kids) != 1 || kids[0].ID != f.citra || kids[0].ClassName != "" {
		t.Errorf("Rina: %+v; want Citra without a class (enrolled only last year)", kids)
	}
	if kids, err := svc.ListChildren(ctx, f.schoolA, f.none); err != nil || len(kids) != 0 {
		t.Errorf("guardian without children: %+v, %v", kids, err)
	}
	// Identitas dengan sekolah yang tidak cocok tidak melihat apa pun.
	if kids, _ := svc.ListChildren(ctx, f.schoolB, f.userSari); len(kids) != 0 {
		t.Errorf("mismatched school: %+v", kids)
	}
	if kids, _ := svc.ListChildren(ctx, f.schoolA, "bukan-uuid"); len(kids) != 0 {
		t.Errorf("invalid user id: %+v", kids)
	}
}

func TestPortalPage(t *testing.T) {
	f := newFixture(t)
	body := f.get(f.userSari, f.schoolA, "orang_tua").Body.String()
	if strings.Count(body, `class="pay-item child-card"`) != 2 || !strings.Contains(body, "Anak Anda (2)") ||
		!strings.Contains(body, "Ani Lestari") || !strings.Contains(body, "Budi Lestari") {
		t.Error("guardian with two children must see two cards")
	}
	if strings.Contains(body, "Citra") || strings.Contains(body, "Dewi") {
		t.Error("children of other families or schools are shown")
	}
	if body := f.get(f.userAgus, f.schoolA, "orang_tua").Body.String(); strings.Count(body, `child-card`) != 1 || !strings.Contains(body, ">Anak Anda<") {
		t.Error("single child view")
	}
	rec := f.get(f.none, f.schoolA, "orang_tua")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Belum ada data anak yang terhubung. Hubungi sekolah.") {
		t.Errorf("no children: status = %d", rec.Code)
	}
	if body := f.get(f.userRina, f.schoolA, "orang_tua").Body.String(); !strings.Contains(body, "Belum ditempatkan di kelas tahun ajaran aktif.") || strings.Contains(mainContent(body), "6A") {
		t.Error("child enrolled only last year must not show last year's class")
	}
	for _, roles := range []string{"admin_tu", "bendahara", "guru", "kepala_sekolah"} {
		if rec := f.get(f.userSari, f.schoolA, roles); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", roles, rec.Code)
		}
	}
}

// mainContent mengembalikan isi <main> (tanpa header dan sprite ikon).
func mainContent(body string) string {
	i, j := strings.Index(body, "<main"), strings.Index(body, "</main>")
	if i < 0 || j < i {
		return body
	}
	return body[i:j]
}
