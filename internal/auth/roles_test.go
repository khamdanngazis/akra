package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"akra/internal/platform/database"
)

// serveWithIdentity menjalankan RequireRole(roles...) dengan Identity yang
// sudah ada di context, seperti setelah RequireLogin.
func serveWithIdentity(id *Identity, roles ...string) *httptest.ResponseRecorder {
	h := RequireRole(roles...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/tagihan", nil)
	if id != nil {
		req = req.WithContext(withIdentity(req.Context(), *id))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRequireRole(t *testing.T) {
	cases := []struct {
		name       string
		userRoles  []string
		allowed    []string
		wantStatus int
	}{
		{"guru denied bendahara route", []string{RoleGuru}, []string{RoleBendahara}, http.StatusForbidden},
		{"bendahara allowed", []string{RoleBendahara}, []string{RoleBendahara}, http.StatusOK},
		{"guru and bendahara allowed", []string{RoleGuru, RoleBendahara}, []string{RoleBendahara}, http.StatusOK},
		{"any of several allowed roles", []string{RoleKepalaSekolah}, []string{RoleBendahara, RoleKepalaSekolah}, http.StatusOK},
		{"no roles denied", nil, []string{RoleBendahara}, http.StatusForbidden},
		{"orang tua denied staff route", []string{RoleOrangTua}, []string{RoleAdminTU, RoleBendahara, RoleGuru, RoleKepalaSekolah}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveWithIdentity(&Identity{UserID: "u1", SchoolID: "s1", Roles: tc.userRoles}, tc.allowed...)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusForbidden && !strings.Contains(rec.Body.String(), "Akses ditolak") {
				t.Error("403 response is not the access denied page")
			}
		})
	}
}

func TestRequireRole_NotLoggedIn(t *testing.T) {
	rec := serveWithIdentity(nil, RoleBendahara)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?next=%2Ftagihan" {
		t.Errorf("status = %d, Location = %q; want 302 to /login?next=%%2Ftagihan", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRequireRole_PanicsWithoutRoles(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("RequireRole() without roles did not panic")
		}
	}()
	RequireRole()
}

func TestHasAnyRole(t *testing.T) {
	id := Identity{Roles: []string{RoleGuru, RoleBendahara}}
	if !HasAnyRole(id, RoleAdminTU, RoleBendahara) {
		t.Error("HasAnyRole(guru+bendahara, admin_tu, bendahara) = false, want true")
	}
	if HasAnyRole(id, RoleAdminTU, RoleKepalaSekolah) {
		t.Error("HasAnyRole(guru+bendahara, admin_tu, kepala_sekolah) = true, want false")
	}
	if HasAnyRole(id) {
		t.Error("HasAnyRole(id) with no roles = true, want false")
	}
}

// TestRoleCodesMatchDatabase memastikan konstanta peran sama dengan isi
// tabel roles. Butuh TEST_DATABASE_URL.
func TestRoleCodesMatchDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := database.Connect(context.Background(), dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	rows, err := pool.Query(context.Background(), `SELECT code FROM roles ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		codes = append(codes, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := slices.Clone(AllRoles)
	slices.Sort(want)
	slices.Sort(codes)
	if !slices.Equal(codes, want) {
		t.Errorf("roles in database = %v, constants = %v", codes, want)
	}
}
