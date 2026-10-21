package importer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"akra/internal/auth"
	"akra/internal/grades"
	"akra/internal/school"
)

// gradeSetup menyiapkan data nilai sekolah A: MTK diampu guru di 1A
// (komponen UTS dan UAS aktif, Remedial nonaktif), IPA tanpa penugasan di 1A.
type gradeSetup struct {
	mtk, ipa         string
	uts, uas, ipaUTS string
}

func (f *uploadFixture) setupGrades(t *testing.T) gradeSetup {
	t.Helper()
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := f.tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	var g gradeSetup
	year := one(`SELECT academic_year_id FROM classes WHERE id = $1`, f.class1A)
	teacher := one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'guru@a.id', 'x', 'Guru MTK') RETURNING id`, f.schoolA)
	subject := `INSERT INTO subjects (school_id, name, code) VALUES ($1, $2, $3) RETURNING id`
	g.mtk, g.ipa = one(subject, f.schoolA, "Matematika", "MTK"), one(subject, f.schoolA, "IPA", "IPA")
	if _, err := f.tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`,
		f.schoolA, year, f.class1A, g.mtk, teacher); err != nil {
		t.Fatal(err)
	}
	comp := `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent, is_active) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`
	g.uts, g.uas = one(comp, f.schoolA, year, g.mtk, "UTS", 40, true), one(comp, f.schoolA, year, g.mtk, "UAS", 60, true)
	one(comp, f.schoolA, year, g.mtk, "Remedial", 10, false)
	g.ipaUTS = one(comp, f.schoolA, year, g.ipa, "UTS", 100, true)
	return g
}

// enroll mendaftarkan siswa ber-NIS ke kelas 1A tahun ajaran aktif.
func (f *uploadFixture) enroll(t *testing.T, nis ...string) {
	t.Helper()
	if _, err := f.tx.Exec(context.Background(), `WITH s AS (
			INSERT INTO students (school_id, nis, full_name) SELECT $1, n, 'Siswa ' || n FROM unnest($3::text[]) n RETURNING id)
		INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
		SELECT $1, s.id, c.id, c.academic_year_id, '2026-07-13' FROM s, classes c WHERE c.id = $2`, f.schoolA, f.class1A, nis); err != nil {
		t.Fatal(err)
	}
}

func gradeHeader() []any { return []any{"NIS", "Kode mapel", "Nama komponen", "Nilai"} }

// uploadGrades mengunggah file nilai ke /impor/nilai (mapping kosong = judul baku).
func (f *uploadFixture) uploadGrades(t *testing.T, mappingID string, data []byte, roles ...string) *httptest.ResponseRecorder {
	t.Helper()
	if roles == nil {
		roles = []string{auth.RoleAdminTU}
	}
	req := multipartRequest(t, map[string][]string{"mapping_id": {mappingID}}, "nilai.xlsx", data)
	req.URL.Path, req.RequestURI = gradesPath, gradesPath
	return f.do(req, f.schoolA, f.adminA, roles...)
}

