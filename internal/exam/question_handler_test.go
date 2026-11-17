package exam

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func (e *env) upload(path, u, role, name string, data []byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Test-User", u+"|"+role)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, x%h, color.RGBA{200, 0, 0, 255})
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestEditorFlow(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	teacher := e.a.Teacher.ID
	bankPath := "/ujian/bank/" + e.bank.ID

	for _, k := range Kinds {
		if rec := e.getAs(bankPath+"/soal/baru?jenis="+string(k), teacher, "guru"); rec.Code != http.StatusOK {
			t.Fatalf("new %s form = %d\n%s", k, rec.Code, rec.Body.String())
		}
	}

	// Unggah gambar: PNG besar dikecilkan server menjadi sisi 1600 px.
	rec := e.upload(bankPath+"/gambar", teacher, "guru", "segitiga.png", pngBytes(2400, 1200))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body.String())
	}
	var up struct {
		ID            string
		URL           string
		Width, Height int
	}
	json.Unmarshal(rec.Body.Bytes(), &up)
	if up.Width != 1600 || up.Height != 800 {
		t.Errorf("uploaded size = %dx%d, want 1600x800", up.Width, up.Height)
	}
	if rec := e.upload(bankPath+"/gambar", teacher, "guru", "x.txt", []byte("bukan gambar")); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("text upload = %d", rec.Code)
	}
	if rec := e.getAs("/ujian/aset/"+up.ID, e.teacher2, "guru"); rec.Code != http.StatusFound {
		t.Errorf("asset redirect = %d", rec.Code)
	}

	// Simpan PG berumus dan bergambar, lalu "simpan & tambah berikutnya".
	form := url.Values{"kind": {"pg"}, "body": {`<p>Luas segitiga <img data-asset="` + up.ID + `" alt="Segitiga"> dengan \(a=\frac{1}{2}\) adalah …</p>`},
		"choice_0": {"6"}, "choice_1": {"12"}, "choice_2": {"24"}, "correct": {"1"}, "difficulty": {"sedang"}, "points": {"2"}, "next": {"1"}}
	rec = e.post(bankPath+"/soal", teacher, "guru", form)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "jenis=pg") || !strings.Contains(rec.Header().Get("Location"), "kesulitan=sedang") {
		t.Fatalf("create = %d %s\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	var qid string
	e.tx.QueryRow(ctx, `SELECT id FROM exam_questions WHERE bank_id = $1 AND created_by = $2`, e.bank.ID, teacher).Scan(&qid)
	q, err := GetQuestion(ctx, e.tx, e.a.ID, qid)
	if err != nil || q.Key.Correct[0] != "b" || !strings.Contains(q.BodyHTML, "katex") {
		t.Fatalf("saved question = %+v, %v", q, err)
	}

	// Validasi: tanpa kunci ditolak dengan pesan.
	bad := url.Values{"kind": {"pg"}, "body": {"x"}, "choice_0": {"a"}, "choice_1": {"b"}}
	if rec := e.post(bankPath+"/soal", teacher, "guru", bad); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Tandai satu opsi yang benar") {
		t.Errorf("invalid = %d", rec.Code)
	}
	// Gambar sekolah lain ditolak.
	var otherAsset string
	e.tx.QueryRow(ctx, `SELECT id FROM exam_assets WHERE school_id = $1`, e.b.ID).Scan(&otherAsset)
	alien := url.Values{"kind": {"esai"}, "body": {`<p><img data-asset="` + otherAsset + `" alt="x"></p>`}}
	if rec := e.post(bankPath+"/soal", teacher, "guru", alien); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("other school's asset = %d", rec.Code)
	}

	// Lihat, ubah (versi naik), dan pratinjau.
	if rec := e.getAs("/ujian/soal/"+qid, e.teacher2, "guru"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/ujian/aset/"+up.ID) || strings.Contains(rec.Body.String(), "/ujian/soal/"+qid+"/ubah") {
		t.Errorf("view by other teacher = %d (edit link must be hidden)", rec.Code)
	}
	if rec := e.getAs("/ujian/soal/"+qid+"/ubah", teacher, "guru"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-tex="a=\frac{1}{2}"`) {
		t.Errorf("edit form = %d", rec.Code)
	}
	if rec := e.getAs("/ujian/soal/"+qid+"/ubah", e.teacher2, "guru"); rec.Code != http.StatusForbidden {
		t.Errorf("edit by other teacher = %d", rec.Code)
	}
	form.Set("choice_2", "36")
	form.Del("next")
	if rec := e.post("/ujian/soal/"+qid, teacher, "guru", form); rec.Code != http.StatusSeeOther {
		t.Errorf("update = %d %s", rec.Code, rec.Body.String())
	}
	if q, _ := GetQuestion(ctx, e.tx, e.a.ID, qid); q.Version != 2 {
		t.Errorf("version = %d, want 2", q.Version)
	}
	rec = e.post("/ujian/pratinjau", teacher, "guru", url.Values{"kind": {"pg"}, "body": {`Hitung \(\sqrt{144}\)`}, "choice_0": {"12"}, "choice_1": {"14"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "katex") || strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("preview = %d", rec.Code)
	}

	// Draf otomatis: tersimpan, ditawarkan saat membuka editor, dibuang.
	rec = e.post("/ujian/draf", teacher, "guru", url.Values{"bank_id": {e.bank.ID}, "kind": {"isian"}, "body": {"Draf belum selesai"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("draft = %d %s", rec.Code, rec.Body.String())
	}
	if body := e.getAs(bankPath+"/soal/baru", teacher, "guru").Body.String(); !strings.Contains(body, "Pulihkan draf") {
		t.Error("draft banner missing")
	}
	if body := e.getAs(bankPath+"/soal/baru?draf=1", teacher, "guru").Body.String(); !strings.Contains(body, "Draf belum selesai") {
		t.Error("restored draft content missing")
	}
	if rec := e.post("/ujian/draf", e.teacher2, "guru", url.Values{"bank_id": {e.bank.ID}, "question_id": {qid}, "body": {"x"}}); rec.Code != http.StatusNotFound {
		t.Errorf("draft for someone else's question = %d", rec.Code)
	}
	e.post("/ujian/draf/buang", teacher, "guru", url.Values{"bank_id": {e.bank.ID}})
	if body := e.getAs(bankPath+"/soal/baru", teacher, "guru").Body.String(); strings.Contains(body, "Pulihkan draf") {
		t.Error("draft not discarded")
	}
}

func TestStimulusPages(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	teacher := e.a.Teacher.ID
	bankPath := "/ujian/bank/" + e.bank.ID
	if rec := e.getAs(bankPath+"/stimulus/baru", teacher, "guru"); rec.Code != http.StatusOK {
		t.Fatalf("new stimulus = %d\n%s", rec.Code, rec.Body.String())
	}
	rec := e.post(bankPath+"/stimulus", teacher, "guru", url.Values{"title": {"Teks untuk soal 1–5"}, "numbered": {"on"},
		"body": {`<p dir="rtl">ذَهَبَ أَحْمَدُ إِلَى الْمَدْرَسَةِ</p><p>Baris kedua</p>`}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create stimulus = %d %s", rec.Code, rec.Body.String())
	}
	stimPath := rec.Header().Get("Location")
	sid := strings.TrimPrefix(stimPath, "/ujian/stimulus/")
	var ids []string
	for i := 0; i < 5; i++ {
		rec := e.post(bankPath+"/soal", teacher, "guru", url.Values{"kind": {"esai"}, "body": {"Soal " + string(rune('1'+i))}, "stimulus_id": {sid}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("question %d = %d %s", i, rec.Code, rec.Body.String())
		}
	}
	qs, _ := StimulusQuestions(ctx, e.tx, e.a.ID, sid)
	for i := len(qs) - 1; i >= 0; i-- {
		ids = append(ids, qs[i].ID)
	}
	if len(ids) != 5 {
		t.Fatalf("stimulus questions = %d", len(ids))
	}
	body := e.getAs(stimPath, teacher, "guru").Body.String()
	if !strings.Contains(body, "numbered") || !strings.Contains(body, `<p dir="rtl">ذَهَبَ`) || strings.Count(body, `class="exam-q has-stimulus"`) != 0 {
		t.Error("stimulus page: numbering, Arabic, or preview layout wrong")
	}
	if rec := e.post(stimPath+"/urutan", teacher, "guru", url.Values{"order": ids}); rec.Code != http.StatusSeeOther {
		t.Fatalf("reorder = %d", rec.Code)
	}
	if qs, _ := StimulusQuestions(ctx, e.tx, e.a.ID, sid); qs[0].ID != ids[0] {
		t.Error("reorder not saved")
	}
	// Pratinjau soal stimulus memakai tata letak dua kolom.
	if body := e.getAs("/ujian/soal/"+ids[0], teacher, "guru").Body.String(); !strings.Contains(body, `class="exam-q has-stimulus"`) {
		t.Error("question with stimulus not shown with stimulus layout")
	}
	if rec := e.post(stimPath+"/arsip", teacher, "guru", url.Values{}); rec.Code != http.StatusConflict {
		t.Errorf("archive stimulus in use = %d", rec.Code)
	}
}
