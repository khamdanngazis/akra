package dapodik

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"akra/internal/dapodik/ref"
	"akra/internal/platform/pii"
	"akra/internal/students"
)

func exportFile(t *testing.T, rows []ExportRow) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WritePDExport(&buf, "SD Uji", "TU", rows, time.Now()); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPlanAndApplyPD(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// Budi sudah punya agama berbeda di AKRA; alamat masih kosong.
	if _, err := students.NewProfileService(e.tx, e.keys).Save(ctx, e.actor, e.budi, map[string]string{"religion": "02"}); err != nil {
		t.Fatal(err)
	}
	birth := time.Date(2019, 5, 17, 0, 0, 0, 0, time.UTC)
	file := exportFile(t, []ExportRow{
		{ClassName: "1A", Input: Input{ // cocok lewat NIS 001
			Student: students.Student{FullName: "Budi", NIS: "001", Gender: "L", BirthDate: &birth},
			Profile: students.ProfileForTest(map[string]string{"religion": "01", "street": "Jl. Melati 5", "nik": "3201010101010001", "special_needs": "02,03", "has_kip": "true"}),
			Parents: map[string]Parent{"ibu": {Name: "Sari", Details: students.GuardianDetails{NIK: pii.New("3201010101800002"), Education: "09"}}},
		}},
		{ClassName: "1A", Input: Input{ // siswa baru
			Student: students.Student{FullName: "Dewi", NIS: "050", Gender: "P", BirthDate: &birth},
			Profile: students.ProfileForTest(map[string]string{"nisn": "0091234567", "residence_type": "01"}),
		}},
	})
	plan, err := PlanPD(ctx, e.tx, e.keys, e.schoolA, bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Matched != 1 || plan.New != 1 || plan.WithError != 0 || len(plan.Rows) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	b := plan.Rows[0]
	if b.StudentID != e.budi || b.MatchBy != "NIS" || len(b.Conflicts) != 1 || b.Conflicts[0].Key != "religion" || b.Conflicts[0].Incoming != "Islam" {
		t.Errorf("budi row = %+v", b)
	}

	// Konflik agama tidak dicentang: hanya field kosong yang diisi.
	res, err := ApplyPD(ctx, e.tx, e.keys, e.actor, plan, map[string]bool{})
	if err != nil || res.Created != 1 || res.Updated != 1 || len(res.Failed) != 0 {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	prof, _ := students.NewProfileService(e.tx, e.keys).Get(ctx, e.schoolA, e.budi)
	if prof.Get("religion") != "02" || prof.Get("street") != "Jl. Melati 5" || prof.Secret("nik").Reveal() != "3201010101010001" || prof.Get("special_needs") != "02,03" || prof.Get("has_kip") != "true" {
		t.Errorf("budi profile religion=%q street=%q", prof.Get("religion"), prof.Get("street"))
	}
	gs, _ := students.ListByStudent(ctx, e.tx, e.schoolA, e.budi)
	if len(gs) != 1 || gs[0].FullName != "Sari" || gs[0].Relationship != "ibu" {
		t.Errorf("guardians = %+v", gs)
	}
	var dewi string
	if err := e.tx.QueryRow(ctx, `SELECT id FROM students WHERE school_id = $1 AND nis = '050'`, e.schoolA).Scan(&dewi); err != nil {
		t.Fatal("new student not created")
	}
	hist, _ := students.ListEnrollments(ctx, e.tx, e.schoolA, dewi)
	if len(hist) != 1 || hist[0].ClassName != "1A" {
		t.Errorf("new student class = %+v", hist)
	}

	// Impor ulang file yang sama: tidak ada yang berubah selain konflik agama.
	plan2, _ := PlanPD(ctx, e.tx, e.keys, e.schoolA, bytes.NewReader(file))
	for _, r := range plan2.Rows {
		if len(r.Fills) != 0 || r.StudentID == "" {
			t.Errorf("re-import row %d fills=%v id=%q", r.Line, r.Fills, r.StudentID)
		}
	}
	// Dicentang: agama diganti.
	if _, err := ApplyPD(ctx, e.tx, e.keys, e.actor, plan2, map[string]bool{"7:religion": true}); err != nil {
		t.Fatal(err)
	}
	prof, _ = students.NewProfileService(e.tx, e.keys).Get(ctx, e.schoolA, e.budi)
	if prof.Get("religion") != "01" {
		t.Errorf("checked conflict not applied: %q", prof.Get("religion"))
	}
}

func TestPlanPDErrors(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if c, ok := labelsToCodes(ref.KebutuhanKhusus, "Netra (A), Rungu (B)"); !ok || c != "02,03" {
		t.Errorf("multi labels = %q, %v", c, ok)
	}
	if _, ok := labelsToCodes(ref.Agama, "Agama Baru"); ok {
		t.Error("unknown label accepted")
	}
	// Label pilihan tidak dikenal menjadi error per baris, bukan dikosongkan.
	f, err := excelize.OpenReader(bytes.NewReader(exportFile(t, []ExportRow{{Input: Input{Student: students.Student{FullName: "X", NIS: "099"}, Profile: students.ProfileForTest(map[string]string{})}}})))
	if err != nil {
		t.Fatal(err)
	}
	_ = f.SetCellStr("Sheet1", "I7", "Agama Baru")
	_ = f.SetCellStr("Sheet1", "D7", "X")
	var buf bytes.Buffer
	_ = f.Write(&buf)
	plan, err := PlanPD(ctx, e.tx, e.keys, e.schoolA, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if plan.WithError != 1 || len(plan.Rows[0].Errors) != 2 {
		t.Errorf("errors = %+v", plan.Rows[0].Errors)
	}
	res, _ := ApplyPD(ctx, e.tx, e.keys, e.actor, plan, nil)
	if res.Created != 0 || len(res.Failed) != 1 {
		t.Errorf("apply with errors = %+v", res)
	}
	if _, err := PlanPD(ctx, e.tx, e.keys, e.schoolA, bytes.NewReader(exportFileWithHeader(t))); err != ErrNotPDExport {
		t.Errorf("non-PD file: %v", err)
	}
}

// exportFileWithHeader membuat Excel biasa tanpa kolom NIPD.
func exportFileWithHeader(t *testing.T) []byte {
	f := excelize.NewFile()
	_ = f.SetSheetRow("Sheet1", "A1", &[]string{"NIS", "Nama lengkap", "Kelas"})
	_ = f.SetSheetRow("Sheet1", "A2", &[]string{"1", "A", "1A"})
	var buf bytes.Buffer
	_ = f.Write(&buf)
	return buf.Bytes()
}

func TestImportHandler(t *testing.T) {
	e := newEnv(t)
	birth := time.Date(2019, 5, 17, 0, 0, 0, 0, time.UTC)
	file := exportFile(t, []ExportRow{{ClassName: "1A", Input: Input{Student: students.Student{FullName: "Eka", NIS: "060", Gender: "P", BirthDate: &birth}, Profile: students.ProfileForTest(map[string]string{})}}})
	srv := httptest.NewServer(e.router)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	upload := func(data []byte) (int, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", "pd.xlsx")
		_, _ = fw.Write(data)
		_ = mw.Close()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dapodik/impor", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Role", "admin_tu")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := upload([]byte("bukan excel")); code != http.StatusBadRequest || !strings.Contains(body, ".xlsx") {
		t.Errorf("bad file: %d", code)
	}
	code, body := upload(file)
	if code != http.StatusOK || !strings.Contains(body, "1 siswa baru") {
		t.Fatalf("preview: %d %s", code, body)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dapodik/impor/terapkan", strings.NewReader(url.Values{}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Role", "admin_tu")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(b), "1 siswa baru dibuat") {
		t.Errorf("apply: %d %s", res.StatusCode, b)
	}
	if rec := e.do(http.MethodGet, "/dapodik/impor", "kepala_sekolah", nil); rec.Code != http.StatusForbidden {
		t.Errorf("principal import: %d", rec.Code)
	}
}
