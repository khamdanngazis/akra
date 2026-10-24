package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func schoolCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == SchoolCookieName {
			return c
		}
	}
	return nil
}

// fixedSchool melaporkan apakah halaman login menampilkan sekolah yang sudah
// diketahui (bukan kolom kode).
func fixedSchool(body string) bool {
	return strings.Contains(body, `name="school_fixed"`) && strings.Contains(body, "SD Harapan") && !strings.Contains(body, `id="school_code"`)
}

func TestSchoolLoginLink(t *testing.T) {
	h := newTestRouter(t)
	for _, path := range []string{"/s/sd-harapan", "/s/SD-Harapan"} {
		rec := get(h, path, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !fixedSchool(body) || !strings.Contains(body, `name="school_code" value="sd-harapan"`) {
			t.Errorf("%s: status %d, fixed school %v", path, rec.Code, fixedSchool(body))
		}
		if !strings.Contains(body, `href="/login?ganti=1"`) {
			t.Errorf("%s: no change-school link", path)
		}
	}
	// Kode tidak dikenal atau berformat salah: form biasa dengan kolom kode
	// kosong, tanpa pesan yang membedakan sekolah ada atau tidak.
	plain := get(h, "/login", nil).Body.String()
	for _, path := range []string{"/s/sd-tidak-ada", "/s/x", "/s/bukan%20kode", "/s/" + strings.Repeat("a", 40)} {
		rec := get(h, path, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || fixedSchool(body) || !strings.Contains(body, `id="school_code" name="school_code" value=""`) {
			t.Errorf("%s: status %d", path, rec.Code)
		}
		if strings.Count(body, "<input") != strings.Count(plain, "<input") || strings.Contains(strings.ToLower(body), "tidak ditemukan") {
			t.Errorf("%s: page differs from the plain login page", path)
		}
	}
	// next ikut dibawa.
	if body := get(h, "/s/sd-harapan?next=/absensi", nil).Body.String(); !strings.Contains(body, `name="next" value="/absensi"`) || !strings.Contains(body, `href="/login?ganti=1&amp;next=%2fabsensi"`) {
		t.Error("next not carried on school link")
	}
}

func TestSchoolLogin_RememberedAfterLoginAndLogout(t *testing.T) {
	h, _ := newTestRouterWithSessions(t)
	// Login dari link sekolah dengan kode berhuruf besar: cookie berisi kode
	// yang dinormalisasi.
	rec := postLogin(h, " SD-Harapan ", "budi@sekolah.id", "benar-123")
	sc := schoolCookie(rec)
	if rec.Code != http.StatusSeeOther || sc == nil || sc.Value != "sd-harapan" || !sc.HttpOnly || sc.MaxAge < 300*24*3600 || sc.SameSite != http.SameSiteLaxMode {
		t.Fatalf("school cookie after login = %+v", sc)
	}

	// Logout kembali ke link sekolah dan tidak menghapus cookie sekolah.
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(sessionCookie(t, rec))
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if loc := out.Header().Get("Location"); out.Code != http.StatusSeeOther || loc != "/s/sd-harapan" {
		t.Fatalf("logout: %d %q", out.Code, loc)
	}
	if c := schoolCookie(out); c != nil {
		t.Errorf("logout touched the school cookie: %+v", c)
	}
	// Halaman tujuan logout menampilkan sekolah dan pesan keluar.
	page := get(h, "/s/sd-harapan", sessionCookie(t, out)).Body.String()
	if !fixedSchool(page) || !strings.Contains(page, msgLoggedOut) {
		t.Error("school login page after logout")
	}

	// /login biasa memakai cookie sekolah terakhir.
	if body := get(h, "/login", sc).Body.String(); !fixedSchool(body) {
		t.Error("/login does not use the remembered school")
	}
	// Ganti sekolah: cookie dihapus dan kolom kode kosong.
	rec = get(h, "/login?ganti=1", sc)
	if c := schoolCookie(rec); c == nil || c.MaxAge >= 0 {
		t.Errorf("change school did not clear the cookie: %+v", c)
	}
	if fixedSchool(rec.Body.String()) {
		t.Error("change school still shows the school")
	}
	// Cookie berisi kode tidak dikenal atau rusak: form biasa.
	for _, v := range []string{"sd-tidak-ada", "<script>"} {
		if body := get(h, "/login", &http.Cookie{Name: SchoolCookieName, Value: v}).Body.String(); fixedSchool(body) || !strings.Contains(body, `id="school_code"`) {
			t.Errorf("cookie %q: unexpected page", v)
		}
	}
}

func TestSchoolLogin_FailedLoginKeepsSchool(t *testing.T) {
	h := newTestRouter(t)
	form := map[string][]string{"school_code": {"sd-harapan"}, "school_fixed": {"1"}, "email": {"budi@sekolah.id"}, "password": {"salah"}}
	rec := postLoginForm(h, form)
	if rec.Code != http.StatusUnauthorized || !fixedSchool(rec.Body.String()) || alertText(t, rec.Body.String()) != msgInvalidCredentials {
		t.Errorf("failed login from school link: %d", rec.Code)
	}
	if schoolCookie(rec) != nil {
		t.Error("failed login set the school cookie")
	}
	// Dari form biasa: kolom kode tetap kolom isian.
	if rec := postLogin(h, "sd-harapan", "budi@sekolah.id", "salah"); fixedSchool(rec.Body.String()) {
		t.Error("plain form turned into a fixed school after a failed login")
	}
}

func TestLogout_OldSessionWithoutSchoolCode(t *testing.T) {
	h, _ := newTestRouterWithSessions(t)
	// Sesi anonim (tanpa kode sekolah) tetap kembali ke /login.
	c := sessionCookie(t, get(h, "/test/anon", nil))
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q", loc)
	}
}

func TestValidSchoolCode(t *testing.T) {
	for code, want := range map[string]bool{"sd-harapan": true, "abc": true, "ab": false, "sd--harapan": false, "-sd": false, "SD": false, "sd_harapan": false, strings.Repeat("a", 32): true, strings.Repeat("a", 33): false} {
		if got := ValidSchoolCode(code); got != want {
			t.Errorf("ValidSchoolCode(%q) = %v", code, got)
		}
	}
}
