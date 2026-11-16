package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
)

func TestParticipantSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := memstore.NewWithCleanupInterval(0)
	psm := NewParticipantSessionManager(store, true, logger)
	staff := NewSessionManager(store, true, logger)

	var expires time.Time
	mux := http.NewServeMux()
	mux.HandleFunc("/u/masuk", func(w http.ResponseWriter, r *http.Request) {
		p := Participant{Kind: RoleSiswa, SchoolID: "s1", SchoolCode: "sd", StudentID: "st1", CredentialID: "c1", Name: "Ani", ExpiresAt: expires}
		if err := StartParticipantSession(r.Context(), psm, p); err != nil {
			t.Fatal(err)
		}
	})
	var got Participant
	var reached bool
	protected := RequireParticipant(psm, func(_ *http.Request, code string) string { return "/u/" + code })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, reached = CurrentParticipant(r.Context())
	}))
	mux.Handle("/u/beranda", protected)
	mux.Handle("/", RequireLogin(staff)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("staff route reached with participant session")
	})))
	app := psm.LoadAndSave(staff.LoadAndSave(mux))

	run := func(path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec
	}

	expires = time.Now().Add(time.Hour)
	rec := run("/u/masuk", nil)
	cookies := rec.Result().Cookies()
	var pc *http.Cookie
	for _, c := range cookies {
		if c.Name == ParticipantCookieName {
			pc = c
		}
		if c.Name == SessionCookieName {
			t.Error("participant login set the staff cookie")
		}
	}
	if pc == nil || pc.Path != "/u" || !pc.HttpOnly || !pc.Secure {
		t.Fatalf("participant cookie = %+v", pc)
	}
	if d := time.Until(pc.Expires); d > time.Hour+time.Minute || d < 59*time.Minute {
		t.Errorf("cookie expires in %v, want about 1h (session deadline)", d)
	}
	if run("/u/beranda", cookies); !reached || got.StudentID != "st1" || got.Kind != RoleSiswa {
		t.Errorf("participant = %+v, reached %v", got, reached)
	}
	// Cookie peserta di route staf: tetap dianggap belum login.
	if rec := run("/siswa", cookies); rec.Code != http.StatusFound {
		t.Errorf("staff route = %d, want redirect", rec.Code)
	}

	// Sesi yang sudah lewat masa berlaku ditolak walau cookie masih dikirim.
	expires = time.Now().Add(-time.Second)
	cookies = run("/u/masuk", nil).Result().Cookies()
	reached = false
	if rec := run("/u/beranda", cookies); reached || rec.Code != http.StatusSeeOther {
		t.Errorf("expired session: reached %v, code %d", reached, rec.Code)
	}
}
