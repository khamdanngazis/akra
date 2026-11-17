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

// examEnv adalah bankEnv dengan beberapa soal siap di bank guru.
type examEnv struct {
	*bankEnv
	owner StaffActor
	qs    []Question
}

func newExamEnv(t *testing.T) *examEnv {
	t.Helper()
	e := &examEnv{bankEnv: newBankEnv(t)}
	ctx := context.Background()
	e.owner = e.staff(e.a.Teacher.ID, "guru")
	for i, d := range []string{"mudah", "mudah", "mudah", "sedang", "sedang", "sulit"} {
		q := sampleQuestion(KindMC)
		q.BodySrc = "<p>Soal ujian " + itoa(i) + "</p>"
		q.BodyHTML = q.BodySrc
		q.Difficulty = d
		created, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, q, now)
		if err != nil {
			t.Fatal(err)
		}
		e.qs = append(e.qs, created)
	}
	return e
}

func (e *examEnv) create(t *testing.T, sa StaffActor, purpose Purpose) Exam {
	t.Helper()
	ex, err := CreateExam(context.Background(), e.tx, sa, BasicsForm{Name: "Ulangan Pecahan", Purpose: string(purpose),
		SubjectID: e.a.SubjectID, Duration: "40"}, now)
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

func (e *examEnv) firstSection(t *testing.T, ex Exam) string {
	t.Helper()
	secs, err := LoadContent(context.Background(), e.tx, ex)
	if err != nil || len(secs) == 0 {
		t.Fatalf("sections: %v", err)
	}
	return secs[0].ID
}

// Aturan acak yang melebihi isi bank ditolak dengan jumlah tersedia.
func TestRuleExceedingBankRejected(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	ex := e.create(t, e.owner, PurposeQuiz)
	sec := e.firstSection(t, ex)
	// Satu soal tetap mudah: tersisa 2 soal mudah untuk aturan acak.
	if _, err := AddQuestions(ctx, e.tx, e.owner, ex.ID, sec, []string{e.qs[0].ID}, now); err != nil {
		t.Fatal(err)
	}
	err := AddRule(ctx, e.tx, e.owner, ex.ID, RuleForm{SectionID: sec, BankID: e.bank.ID, Kind: "pg", Difficulty: "mudah", Count: "3"}, now)
	var fe *FieldError
	if !errors.As(err, &fe) || !strings.Contains(fe.Fields["count"], "Hanya tersedia 2 soal") {
		t.Fatalf("over-bank rule: %v", err)
	}
	if err := AddRule(ctx, e.tx, e.owner, ex.ID, RuleForm{SectionID: sec, BankID: e.bank.ID, Kind: "pg", Difficulty: "mudah", Count: "2"}, now); err != nil {
		t.Fatal(err)
	}
	// Aturan kedua bersaing dengan yang pertama: semua soal mudah sudah terpakai.
	err = AddRule(ctx, e.tx, e.owner, ex.ID, RuleForm{SectionID: sec, BankID: e.bank.ID, Difficulty: "mudah", Count: "1"}, now)
	if !errors.As(err, &fe) || !strings.Contains(fe.Fields["count"], "Hanya tersedia 0") {
		t.Errorf("competing rule: %v", err)
	}
	// Lewat HTTP: pesan dan jumlah tersedia tampil di langkah Soal.
	rec := e.post("/ujian/paket/"+ex.ID+"/acak", e.a.Teacher.ID, "guru", url.Values{"section_id": {sec}, "bank_id": {e.bank.ID}, "count": {"9"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Hanya tersedia 3 soal") ||
		!strings.Contains(rec.Body.String(), "tersedia 2") {
		t.Errorf("http rule = %d", rec.Code)
	}
	p, _ := BuildPlan(ctx, e.tx, ex)
	if c := p.Composition(); c.Count != 3 || c.ByDifficulty["mudah"] != 3 {
		t.Errorf("plan composition = %+v", c)
	}
}

// Setelah terkunci, menyunting soal di bank tidak mengubah snapshot.
func TestLockFreezesSnapshot(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	ex := e.create(t, e.owner, PurposeQuiz)
	sec := e.firstSection(t, ex)
	if _, err := AddQuestions(ctx, e.tx, e.owner, ex.ID, sec, []string{e.qs[0].ID, e.qs[1].ID}, now); err != nil {
		t.Fatal(err)
	}
	start := now.Add(time.Hour)
	if _, err := CreateClassSessions(ctx, e.tx, e.owner, ex.ID, []ClassSlot{{ClassID: e.a.ClassID, Start: start, End: start.Add(time.Hour)}}, now); err != nil {
		t.Fatal(err)
	}
	if err := ScheduleExam(ctx, e.tx, e.owner, ex.ID, now); err != nil {
		t.Fatal(err)
	}
	ex, _ = GetExam(ctx, e.tx, e.a.ID, ex.ID)
	// Belum mulai: belum terkunci.
	if ex, _ = EnsureLocked(ctx, e.tx, ex, now); ex.Locked() {
		t.Fatal("locked before start")
	}
	locked, _, err := AdvanceExams(ctx, e.tx, start.Add(time.Minute))
	if err != nil || locked != 1 {
		t.Fatalf("advance = %d, %v", locked, err)
	}
	ex, _ = GetExam(ctx, e.tx, e.a.ID, ex.ID)
	if !ex.Locked() || ex.Status != ExamRunning {
		t.Fatalf("after start = %s locked=%v", ex.Status, ex.Locked())
	}
	vs, _ := ListVariants(ctx, e.tx, e.a.ID, ex.ID)
	edit := e.qs[0]
	edit.BodySrc, edit.BodyHTML = "<p>Diubah setelah ujian</p>", "<p>Diubah setelah ujian</p>"
	if _, err := UpdateQuestion(ctx, e.tx, e.owner, edit.ID, edit, now); err != nil {
		t.Fatal(err)
	}
	paper, err := LoadPaper(ctx, e.tx, ex, vs[0].ID)
	if err != nil || len(paper) != 2 {
		t.Fatalf("paper = %d, %v", len(paper), err)
	}
	for _, it := range paper {
		if strings.Contains(it.Question.BodyHTML, "Diubah") || it.Question.Version != 1 {
			t.Errorf("snapshot changed: %q v%d", it.Question.BodyHTML, it.Question.Version)
		}
	}
	// Isi tidak bisa diubah lagi.
	if err := RemoveItem(ctx, e.tx, e.owner, ex.ID, "00000000-0000-0000-0000-000000000000", now); !errors.Is(err, ErrExamLocked) {
		t.Errorf("edit locked exam: %v", err)
	}
	// Semua sesi berakhir: selesai.
	if _, done, _ := AdvanceExams(ctx, e.tx, start.Add(3*time.Hour)); done != 1 {
		t.Errorf("finished = %d", done)
	}
}

// Guru tidak bisa memilih kelas yang tidak ia ampu.
func TestTeacherCannotPickOtherClass(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	ex := e.create(t, e.owner, PurposeQuiz)
	var other string
	e.tx.QueryRow(ctx, `SELECT id FROM classes WHERE school_id = $1 AND name = '7Z'`, e.a.ID).Scan(&other)
	classes, err := ClassesFor(ctx, e.tx, e.owner.Actor, ex, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range classes {
		if c.ID == other || c.ID == e.a.Class2ID {
			t.Errorf("class %s offered to teacher", c.Name)
		}
	}
	if len(classes) != 1 || classes[0].ID != e.a.ClassID {
		t.Errorf("classes = %+v", classes)
	}
	start := now.Add(time.Hour)
	_, err = CreateClassSessions(ctx, e.tx, e.owner, ex.ID, []ClassSlot{{ClassID: other, Start: start, End: start.Add(time.Hour)}}, now)
	if !errors.Is(err, ErrNotYourClass) {
		t.Errorf("other class: %v", err)
	}
	// Lewat HTTP juga ditolak, tanpa sesi tersimpan.
	e.post("/ujian/paket/"+ex.ID+"/sesi", e.a.Teacher.ID, "guru", url.Values{"class_id": {other}, "date_" + other: {"2027-03-11"},
		"start_" + other: {"07:00"}, "end_" + other: {"09:00"}})
	if s, _ := ListSessions(ctx, e.tx, e.a.ID, ex.ID); len(s) != 0 {
		t.Errorf("sessions = %d", len(s))
	}
	// Admin boleh memilih kelas mana pun.
	admin := e.staff(e.a.Admin.ID, "admin_tu")
	n, err := CreateClassSessions(ctx, e.tx, admin, ex.ID, []ClassSlot{{ClassID: other, Start: start, End: start.Add(time.Hour)}}, now)
	if err != nil || n != 1 {
		t.Errorf("admin other class: %d %v", n, err)
	}
	// Jendela lebih pendek dari durasi ditolak.
	_, err = CreateClassSessions(ctx, e.tx, e.owner, ex.ID, []ClassSlot{{ClassID: e.a.ClassID, Start: start, End: start.Add(10 * time.Minute)}}, now)
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Errorf("short window: %v", err)
	}
}

// Memilih soal berstimulus menambahkan seluruh kelompok; urutan ulang
// selalu menjaga kelompok tetap berdampingan.
func TestStimulusGroupAddedWhole(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	st, err := CreateStimulus(ctx, e.tx, e.owner, e.bank.ID, Stimulus{Title: "Bacaan", BodySrc: "<p>Teks</p>", BodyHTML: "<p>Teks</p>"}, now)
	if err != nil {
		t.Fatal(err)
	}
	var group []string
	for i := 0; i < 3; i++ {
		q := sampleQuestion(KindMC)
		q.StimulusID = st.ID
		q.BodySrc = "<p>Bacaan soal " + itoa(i) + "</p>"
		q.BodyHTML = q.BodySrc
		c, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, q, now)
		if err != nil {
			t.Fatal(err)
		}
		group = append(group, c.ID)
	}
	ex := e.create(t, e.owner, PurposeQuiz)
	sec := e.firstSection(t, ex)
	n, err := AddQuestions(ctx, e.tx, e.owner, ex.ID, sec, []string{e.qs[0].ID, group[1], e.qs[1].ID}, now)
	if err != nil || n != 5 {
		t.Fatalf("added = %d, %v", n, err)
	}
	secs, _ := LoadContent(ctx, e.tx, ex)
	var ids []string
	for _, it := range secs[0].Items {
		ids = append(ids, it.ID)
	}
	// Seret soal stimulus terakhir ke paling akhir: kelompok tetap utuh.
	moved := []string{ids[0], ids[1], ids[2], ids[4], ids[3]}
	if err := ReorderItems(ctx, e.tx, e.owner, ex.ID, sec, moved, now); err != nil {
		t.Fatal(err)
	}
	secs, _ = LoadContent(ctx, e.tx, ex)
	var stims []string
	for _, it := range secs[0].Items {
		stims = append(stims, it.Question.StimulusID)
	}
	runs := 0
	for i, s := range stims {
		if s == st.ID && (i == 0 || stims[i-1] != st.ID) {
			runs++
		}
	}
	if runs != 1 {
		t.Errorf("stimulus split after reorder: %v", stims)
	}
	// Melepas satu soal dari kelompok untuk ujian ini.
	for _, it := range secs[0].Items {
		if it.Question.ID == group[2] {
			if err := RemoveItem(ctx, e.tx, e.owner, ex.ID, it.ID, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	if p, _ := BuildPlan(ctx, e.tx, ex); p.Composition().Count != 4 || p.Composition().Stimuli != 1 {
		t.Errorf("after detach = %+v", p.Composition())
	}
}

// Butir merah memblokir penjadwalan; setelah beres ujian terjadwal dan
// varian dibagi bergilir.
func TestScheduleReadiness(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	ex := e.create(t, e.owner, PurposeQuiz)
	if err := ScheduleExam(ctx, e.tx, e.owner, ex.ID, now); !errors.Is(err, ErrNotReady) {
		t.Fatalf("empty exam scheduled: %v", err)
	}
	p, _ := BuildPlan(ctx, e.tx, ex)
	rd, _ := CheckReadiness(ctx, e.tx, ex, p, nil, ExamSettings{}, now)
	for _, k := range []string{"questions", "sessions"} {
		if it, _ := rd.Item(k); it.Level != ReadyBlock {
			t.Errorf("%s = %s, want block", k, it.Level)
		}
	}
	sec := e.firstSection(t, ex)
	// Soal tanpa kunci memblokir.
	nokey := sampleQuestion(KindShort)
	nokey.Key = AnswerKey{}
	nokey.Status = StatusDraft
	nk, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, nokey, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddQuestions(ctx, e.tx, e.owner, ex.ID, sec, []string{e.qs[3].ID, nk.ID}, now); err != nil {
		t.Fatal(err)
	}
	start := now.Add(2 * time.Hour)
	if _, err := CreateClassSessions(ctx, e.tx, e.owner, ex.ID, []ClassSlot{{ClassID: e.a.ClassID, Start: start, End: start.Add(time.Hour)}}, now); err != nil {
		t.Fatal(err)
	}
	ex, _ = GetExam(ctx, e.tx, e.a.ID, ex.ID)
	p, _ = BuildPlan(ctx, e.tx, ex)
	sessions, _ := ListSessions(ctx, e.tx, e.a.ID, ex.ID)
	rd, _ = CheckReadiness(ctx, e.tx, ex, p, sessions, ExamSettings{}, now)
	if it, _ := rd.Item("keys"); it.Level != ReadyBlock {
		t.Errorf("missing key = %s", it.Level)
	}
	if it, _ := rd.Item("status"); it.Level != ReadyWarn {
		t.Errorf("draft question = %s, want warn", it.Level)
	}
	if err := ScheduleExam(ctx, e.tx, e.owner, ex.ID, now); !errors.Is(err, ErrNotReady) {
		t.Errorf("scheduled with missing key: %v", err)
	}
	// Lepas soal tanpa kunci, tambah 2 paket: terjadwal.
	secs, _ := LoadContent(ctx, e.tx, ex)
	for _, it := range secs[0].Items {
		if it.Question.ID == nk.ID {
			RemoveItem(ctx, e.tx, e.owner, ex.ID, it.ID, now)
		}
	}
	if err := SetVariantCount(ctx, e.tx, e.owner, ex.ID, 2, now); err != nil {
		t.Fatal(err)
	}
	if err := ScheduleExam(ctx, e.tx, e.owner, ex.ID, now); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	ex, _ = GetExam(ctx, e.tx, e.a.ID, ex.ID)
	if ex.Status != ExamScheduled {
		t.Errorf("status = %s", ex.Status)
	}
	parts, _ := ListParticipants(ctx, e.tx, e.a.ID, sessions[0].ID)
	if len(parts) == 0 || parts[0].Variant != "A" {
		t.Errorf("participants = %+v", parts)
	}
	// Detail ujian menampilkan kartu langkah berikutnya dan tab.
	body := e.getAs("/ujian/paket/"+ex.ID, e.a.Teacher.ID, "guru").Body.String()
	if !strings.Contains(body, "Langkah berikutnya") || !strings.Contains(body, "Terjadwal") {
		t.Error("detail missing next step")
	}
}

// Akses: guru lain tidak melihat ujian; kepala sekolah hanya membaca;
// salin membuat draf baru milik penyalin.
func TestExamAccessAndCopy(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	ex := e.create(t, e.owner, PurposeQuiz)
	if rec := e.getAs("/ujian/paket/"+ex.ID, e.teacher2, "guru"); rec.Code != http.StatusNotFound {
		t.Errorf("other teacher = %d", rec.Code)
	}
	if body := e.getAs("/ujian/paket/", e.teacher2, "guru").Body.String(); strings.Contains(body, "Ulangan Pecahan") {
		t.Error("other teacher sees exam in list")
	}
	if rec := e.getAs("/ujian/paket/"+ex.ID+"/atur/soal", e.a.Principal.ID, "kepala_sekolah"); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "hanya bisa melihat") {
		t.Errorf("principal wizard = %d", rec.Code)
	}
	// Guru tanpa penugasan mapel tidak bisa membuat ulangan mapel itu.
	if _, err := CreateExam(ctx, e.tx, e.staff(e.teacher2, "guru"), BasicsForm{Name: "X", Purpose: "ulangan", SubjectID: e.b.SubjectID, Duration: "30"}, now); err == nil {
		t.Error("teacher created exam for unknown subject")
	}
	// Tes seleksi hanya admin_tu.
	if _, err := CreateExam(ctx, e.tx, e.owner, BasicsForm{Name: "TPA", Purpose: "seleksi", Duration: "60"}, now); !errors.Is(err, ErrForbidden) {
		t.Errorf("teacher created admission test: %v", err)
	}
	sel, err := CreateExam(ctx, e.tx, e.staff(e.a.Admin.ID, "admin_tu"), BasicsForm{Name: "TPA", Purpose: "seleksi", Duration: "60"}, now)
	if err != nil || sel.ResultsMode != ResultsNone {
		t.Errorf("admission defaults = %+v, %v", sel, err)
	}
	sec := e.firstSection(t, ex)
	AddQuestions(ctx, e.tx, e.owner, ex.ID, sec, []string{e.qs[0].ID}, now)
	AddRule(ctx, e.tx, e.owner, ex.ID, RuleForm{SectionID: sec, BankID: e.bank.ID, Count: "2"}, now)
	cp, err := CopyExam(ctx, e.tx, e.owner, ex.ID, "Ulangan 7B", now)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := BuildPlan(ctx, e.tx, cp)
	if cp.Name != "Ulangan 7B" || cp.Status != ExamDraft || p.Composition().Count != 3 || cp.CopiedFrom != ex.ID {
		t.Errorf("copy = %+v count=%d", cp, p.Composition().Count)
	}
	// Wizard lengkap bisa dibuka pemilik.
	for _, st := range []string{"dasar", "soal", "aturan", "jadwal", "cek"} {
		if rec := e.getAs("/ujian/paket/"+cp.ID+"/atur/"+st, e.a.Teacher.ID, "guru"); rec.Code != http.StatusOK {
			t.Errorf("wizard %s = %d", st, rec.Code)
		}
	}
	if rec := e.getAs("/ujian/paket/"+cp.ID+"/pilih?bank="+e.bank.ID, e.a.Teacher.ID, "guru"); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "sudah di ujian") {
		t.Errorf("picker = %d", rec.Code)
	}
}

// Buat ujian lewat HTTP dari wizard sampai langkah Soal.
func TestCreateExamHTTP(t *testing.T) {
	e := newExamEnv(t)
	rec := e.post("/ujian/paket/", e.a.Teacher.ID, "guru", url.Values{"name": {"Ulangan 1"}, "purpose": {"ulangan"},
		"subject_id": {e.a.SubjectID}, "duration": {"45"}})
	if rec.Code != http.StatusSeeOther || !strings.HasSuffix(rec.Header().Get("Location"), "/atur/soal") {
		t.Fatalf("create = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	rec = e.post("/ujian/paket/", e.a.Teacher.ID, "guru", url.Values{"purpose": {"ulangan"}, "duration": {"2"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Nama ujian wajib diisi") {
		t.Errorf("invalid create = %d", rec.Code)
	}
}
