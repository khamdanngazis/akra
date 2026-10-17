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
	rd, err := New(goodFS(), nil, discard)
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

	_, err := New(fsys, nil, discard)
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
	if _, err := New(fsys, nil, discard); err == nil {
		t.Error("New() without layout.html: error = nil")
	}
}

func TestRender(t *testing.T) {
	rd, err := New(goodFS(), nil, discard)
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
	rd, err := New(fsys, nil, discard)
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
	rd, _ := New(goodFS(), nil, discard)
	rec := httptest.NewRecorder()
	rd.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, "nope", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestAsset(t *testing.T) {
	fsys := goodFS()
	fsys["home.html"] = &fstest.MapFile{Data: []byte(`{{define "content"}}{{asset "css/app.css"}}{{end}}`)}
	static := fstest.MapFS{"css/app.css": {Data: []byte("body{}")}}

	rd, err := New(fsys, static, discard)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rd.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, "home", nil)
	first := rec.Body.String()
	if !strings.Contains(first, "/static/css/app.css?v=") {
		t.Fatalf("body = %q, want versioned asset URL", first)
	}

	static["css/app.css"] = &fstest.MapFile{Data: []byte("body{color:red}")}
	rd, _ = New(fsys, static, discard)
	rec = httptest.NewRecorder()
	rd.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, "home", nil)
	if rec.Body.String() == first {
		t.Error("asset URL did not change after the file content changed")
	}

	fsys["home.html"] = &fstest.MapFile{Data: []byte(`{{define "content"}}{{asset "css/salah.css"}}{{end}}`)}
	rd, _ = New(fsys, static, discard)
	rec = httptest.NewRecorder()
	rd.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, "home", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("unknown asset: status = %d, want 500", rec.Code)
	}
}
