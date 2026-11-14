package admission

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/pii"
	"akra/internal/students"
)

func TestValidateTransfer(t *testing.T) {
	today := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	ok := map[string]string{TransferNPSN: "20212345", TransferLastGrade: "3", TransferTargetGrade: "4",
		TransferReason: "  ikut   orang tua pindah tugas ", TransferEntryDate: "2027-01-04"}
	got, err := validateTransfer(ok, "SD Negeri 1 Cimahi", today)
	if err != nil || got[TransferReason] != "ikut orang tua pindah tugas" || got[TransferTargetGrade] != "4" {
		t.Fatalf("valid transfer: %v %v", got, err)
	}
	cases := []struct {
		name   string
		change map[string]string
		school string
		field  string
	}{
		{"no previous school", nil, "", "previous_school"},
		{"bad npsn", map[string]string{TransferNPSN: "1234"}, "SD", TransferNPSN},
		{"missing last grade", map[string]string{TransferLastGrade: ""}, "SD", TransferLastGrade},
		{"grade out of range", map[string]string{TransferTargetGrade: "13"}, "SD", TransferTargetGrade},
		{"skips a grade", map[string]string{TransferTargetGrade: "6"}, "SD", TransferTargetGrade},
		{"goes back", map[string]string{TransferTargetGrade: "2"}, "SD", TransferTargetGrade},
		{"no reason", map[string]string{TransferReason: " "}, "SD", TransferReason},
		{"long reason", map[string]string{TransferReason: strings.Repeat("a", 201)}, "SD", TransferReason},
		{"no entry date", map[string]string{TransferEntryDate: ""}, "SD", TransferEntryDate},
		{"entry too far", map[string]string{TransferEntryDate: "2028-01-04"}, "SD", TransferEntryDate},
	}
	for _, c := range cases {
		v := map[string]string{}
		for k, x := range ok {
			v[k] = x
		}
		for k, x := range c.change {
			v[k] = x
		}
		_, err := validateTransfer(v, c.school, today)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Fields[c.field] == "" {
			t.Errorf("%s: want error on %s, got %v", c.name, c.field, err)
		}
	}
	// NPSN opsional.
	delete(ok, TransferNPSN)
	if _, err := validateTransfer(ok, "SD", today); err != nil {
		t.Errorf("without npsn: %v", err)
	}
}

func TestMissingByKind(t *testing.T) {
	base := Applicant{Kind: KindNew, Values: map[string]string{}, Guardians: map[string]map[string]string{}, Transfer: map[string]string{}}
	keys := func(a Applicant) map[string]bool {
		m := map[string]bool{}
		for _, f := range Missing(a) {
			m[f.Key] = true
		}
		return m
	}
	if keys(base)[TransferReason] || keys(base)["previous_school"] {
		t.Error("new student asked for transfer fields")
	}
	tr := base
	tr.Kind = KindTransfer
	got := keys(tr)
	for _, k := range []string{"previous_school", TransferLastGrade, TransferTargetGrade, TransferReason, TransferEntryDate} {
		if !got[k] {
			t.Errorf("transfer missing %s not reported", k)
		}
		if stepOfField(k) != StepSchool {
			t.Errorf("stepOfField(%s) = %s", k, stepOfField(k))
		}
	}
	if got[TransferNPSN] {
		t.Error("npsn must be optional")
	}
}

// transferPeriod membuat periode pindahan yang sedang dibuka.
func (e *env) transferPeriod(t *testing.T) (Period, Track) {
	t.Helper()
	ctx := context.Background()
	f := e.form()
	f.Name, f.Kind, f.TargetGrade, f.FeeAmount = "Pindahan "+fmt.Sprint(time.Now().UnixNano()), KindTransfer, "", ""
	f.RequiredDocs = nil
	p, err := e.svc.Create(ctx, e.actor, f)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindTransfer || p.TargetGrade != 0 {
		t.Fatalf("transfer period = %+v", p)
	}
	tr, err := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Umum"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetStatus(ctx, e.actor, p.ID, StatusOpen); err != nil {
		t.Fatal(err)
	}
	return p, tr
}

