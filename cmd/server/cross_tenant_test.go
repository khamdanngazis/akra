package main

// Uji otorisasi lintas sekolah (T58). Daftar route dan data uji ada di
// internal/testutil; uji ini ada di sini karena router lengkap (newApp)
// berada di paket main.

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/internal/platform/storage"
	"akra/internal/testutil"
)

const csrfFailureText = "Sesi formulir sudah tidak berlaku"

// Setiap route yang didaftarkan harus punya entri di testutil.Routes, dan
// setiap entri harus menunjuk route yang ada.
func TestCrossTenant_EveryRouteListed(t *testing.T) {
	registered := map[string]bool{}
	err := chi.Walk(newTestRouter(t).(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, r := range testutil.Routes {
		key := r.Method + " " + r.Pattern
		if listed[key] {
			t.Errorf("route %s is listed twice in internal/testutil/routes.go", key)
		}
		listed[key] = true
		if !registered[key] {
			t.Errorf("route %s is listed in internal/testutil/routes.go but not registered; remove or fix the entry", key)
		}
		if r.Kind == 0 {
			t.Errorf("route %s has no Kind", key)
		}
		if r.Kind != testutil.KindPublic && len(r.Roles) == 0 {
			t.Errorf("route %s has no Roles", key)
		}
	}
	for key := range registered {
		if !listed[key] {
			t.Errorf("route %s has no entry in internal/testutil/routes.go: add one with its roles and requests that point to another school's data (T58)", key)
		}
	}
}

// Pengguna sekolah A tidak bisa membaca atau mengubah data sekolah B, dan
// sebaliknya, di setiap route utama.
func TestCrossTenant_NoAccessToOtherSchool(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hasher := auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1})
	hash, err := hasher.HashPassword(testutil.Password)
	if err != nil {
		t.Fatal(err)
	}
	a, b := testutil.SeedTwoSchools(t, tx, hash)
	rd, err := render.New(mustSub(t, "templates"), mustSub(t, "static"), logger)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := newApp(appDeps{
		db: tx, pinger: pool, sessions: auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger),
		render: rd, static: mustSub(t, "static"), mailer: &captureMailer{}, hasher: hasher,
		baseURL: "https://akra.test", logger: logger, store: store,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, dir := range []struct {
		name       string
		own, other *testutil.School
	}{{"A to B", a, b}, {"B to A", b, a}} {
		t.Run(dir.name, func(t *testing.T) {
			before := schoolSnapshot(t, tx, dir.other.ID)
			for _, u := range dir.own.Users() {
				c := newClient(t, app)
				if rec := c.login(dir.own.Code, u.Email, testutil.Password); rec.Code != http.StatusSeeOther {
					t.Fatalf("login %s: status = %d", u.Email, rec.Code)
				}
				c.get(map[bool]string{true: "/portal/", false: "/"}[u.Role == testutil.Parent]) // token CSRF
				for _, r := range testutil.Routes {
					if r.Kind == testutil.KindPublic {
						continue
					}
					checkRoute(t, c, r, u, dir.own, dir.other)
				}
				// Kontrol positif setelah semua uji lintas sekolah pengguna ini,
				// karena sebagian mengubah data sekolah sendiri.
				for _, r := range testutil.Routes {
					checkOwnRoute(t, c, r, u, dir.own)
				}
			}
			for _, r := range testutil.Routes {
				if r.Kind == testutil.KindByID && !checked[r.Method+" "+r.Pattern] {
					t.Errorf("route %s %s was never checked with an allowed role", r.Method, r.Pattern)
				}
			}
			after := schoolSnapshot(t, tx, dir.other.ID)
			for table, sum := range before {
				if after[table] != sum {
					t.Errorf("data of the other school changed in table %s", table)
				}
			}
		})
	}
}

// checked mencatat route KindByID yang sudah diuji dengan ID sekolah lain.
var checked = map[string]bool{}

func checkRoute(t *testing.T, c *client, r testutil.Route, u testutil.User, own, other *testutil.School) {
	t.Helper()
	reqs := []testutil.Request{{Path: r.Pattern}}
	if r.Requests != nil {
		reqs = r.Requests(own, other)
	}
	allowed := slices.Contains(r.Roles, u.Role)
	for _, req := range reqs {
		rec := c.do(r.Method, req.Path, req.Form)
		body := responseText(rec.Body.Bytes())
		where := r.Method + " " + req.Path + " as " + u.Role
		if strings.Contains(body, csrfFailureText) {
			t.Errorf("%s: CSRF check failed, the request did not reach the handler", where)
			continue
		}
		if strings.Contains(body, other.Marker) {
			t.Errorf("%s: response contains data of the other school (%s)", where, other.Marker)
		}
		code := rec.Code
		switch {
		case !allowed:
			if code != http.StatusForbidden {
				t.Errorf("%s: status = %d, want 403 (role not allowed)", where, code)
			}
		case code >= 500:
			t.Errorf("%s: status = %d", where, code)
		case r.Kind == testutil.KindByID:
			if code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404 for another school's ID", where, code)
			}
			checked[r.Method+" "+r.Pattern] = true
		case r.Kind == testutil.KindFormRef:
			if code < 400 || code == http.StatusForbidden {
				t.Errorf("%s: status = %d, want 4xx rejection for another school's ID in the form", where, code)
			}
		case r.Kind == testutil.KindPage, r.Kind == testutil.KindOwn:
			// 403 untuk filter berisi ID sekolah lain adalah penolakan yang sah;
			// tanpa filter, 403 berarti Roles di daftar salah.
			if code == http.StatusForbidden && req.Path == r.Pattern {
				t.Errorf("%s: status = 403 for an allowed role; fix Roles in internal/testutil/routes.go", where)
			}
		}
	}
}

