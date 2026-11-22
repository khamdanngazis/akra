package exam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"akra/internal/auth"
)

// sampleQuestion membuat soal contoh per jenis dengan penanda rahasia di
// kunci, rubrik, dan pembahasan.
func sampleQuestion(k Kind) Question {
	q := Question{Kind: k, BodySrc: "<p>Soal " + string(k) + "</p>", BodyHTML: "<p>Soal " + string(k) + "</p>", Points: 2,
		ExplanationSrc: "RAHASIA-PEMBAHASAN", ExplanationHTML: "<p>RAHASIA-PEMBAHASAN</p>", Status: StatusReady}
	c := func(id, t string) Choice { return Choice{ID: id, Src: t, HTML: t} }
	switch k {
	case KindMC, KindMulti:
		q.Options.Choices = []Choice{c("a", "satu"), c("b", "dua"), c("c", "tiga")}
		q.Key.Correct = []string{"b"}
		if k == KindMulti {
			q.Key.Correct, q.Key.Scoring = []string{"a", "c"}, ScoringProportional
		}
	case KindTF:
		q.Options.Statements = []Choice{c("s1", "Air mendidih 100°C"), c("s2", "Bumi datar")}
		q.Key.Truth = map[string]bool{"s1": true, "s2": false}
	case KindShort:
		q.Options.Input = "angka"
		q.Key.Accepted, q.Key.Tolerance = []string{"RAHASIA-12"}, 0.5
	case KindMatch:
		q.Options.Left = []Choice{c("l1", "Ibu kota Jawa Barat")}
		q.Options.Right = []Choice{c("r1", "Bandung"), c("r2", "Surabaya")}
		q.Key.Pairs = map[string]string{"l1": "r1"}
	case KindEssay:
		q.Key.RubricSrc, q.Key.RubricHTML = "RAHASIA-RUBRIK", "<p>RAHASIA-RUBRIK</p>"
	}
	return q
}

// Serialisasi peserta tidak pernah memuat kunci, rubrik, pembahasan, atau
// sumber (kriteria T127), untuk setiap jenis.
func TestForParticipantHidesKey(t *testing.T) {
	for _, k := range Kinds {
		b, err := json.Marshal(ForParticipant(sampleQuestion(k)))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, banned := range []string{"RAHASIA", "correct", "truth", "accepted", "tolerance", "pairs", "rubric", "explanation", "scoring", `"src"`} {
			if strings.Contains(s, banned) {
				t.Errorf("%s: participant JSON contains %q: %s", k, banned, s)
			}
		}
		if !strings.Contains(s, "Soal "+string(k)) {
			t.Errorf("%s: body missing: %s", k, s)
		}
	}
	if p := ForParticipant(sampleQuestion(KindMulti)); !p.Multiple || len(p.Choices) != 3 {
		t.Errorf("multi = %+v", p)
	}
	if p := ForParticipant(sampleQuestion(KindMatch)); len(p.Right) != 2 || len(p.Left) != 1 {
		t.Errorf("match = %+v", p)
	}
}

func TestNormalizeArabicSearch(t *testing.T) {
	if got, want := NormalizeSearch("الصَّلَاةُ"), NormalizeSearch("الصلاة"); got != want {
		t.Errorf("harakat: %q != %q", got, want)
	}
	if got := NormalizeArabic("إِيَّاكَ مَدْرَسَةٌ عَلَى"); got != "اياك مدرسه علي" {
		t.Errorf("NormalizeArabic = %q", got)
	}
	if got := PlainText(`<p dir="rtl">نص</p><p>Hitung <span class="math" data-tex="\frac{1}{2}"></span> ya</p>`); got != `نص Hitung \frac{1}{2} ya` {
		t.Errorf("PlainText = %q", got)
	}
}

// bankEnv: sekolah A dengan bank mapel SubjectID, guru pengampu (Teacher)
// dan guru kedua (teacher2) yang juga mengampu mapel itu.
type bankEnv struct {
	*env
	bank     Bank
	teacher2 string
}

