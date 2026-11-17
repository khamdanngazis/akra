package exam

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Sesi susulan berisi tepat peserta tidak hadir dan memakai varian berbeda.
func TestMakeupSession(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	admin := e.staff(e.a.Admin.ID, "admin_tu")
	class := e.seedGrade(t, 1, 1, 4)[0]
	// Ujian 2 paket dengan cukup soal mudah agar paket B berbeda.
	ex := e.create(t, e.owner, PurposeQuiz)
	sec := e.firstSection(t, ex)
	if err := AddRule(ctx, e.tx, e.owner, ex.ID, RuleForm{SectionID: sec, BankID: e.bank.ID, Difficulty: "mudah", Count: "1"}, now); err != nil {
		t.Fatal(err)
	}
	SetVariantCount(ctx, e.tx, e.owner, ex.ID, 2, now)
	start := now.Add(time.Hour)
	if _, err := CreateClassSessions(ctx, e.tx, admin, ex.ID, []ClassSlot{{ClassID: class, Start: start, End: start.Add(time.Hour)}}, now); err != nil {
		t.Fatal(err)
	}
	if err := ScheduleExam(ctx, e.tx, e.owner, ex.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := IssueCards(ctx, e.tx, e.owner, NewIssuer(), CardScope{ExamID: ex.ID}, ex.Name, "", false, now); err != nil {
		t.Fatal(err)
	}
	// Satu siswa masuk (perangkat terikat); tiga lainnya tidak hadir.
	ss, _ := ListSessions(ctx, e.tx, e.a.ID, ex.ID)
	parts, _ := ListParticipants(ctx, e.tx, e.a.ID, ss[0].ID)
	e.tx.Exec(ctx, `UPDATE student_credentials SET device_id = gen_random_uuid() WHERE exam_session_id = $1 AND student_id = $2`, ss[0].ID, parts[0].StudentID)
	after := start.Add(2 * time.Hour)
	cands, err := MakeupCandidates(ctx, e.tx, ex, after)
	if err != nil {
		t.Fatal(err)
	}
	var absent []string
	variantOf := map[string]string{}
	for _, c := range cands {
		variantOf[c.StudentID] = c.Variant
		if c.Absent {
			absent = append(absent, c.ID)
			if c.StudentID == parts[0].StudentID {
				t.Error("present student marked absent")
			}
		}
	}
	if len(absent) != 3 {
		t.Fatalf("absent = %d", len(absent))
	}
	// Sebelum sesi mulai, belum ada yang dianggap tidak hadir.
	if early, _ := MakeupCandidates(ctx, e.tx, ex, now); early[0].Absent {
		t.Error("absent before the session started")
	}
	mk, err := CreateMakeupSession(ctx, e.tx, e.owner, ex.ID, MakeupForm{Participants: absent, Start: after.Add(24 * time.Hour), End: after.Add(25 * time.Hour)}, after)
	if err != nil {
		t.Fatal(err)
	}
	if mk.Kind != SessionMakeup || mk.ExamID != ex.ID || mk.Participants != 3 {
		t.Errorf("makeup = %+v", mk)
	}
	mps, _ := ListParticipants(ctx, e.tx, e.a.ID, mk.ID)
	for _, p := range mps {
		if p.Variant == variantOf[p.StudentID] || p.Variant == "" {
			t.Errorf("%s makeup variant %s, main %s", p.Name, p.Variant, variantOf[p.StudentID])
		}
	}
	// Penjadwalan ulang tidak mengubah varian susulan; peserta sudah
	// bersusulan tidak muncul lagi.
	assignParticipantVariants(ctx, e.tx, ex.ID)
	again, _ := ListParticipants(ctx, e.tx, e.a.ID, mk.ID)
	for i := range again {
		if again[i].Variant != mps[i].Variant {
			t.Error("makeup variant reassigned")
		}
	}
	if c2, _ := MakeupCandidates(ctx, e.tx, ex, after); len(c2) != 1 {
		t.Errorf("candidates after makeup = %d", len(c2))
	}
	hist, _ := FollowUpHistory(ctx, e.tx, e.a.ID, ex.ID)
	n := 0
	for _, h := range hist {
		if strings.Contains(h.Summary(), "Susulan") {
			n++
		}
	}
	if n != 3 {
		t.Errorf("history with makeup = %d", n)
	}
}

func TestPlanRemedialAndRules(t *testing.T) {
	ms := []StudentMastery{
		{StudentID: "s1", Score: 60, Objectives: map[string]float64{"tp1": 50, "tp2": 90, "tp3": 70}},
		{StudentID: "s2", Score: 80, Objectives: map[string]float64{"tp1": 40}},
		{StudentID: "s3", Score: 74, Objectives: map[string]float64{"tp1": 80, "tp2": 76}},
	}
	p := PlanRemedial(ms, 75)
	if strings.Join(p.Students, ",") != "s1,s3" || strings.Join(p.Objectives, ",") != "tp1,tp2,tp3" {
		t.Errorf("plan = %+v", p)
	}
	if strings.Join(p.PerStudent["s1"], ",") != "tp1,tp3" || strings.Join(p.PerStudent["s3"], ",") != "tp1,tp2" {
		t.Errorf("per student = %+v", p.PerStudent)
	}
	parent := []VariantItem{
		{Q: PoolQuestion{ObjectiveID: "tp1", BankID: "b1"}}, {Q: PoolQuestion{ObjectiveID: "tp1", BankID: "b1"}},
		{Q: PoolQuestion{ObjectiveID: "tp1", BankID: "b2"}}, {Q: PoolQuestion{ObjectiveID: "tp3", BankID: "b2"}},
	}
	rules := RemedialRules([]string{"tp1", "tp3", "tp9"}, parent, 2)
	if r := rules["tp1"]; r.BankID != "b1" || r.Count != 3 || r.ObjectiveID != "tp1" {
		t.Errorf("tp1 rule = %+v", r)
	}
	if r := rules["tp3"]; r.Count != 2 || r.BankID != "b2" {
		t.Errorf("tp3 rule = %+v", r)
	}
	if _, ok := rules["tp9"]; ok {
		t.Error("rule for objective not in parent")
	}
}

// Nilai remedial tidak menurunkan dan dibatasi KKTP sesuai aturan sekolah.
func TestFollowUpScores(t *testing.T) {
	for _, c := range []struct {
		orig, rem float64
		policy    string
		want      float64
	}{
		{60, 90, "maks_kktp", 75},
		{60, 70, "maks_kktp", 70},
		{60, 50, "maks_kktp", 60},
		{80, 90, "maks_kktp", 80},
		{60, 90, "tertinggi", 90},
		{60, 50, "tertinggi", 60},
	} {
		if got := RemedialReportScore(c.orig, c.rem, 75, c.policy); got != c.want {
			t.Errorf("remedial(%v, %v, %s) = %v, want %v", c.orig, c.rem, c.policy, got, c.want)
		}
	}
	if EnrichmentReportScore(85, 70) != 85 || EnrichmentReportScore(85, 95) != 95 {
		t.Error("enrichment lowers or ignores score")
	}
}

// Remedial terfokus TP: satu bagian per TP dengan aturan acak dari bank
// yang sama, peserta terpilih dengan TP fokus.
func TestCreateRemedialExam(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	var tp1, tp2 string
	for i, dst := range []*string{&tp1, &tp2} {
		if err := e.tx.QueryRow(ctx, `INSERT INTO learning_objectives (school_id, subject_id, grade_level, code, description, created_by)
			VALUES ($1, $2, 7, $3, 'TP uji', $4) RETURNING id`, e.a.ID, e.a.SubjectID, "3."+itoa(i+1), e.a.Teacher.ID).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	var ids []string
	for i := 0; i < 6; i++ {
		q := sampleQuestion(KindMC)
		q.BodySrc = "<p>Soal TP " + itoa(i) + "</p>"
		q.BodyHTML = q.BodySrc
		q.ObjectiveID = []string{tp1, tp1, tp1, tp1, tp2, tp2}[i]
		c, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, q, now)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	parent := e.scheduledExam(t, now.Add(time.Hour), ids[0], ids[1], ids[4])
	student := e.a.StudentIDs[0]
	start := now.Add(48 * time.Hour)
	fu, err := CreateFollowUp(ctx, e.tx, e.owner, parent.ID, FollowUpForm{Kind: FollowRemedial, Students: []string{student}, Objectives: []string{tp1},
		Start: start, End: start.Add(time.Hour)}, now)
	if err != nil {
		t.Fatal(err)
	}
	if fu.ParentExamID != parent.ID || fu.FollowUp != FollowRemedial || fu.Status != ExamDraft {
		t.Errorf("followup = %+v", fu)
	}
	p, _ := BuildPlan(ctx, e.tx, fu)
	c := p.Composition()
	if len(p.Sections) != 1 || !strings.Contains(p.Sections[0].Title, "3.1") || c.Count != 2 || c.ByObjective[tp1] != 2 {
		t.Errorf("remedial plan: sections=%d count=%d tp1=%d", len(p.Sections), c.Count, c.ByObjective[tp1])
	}
	var focus []string
	e.tx.QueryRow(ctx, `SELECT p.focus_objectives::text[] FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id
		WHERE x.exam_id = $1`, fu.ID).Scan(&focus)
	if len(focus) != 1 || focus[0] != tp1 {
		t.Errorf("focus = %v", focus)
	}
	// Siswa yang bukan peserta ujian induk ditolak.
	if _, err := CreateFollowUp(ctx, e.tx, e.owner, parent.ID, FollowUpForm{Kind: FollowRemedial, Students: []string{e.b.StudentIDs[0]},
		Start: start, End: start.Add(time.Hour)}, now); err == nil {
		t.Error("followup for non-participant accepted")
	}
	// Pengayaan: draf kosong untuk disusun guru.
	en, err := CreateFollowUp(ctx, e.tx, e.owner, parent.ID, FollowUpForm{Kind: FollowEnrichment, Students: []string{student}, Start: start.Add(24 * time.Hour), End: start.Add(25 * time.Hour)}, now)
	if err != nil || en.FollowUp != FollowEnrichment {
		t.Fatalf("enrichment = %+v, %v", en, err)
	}
	if fus, _ := ListFollowUps(ctx, e.tx, e.a.ID, parent.ID); len(fus) != 2 {
		t.Errorf("followups = %d", len(fus))
	}
	hist, _ := FollowUpHistory(ctx, e.tx, e.a.ID, parent.ID)
	if len(hist) != 1 || hist[0].Summary() != "Utama → Remedial → Pengayaan" {
		t.Errorf("history = %+v", hist)
	}
	if body := e.getAs("/ujian/paket/"+parent.ID+"?tab=lanjut", e.a.Teacher.ID, "guru").Body.String(); !strings.Contains(body, "Remedial · ") {
		t.Error("followup tab missing remedial exam")
	}
}
