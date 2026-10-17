package billing

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

type hEnv struct {
	*fixture
	router  http.Handler
	cookies map[string]*http.Cookie
}

const idHeader = "X-Test-Identity" // "<A|B>|<role,role>"

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
			schoolID := f.schoolA
			if s == "B" {
				schoolID = f.schoolB
			}
			id := auth.Identity{UserID: f.actor.UserID, SchoolID: schoolID, Roles: strings.Split(roles, ","), FullName: "Uji", SchoolName: "Uji"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	NewFeeTypeHandler(NewFeeTypeService(f.tx), sm, rd, logger).Routes(r)
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

func (e *hEnv) audits(action string) int {
	var n int
	_ = e.tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, e.schoolA, action).Scan(&n)
	return n
}

func feeForm(name, amount, day string) url.Values {
	return url.Values{"name": {name}, "default_amount": {amount}, "default_due_day": {day}}
}

const (
	treasurer = "A|bendahara"
	adminTU   = "A|admin_tu"
	principal = "A|kepala_sekolah"
	teacher   = "A|guru"
	parent    = "A|orang_tua"
)

var idInEditLink = regexp.MustCompile(`/keuangan/jenis-tagihan/([0-9a-f-]{36})/ubah`)

func TestFeeTypeHandler_TreasurerManagesFeeTypes(t *testing.T) {
	e := newHEnv(t)
	ctx := context.Background()

	rec := e.do(http.MethodPost, "/keuangan/jenis-tagihan", treasurer, feeForm("SPP Bulanan", "Rp150.000", "10"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: status = %d\n%s", rec.Code, rec.Body.String())
	}
	list := e.do(http.MethodGet, "/keuangan/jenis-tagihan", treasurer, nil).Body.String()
	for _, want := range []string{"SPP Bulanan", "Rp150.000", "Tanggal 10", "berhasil dibuat"} {
		if !strings.Contains(list, want) {
			t.Errorf("list does not contain %q", want)
		}
	}
	id := idInEditLink.FindStringSubmatch(list)[1]

	// Form ubah menampilkan nominal berformat dan catatan tagihan lama.
	form := e.do(http.MethodGet, "/keuangan/jenis-tagihan/"+id+"/ubah", treasurer, nil).Body.String()
	if !strings.Contains(form, `value="150.000"`) || !strings.Contains(form, "tidak</strong> mengubah tagihan yang sudah dibuat") {
		t.Error("edit form misses formatted amount or the note about existing invoices")
	}
	if rec := e.do(http.MethodPost, "/keuangan/jenis-tagihan/"+id, treasurer, feeForm("SPP Bulanan", "175.000", "")); rec.Code != http.StatusSeeOther {
		t.Fatalf("update: status = %d", rec.Code)
	}
	ft, _ := GetFeeType(ctx, e.tx, e.schoolA, id)
	if ft.DefaultAmount != 175000 || ft.DefaultDueDay != 0 {
		t.Errorf("after update = %+v", ft)
	}

	if rec := e.do(http.MethodPost, "/keuangan/jenis-tagihan/"+id+"/nonaktifkan", treasurer, url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("deactivate: status = %d", rec.Code)
	}
	if active, _ := ListActiveFeeTypes(ctx, e.tx, e.schoolA); len(active) != 0 {
		t.Errorf("deactivated fee type is still active: %+v", active)
	}
	list = e.do(http.MethodGet, "/keuangan/jenis-tagihan", treasurer, nil).Body.String()
	if !strings.Contains(list, "Nonaktif") || !strings.Contains(list, "/"+id+"/aktifkan") {
		t.Error("list does not show the inactive status and activate button")
	}
	e.do(http.MethodPost, "/keuangan/jenis-tagihan/"+id+"/aktifkan", treasurer, url.Values{})
	if ft, _ := GetFeeType(ctx, e.tx, e.schoolA, id); !ft.IsActive {
		t.Error("activate did not work")
	}
	// Admin TU juga boleh.
	if rec := e.do(http.MethodPost, "/keuangan/jenis-tagihan", adminTU, feeForm("Uang Pangkal", "5000000", "")); rec.Code != http.StatusSeeOther {
		t.Errorf("admin create: status = %d", rec.Code)
	}

	for action, want := range map[string]int{ActionFeeTypeCreate: 2, ActionFeeTypeUpdate: 1, ActionFeeTypeDeactivate: 1, ActionFeeTypeActivate: 1} {
		if got := e.audits(action); got != want {
			t.Errorf("audit %s = %d, want %d", action, got, want)
		}
	}
}

func TestFeeTypeHandler_RoleAccess(t *testing.T) {
	e := newHEnv(t)
	ft, err := CreateFeeType(context.Background(), e.tx, e.schoolA, FeeTypeInput{Name: "SPP", DefaultAmount: 100000})
	if err != nil {
		t.Fatal(err)
	}
	routes := []struct{ method, path string }{
		{http.MethodGet, "/keuangan/jenis-tagihan"},
		{http.MethodGet, "/keuangan/jenis-tagihan/baru"},
		{http.MethodPost, "/keuangan/jenis-tagihan"},
		{http.MethodGet, "/keuangan/jenis-tagihan/" + ft.ID + "/ubah"},
		{http.MethodPost, "/keuangan/jenis-tagihan/" + ft.ID},
		{http.MethodPost, "/keuangan/jenis-tagihan/" + ft.ID + "/nonaktifkan"},
		{http.MethodPost, "/keuangan/jenis-tagihan/" + ft.ID + "/aktifkan"},
	}
	for _, who := range []string{teacher, parent} {
		for _, rt := range routes {
			var form url.Values
			if rt.method == http.MethodPost {
				form = feeForm("Diretas", "1", "")
			}
			if rec := e.do(rt.method, rt.path, who, form); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s %s: status = %d, want 403", who, rt.method, rt.path, rec.Code)
			}
		}
	}
	// Kepala sekolah: hanya baca.
	rec := e.do(http.MethodGet, "/keuangan/jenis-tagihan", principal, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "/ubah") || strings.Contains(rec.Body.String(), "Tambah jenis tagihan") {
		t.Errorf("principal list: status = %d, shows edit controls", rec.Code)
	}
	for _, rt := range routes[1:] {
		var form url.Values
		if rt.method == http.MethodPost {
			form = feeForm("Diretas", "1", "")
		}
		if rec := e.do(rt.method, rt.path, principal, form); rec.Code != http.StatusForbidden {
			t.Errorf("principal %s %s: status = %d, want 403", rt.method, rt.path, rec.Code)
		}
	}
	if got, _ := GetFeeType(context.Background(), e.tx, e.schoolA, ft.ID); got.Name != "SPP" || !got.IsActive {
		t.Error("forbidden requests changed the fee type")
	}
}

