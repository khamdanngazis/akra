package dapodik

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"akra/internal/students"
)

func TestGradeFillHandler(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := students.NewProfileService(e.tx, e.keys).Save(ctx, e.actor, e.budi, map[string]string{"nisn": "0091234567"}); err != nil {
		t.Fatal(err)
	}
	var subject, yearID, comp, enrollBudi string
	q := func(sql string, dst *string, args ...any) {
		t.Helper()
		if err := e.tx.QueryRow(ctx, sql, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	q(`SELECT academic_year_id FROM classes WHERE id = $1`, &yearID, e.classA)
	q(`INSERT INTO subjects (school_id, name) VALUES ($1, 'Bahasa Indonesia') RETURNING id`, &subject, e.schoolA)
	q(`INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent) VALUES ($1, $2, $3, 'Ujian', 100) RETURNING id`, &comp, e.schoolA, yearID, subject)
	q(`SELECT id FROM enrollments WHERE student_id = $1`, &enrollBudi, e.budi)
	if _, err := e.tx.Exec(ctx, `INSERT INTO grades (school_id, academic_year_id, enrollment_id, component_id, score, recorded_by) VALUES ($1, $2, $3, $4, 86.5, $5)`,
		e.schoolA, yearID, enrollBudi, comp, testUser); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(e.router)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(role, classID, subjectID string, data []byte) (int, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("class_id", classID)
		_ = mw.WriteField("subject_id", subjectID)
		fw, _ := mw.CreateFormFile("file", "format.xlsx")
		_, _ = fw.Write(data)
		_ = mw.Close()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dapodik/nilai", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Role", role)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	tpl := gradeTemplate(t)

	if code, body := post("admin_tu", e.classA, subject, []byte("x")); code != http.StatusBadRequest || !strings.Contains(body, "bukan format import nilai") {
		t.Errorf("bad file: %d", code)
	}
	// Guru tanpa penugasan ditolak.
	if code, body := post("guru", e.classA, subject, tpl); code != http.StatusForbidden || !strings.Contains(body, "mapel dan kelas yang Anda ampu") {
		t.Errorf("unassigned teacher: %d", code)
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`,
		e.schoolA, yearID, e.classA, subject, testUser); err != nil {
		t.Fatal(err)
	}
	code, body := post("guru", e.classA, subject, tpl)
	if code != http.StatusOK || !strings.Contains(body, "File terisi: 1 siswa") || !strings.Contains(body, "Tidak Ada (NIS 999)") {
		t.Fatalf("fill: %d\n%s", code, body)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dapodik/nilai/unduh", nil)
	req.Header.Set("X-Role", "guru")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(res.Body)
	res.Body.Close()
	f, err := excelize.OpenReader(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("download: %d %v", res.StatusCode, err)
	}
	g, _ := f.GetCellValue(gradeSheet, "G18")
	h, _ := f.GetCellValue(gradeSheet, "H18")
	if g != "87" || h != "B" {
		t.Errorf("filled G18=%q H18=%q", g, h)
	}
	if code, _ := post("admin_tu", e.classA, "00000000-0000-0000-0000-000000000000", tpl); code != http.StatusBadRequest {
		t.Errorf("unknown subject: %d", code)
	}
	if rec := e.do(http.MethodGet, "/dapodik/nilai", "kepala_sekolah", nil); rec.Code != http.StatusForbidden {
		t.Errorf("principal: %d", rec.Code)
	}
}
