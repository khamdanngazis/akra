package dapodik

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/pii"
	"akra/internal/students"
)

func fields(issues []Issue, level string) string {
	var out []string
	for _, i := range issues {
		if level == "" || i.Level == level {
			out = append(out, i.Field)
		}
	}
	return strings.Join(out, ",")
}

func TestCheck(t *testing.T) {
	today := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	birth := time.Date(2019, 5, 17, 0, 0, 0, 0, time.UTC)
	good := Input{
		Student: students.Student{FullName: "Budi Santoso", BirthDate: &birth},
		Profile: students.ProfileForTest(map[string]string{"nik": "3201010101010001", "kk_number": "3201010101010099", "nisn": "0091234567", "postal_code": "40123"}),
		Parents: map[string]Parent{"ibu": {Name: "Sari Dewi", Details: students.GuardianDetails{NIK: pii.New("x"), BirthYear: 1988}}, "ayah": {Name: "Joko"}},
	}
	if got := Check(good, 1, today); len(got) != 0 {
		t.Errorf("good data issues = %+v", got)
	}

	cases := []struct {
		name  string
		edit  func(in *Input)
		grade int
		field string
		level string
	}{
		{"nik empty", func(in *Input) {
			in.Profile = students.ProfileForTest(map[string]string{"kk_number": "3201010101010099"})
		}, 1, "nik", LevelError},
		{"nik equals kk", func(in *Input) {
			in.Profile = students.ProfileForTest(map[string]string{"nik": "3201010101010001", "kk_number": "3201010101010001"})
		}, 1, "kk_number", LevelError},
		{"nisn 9 digits", func(in *Input) {
			in.Profile = students.ProfileForTest(map[string]string{"nik": "3201010101010001", "kk_number": "3201010101010099", "nisn": "009123456"})
		}, 1, "nisn", LevelError},
		{"no mother", func(in *Input) { in.Parents = map[string]Parent{} }, 1, "mother_name", LevelError},
		{"mother title", func(in *Input) { in.Parents = map[string]Parent{"ibu": {Name: "Hj. Sari"}} }, 1, "mother_name", LevelWarning},
		{"mother almh", func(in *Input) { in.Parents = map[string]Parent{"ibu": {Name: "Almh. Sari"}} }, 1, "mother_name", LevelWarning},
		{"father degree", func(in *Input) { in.Parents["ayah"] = Parent{Name: "Joko, S.Pd"} }, 1, "father_name", LevelWarning},
		{"student digits", func(in *Input) { in.Student.FullName = "Budi 2" }, 1, "full_name", LevelWarning},
		{"age for grade 7", nil, 7, "birth_date", LevelWarning},
		{"parent too young", func(in *Input) {
			in.Parents["ibu"] = Parent{Name: "Sari", Details: students.GuardianDetails{BirthYear: 2012}}
		}, 1, "ibu.birth_year", LevelWarning},
	}
	for _, c := range cases {
		in := good
		in.Parents = map[string]Parent{"ibu": good.Parents["ibu"], "ayah": good.Parents["ayah"]}
		if c.edit != nil {
			c.edit(&in)
		}
		got := Check(in, c.grade, today)
		if !strings.Contains(","+fields(got, c.level)+",", ","+c.field+",") {
			t.Errorf("%s: issues = %+v, want %s %s", c.name, got, c.level, c.field)
		}
	}
	// Kata yang mengandung "dr"/"h" tidak dianggap gelar.
	for _, ok := range []string{"Hendra Hidayat", "Andri Drajat", "Siti Sholihah"} {
		if issues := nameIssues("x", "Nama", ok); len(issues) != 0 {
			t.Errorf("%q flagged: %+v", ok, issues)
		}
	}
}

func TestCheckPage(t *testing.T) {
	e := newEnv(t)
	body := e.do(http.MethodGet, "/dapodik/cek", "kepala_sekolah", nil).Body.String()
	if !strings.Contains(body, "2 siswa aktif diperiksa") || !strings.Contains(body, "NIK belum diisi") || !strings.Contains(body, "1A") {
		t.Errorf("check page:\n%s", body)
	}
	if strings.Contains(body, "Perbaiki</a>") {
		t.Error("principal sees fix links")
	}
	if rec := e.do(http.MethodGet, "/dapodik/cek", "guru", nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher: %d", rec.Code)
	}
}
