package schedule

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/internal/school"
	"akra/web"
)

// fixture: sekolah A dengan kelas 1A dan 1B, guru1 mengampu Matematika di
// kedua kelas, guru2 mengampu IPA di 1A; IPA di 1B belum punya guru. Sekolah
// B punya kelas dan mapel sendiri. Semua di satu transaksi yang dibatalkan.
type fixture struct {
	tx                    pgx.Tx
	svc                   *Service
	actor, actorB         school.Actor
	schoolA, schoolB      string
	class1A, class1B, old string
	classB                string
	guru1, guru2          string
	mtk, ipa, offSubject  string
	subjectB              string
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
	f := &fixture{tx: tx, svc: NewService(tx)}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%v: %s", err, sql)
		}
		return id
	}
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('A', 'test-jadwal-a') RETURNING id`)
	f.schoolB = one(`INSERT INTO schools (name, code) VALUES ('B', 'test-jadwal-b') RETURNING id`)
	user := func(school, email, name, role string) string {
		id := one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, $2, 'x', $3) RETURNING id`, school, email, name)
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = $2`, id, role); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.actor = school.Actor{SchoolID: f.schoolA, UserID: user(f.schoolA, "admin@a.uji", "Admin", "admin_tu"), IP: netip.MustParseAddr("192.0.2.5")}
	f.actorB = school.Actor{SchoolID: f.schoolB, UserID: user(f.schoolB, "admin@b.uji", "Admin B", "admin_tu")}
	f.guru1 = user(f.schoolA, "guru1@a.uji", "Bu Sari", "guru")
	f.guru2 = user(f.schoolA, "guru2@a.uji", "Pak Budi", "guru")
	guruB := user(f.schoolB, "guru@b.uji", "Guru B", "guru")

	year := `INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, $2, $3, $4, $5) RETURNING id`
	yearA := one(year, f.schoolA, "2026/2027", "2026-07-13", "2027-06-25", true)
	oldYear := one(year, f.schoolA, "2025/2026", "2025-07-14", "2026-06-26", false)
	yearB := one(year, f.schoolB, "2026/2027", "2026-07-13", "2027-06-25", true)
	class := `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 1) RETURNING id`
	f.class1A, f.class1B = one(class, f.schoolA, yearA, "1A"), one(class, f.schoolA, yearA, "1B")
	f.old = one(class, f.schoolA, oldYear, "6A")
	f.classB = one(class, f.schoolB, yearB, "1A")
	subject := `INSERT INTO subjects (school_id, name, code) VALUES ($1, $2, $3) RETURNING id`
	f.mtk, f.ipa = one(subject, f.schoolA, "Matematika", "MTK"), one(subject, f.schoolA, "IPA", "IPA")
	f.offSubject = one(subject, f.schoolA, "Bahasa Jawa", "BJW")
	f.subjectB = one(subject, f.schoolB, "Matematika", "MTK")
	assign := func(school, yearID, classID, subjectID, teacherID string) {
		t.Helper()
		if _, err := tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`,
			school, yearID, classID, subjectID, teacherID); err != nil {
			t.Fatal(err)
		}
	}
	assign(f.schoolA, yearA, f.class1A, f.mtk, f.guru1)
	assign(f.schoolA, yearA, f.class1B, f.mtk, f.guru1)
	assign(f.schoolA, yearA, f.class1A, f.ipa, f.guru2)
	assign(f.schoolA, yearA, f.class1A, f.offSubject, f.guru2)
	assign(f.schoolA, oldYear, f.old, f.mtk, f.guru1)
	assign(f.schoolB, yearB, f.classB, f.subjectB, guruB)
	if _, err := tx.Exec(ctx, `UPDATE subjects SET is_active = false WHERE id = $1`, f.offSubject); err != nil {
		t.Fatal(err)
	}
	return f
}

func form(subjectID, day, start, end string) Form {
	return Form{SubjectID: subjectID, Day: day, Start: start, End: end}
}

func (f *fixture) mustCreate(t *testing.T, classID string, fm Form) Slot {
	t.Helper()
	s, err := f.svc.Create(context.Background(), f.actor, classID, fm)
	if err != nil {
		t.Fatalf("create %+v: %v", fm, err)
	}
	return s
}

func (f *fixture) audits(action string) int {
	var n int
	_ = f.tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, f.schoolA, action).Scan(&n)
	return n
}

