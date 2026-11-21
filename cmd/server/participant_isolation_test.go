package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/exam"
	"akra/internal/testutil"
)

// Sesi peserta ujian (siswa/pendaftar) tidak pernah membuka route staf atau
// portal, dan sesi staf tidak membuka halaman peserta (T153).
func TestParticipantSessionCannotUseStaffRoutes(t *testing.T) {
	app, tx, a, _ := newTenantApp(t)
	ctx := context.Background()
	for _, owner := range []exam.Owner{{StudentID: a.StudentIDs[0]}, {ApplicantID: a.ApplicantID}} {
		var ident string
		if owner.StudentID != "" {
			tx.QueryRow(ctx, `SELECT nis FROM students WHERE id = $1`, owner.StudentID).Scan(&ident)
		} else {
			tx.QueryRow(ctx, `SELECT registration_number FROM applicants WHERE id = $1`, owner.ApplicantID).Scan(&ident)
		}
		_, code, err := exam.NewIssuer().Issue(ctx, tx, a.ID, a.Admin.ID, exam.IssueRequest{Owner: owner,
			Start: time.Now().Add(-5 * time.Minute), End: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		c := newClient(t, app)
		c.get("/u/" + a.Code + "?nis=" + url.QueryEscape(ident))
		if rec := c.do(http.MethodPost, "/u/"+a.Code, url.Values{"identifier": {ident}, "code": {code}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("participant login %s = %d", ident, rec.Code)
		}
		rec := c.get("/u/beranda")
		if rec.Code != http.StatusOK {
			t.Fatalf("participant home = %d", rec.Code)
		}
		// Halaman ujian tidak boleh dibingkai situs lain (clickjacking).
		if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("participant home lacks security headers: %v", rec.Header())
		}
		checked := 0
		for _, r := range testutil.Routes {
			if r.Kind == testutil.KindPublic {
				continue
			}
			reqs := []testutil.Request{{Path: r.Pattern}}
			if r.Requests != nil {
				reqs = r.Requests(a, a)
			}
			for _, req := range reqs {
				var form url.Values
				if r.Method != http.MethodGet {
					form = url.Values{}
					for k, v := range req.Form {
						form[k] = v
					}
				}
				rec := c.do(r.Method, req.Path, form)
				checked++
				// Staf: dialihkan ke login staf; selain itu ditolak 4xx.
				ok := (rec.Code == http.StatusSeeOther || rec.Code == http.StatusFound) && strings.HasPrefix(rec.Header().Get("Location"), "/login") ||
					rec.Code >= 400 && rec.Code < 500
				if !ok {
					t.Errorf("participant %s: %s %s = %d %s", ident, r.Method, req.Path, rec.Code, rec.Header().Get("Location"))
				}
			}
		}
		if checked < 300 {
			t.Errorf("only %d staff requests checked", checked)
		}
	}
	// Sesi staf (admin TU) tidak membuka beranda peserta.
	s := newClient(t, app)
	if rec := s.login(a.Code, a.Admin.Email, testutil.Password); rec.Code != http.StatusSeeOther {
		t.Fatalf("staff login = %d", rec.Code)
	}
	for _, p := range []string{"/u/beranda", "/u/ujian/" + a.ExamSessionID, "/u/a/00000000-0000-0000-0000-000000000000", "/u/hasil/00000000-0000-0000-0000-000000000000"} {
		if rec := s.get(p); rec.Code == http.StatusOK {
			t.Errorf("staff session opened participant page %s", p)
		}
	}
}
