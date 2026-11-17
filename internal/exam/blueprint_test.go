package exam

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBuildBlueprintWarnings(t *testing.T) {
	mk := func(level, diff string) Question {
		q := sampleQuestion(KindMC)
		q.CognitiveLevel, q.Difficulty = level, diff
		return q
	}
	// Semua LOTS dan mudah: menyimpang dari 30/50/20.
	qs := []Question{mk("C1", "mudah"), mk("C2", "mudah"), mk("C1", "mudah"), mk("", "")}
	tg, _ := ParseTarget("30/50/20")
	b := BuildBlueprint(qs, nil, Targets{Cognitive: tg, Difficulty: tg})
	if len(b.Rows) != 4 || b.Cognitive[0].Percent != 100 || !b.Cognitive[0].Off || len(b.Warnings) != 3 || b.Untagged != 1 {
		t.Errorf("blueprint = %+v", b)
	}
	// Sesuai target: tidak ada peringatan.
	var ok []Question
	for i := 0; i < 10; i++ {
		lvl := []string{"C1", "C1", "C1", "C3", "C3", "C3", "C3", "C3", "C4", "C5"}[i]
		diff := []string{"mudah", "mudah", "mudah", "sedang", "sedang", "sedang", "sedang", "sedang", "sulit", "sulit"}[i]
		ok = append(ok, mk(lvl, diff))
	}
	if b := BuildBlueprint(ok, nil, Targets{Cognitive: tg, Difficulty: tg}); len(b.Warnings) != 0 {
		t.Errorf("warnings for on-target set: %v", b.Warnings)
	}
	if _, valid := ParseTarget("30/50/30"); valid {
		t.Error("target not summing to 100 accepted")
	}
}

func TestCheckQuality(t *testing.T) {
	var qs []Question
	for i := 0; i < 10; i++ {
		q := sampleQuestion(KindMC) // kunci selalu "b"
		q.BodySrc = "<p>Soal unik nomor " + itoa(i) + " tentang bab " + itoa(i*7) + "</p>"
		if i < 3 {
			q.ObjectiveID = "x"
		}
		qs = append(qs, q)
	}
	allTrue := sampleQuestion(KindMC)
	allTrue.Options.Choices = append(allTrue.Options.Choices, Choice{ID: "d", Src: "<p>Semua benar.</p>"})
	qs = append(qs, allTrue)
	// Impor ganda: soal yang persis sama dua kali.
	dup := sampleQuestion(KindEssay)
	dup.BodySrc = "<p>Jelaskan siklus air secara lengkap dan beri contoh.</p>"
	qs = append(qs, dup, dup)
	rep := CheckQuality(qs)
	if !strings.HasPrefix(rep.KeySkewed, "B (") {
		t.Errorf("key skew = %q", rep.KeySkewed)
	}
	if len(rep.AllCorrect) != 1 || rep.NoObjective != 10 || len(rep.Duplicates) != 1 {
		t.Errorf("all-correct %d, no TP %d, duplicates %d", len(rep.AllCorrect), rep.NoObjective, len(rep.Duplicates))
	}
	// Hampir sama (beda satu kata) juga terdeteksi.
	near := dup
	near.BodySrc = "<p>Jelaskan siklus air secara lengkap dan beri contohnya.</p>"
	long := sampleQuestion(KindEssay)
	long.BodySrc = "<p>Jelaskan siklus air secara lengkap dari penguapan hingga hujan dan beri contoh di sekitar rumah.</p>"
	long2 := long
	long2.BodySrc = "<p>Jelaskan siklus air secara lengkap dari penguapan hingga hujan dan beri contoh di sekitar sekolah.</p>"
	if r := CheckQuality([]Question{long, long2}); len(r.Duplicates) != 1 {
		t.Errorf("near duplicates = %d", len(r.Duplicates))
	}
	_ = near
}

