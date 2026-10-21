package importer

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"akra/internal/school"
)

func mappingErr(t *testing.T, err error) map[string]string {
	t.Helper()
	var me *MappingError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want MappingError", err)
	}
	return me.Fields
}

func TestValidateMapping(t *testing.T) {
	header := []string{"Nomor Induk", "Nama Siswa", "Rombel", "Tgl Lahir", "Agama"}
	ok := ColumnMap{"NIS": "Nomor Induk", "Nama lengkap": " nama siswa ", "Kelas": "Rombel", "Tanggal lahir": "Tgl Lahir", "Jenis kelamin": ""}
	name, m, err := ValidateMapping("  Ekspor   Dapodik ", TargetStudents, ok, header)
	if err != nil || name != "Ekspor Dapodik" || len(m) != 4 || m["Nama lengkap"] != "nama siswa" {
		t.Fatalf("valid: %v %q %v", err, name, m)
	}
	for _, tc := range []struct {
		name   string
		target string
		m      ColumnMap
		header []string
		field  string
		msg    string
	}{
		{"required unmapped", TargetStudents, ColumnMap{"NIS": "Nomor Induk", "Nama lengkap": "Nama Siswa"}, header, "Kelas", "Kolom wajib"},
		{"required empty", TargetStudents, ColumnMap{"NIS": "", "Nama lengkap": "Nama Siswa", "Kelas": "Rombel"}, header, "NIS", "Kolom wajib"},
		{"unknown standard column", TargetStudents, ColumnMap{"NIS": "Nomor Induk", "Nama lengkap": "Nama Siswa", "Kelas": "Rombel", "Agama": "Agama"}, header, "target", "tidak dikenal"},
		{"source not in sample", TargetStudents, ColumnMap{"NIS": "NISN", "Nama lengkap": "Nama Siswa", "Kelas": "Rombel"}, header, "NIS", "tidak ada di file contoh"},
		{"same source twice", TargetStudents, ColumnMap{"NIS": "Nomor Induk", "Nama lengkap": "Nomor Induk", "Kelas": "Rombel"}, header, "", "sudah dipakai"},
		{"fee types not supported", "fee_types", ColumnMap{"Nama": "Nama"}, nil, "target", "Pilih jenis data"},
		{"grades all required", TargetGrades, ColumnMap{"NIS": "NIS", "Kode mapel": "Mapel", "Nama komponen": "Komponen"}, nil, "Nilai", "Kolom wajib"},
	} {
		_, _, err := ValidateMapping("Uji", tc.target, tc.m, tc.header)
		fields := mappingErr(t, err)
		found := false
		for k, v := range fields {
			if (tc.field == "" || k == tc.field) && strings.Contains(v, tc.msg) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: %v", tc.name, fields)
		}
	}
	if _, _, err := ValidateMapping(" ", TargetStudents, ok, header); mappingErr(t, err)["name"] == "" {
		t.Error("blank name accepted")
	}
}

