package exam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func (e *env) post(path string, u string, role string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-User", u+"|"+role)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *env) getAs(path, u, role string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Test-User", u+"|"+role)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func TestBankPages(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	teacher, other := e.a.Teacher.ID, e.teacher2

	// Daftar bank: kartu bank fixture dan bank buatan uji.
	rec := e.getAs("/ujian/bank/", teacher, "guru")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Matematika 7") {
		t.Fatalf("banks = %d", rec.Code)
	}
	// Buat bank lewat form.
	rec = e.post("/ujian/bank/", teacher, "guru", url.Values{"name": {"IPA 8"}, "subject_id": {e.a.SubjectID}, "grade_level": {"8"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create bank = %d\n%s", rec.Code, rec.Body.String())
	}
	if rec := e.post("/ujian/bank/", teacher, "guru", url.Values{"name": {""}, "subject_id": {e.a.SubjectID}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("empty name = %d", rec.Code)
	}
	// Bank umum tanpa mapel: guru tidak boleh, admin boleh.
	if rec := e.post("/ujian/bank/", teacher, "guru", url.Values{"name": {"TPA"}}); rec.Code != http.StatusForbidden {
		t.Errorf("teacher general bank = %d", rec.Code)
	}
	if rec := e.post("/ujian/bank/", e.a.Admin.ID, "admin_tu", url.Values{"name": {"TPA"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("admin general bank = %d", rec.Code)
	}

	// Detail: soal guru lain tampil tanpa tombol arsip; soal sendiri ada.
	owner := e.staff(teacher, "guru")
	q := sampleQuestion(KindMC)
	q.BodySrc = `<p dir="rtl">مَا مَعْنَى الصَّلَاةُ؟</p>`
	mine, err := CreateQuestion(ctx, e.tx, owner, e.bank.ID, q, now)
	if err != nil {
		t.Fatal(err)
	}
	path := "/ujian/bank/" + e.bank.ID
	body := e.getAs(path, other, "guru").Body.String()
	if !strings.Contains(body, "الصَّلَاةُ") || strings.Contains(body, "/ujian/soal/"+mine.ID+"/arsip") || !strings.Contains(body, "/ujian/soal/"+mine.ID+"/duplikat") {
		t.Error("other teacher view: question missing, archive shown, or duplicate missing")
	}
	if body := e.getAs(path, teacher, "guru").Body.String(); !strings.Contains(body, "/ujian/soal/"+mine.ID+"/arsip") {
		t.Error("owner view: archive button missing")
	}
	if body := e.getAs(path+"?q="+url.QueryEscape("الصلاة"), other, "guru").Body.String(); !strings.Contains(body, "1 soal") {
		t.Error("Arabic search without harakat found nothing")
	}
	// Kepala sekolah membaca, tidak menulis.
	if rec := e.getAs(path, e.a.Principal.ID, "kepala_sekolah"); rec.Code != http.StatusOK {
		t.Errorf("principal detail = %d", rec.Code)
	}
	if rec := e.post(path+"/massal", e.a.Principal.ID, "kepala_sekolah", url.Values{"action": {"arsip"}, "ids": {mine.ID}}); rec.Code != http.StatusForbidden {
		t.Errorf("principal bulk = %d", rec.Code)
	}
	// Massal oleh guru lain: dilewati.
	rec = e.post(path+"/massal", other, "guru", url.Values{"action": {"kesulitan"}, "difficulty": {"sulit"}, "ids": {mine.ID}})
	if rec.Code != http.StatusSeeOther {
		t.Errorf("other bulk = %d", rec.Code)
	}
	if cur, _ := GetQuestion(ctx, e.tx, e.a.ID, mine.ID); cur.Difficulty == "sulit" {
		t.Error("other teacher changed difficulty")
	}
	// Duplikat oleh guru lain menjadi draf miliknya.
	if rec := e.post("/ujian/soal/"+mine.ID+"/duplikat", other, "guru", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Errorf("duplicate = %d", rec.Code)
	}
	var n int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_questions WHERE created_by = $1`, other).Scan(&n)
	if n != 1 {
		t.Errorf("duplicates by other teacher = %d", n)
	}

	// TP: tempel dan tampil.
	rec = e.post("/ujian/tp/tempel", teacher, "guru", url.Values{"subject_id": {e.a.SubjectID}, "grade_level": {"7"}, "paste": {"TP 7.1\tBilangan\nTP 7.2\tAljabar"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("paste = %d", rec.Code)
	}
	if body := e.getAs("/ujian/tp/?mapel="+e.a.SubjectID+"&tingkat=7", teacher, "guru").Body.String(); !strings.Contains(body, "TP 7.2") {
		t.Error("pasted TP not listed")
	}
}