func conflict(t *testing.T, err error) *ConflictError {
	t.Helper()
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
	return ce
}

func TestSlots_ClassConflict(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := f.mustCreate(t, f.class1A, form(f.mtk, "1", "07:00", "08:00"))
	if first.TeacherID != f.guru1 || first.TeacherName != "Bu Sari" || first.Start != 420 || first.End != 480 || first.ClassName != "1A" {
		t.Fatalf("created slot: %+v", first)
	}
	// Dua slot berpotongan di kelas yang sama ditolak, meski mapel dan
	// gurunya berbeda.
	for _, tc := range [][2]string{{"07:30", "08:30"}, {"06:30", "07:01"}, {"07:10", "07:50"}, {"06:00", "09:00"}, {"07:00", "08:00"}} {
		_, err := f.svc.Create(ctx, f.actor, f.class1A, form(f.ipa, "1", tc[0], tc[1]))
		ce := conflict(t, err)
		if !ce.ByClass || ce.With.ID != first.ID || !strings.Contains(ce.Message(), "Bentrok dengan Matematika di kelas ini (Senin 07.00–08.00)") {
			t.Errorf("%v: conflict %+v, message %q", tc, ce, ce.Message())
		}
	}
	// Bersebelahan (selesai 08.00, mulai 08.00) dan hari lain diterima.
	f.mustCreate(t, f.class1A, form(f.ipa, "1", "08:00", "08:40"))
	f.mustCreate(t, f.class1A, form(f.ipa, "1", "06:20", "07:00"))
	f.mustCreate(t, f.class1A, form(f.ipa, "2", "07:00", "08:00"))
	slots, err := ListClassSlots(ctx, f.tx, f.schoolA, f.class1A)
	if err != nil || len(slots) != 4 || slots[0].Start != 380 || slots[3].Day != 2 {
		t.Fatalf("list: %v %+v", err, slots)
	}
	if n := f.audits(ActionScheduleCreate); n != 4 {
		t.Errorf("create audits = %d, want 4", n)
	}
}

func TestSlots_TeacherConflictAcrossClasses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mustCreate(t, f.class1A, form(f.mtk, "3", "09:00", "10:00"))
	// guru1 mengampu Matematika di 1A dan 1B: jam yang sama di 1B ditolak.
	_, err := f.svc.Create(ctx, f.actor, f.class1B, form(f.mtk, "3", "09:30", "10:30"))
	ce := conflict(t, err)
	if ce.ByClass || !strings.Contains(ce.Message(), "Guru Bu Sari sudah mengajar Matematika di kelas 1A pada Rabu 09.00–10.00") {
		t.Errorf("message %q", ce.Message())
	}
	// Bersebelahan diterima; guru lain di jam yang sama juga diterima.
	f.mustCreate(t, f.class1B, form(f.mtk, "3", "10:00", "10:40"))
	f.mustCreate(t, f.class1A, form(f.ipa, "3", "10:00", "10:40"))
	if _, err := f.svc.Create(ctx, f.actor, f.class1A, form(f.ipa, "3", "10:20", "11:00")); err == nil {
		t.Error("overlap with own class accepted")
	}
}