func (e *bankEnv) staff(userID string, roles ...string) StaffActor {
	e.t.Helper()
	a, err := LoadActor(context.Background(), e.tx, auth.Identity{UserID: userID, SchoolID: e.a.ID, Roles: roles})
	if err != nil {
		e.t.Fatal(err)
	}
	return StaffActor{Actor: a, SchoolID: e.a.ID}
}

func newBankEnv(t *testing.T) *bankEnv {
	t.Helper()
	e := &bankEnv{env: newEnv(t)}
	ctx := context.Background()
	if err := e.tx.QueryRow(ctx, `
		WITH u AS (INSERT INTO users (school_id, email, password_hash, full_name, is_active) VALUES ($1, 'guru2@a.uji', 'x', 'Guru Dua', true) RETURNING id),
		     r AS (INSERT INTO user_roles (user_id, role_id) SELECT u.id, roles.id FROM u, roles WHERE roles.code = 'guru'),
		     c AS (INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '7Z', 7) RETURNING id),
		     ta AS (INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id)
		            SELECT $1, $2, c.id, $3, u.id FROM c, u)
		SELECT id FROM u`, e.a.ID, e.a.YearID, e.a.SubjectID).Scan(&e.teacher2); err != nil {
		t.Fatal(err)
	}
	var err error
	e.bank, err = CreateBank(ctx, e.tx, e.staff(e.a.Teacher.ID, "guru"), BankForm{Name: "Matematika 7", SubjectID: e.a.SubjectID, GradeLevel: "7"}, now)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestQuestionVersionsAndSnapshot(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	owner := e.staff(e.a.Teacher.ID, "guru")
	q, err := CreateQuestion(ctx, e.tx, owner, e.bank.ID, sampleQuestion(KindMC), now)
	if err != nil {
		t.Fatal(err)
	}
	if q.Version != 1 || q.CreatedBy != e.a.Teacher.ID {
		t.Fatalf("created = %+v", q)
	}
	edit := q
	edit.BodySrc, edit.BodyHTML = "<p>Diubah</p>", "<p>Diubah</p>"
	q2, err := UpdateQuestion(ctx, e.tx, owner, q.ID, edit, now)
	if err != nil {
		t.Fatal(err)
	}
	if q2.Version != 2 || q2.BodyHTML != "<p>Diubah</p>" {
		t.Errorf("updated = v%d %q", q2.Version, q2.BodyHTML)
	}
	// Snapshot versi 1 (yang dipakai ujian lama) tetap sama.
	v1, err := QuestionVersion(ctx, e.tx, e.a.ID, q.ID, 1)
	if err != nil || v1.BodyHTML != "<p>Soal pg</p>" || v1.Key.Correct[0] != "b" {
		t.Errorf("snapshot v1 = %q %v, %v", v1.BodyHTML, v1.Key, err)
	}
	if v2, _ := QuestionVersion(ctx, e.tx, e.a.ID, q.ID, 2); v2.BodyHTML != "<p>Diubah</p>" {
		t.Errorf("snapshot v2 = %q", v2.BodyHTML)
	}
	// Snapshot tidak bisa diubah atau dihapus.
	if _, err := e.tx.Exec(ctx, `SAVEPOINT s`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `UPDATE exam_question_versions SET snapshot = '{}' WHERE question_id = $1`, q.ID); err == nil {
		t.Error("snapshot update allowed")
	}
	e.tx.Exec(ctx, `ROLLBACK TO SAVEPOINT s`)
}

// Bank dipakai bersama: guru lain memakai dan menduplikat, tetapi tidak
// bisa mengubah atau mengarsipkan; admin_tu bisa.
func TestSharedBankPermissions(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	owner := e.staff(e.a.Teacher.ID, "guru")
	other := e.staff(e.teacher2, "guru")
	admin := e.staff(e.a.Admin.ID, "admin_tu")
	principal := e.staff(e.a.Principal.ID, "kepala_sekolah")
	q, err := CreateQuestion(ctx, e.tx, owner, e.bank.ID, sampleQuestion(KindMC), now)
	if err != nil {
		t.Fatal(err)
	}
	page, err := ListQuestions(ctx, e.tx, other.Actor, e.bank, QuestionFilter{}, now)
	if err != nil || page.Total != 1 || page.Rows[0].CanEdit {
		t.Fatalf("other teacher list = %+v, %v", page, err)
	}
	if _, err := UpdateQuestion(ctx, e.tx, other, q.ID, q, now); !errors.Is(err, ErrNotEditable) {
		t.Errorf("other teacher update: %v", err)
	}
	if err := ArchiveQuestion(ctx, e.tx, other, q.ID, now); !errors.Is(err, ErrNotEditable) {
		t.Errorf("other teacher archive: %v", err)
	}
	if res, err := BulkUpdate(ctx, e.tx, other, e.bank.ID, []string{q.ID}, Bulk{Action: "arsip"}, now); err != nil || res.Skipped != 1 || res.Updated != 0 {
		t.Errorf("other teacher bulk = %+v, %v", res, err)
	}
	dup, err := DuplicateQuestion(ctx, e.tx, other, q.ID, "", now)
	if err != nil || dup.CreatedBy != e.teacher2 || dup.Status != StatusDraft || dup.Key.Correct[0] != "b" {
		t.Fatalf("duplicate = %+v, %v", dup, err)
	}
	if _, err := UpdateQuestion(ctx, e.tx, other, dup.ID, dup, now); err != nil {
		t.Errorf("edit own duplicate: %v", err)
	}
	if _, err := UpdateQuestion(ctx, e.tx, principal, q.ID, q, now); !errors.Is(err, ErrNotEditable) {
		t.Errorf("principal update: %v", err)
	}
	if _, err := UpdateQuestion(ctx, e.tx, admin, q.ID, q, now); err != nil {
		t.Errorf("admin update: %v", err)
	}
	// Guru yang tidak mengampu mapel tidak melihat bank sama sekali.
	var outsider string
	e.tx.QueryRow(ctx, `WITH u AS (INSERT INTO users (school_id, email, password_hash, full_name, is_active) VALUES ($1, 'guru3@a.uji', 'x', 'Guru Tiga', true) RETURNING id),
		r AS (INSERT INTO user_roles (user_id, role_id) SELECT u.id, roles.id FROM u, roles WHERE roles.code = 'guru') SELECT id FROM u`, e.a.ID).Scan(&outsider)
	if banks, _ := ListBanks(ctx, e.tx, e.a.ID, e.staff(outsider, "guru").Actor, BankFilter{}, now); len(banks) != 0 {
		t.Errorf("outsider sees %d banks", len(banks))
	}
	if _, err := DuplicateQuestion(ctx, e.tx, e.staff(outsider, "guru"), q.ID, "", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider duplicate: %v", err)
	}
	// Sekolah lain: soal tidak ditemukan.
	if _, err := GetQuestion(ctx, e.tx, e.b.ID, q.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("other school get: %v", err)
	}
}

func TestStimulusGroupingAndSearch(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	owner := e.staff(e.a.Teacher.ID, "guru")
	st, err := CreateStimulus(ctx, e.tx, owner, e.bank.ID, Stimulus{Title: "Teks Arab untuk soal 1–2",
		BodySrc: `<p dir="rtl">ذَهَبَ أَحْمَدُ إِلَى الْمَدْرَسَةِ</p>`, BodyHTML: `<p dir="rtl">ذَهَبَ أَحْمَدُ إِلَى الْمَدْرَسَةِ</p>`}, now)
	if err != nil {
		t.Fatal(err)
	}
	solo, _ := CreateQuestion(ctx, e.tx, owner, e.bank.ID, sampleQuestion(KindEssay), now)
	var inStim []string
	for range 2 {
		q := sampleQuestion(KindMC)
		q.StimulusID = st.ID
		cq, err := CreateQuestion(ctx, e.tx, owner, e.bank.ID, q, now)
		if err != nil {
			t.Fatal(err)
		}
		inStim = append(inStim, cq.ID)
	}
	arab := sampleQuestion(KindShort)
	arab.BodySrc = `<p dir="rtl">مَا مَعْنَى الصَّلَاةُ؟</p>`
	if _, err := CreateQuestion(ctx, e.tx, owner, e.bank.ID, arab, now); err != nil {
		t.Fatal(err)
	}
	page, err := ListQuestions(ctx, e.tx, owner.Actor, e.bank, QuestionFilter{}, now)
	if err != nil || page.Total != 4 {
		t.Fatalf("list = %d, %v", page.Total, err)
	}
	// Soal stimulus berurutan dan berdampingan.
	var order []string
	for _, r := range page.Rows {
		order = append(order, r.ID)
	}
	idx := func(id string) int {
		for i, o := range order {
			if o == id {
				return i
			}
		}
		return -1
	}
	if idx(inStim[1]) != idx(inStim[0])+1 || page.Rows[idx(inStim[0])].StimulusTitle == "" {
		t.Errorf("stimulus questions not grouped: %v (solo %s)", order, solo.ID)
	}
	// Cari Arab dengan dan tanpa harakat.
	for _, term := range []string{"الصلاة", "الصَّلَاةُ"} {
		if p, _ := ListQuestions(ctx, e.tx, owner.Actor, e.bank, QuestionFilter{Text: term}, now); p.Total != 1 {
			t.Errorf("search %q = %d, want 1", term, p.Total)
		}
	}
	// Cari teks stimulus menemukan soal-soalnya.
	if p, _ := ListQuestions(ctx, e.tx, owner.Actor, e.bank, QuestionFilter{Text: "المدرسة"}, now); p.Total != 2 {
		t.Errorf("search stimulus text = %d, want 2", p.Total)
	}
	// Urutan ulang dan versi stimulus.
	if err := ReorderStimulus(ctx, e.tx, owner, st.ID, []string{inStim[1], inStim[0]}, now); err != nil {
		t.Fatal(err)
	}
	if qs, _ := StimulusQuestions(ctx, e.tx, e.a.ID, st.ID); qs[0].ID != inStim[1] {
		t.Error("reorder not applied")
	}
	upd := st
	upd.BodySrc, upd.BodyHTML = "<p>Teks baru</p>", "<p>Teks baru</p>"
	if s2, err := UpdateStimulus(ctx, e.tx, owner, st.ID, upd, now); err != nil || s2.Version != 2 {
		t.Fatalf("update stimulus = %v, %v", s2.Version, err)
	}
	if v1, _ := StimulusVersion(ctx, e.tx, e.a.ID, st.ID, 1); !strings.Contains(v1.BodyHTML, "ذَهَبَ") {
		t.Errorf("stimulus snapshot v1 changed: %q", v1.BodyHTML)
	}
	if err := ArchiveStimulus(ctx, e.tx, owner, st.ID, now); !errors.Is(err, ErrStimulusInUse) {
		t.Errorf("archive in-use stimulus: %v", err)
	}
	if err := ReorderStimulus(ctx, e.tx, e.staff(e.teacher2, "guru"), st.ID, []string{inStim[0], inStim[1]}, now); !errors.Is(err, ErrNotEditable) {
		t.Errorf("other teacher reorder: %v", err)
	}
}

func TestObjectivesAndBankSummary(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	owner := e.staff(e.a.Teacher.ID, "guru")
	res, err := PasteObjectives(ctx, e.tx, owner, e.a.SubjectID, "7", "TP 1.1\tMengenal bilangan bulat\nTP 1.2 - Operasi pecahan\nbaris tanpa pemisah\nTP 1.1\tduplikat", now)
	if err != nil || res.Added != 2 || len(res.Skipped) != 2 {
		t.Fatalf("paste = %+v, %v", res, err)
	}
	objs, _ := ListObjectives(ctx, e.tx, e.a.ID, e.a.SubjectID, 7)
	if len(objs) != 2 || objs[0].Code != "TP 1.1" {
		t.Fatalf("objectives = %+v", objs)
	}
	q := sampleQuestion(KindMC)
	q.ObjectiveID, q.Difficulty = objs[0].ID, "mudah"
	if _, err := CreateQuestion(ctx, e.tx, owner, e.bank.ID, q, now); err != nil {
		t.Fatal(err)
	}
	draft := sampleQuestion(KindEssay)
	draft.Status = StatusDraft
	CreateQuestion(ctx, e.tx, owner, e.bank.ID, draft, now)
	banks, err := ListBanks(ctx, e.tx, e.a.ID, owner.Actor, BankFilter{Grade: 7}, now)
	if err != nil {
		t.Fatal(err)
	}
	var s BankSummary
	for _, b := range banks {
		if b.ID == e.bank.ID {
			s = b
		}
	}
	if s.Ready != 1 || s.Draft != 1 || s.ByKind[KindMC] != 1 || s.ByDifficulty["mudah"] != 1 || s.UncoveredObjectives != 1 || !s.CanEdit {
		t.Errorf("summary = %+v", s)
	}
	// TP dari mapel lain ditolak.
	var otherSubj string
	e.tx.QueryRow(ctx, `INSERT INTO subjects (school_id, name) VALUES ($1, 'Seni') RETURNING id`, e.a.ID).Scan(&otherSubj)
	if _, err := CreateObjective(ctx, e.tx, owner, ObjectiveForm{SubjectID: otherSubj, GradeLevel: "7", Code: "X", Description: "Y"}, now); !errors.Is(err, ErrNotEditable) {
		t.Errorf("objective for untaught subject: %v", err)
	}
	// Guru lain pengampu mapel yang sama boleh mengatur TP.
	if _, err := CreateObjective(ctx, e.tx, e.staff(e.teacher2, "guru"), ObjectiveForm{SubjectID: e.a.SubjectID, GradeLevel: "7", Code: "TP 2.1", Description: "Geometri"}, now); err != nil {
		t.Errorf("co-teacher objective: %v", err)
	}
}

// Pencarian dan filter di bank 5.000 soal < 300 ms (kriteria T127).
func TestSearch5000Questions(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	e := newBankEnv(t)
	ctx := context.Background()
	if _, err := e.tx.Exec(ctx, `
		INSERT INTO exam_questions (school_id, bank_id, kind, body_src, body_html, status, difficulty, search_text, created_by, updated_by)
		SELECT $1, $2, 'pg', '<p>Soal nomor ' || g || '</p>', '<p>Soal nomor ' || g || '</p>',
		       CASE WHEN g % 3 = 0 THEN 'draf' ELSE 'siap' END, (ARRAY['mudah','sedang','sulit'])[g % 3 + 1],
		       'soal nomor ' || g || ' tentang ' || (ARRAY['pecahan','aljabar','geometri','statistika'])[g % 4 + 1], $3, $3
		FROM generate_series(1, 5000) g`, e.a.ID, e.bank.ID, e.a.Teacher.ID); err != nil {
		t.Fatal(err)
	}
	e.tx.Exec(ctx, `ANALYZE exam_questions`)
	a := e.staff(e.a.Teacher.ID, "guru").Actor
	for _, f := range []QuestionFilter{{}, {Text: "aljabar"}, {Text: "nomor 4999"}, {Difficulty: "sulit", Status: "siap"}, {Text: "geometri", Page: 10}} {
		start := time.Now()
		p, err := ListQuestions(ctx, e.tx, a, e.bank, f, now)
		el := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		if el > 300*time.Millisecond {
			t.Errorf("filter %+v took %v (total %d)", f, el, p.Total)
		}
		t.Log(fmt.Sprintf("filter %+v: %d rows in %v", f, p.Total, el))
	}
}
