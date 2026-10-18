package announcements_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/announcements"
	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/internal/portal"
	"akra/internal/school"
	"akra/web"
)

type fixture struct {
	tx                                             pgx.Tx
	schoolA, schoolB                               string
	c1A, c1B, oldClass, cB                         string
	admin, adminB                                  school.Actor
	parentA, parentB, parentOld, parentOtherSchool string
	svc                                            *announcements.Service
	h                                              http.Handler
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
	f := &fixture{tx: tx, svc: announcements.NewService(tx)}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('SD A', 'test-ann-a') RETURNING id`)
	f.schoolB = one(`INSERT INTO schools (name, code) VALUES ('SD B', 'test-ann-b') RETURNING id`)
	user := func(school, email string, guardian any) string {
		return one(`INSERT INTO users (school_id, email, password_hash, full_name, guardian_id) VALUES ($1, $2, 'x', $2, $3) RETURNING id`, school, email, guardian)
	}
	f.admin = school.Actor{SchoolID: f.schoolA, UserID: user(f.schoolA, "admin@uji.id", nil)}
	f.adminB = school.Actor{SchoolID: f.schoolB, UserID: user(f.schoolB, "admin@uji.id", nil)}
	oldYear := one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2025/2026', '2025-07-14', '2026-06-26') RETURNING id`, f.schoolA)
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, f.schoolA)
	yearB := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, f.schoolB)
	class := func(school, y, name string) string {
		return one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 1) RETURNING id`, school, y, name)
	}
	f.c1A, f.c1B, f.oldClass, f.cB = class(f.schoolA, year, "1A"), class(f.schoolA, year, "1B"), class(f.schoolA, oldYear, "6A"), class(f.schoolB, yearB, "1A")
	parent := func(school, nis, classID, yearID, email string) string {
		st := one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $2) RETURNING id`, school, nis)
		if _, err := tx.Exec(ctx, `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, '2025-07-14')`, school, st, classID, yearID); err != nil {
			t.Fatal(err)
		}
		g := one(`INSERT INTO guardians (school_id, full_name) VALUES ($1, $2) RETURNING id`, school, email)
		if _, err := tx.Exec(ctx, `INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship) VALUES ($1, $2, $3, 'ibu')`, school, st, g); err != nil {
			t.Fatal(err)
		}
		return user(school, email, g)
	}
	f.parentA = parent(f.schoolA, "001", f.c1A, year, "ortu-1a@uji.id")
	f.parentB = parent(f.schoolA, "002", f.c1B, year, "ortu-1b@uji.id")
	f.parentOld = parent(f.schoolA, "003", f.oldClass, oldYear, "ortu-lama@uji.id")
	f.parentOtherSchool = parent(f.schoolB, "001", f.cB, yearB, "ortu-b@uji.id")

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
			id := auth.Identity{UserID: r.Header.Get("X-User"), SchoolID: r.Header.Get("X-School"), Roles: strings.Split(r.Header.Get("X-Roles"), ","), FullName: "Uji", SchoolName: "SD A"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	announcements.NewHandler(f.svc, sm, rd, logger).Routes(r)
	portal.NewHandler(portal.NewService(tx), sm, rd, logger).Routes(r)
	f.h = r
	return f
}

