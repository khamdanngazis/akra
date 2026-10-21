package builder

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/render"
	"akra/internal/school"
	"akra/internal/testutil"
	"akra/web"
)

func ident(s *testutil.School, u testutil.User) auth.Identity {
	return auth.Identity{SchoolID: s.ID, UserID: u.ID, Roles: []string{u.Role}}
}

func actorFor(s *testutil.School, u testutil.User) school.Actor {
	return school.Actor{SchoolID: s.ID, UserID: u.ID}
}

func cfgErr(t *testing.T, err error) map[string]string {
	t.Helper()
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want ConfigError", err)
	}
	return ce.Fields
}

func countAudits(t *testing.T, tx pgx.Tx, schoolID, action string) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, schoolID, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestConfig_ValidationBeforeSave(t *testing.T) {
	tx, a, _ := seededTx(t)
	svc := NewConfigService(tx, Default)
	ctx := context.Background()
	admin := ident(a, a.Admin)
	good := Config{Source: "attendance", Columns: []string{"nis", "student_name", "status"},
		Filters: []Filter{{Column: "session_date", Op: OpGte, Value: "2026-07-01"}, {Column: "status", Op: OpIn, Values: []string{"sakit", "alpa"}}},
		Order:   []Order{{Column: "session_date", Desc: true}}}
	saved, err := svc.Create(ctx, actorFor(a, a.Admin), admin, ConfigInput{Name: "  Absensi   bulanan ", Config: good})
	if err != nil {
		t.Fatal(err)
	}
	// Tersimpan apa adanya (JSON bolak-balik) dan nama dirapikan.
	got, err := svc.Get(ctx, admin, saved.ID)
	if err != nil || got.Name != "Absensi bulanan" || !reflect.DeepEqual(got.Config, good) || got.OwnerID != a.Admin.ID {
		t.Fatalf("round trip: %v\n got %+v\nwant %+v", err, got.Config, good)
	}
	before := countAudits(t, tx, a.ID, ActionConfigCreate)
	for _, tc := range []struct {
		name  string
		id    auth.Identity
		cfg   Config
		field string
	}{
		{"unknown column", admin, Config{Source: "attendance", Columns: []string{"nis", "password_hash"}}, "columns"},
		{"unknown filter column", admin, Config{Source: "attendance", Columns: []string{"nis"}, Filters: []Filter{{Column: "1=1", Op: OpEq, Value: "x"}}}, "columns"},
		{"unknown source", admin, Config{Source: "users", Columns: []string{"nis"}}, "source"},
		{"operator outside list", admin, Config{Source: "attendance", Columns: []string{"nis"}, Filters: []Filter{{Column: "status", Op: "LIKE", Value: "x"}}}, "filters"},
		{"operator wrong type", admin, Config{Source: "attendance", Columns: []string{"nis"}, Filters: []Filter{{Column: "session_date", Op: OpContains, Value: "2026"}}}, "filters"},
		{"bad date", admin, Config{Source: "attendance", Columns: []string{"nis"}, Filters: []Filter{{Column: "session_date", Op: OpGte, Value: "kemarin"}}}, "filters"},
		{"no columns", admin, Config{Source: "attendance"}, "columns"},
		{"teacher cannot use payments", ident(a, a.Teacher), Config{Source: "payments", Columns: []string{"nis"}}, "source"},
		{"treasurer cannot use attendance", ident(a, a.Treasurer), Config{Source: "attendance", Columns: []string{"nis"}}, "source"},
	} {
		_, err := svc.Create(ctx, actorFor(a, a.Admin), tc.id, ConfigInput{Name: "Uji " + tc.name, Config: tc.cfg})
		if fields := cfgErr(t, err); fields[tc.field] == "" {
			t.Errorf("%s: fields %v, want %s", tc.name, fields, tc.field)
		}
	}
	if n := countAudits(t, tx, a.ID, ActionConfigCreate); n != before {
		t.Errorf("invalid configs were saved: %d audits", n-before)
	}
	if _, err := svc.Create(ctx, actorFor(a, a.Admin), admin, ConfigInput{Name: " ", Config: good}); cfgErr(t, err)["name"] == "" {
		t.Error("blank name accepted")
	}
}

