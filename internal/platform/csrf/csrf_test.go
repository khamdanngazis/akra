package csrf

import (
	"bytes"
	"html"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddleware_SecureCookie(t *testing.T) {
	for _, secure := range []bool{false, true} {
		h := Middleware(secure, slog.New(slog.NewTextHandler(io.Discard, nil)))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		var c *http.Cookie
		for _, rc := range rec.Result().Cookies() {
			if rc.Name == CookieName {
				c = rc
			}
		}
		if c == nil {
			t.Fatalf("secure=%v: no %s cookie", secure, CookieName)
		}
		if c.Secure != secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Errorf("secure=%v: cookie = %+v", secure, c)
		}
	}
}

func TestFuncMap_RendersToken(t *testing.T) {
	var field template.HTML
	h := Middleware(false, slog.New(slog.NewTextHandler(io.Discard, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.New("f").Funcs(PlaceholderFuncs).Parse(`{{ csrfField }}|{{ csrfToken }}`))
		var buf bytes.Buffer
		if err := template.Must(tmpl.Clone()).Funcs(FuncMap(r)).Execute(&buf, nil); err != nil {
			t.Fatal(err)
		}
		field = template.HTML(buf.String())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	got := string(field)
	if !strings.HasPrefix(got, `<input type="hidden" name="csrf_token" value="`) {
		t.Fatalf("csrfField = %q", got)
	}
	_, token, _ := strings.Cut(got, "|")
	// html/template menulis "+" sebagai "&#43;"; bandingkan setelah di-unescape.
	token = html.UnescapeString(token)
	if token == "" || !strings.Contains(got, `value="`+token+`"`) {
		t.Errorf("csrfToken %q does not match the field %q", token, got)
	}
}

func TestPlaceholderFuncs_FailWithoutRequest(t *testing.T) {
	tmpl := template.Must(template.New("f").Funcs(PlaceholderFuncs).Parse(`{{ csrfField }}`))
	if err := tmpl.Execute(io.Discard, nil); err == nil {
		t.Error("rendering with placeholder funcs succeeded, want error")
	}
}
