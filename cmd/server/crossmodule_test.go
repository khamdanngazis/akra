package main

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"akra/internal/platform/module"
	"akra/internal/testutil"
)

// Fitur lintas modul dan peran (T166, ADR 0002 bagian 4).
func TestCrossModuleFeatures(t *testing.T) {
	app, tx, a, _ := newTenantApp(t)
	ctx := context.Background()
	t.Cleanup(func() { module.Forget(a.ID) })
	set := func(m module.ID, on bool) {
		t.Helper()
		saveModule(t, tx, a.ID, m, module.State{Enabled: on, Options: map[string]string{"release": "r3"}})
	}
	login := func(u testutil.User) *client {
		cl := newClient(t, app)
		if rec := cl.login(a.Code, u.Email, testutil.Password); rec.Code != http.StatusSeeOther {
			t.Fatalf("login %s: %d", u.Role, rec.Code)
		}
		cl.get("/")
		return cl
	}
	admin, teacher, treasurer := login(a.Admin), login(a.Teacher), login(a.Treasurer)

	t.Run("form fee without billing is recorded by admin with sequential receipts", func(t *testing.T) {
		var ids [2]string
		for i := range ids {
			if err := tx.QueryRow(ctx, `INSERT INTO applicants (school_id, period_id, track_id, registration_number, status, full_name, gender, birth_date,
				contact_phone, consent_at, submitted_at, fee_amount) VALUES ($1, $2, $3, $4, 'dikirim', $5, 'P', '2019-05-17', '+6281200000010', now(), now(), 100000) RETURNING id`,
				a.ID, a.AdmissionPeriodID, a.AdmissionTrackID, "SPMB-T166-"+string(rune('A'+i)), "Calon "+string(rune('A'+i))).Scan(&ids[i]); err != nil {
				t.Fatal(err)
			}
		}
		pay := func(cl *client, id string) *http.Response {
			return cl.do(http.MethodPost, "/pendaftaran/"+id+"/biaya", url.Values{"paid_on": {"2026-11-20"}, "method": {"tunai"}}).Result()
		}
		// Keuangan aktif: admin TU tidak mencatat (bendahara).
		if res := pay(admin, ids[0]); res.StatusCode != http.StatusForbidden {
			t.Errorf("admin with billing = %d, want 403", res.StatusCode)
		}
		set(module.Billing, false)
		defer set(module.Billing, true)
		if body := admin.get("/pendaftaran/" + ids[0]).Body.String(); !strings.Contains(body, "admin TU yang mencatat pembayaran") {
			t.Error("applicant page lacks the admin fee hint")
		}
		if body := admin.get("/pendaftaran/biaya").Body.String(); !strings.Contains(body, "Catat pembayaran") {
			t.Error("fee page lacks the record button for admin")
		}
		var receipts []string
		for _, id := range ids {
			if res := pay(admin, id); res.StatusCode != http.StatusSeeOther {
				t.Fatalf("admin record fee = %d", res.StatusCode)
			}
			var pid, no string
			if err := tx.QueryRow(ctx, `SELECT id, receipt_number FROM applicant_fee_payments WHERE applicant_id = $1`, id).Scan(&pid, &no); err != nil {
				t.Fatal(err)
			}
			receipts = append(receipts, no)
			if rec := admin.get("/pendaftaran/" + id + "/biaya/" + pid + "/kuitansi"); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "%PDF-") {
				t.Errorf("receipt = %d", rec.Code)
			}
		}
		seq := regexp.MustCompile(`(\d+)$`)
		n1, n2 := seq.FindString(receipts[0]), seq.FindString(receipts[1])
		if len(n1) == 0 || len(n2) == 0 || atoi(n2) != atoi(n1)+1 {
			t.Errorf("receipts not sequential: %v", receipts)
		}
	})

	t.Run("online selection test needs exam", func(t *testing.T) {
		set(module.Exam, false)
		defer set(module.Exam, true)
		path := "/pendaftaran/periode/" + a.AdmissionPeriodID + "/tes"
		body := admin.get(path).Body.String()
		if strings.Contains(body, `value="online"`) || !strings.Contains(body, "Ujian online aktif bila modul Ujian Online dipakai") {
			t.Error("tests page offers online test without exam")
		}
		rec := admin.do(http.MethodPost, path, url.Values{"test_name": {"Tes online"}, "weight": {"10"}, "max_score": {"100"}, "method": {"online"}, "exam_id": {a.ID}})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Ujian online aktif bila modul Ujian Online dipakai") {
			t.Errorf("online test without exam = %d", rec.Code)
		}
		if rec := admin.do(http.MethodPost, path, url.Values{"test_name": {"Wawancara"}, "weight": {"10"}, "max_score": {"100"}, "method": {"kertas"}}); rec.Code != http.StatusSeeOther {
			t.Errorf("paper test without exam = %d", rec.Code)
		}
	})

	t.Run("exam send to report needs grades", func(t *testing.T) {
		var examID string
		if err := tx.QueryRow(ctx, `SELECT id FROM exams WHERE school_id = $1 LIMIT 1`, a.ID).Scan(&examID); err != nil {
			t.Fatal(err)
		}
		set(module.Grades, false)
		defer set(module.Grades, true)
		if rec := admin.get("/ujian/paket/" + examID + "/rapor"); rec.Code != http.StatusNotFound {
			t.Errorf("send to report without grades = %d", rec.Code)
		}
		if body := admin.get("/ujian/paket/" + examID + "/hasil").Body.String(); strings.Contains(body, "/ujian/paket/"+examID+"/rapor") {
			t.Error("results page links to send to report without grades")
		}
		// Nilai ke Dapodik juga butuh modul nilai.
		if rec := teacher.get("/dapodik/nilai"); rec.Code != http.StatusNotFound {
			t.Errorf("Dapodik grades without grades = %d", rec.Code)
		}
		if body := admin.get("/dapodik/").Body.String(); strings.Contains(body, `href="/dapodik/nilai"`) {
			t.Error("Dapodik page links to grades export without grades")
		}
	})

	t.Run("admission test purpose needs admission", func(t *testing.T) {
		set(module.Admission, false)
		defer set(module.Admission, true)
		body := admin.get("/ujian/paket/baru?tujuan=seleksi").Body.String()
		if strings.Contains(body, `value="seleksi"`) {
			t.Error("wizard offers admission test without admission")
		}
	})

	t.Run("coordinator duty needs exam", func(t *testing.T) {
		set(module.Exam, false)
		defer set(module.Exam, true)
		body := admin.get("/pengaturan/tugas").Body.String()
		if strings.Contains(body, `value="koordinator_ujian"`) || !strings.Contains(body, "Tugas koordinator ujian tersedia bila sekolah memakai modul Ujian Online") {
			t.Error("duty form offers exam coordinator without exam")
		}
		rec := admin.do(http.MethodPost, "/pengaturan/tugas", url.Values{"academic_year_id": {a.YearID}, "user_id": {a.Teacher.ID}, "duty": {"koordinator_ujian"}})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("assign coordinator without exam = %d", rec.Code)
		}
	})

	t.Run("treasurer without billing sees an explained home", func(t *testing.T) {
		set(module.Billing, false)
		defer set(module.Billing, true)
		rec := treasurer.get("/")
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "Sekolah Anda tidak memakai modul Keuangan") {
			t.Fatalf("treasurer home = %d", rec.Code)
		}
		menu := between(body, `<nav id="app-nav"`, "</nav>")
		for _, p := range hrefRe.FindAllStringSubmatch(menu, -1) {
			if p[1] != "/" && p[1] != "/pengumuman" && p[1] != "/pendaftaran/biaya" {
				t.Errorf("treasurer menu without billing has %s", p[1])
			}
		}
	})
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
