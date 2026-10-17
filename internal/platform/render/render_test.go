package render

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func goodFS() fstest.MapFS {
	return fstest.MapFS{
		"layout.html":         {Data: []byte(`{{define "layout"}}<title>{{block "title" .}}AKRA{{end}}</title>{{template "content" .}}{{end}}`)},
		"partials/flash.html": {Data: []byte(`{{define "flash"}}<p class="flash">{{.}}</p>{{end}}`)},
		"home.html":           {Data: []byte(`{{define "title"}}Beranda{{end}}{{define "content"}}Halo {{.Name}}{{template "flash" "hi"}}{{end}}`)},
		"auth/login.html":     {Data: []byte(`{{define "content"}}<form>{{csrfField}}</form>{{end}}`)},
	}
}

func TestNew_ParsesPages(t *testing.T) {
	rd, err := New(goodFS(), discard)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got, want := rd.Pages(), []string{"auth/login", "home"}; !slices.Equal(got, want) {
		t.Errorf("Pages() = %v, want %v", got, want)
	}
}

func TestNew_ParseErrorNamesFile(t *testing.T) {
	fsys := goodFS()
	fsys["broken.html"] = &fstest.MapFile{Data: []byte(`{{define "content"}}{{if}}{{end}}`)}

	_, err := New(fsys, discard)
	if err == nil {
		t.Fatal("New() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "broken.html") {
		t.Errorf("error does not name the broken file: %v", err)
	}
}

func TestNew_MissingLayout(t *testing.T) {
	fsys := goodFS()
	delete(fsys, "layout.html")
	if _, err := New(fsys, discard); err == nil {
		t.Error("New() without layout.html: error = nil")
	}
}

func TestRender(t *testing.T) {
	rd, err := New(goodFS(), discard)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rd.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusTeapot, "home", map[string]string{"Name": "<Budi>"})

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<title>Beranda</title>") || !strings.Contains(body, "Halo &lt;Budi&gt;") {
		t.Errorf("body = %q", body)
	}
	if !strings.Contains(body, `<p class="flash">hi</p>`) {
		t.Errorf("partial not rendered: %q", body)
	}
}

func TestRender_ExecErrorReturns500(t *testing.T) {
	fsys := goodFS()
	fsys["bad.html"] = &fstest.MapFile{Data: []byte(`{{define "content"}}{{.Missing.Field}}{{end}}`)}
	rd, err := New(fsys, discard)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rd.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, "bad", struct{}{})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<title>") {
		t.Error("partial page output was written before the error")
	}
}

func TestRender_UnknownPage(t *testing.T) {
	rd, _ := New(goodFS(), discard)
	rec := httptest.NewRecorder()
	rd.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, "nope", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