func TestPeriodKind(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	f := e.form()
	f.TargetGrade = ""
	var ve *ValidationError
	if _, err := e.svc.Create(ctx, e.actor, f); !errors.As(err, &ve) || ve.Fields["target_grade"] == "" {
		t.Errorf("new-student period without grade: %v", err)
	}
	f.Kind = "lain"
	if _, err := e.svc.Create(ctx, e.actor, f); !errors.As(err, &ve) || ve.Fields["kind"] == "" {
		t.Errorf("unknown kind: %v", err)
	}
	p, _ := e.transferPeriod(t)
	// Dokumen khusus pindahan bisa diwajibkan.
	f = e.form()
	f.Name, f.Kind, f.RequiredDocs = "Pindahan dok", KindTransfer, []string{"kk", "surat_pindah", "rapor"}
	if withDocs, err := e.svc.Create(ctx, e.actor, f); err != nil || !withDocs.Requires("surat_pindah") || !withDocs.Requires("rapor") {
		t.Errorf("transfer documents: %+v %v", withDocs.RequiredDocs, err)
	}
	// Jenis tidak berubah saat periode diubah.
	f = e.form()
	f.Kind, f.TargetGrade = KindNew, ""
	got, err := e.svc.Update(ctx, e.actor, p.ID, f)
	if err != nil || got.Kind != KindTransfer {
		t.Errorf("update transfer period: %+v %v", got.Kind, err)
	}
}

func TestTransferPublicFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	newP, newTr := e.openPeriod(t)
	p, tr := e.transferPeriod(t)
	c := newPublicServer(t, e)

	// Halaman siswa baru hanya menampilkan periode siswa baru, dan
	// sebaliknya.
	_, body := c.get("/s/test-adm-a/daftar/")
	if !strings.Contains(body, newP.Name) || strings.Contains(body, p.Name) || !strings.Contains(body, "/s/test-adm-a/pindah") {
		t.Error("new-student start page lists wrong periods")
	}
	code, body := c.get("/s/test-adm-a/pindah/")
	if code != http.StatusOK || !strings.Contains(body, "Pendaftaran Siswa Pindahan") || !strings.Contains(body, p.Name) || strings.Contains(body, newP.Name) {
		t.Fatalf("transfer start page: %d", code)
	}
	start := url.Values{"choice": {newP.ID + "|" + newTr.ID}, "full_name": {"Rani Putri"}, "gender": {"P"}, "birth_date": {"2018-03-02"},
		"phone": {"081377770000"}, "consent": {"on"}}
	if code, _, _ := c.post("/s/test-adm-a/pindah/", start); code != http.StatusBadRequest {
		t.Errorf("new-student period via transfer form: %d", code)
	}
	start.Set("choice", p.ID+"|"+tr.ID)
	if code, loc, _ := c.post("/s/test-adm-a/pindah/", start); code != http.StatusSeeOther || !strings.HasSuffix(loc, "/daftar/isi/calon") {
		t.Fatalf("create transfer draft: %d %s", code, loc)
	}

	base := "/s/test-adm-a/daftar"
	steps := []struct {
		step string
		form url.Values
	}{
		{"calon", url.Values{"full_name": {"Rani Putri"}, "gender": {"P"}, "birth_date": {"2018-03-02"}, "nik": {childNIK}, "kk_number": {childKK},
			"birth_place": {"Cimahi"}, "religion": {"01"}, "nisn": {"0181234567"}}},
		{"alamat", url.Values{"street": {"Jl. Mawar 2"}, "village": {"Cibabat"}, "district": {"Cimahi Utara"}, "residence_type": {"01"}, "transport_mode": {"02"}}},
		{"orangtua", url.Values{"ibu.full_name": {"Wati"}, "ibu.occupation": {"98"}, "ibu.nik": {motherNIK}, "ibu.income": {"02"}}},
	}
	for _, s := range steps {
		if code, _, body := c.post(base+"/isi/"+s.step, s.form); code != http.StatusSeeOther {
			t.Fatalf("step %s: %d\n%s", s.step, code, body)
		}
	}
	code, body = c.get(base + "/isi/sekolah")
	if code != http.StatusOK || !strings.Contains(body, "Sekolah asal (sebelum pindah)") || !strings.Contains(body, "Alasan pindah") || strings.Contains(body, "Nomor peserta ujian") {
		t.Fatalf("transfer school step: %d", code)
	}
	school := url.Values{"previous_school": {"SD Negeri 1 Cimahi"}, TransferLastGrade: {"2"}, TransferTargetGrade: {"5"},
		TransferReason: {"Ikut orang tua pindah tugas"}, TransferEntryDate: {"2027-08-03"}}
	if code, _, body := c.post(base+"/isi/sekolah", school); code != http.StatusBadRequest || !strings.Contains(body, "Kelas tujuan biasanya") {
		t.Errorf("skipped grade accepted: %d", code)
	}
	school.Set(TransferTargetGrade, "3")
	if code, _, body := c.post(base+"/isi/sekolah", school); code != http.StatusSeeOther {
		t.Fatalf("school step: %d\n%s", code, body)
	}
	_, body = c.get(base + "/isi/kirim")
	if !strings.Contains(body, "Kelas 2 → kelas 3") || !strings.Contains(body, "Ikut orang tua pindah tugas") {
		t.Error("review without transfer summary")
	}
	if code, loc, _ := c.post(base+"/kirim", url.Values{}); code != http.StatusSeeOther || !strings.HasSuffix(loc, "/selesai") {
		t.Fatalf("submit: %d %s", code, loc)
	}
	_, body = c.get(base + "/selesai")
	number := regexp.MustCompile(`PINDAH-2027-\d{4}`).FindString(body)
	if number != "PINDAH-2027-0001" {
		t.Errorf("transfer number = %q", number)
	}

	keys := pii.DevKeyring()
	apps := NewApplicantService(e.tx, keys, e.svc)
	var id string
	if err := e.tx.QueryRow(ctx, `SELECT id FROM applicants WHERE registration_number = $1`, number).Scan(&id); err != nil {
		t.Fatal(err)
	}
	a, _ := apps.Load(ctx, e.schoolA, id)
	// Ibu meninggal: NIK dan penghasilan tidak disimpan.
	if m := a.Guardians[students.RelationMother]; m["nik"] != "" || m["income"] != "" || m["occupation"] != "98" {
		t.Errorf("deceased mother = %+v", m)
	}
	// Nomor siswa baru tetap urutan sendiri.
	newApp := e.submitted(t, apps, newP, newTr)
	if !strings.HasPrefix(newApp.RegistrationNumber, "SPMB-2027-") {
		t.Errorf("new-student number = %q", newApp.RegistrationNumber)
	}
	// Filter daftar per jenis.
	list, err := apps.List(ctx, e.schoolA, ListFilter{Kind: KindTransfer})
	if err != nil || len(list) != 1 || list[0].ID != id || list[0].TargetGrade != "3" {
		t.Errorf("transfer list = %+v %v", list, err)
	}

	// Diterima → siswa dengan jenis pendaftaran Pindahan dan tanggal masuk
	// dari rencana.
	for _, to := range []string{ApplicantVerified, ApplicantAccepted} {
		if _, err := apps.Transition(ctx, e.actor, id, to, ""); err != nil {
			t.Fatal(err)
		}
	}
	var classID string
	if err := e.tx.QueryRow(ctx, `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '3A', 3) RETURNING id`, e.schoolA, e.yearA).Scan(&classID); err != nil {
		t.Fatal(err)
	}
	sid, _, _, err := NewEnroller(e.tx, keys, apps).Enroll(ctx, e.actor, id, EnrollOptions{ClassID: classID})
	if err != nil {
		t.Fatal(err)
	}
	prof, _ := students.NewProfileService(e.tx, keys).Get(ctx, e.schoolA, sid)
	if prof.Get("registration_type") != "02" || prof.Get("entry_date") != "2027-08-03" || prof.Get("previous_school") != "SD Negeri 1 Cimahi" {
		t.Errorf("transfer profile type=%q entry=%q school=%q", prof.Get("registration_type"), prof.Get("entry_date"), prof.Get("previous_school"))
	}
	hist, _ := students.ListEnrollments(ctx, e.tx, e.schoolA, sid)
	if len(hist) != 1 || hist[0].ClassName != "3A" || hist[0].StartDate.Format(dateLayout) != "2027-08-03" {
		t.Errorf("enrollments = %+v", hist)
	}
}

