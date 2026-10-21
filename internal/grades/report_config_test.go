package grades

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"akra/internal/academic"
	"akra/internal/platform/settings"
)

func configForm(bands ...[3]string) ReportConfigForm {
	f := ReportConfigForm{Format: "angka", KKMDefault: "75", PrincipalName: "Hj. Siti Aminah", SignerTitle: "Kepala Sekolah"}
	for _, b := range bands {
		f.Bands = append(f.Bands, BandInput{b[0], b[1], b[2]})
	}
	return f
}

func configErrors(t *testing.T, err error) map[string]string {
	t.Helper()
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want ConfigError", err)
	}
	return ce.Fields
}

func TestValidateReportConfig(t *testing.T) {
	ok := configForm([3]string{"A", "90", "100"}, [3]string{"B", "80", "89"}, [3]string{"", "", ""}, [3]string{"C", "0", "79"})
	c, err := ValidateReportConfig(ok)
	if err != nil || len(c.Scale) != 3 || c.KKMDefault != 75 || c.SignerTitle != "Kepala Sekolah" {
		t.Fatalf("valid form: %v %+v", err, c)
	}
	for _, tc := range []struct {
		name  string
		f     ReportConfigForm
		field string
		msg   string
	}{
		{"gap", configForm([3]string{"A", "90", "100"}, [3]string{"B", "0", "85"}), "scale", "celah"},
		{"overlap", configForm([3]string{"A", "85", "100"}, [3]string{"B", "0", "90"}), "scale", "tumpang tindih"},
		{"not from 0", configForm([3]string{"A", "50", "100"}), "scale", "0 sampai 100"},
		{"not to 100", configForm([3]string{"A", "0", "99"}), "scale", "0 sampai 100"},
		{"no rows", configForm(), "scale", "0 sampai 100"},
		{"duplicate letter", configForm([3]string{"A", "50", "100"}, [3]string{"A", "0", "49"}), "scale", "tidak boleh ganda"},
		{"incomplete row", configForm([3]string{"A", "", "100"}, [3]string{"B", "0", "89"}), "band_0", "Isi huruf"},
		{"letter only", configForm([3]string{"A", "90", "100"}, [3]string{"B", "x", "89"}), "band_1", "bilangan bulat"},
	} {
		_, err := ValidateReportConfig(tc.f)
		if got := configErrors(t, err)[tc.field]; !strings.Contains(got, tc.msg) {
			t.Errorf("%s: %s = %q", tc.name, tc.field, got)
		}
	}
	for _, mut := range []struct {
		field string
		fn    func(*ReportConfigForm)
	}{
		{"kkm_default", func(f *ReportConfigForm) { f.KKMDefault = "101" }},
		{"kkm_default", func(f *ReportConfigForm) { f.KKMDefault = "tujuh puluh" }},
		{"format", func(f *ReportConfigForm) { f.Format = "pdf" }},
		{"signer_title", func(f *ReportConfigForm) { f.SignerTitle = "  " }},
		{"principal_name", func(f *ReportConfigForm) { f.PrincipalName = strings.Repeat("x", 81) }},
	} {
		f := ok
		mut.fn(&f)
		if _, err := ValidateReportConfig(f); configErrors(t, err)[mut.field] == "" {
			t.Errorf("%s not rejected", mut.field)
		}
	}
}