func (f *fixture) do(method, path, user, school, roles string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-User", user)
	req.Header.Set("X-School", school)
	req.Header.Set("X-Roles", roles)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func titles(list []announcements.Announcement) string {
	var s []string
	for _, a := range list {
		s = append(s, a.Title)
	}
	return strings.Join(s, ",")
}

func TestAnnouncements_Audience(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mk := func(actor school.Actor, title, classID string) {
		t.Helper()
		if _, err := f.svc.Create(ctx, actor, announcements.Form{Title: title, Body: "Isi " + title, ClassID: classID}); err != nil {
			t.Fatal(err)
		}
	}
	mk(f.admin, "Semua", "")
	mk(f.admin, "Kelas 1A", f.c1A)
	mk(f.adminB, "Sekolah B", "")
	// Dalam satu transaksi uji now() sama; beri waktu terbit berbeda agar
	// urutan "terbaru dulu" teruji.
	if _, err := f.tx.Exec(ctx, `UPDATE announcements SET published_at = published_at - interval '1 hour' WHERE title = 'Semua'`); err != nil {
		t.Fatal(err)
	}
	// Pengumuman tahun ajaran lalu tidak tampil lagi.
	if _, err := f.tx.Exec(ctx, `INSERT INTO announcements (school_id, academic_year_id, title, body, created_by)
		SELECT school_id, academic_year_id, 'Tahun lalu', 'x', $2 FROM classes WHERE id = $1`, f.oldClass, f.admin.UserID); err != nil {
		t.Fatal(err)
	}
	list := func(schoolID, user string) string {
		l, err := announcements.ListForParent(ctx, f.tx, schoolID, user, 50)
		if err != nil {
			t.Fatal(err)
		}
		return titles(l)
	}
	for name, c := range map[string]struct{ school, user, want string }{
		"parent of 1A":                   {f.schoolA, f.parentA, "Kelas 1A,Semua"},
		"parent of 1B":                   {f.schoolA, f.parentB, "Semua"},
		"parent enrolled last year only": {f.schoolA, f.parentOld, "Semua"},
		"parent of other school":         {f.schoolB, f.parentOtherSchool, "Sekolah B"},
		"staff account":                  {f.schoolA, f.admin.UserID, ""},
		"mismatched school":              {f.schoolB, f.parentA, ""},
	} {
		if got := list(c.school, c.user); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
	// Halaman staf: semua pengumuman sekolah sendiri (urutan dalam satu
	// transaksi uji tidak dijamin karena waktu terbitnya sama).
	staff, _ := f.svc.List(ctx, f.schoolA, 50)
	if got := titles(staff); len(staff) != 3 || !strings.Contains(got, "Kelas 1A") || !strings.Contains(got, "Tahun lalu") || strings.Contains(got, "Sekolah B") {
		t.Errorf("staff list = %q", got)
	}
	var n int
	_ = f.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'announcement.create'`, f.schoolA).Scan(&n)
	if n != 2 {
		t.Errorf("announcement.create audits = %d, want 2", n)
	}
}

func TestAnnouncements_Validation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for name, c := range map[string]struct {
		form  announcements.Form
		field string
	}{
		"empty title": {announcements.Form{Title: "  ", Body: "x"}, "title"},
		"long title":  {announcements.Form{Title: strings.Repeat("a", 151), Body: "x"}, "title"},
		"empty body":  {announcements.Form{Title: "x", Body: " \n "}, "body"},
		"long body":   {announcements.Form{Title: "x", Body: strings.Repeat("é", 5001)}, "body"},
	} {
		_, err := f.svc.Create(ctx, f.admin, c.form)
		var ve *announcements.ValidationError
		if !errors.As(err, &ve) || ve.Fields[c.field] == "" {
			t.Errorf("%s: error = %v, want field %s", name, err, c.field)
		}
	}
	if _, err := f.svc.Create(ctx, f.admin, announcements.Form{Title: strings.Repeat("a", 150), Body: strings.Repeat("é", 5000)}); err != nil {
		t.Errorf("at limits: %v", err)
	}
	for name, classID := range map[string]string{"class of last year": f.oldClass, "class of other school": f.cB, "invalid id": "1A"} {
		if _, err := f.svc.Create(ctx, f.admin, announcements.Form{Title: "x", Body: "y", ClassID: classID}); !errors.Is(err, announcements.ErrClassNotFound) {
			t.Errorf("%s: error = %v, want ErrClassNotFound", name, err)
		}
	}
	noYear := school.Actor{SchoolID: f.schoolB, UserID: f.adminB.UserID}
	if _, err := f.tx.Exec(ctx, `UPDATE academic_years SET is_active = false WHERE school_id = $1`, f.schoolB); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Create(ctx, noYear, announcements.Form{Title: "x", Body: "y"}); !errors.Is(err, announcements.ErrNoActiveYear) {
		t.Errorf("no active year: error = %v", err)
	}
}

func TestAnnouncements_Pages(t *testing.T) {
	f := newFixture(t)
	a := f.admin
	form := url.Values{"title": {`<script>alert("x")</script> Rapat`}, "body": {"Baris satu\n<b>tebal</b>"}, "class_id": {""}}

	for _, roles := range []string{"bendahara", "guru"} {
		if rec := f.do(http.MethodGet, "/pengumuman/baru", a.UserID, a.SchoolID, roles, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s form: status = %d, want 403", roles, rec.Code)
		}
		if rec := f.do(http.MethodPost, "/pengumuman", a.UserID, a.SchoolID, roles, form); rec.Code != http.StatusForbidden {
			t.Errorf("%s create: status = %d, want 403", roles, rec.Code)
		}
	}
	if rec := f.do(http.MethodGet, "/pengumuman", a.UserID, a.SchoolID, "orang_tua", nil); rec.Code != http.StatusForbidden {
		t.Errorf("parent on staff page: status = %d", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/pengumuman", a.UserID, a.SchoolID, "kepala_sekolah", url.Values{"title": {""}, "body": {""}}); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "Judul wajib diisi.") {
		t.Errorf("invalid form: status = %d", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/pengumuman", a.UserID, a.SchoolID, "admin_tu", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("create: status = %d", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/pengumuman", a.UserID, a.SchoolID, "admin_tu", url.Values{"title": {"x"}, "body": {"y"}, "class_id": {f.cB}}); rec.Code != http.StatusNotFound {
		t.Errorf("class of other school: status = %d, want 404", rec.Code)
	}

	check := func(name, body string) {
		t.Helper()
		if strings.Contains(body, `<script>alert`) || strings.Contains(body, "<b>tebal</b>") {
			t.Errorf("%s: HTML in the announcement is not escaped", name)
		}
		if !strings.Contains(body, "&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; Rapat") || !strings.Contains(body, "&lt;b&gt;tebal&lt;/b&gt;") {
			t.Errorf("%s: escaped text missing", name)
		}
	}
	for _, roles := range []string{"bendahara", "guru", "kepala_sekolah"} {
		rec := f.do(http.MethodGet, "/pengumuman", a.UserID, a.SchoolID, roles, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s list: status = %d", roles, rec.Code)
		}
		check(roles+" list", rec.Body.String())
		if roles == "bendahara" && strings.Contains(rec.Body.String(), "Buat pengumuman") {
			t.Error("read-only roles must not see the create button")
		}
	}
	rec := f.do(http.MethodGet, "/portal/pengumuman", f.parentB, f.schoolA, "orang_tua", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("portal: status = %d", rec.Code)
	}
	check("portal", rec.Body.String())
	if body := f.do(http.MethodGet, "/portal/pengumuman", f.parentOtherSchool, f.schoolB, "orang_tua", nil).Body.String(); strings.Contains(body, "Rapat") || !strings.Contains(body, "Belum ada pengumuman.") {
		t.Error("other school's parent sees the announcement")
	}
	if rec := f.do(http.MethodGet, "/portal/pengumuman", a.UserID, a.SchoolID, "admin_tu", nil); rec.Code != http.StatusForbidden {
		t.Errorf("staff on portal: status = %d, want 403", rec.Code)
	}
}