func TestSlots_Validation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		f     Form
		field string
		msg   string
	}{
		{"no assignment", form(f.ipa, "1", "07:00", "08:00"), "subject", "belum punya guru di kelas ini"},
		{"inactive subject", form(f.offSubject, "1", "07:00", "08:00"), "subject", "nonaktif"},
		{"empty subject", form("", "1", "07:00", "08:00"), "subject", "Pilih mata pelajaran"},
		{"sunday", form(f.mtk, "7", "07:00", "08:00"), "day", "Senin–Sabtu"},
		{"bad day", form(f.mtk, "x", "07:00", "08:00"), "day", "Senin–Sabtu"},
		{"bad start", form(f.mtk, "1", "7", "08:00"), "start", "jam mulai"},
		{"bad end", form(f.mtk, "1", "07:00", ""), "end", "jam selesai"},
		{"end before start", form(f.mtk, "1", "08:00", "07:00"), "end", "setelah jam mulai"},
		{"zero length", form(f.mtk, "1", "08:00", "08:00"), "end", "setelah jam mulai"},
	} {
		classID := f.class1A
		if tc.name == "no assignment" {
			classID = f.class1B
		}
		_, err := f.svc.Create(ctx, f.actor, classID, tc.f)
		var ve *ValidationError
		if !errors.As(err, &ve) || !strings.Contains(ve.Fields[tc.field], tc.msg) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
	// Kelas/mapel sekolah lain dan kelas tahun ajaran lama: tidak ditemukan.
	if _, err := f.svc.Create(ctx, f.actor, f.classB, form(f.mtk, "1", "07:00", "08:00")); !errors.Is(err, ErrClassNotFound) {
		t.Errorf("other school class: %v", err)
	}
	if _, err := f.svc.Create(ctx, f.actor, f.class1A, form(f.subjectB, "1", "07:00", "08:00")); !errors.Is(err, ErrSubjectNotFound) {
		t.Errorf("other school subject: %v", err)
	}
	if _, err := f.svc.Create(ctx, f.actor, f.old, form(f.mtk, "1", "07:00", "08:00")); !errors.Is(err, ErrClassNotFound) {
		t.Errorf("old year class: %v", err)
	}
	if _, err := f.svc.Create(ctx, f.actor, "bukan-uuid", form(f.mtk, "1", "07:00", "08:00")); !errors.Is(err, ErrClassNotFound) {
		t.Errorf("bad class id: %v", err)
	}
	if n := f.audits(ActionScheduleCreate); n != 0 {
		t.Errorf("audits after rejected creates = %d", n)
	}
}

func TestSlots_UpdateDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mustCreate(t, f.class1A, form(f.mtk, "1", "07:00", "08:00"))
	b := f.mustCreate(t, f.class1A, form(f.ipa, "1", "08:00", "09:00"))
	// Menggeser slot ke rentang yang memotong rentang lamanya sendiri boleh.
	moved, err := f.svc.Update(ctx, f.actor, a.ID, form(f.mtk, "1", "07:10", "07:50"))
	if err != nil || moved.Start != 430 || moved.End != 470 {
		t.Fatalf("move: %v %+v", err, moved)
	}
	// Memotong slot lain ditolak; slot tidak berubah.
	if _, err := f.svc.Update(ctx, f.actor, a.ID, form(f.mtk, "1", "07:30", "08:30")); conflict(t, err).With.ID != b.ID {
		t.Error("conflict with wrong slot")
	}
	// Ganti mapel: guru ikut penugasan mapel baru.
	switched, err := f.svc.Update(ctx, f.actor, a.ID, form(f.ipa, "2", "07:00", "08:00"))
	if err != nil || switched.TeacherID != f.guru2 || switched.SubjectName != "IPA" || switched.Day != 2 {
		t.Fatalf("switch subject: %v %+v", err, switched)
	}
	// Tanpa perubahan: tidak menambah audit.
	if _, err := f.svc.Update(ctx, f.actor, a.ID, FormOf(switched)); err != nil {
		t.Fatal(err)
	}
	if n := f.audits(ActionScheduleUpdate); n != 2 {
		t.Errorf("update audits = %d, want 2", n)
	}
	var before, after string
	if err := f.tx.QueryRow(ctx, `SELECT before::text, after::text FROM audit_logs WHERE school_id = $1 AND action = $2 ORDER BY created_at DESC, id DESC LIMIT 1`,
		f.schoolA, ActionScheduleUpdate).Scan(&before, &after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, `"start_time": "07:10"`) || !strings.Contains(after, `"day_of_week": 2`) || !strings.Contains(after, f.guru2) {
		t.Errorf("audit before %s after %s", before, after)
	}
	// Sekolah lain tidak bisa membaca, mengubah, atau menghapus.
	if _, err := f.svc.Get(ctx, f.schoolB, a.ID); !errors.Is(err, ErrSlotNotFound) {
		t.Errorf("get other school: %v", err)
	}
	if _, err := f.svc.Update(ctx, f.actorB, a.ID, form(f.subjectB, "1", "07:00", "08:00")); !errors.Is(err, ErrSlotNotFound) {
		t.Errorf("update other school: %v", err)
	}
	if _, err := f.svc.Delete(ctx, f.actorB, a.ID); !errors.Is(err, ErrSlotNotFound) {
		t.Errorf("delete other school: %v", err)
	}
	if _, err := f.svc.Delete(ctx, f.actor, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(ctx, f.schoolA, a.ID); !errors.Is(err, ErrSlotNotFound) {
		t.Errorf("deleted slot still found: %v", err)
	}
	if n := f.audits(ActionScheduleDelete); n != 1 {
		t.Errorf("delete audits = %d", n)
	}
}