// TestReportConfig_AffectsRecap: skala dan KKM baru langsung dipakai rekap
// T72, tanpa restart (dibaca ulang setiap kali).
func TestReportConfig_AffectsRecap(t *testing.T) {
	f := newRecapFixture(t)
	ctx := context.Background()
	f.grade(t, f.ipaComps[0], map[string]string{f.enrollments[0]: "80", f.enrollments[1]: "90"})
	f.grade(t, f.ipaComps[1], map[string]string{f.enrollments[0]: "90", f.enrollments[1]: "90"})
	f.grade(t, f.ipaComps[2], map[string]string{f.enrollments[0]: "70", f.enrollments[1]: "90"})
	row := func(rep SubjectReport, enrollment string) StudentGrades {
		for _, r := range rep.Rows {
			if r.EnrollmentID == enrollment {
				return r
			}
		}
		t.Fatalf("row %s missing", enrollment)
		return StudentGrades{}
	}
	recap := func() SubjectReport {
		rep, err := SubjectSummary(ctx, f.tx, f.schoolA, f.class(t), f.subject(t, f.ipa))
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	rep := recap()
	// Default: skala A/B/C/D, KKM 70, sebutan KKM. 79 = C, tidak di bawah KKM.
	if r := row(rep, f.enrollments[0]); r.Result.Score != 79 || r.Letter != "C" || r.BelowKKM || rep.KKM != academic.DefaultKKM || rep.KKMLabel != "KKM" {
		t.Fatalf("defaults: %+v %s below=%v kkm=%d %s", r.Result, r.Letter, r.BelowKKM, rep.KKM, rep.KKMLabel)
	}

	svc := NewReportConfigService(f.tx)
	form := configForm([3]string{"Sangat Baik", "75", "100"}, [3]string{"Baik", "60", "74"}, [3]string{"Perlu Bimbingan", "0", "59"})
	form.KKMDefault, form.Format = "80", "merdeka"
	changed, err := svc.Save(ctx, f.actor, form)
	if err != nil || !changed {
		t.Fatalf("save: %v %v", changed, err)
	}
	rep = recap()
	if r := row(rep, f.enrollments[0]); r.Letter != "Sangat Baik" || !r.BelowKKM || rep.KKM != 80 || rep.KKMLabel != "KKTP" || rep.BelowKKM != 1 {
		t.Errorf("after save: letter %q below %v kkm %d label %s count %d", r.Letter, r.BelowKKM, rep.KKM, rep.KKMLabel, rep.BelowKKM)
	}
	// KKM mapel sendiri mengalahkan default.
	if _, err := f.tx.Exec(ctx, `UPDATE subjects SET kkm = 78 WHERE id = $1`, f.ipa); err != nil {
		t.Fatal(err)
	}
	if rep = recap(); rep.KKM != 78 || row(rep, f.enrollments[0]).BelowKKM {
		t.Errorf("subject KKM: %d below %v", rep.KKM, row(rep, f.enrollments[0]).BelowKKM)
	}

	// Audit dengan isi sebelum dan sesudah; simpan ulang tanpa perubahan tidak menambah audit.
	var before, after string
	if err := f.tx.QueryRow(ctx, `SELECT before::text, after::text FROM audit_logs WHERE school_id = $1 AND action = $2`, f.schoolA, ActionReportConfigUpdate).Scan(&before, &after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, `"kkm_default": 70`) || !strings.Contains(after, `"kkm_default": 80`) || !strings.Contains(after, "Sangat Baik") {
		t.Errorf("audit before %s after %s", before, after)
	}
	if changed, err := svc.Save(ctx, f.actor, form); err != nil || changed {
		t.Errorf("no-op save: %v %v", changed, err)
	}
	if n := f.audits(ActionReportConfigUpdate); n != 1 {
		t.Errorf("audits = %d", n)
	}
	cfg, err := LoadReportConfig(ctx, f.tx, f.schoolA)
	if err != nil || cfg.PrincipalName != "Hj. Siti Aminah" || cfg.Format != "merdeka" {
		t.Errorf("load: %v %+v", err, cfg)
	}
	// Sekolah lain tetap default.
	if other, err := LoadReportConfig(ctx, f.tx, f.schoolB); err != nil || other.KKMDefault != 70 || other.Scale[0].Letter != "A" {
		t.Errorf("other school: %v %+v", err, other)
	}
	// Nilai tersimpan yang rusak adalah error.
	if err := settings.Set(ctx, f.tx, f.schoolA, settings.ReportFormat, "pdf"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReportConfig(ctx, f.tx, f.schoolA); err == nil {
		t.Error("invalid stored format accepted")
	}
}

func TestReportConfigHandler(t *testing.T) {
	f := newFixture(t)
	e := newEnvFor(t, f)
	admin := f.actor.UserID
	body := e.do(http.MethodGet, "/pengaturan/rapor", admin, "admin_tu", nil).Body.String()
	if !strings.Contains(body, `name="letter_0" value="A"`) || !strings.Contains(body, `name="kkm_default" value="70"`) || !strings.Contains(body, `value="angka" checked`) {
		t.Error("default form")
	}
	form := func(bands ...[3]string) url.Values {
		v := url.Values{"format": {"angka"}, "kkm_default": {"75"}, "principal_name": {"Pak Kepala"}, "signer_title": {"Kepala Sekolah"}}
		for i, b := range bands {
			k := strconv.Itoa(i)
			v.Set("letter_"+k, b[0])
			v.Set("min_"+k, b[1])
			v.Set("max_"+k, b[2])
		}
		return v
	}
	rec := e.do(http.MethodPost, "/pengaturan/rapor", admin, "admin_tu", form([3]string{"A", "90", "100"}, [3]string{"B", "0", "80"}))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "celah") || !strings.Contains(rec.Body.String(), `name="max_1" value="80"`) {
		t.Errorf("gap: status = %d", rec.Code)
	}
	rec = e.do(http.MethodPost, "/pengaturan/rapor", admin, "admin_tu", form([3]string{"A", "85", "100"}, [3]string{"B", "0", "84"}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: status = %d", rec.Code)
	}
	if body := e.do(http.MethodGet, "/pengaturan/rapor", admin, "admin_tu", nil).Body.String(); !strings.Contains(body, `name="min_0" value="85"`) || !strings.Contains(body, "Pak Kepala") {
		t.Error("saved config not shown")
	}
	// Guru dan peran lain: 403.
	for _, role := range []string{"guru", "kepala_sekolah", "bendahara", "orang_tua"} {
		if rec := e.do(http.MethodGet, "/pengaturan/rapor", admin, role, nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET as %s: status = %d", role, rec.Code)
		}
		if rec := e.do(http.MethodPost, "/pengaturan/rapor", admin, role, form([3]string{"A", "0", "100"})); rec.Code != http.StatusForbidden {
			t.Errorf("POST as %s: status = %d", role, rec.Code)
		}
	}
	// Skala tersimpan rusak: halaman tetap terbuka dengan peringatan.
	if err := settings.Set(context.Background(), f.tx, f.schoolA, settings.ReportScale, `[{"letter":"A","min":10,"max":100}]`); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(http.MethodGet, "/pengaturan/rapor", admin, "admin_tu", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tidak valid") {
		t.Errorf("broken config page: status = %d", rec.Code)
	}
}
