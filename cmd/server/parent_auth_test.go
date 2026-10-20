package main

// Uji otorisasi orang tua (T59): orang tua hanya melihat anaknya sendiri dan
// tidak bisa membuka halaman staf. Melengkapi uji lintas sekolah (T58)
// untuk orang tua di sekolah yang sama.

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"akra/internal/testutil"
)

func loginParent(t *testing.T, app http.Handler, s *testutil.School, u testutil.User) *client {
	t.Helper()
	c := newClient(t, app)
	if rec := c.login(s.Code, u.Email, testutil.Password); rec.Code != http.StatusSeeOther {
		t.Fatalf("login %s: status = %d", u.Email, rec.Code)
	}
	c.get("/portal/")
	return c
}

func TestParentAuth_Ownership(t *testing.T) {
	app, _, a, _ := newTenantApp(t)
	p1 := loginParent(t, app, a, a.Parent)
	p2 := loginParent(t, app, a, a.Parent2)

	own := map[string]string{
		"detail":   "/portal/siswa/" + a.StudentIDs[0],
		"tagihan":  "/portal/siswa/" + a.StudentIDs[0] + "/tagihan",
		"jadwal":   "/portal/siswa/" + a.StudentIDs[0] + "/jadwal",
		"kuitansi": "/portal/pembayaran/" + a.PaymentID + "/kuitansi",
	}
	sibling := map[string]string{
		"detail":   "/portal/siswa/" + a.StudentIDs[1],
		"tagihan":  "/portal/siswa/" + a.StudentIDs[1] + "/tagihan",
		"jadwal":   "/portal/siswa/" + a.StudentIDs[1] + "/jadwal",
		"kuitansi": "/portal/pembayaran/" + a.Payment2ID + "/kuitansi",
	}
	for name, path := range own {
		if rec := p1.get(path); rec.Code != http.StatusOK {
			t.Errorf("parent 1, own child %s: status = %d", name, rec.Code)
		}
	}
	for name, path := range sibling {
		rec := p1.get(path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("parent 1, other child %s: status = %d, want 404", name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "Siswa "+a.Marker+" 2") {
			t.Errorf("parent 1, other child %s: response shows the other child", name)
		}
		// Orang tua 2 bisa membuka data anaknya sendiri (kontrol positif).
		if rec := p2.get(path); rec.Code != http.StatusOK {
			t.Errorf("parent 2, own child %s: status = %d", name, rec.Code)
		}
	}
	if rec := p2.get(own["detail"]); rec.Code != http.StatusNotFound {
		t.Errorf("parent 2, other child detail: status = %d, want 404", rec.Code)
	}

	// Pengumuman kelas anak Y tidak muncul untuk orang tua 1; pengumuman
	// umum muncul untuk keduanya.
	ann1 := p1.get("/portal/pengumuman").Body.String()
	ann2 := p2.get("/portal/pengumuman").Body.String()
	if strings.Contains(ann1, a.Class2AnnouncementTitle) {
		t.Error("parent 1 sees the announcement for the other child's class")
	}
	if !strings.Contains(ann2, a.Class2AnnouncementTitle) {
		t.Error("parent 2 does not see the announcement for their child's class")
	}
	if !strings.Contains(ann1, "Pengumuman "+a.Marker) || !strings.Contains(ann2, "Pengumuman "+a.Marker) {
		t.Error("school-wide announcement is missing")
	}
	// Jadwal anak X (kelas ClassID) berisi slot fixture; jadwal anak Y
	// (kelas lain) tidak.
	if !strings.Contains(p1.get(own["jadwal"]).Body.String(), "Mapel "+a.Marker) {
		t.Error("parent 1 does not see the slot in their child's schedule")
	}
	if strings.Contains(p2.get(sibling["jadwal"]).Body.String(), "Mapel "+a.Marker) {
		t.Error("parent 2 sees a slot from another class")
	}
	// Daftar anak hanya berisi anak sendiri.
	list := p1.get("/portal/").Body.String()
	if !strings.Contains(list, "Siswa "+a.Marker+" 1") || strings.Contains(list, "Siswa "+a.Marker+" 2") {
		t.Error("parent 1 children list")
	}
}

// Orang tua mendapat 403 di setiap route staf, dan 404 untuk path portal
// yang tidak ada.
func TestParentAuth_StaffRoutesForbidden(t *testing.T) {
	app, _, a, _ := newTenantApp(t)
	p1 := loginParent(t, app, a, a.Parent)
	prefixes := []string{"/siswa", "/keuangan", "/absensi", "/laporan", "/impor", "/pengaturan", "/kelas", "/pengumuman"}
	covered := map[string]bool{}
	for _, r := range testutil.Routes {
		if r.Kind == testutil.KindPublic || slices.Contains(r.Roles, testutil.Parent) {
			continue
		}
		reqs := []testutil.Request{{Path: r.Pattern}}
		if r.Requests != nil {
			// ID milik sekolah orang tua itu sendiri: penolakan harus karena
			// peran, bukan karena data tidak ditemukan.
			reqs = r.Requests(a, a)
		}
		for _, req := range reqs {
			rec := p1.do(r.Method, req.Path, req.Form)
			if strings.Contains(rec.Body.String(), csrfFailureText) {
				t.Errorf("%s %s: CSRF check failed", r.Method, req.Path)
				continue
			}
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s as parent: status = %d, want 403", r.Method, req.Path, rec.Code)
			}
		}
		for _, p := range prefixes {
			if strings.HasPrefix(r.Pattern, p) {
				covered[p] = true
			}
		}
	}
	for _, p := range prefixes {
		if !covered[p] {
			t.Errorf("no staff route under %s was checked", p)
		}
	}
	// Beranda staf mengalihkan orang tua ke portal.
	if rec := p1.get("/"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/portal" && rec.Header().Get("Location") != "/portal/" {
		t.Errorf("home as parent: status = %d, location %q", rec.Code, rec.Header().Get("Location"))
	}

	for _, path := range []string{
		"/portal/tidak-ada",
		"/portal/siswa",
		"/portal/siswa/" + a.StudentIDs[0] + "/hapus",
		"/portal/siswa/" + a.StudentIDs[0] + "/absensi/rahasia",
		"/portal/pembayaran/" + a.PaymentID,
		"/portal/tagihan/" + a.InvoiceID,
	} {
		if rec := p1.get(path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s as parent: status = %d, want 404", path, rec.Code)
		}
	}
	if rec := p1.do(http.MethodPost, "/portal/siswa/"+a.StudentIDs[0], url.Values{}); rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Errorf("POST to a portal page: status = %d, want 404 or 405", rec.Code)
	}
}

