package auditview

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/web"
)

type fixture struct {
	tx                pgx.Tx
	schoolA, schoolB  string
	kepala, bendahara string
	h                 http.Handler
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
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('test-auv-a', 'test-auv-a') RETURNING id`)
	f.schoolB = one(`INSERT INTO schools (name, code) VALUES ('test-auv-b', 'test-auv-b') RETURNING id`)
	f.kepala = one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'kepala@uji.id', 'x', 'Bu Kepala') RETURNING id`, f.schoolA)
	f.bendahara = one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'bendahara@uji.id', 'x', 'Pak Bendahara') RETURNING id`, f.schoolA)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.Identity{UserID: f.kepala, SchoolID: f.schoolA, Roles: strings.Split(r.Header.Get("X-Roles"), ","), FullName: "Uji", SchoolName: "Uji"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	NewHandler(tx, rd, logger).Routes(r)
	f.h = r
	return f
}

func (f *fixture) log(t *testing.T, school, user, action, entity, at, after string) {
	t.Helper()
	var u any
	if user != "" {
		u = user
	}
	if _, err := f.tx.Exec(context.Background(), `INSERT INTO audit_logs (school_id, user_id, action, entity_type, entity_id, after, created_at)
		VALUES ($1, $2, $3, 'payment', $4, $5, $6)`, school, u, action, entity, after, at); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) get(path, roles string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Roles", roles)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func TestAuditPage(t *testing.T) {
	f := newFixture(t)
	f.log(t, f.schoolA, f.bendahara, "payment.create", "pay-1", "2026-10-01T09:00:00+07:00", `{"amount": 150000, "method": "transfer"}`)
	f.log(t, f.schoolA, f.bendahara, "payment.void_request", "pay-1", "2026-10-02T09:15:00+07:00", `{"reason": "transfer ganda", "amount": 150000}`)
	f.log(t, f.schoolA, f.kepala, "payment.void_approve", "pay-1", "2026-10-02T23:59:00+07:00", `{"status": "approved"}`)
	f.log(t, f.schoolA, f.bendahara, "student.create", "stu-1", "2026-10-02T10:00:00+07:00", `{"full_name": "Siswa Rahasia"}`)
	f.log(t, f.schoolB, "", "payment.create", "pay-b", "2026-10-02T10:00:00+07:00", `{"amount": 99}`)

	for _, roles := range []string{"guru", "bendahara", "orang_tua"} {
		if rec := f.get("/keuangan/audit", roles); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", roles, rec.Code)
		}
	}
	for _, roles := range []string{"kepala_sekolah", "admin_tu"} {
		if rec := f.get("/keuangan/audit", roles); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", roles, rec.Code)
		}
	}

	body := f.get("/keuangan/audit", "kepala_sekolah").Body.String()
	for _, want := range []string{"Pembayaran dicatat", "Pembatalan diajukan", "Pembatalan disetujui", "Pak Bendahara", "Bu Kepala", "Rp150.000", "transfer ganda", "1 Okt 2026 09.00 WIB"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, "Siswa Rahasia") || strings.Contains(body, "pay-b") {
		t.Error("page shows non-finance actions or another school's rows")
	}

	// "Siapa membatalkan pembayaran pay-1?"
	body = f.get("/keuangan/audit?aksi=payment.void_approve&entity=pay-1", "kepala_sekolah").Body.String()
	if !strings.Contains(body, "<strong>Pembatalan disetujui</strong>") || strings.Contains(body, "<strong>Pembayaran dicatat</strong>") || !strings.Contains(body, "Bu Kepala") {
		t.Error("filter by action and entity")
	}
	// Tanggal 2 Oktober (WIB) termasuk 23.59.
	body = f.get("/keuangan/audit?dari=2026-10-02&sampai=2026-10-02", "kepala_sekolah").Body.String()
	if strings.Contains(body, "<strong>Pembayaran dicatat</strong>") || !strings.Contains(body, "<strong>Pembatalan diajukan</strong>") || !strings.Contains(body, "<strong>Pembatalan disetujui</strong>") {
		t.Error("filter by date range")
	}
	if body := f.get("/keuangan/audit?aksi=student.create", "kepala_sekolah").Body.String(); !strings.Contains(body, "Jenis aksi tidak dikenal.") || strings.Contains(body, "Siswa Rahasia") {
		t.Error("non-finance action filter must be rejected")
	}
	if body := f.get("/keuangan/audit?dari=kemarin", "kepala_sekolah").Body.String(); !strings.Contains(body, "Tanggal tidak valid.") {
		t.Error("invalid date message")
	}

	for i := 0; i < pageSize; i++ {
		f.log(t, f.schoolA, f.bendahara, "payment.proof_view", fmt.Sprintf("proof-%d", i), "2026-10-05T08:00:00+07:00", `{}`)
	}
	body = f.get("/keuangan/audit", "kepala_sekolah").Body.String()
	if !strings.Contains(body, "Muat lebih lama") || strings.Contains(body, "<strong>Pembayaran dicatat</strong>") {
		t.Fatal("first page must be limited and offer the next page")
	}
	i := strings.Index(body, `href="/keuangan/audit?sebelum=`)
	next := strings.ReplaceAll(body[i+6:i+6+strings.Index(body[i+6:], `"`)], "&amp;", "&")
	if body := f.get(next, "kepala_sekolah").Body.String(); !strings.Contains(body, "<strong>Pembayaran dicatat</strong>") || strings.Contains(body, "Muat lebih lama") {
		t.Error("second page")
	}
}