func TestNewStudentFormSimplified(t *testing.T) {
	e := newEnv(t)
	p, tr := e.openPeriod(t)
	c := newPublicServer(t, e)
	base := "/s/test-adm-a/daftar"
	start := url.Values{"choice": {p.ID + "|" + tr.ID}, "full_name": {"Budi"}, "gender": {"L"}, "birth_date": {"2019-05-17"}, "phone": {"081234567890"}, "consent": {"on"}}
	if code, _, _ := c.post(base+"/", start); code != http.StatusSeeOther {
		t.Fatal("create draft")
	}
	_, body := c.get(base + "/isi/sekolah")
	for _, hidden := range []string{"Nomor peserta ujian", "No seri ijazah", "No SKHUN", "Alasan pindah"} {
		if strings.Contains(body, hidden) {
			t.Errorf("new-student school step shows %q", hidden)
		}
	}
	if !strings.Contains(body, "SD/MI asal") {
		t.Error("previous school label for grade 7 missing")
	}
	_, body = c.get(base + "/isi/alamat")
	if strings.Contains(body, "Layak PIP") || !strings.Contains(body, "Bantuan pemerintah") || !strings.Contains(body, `x-show="hasKip"`) {
		t.Error("address step: PIP shown or assistance section missing")
	}
	_, body = c.get(base + "/isi/orangtua")
	if !strings.Contains(body, "showWali: false") || !strings.Contains(body, "+ Tambah data wali") {
		t.Error("guardian section not collapsed by default")
	}

	// KKS tidak dicentang: nomor KKS tidak disimpan; KIP dicentang: disimpan.
	form := url.Values{"street": {"Jl. Melati 5"}, "residence_type": {"02"}, "has_kip": {"on"}, "kip_number": {"KIP1"}, "kks_number": {"KKS1"}}
	if code, _, body := c.post(base+"/isi/alamat", form); code != http.StatusSeeOther {
		t.Fatalf("address: %d\n%s", code, body)
	}
	var kip, kks []byte
	_ = e.tx.QueryRow(context.Background(), `SELECT kip_enc, kks_enc FROM applicants WHERE school_id = $1 AND full_name = 'Budi' ORDER BY created_at DESC LIMIT 1`, e.schoolA).Scan(&kip, &kks)
	if kip == nil || kks != nil {
		t.Errorf("kip stored=%v kks stored=%v", kip != nil, kks != nil)
	}
	// Tinggal bersama wali: bagian wali langsung terbuka.
	_, body = c.get(base + "/isi/orangtua")
	if !strings.Contains(body, "showWali: true") {
		t.Error("guardian section closed although living with guardian")
	}
}

