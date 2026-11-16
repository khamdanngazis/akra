package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/auth"
	"akra/internal/exam"
	"akra/internal/testutil"
)

// Sesi siswa/pendaftar ditolak di semua route staf (T124). Klien uji
// mengirim semua cookie ke semua path (lebih buruk dari browser yang
// membatasi cookie peserta ke /u), jadi uji ini juga membuktikan route staf
// tidak membaca sesi peserta sama sekali.
func TestParticipant_StaffRoutesRejected(t *testing.T) {
	app, tx, a, b := newTenantApp(t)
	ctx := context.Background()
	now := time.Now()

	var nis string
	if err := tx.QueryRow(ctx, `SELECT nis FROM students WHERE id = $1`, a.StudentIDs[0]).Scan(&nis); err != nil {
		t.Fatal(err)
	}
	issuer := exam.NewIssuer()
	_, code, err := issuer.Issue(ctx, tx, a.ID, a.Admin.ID, exam.IssueRequest{
		Owner: exam.Owner{StudentID: a.StudentIDs[0]}, Start: now.Add(-10 * time.Minute), End: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_, appCode, err := issuer.Issue(ctx, tx, a.ID, a.Admin.ID, exam.IssueRequest{
		Owner: exam.Owner{ApplicantID: a.ApplicantID}, Start: now.Add(-10 * time.Minute), End: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	var regNumber string
	if err := tx.QueryRow(ctx, `SELECT registration_number FROM applicants WHERE id = $1`, a.ApplicantID).Scan(&regNumber); err != nil {
		t.Fatal(err)
	}

	for _, who := range []struct{ name, id, code string }{{"siswa", nis, code}, {"pendaftar", regNumber, appCode}} {
		t.Run(who.name, func(t *testing.T) {
			c := newClient(t, app)
			c.get("/u/" + a.Code)
			rec := c.do(http.MethodPost, "/u/"+a.Code, url.Values{"identifier": {who.id}, "code": {strings.ToLower(who.code)}})
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/u/beranda" {
				t.Fatalf("participant login = %d %s\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
			}
			cookie, ok := c.cookies[auth.ParticipantCookieName]
			if !ok || cookie.Path != auth.ParticipantCookiePath || !cookie.HttpOnly {
				t.Fatalf("participant cookie = %+v", cookie)
			}
			if _, staff := c.cookies[auth.SessionCookieName]; staff {
				t.Error("participant login set the staff session cookie")
			}
			if rec := c.get("/u/beranda"); rec.Code != http.StatusOK {
				t.Fatalf("participant home = %d", rec.Code)
			}

			checked := 0
			for _, r := range testutil.Routes {
				if r.Kind == testutil.KindPublic {
					continue
				}
				reqs := []testutil.Request{{Path: r.Pattern}}
				if r.Requests != nil {
					reqs = r.Requests(a, b)
				}
				for _, req := range reqs {
					if strings.Contains(req.Path, "{") {
						continue
					}
					var rec = c.get(req.Path)
					if r.Method == http.MethodPost {
						form := url.Values{}
						for k, v := range req.Form {
							form[k] = v
						}
						rec = c.do(http.MethodPost, req.Path, form)
					}
					loc := rec.Header().Get("Location")
					if (rec.Code != http.StatusFound && rec.Code != http.StatusSeeOther) || !strings.HasPrefix(loc, "/login") {
						t.Errorf("%s %s with %s session: %d %s, want redirect to /login", r.Method, req.Path, who.name, rec.Code, loc)
					}
					checked++
				}
			}
			if checked < 100 {
				t.Errorf("only %d staff requests checked", checked)
			}
		})
	}
}