func TestParentAuth_LogoutEndsSession(t *testing.T) {
	app, _, a, _ := newTenantApp(t)
	p1 := loginParent(t, app, a, a.Parent)
	if rec := p1.get("/portal/"); rec.Code != http.StatusOK {
		t.Fatalf("portal before logout: status = %d", rec.Code)
	}
	if rec := p1.do(http.MethodPost, "/logout", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("logout: status = %d", rec.Code)
	}
	for _, path := range []string{"/portal/", "/portal/siswa/" + a.StudentIDs[0], "/portal/pembayaran/" + a.PaymentID + "/kuitansi"} {
		rec := p1.get(path)
		loc := rec.Header().Get("Location")
		if (rec.Code != http.StatusFound && rec.Code != http.StatusSeeOther) || !strings.HasPrefix(loc, "/login") {
			t.Errorf("GET %s after logout: status = %d, location %q, want redirect to /login", path, rec.Code, loc)
		}
	}
}

// tokenPattern menghapus nilai yang berbeda di setiap respons (token CSRF
// bertopeng di form dan meta) sebelum dua respons dibandingkan.
var tokenPattern = regexp.MustCompile(`(name="csrf_token" value="|<meta name="csrf-token" content=")[^"]*(")`)

// Orang tua tidak bisa membedakan "ada tetapi bukan milik saya" dari "tidak
// ada": status dan isi 404 identik.
func TestParentAuth_NoEnumeration(t *testing.T) {
	app, _, a, b := newTenantApp(t)
	p1 := loginParent(t, app, a, a.Parent)
	const random = "7d4f3c2a-1b2e-4c6d-9e8f-0a1b2c3d4e5f"
	for _, tc := range []struct{ name, notMine, otherSchool, missing string }{
		{"detail", "/portal/siswa/" + a.StudentIDs[1], "/portal/siswa/" + b.StudentIDs[0], "/portal/siswa/" + random},
		{"tagihan", "/portal/siswa/" + a.StudentIDs[1] + "/tagihan", "/portal/siswa/" + b.StudentIDs[0] + "/tagihan", "/portal/siswa/" + random + "/tagihan"},
		{"jadwal", "/portal/siswa/" + a.StudentIDs[1] + "/jadwal", "/portal/siswa/" + b.StudentIDs[0] + "/jadwal", "/portal/siswa/" + random + "/jadwal"},
		{"kuitansi", "/portal/pembayaran/" + a.Payment2ID + "/kuitansi", "/portal/pembayaran/" + b.PaymentID + "/kuitansi", "/portal/pembayaran/" + random + "/kuitansi"},
		{"bukan uuid", "/portal/siswa/" + a.StudentIDs[1], "/portal/siswa/" + b.StudentIDs[0], "/portal/siswa/123"},
	} {
		missing := p1.get(tc.missing)
		want := tokenPattern.ReplaceAllString(missing.Body.String(), "$1$2")
		if missing.Code != http.StatusNotFound {
			t.Errorf("%s: missing id: status = %d", tc.name, missing.Code)
		}
		for label, path := range map[string]string{"not mine": tc.notMine, "other school": tc.otherSchool} {
			rec := p1.get(path)
			got := tokenPattern.ReplaceAllString(rec.Body.String(), "$1$2")
			if rec.Code != missing.Code {
				t.Errorf("%s, %s: status = %d, missing id gives %d", tc.name, label, rec.Code, missing.Code)
			}
			if got != want {
				t.Errorf("%s, %s: body differs from the missing-id response", tc.name, label)
			}
			if ct, wct := rec.Header().Get("Content-Type"), missing.Header().Get("Content-Type"); ct != wct {
				t.Errorf("%s, %s: content type %q, missing id gives %q", tc.name, label, ct, wct)
			}
		}
	}
}
