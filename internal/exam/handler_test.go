package exam

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/duty"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/internal/platform/settings"
	"akra/internal/platform/storage"
	"akra/internal/testutil"
	"akra/web"
)

type env struct {
	t      *testing.T
	store  storage.Store
	tx     pgx.Tx
	a, b   *testutil.School
	router http.Handler
	// schoolID mengganti sekolah identitas uji (kosong = sekolah A).
	schoolID string
}

func newEnv(t *testing.T) *env {
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
	e := &env{t: t, tx: tx}
	e.a, e.b = testutil.SeedTwoSchools(t, tx, "$argon2id$v=19$m=1024,t=1,p=1$c2FsdA$aGFzaA")

	logger := slog.New(slog.NewTextHandler(debugOut(), nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	// Identitas uji dari header: "<user id>|<role>" di sekolah A.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, role, _ := strings.Cut(r.Header.Get("X-Test-User"), "|")
			sid := e.a.ID
			if e.schoolID != "" {
				sid = e.schoolID
			}
			id := auth.Identity{UserID: userID, SchoolID: sid, Roles: []string{role}}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.store = store
	NewHandler(tx, store, mr(t), sm, rd, logger).Routes(r)
	e.router = r
	return e
}

func (e *env) get(path string, u testutil.User) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Test-User", u.ID+"|"+u.Role)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func TestModuleRoutes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.a

	if rec := e.get("/ujian/", a.Admin); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `href="/ujian/agenda"`) {
		t.Fatalf("admin /ujian/ = %d", rec.Code)
	}
	if rec := e.get("/ujian/", a.Treasurer); rec.Code != http.StatusForbidden {
		t.Errorf("treasurer /ujian/ = %d, want 403", rec.Code)
	}
	if rec := e.get("/ujian/agenda", a.Principal); rec.Code != http.StatusOK {
		t.Errorf("principal agenda = %d", rec.Code)
	}

	// Guru tanpa tugas: 403. Setelah ditugasi koordinator: langsung 200
	// tanpa login ulang, karena hak dibaca dari database.
	if rec := e.get("/ujian/agenda", a.Teacher); rec.Code != http.StatusForbidden {
		t.Errorf("teacher agenda = %d, want 403", rec.Code)
	}
	if rec := e.get("/ujian/", a.Teacher); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `href="/ujian/agenda"`) {
		t.Errorf("teacher /ujian/ = %d or shows agenda", rec.Code)
	}
	if _, err := duty.Assign(ctx, e.tx, duty.Actor{SchoolID: a.ID, UserID: a.Admin.ID}, a.YearID, a.Teacher.ID, duty.ExamCoordinator); err != nil {
		t.Fatal(err)
	}
	if rec := e.get("/ujian/agenda", a.Teacher); rec.Code != http.StatusOK {
		t.Errorf("coordinator agenda = %d, want 200", rec.Code)
	}

	// Modul nonaktif: semua route ujian 404, termasuk untuk admin.
	if err := settings.Set(ctx, e.tx, a.ID, settings.ExamEnabled, "false"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/ujian/", "/ujian/agenda"} {
		if rec := e.get(p, a.Admin); rec.Code != http.StatusNotFound {
			t.Errorf("disabled module %s = %d, want 404", p, rec.Code)
		}
	}
	// Belum pernah diatur sama dengan nonaktif.
	if _, err := e.tx.Exec(ctx, `DELETE FROM school_settings WHERE school_id = $1 AND key LIKE 'exam.%'`, a.ID); err != nil {
		t.Fatal(err)
	}
	if rec := e.get("/ujian/", a.Admin); rec.Code != http.StatusNotFound {
		t.Errorf("unset module = %d, want 404", rec.Code)
	}
}

func TestLoadModule(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m, err := LoadModule(ctx, e.tx, e.a.ID)
	if err != nil || !m.Enabled || m.Release != R3 || !m.Has(R1) || !m.Has(R3) {
		t.Errorf("fixture module = %+v, %v", m, err)
	}
	if err := SetModule(ctx, e.tx, e.a.ID, Module{Enabled: true, Release: R1}); err != nil {
		t.Fatal(err)
	}
	if m, _ := LoadModule(ctx, e.tx, e.a.ID); !m.Has(R1) || m.Has(R2) {
		t.Errorf("r1 module = %+v", m)
	}
	if err := SetModule(ctx, e.tx, e.a.ID, Module{Enabled: true, Release: "r9"}); err == nil {
		t.Error("SetModule accepted r9")
	}
	if err := settings.Set(ctx, e.tx, e.a.ID, settings.ExamRelease, "r9"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadModule(ctx, e.tx, e.a.ID); err == nil {
		t.Error("LoadModule accepted stored r9")
	}
	if m := (Module{Enabled: false, Release: R3}); m.Has(R1) {
		t.Error("disabled module Has(R1)")
	}
}

func TestLoadActor(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.a
	act, err := LoadActor(ctx, e.tx, auth.Identity{UserID: a.Teacher.ID, SchoolID: a.ID, Roles: []string{auth.RoleGuru}})
	if err != nil {
		t.Fatal(err)
	}
	if act.Coordinator || !slices.Contains(act.Subjects, a.SubjectID) || !slices.Contains(act.Classes, a.ClassID+"|"+a.SubjectID) {
		t.Errorf("teacher actor = %+v", act)
	}
	// Mapel sekolah lain tidak pernah masuk.
	if slices.Contains(act.Subjects, e.b.SubjectID) {
		t.Error("actor contains other school's subject")
	}
	// Kepala sekolah dengan tugas koordinator (data fixture) tetap bukan
	// koordinator karena bukan guru.
	act, err = LoadActor(ctx, e.tx, auth.Identity{UserID: a.Principal.ID, SchoolID: a.ID, Roles: []string{auth.RoleKepalaSekolah}})
	if err != nil || act.Coordinator || act.Can(ActAgenda, Resource{}, now) != Read {
		t.Errorf("principal actor = %+v, %v", act, err)
	}
}

// debugOut mengarahkan log uji ke stderr bila AKRA_TEST_LOG=1.
func debugOut() io.Writer {
	var w io.Writer = io.Discard
	if os.Getenv("AKRA_TEST_LOG") == "1" {
		w = os.Stderr
	}
	if logCapture != nil {
		return io.MultiWriter(w, logCapture)
	}
	return w
}

// logCapture, bila diisi sebelum env dibuat, menerima salinan semua log
// handler (uji pemindai log T153).
var logCapture *lockedBuffer

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