func TestConfig_DuplicateName(t *testing.T) {
	tx, a, _ := seededTx(t)
	svc := NewConfigService(tx, Default)
	ctx := context.Background()
	cfg := Config{Source: "payments", Columns: []string{"nis", "amount"}}
	first, err := svc.Create(ctx, actorFor(a, a.Treasurer), ident(a, a.Treasurer), ConfigInput{Name: "Pembayaran Oktober", Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	// Nama sama untuk pemilik yang sama ditolak, tanpa membedakan huruf.
	if _, err := svc.Create(ctx, actorFor(a, a.Treasurer), ident(a, a.Treasurer), ConfigInput{Name: "pembayaran oktober", Config: cfg}); !strings.Contains(cfgErr(t, err)["name"], "sudah punya") {
		t.Error("duplicate name accepted")
	}
	// Pemilik lain boleh memakai nama yang sama.
	if _, err := svc.Create(ctx, actorFor(a, a.Principal), ident(a, a.Principal), ConfigInput{Name: "Pembayaran Oktober", Config: cfg}); err != nil {
		t.Errorf("same name, other owner: %v", err)
	}
	// Mengganti nama ke nama konfigurasi lain milik sendiri juga ditolak.
	second, err := svc.Create(ctx, actorFor(a, a.Treasurer), ident(a, a.Treasurer), ConfigInput{Name: "Lain", Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, actorFor(a, a.Treasurer), ident(a, a.Treasurer), second.ID, ConfigInput{Name: "PEMBAYARAN OKTOBER", Config: cfg}); cfgErr(t, err)["name"] == "" {
		t.Error("rename to duplicate accepted")
	}
	// Setelah dihapus, namanya boleh dipakai lagi.
	if _, err := svc.Delete(ctx, actorFor(a, a.Treasurer), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, actorFor(a, a.Treasurer), ident(a, a.Treasurer), ConfigInput{Name: "Pembayaran Oktober", Config: cfg}); err != nil {
		t.Errorf("name of deleted config: %v", err)
	}
}

func TestConfig_OwnershipAndSharing(t *testing.T) {
	tx, a, b := seededTx(t)
	svc := NewConfigService(tx, Default)
	ctx := context.Background()
	admin, principal, treasurer := ident(a, a.Admin), ident(a, a.Principal), ident(a, a.Treasurer)
	cfg := Config{Source: "attendance", Columns: []string{"nis", "status"}}
	private, err := svc.Create(ctx, actorFor(a, a.Admin), admin, ConfigInput{Name: "Pribadi", Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := svc.Create(ctx, actorFor(a, a.Admin), admin, ConfigInput{Name: "Bersama", Config: cfg, Shared: true})
	if err != nil {
		t.Fatal(err)
	}
	if countAudits(t, tx, a.ID, ActionConfigShare) != 1 {
		t.Error("share on create not audited")
	}
	// Konfigurasi bersama sekolah B (sumber yang sama) untuk uji isolasi.
	otherShared, err := NewConfigService(tx, Default).Create(ctx, actorFor(b, b.Admin), ident(b, b.Admin), ConfigInput{Name: "Bersama B", Config: cfg, Shared: true})
	if err != nil {
		t.Fatal(err)
	}

	names := func(id auth.Identity) map[string]bool {
		list, err := svc.Visible(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, c := range list {
			out[c.Name] = true
		}
		return out
	}
	// Kepala sekolah (berhak atas absensi) melihat yang dibagikan, tidak yang pribadi.
	if got := names(principal); !got["Bersama"] || got["Pribadi"] || got["Bersama B"] {
		t.Errorf("principal sees %v", got)
	}
	// Bendahara tidak berhak atas sumber absensi: tidak melihat meski dibagikan.
	if got := names(treasurer); got["Bersama"] || got["Pribadi"] {
		t.Errorf("treasurer sees %v", got)
	}
	// Konfigurasi bersama sekolah lain tidak terlihat sama sekali.
	if got := names(admin); got["Bersama B"] || !got["Pribadi"] || !got["Bersama"] {
		t.Errorf("admin sees %v", got)
	}
	if _, err := svc.Get(ctx, admin, otherShared.ID); !errors.Is(err, ErrConfigNotFound) {
		t.Errorf("other school's shared config: %v", err)
	}
	if _, err := svc.Get(ctx, principal, private.ID); !errors.Is(err, ErrConfigNotFound) {
		t.Errorf("someone else's private config: %v", err)
	}
	if got, err := svc.Get(ctx, principal, shared.ID); err != nil || got.Name != "Bersama" {
		t.Errorf("shared config: %v", err)
	}
	// Hanya pemilik yang boleh mengubah atau menghapus, termasuk konfigurasi bersama.
	for _, id := range []string{private.ID, shared.ID} {
		if _, err := svc.Update(ctx, actorFor(a, a.Principal), principal, id, ConfigInput{Name: "Diambil", Config: cfg}); !errors.Is(err, ErrNotOwner) {
			t.Errorf("update by non-owner: %v", err)
		}
		if _, err := svc.Delete(ctx, actorFor(a, a.Principal), id); !errors.Is(err, ErrNotOwner) {
			t.Errorf("delete by non-owner: %v", err)
		}
	}
	if _, err := svc.Update(ctx, actorFor(a, a.Admin), admin, otherShared.ID, ConfigInput{Name: "X", Config: cfg}); !errors.Is(err, ErrConfigNotFound) {
		t.Errorf("update other school's config: %v", err)
	}
	// Pemilik: ubah isi (audit update), tanpa perubahan (tanpa audit), berhenti berbagi (audit share).
	if _, err := svc.Update(ctx, actorFor(a, a.Admin), admin, shared.ID, ConfigInput{Name: "Bersama", Config: Config{Source: "attendance", Columns: []string{"nis"}}, Shared: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, actorFor(a, a.Admin), admin, shared.ID, ConfigInput{Name: "Bersama", Config: Config{Source: "attendance", Columns: []string{"nis"}}, Shared: true}); err != nil {
		t.Fatal(err)
	}
	if n := countAudits(t, tx, a.ID, ActionConfigUpdate); n != 1 {
		t.Errorf("update audits = %d, want 1", n)
	}
	if _, err := svc.Update(ctx, actorFor(a, a.Admin), admin, shared.ID, ConfigInput{Name: "Bersama", Config: Config{Source: "attendance", Columns: []string{"nis"}}}); err != nil {
		t.Fatal(err)
	}
	if n := countAudits(t, tx, a.ID, ActionConfigShare); n != 2 {
		t.Errorf("share audits = %d, want 2", n)
	}
	if got := names(principal); got["Bersama"] {
		t.Error("unshared config still visible")
	}
	// Hapus lunak: tidak terlihat lagi, berbagi dimatikan, baris tetap ada.
	if _, err := svc.Update(ctx, actorFor(a, a.Admin), admin, shared.ID, ConfigInput{Name: "Bersama", Config: cfg, Shared: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Delete(ctx, actorFor(a, a.Admin), shared.ID); err != nil {
		t.Fatal(err)
	}
	var isShared, deleted bool
	if err := tx.QueryRow(ctx, `SELECT is_shared, deleted_at IS NOT NULL FROM report_configs WHERE id = $1`, shared.ID).Scan(&isShared, &deleted); err != nil || isShared || !deleted {
		t.Errorf("soft delete: shared %v deleted %v (%v)", isShared, deleted, err)
	}
	if _, err := svc.Get(ctx, admin, shared.ID); !errors.Is(err, ErrConfigNotFound) {
		t.Errorf("deleted config: %v", err)
	}
	if n := countAudits(t, tx, a.ID, ActionConfigDelete); n != 1 {
		t.Errorf("delete audits = %d", n)
	}
}

type doFunc func(method, path string, u testutil.User, form url.Values) *httptest.ResponseRecorder

// configRouter memasang handler konfigurasi dan pembangun laporan untuk
// pengguna sekolah a (dipilih lewat header X-User).
func configRouter(t *testing.T, tx pgx.Tx, a *testutil.School) doFunc {
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
	users := map[string]testutil.User{}
	for _, u := range a.Users() {
		users[u.ID] = u
	}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			u := users[req.Header.Get("X-User")]
			id := auth.Identity{UserID: u.ID, SchoolID: a.ID, Roles: []string{u.Role}, FullName: "Uji", SchoolName: "A"}
			next.ServeHTTP(w, req.WithContext(auth.ContextWithIdentity(req.Context(), id)))
		})
	})
	NewConfigHandler(NewConfigService(tx, Default), nil, sm, rd, logger).Routes(r)
	return func(method, path string, u testutil.User, form url.Values) *httptest.ResponseRecorder {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req := httptest.NewRequest(method, path, body)
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.Header.Set("X-User", u.ID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
}

func TestConfigHandler(t *testing.T) {
	tx, a, b := seededTx(t)
	do := configRouter(t, tx, a)

	// Simpan lewat form: kolom, filter "in" dipisah koma, urutan.
	form := url.Values{"name": {"Tunggakan besar"}, "source": {"arrears"}, "columns_arrears": {"nis", "remaining"},
		"f_arrears_col_0": {"remaining"}, "f_arrears_op_0": {">="}, "f_arrears_val_0": {"100000"},
		"f_arrears_col_1": {"class_name"}, "f_arrears_op_1": {"in"}, "f_arrears_val_1": {"1A, 1B"},
		"s_arrears_col_0": {"remaining"}, "s_arrears_desc_0": {"on"},
		// Isian sumber lain diabaikan.
		"columns_payments": {"amount"}, "f_payments_col_0": {"amount"}, "f_payments_op_0": {"="}, "f_payments_val_0": {"x"}}
	if rec := do(http.MethodPost, "/laporan/konfigurasi", a.Treasurer, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("create: status = %d\n%s", rec.Code, rec.Body.String())
	}
	list, _ := NewConfigService(tx, Default).Visible(context.Background(), ident(a, a.Treasurer))
	var saved SavedConfig
	for _, c := range list {
		if c.Name == "Tunggakan besar" {
			saved = c
		}
	}
	want := Config{Source: "arrears", Columns: []string{"nis", "remaining"},
		Filters: []Filter{{Column: "remaining", Op: OpGte, Value: "100000"}, {Column: "class_name", Op: OpIn, Values: []string{"1A", "1B"}}},
		Order:   []Order{{Column: "remaining", Desc: true}}}
	if !reflect.DeepEqual(saved.Config, want) {
		t.Fatalf("saved %+v\nwant  %+v", saved.Config, want)
	}
	// Kolom tidak terdaftar: 422, tidak tersimpan.
	bad := url.Values{"name": {"Jahat"}, "source": {"arrears"}, "columns_arrears": {"nis", "st.password_hash"}}
	if rec := do(http.MethodPost, "/laporan/konfigurasi", a.Treasurer, bad); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "tidak terdaftar") {
		t.Errorf("unknown column: status = %d", rec.Code)
	}
	// Pengguna lain: 403 untuk ubah dan hapus; sekolah lain dan ID rusak 404.
	path := "/laporan/konfigurasi/" + saved.ID
	edit := url.Values{"name": {"Diambil"}, "source": {"arrears"}, "columns_arrears": {"nis"}}
	for _, u := range []testutil.User{a.Admin, a.Principal} {
		if rec := do(http.MethodPost, path, u, edit); rec.Code != http.StatusForbidden {
			t.Errorf("update as %s: status = %d", u.Role, rec.Code)
		}
		if rec := do(http.MethodPost, path+"/hapus", u, url.Values{}); rec.Code != http.StatusForbidden {
			t.Errorf("delete as %s: status = %d", u.Role, rec.Code)
		}
	}
	for _, p := range []string{"/laporan/konfigurasi/" + b.ReportConfigID, "/laporan/konfigurasi/bukan"} {
		if rec := do(http.MethodPost, p, a.Treasurer, edit); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: status = %d", p, rec.Code)
		}
	}
	// Halaman: pemilik melihat form ubah; guru 403 (tidak punya sumber).
	if rec := do(http.MethodGet, "/laporan/konfigurasi?ubah="+saved.ID, a.Treasurer, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `value="Tunggakan besar"`) {
		t.Errorf("edit page: status = %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/laporan/konfigurasi?ubah="+saved.ID, a.Principal, nil); rec.Code != http.StatusNotFound {
		t.Errorf("edit private config of someone else: status = %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/laporan/konfigurasi", a.Teacher, nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher: status = %d", rec.Code)
	}
	// Pemilik menghapus.
	if rec := do(http.MethodPost, path+"/hapus", a.Treasurer, url.Values{}); rec.Code != http.StatusSeeOther {
		t.Errorf("delete: status = %d", rec.Code)
	}
}
