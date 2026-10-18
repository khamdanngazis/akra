package portal

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/payments"
	"akra/internal/platform/clock"
	"akra/internal/platform/render"
	"akra/internal/platform/storage"
	"akra/internal/school"
	"akra/web"
)

// treasurerReceipt mengunduh kuitansi lewat handler bendahara (T41).
func (f *fixture) treasurerReceipt(t *testing.T, userID, paymentID string) []byte {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, _ := render.New(templates, static, logger)
	store, _ := storage.NewLocal(t.TempDir())
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.Identity{UserID: userID, SchoolID: f.schoolA, Roles: []string{auth.RoleBendahara}}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	payments.NewHandler(payments.NewService(f.tx), payments.NewProofService(f.tx, store), payments.NewVoidService(f.tx), sm, rd, logger).Routes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/keuangan/pembayaran/"+paymentID+"/kuitansi", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("treasurer receipt: status = %d", rec.Code)
	}
	return rec.Body.Bytes()
}

func TestParentReceipt(t *testing.T) {
	f := newFixture(t)
	d := f.billingData(t)
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := f.tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	bendahara := one(`SELECT id FROM users WHERE school_id = $1 AND email = 'bendahara@uji.id'`, f.schoolA)
	active := one(`SELECT id FROM payments WHERE invoice_id = $1 AND amount = 50000`, d.sept.ID)

	// Pembayaran keluarga lain (Citra) di sekolah yang sama.
	citraInv := one(`SELECT i.id FROM invoices i JOIN enrollments e ON e.id = i.enrollment_id WHERE e.student_id = $1`, f.citra)
	citraPay, err := payments.RecordPayment(ctx, f.tx, school.Actor{SchoolID: f.schoolA, UserID: bendahara},
		payments.Request{InvoiceID: citraInv, Amount: 1000, PaidOn: clock.Today(), Method: payments.MethodCash})
	if err != nil {
		t.Fatal(err)
	}
	// Pembayaran sekolah lain (Dewi).
	yearB := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, f.schoolB)
	classB := one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '1A', 1) RETURNING id`, f.schoolB, yearB)
	enrB := one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, '2026-07-13') RETURNING id`, f.schoolB, f.dewi, classB, yearB)
	actorB := school.Actor{SchoolID: f.schoolB, UserID: one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'b@uji.id', 'x', 'B') RETURNING id`, f.schoolB)}
	ftB, err := billing.CreateFeeType(ctx, f.tx, f.schoolB, billing.FeeTypeInput{Name: "SPP", DefaultAmount: 5000})
	if err != nil {
		t.Fatal(err)
	}
	invB, err := billing.CreateInvoice(ctx, f.tx, actorB, billing.InvoiceRequest{EnrollmentID: enrB, Period: "2026-09", DueDate: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), Items: []billing.ItemRequest{{FeeTypeID: ftB.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	payB, err := payments.RecordPayment(ctx, f.tx, actorB, payments.Request{InvoiceID: invB.ID, Amount: 5000, PaidOn: clock.Today(), Method: payments.MethodCash})
	if err != nil {
		t.Fatal(err)
	}

	downloads := func() int {
		var n int
		_ = f.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'receipt.download_parent' AND user_id = $1`, f.userSari).Scan(&n)
		return n
	}
	get := func(paymentID, roles string) *httptest.ResponseRecorder {
		return f.getPath("/portal/pembayaran/"+paymentID+"/kuitansi", f.userSari, f.schoolA, roles)
	}

	rec := get(active, "orang_tua")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" || rec.Header().Get("Cache-Control") != "private, no-store" ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), `attachment; filename="kuitansi-KW-`) {
		t.Fatalf("download: status = %d, headers = %v", rec.Code, rec.Header())
	}
	if !bytes.Equal(rec.Body.Bytes(), f.treasurerReceipt(t, bendahara, active)) {
		t.Error("parent PDF differs from the treasurer's PDF")
	}
	if n := downloads(); n != 1 {
		t.Errorf("audit rows = %d, want 1", n)
	}

	// Pembayaran yang dibatalkan tetap bisa diunduh (dengan watermark).
	if rec := get(d.voided.ID, "orang_tua"); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), f.treasurerReceipt(t, bendahara, d.voided.ID)) {
		t.Errorf("voided receipt: status = %d or differs from treasurer's", rec.Code)
	}
	if rc, err := NewService(f.tx).ParentReceipt(ctx, school.Actor{SchoolID: f.schoolA, UserID: f.userSari}, d.voided.ID); err != nil || !rc.Voided || rc.VoidReason != "transfer ganda" {
		t.Errorf("voided receipt data = %+v, %v", rc, err)
	}
	before := downloads()

	notFound := get("00000000-0000-0000-0000-000000000000", "orang_tua")
	for name, id := range map[string]string{"other family": citraPay.ID, "other school": payB.ID, "invalid": "abc"} {
		rec := get(id, "orang_tua")
		if rec.Code != http.StatusNotFound || rec.Body.String() != notFound.Body.String() {
			t.Errorf("%s: status = %d, want identical 404", name, rec.Code)
		}
	}
	// Wali kedua Ani (Agus) juga boleh; wali tanpa anak tidak.
	if rec := f.getPath("/portal/pembayaran/"+active+"/kuitansi", f.userAgus, f.schoolA, "orang_tua"); rec.Code != http.StatusOK {
		t.Errorf("second guardian: status = %d", rec.Code)
	}
	if rec := f.getPath("/portal/pembayaran/"+active+"/kuitansi", f.none, f.schoolA, "orang_tua"); rec.Code != http.StatusNotFound {
		t.Errorf("guardian without children: status = %d, want 404", rec.Code)
	}
	if rec := get(active, "bendahara"); rec.Code != http.StatusForbidden {
		t.Errorf("staff on portal route: status = %d, want 403", rec.Code)
	}
	if n := downloads(); n != before {
		t.Errorf("failed downloads were audited: %d -> %d", before, n)
	}

	page := f.getPath("/portal/siswa/"+f.ani+"/tagihan", f.userSari, f.schoolA, "orang_tua").Body.String()
	if !strings.Contains(page, `href="/portal/pembayaran/`+active+`/kuitansi"`) || !strings.Contains(page, "Unduh kuitansi (dibatalkan)") {
		t.Error("billing page must link every payment to its receipt")
	}
}