func (f *uploadFixture) gradeBatch(t *testing.T, mappingID string, rows ...[]any) string {
	t.Helper()
	rec := f.uploadGrades(t, mappingID, buildXLSX(t, append([][]any{gradeHeader()}, rows...)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("grade upload: status = %d\n%s", rec.Code, rec.Body.String())
	}
	return strings.TrimPrefix(rec.Header().Get("Location"), "/impor/batch/")
}

// rowKinds mengembalikan jenis kesalahan per baris file.
func (f *uploadFixture) rowKinds(t *testing.T, batchID string) map[int]string {
	t.Helper()
	rows, err := f.tx.Query(context.Background(), `SELECT line, coalesce(string_agg(e->>'kind', ',' ORDER BY e->>'kind'), '')
		FROM import_rows r LEFT JOIN LATERAL jsonb_array_elements(r.errors) e ON true
		WHERE r.school_id = $1 AND r.batch_id = $2 GROUP BY line`, f.schoolA, batchID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var line int
		var k string
		if err := rows.Scan(&line, &k); err != nil {
			t.Fatal(err)
		}
		out[line] = k
	}
	return out
}

func (f *uploadFixture) score(t *testing.T, nis, componentID string) string {
	t.Helper()
	var s *string
	if err := f.tx.QueryRow(context.Background(), `SELECT g.score::text FROM grades g
		JOIN enrollments e ON e.id = g.enrollment_id JOIN students st ON st.id = e.student_id
		WHERE g.school_id = $1 AND st.nis = $2 AND g.component_id = $3`, f.schoolA, nis, componentID).Scan(&s); err != nil {
		t.Fatalf("score %s: %v", nis, err)
	}
	if s == nil {
		return "NULL"
	}
	return *s
}

func TestGradeImport_ValidationSaveAndReimport(t *testing.T) {
	f := newUploadFixture(t)
	g := f.setupGrades(t)
	f.enroll(t, "001", "002", "003", "005")
	audits := func(action string) int {
		return f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, f.schoolA, action)
	}
	rows := [][]any{
		{"001", "MTK", "UTS", "85,5"}, // baris 2: valid
		{"002", "mtk", " uts ", ""},   // baris 3: valid, kosong = belum dinilai
		{"003", "IPA", "UTS", "80"},   // baris 4: IPA tanpa guru di 1A
		{"004", "MTK", "UTS", "80"},   // baris 5: siswa tidak terdaftar
		{"001", "MTK", "Remedial", "70"},
		{"005", "MTK", "UAS", "101"},
		{"001", "MTK", "UTS", "90"}, // baris 8: ganda dengan baris 2
		{"003", "BIO", "", "abc"},
	}
	batchID := f.gradeBatch(t, "", rows...)
	b := f.batch(t, batchID)
	if b.Target != TargetGrades || !b.Grades() || b.ValidRows != 2 || b.ErrorRows != 6 || b.NewRows() != 2 {
		t.Fatalf("batch = %+v", b)
	}
	want := map[int]string{2: "", 3: "", 4: KindNoAssignment, 5: KindGradeStudent, 6: KindComponentNotFound, 7: KindScore, 8: KindGradeDuplicate,
		9: strings.Join([]string{KindComponentEmpty, KindSubjectNotFound, KindScore}, ",")}
	if got := f.rowKinds(t, batchID); !reflect.DeepEqual(got, want) {
		t.Errorf("row kinds:\n got %v\nwant %v", got, want)
	}
	page := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID, nil), f.schoolA, f.adminA, auth.RoleAdminTU).Body.String()
	for _, s := range []string{"Impor nilai", "Simpan 2 nilai", "belum punya guru pengampu di kelas 1A", `href="/impor/nilai"`} {
		if !strings.Contains(page, s) {
			t.Errorf("batch page misses %q", s)
		}
	}
	// Laporan kesalahan memakai kolom nilai, bukan kolom siswa.
	if x := f.report(t, batchID); x != nil {
		hdr, _ := x.GetRows(x.GetSheetName(0))
		if len(hdr) == 0 || hdr[0][1] != "NIS" || hdr[0][2] != "Kode mapel" {
			t.Errorf("error report header = %v", hdr)
		}
	}

	if rec := f.runBatch(t, batchID); rec.Code != http.StatusSeeOther {
		t.Fatalf("run: status = %d", rec.Code)
	}
	b = f.batch(t, batchID)
	if b.Status != StatusDone || b.CreatedCount != 2 || b.FailedCount != 0 {
		t.Fatalf("after run = %+v", b)
	}
	if s := f.score(t, "001", g.uts); s != "85.50" {
		t.Errorf("001 UTS = %s", s)
	}
	// Nilai kosong = belum dinilai: tidak membuat baris nilai (aturan T70).
	if n := f.count(t, `SELECT count(*) FROM grades WHERE school_id = $1`, f.schoolA); n != 1 {
		t.Errorf("grades = %d", n)
	}
	if n := audits(grades.ActionGradeCreate); n != 1 {
		t.Errorf("grade.create audits = %d", n)
	}
	// Audit dicatat atas nama pengunggah.
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2 AND user_id = $3`, f.schoolA, grades.ActionGradeCreate, f.adminA); n != 1 {
		t.Errorf("audits by uploader = %d", n)
	}

	// Impor ulang file yang sama: baris valid menjadi "diperbarui", tidak
	// ada nilai ganda, dan nilai yang tidak berubah tidak diaudit.
	again := f.gradeBatch(t, "", rows...)
	if b := f.batch(t, again); b.UpdateRows != 1 || b.NewRows() != 1 {
		t.Fatalf("reimport batch = %+v", b)
	}
	f.runBatch(t, again)
	if n := f.count(t, `SELECT count(*) FROM grades WHERE school_id = $1`, f.schoolA); n != 1 {
		t.Errorf("grades after reimport = %d", n)
	}
	if n := audits(grades.ActionGradeUpdate); n != 0 {
		t.Errorf("grade.update audits after identical reimport = %d", n)
	}
	// Nilai berubah: diganti, satu audit update.
	changed := f.gradeBatch(t, "", []any{"001", "MTK", "UTS", "88"})
	f.runBatch(t, changed)
	if s := f.score(t, "001", g.uts); s != "88.00" {
		t.Errorf("001 UTS after change = %s", s)
	}
	if n := audits(grades.ActionGradeUpdate); n != 1 {
		t.Errorf("grade.update audits = %d", n)
	}
	// Nilai yang sudah ada dikosongkan: menjadi belum dinilai, tetap satu baris.
	cleared := f.gradeBatch(t, "", []any{"001", "MTK", "UTS", ""})
	f.runBatch(t, cleared)
	if s := f.score(t, "001", g.uts); s != "NULL" {
		t.Errorf("001 UTS after clearing = %s", s)
	}
	if n := f.count(t, `SELECT count(*) FROM grades WHERE school_id = $1`, f.schoolA); n != 1 {
		t.Errorf("grades after change = %d", n)
	}
}

// File dengan judul kolom berbeda + mapping menghasilkan data batch yang
// sama persis dengan file baku, untuk siswa maupun nilai.
func TestMappedImport_SameAsStandard(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	f.setupGrades(t)
	f.enroll(t, "001", "002")
	svc := NewMappingService(f.tx)
	actor := school.Actor{SchoolID: f.schoolA, UserID: f.adminA}
	batchData := func(id string) [][2]any {
		t.Helper()
		rs, err := f.tx.Query(ctx, `SELECT raw, data FROM import_rows WHERE school_id = $1 AND batch_id = $2 ORDER BY line`, f.schoolA, id)
		if err != nil {
			t.Fatal(err)
		}
		defer rs.Close()
		var out [][2]any
		for rs.Next() {
			var raw []string
			var data map[string]any
			if err := rs.Scan(&raw, &data); err != nil {
				t.Fatal(err)
			}
			out = append(out, [2]any{raw, data})
		}
		return out
	}

	// Siswa.
	stdID := f.uploadAndValidate(t,
		[]any{"007", "Siti Aminah", "2019-05-02", "P", "1A", "Aminah", "081234567890", "aminah@contoh.id"},
		[]any{"008", "Budi Santoso", "", "L", "1B", "", "", ""})
	sm, err := svc.Create(ctx, actor, "Dapodik", TargetStudents, ColumnMap{
		"NIS": "NIPD", "Nama lengkap": "Nama Peserta Didik", "Tanggal lahir": "Tanggal Lahir", "Jenis kelamin": "JK", "Kelas": "Rombel",
		"Nama wali": "Nama Ibu", "Telepon wali": "HP Ortu", "Email wali": "Email Ortu"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	other := buildXLSX(t, [][]any{
		{"DAFTAR PESERTA DIDIK"},
		{"No", "Rombel", "Nama Peserta Didik", "NIPD", "JK", "Tanggal Lahir", "Agama", "Nama Ibu", "HP Ortu", "Email Ortu"},
		{1, "1A", "Siti Aminah", "007", "P", 43587, "Islam", "Aminah", "081234567890", "aminah@contoh.id"},
		{2, "1B", "Budi Santoso", "008", "L", "", "Islam", "", "", ""},
	}, 1, 3, 4, 5)
	req := multipartRequest(t, map[string][]string{"mapping_id": {sm.ID}}, "dapodik.xlsx", other)
	req.URL.Path, req.RequestURI = "/impor/siswa", "/impor/siswa"
	rec := f.do(req, f.schoolA, f.adminA, auth.RoleAdminTU)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("mapped student upload: %d\n%s", rec.Code, rec.Body.String())
	}
	mappedID := strings.TrimPrefix(rec.Header().Get("Location"), "/impor/batch/")
	if got, want := batchData(mappedID), batchData(stdID); len(want) != 2 || !reflect.DeepEqual(got, want) {
		t.Errorf("student data:\n got %v\nwant %v", got, want)
	}
	if b := f.batch(t, mappedID); b.MappingName != "Dapodik" || b.ValidRows != 2 {
		t.Errorf("mapped student batch = %+v", b)
	}

	// Nilai.
	rows := [][]any{{"001", "MTK", "UTS", "85,5"}, {"002", "MTK", "UAS", ""}}
	stdGrades := f.gradeBatch(t, "", rows...)
	gm, err := svc.Create(ctx, actor, "Rapor lama", TargetGrades, ColumnMap{"NIS": "No Induk", "Kode mapel": "Mapel", "Nama komponen": "Jenis Penilaian", "Nilai": "Skor"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gf := buildXLSX(t, [][]any{
		{"Mapel", "No Induk", "Jenis Penilaian", "Skor", "Keterangan"},
		{"MTK", "001", "UTS", "85,5", "-"},
		{"MTK", "002", "UAS", "", "susulan"},
	})
	rec = f.uploadGrades(t, gm.ID, gf)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("mapped grade upload: %d\n%s", rec.Code, rec.Body.String())
	}
	mappedGrades := strings.TrimPrefix(rec.Header().Get("Location"), "/impor/batch/")
	if got, want := batchData(mappedGrades), batchData(stdGrades); len(want) != 2 || !reflect.DeepEqual(got, want) {
		t.Errorf("grade data:\n got %v\nwant %v", got, want)
	}

	// Mapping target lain ditolak.
	if rec := f.uploadGrades(t, sm.ID, gf); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("student mapping on grades: %d", rec.Code)
	}
	// Halaman impor nilai hanya menawarkan mapping nilai.
	page := f.do(httptest.NewRequest(http.MethodGet, gradesPath, nil), f.schoolA, f.adminA, auth.RoleAdminTU).Body.String()
	if !strings.Contains(page, "Mapping: Rapor lama") || strings.Contains(page, "Mapping: Dapodik") {
		t.Error("grades page mapping options")
	}
}

// 1.000 baris nilai: disimpan lewat job River, bukan di request.
func TestGradeImport_ThousandRowsViaJob(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	g := f.setupGrades(t)
	var nis []string
	var rows [][]any
	for i := range 500 {
		n := fmt.Sprintf("%04d", i)
		nis = append(nis, n)
		rows = append(rows, []any{n, "MTK", "UTS", fmt.Sprint(i % 101)}, []any{n, "MTK", "UAS", fmt.Sprintf("%d,5", i%100)})
	}
	f.enroll(t, nis...)
	batchID := f.gradeBatch(t, "", rows...)
	if b := f.batch(t, batchID); b.ValidRows != 1000 || b.ErrorRows != 0 {
		t.Fatalf("batch = %+v", b)
	}
	if rec := f.runBatch(t, batchID); rec.Code != http.StatusSeeOther {
		t.Fatalf("run: %d", rec.Code)
	}
	b := f.batch(t, batchID)
	if b.Status != StatusRunning || b.Mode != "async" || len(f.jobs.args) != 1 {
		t.Fatalf("batch = %+v, jobs %d", b, len(f.jobs.args))
	}
	if n := f.count(t, `SELECT count(*) FROM grades WHERE school_id = $1`, f.schoolA); n != 0 {
		t.Errorf("grades saved during request: %d", n)
	}
	w := NewImportStudentsWorker(f.tx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := w.run(ctx, f.jobs.args[0].(ImportStudentsArgs)); err != nil {
		t.Fatal(err)
	}
	b = f.batch(t, batchID)
	if b.Status != StatusDone || b.ProcessedRows != 1000 || b.CreatedCount != 1000 || b.FailedCount != 0 {
		t.Errorf("after job = %+v", b)
	}
	if n := f.count(t, `SELECT count(*) FROM grades WHERE school_id = $1 AND component_id = $2 AND score IS NOT NULL`, f.schoolA, g.uts); n != 500 {
		t.Errorf("UTS grades = %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM grades WHERE school_id = $1 AND component_id = $2 AND score = 99.5`, f.schoolA, g.uas); n != 5 {
		t.Errorf("UAS 99,5 = %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, f.schoolA, grades.ActionGradeCreate); n != 1000 {
		t.Errorf("grade.create audits = %d", n)
	}
}

