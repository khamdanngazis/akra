package importer

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"akra/internal/auth"
	"akra/internal/classes"
)

var updateTestdata = flag.Bool("update", false, "tulis ulang file di testdata")

// testToday adalah "hari ini" untuk uji validasi.
var testToday = time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)

func rowContext(existing ...string) *RowContext {
	rc := &RowContext{Today: testToday, YearName: "2026/2027",
		classes:  map[string]classes.Class{"1a": {ID: "c1a", Name: "1A"}, "1b": {ID: "c1b", Name: "1B"}},
		existing: map[string]bool{}, seen: map[string]int{}}
	for _, n := range existing {
		rc.existing[n] = true
	}
	return rc
}

func row(line int, values ...string) Row {
	r := Row{Line: line}
	copy(r.Values[:], values)
	return r
}

func kinds(errs []RowError) string {
	var k []string
	for _, e := range errs {
		k = append(k, e.Kind)
	}
	return strings.Join(k, ",")
}

func TestValidateRow_Valid(t *testing.T) {
	rc := rowContext("002")
	got, errs := ValidateRow(row(2, " 001 ", "  Siti   Aminah ", "2019-05-02", "p", "1a", "Aminah", "0812-3456-7890", "Aminah@Contoh.ID"), rc)
	if errs != nil {
		t.Fatalf("errors = %+v", errs)
	}
	want := ValidRow{NIS: "001", FullName: "Siti Aminah", BirthDate: "2019-05-02", Gender: "P", ClassID: "c1a", ClassName: "1A",
		GuardianName: "Aminah", GuardianPhone: "+6281234567890", GuardianEmail: "aminah@contoh.id"}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	// Kolom opsional kosong; NIS yang sudah ada ditandai diperbarui.
	got, errs = ValidateRow(row(3, "002", "Budi", "", "", "1B"), rc)
	if errs != nil || !got.Update || got.BirthDate != "" || got.Gender != "" || got.GuardianName != "" {
		t.Errorf("existing nis: %+v, %+v", got, errs)
	}
}

func TestValidateRow_Errors(t *testing.T) {
	cases := []struct {
		name string
		row  Row
		kind string
		msg  string
	}{
		{"empty nis", row(2, "", "Ani", "", "", "1A"), KindNISEmpty, "NIS wajib diisi."},
		{"long nis", row(2, strings.Repeat("1", 31), "Ani", "", "", "1A"), KindNISTooLong, "NIS maksimal 30 karakter."},
		{"empty name", row(2, "001", "  ", "", "", "1A"), KindNameEmpty, "Nama lengkap wajib diisi."},
		{"long name", row(2, "001", strings.Repeat("a", 101), "", "", "1A"), KindNameTooLong, "Nama lengkap maksimal 100 karakter."},
		{"date format", row(2, "001", "Ani", "02/05/2019", "", "1A"), KindBirthDate, `Tanggal lahir "02/05/2019" tidak sesuai format`},
		{"date invalid", row(2, "001", "Ani", "2019-13-01", "", "1A"), KindBirthDate, "tidak sesuai format"},
		{"date future", row(2, "001", "Ani", "2026-10-19", "", "1A"), KindBirthDate, "Tanggal lahir tidak boleh di masa depan."},
		{"date too old", row(2, "001", "Ani", "1949-12-31", "", "1A"), KindBirthDate, "sebelum tahun 1950"},
		{"gender", row(2, "001", "Ani", "", "Laki-laki", "1A"), KindGender, `Jenis kelamin "Laki-laki" tidak dikenal. Isi L atau P.`},
		{"class empty", row(2, "001", "Ani", "", "", ""), KindClassEmpty, "Kelas wajib diisi."},
		{"class missing", row(2, "001", "Ani", "", "", "1X"), KindClassNotFound, "Kelas 1X tidak ditemukan di tahun ajaran aktif (2026/2027)."},
		{"phone", row(2, "001", "Ani", "", "", "1A", "Ibu", "0812-ABC"), KindGuardianPhone, `Telepon wali "0812-ABC" tidak valid.`},
		{"email", row(2, "001", "Ani", "", "", "1A", "Ibu", "", "ibu@"), KindGuardianEmail, `Email wali "ibu@" tidak valid.`},
		{"guardian name", row(2, "001", "Ani", "", "", "1A", "", "08123456789"), KindGuardianName, "Nama wali wajib diisi jika telepon atau email wali diisi."},
	}
	for _, c := range cases {
		_, errs := ValidateRow(c.row, rowContext())
		if len(errs) != 1 || errs[0].Kind != c.kind || !strings.Contains(errs[0].Message, c.msg) {
			t.Errorf("%s: errors = %+v", c.name, errs)
		}
	}
}