func TestAddressWilayah(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.tx.Exec(ctx, `INSERT INTO wilayah (kode, nama, tingkat, induk, kode_pos) VALUES
		('32', 'Jawa Barat', 1, NULL, NULL), ('32.73', 'Kota Bandung', 2, '32', NULL),
		('32.73.04', 'Bojongloa Kaler', 3, '32.73', NULL), ('32.73.04.1001', 'Kopo', 4, '32.73.04', '40233')
		ON CONFLICT (kode) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	p, tr := e.openPeriod(t)
	c := newPublicServer(t, e)
	base := "/s/test-adm-a/daftar"
	start := url.Values{"choice": {p.ID + "|" + tr.ID}, "full_name": {"Budi"}, "gender": {"L"}, "birth_date": {"2019-05-17"}, "phone": {"081234567890"}, "consent": {"on"}}
	if code, _, _ := c.post(base+"/", start); code != http.StatusSeeOther {
		t.Fatal("create draft")
	}
	_, body := c.get(base + "/isi/alamat")
	// T106: lintang/bujur satu kelompok dengan tombol ambil lokasi.
	if !strings.Contains(body, "Ambil lokasi saya") || !strings.Contains(body, "Titik lokasi rumah") || !strings.Contains(body, "getCurrentPosition") {
		t.Error("address step without location button")
	}
	if code, _, body := c.post(base+"/isi/alamat", url.Values{"street": {"Jl. Melati 5"}, "latitude": {"48.85"}, "longitude": {"2.35"}}); code != http.StatusBadRequest || !strings.Contains(body, "Lintang") {
		t.Errorf("coordinates outside Indonesia: %d", code)
	}
	if !strings.Contains(body, "js/wilayah.js") || !strings.Contains(body, `x-data="wilayahPicker($el)"`) || !strings.Contains(body, "Desa tidak ada di daftar") {
		t.Error("address step without wilayah picker")
	}
	addr := url.Values{"street": {"Jl. Melati 5"}, "wilayah_kode": {"32.73.04.9999"}}
	if code, _, body := c.post(base+"/isi/alamat", addr); code != http.StatusBadRequest || !strings.Contains(body, "tidak ditemukan di daftar wilayah") {
		t.Errorf("unknown code: %d", code)
	}
	addr.Set("wilayah_kode", "32.73.04.1001")
	addr.Set("village", "ketikan bebas")
	addr.Set("latitude", "-6.9347")
	addr.Set("longitude", "107.5953")
	if code, _, body := c.post(base+"/isi/alamat", addr); code != http.StatusSeeOther {
		t.Fatalf("save address: %d\n%s", code, body)
	}
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	var id string
	_ = e.tx.QueryRow(ctx, `SELECT id FROM applicants WHERE school_id = $1 ORDER BY created_at DESC LIMIT 1`, e.schoolA).Scan(&id)
	a, _ := apps.Load(ctx, e.schoolA, id)
	if a.Values["village"] != "Kopo" || a.Values["district"] != "Bojongloa Kaler" || a.Values["postal_code"] != "40233" || a.Values["latitude"] != "-6.9347" || a.Values["longitude"] != "107.5953" {
		t.Errorf("applicant address = %v", a.Values)
	}
	// Manual (desa tidak ada di daftar): nama disimpan apa adanya, ditandai di detail TU.
	manual := url.Values{"street": {"Jl. Melati 5"}, "village": {"Desa Pemekaran"}, "district": {"Bojongloa Kaler"}}
	if code, _, _ := c.post(base+"/isi/alamat", manual); code != http.StatusSeeOther {
		t.Fatal("manual address")
	}
	a, _ = apps.Load(ctx, e.schoolA, id)
	if a.Values["village"] != "Desa Pemekaran" || a.Values["wilayah_kode"] != "" {
		t.Errorf("manual address = %v", a.Values)
	}
	rows := displayApplicant(a)
	flagged := false
	for _, s := range rows {
		for _, r := range s.Rows {
			flagged = flagged || strings.Contains(r.Value, "diisi manual")
		}
	}
	if !flagged {
		t.Error("manual address not flagged for staff")
	}
}
