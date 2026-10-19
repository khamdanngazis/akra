package academic

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/render"
	"akra/web"
)

const idHeader = "X-Test-Identity" // "<A|B>|<role,role>"

const (
	adminTU   = "A|admin_tu"
	treasurer = "A|bendahara"
	principal = "A|kepala_sekolah"
	teacher   = "A|guru"
	parent    = "A|orang_tua"
)

type hEnv struct {
	*fixture
	router  http.Handler
	cookies map[string]*http.Cookie
}

func newHEnv(t *testing.T) *hEnv {
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
	e := &hEnv{fixture: f, cookies: map[string]*http.Cookie{}}
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, roles, _ := strings.Cut(r.Header.Get(idHeader), "|")
			actor := f.actor
			if s == "B" {
				actor = f.actorB
			}
			id := auth.Identity{UserID: actor.UserID, SchoolID: actor.SchoolID, Roles: strings.Split(roles, ","), FullName: "Uji", SchoolName: "Uji"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	NewSubjectHandler(NewSubjectService(f.tx), sm, rd, logger).Routes(r)
	e.router = r
	return e
}

func (e *hEnv) do(method, path, who string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set(idHeader, who)
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

func newForm(name, code string) url.Values {
	return url.Values{"name": {name}, "code": {code}}
}

func editForm(name, code, status string) url.Values {
	return url.Values{"name": {name}, "code": {code}, "status": {status}}
}

var idInForm = regexp.MustCompile(`action="/pengaturan/mapel/([0-9a-f-]{36})"`)

func TestSubjectHandler_OnlyAdminTU(t *testing.T) {
	e := newHEnv(t)
	sub, err := e.svc.Create(context.Background(), e.actor, SubjectForm{Name: "Matematika", Code: "MTK"})
	if err != nil {
		t.Fatal(err)
	}

	// Guru, bendahara, kepala sekolah, dan orang tua tidak boleh membuka
	// atau mengubah apa pun.
	for _, who := range []string{teacher, treasurer, principal, parent} {
		for _, req := range []struct {
			method, path string
			form         url.Values
		}{
			{http.MethodGet, "/pengaturan/mapel", nil},
			{http.MethodPost, "/pengaturan/mapel", newForm("Dibuat "+who, "")},
			{http.MethodPost, "/pengaturan/mapel/" + sub.ID, editForm("Diubah", "", StatusInactive)},
		} {
			if rec := e.do(req.method, req.path, who, req.form); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s as %s: status = %d, want 403", req.method, req.path, who, rec.Code)
			}
		}
	}

	list, _ := e.svc.ListAll(context.Background(), e.schoolA)
	if len(list) != 1 || list[0].Name != "Matematika" || !list[0].IsActive {
		t.Errorf("subjects changed by forbidden roles: %+v", list)
	}
	for _, action := range []string{ActionSubjectUpdate, ActionSubjectDeactivate} {
		if n := e.audits(e.schoolA, action); n != 0 {
			t.Errorf("%s audits = %d, want 0", action, n)
		}
	}

	if rec := e.do(http.MethodGet, "/pengaturan/mapel", adminTU, nil); rec.Code != http.StatusOK {
		t.Errorf("admin_tu GET: status = %d, want 200", rec.Code)
	}
}

func TestSubjectHandler_AdminManagesSubjects(t *testing.T) {
	e := newHEnv(t)
	ctx := context.Background()

	rec := e.do(http.MethodPost, "/pengaturan/mapel", adminTU, newForm(" Matematika ", "mtk"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/pengaturan/mapel" {
		t.Fatalf("create: status = %d, location = %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	list := e.do(http.MethodGet, "/pengaturan/mapel", adminTU, nil).Body.String()
	for _, want := range []string{"Matematika", "mtk", "berhasil dibuat", "Aktif"} {
		if !strings.Contains(list, want) {
			t.Errorf("list does not contain %q", want)
		}
	}
	m := idInForm.FindStringSubmatch(list)
	if m == nil {
		t.Fatalf("no edit form in list:\n%s", list)
	}
	id := m[1]

	// Ubah nama dan kode.
	if rec := e.do(http.MethodPost, "/pengaturan/mapel/"+id, adminTU, editForm("Matematika Wajib", "MTK-W", StatusActive)); rec.Code != http.StatusSeeOther {
		t.Fatalf("update: status = %d\n%s", rec.Code, rec.Body.String())
	}
	got, _ := GetSubjectByID(ctx, e.tx, e.schoolA, id)
	if got.Name != "Matematika Wajib" || got.Code != "MTK-W" || !got.IsActive {
		t.Errorf("after update = %+v", got)
	}

	// Nonaktifkan: tidak dihapus, tetap tampil di daftar pengaturan, tetapi
	// tidak ada di pilihan penugasan dan jadwal (ListActive).
	if rec := e.do(http.MethodPost, "/pengaturan/mapel/"+id, adminTU, editForm("Matematika Wajib", "MTK-W", StatusInactive)); rec.Code != http.StatusSeeOther {
		t.Fatalf("deactivate: status = %d", rec.Code)
	}
	list = e.do(http.MethodGet, "/pengaturan/mapel", adminTU, nil).Body.String()
	if !strings.Contains(list, "Matematika Wajib") || !strings.Contains(list, "Nonaktif") || !strings.Contains(list, "row-inactive") {
		t.Error("inactive subject is not shown as inactive in the settings list")
	}
	if active, _ := ListActiveSubjects(ctx, e.tx, e.schoolA); len(active) != 0 {
		t.Errorf("ListActive after deactivate = %+v, want none", active)
	}

	// Aktifkan lagi.
	if rec := e.do(http.MethodPost, "/pengaturan/mapel/"+id, adminTU, editForm("Matematika Wajib", "MTK-W", StatusActive)); rec.Code != http.StatusSeeOther {
		t.Fatalf("activate: status = %d", rec.Code)
	}
	if active, _ := ListActiveSubjects(ctx, e.tx, e.schoolA); len(active) != 1 {
		t.Errorf("ListActive after activate = %+v, want 1", active)
	}

	for action, want := range map[string]int{
		ActionSubjectCreate: 1, ActionSubjectUpdate: 1, ActionSubjectDeactivate: 1, ActionSubjectActivate: 1,
	} {
		if n := e.audits(e.schoolA, action); n != want {
			t.Errorf("%s audits = %d, want %d", action, n, want)
		}
	}
}

func TestSubjectHandler_ValidationErrorsKeepInput(t *testing.T) {
	e := newHEnv(t)
	ctx := context.Background()
	if _, err := e.svc.Create(ctx, e.actor, SubjectForm{Name: "Matematika", Code: "MTK"}); err != nil {
		t.Fatal(err)
	}
	ipa, err := e.svc.Create(ctx, e.actor, SubjectForm{Name: "IPA", Code: "IPA"})
	if err != nil {
		t.Fatal(err)
	}

	// Dua mapel dengan nama sama di sekolah yang sama ditolak.
	rec := e.do(http.MethodPost, "/pengaturan/mapel", adminTU, newForm("matematika", "BARU"))
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "Nama mata pelajaran sudah dipakai.") {
		t.Fatalf("duplicate name: status = %d\n%s", rec.Code, body)
	}
	if !strings.Contains(body, `value="matematika"`) || !strings.Contains(body, `value="BARU"`) {
		t.Error("the rejected input is not shown again in the add form")
	}
	if list, _ := e.svc.ListAll(ctx, e.schoolA); len(list) != 2 {
		t.Errorf("subjects after rejected create = %d, want 2", len(list))
	}

	// Kode kembar juga ditolak.
	if rec := e.do(http.MethodPost, "/pengaturan/mapel", adminTU, newForm("Fisika", "mtk")); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "Kode mata pelajaran sudah dipakai.") {
		t.Errorf("duplicate code: status = %d", rec.Code)
	}

	// Gagal mengubah: pesan tampil di form baris itu, isian tetap, form terbuka.
	rec = e.do(http.MethodPost, "/pengaturan/mapel/"+ipa.ID, adminTU, editForm("Matematika", "IPA", StatusActive))
	body = rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "Nama mata pelajaran sudah dipakai.") ||
		!strings.Contains(body, `x-data="{ editing: true }"`) {
		t.Errorf("duplicate name on update: status = %d, edit form open = %v", rec.Code, strings.Contains(body, `editing: true`))
	}
	if got, _ := GetSubjectByID(ctx, e.tx, e.schoolA, ipa.ID); got.Name != "IPA" {
		t.Errorf("subject changed by rejected update: %+v", got)
	}

	// Status yang hilang tidak diam-diam menonaktifkan mapel.
	rec = e.do(http.MethodPost, "/pengaturan/mapel/"+ipa.ID, adminTU, url.Values{"name": {"IPA"}, "code": {"IPA"}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing status: status = %d, want 400", rec.Code)
	}
	if got, _ := GetSubjectByID(ctx, e.tx, e.schoolA, ipa.ID); !got.IsActive {
		t.Error("a form without status deactivated the subject")
	}

	// Nama kosong.
	if rec := e.do(http.MethodPost, "/pengaturan/mapel", adminTU, newForm("  ", "")); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "Nama mata pelajaran wajib diisi.") {
		t.Errorf("empty name: status = %d", rec.Code)
	}
}

func TestSubjectHandler_OtherSchoolIsInvisible(t *testing.T) {
	e := newHEnv(t)
	ctx := context.Background()
	secret, err := e.svc.Create(ctx, e.actorB, SubjectForm{Name: "Rahasia Sekolah B", Code: "RHS"})
	if err != nil {
		t.Fatal(err)
	}

	// Daftar sekolah A tidak memuat mapel sekolah B.
	if body := e.do(http.MethodGet, "/pengaturan/mapel", adminTU, nil).Body.String(); strings.Contains(body, "Rahasia Sekolah B") || strings.Contains(body, secret.ID) {
		t.Error("the list of school A shows a subject of school B")
	}

	// Mengubah mapel sekolah B dari sekolah A: 404, tanpa perubahan.
	for _, status := range []string{StatusActive, StatusInactive} {
		rec := e.do(http.MethodPost, "/pengaturan/mapel/"+secret.ID, adminTU, editForm("Diretas", "HCK", status))
		if rec.Code != http.StatusNotFound {
			t.Errorf("update of another school's subject (%s): status = %d, want 404", status, rec.Code)
		}
	}
	// ID yang bukan UUID juga 404.
	if rec := e.do(http.MethodPost, "/pengaturan/mapel/bukan-uuid", adminTU, editForm("X", "", StatusActive)); rec.Code != http.StatusNotFound {
		t.Errorf("invalid id: status = %d, want 404", rec.Code)
	}
	got, _ := GetSubjectByID(ctx, e.tx, e.schoolB, secret.ID)
	if got.Name != "Rahasia Sekolah B" || got.Code != "RHS" || !got.IsActive {
		t.Errorf("subject of school B was changed: %+v", got)
	}
	if n := e.audits(e.schoolB, ActionSubjectUpdate) + e.audits(e.schoolB, ActionSubjectDeactivate); n != 0 {
		t.Errorf("school B update audits = %d, want 0", n)
	}

	// Sekolah B boleh memakai nama dan kode yang sama dengan sekolah A.
	if rec := e.do(http.MethodPost, "/pengaturan/mapel", adminTU, newForm("Rahasia Sekolah B", "RHS")); rec.Code != http.StatusSeeOther {
		t.Errorf("school A reusing the name of a school B subject: status = %d, want 303", rec.Code)
	}
}
