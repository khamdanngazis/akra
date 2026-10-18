package importer

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/xuri/excelize/v2"

	"akra/internal/auth"
	"akra/internal/platform/render"
	"akra/web"
)

func openTemplate(t *testing.T, data []byte) *excelize.File {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestStudentTemplate_HeaderAndExample(t *testing.T) {
	data, err := StudentTemplate()
	if err != nil {
		t.Fatal(err)
	}
	f := openTemplate(t, data)
	rows, err := f.GetRows(TemplateSheet)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatalf("rows = %d, want header and example", len(rows))
	}
	want := []string{"NIS", "Nama lengkap", "Tanggal lahir", "Jenis kelamin", "Kelas", "Nama wali", "Telepon wali", "Email wali"}
	if strings.Join(rows[0], "|") != strings.Join(want, "|") {
		t.Errorf("header = %q, want %q", rows[0], want)
	}
	if len(rows[1]) != len(want)+1 || rows[1][ColNIS] != "007" || rows[1][len(want)] != ExampleMarker {
		t.Errorf("example row = %q", rows[1])
	}
}

func TestStudentTemplate_NISStaysText(t *testing.T) {
	data, err := StudentTemplate()
	if err != nil {
		t.Fatal(err)
	}
	f := openTemplate(t, data)
	// Mensimulasikan pengguna mengisi baris baru, menyimpan, lalu membuka lagi.
	if err := f.SetCellValue(TemplateSheet, "A3", "007"); err != nil {
		t.Fatal(err)
	}
	for _, cell := range []string{"A2", "A3", "A500"} {
		styleID, err := f.GetCellStyle(TemplateSheet, cell)
		if err != nil {
			t.Fatal(err)
		}
		st, err := f.GetStyle(styleID)
		if err != nil {
			t.Fatal(err)
		}
		if st.NumFmt != numFmtText {
			t.Errorf("%s number format = %d, want text (%d)", cell, st.NumFmt, numFmtText)
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	reopened := openTemplate(t, buf.Bytes())
	got, err := reopened.GetCellValue(TemplateSheet, "A3")
	if err != nil || got != "007" {
		t.Errorf("A3 = %q, %v; want 007", got, err)
	}
}

func TestStudentTemplate_GenderDropdown(t *testing.T) {
	data, err := StudentTemplate()
	if err != nil {
		t.Fatal(err)
	}
	f := openTemplate(t, data)
	dvs, err := f.GetDataValidations(TemplateSheet)
	if err != nil {
		t.Fatal(err)
	}
	if len(dvs) != 1 {
		t.Fatalf("data validations = %d, want 1", len(dvs))
	}
	dv := dvs[0]
	if dv.Type != "list" || !strings.HasPrefix(dv.Sqref, "D2:D") || !strings.Contains(dv.Formula1, "L,P") {
		t.Errorf("gender validation = type %q, sqref %q, formula %q", dv.Type, dv.Sqref, dv.Formula1)
	}
}

func newRouter(t *testing.T) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, static, err := web.Load("")
	if err != nil {
		t.Fatal(err)
	}
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	NewHandler(rd, logger).Routes(r)
	return r
}

func get(h http.Handler, path string, roles ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(auth.ContextWithIdentity(context.Background(), auth.Identity{UserID: "u1", SchoolID: "s1", Roles: roles}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandler_Access(t *testing.T) {
	h := newRouter(t)
	for _, path := range []string{"/impor/siswa", "/impor/siswa/template.xlsx"} {
		for _, role := range []string{auth.RoleGuru, auth.RoleBendahara, auth.RoleKepalaSekolah, auth.RoleOrangTua} {
			if rec := get(h, path, role); rec.Code != http.StatusForbidden {
				t.Errorf("%s as %s: status = %d, want 403", path, role, rec.Code)
			}
		}
	}
}

func TestHandler_IndexAndDownload(t *testing.T) {
	h := newRouter(t)
	rec := get(h, "/impor/siswa", auth.RoleAdminTU)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/impor/siswa/template.xlsx") || !strings.Contains(rec.Body.String(), "Email wali") {
		t.Errorf("index: status = %d", rec.Code)
	}
	rec = get(h, "/impor/siswa/template.xlsx", auth.RoleAdminTU)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), templateFilename) {
		t.Fatalf("download: status = %d, disposition %q", rec.Code, rec.Header().Get("Content-Disposition"))
	}
	f := openTemplate(t, rec.Body.Bytes())
	if v, _ := f.GetCellValue(TemplateSheet, "A1"); v != "NIS" {
		t.Errorf("A1 = %q", v)
	}
}
