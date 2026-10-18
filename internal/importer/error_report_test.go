package importer

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"akra/internal/auth"
)

func (f *uploadFixture) report(t *testing.T, batchID string) *excelize.File {
	t.Helper()
	rec := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID+"/kesalahan.xlsx", nil), f.schoolA, f.adminA, auth.RoleAdminTU)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "kesalahan-impor-"+batchID[:8]+".xlsx") {
		t.Fatalf("report: status = %d, disposition %q", rec.Code, rec.Header().Get("Content-Disposition"))
	}
	x, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	return x
}

func TestErrorReport_200Rows(t *testing.T) {
	f := newUploadFixture(t)
	data, err := os.ReadFile(testdata200)
	if err != nil {
		t.Fatal(err)
	}
	rec := f.do(uploadRequest(t, "impor-200.xlsx", data), f.schoolA, f.adminA, auth.RoleAdminTU)
	batchID := strings.TrimPrefix(rec.Header().Get("Location"), "/impor/batch/")

	x := f.report(t, batchID)
	rows, err := x.GetRows(ErrorReportSheet)
	if err != nil {
		t.Fatal(err)
	}
	wantHeader := append(append([]string{"Baris asli"}, columnHeaders()...), "Kesalahan")
	if strings.Join(rows[0], "|") != strings.Join(wantHeader, "|") {
		t.Errorf("header = %q", rows[0])
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want header + 3", len(rows))
	}
	for i, want := range []struct{ line, nis, msg string }{
		{"51", "20260050", "Kelas 1X tidak ditemukan di tahun ajaran aktif (2026/2027)."},
		{"121", "20260120", `Tanggal lahir "2019-13-01" tidak sesuai format`},
		{"181", "20260010", "NIS 20260010 sudah dipakai di baris 11 file ini."},
	} {
		r := rows[i+1]
		if r[0] != want.line || r[1] != want.nis || !strings.Contains(r[len(r)-1], want.msg) {
			t.Errorf("row %d = %q", i, r)
		}
		// Pesan tidak berisi istilah teknis (nama kolom database, jenis kesalahan).
		for _, tech := range []string{"_", "null", "nis_text", "kind"} {
			if strings.Contains(r[len(r)-1], tech) {
				t.Errorf("row %d message contains %q: %s", i, tech, r[len(r)-1])
			}
		}
	}

	// Ringkasan per jenis sama dengan jumlah baris error per jenis.
	sum, err := NewService(f.tx, nil).ErrorSummary(context.Background(), f.schoolA, batchID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, k := range sum {
		got[k.Kind] = k.Rows
	}
	if len(got) != 3 || got[KindClassNotFound] != 1 || got[KindBirthDate] != 1 || got[KindNISDuplicate] != 1 {
		t.Errorf("summary = %+v", sum)
	}
	page := f.do(httptest.NewRequest(http.MethodGet, "/impor/batch/"+batchID, nil), f.schoolA, f.adminA, auth.RoleAdminTU).Body.String()
	for _, s := range []string{"Ringkasan kesalahan", "<strong>1</strong> kelas tidak ditemukan", "/impor/batch/" + batchID + "/kesalahan.xlsx"} {
		if !strings.Contains(page, s) {
			t.Errorf("batch page misses %q", s)
		}
	}
}

// Satu baris dengan beberapa kesalahan dihitung di setiap jenisnya.
func TestErrorReport_SummaryCountsRowsPerKind(t *testing.T) {
	f := newUploadFixture(t)
	r1 := student("", "")      // NIS dan nama kosong
	r2 := student("", "Budi")  // NIS kosong
	r3 := student("003", "Ci") // kelas tidak ada
	r3[ColClass] = "9Z"
	batchID := f.uploadAndValidate(t, r1, r2, r3)
	sum, err := NewService(f.tx, nil).ErrorSummary(context.Background(), f.schoolA, batchID)
	if err != nil {
		t.Fatal(err)
	}
	want := []KindCount{{KindNISEmpty, "NIS kosong", 2}, {KindClassNotFound, "kelas tidak ditemukan", 1}, {KindNameEmpty, "nama kosong", 1}}
	if len(sum) != len(want) {
		t.Fatalf("summary = %+v", sum)
	}
	for i := range want {
		if sum[i] != want[i] {
			t.Errorf("summary[%d] = %+v, want %+v", i, sum[i], want[i])
		}
	}
	var direct int
	if err := f.tx.QueryRow(context.Background(), `SELECT count(*) FROM import_rows WHERE batch_id = $1 AND errors @> '[{"kind": "nis_kosong"}]'`, batchID).Scan(&direct); err != nil || direct != 2 {
		t.Errorf("direct nis_kosong rows = %d, %v", direct, err)
	}
	x := f.report(t, batchID)
	rows, _ := x.GetRows(ErrorReportSheet)
	if len(rows) != 4 || rows[1][len(rows[1])-1] != "NIS wajib diisi.\nNama lengkap wajib diisi." {
		t.Errorf("report rows = %q", rows)
	}
}

// File laporan yang sudah diperbaiki bisa diunggah ulang: kolomnya sama
// dengan template, kolom "Baris asli" dan "Kesalahan" diabaikan.
func TestErrorReport_ReusableAsInput(t *testing.T) {
	f := newUploadFixture(t)
	bad1 := student("007", "Ani")
	bad1[ColClass] = "1X"
	bad2 := student("008", "=1+1") // isi berawalan "=" tetap teks
	bad2[ColBirthDate] = "2019-02-30"
	batchID := f.uploadAndValidate(t, student("001", "Ok"), bad1, bad2)

	x := f.report(t, batchID)
	if formula, _ := x.GetCellFormula(ErrorReportSheet, "C3"); formula != "" {
		t.Errorf("value became a formula: %q", formula)
	}
	if v, _ := x.GetCellValue(ErrorReportSheet, "B2"); v != "007" {
		t.Errorf("NIS = %q", v)
	}
	// Perbaiki di Excel.
	_ = x.SetCellStr(ErrorReportSheet, "F2", "1A")
	_ = x.SetCellStr(ErrorReportSheet, "D3", "2019-02-28")
	buf, err := x.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseFile(bytes.NewReader(buf.Bytes()))
	if err != nil || len(parsed.Data) != 2 || parsed.Data[0].Values[ColNIS] != "007" || parsed.Data[0].Values[ColClass] != "1A" || parsed.Data[1].Values[ColFullName] != "=1+1" {
		t.Fatalf("parsed = %+v, %v", parsed.Data, err)
	}
	rec := f.do(uploadRequest(t, "perbaikan.xlsx", buf.Bytes()), f.schoolA, f.adminA, auth.RoleAdminTU)
	b := f.batch(t, strings.TrimPrefix(rec.Header().Get("Location"), "/impor/batch/"))
	if b.ValidRows != 2 || b.ErrorRows != 0 {
		t.Errorf("re-upload batch = %+v", b)
	}
}

// Baris yang lolos validasi tetapi gagal disimpan (T55) ikut di laporan.
func TestErrorReport_IncludesSaveFailures(t *testing.T) {
	f := newUploadFixture(t)
	r := student("002", "Budi")
	r[ColClass] = "1B"
	batchID := f.uploadAndValidate(t, student("001", "Ani"), r)
	if _, err := f.tx.Exec(context.Background(), `DELETE FROM classes WHERE id = $1`, f.class1B); err != nil {
		t.Fatal(err)
	}
	f.runBatch(t, batchID)
	x := f.report(t, batchID)
	rows, _ := x.GetRows(ErrorReportSheet)
	if len(rows) != 2 || rows[1][0] != "3" || !strings.Contains(rows[1][len(rows[1])-1], "Kelas 1B sudah tidak ada") {
		t.Errorf("rows = %q", rows)
	}
	sum, _ := NewService(f.tx, nil).ErrorSummary(context.Background(), f.schoolA, batchID)
	if len(sum) != 1 || sum[0].Kind != KindSaveFailed || sum[0].Rows != 1 {
		t.Errorf("summary = %+v", sum)
	}
}

func TestErrorReport_Access(t *testing.T) {
	f := newUploadFixture(t)
	bad := student("001", "Ani")
	bad[ColClass] = "9Z"
	batchID := f.uploadAndValidate(t, bad)
	path := "/impor/batch/" + batchID + "/kesalahan.xlsx"
	for _, role := range []string{auth.RoleGuru, auth.RoleBendahara, auth.RoleKepalaSekolah, auth.RoleOrangTua} {
		if rec := f.do(httptest.NewRequest(http.MethodGet, path, nil), f.schoolA, f.adminA, role); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d", role, rec.Code)
		}
	}
	if rec := f.do(httptest.NewRequest(http.MethodGet, path, nil), f.schoolB, f.adminB, auth.RoleAdminTU); rec.Code != http.StatusNotFound {
		t.Errorf("other school: status = %d", rec.Code)
	}
	// Batch tanpa kesalahan menghasilkan file dengan header saja.
	ok := f.uploadAndValidate(t, student("002", "Budi"))
	rows, _ := f.report(t, ok).GetRows(ErrorReportSheet)
	if len(rows) != 1 {
		t.Errorf("rows without errors = %d", len(rows))
	}
}