// Dua format file berbeda menghasilkan baris yang sama setelah dipetakan.
func TestParseMapped_TwoFormatsSameRows(t *testing.T) {
	// Format A: template baku AKRA.
	standard := buildXLSX(t, [][]any{header(),
		{"007", "Siti Aminah", "2019-05-02", "P", "1A", "Aminah", "081234567890", "aminah@contoh.id"},
		{"008", "Budi Santoso", "", "L", "1B", "", "", ""},
	})
	want, err := ParseFile(bytes.NewReader(standard))
	if err != nil {
		t.Fatal(err)
	}
	// Format B: ekspor aplikasi lain. Judul berbeda, urutan berbeda, kolom
	// tambahan, baris judul laporan di atas header, tanggal lahir berupa
	// tanggal Excel (angka seri 43587 = 2019-05-02).
	other := buildXLSX(t, [][]any{
		{"DAFTAR PESERTA DIDIK"},
		{"No", "Rombel", "Nama Peserta Didik", "NIPD", "JK", "Tanggal Lahir", "Agama", "Nama Ibu", "HP Ortu", "Email Ortu"},
		{1, "1A", "Siti Aminah", "007", "P", 43587, "Islam", "Aminah", "081234567890", "aminah@contoh.id"},
		{2, "1B", "Budi Santoso", "008", "L", "", "Islam", "", "", ""},
	}, 1, 3, 4, 5)
	mp := Mapping{Name: "Ekspor rombel", Target: TargetStudents, Columns: ColumnMap{
		"NIS": "NIPD", "Nama lengkap": "Nama Peserta Didik", "Tanggal lahir": "Tanggal Lahir", "Jenis kelamin": "JK", "Kelas": "Rombel",
		"Nama wali": "Nama Ibu", "Telepon wali": "HP Ortu", "Email wali": "Email Ortu"}}
	// Baris judul laporan di atas header dilewati; datanya tetap sama.
	withTitle, err := ParseMapped(bytes.NewReader(other), mp)
	if err != nil {
		t.Fatalf("file with title row: %v", err)
	}
	if len(withTitle.Data) != 2 || withTitle.Data[0].Line != 4 {
		t.Fatalf("title row file: %+v", withTitle.Data)
	}
	other = buildXLSX(t, [][]any{
		{"No", "Rombel", "Nama Peserta Didik", "NIPD", "JK", "Tanggal Lahir", "Agama", "Nama Ibu", "HP Ortu", "Email Ortu"},
		{1, "1A", "Siti Aminah", "007", "P", 43587, "Islam", "Aminah", "081234567890", "aminah@contoh.id"},
		{2, "1B", "Budi Santoso", "008", "L", "", "Islam", "", "", ""},
	})
	mapped, err := ParseMapped(bytes.NewReader(other), mp)
	if err != nil {
		t.Fatal(err)
	}
	got, err := mapped.StudentRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != len(want.Data) {
		t.Fatalf("rows %d, want %d", len(got.Data), len(want.Data))
	}
	for i := range want.Data {
		if got.Data[i].Values != want.Data[i].Values {
			t.Errorf("row %d:\n got %q\nwant %q", i, got.Data[i].Values, want.Data[i].Values)
		}
	}
	// Mapping yang merujuk kolom tidak ada di file: ditolak dengan pesan jelas.
	missing := mp
	missing.Columns = ColumnMap{"NIS": "NISN", "Nama lengkap": "Nama Peserta Didik", "Kelas": "Kelas"}
	_, err = ParseMapped(bytes.NewReader(other), missing)
	var se *MappingSourceError
	if !errors.As(err, &se) || !strings.Contains(se.Message(), `"NISN" (NIS) dan "Kelas" (Kelas)`) || !strings.Contains(se.Message(), "Ekspor rombel") ||
		parseMessage(err) != se.Message() {
		t.Errorf("missing source: %v", err)
	}
	// Mapping tanpa kolom opsional: kolom itu kosong.
	mp.Columns = ColumnMap{"NIS": "NIPD", "Nama lengkap": "Nama Peserta Didik", "Kelas": "Rombel"}
	mapped, _ = ParseMapped(bytes.NewReader(other), mp)
	if rows, _ := mapped.StudentRows(); rows.Data[0].Values[ColGender] != "" || rows.Data[0].Values[ColNIS] != "007" {
		t.Errorf("partial mapping: %q", rows.Data[0].Values)
	}
}

func TestParseMapped_Grades(t *testing.T) {
	a := buildXLSX(t, [][]any{{"NIS", "Kode mapel", "Nama komponen", "Nilai"}, {"007", "MTK", "UTS", "85,5"}, {"008", "MTK", "UTS", ""}})
	b := buildXLSX(t, [][]any{{"Mapel", "No Induk", "Jenis Penilaian", "Skor", "Keterangan"}, {"MTK", "007", "UTS", "85,5", "-"}, {"MTK", "008", "UTS", "", "susulan"}})
	ma := Mapping{Name: "Baku", Target: TargetGrades, Columns: ColumnMap{"NIS": "NIS", "Kode mapel": "Kode mapel", "Nama komponen": "Nama komponen", "Nilai": "Nilai"}}
	mb := Mapping{Name: "Rapor lama", Target: TargetGrades, Columns: ColumnMap{"NIS": "No Induk", "Kode mapel": "Mapel", "Nama komponen": "Jenis Penilaian", "Nilai": "Skor"}}
	ra, err := ParseMapped(bytes.NewReader(a), ma)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := ParseMapped(bytes.NewReader(b), mb)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ra.Data, rb.Data) || len(ra.Data) != 2 || ra.Data[0].Values[GradeColScore] != "85,5" {
		t.Errorf("grades:\n a %v\n b %v", ra.Data, rb.Data)
	}
	if _, err := ra.StudentRows(); err == nil {
		t.Error("grade rows converted to student rows")
	}
	// Kolom sumber ada dua kali di file: ditolak.
	dup := buildXLSX(t, [][]any{{"No Induk", "Mapel", "Jenis Penilaian", "Skor", "Skor"}, {"007", "MTK", "UTS", "80", "90"}})
	var de *DuplicateColumnError
	if _, err := ParseMapped(bytes.NewReader(dup), mb); !errors.As(err, &de) {
		t.Errorf("duplicate source: %v", err)
	}
}