// Semua kesalahan satu baris dilaporkan sekaligus, dengan nama kolom.
func TestValidateRow_AllErrorsAtOnce(t *testing.T) {
	_, errs := ValidateRow(row(7, "", "", "kemarin", "X", "9Z"), rowContext())
	if kinds(errs) != "nis_kosong,nama_kosong,tanggal_lahir,jenis_kelamin,kelas_tidak_ada" {
		t.Errorf("kinds = %s", kinds(errs))
	}
	if errs[4].Column != "Kelas" {
		t.Errorf("column = %q", errs[4].Column)
	}
}

func TestValidateRow_DuplicateNIS(t *testing.T) {
	rc := rowContext()
	if _, errs := ValidateRow(row(2, "001", "Ani", "", "", "1A"), rc); errs != nil {
		t.Fatal(errs)
	}
	_, errs := ValidateRow(row(9, "001", "Budi", "", "", "1A"), rc)
	if len(errs) != 1 || errs[0].Kind != KindNISDuplicate || errs[0].Message != "NIS 001 sudah dipakai di baris 2 file ini." {
		t.Errorf("errors = %+v", errs)
	}
	// Baris pertama yang error karena hal lain tetap memesan NIS-nya.
	rc = rowContext()
	ValidateRow(row(2, "005", "", "", "", "1A"), rc)
	if _, errs := ValidateRow(row(3, "005", "Ani", "", "", "1A"), rc); len(errs) != 1 || errs[0].Kind != KindNISDuplicate {
		t.Errorf("duplicate after invalid row: %+v", errs)
	}
}

// fixture200 adalah isi testdata/impor-200.xlsx: 200 siswa di baris 2–201
// dengan 3 baris salah (baris 51, 121, 181).
func fixture200() [][]any {
	data := [][]any{header()}
	for i := 1; i <= 200; i++ {
		class := []string{"1A", "1B"}[i%2]
		r := []any{fmt.Sprintf("2026%04d", i), fmt.Sprintf("Siswa %03d", i), "2019-05-02", "L", class, fmt.Sprintf("Wali %03d", i), "0812345678" + fmt.Sprintf("%02d", i%100), ""}
		switch i {
		case 50:
			r[ColClass] = "1X" // kelas tidak ada
		case 120:
			r[ColBirthDate] = "2019-13-01" // tanggal tidak valid
		case 180:
			r[ColNIS] = "20260010" // NIS sama dengan baris 11
		}
		data = append(data, r)
	}
	return data
}

const testdata200 = "testdata/impor-200.xlsx"