func TestReviewFlow(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	author := e.staff(e.a.Teacher.ID, "guru")
	reviewer := e.staff(e.teacher2, "guru")
	admin := e.staff(e.a.Admin.ID, "admin_tu")
	if err := SaveExamSettings(ctx, e.tx, admin, true, "30/50/20", "20/60/20"); err != nil {
		t.Fatal(err)
	}
	// Disimpan "siap" lewat editor: masuk telaah.
	rec := e.post("/ujian/bank/"+e.bank.ID+"/soal", e.a.Teacher.ID, "guru", url.Values{"kind": {"pg"}, "body": {"Soal telaah"},
		"choice_0": {"a"}, "choice_1": {"b"}, "correct": {"0"}, "status": {"siap"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	qid := strings.TrimPrefix(rec.Header().Get("Location"), "/ujian/soal/")
	q, _ := GetQuestion(ctx, e.tx, e.a.ID, qid)
	if q.Status != StatusInReview {
		t.Fatalf("status = %s, want ditelaah", q.Status)
	}
	s, _ := LoadExamSettings(ctx, e.tx, e.a.ID)
	if UsableInSchoolExam(q, s) {
		t.Error("question under review usable in PTS/PAS")
	}
	if err := ReviewQuestion(ctx, e.tx, author, qid, "setuju", "", now); !errors.Is(err, ErrReviewOwn) {
		t.Errorf("own review: %v", err)
	}
	if err := ReviewQuestion(ctx, e.tx, reviewer, qid, "perbaiki", "", now); err == nil {
		t.Error("fix request without note accepted")
	}
	if err := ReviewQuestion(ctx, e.tx, reviewer, qid, "perbaiki", "Opsi B ambigu", now); err != nil {
		t.Fatal(err)
	}
	q, _ = GetQuestion(ctx, e.tx, e.a.ID, qid)
	if q.Status != StatusDraft {
		t.Errorf("after fix request = %s", q.Status)
	}
	if body := e.getAs("/ujian/soal/"+qid, e.a.Teacher.ID, "guru").Body.String(); !strings.Contains(body, "Opsi B ambigu") || !strings.Contains(body, "Ajukan untuk ditelaah") {
		t.Error("author does not see the note or the submit button")
	}
	if err := SubmitForReview(ctx, e.tx, author, qid, now); err != nil {
		t.Fatal(err)
	}
	if body := e.getAs("/ujian/telaah", e.teacher2, "guru").Body.String(); !strings.Contains(body, "Soal telaah") {
		t.Error("reviewer queue missing the question")
	}
	if body := e.getAs("/ujian/telaah", e.a.Teacher.ID, "guru").Body.String(); strings.Contains(body, "Soal telaah") {
		t.Error("author sees own question in the review queue")
	}
	if rec := e.post("/ujian/soal/"+qid+"/telaah", e.teacher2, "guru", url.Values{"decision": {"setuju"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("approve = %d", rec.Code)
	}
	q, _ = GetQuestion(ctx, e.tx, e.a.ID, qid)
	if q.Status != StatusReady || !UsableInSchoolExam(q, s) {
		t.Errorf("after approval = %s", q.Status)
	}
	// Mengubah soal yang sudah disetujui mengembalikannya ke telaah.
	edit := FormFromQuestion(q).Values()
	edit.Set("body", "Soal telaah diubah")
	if rec := e.post("/ujian/soal/"+qid, e.a.Teacher.ID, "guru", edit); rec.Code != http.StatusSeeOther {
		t.Fatalf("edit = %d", rec.Code)
	}
	if q, _ = GetQuestion(ctx, e.tx, e.a.ID, qid); q.Status != StatusInReview {
		t.Errorf("after edit = %s, want ditelaah", q.Status)
	}
	// Impor saat telaah aktif: soal "siap" diajukan untuk ditelaah.
	tpl, _ := WordTemplate()
	imp, _ := CreateImport(ctx, e.tx, e.store, author, e.bank.ID, SourceWord, "t.docx", tpl, time.Now())
	ProcessImport(ctx, e.tx, e.store, mr(t), e.a.ID, imp.ID)
	imp, _ = GetImport(ctx, e.tx, e.a.ID, imp.ID)
	if _, err := CommitImport(ctx, e.tx, mr(t), author, imp, CommitOptions{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var ready int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_questions WHERE bank_id = $1 AND status = 'siap'`, e.bank.ID).Scan(&ready)
	if ready != 0 {
		t.Errorf("%d imported questions bypassed review", ready)
	}
	if err := SaveExamSettings(ctx, e.tx, admin, true, "40/40", "x"); err == nil {
		t.Error("invalid targets accepted")
	}
}

func TestDocumentPages(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	owner := e.staff(e.a.Teacher.ID, "guru")
	for i := 0; i < 3; i++ {
		q := sampleQuestion(KindMC)
		q.BodySrc = "<p>Soal kisi " + itoa(i) + `</p>`
		q.BodyHTML = q.BodySrc
		q.CognitiveLevel, q.Difficulty, q.Indicator = "C3", "sedang", "Menghitung pecahan "+itoa(i)
		if _, err := CreateQuestion(ctx, e.tx, owner, e.bank.ID, q, now); err != nil {
			t.Fatal(err)
		}
	}
	base := "/ujian/bank/" + e.bank.ID
	for path, want := range map[string]string{
		"/kisi-kisi":  "Menghitung pecahan 2",
		"/kartu-soal": "Kunci: B",
		"/mutu":       "Posisi kunci pilihan ganda",
	} {
		rec := e.getAs(base+path, e.a.Principal.ID, "kepala_sekolah")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s = %d, want %q", path, rec.Code, want)
		}
	}
	// Cek mutu membaca kunci dari database (bukan hanya dari memori).
	all, _ := AllQuestions(ctx, e.tx, owner.Actor, e.bank, QuestionFilter{}, nil, now)
	if rep := CheckQuality(rowsToQuestions(all)); rep.MCCount != 3 || rep.KeyShares[1].Count != 3 {
		t.Errorf("quality from DB: mc=%d shares=%v", rep.MCCount, rep.KeyShares)
	}
	if rec := e.getAs(base+"/kisi-kisi.xlsx", e.a.Teacher.ID, "guru"); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "PK") {
		t.Errorf("kisi-kisi xlsx = %d", rec.Code)
	}
	// Kisi-kisi soal terpilih saja.
	page, _ := ListQuestions(ctx, e.tx, owner.Actor, e.bank, QuestionFilter{}, now)
	body := e.getAs(base+"/kisi-kisi?ids="+page.Rows[0].ID, e.a.Teacher.ID, "guru").Body.String()
	if strings.Count(body, "Menghitung pecahan") != 1 {
		t.Error("selected-question blueprint does not have exactly one row")
	}
	// Pengaturan: admin dan koordinator; guru biasa 403.
	if rec := e.getAs("/ujian/pengaturan", e.a.Teacher.ID, "guru"); rec.Code != http.StatusForbidden {
		t.Errorf("teacher settings = %d", rec.Code)
	}
	if rec := e.post("/ujian/pengaturan", e.a.Admin.ID, "admin_tu", url.Values{"review": {"on"}, "cognitive": {"20/50/30"}, "difficulty": {"30/40/30"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("save settings = %d", rec.Code)
	}
}

func rowsToQuestions(rows []QuestionRow) []Question {
	out := make([]Question, len(rows))
	for i, r := range rows {
		out[i] = r.Question
	}
	return out
}
