package payments

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/render"
	"akra/internal/platform/storage"
	"akra/web"
)

type client struct {
	t       *testing.T
	h       http.Handler
	cookies []*http.Cookie
}

// newClient membuat router uji. stores opsional: storage yang dipakai bukti
// transfer (default: driver local baru).
func newClient(t *testing.T, f *fixture, stores ...storage.Store) *client {
	t.Helper()
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
			id := auth.Identity{UserID: f.actor.UserID, SchoolID: f.schoolA, Roles: strings.Split(r.Header.Get("X-Roles"), ","), FullName: "Uji", SchoolName: "Uji"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	var store storage.Store
	if len(stores) > 0 {
		store = stores[0]
	} else if store, err = storage.NewLocal(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	NewHandler(f.svc, NewProofService(f.tx, store), sm, rd, logger).Routes(r)
	return &client{t: t, h: r}
}

// do mengirim request dan menyimpan cookie sesi (untuk pesan flash).
func (c *client) do(method, path, roles string, form url.Values) *httptest.ResponseRecorder {
	c.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-Roles", roles)
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	if cks := rec.Result().Cookies(); len(cks) > 0 {
		c.cookies = cks
	}
	return rec
}

func (f *fixture) studentOf(t *testing.T, inv billing.Invoice) string {
	t.Helper()
	var id string
	if err := f.tx.QueryRow(context.Background(), `SELECT student_id FROM enrollments WHERE id = $1`, inv.EnrollmentID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func payForm(studentID string, amounts map[string]string, method, bank string) url.Values {
	v := url.Values{"student_id": {studentID}, "method": {method}, "bank_account": {bank}, "paid_on": {clock.Today().Format("2006-01-02")}}
	if method == "" {
		v.Set("method", "tunai")
	}
	for id, a := range amounts {
		v.Add("invoice_id", id)
		v.Set("amount_"+id, a)
	}
	return v
}

func TestHandler_Roles(t *testing.T) {
	f := newFixture(t)
	c := newClient(t, f)
	student := f.studentOf(t, f.invA)
	form := payForm(student, map[string]string{f.invA.ID: "150.000"}, "", "")
	for _, roles := range []string{"guru", "orang_tua", "admin_tu", "kepala_sekolah"} {
		for _, rec := range []*httptest.ResponseRecorder{
			c.do(http.MethodGet, "/keuangan/pembayaran/baru?siswa="+student, roles, nil),
			c.do(http.MethodGet, "/keuangan/pembayaran/cari?q=001", roles, nil),
			c.do(http.MethodPost, "/keuangan/pembayaran", roles, form),
		} {
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s: status = %d, want 403", roles, rec.Code)
			}
		}
	}
	for _, roles := range []string{"guru", "orang_tua"} {
		if rec := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, roles, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s invoice detail: status = %d, want 403", roles, rec.Code)
		}
	}
	for _, roles := range []string{"admin_tu", "kepala_sekolah"} {
		rec := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, roles, nil)
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Catat pembayaran") {
			t.Errorf("%s invoice detail: status = %d, want 200 read-only", roles, rec.Code)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM payments WHERE school_id = $1`, f.schoolA); n != 0 {
		t.Errorf("payments saved = %d", n)
	}
}

func TestHandler_SearchIsScopedToSchool(t *testing.T) {
	f := newFixture(t)
	c := newClient(t, f)
	// Sekolah A dan B sama-sama punya siswa NIS 001.
	rec := c.do(http.MethodGet, "/keuangan/pembayaran/cari?q=001", "bendahara", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Contains(body, "<html") {
		t.Fatalf("status = %d, want 200 fragment", rec.Code)
	}
	if !strings.Contains(body, "siswa="+f.studentOf(t, f.invA)) || strings.Contains(body, "siswa="+f.studentOf(t, f.invB)) {
		t.Errorf("search must return school A student only: %s", body)
	}
	if body := c.do(http.MethodGet, "/keuangan/pembayaran/cari?q=1", "bendahara", nil).Body.String(); strings.Contains(body, "siswa=") {
		t.Error("one-letter query must not return results")
	}
	// Tanpa JavaScript: hasil dirender di halaman.
	if body := c.do(http.MethodGet, "/keuangan/pembayaran/baru?q=001", "bendahara", nil).Body.String(); !strings.Contains(body, "siswa="+f.studentOf(t, f.invA)) {
		t.Error("search page without JS does not list results")
	}
	if rec := c.do(http.MethodGet, "/keuangan/pembayaran/baru?siswa="+f.studentOf(t, f.invB), "bendahara", nil); rec.Code != http.StatusNotFound {
		t.Errorf("form for other school student: status = %d, want 404", rec.Code)
	}
	if rec := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invB.ID, "bendahara", nil); rec.Code != http.StatusNotFound {
		t.Errorf("other school invoice: status = %d, want 404", rec.Code)
	}
}

func TestHandler_RecordPayments(t *testing.T) {
	f := newFixture(t)
	c := newClient(t, f)
	ctx := context.Background()
	student := f.studentOf(t, f.invA)

	page := c.do(http.MethodGet, "/keuangan/pembayaran/baru?siswa="+student, "bendahara", nil).Body.String()
	if !strings.Contains(page, `name="amount_`+f.invA.ID+`" value="150.000"`) || !strings.Contains(page, "sisa Rp150.000") {
		t.Fatal("form does not default the amount to the remaining balance")
	}

	// Sebagian.
	rec := c.do(http.MethodPost, "/keuangan/pembayaran", "bendahara", payForm(student, map[string]string{f.invA.ID: "50.000"}, "", ""))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/keuangan/tagihan/"+f.invA.ID {
		t.Fatalf("partial: status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusPartial {
		t.Errorf("status = %s, want sebagian", s)
	}
	detail := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, "bendahara", nil).Body.String()
	for _, want := range []string{"Pembayaran Rp50.000 tercatat.", "Sebagian", "Rp100.000", "Catat pembayaran"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail page missing %q", want)
		}
	}

	// Transfer tanpa rekening ditolak.
	rec = c.do(http.MethodPost, "/keuangan/pembayaran", "bendahara", payForm(student, map[string]string{f.invA.ID: "100.000"}, "transfer", " "))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Rekening tujuan wajib diisi") {
		t.Errorf("transfer without bank account: status = %d", rec.Code)
	}

	// Dua tagihan siswa yang sama; yang kedua melebihi sisa: tidak ada yang tersimpan.
	ft, err := billing.CreateFeeType(ctx, f.tx, f.schoolA, billing.FeeTypeInput{Name: "Kegiatan", DefaultAmount: 75000})
	if err != nil {
		t.Fatal(err)
	}
	inv2, err := billing.CreateInvoice(ctx, f.tx, f.actor, billing.InvoiceRequest{EnrollmentID: f.invA.EnrollmentID, Period: "2026-10",
		DueDate: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	before := f.count(t, `SELECT count(*) FROM payments WHERE school_id = $1`, f.schoolA)
	rec = c.do(http.MethodPost, "/keuangan/pembayaran", "bendahara", payForm(student, map[string]string{f.invA.ID: "100.000", inv2.ID: "75.001"}, "", ""))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Nominal melebihi sisa tagihan.") {
		t.Errorf("overpayment: status = %d", rec.Code)
	}
	if n := f.count(t, `SELECT count(*) FROM payments WHERE school_id = $1`, f.schoolA); n != before {
		t.Errorf("payments after failed form = %d, want %d", n, before)
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusPartial {
		t.Errorf("first invoice status = %s after rollback, want sebagian", s)
	}

	// Tagihan siswa lain di form siswa ini: 404, tidak tersimpan.
	rec = c.do(http.MethodPost, "/keuangan/pembayaran", "bendahara", payForm(student, map[string]string{f.invA2.ID: "1.000"}, "", ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("invoice of another student: status = %d, want 404", rec.Code)
	}

	// Dua tagihan sekaligus: keduanya lunas.
	rec = c.do(http.MethodPost, "/keuangan/pembayaran", "bendahara", payForm(student, map[string]string{f.invA.ID: "100.000", inv2.ID: "75000"}, "transfer", "BCA 123"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/keuangan/pembayaran/baru?siswa="+student {
		t.Fatalf("two invoices: status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if f.status(t, f.invA.ID) != billing.StatusPaid || f.status(t, inv2.ID) != billing.StatusPaid {
		t.Error("both invoices must be lunas")
	}
	page = c.do(http.MethodGet, "/keuangan/pembayaran/baru?siswa="+student, "bendahara", nil).Body.String()
	if !strings.Contains(page, "2 pembayaran tercatat, total Rp175.000.") || !strings.Contains(page, "Tidak ada tagihan yang belum lunas") {
		t.Error("form after paying everything does not show flash and empty state")
	}
	if detail := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, "bendahara", nil).Body.String(); strings.Contains(detail, "Catat pembayaran") || !strings.Contains(detail, "Ke BCA 123") {
		t.Error("paid invoice detail must hide the pay button and list the transfer")
	}
}