func TestTestdata200UpToDate(t *testing.T) {
	want := buildXLSX(t, fixture200())
	if *updateTestdata {
		if err := os.MkdirAll(filepath.Dir(testdata200), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(testdata200, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(testdata200)
	if err != nil {
		t.Fatalf("%v (jalankan go test -run TestTestdata200UpToDate -update)", err)
	}
	got, err := ParseFile(strings.NewReader(string(data)))
	if err != nil || len(got.Data) != 200 {
		t.Fatalf("parse testdata: %d rows, %v", len(got.Data), err)
	}
}

func TestValidateBatch_200Rows(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	// NIS baris 6 sudah terdaftar: baris itu valid dan ditandai diperbarui.
	if _, err := f.tx.Exec(ctx, `INSERT INTO students (school_id, nis, full_name) VALUES ($1, '20260005', 'Lama')`, f.schoolA); err != nil {
		t.Fatal(err)
	}
	before := f.count(t, `SELECT count(*) FROM students`) + f.count(t, `SELECT count(*) FROM guardians`)*1000 + f.count(t, `SELECT count(*) FROM enrollments`)*1000000

	data, err := os.ReadFile(testdata200)
	if err != nil {
		t.Fatal(err)
	}
	rec := f.do(uploadRequest(t, "impor-200.xlsx", data), f.schoolA, f.adminA, auth.RoleAdminTU)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload: status = %d", rec.Code)
	}
	batchID := strings.TrimPrefix(rec.Header().Get("Location"), "/impor/batch/")
	svc := NewService(f.tx, nil)
	b, err := svc.GetBatch(ctx, f.schoolA, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != StatusValidated || b.TotalRows != 200 || b.ValidRows != 197 || b.ErrorRows != 3 || b.UpdateRows != 1 || b.NewRows() != 196 {
		t.Errorf("batch = %+v", b)
	}
	errRows, err := svc.ErrorRows(ctx, f.schoolA, batchID, 50)
	if err != nil || len(errRows) != 3 {
		t.Fatalf("error rows = %+v, %v", errRows, err)
	}
	for i, want := range []struct {
		line int
		kind string
		msg  string
	}{
		{51, KindClassNotFound, "Kelas 1X tidak ditemukan di tahun ajaran aktif (2026/2027)."},
		{121, KindBirthDate, `Tanggal lahir "2019-13-01" tidak sesuai format`},
		{181, KindNISDuplicate, "NIS 20260010 sudah dipakai di baris 11 file ini."},
	} {
		e := errRows[i]
		if e.Line != want.line || len(e.Errors) != 1 || e.Errors[0].Kind != want.kind || !strings.Contains(e.Errors[0].Message, want.msg) {
			t.Errorf("error row %d = %+v", i, e)
		}
	}
	var action string
	if err := f.tx.QueryRow(ctx, `SELECT action FROM import_rows WHERE batch_id = $1 AND line = 6`, batchID).Scan(&action); err != nil || action != ActionUpdate {
		t.Errorf("line 6 action = %q, %v", action, err)
	}
	var classID string
	if err := f.tx.QueryRow(ctx, `SELECT data->>'class_id' FROM import_rows WHERE batch_id = $1 AND line = 2`, batchID).Scan(&classID); err != nil || classID == "" {
		t.Errorf("line 2 class_id = %q, %v", classID, err)
	}

	after := f.count(t, `SELECT count(*) FROM students`) + f.count(t, `SELECT count(*) FROM guardians`)*1000 + f.count(t, `SELECT count(*) FROM enrollments`)*1000000
	if after != before {
		t.Errorf("validation wrote to students/guardians/enrollments: %d -> %d", before, after)
	}

	page := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID, nil), f.schoolA, f.adminA, auth.RoleAdminTU)
	body := page.Body.String()
	for _, s := range []string{"Baris yang perlu diperbaiki", "Baris 51", "Kelas 1X tidak ditemukan", "Baris 181", "Divalidasi"} {
		if !strings.Contains(body, s) {
			t.Errorf("batch page misses %q", s)
		}
	}
	// Batch yang sudah divalidasi tidak divalidasi ulang.
	if err := svc.ValidateBatch(ctx, f.schoolA, batchID, testToday); err != ErrBatchValidated {
		t.Errorf("revalidate: %v", err)
	}
}

func TestUpload_NoActiveYear(t *testing.T) {
	f := newUploadFixture(t)
	data := buildXLSX(t, [][]any{header(), student("001", "Ani")})
	rec := f.do(uploadRequest(t, "a.xlsx", data), f.schoolB, f.adminB, auth.RoleAdminTU)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Belum ada tahun ajaran aktif") {
		t.Errorf("status = %d", rec.Code)
	}
	if n := f.count(t, `SELECT count(*) FROM import_batches WHERE school_id = $1`, f.schoolB); n != 0 {
		t.Errorf("batch saved without active year: %d", n)
	}
}

// Kelas dicocokkan hanya di tahun ajaran aktif.
func TestValidateBatch_OldYearClassNotMatched(t *testing.T) {
	f := newUploadFixture(t)
	r := student("001", "Ani")
	r[ColClass] = "6A"
	rec := f.do(uploadRequest(t, "a.xlsx", buildXLSX(t, [][]any{header(), r})), f.schoolA, f.adminA, auth.RoleAdminTU)
	batchID := strings.TrimPrefix(rec.Header().Get("Location"), "/impor/batch/")
	b, err := NewService(f.tx, nil).GetBatch(context.Background(), f.schoolA, batchID)
	if err != nil || b.ErrorRows != 1 {
		t.Errorf("batch = %+v, %v", b, err)
	}
}