func TestMappingService(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	svc := NewMappingService(f.tx)
	actorA := school.Actor{SchoolID: f.schoolA, UserID: f.adminA}
	m := ColumnMap{"NIS": "NIPD", "Nama lengkap": "Nama", "Kelas": "Rombel"}
	saved, err := svc.Create(ctx, actorA, "Dapodik", TargetStudents, m, []string{"NIPD", "Nama", "Rombel"})
	if err != nil || saved.Name != "Dapodik" || saved.CreatorName != "Admin A" || !reflect.DeepEqual(saved.Columns, m) {
		t.Fatalf("create: %v %+v", err, saved)
	}
	if _, err := svc.Create(ctx, actorA, "Dapodik", TargetStudents, m, nil); !strings.Contains(mappingErr(t, err)["name"], "sudah dipakai") {
		t.Error("duplicate name accepted")
	}
	if _, err := svc.Create(ctx, actorA, "Kurang", TargetStudents, ColumnMap{"NIS": "NIPD"}, nil); mappingErr(t, err)["Kelas"] == "" {
		t.Error("missing required column accepted")
	}
	// Sekolah lain: nama sama boleh, tetapi tidak saling terlihat.
	other, err := svc.Create(ctx, school.Actor{SchoolID: f.schoolB, UserID: f.adminB}, "Dapodik", TargetStudents, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	listA, _ := svc.List(ctx, f.schoolA)
	listB, _ := svc.List(ctx, f.schoolB)
	if len(listA) != 1 || listA[0].ID != saved.ID || len(listB) != 1 || listB[0].ID != other.ID {
		t.Errorf("lists: A %v, B %v", listA, listB)
	}
	if _, err := svc.Get(ctx, f.schoolA, other.ID); !errors.Is(err, ErrMappingNotFound) {
		t.Errorf("other school's mapping: %v", err)
	}
	var n int
	_ = f.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, f.schoolA, ActionMappingCreate).Scan(&n)
	if n != 1 {
		t.Errorf("audits = %d", n)
	}
}

func multipartRequest(t *testing.T, fields map[string][]string, filename string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, vs := range fields {
		for _, v := range vs {
			_ = mw.WriteField(k, v)
		}
	}
	if filename != "" {
		w, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/impor/mapping", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestMappingHandler(t *testing.T) {
	f := newUploadFixture(t)
	sample := buildXLSX(t, [][]any{{"NIPD", "Nama", "Kelas", "Agama"}, {"007", "Siti", "1A", "Islam"}})

	// Langkah 1: judul kolom file contoh jadi pilihan; "Kelas" terpilih otomatis.
	rec := f.do(multipartRequest(t, map[string][]string{"step": {"contoh"}, "name": {"Dapodik"}, "target": {"students"}}, "contoh.xlsx", sample), f.schoolA, f.adminA, "admin_tu")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `<option value="NIPD">NIPD</option>`) || !strings.Contains(body, `<option value="Kelas" selected>Kelas</option>`) ||
		!strings.Contains(body, `name="header" value="Agama"`) || strings.Contains(body, "Siti") {
		t.Fatalf("step 1: %d", rec.Code)
	}
	// Langkah 2 dengan kolom wajib kosong: 422.
	save := map[string][]string{"step": {"simpan"}, "name": {"Dapodik"}, "target": {"students"}, "header": {"NIPD", "Nama", "Kelas", "Agama"},
		"map_0": {"NIPD"}, "map_1": {"Nama"}}
	if rec := f.do(multipartRequest(t, save, "", nil), f.schoolA, f.adminA, "admin_tu"); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Kolom wajib") {
		t.Errorf("missing required: %d", rec.Code)
	}
	save["map_4"] = []string{"Kelas"}
	if rec := f.do(multipartRequest(t, save, "", nil), f.schoolA, f.adminA, "admin_tu"); rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d", rec.Code)
	}
	// Daftar hanya mapping sekolah sendiri.
	if _, err := NewMappingService(f.tx).Create(context.Background(), school.Actor{SchoolID: f.schoolB, UserID: f.adminB}, "Milik B", TargetStudents,
		ColumnMap{"NIS": "a", "Nama lengkap": "b", "Kelas": "c"}, nil); err != nil {
		t.Fatal(err)
	}
	page := f.do(httptest.NewRequest(http.MethodGet, "/impor/mapping", nil), f.schoolA, f.adminA, "admin_tu").Body.String()
	if !strings.Contains(page, "Dapodik") || !strings.Contains(page, "NIS ← NIPD") || strings.Contains(page, "Milik B") {
		t.Error("mapping list")
	}
	// Bukan file xlsx: pesan jelas. Peran lain 403.
	if rec := f.do(multipartRequest(t, map[string][]string{"step": {"contoh"}, "name": {"X"}, "target": {"students"}}, "data.csv", []byte("a,b")), f.schoolA, f.adminA, "admin_tu"); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("csv sample: %d", rec.Code)
	}
	for _, role := range []string{"guru", "kepala_sekolah", "bendahara"} {
		if rec := f.do(httptest.NewRequest(http.MethodGet, "/impor/mapping", nil), f.schoolA, f.adminA, role); rec.Code != http.StatusForbidden {
			t.Errorf("GET as %s: %d", role, rec.Code)
		}
	}
}
