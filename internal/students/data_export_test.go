package students

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"akra/internal/platform/database"
)

// Ekspor data pribadi siswa (UU PDP, T153): hanya admin TU, sekolah sendiri,
// tercatat di audit, dan memuat bagian ujian dari modul ujian.
func TestDataExport(t *testing.T) {
	e := newHEnv(t)
	ctx := context.Background()
	var id string
	if err := e.tx.QueryRow(ctx, `INSERT INTO students (school_id, nis, full_name) VALUES ($1, 'PDP-1', 'Siswa Ekspor') RETURNING id`, e.schoolA).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, '2026-07-13')`,
		e.schoolA, id, e.class1A, e.yearActive); err != nil {
		t.Fatal(err)
	}
	path := "/siswa/" + id + "/data-pribadi.json"
	if rec := e.do(http.MethodGet, path, principal, nil); rec.Code != http.StatusForbidden {
		t.Errorf("principal = %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/siswa/"+e.studentB+"/data-pribadi.json", admin, nil); rec.Code != http.StatusNotFound {
		t.Errorf("other school's student = %d", rec.Code)
	}
	rec := e.do(http.MethodGet, path, admin, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("export = %d", rec.Code)
	}
	var d map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"siswa", "data_rinci", "wali", "riwayat_kelas", "absensi", "nilai", "tagihan", "pembayaran", "ujian"} {
		if _, ok := d[k]; !ok {
			t.Errorf("export lacks %q", k)
		}
	}
	if string(d["ujian"]) != "[]" {
		t.Errorf("exam section without exporter = %s", d["ujian"])
	}
	if e.audits(ActionDataExport) != 1 {
		t.Errorf("export audit = %d", e.audits(ActionDataExport))
	}
	// Bagian ujian dari pengekspor yang dipasang modul ujian.
	called := ""
	exp, err := BuildDataExport(ctx, e.tx, nil, func(_ context.Context, _ database.DBTX, school, student string) (any, error) {
		called = school + "|" + student
		return []string{"ujian-1"}, nil
	}, e.schoolA, id, e.svc.today())
	if err != nil || called != e.schoolA+"|"+id || len(exp.Exams.([]string)) != 1 || len(exp.Enrollments) != 1 {
		t.Errorf("export with exam exporter = %+v, %v", exp, err)
	}
}