// Komponen dinonaktifkan setelah validasi: baris gagal, bukan job gagal.
func TestGradeImport_StateChangedBeforeRun(t *testing.T) {
	f := newUploadFixture(t)
	g := f.setupGrades(t)
	f.enroll(t, "001", "002")
	batchID := f.gradeBatch(t, "", []any{"001", "MTK", "UTS", "80"}, []any{"002", "MTK", "UAS", "75"})
	if _, err := f.tx.Exec(context.Background(), `UPDATE grade_components SET is_active = false WHERE id = $1`, g.uas); err != nil {
		t.Fatal(err)
	}
	f.runBatch(t, batchID)
	b := f.batch(t, batchID)
	if b.Status != StatusDone || b.CreatedCount != 1 || b.FailedCount != 1 {
		t.Fatalf("batch = %+v", b)
	}
	failed, err := NewService(f.tx, nil).FailedRows(context.Background(), f.schoolA, batchID, 10)
	if err != nil || len(failed) != 1 || !strings.Contains(failed[0].Errors[0].Message, "dinonaktifkan") {
		t.Errorf("failed rows = %+v, %v", failed, err)
	}
}

// Satu siswa keluar kelas setelah validasi: hanya barisnya yang gagal,
// baris lain dalam kelas dan komponen yang sama tetap tersimpan.
func TestGradeImport_GroupFallsBackPerRow(t *testing.T) {
	f := newUploadFixture(t)
	g := f.setupGrades(t)
	f.enroll(t, "001", "002", "003")
	batchID := f.gradeBatch(t, "", []any{"001", "MTK", "UTS", "80"}, []any{"002", "MTK", "UTS", "70"}, []any{"003", "MTK", "UTS", "60"})
	if _, err := f.tx.Exec(context.Background(), `DELETE FROM enrollments e USING students st
		WHERE st.id = e.student_id AND st.school_id = $1 AND st.nis = '002'`, f.schoolA); err != nil {
		t.Fatal(err)
	}
	f.runBatch(t, batchID)
	b := f.batch(t, batchID)
	if b.Status != StatusDone || b.CreatedCount != 2 || b.FailedCount != 1 {
		t.Fatalf("batch = %+v", b)
	}
	if f.score(t, "001", g.uts) != "80.00" || f.score(t, "003", g.uts) != "60.00" {
		t.Error("other rows not saved")
	}
	failed, _ := NewService(f.tx, nil).FailedRows(context.Background(), f.schoolA, batchID, 10)
	if len(failed) != 1 || failed[0].Line != 3 || !strings.Contains(failed[0].Errors[0].Message, "NIS 002") {
		t.Errorf("failed rows = %+v", failed)
	}
}