// responseText mengembalikan isi respons yang bisa diperiksa: untuk file
// .xlsx (ZIP), isi semua XML di dalamnya.
func responseText(body []byte) string {
	if !bytes.HasPrefix(body, []byte("PK\x03\x04")) {
		return string(body)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return string(body)
	}
	var sb strings.Builder
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(rc, 10<<20))
		_ = rc.Close()
		sb.Write(data)
	}
	return sb.String()
}

// schoolSnapshot mengembalikan ringkasan (md5) isi setiap tabel yang punya
// kolom school_id, untuk baris milik schoolID, ditambah peran penggunanya.
func schoolSnapshot(t *testing.T, tx pgx.Tx, schoolID string) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := tx.Query(ctx, `
		SELECT c.table_name FROM information_schema.columns c
		JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = 'public' AND c.column_name = 'school_id' AND t.table_type = 'BASE TABLE'
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) < 20 {
		t.Fatalf("only %d tables with school_id found", len(tables))
	}
	out := map[string]string{}
	for _, table := range append(tables, "user_roles") {
		q := `SELECT md5(coalesce(string_agg(x::text, '|' ORDER BY x::text), '')) FROM ` + pgx.Identifier{table}.Sanitize() + ` x WHERE school_id = $1`
		if table == "user_roles" {
			q = `SELECT md5(coalesce(string_agg(x::text, '|' ORDER BY x::text), '')) FROM user_roles x JOIN users u ON u.id = x.user_id WHERE u.school_id = $1`
		}
		var sum string
		if err := tx.QueryRow(ctx, q, schoolID).Scan(&sum); err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		out[table] = sum
	}
	return out
}

// checkOwnRoute adalah kontrol positif untuk route KindByID: request yang
// sama dengan ID sekolah sendiri harus sampai ke handler (bukan 404 atau
// 403). Tanpa ini, 404 di uji lintas sekolah bisa berasal dari path yang
// salah, bukan dari pengecekan school_id.
func checkOwnRoute(t *testing.T, c *client, r testutil.Route, u testutil.User, own *testutil.School) {
	t.Helper()
	roles := r.ControlRoles
	if roles == nil {
		roles = r.Roles
	}
	if r.Kind != testutil.KindByID || !slices.Contains(roles, u.Role) {
		return
	}
	for _, req := range r.Requests(own, own) {
		rec := c.do(r.Method, req.Path, req.Form)
		where := r.Method + " " + req.Path + " as " + u.Role + " (own school)"
		switch {
		case strings.Contains(rec.Body.String(), csrfFailureText):
			t.Errorf("%s: CSRF check failed", where)
		case rec.Code == http.StatusNotFound || rec.Code == http.StatusForbidden || rec.Code >= 500:
			t.Errorf("%s: status = %d; the request with the school's own IDs must reach the handler", where, rec.Code)
		}
	}
}
