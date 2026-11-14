package admission

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"akra/internal/platform/pii"
	"akra/internal/students"
)

func TestNISFormat(t *testing.T) {
	for _, ok := range []string{"{yyyy}{seq:3}", "{yy}.{seq:4}", "SD-{yyyy}/{seq:2}", "{seq:6}"} {
		if err := ValidateNISFormat(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "{yyyy}", "{seq:3}{seq:3}", "{seq:7}", "{yyyy} {seq:3}", "{bulan}{seq:3}", strings.Repeat("A", 30) + "{seq:3}"} {
		if err := ValidateNISFormat(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if got := RenderNIS("{yyyy}{seq:3}", 2027, 7); got != "2027007" {
		t.Errorf("RenderNIS = %q", got)
	}
	if got := RenderNIS("SD-{yy}/{seq:4}", 2027, 12); got != "SD-27/0012" {
		t.Errorf("RenderNIS = %q", got)
	}
}

func (e *env) accepted(t *testing.T, apps *ApplicantService, p Period, tr Track, guardians map[string]string) Applicant {
	t.Helper()
	ctx := context.Background()
	a := e.submitted(t, apps, p, tr)
	if guardians != nil {
		if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantFix, "ubah orang tua"); err != nil {
			t.Fatal(err)
		}
		if _, err := apps.SaveStep(ctx, e.schoolA, a.ID, StepParents, guardians); err != nil {
			t.Fatal(err)
		}
		if _, err := apps.Submit(ctx, e.schoolA, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, to := range []string{ApplicantVerified, ApplicantAccepted} {
		if _, err := apps.Transition(ctx, e.actor, a.ID, to, ""); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := apps.Load(ctx, e.schoolA, a.ID)
	return got
}

func TestEnroll(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	keys := pii.DevKeyring()
	apps := NewApplicantService(e.tx, keys, e.svc)
	enr := NewEnroller(e.tx, keys, apps)
	var classID string
	if err := e.tx.QueryRow(ctx, `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '7A', 7) RETURNING id`, e.schoolA, e.yearA).Scan(&classID); err != nil {
		t.Fatal(err)
	}
	parents := map[string]string{"ibu.full_name": "Sari", "ibu.nik": motherNIK, "ibu.phone": "081299990000", "ibu.occupation": "07",
		"ayah.full_name": "Joko", "ayah.phone": "081288880000"}
	a := e.accepted(t, apps, p, tr, parents)
	sib := e.accepted(t, apps, p, tr, parents) // adik dengan orang tua sama

	// Belum diterima: ditolak.
	pending := e.submitted(t, apps, p, tr)
	if _, _, _, err := enr.Enroll(ctx, e.actor, pending.ID, EnrollOptions{}); !errors.Is(err, ErrNotAccepted) {
		t.Errorf("enroll pending: %v", err)
	}

	sid, nis, _, err := enr.Enroll(ctx, e.actor, a.ID, EnrollOptions{ClassID: classID})
	if err != nil {
		t.Fatal(err)
	}
	if nis != "2027001" {
		t.Errorf("auto NIS = %q", nis)
	}
	st, err := students.GetByID(ctx, e.tx, e.schoolA, sid)
	if err != nil || st.FullName != a.FullName || st.Gender != "L" || st.BirthDate == nil {
		t.Errorf("student = %+v, %v", st, err)
	}
	prof, err := students.NewProfileService(e.tx, keys).Get(ctx, e.schoolA, sid)
	if err != nil || prof.Secret("nik").Reveal() != a.Values["nik"] || prof.Get("religion") != "01" || prof.Get("entry_date") != "2027-07-12" || prof.Get("registration_type") != "01" {
		t.Errorf("profile nik=%q religion=%q entry=%q, %v", prof.Secret("nik").Reveal(), prof.Get("religion"), prof.Get("entry_date"), err)
	}
	gs, _ := students.NewGuardianService(e.tx).ListByStudent(ctx, e.schoolA, sid)
	if len(gs) != 2 || gs[0].FullName != "Sari" || !gs[0].IsPrimary || gs[0].Relationship != "ibu" {
		t.Errorf("guardians = %+v", gs)
	}
	hist, _ := students.ListEnrollments(ctx, e.tx, e.schoolA, sid)
	if len(hist) != 1 || hist[0].ClassName != "7A" {
		t.Errorf("enrollments = %+v", hist)
	}
	r, _ := students.GetReadiness(ctx, e.tx, e.schoolA, sid)
	if !r.Complete() {
		t.Errorf("readiness after enroll = %+v", r.Missing)
	}

	// Dijalankan ulang: ditolak, tidak membuat siswa ganda.
	if _, _, _, err := enr.Enroll(ctx, e.actor, a.ID, EnrollOptions{}); !errors.Is(err, ErrNotAccepted) {
		t.Errorf("enroll twice: %v", err)
	}
	// Status pendaftar terkunci setelah menjadi siswa.
	if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantWithdrawn, ""); !errors.Is(err, ErrBadTransition) {
		t.Errorf("withdraw enrolled: %v", err)
	}

	// Adik: NIS manual, ibu berbagi data wali (NIK sama), ayah lewat nama + HP.
	sid2, nis2, _, err := enr.Enroll(ctx, e.actor, sib.ID, EnrollOptions{NIS: "9999"})
	if err != nil || nis2 != "9999" {
		t.Fatalf("enroll sibling: %v %q", err, nis2)
	}
	gs2, _ := students.NewGuardianService(e.tx).ListByStudent(ctx, e.schoolA, sid2)
	ids := map[string]bool{}
	for _, g := range gs {
		ids[g.ID] = true
	}
	shared := 0
	for _, g := range gs2 {
		if ids[g.ID] {
			shared++
		}
	}
	if shared != 2 {
		t.Errorf("siblings share %d guardians, want 2", shared)
	}

	// NIS manual yang sudah dipakai: gagal hanya untuk pendaftar itu.
	c := e.accepted(t, apps, p, tr, nil)
	d := e.accepted(t, apps, p, tr, nil)
	res := enr.EnrollMany(ctx, e.actor, map[string]EnrollOptions{c.ID: {NIS: "9999"}, d.ID: {}})
	okCount := 0
	for _, x := range res {
		if x.Err == nil {
			okCount++
			if x.NIS != "2027002" {
				t.Errorf("next auto NIS = %q", x.NIS)
			}
		} else {
			var ve *ValidationError
			if !errors.As(x.Err, &ve) || ve.Fields["nis"] == "" || x.ApplicantID != c.ID {
				t.Errorf("duplicate NIS error = %v", x.Err)
			}
		}
	}
	if okCount != 1 {
		t.Errorf("EnrollMany ok = %d", okCount)
	}

	// Format NIS sekolah dan nomor yang sudah dipakai dilewati.
	if err := SetNISFormat(ctx, e.tx, e.actor, "A{yy}{seq:2}"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO students (school_id, nis, full_name) VALUES ($1, 'A2701', 'Sudah ada')`, e.schoolA); err != nil {
		t.Fatal(err)
	}
	_, nis3, _, err := enr.Enroll(ctx, e.actor, c.ID, EnrollOptions{})
	if err != nil || nis3 != "A2702" {
		t.Errorf("custom format NIS = %q, %v", nis3, err)
	}
}

func TestEnrollHandler(t *testing.T) {
	e := newEnv(t)
	p, tr := e.openPeriod(t)
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	a := e.accepted(t, apps, p, tr, nil)
	body := e.do(http.MethodGet, "/pendaftaran/terima", "admin_tu", nil).Body.String()
	if !strings.Contains(body, a.FullName) || !strings.Contains(body, "2027001") {
		t.Errorf("enroll page content")
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/format-nis", "admin_tu", url.Values{"nis_format": {"{yyyy}"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid format: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/terima", "admin_tu", url.Values{}); rec.Code != http.StatusBadRequest {
		t.Errorf("no picks: %d", rec.Code)
	}
	rec := e.do(http.MethodPost, "/pendaftaran/terima", "admin_tu", url.Values{"pick": {a.ID}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "1 dari 1 berhasil") {
		t.Errorf("enroll: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/terima", "admin_tu", url.Values{"pick": {a.ID}}); rec.Code != http.StatusBadRequest {
		t.Errorf("enroll processed applicant again: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/terima", "kepala_sekolah", url.Values{"pick": {a.ID}}); rec.Code != http.StatusForbidden {
		t.Errorf("principal enroll: %d", rec.Code)
	}
}