// TestSlots_DatabaseExclusion memastikan constraint EXCLUDE menolak bentrok
// meski pemeriksaan service dilewati.
func TestSlots_DatabaseExclusion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mustCreate(t, f.class1A, form(f.mtk, "4", "07:00", "08:00"))
	c, err := activeClass(ctx, f.tx, f.schoolA, f.class1A)
	if err != nil {
		t.Fatal(err)
	}
	c1B, err := activeClass(ctx, f.tx, f.schoolA, f.class1B)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		in   slotInput
		want error
	}{
		{"same class", slotInput{c.AcademicYearID, c.ID, f.ipa, f.guru2, 4, 450, 510}, ErrOverlap},
		{"same teacher", slotInput{c1B.AcademicYearID, c1B.ID, f.mtk, f.guru1, 4, 450, 510}, ErrOverlap},
		{"adjacent", slotInput{c.AcademicYearID, c.ID, f.ipa, f.guru2, 4, 480, 520}, nil},
	} {
		sp, err := f.tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = insertSlot(ctx, sp, f.schoolA, tc.in)
		_ = sp.Rollback(ctx)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

// TestSlots_BlocksAssignmentChange: guru mapel yang sudah dijadwalkan tidak
// bisa diganti atau dilepas di Penugasan Guru (T65).
func TestSlots_BlocksAssignmentChange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mustCreate(t, f.class1A, form(f.mtk, "1", "07:00", "08:00"))
	assignments := academic.NewAssignmentService(f.tx)
	for _, teacher := range []string{f.guru2, ""} {
		_, err := assignments.Save(ctx, f.actor, f.class1A, map[string]string{f.mtk: teacher})
		var ve *academic.ValidationError
		if !errors.As(err, &ve) || !strings.Contains(ve.Fields[f.mtk], "sudah ada di jadwal kelas") {
			t.Errorf("teacher %q: err = %v", teacher, err)
		}
	}
	// Mapel yang belum dijadwalkan tetap bisa diganti.
	if _, err := assignments.Save(ctx, f.actor, f.class1A, map[string]string{f.ipa: f.guru1}); err != nil {
		t.Errorf("unscheduled subject: %v", err)
	}
}

type env struct {
	*fixture
	router http.Handler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	f := newFixture(t)
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
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			userID, roles, _ := strings.Cut(req.Header.Get("X-Test-User"), "|")
			id := auth.Identity{UserID: userID, SchoolID: f.schoolA, Roles: strings.Split(roles, ","), FullName: "Uji", SchoolName: "A"}
			next.ServeHTTP(w, req.WithContext(auth.ContextWithIdentity(req.Context(), id)))
		})
	})
	NewHandler(f.svc, f.tx, sm, rd, logger).Routes(r)
	return &env{fixture: f, router: r}
}

func (e *env) do(method, path, roles string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-Test-User", e.actor.UserID+"|"+roles)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func slotForm(subjectID, day, start, end string) url.Values {
	return url.Values{"subject_id": {subjectID}, "day": {day}, "start": {start}, "end": {end}}
}

