package exam

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWordImportFlow(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	teacher := e.a.Teacher.ID
	owner := e.staff(teacher, "guru")
	if _, err := PasteObjectives(ctx, e.tx, owner, e.a.SubjectID, "7", "TP 1.1\tPecahan", now); err != nil {
		t.Fatal(err)
	}
	bankPath := "/ujian/bank/" + e.bank.ID

	if rec := e.getAs(bankPath+"/impor", teacher, "guru"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "template-soal.docx") {
		t.Fatalf("import page = %d", rec.Code)
	}
	if rec := e.getAs("/ujian/impor/template-soal.docx", teacher, "guru"); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "PK") {
		t.Fatalf("template download = %d", rec.Code)
	}
	tpl, _ := WordTemplate()
	rec := e.upload(bankPath+"/impor?source=word", teacher, "guru", "soal.docx", tpl)
	// FormValue membaca source dari query bila tidak ada di multipart.
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body.String())
	}
	impPath := rec.Header().Get("Location")
	impID := strings.TrimPrefix(impPath, "/ujian/impor/")
	body := e.getAs(impPath, teacher, "guru").Body.String()
	if !strings.Contains(body, "9 siap") || !strings.Contains(body, "Stimulus: Teks untuk soal nomor 7–8") || !strings.Contains(body, "katex") {
		t.Fatalf("preview missing counts, stimulus, or rendered math")
	}
	// Orang lain (guru lain) tidak bisa membuka impor ini.
	if rec := e.getAs(impPath, e.teacher2, "guru"); rec.Code != http.StatusNotFound {
		t.Errorf("other teacher preview = %d", rec.Code)
	}

	// Perbaiki soal 1 di pratinjau (editor dalam mode impor).
	if rec := e.getAs(impPath+"/butir/0", teacher, "guru"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Perbaiki soal impor") || strings.Contains(rec.Body.String(), "data-question=") {
		t.Fatalf("item edit = %d", rec.Code)
	}
	items, _ := ListImportItems(ctx, e.tx, e.a.ID, impID)
	fix := items[0].Form.Values()
	fix.Set("body", "<p>Diperbaiki di pratinjau</p>")
	if rec := e.post(impPath+"/butir/0", teacher, "guru", fix); rec.Code != http.StatusSeeOther {
		t.Fatalf("item save = %d", rec.Code)
	}

	// Simpan: soal 9 tidak diikutkan, soal 6 dipindah ke stimulus.
	form := url.Values{"mode": {"siap"}, "tags": {"impor-uji"}, "difficulty": {"sedang"}}
	var stimSeq string
	for _, it := range items {
		if it.Kind == "stimulus" {
			stimSeq = itoa(it.Seq)
		}
	}
	for _, it := range items {
		if it.Kind == "soal" && it.Number != "9" {
			form.Add("include", itoa(it.Seq))
		}
		if it.Number == "6" {
			form.Set("stim_"+itoa(it.Seq), stimSeq)
		}
	}
	if rec := e.post(impPath+"/simpan", teacher, "guru", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("commit = %d %s", rec.Code, rec.Body.String())
	}
	if body := e.getAs(impPath, teacher, "guru").Body.String(); !strings.Contains(body, "8 soal masuk (siap 8, draf 0), 1 stimulus, 1 dilewati") {
		t.Errorf("report missing: %s", body[strings.Index(body, "<main"):][:400])
	}
	var n, inStim, withTP, tagged int
	e.tx.QueryRow(ctx, `SELECT count(*), count(stimulus_id), count(objective_id), count(*) FILTER (WHERE 'impor-uji' = ANY(tags))
		FROM exam_questions WHERE bank_id = $1 AND archived_at IS NULL`, e.bank.ID).Scan(&n, &inStim, &withTP, &tagged)
	if n != 8 || inStim != 3 || withTP != 1 || tagged != 8 {
		t.Errorf("saved = %d questions, %d in stimulus, %d with TP, %d tagged", n, inStim, withTP, tagged)
	}
	var fixed int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_questions WHERE bank_id = $1 AND body_src LIKE '%Diperbaiki di pratinjau%'`, e.bank.ID).Scan(&fixed)
	if fixed != 1 {
		t.Error("fixed item not saved")
	}
	// Impor yang sudah selesai tidak bisa disimpan ulang.
	if rec := e.post(impPath+"/simpan", teacher, "guru", form); rec.Code == http.StatusSeeOther {
		t.Error("committed twice")
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

// Gagal di tengah tidak menyimpan sebagian: satu soal merujuk gambar yang
// tidak ada, maka tidak ada soal maupun stimulus yang tersimpan.
func TestCommitIsAtomic(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	sa := e.staff(e.a.Teacher.ID, "guru")
	tpl, _ := WordTemplate()
	imp, err := CreateImport(ctx, e.tx, e.store, sa, e.bank.ID, SourceWord, "a.docx", tpl, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := ProcessImport(ctx, e.tx, e.store, mr(t), e.a.ID, imp.ID); err != nil {
		t.Fatal(err)
	}
	imp, _ = GetImport(ctx, e.tx, e.a.ID, imp.ID)
	items, _ := ListImportItems(ctx, e.tx, e.a.ID, imp.ID)
	last := items[len(items)-1]
	last.Form.Body = `<p><img data-asset="99999999-9999-4999-8999-999999999999" alt="hilang"></p>`
	if _, err := SaveImportItem(ctx, e.tx, mr(t), imp, last.Seq, last.Form, StimulusDraft{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitImport(ctx, e.tx, mr(t), sa, imp, CommitOptions{IncludeCheck: true}, time.Now()); err == nil {
		t.Fatal("commit with missing asset succeeded")
	}
	var q, s int
	e.tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM exam_questions WHERE bank_id = $1), (SELECT count(*) FROM exam_stimuli WHERE bank_id = $1)`, e.bank.ID).Scan(&q, &s)
	if q != 0 || s != 0 {
		t.Errorf("partial save: %d questions, %d stimuli", q, s)
	}
}
