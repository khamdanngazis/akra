package render

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/justinas/nosurf"

	"akra/internal/platform/csrf"
)

// Salinan template dipakai ulang antar-request (T152): token CSRF tiap
// response harus milik request itu sendiri walau dirender bersamaan.
func TestRenderPoolKeepsRequestFuncsApart(t *testing.T) {
	rd, err := New(goodFS(), nil, discard)
	if err != nil {
		t.Fatal(err)
	}
	h := csrf.Middleware(false, discard)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rd.Render(w, r, http.StatusOK, "auth/login", nil)
	}))
	field := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	var wg sync.WaitGroup
	errs := make(chan string, 400)
	for i := 0; i < 400; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
			m := field.FindStringSubmatch(rec.Body.String())
			var cookie string
			for _, c := range rec.Result().Cookies() {
				if c.Name == csrf.CookieName {
					cookie = c.Value
				}
			}
			if m == nil || cookie == "" || !nosurf.VerifyToken(cookie, m[1]) {
				errs <- rec.Body.String()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("token does not belong to its own response: %s", e)
	}
}