func TestHandler(t *testing.T) {
	e := newEnv(t)
	// Admin menambah slot lalu melihatnya di jadwal mingguan.
	rec := e.do(http.MethodPost, "/jadwal/kelas/"+e.class1A, "admin_tu", slotForm(e.mtk, "1", "07:00", "07:40"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/jadwal?kelas="+e.class1A {
		t.Fatalf("create: status = %d, location %q", rec.Code, rec.Header().Get("Location"))
	}
	body := e.do(http.MethodGet, "/jadwal?kelas="+e.class1A, "admin_tu", nil).Body.String()
	if !strings.Contains(body, "07.00–07.40") || !strings.Contains(body, "Bu Sari") || !strings.Contains(body, `action="/jadwal/kelas/`+e.class1A+`"`) {
		t.Error("admin page misses slot or form")
	}
	slots, _ := ListClassSlots(context.Background(), e.tx, e.schoolA, e.class1A)
	if len(slots) != 1 {
		t.Fatalf("slots = %d", len(slots))
	}
	slotID := slots[0].ID
	// Bentrok: 422 dengan pesan, form tetap berisi isian.
	rec = e.do(http.MethodPost, "/jadwal/kelas/"+e.class1A, "admin_tu", slotForm(e.ipa, "1", "07:20", "08:00"))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Bentrok dengan Matematika di kelas ini") || !strings.Contains(rec.Body.String(), `value="07:20"`) {
		t.Errorf("conflict: status = %d", rec.Code)
	}
	// Mapel tanpa guru di kelas: 422.
	if rec := e.do(http.MethodPost, "/jadwal/kelas/"+e.class1B, "admin_tu", slotForm(e.ipa, "1", "07:00", "07:40")); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "belum punya guru di kelas ini") {
		t.Errorf("no assignment: status = %d", rec.Code)
	}
	// Ubah dan hapus.
	if rec := e.do(http.MethodGet, "/jadwal/slot/"+slotID+"/ubah", "admin_tu", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `value="07:00"`) {
		t.Errorf("edit page: status = %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/jadwal/slot/"+slotID, "admin_tu", slotForm(e.mtk, "1", "06:00", "06:00")); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "setelah jam mulai") {
		t.Errorf("invalid update: status = %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/jadwal/slot/"+slotID, "admin_tu", slotForm(e.mtk, "2", "07:00", "07:40")); rec.Code != http.StatusSeeOther {
		t.Errorf("update: status = %d", rec.Code)
	}
	// Kelas, mapel, dan slot sekolah lain: 404.
	for _, tc := range []struct {
		method, path string
		form         url.Values
	}{
		{http.MethodGet, "/jadwal?kelas=" + e.classB, nil},
		{http.MethodPost, "/jadwal/kelas/" + e.classB, slotForm(e.subjectB, "1", "09:00", "09:40")},
		{http.MethodPost, "/jadwal/kelas/" + e.class1A, slotForm(e.subjectB, "1", "09:00", "09:40")},
		{http.MethodGet, "/jadwal/slot/bukan-uuid/ubah", nil},
		{http.MethodPost, "/jadwal/slot/00000000-0000-0000-0000-000000000000/hapus", url.Values{}},
	} {
		if rec := e.do(tc.method, tc.path, "admin_tu", tc.form); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
	// Kepala sekolah melihat tanpa form; peran lain 403 untuk menulis.
	kepala := e.do(http.MethodGet, "/jadwal?kelas="+e.class1A, "kepala_sekolah", nil)
	if kepala.Code != http.StatusOK || strings.Contains(kepala.Body.String(), `action="/jadwal/kelas/`) || strings.Contains(kepala.Body.String(), "/ubah") {
		t.Errorf("principal page: status = %d", kepala.Code)
	}
	for _, role := range []string{"guru", "bendahara", "orang_tua"} {
		if rec := e.do(http.MethodGet, "/jadwal", role, nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET as %s: status = %d", role, rec.Code)
		}
	}
	for _, role := range []string{"kepala_sekolah", "guru", "bendahara", "orang_tua"} {
		for _, path := range []string{"/jadwal/kelas/" + e.class1A, "/jadwal/slot/" + slotID, "/jadwal/slot/" + slotID + "/hapus"} {
			if rec := e.do(http.MethodPost, path, role, slotForm(e.ipa, "3", "07:00", "07:40")); rec.Code != http.StatusForbidden {
				t.Errorf("POST %s as %s: status = %d", path, role, rec.Code)
			}
		}
	}
	if rec := e.do(http.MethodPost, "/jadwal/slot/"+slotID+"/hapus", "admin_tu", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Errorf("delete: status = %d", rec.Code)
	}
	if n := e.audits(ActionScheduleDelete); n != 1 {
		t.Errorf("delete audits = %d", n)
	}
}

func TestHandler_EmptyStates(t *testing.T) {
	e := newEnv(t)
	// Kelas pertama dipilih otomatis; 1B tanpa jadwal menampilkan hari kosong.
	if rec := e.do(http.MethodGet, "/jadwal", "admin_tu", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Tidak ada pelajaran.") {
		t.Errorf("default class: status = %d", rec.Code)
	}
	if _, err := e.tx.Exec(context.Background(), `UPDATE academic_years SET is_active = false WHERE school_id = $1`, e.schoolA); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(http.MethodGet, "/jadwal", "admin_tu", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Belum ada tahun ajaran aktif.") {
		t.Errorf("no year: status = %d", rec.Code)
	}
}