func TestGradeImport_Access(t *testing.T) {
	f := newUploadFixture(t)
	f.setupGrades(t)
	data := buildXLSX(t, [][]any{gradeHeader(), {"001", "MTK", "UTS", "80"}})
	for _, role := range []string{auth.RoleGuru, auth.RoleBendahara} {
		if rec := f.uploadGrades(t, "", data, role); rec.Code != http.StatusForbidden {
			t.Errorf("%s upload: %d", role, rec.Code)
		}
		if rec := f.do(httptest.NewRequest(http.MethodGet, gradesPath, nil), f.schoolA, f.adminA, role); rec.Code != http.StatusForbidden {
			t.Errorf("%s page: %d", role, rec.Code)
		}
	}
	// Mapping milik sekolah lain tidak bisa dipakai.
	other, err := NewMappingService(f.tx).Create(context.Background(), school.Actor{SchoolID: f.schoolB, UserID: f.adminB}, "B", TargetGrades,
		ColumnMap{"NIS": "NIS", "Kode mapel": "Kode mapel", "Nama komponen": "Nama komponen", "Nilai": "Nilai"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec := f.uploadGrades(t, other.ID, data); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Mapping tidak ditemukan") {
		t.Errorf("other school's mapping: %d", rec.Code)
	}
	if n := f.count(t, `SELECT count(*) FROM import_batches WHERE school_id = $1`, f.schoolA); n != 0 {
		t.Errorf("batches = %d", n)
	}
	// Judul kolom file tidak cocok: pesan jelas, tanpa batch.
	bad := buildXLSX(t, [][]any{{"Nama", "Nilai"}, {"Ani", "80"}})
	if rec := f.uploadGrades(t, "", bad); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad header: %d", rec.Code)
	}
}
