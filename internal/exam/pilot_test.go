package exam

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPilotReport(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	var now time.Time
	if err := e.tx.QueryRow(ctx, `SELECT started_at FROM exam_attempts WHERE id = $1`, e.attA).Scan(&now); err != nil {
		t.Fatal(err)
	}
	from, to := now.Add(-time.Hour), now.Add(time.Hour)

	empty, err := BuildPilotReport(ctx, e.tx, e.a.ID, from, to, 10)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Attempts != 2 || empty.Assisted != 0 || empty.LostAnswers != 0 || empty.UnassistedPct() != 100 {
		t.Fatalf("before = %+v", empty)
	}

	// attA: dikumpulkan, lalu satu perubahan tiba terlambat (hilang).
	var q2 string
	if err := e.tx.QueryRow(ctx, `SELECT question_order[2]::text FROM exam_attempts WHERE id = $1`, e.attA).Scan(&q2); err != nil {
		t.Fatal(err)
	}
	if rec := e.form("hpA", "/u/a/"+e.attA+"/kumpul", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("submit = %d", rec.Code)
	}
	if _, err := RecordLostAnswers(ctx, e.tx, e.a.ID, e.attA, []AnswerChange{{QuestionID: q2, Value: json.RawMessage(`"x"`), Seq: 7}}, now); err != nil {
		t.Fatal(err)
	}
	// attB: dibantu pengawas (reset login peserta).
	if _, err := e.tx.Exec(ctx, `INSERT INTO audit_logs (school_id, user_id, action, entity_type, entity_id)
		SELECT t.school_id, $2, 'exam.participant_reset_login', 'exam_session_participant', t.participant_id::text FROM exam_attempts t WHERE t.id = $1`,
		e.attB, e.a.Admin.ID); err != nil {
		t.Fatal(err)
	}
	r, err := BuildPilotReport(ctx, e.tx, e.a.ID, from, to, 10)
	if err != nil {
		t.Fatal(err)
	}
	if r.Attempts != 2 || r.Participants != 2 || r.Submitted != 1 || r.LostAnswers != 1 || r.LostAttempts != 1 || r.Assisted != 1 || r.UnassistedPct() != 50 {
		t.Errorf("report = %+v", r)
	}
	// Peserta mengumpulkan sendiri tidak dihitung sebagai dibantu; pengawas
	// mengumpulkan dihitung.
	if _, err := e.tx.Exec(ctx, `INSERT INTO audit_logs (school_id, user_id, action, entity_type, entity_id) VALUES ($1, $2, 'exam.attempt_submit', 'exam_attempt', $3)`,
		e.a.ID, e.a.Admin.ID, e.attA); err != nil {
		t.Fatal(err)
	}
	if r2, _ := BuildPilotReport(ctx, e.tx, e.a.ID, from, to, 0); r2.Assisted != 2 || r2.Sample != nil {
		t.Errorf("assisted after proctor submit = %d", r2.Assisted)
	}
	// Di luar rentang: kosong.
	if old, _ := BuildPilotReport(ctx, e.tx, e.a.ID, from.AddDate(0, -1, 0), from.AddDate(0, 0, -1), 10); old.Attempts != 0 {
		t.Errorf("outside window attempts = %d", old.Attempts)
	}

	var b bytes.Buffer
	if err := r.WriteMarkdown(&b, 1, "https://akra.test"); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	for _, want := range []string{"| Jawaban hilang | 0 | 1 soal di 1 attempt | ❌ |", "50,0% (1 dibantu)", "| Tiket bantuan per 100 peserta | ≤ 1,0 | 50,0 (1 tiket) | ❌ |"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestFormatX100(t *testing.T) {
	for in, want := range map[int64]string{8000: "80", 7550: "75,5", 7505: "75,05", 0: "0", 10000: "100"} {
		if got := formatX100(in); got != want {
			t.Errorf("formatX100(%d) = %q, want %q", in, got, want)
		}
	}
}