func TestFeeTypeHandler_ValidationAndIsolation(t *testing.T) {
	e := newHEnv(t)
	other, err := CreateFeeType(context.Background(), e.tx, e.schoolB, FeeTypeInput{Name: "SPP Sekolah B", DefaultAmount: 1})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		form url.Values
		want string
	}{
		{feeForm("", "100.000", ""), "Nama jenis tagihan wajib diisi."},
		{feeForm("SPP", "-5000", ""), "Nominal tidak boleh negatif."},
		{feeForm("SPP", "150.000,50", ""), "rupiah bulat"},
		{feeForm("SPP", "100", "29"), "Tanggal jatuh tempo 1 sampai 28."},
	}
	for _, tc := range cases {
		rec := e.do(http.MethodPost, "/keuangan/jenis-tagihan", treasurer, tc.form)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%v: status = %d, message %q shown = %v", tc.form, rec.Code, tc.want, strings.Contains(rec.Body.String(), tc.want))
		}
	}
	e.do(http.MethodPost, "/keuangan/jenis-tagihan", treasurer, feeForm("SPP", "100", ""))
	if rec := e.do(http.MethodPost, "/keuangan/jenis-tagihan", treasurer, feeForm("spp", "200", "")); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "sudah dipakai") {
		t.Errorf("duplicate name: status = %d", rec.Code)
	}

	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/keuangan/jenis-tagihan/" + other.ID + "/ubah"},
		{http.MethodPost, "/keuangan/jenis-tagihan/" + other.ID},
		{http.MethodPost, "/keuangan/jenis-tagihan/" + other.ID + "/nonaktifkan"},
		{http.MethodGet, "/keuangan/jenis-tagihan/bukan-uuid/ubah"},
	} {
		var form url.Values
		if rt.method == http.MethodPost {
			form = feeForm("Diretas", "1", "")
		}
		if rec := e.do(rt.method, rt.path, treasurer, form); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", rt.method, rt.path, rec.Code)
		}
	}
	if body := e.do(http.MethodGet, "/keuangan/jenis-tagihan", treasurer, nil).Body.String(); strings.Contains(body, "SPP Sekolah B") {
		t.Error("list shows another school's fee type")
	}
}
