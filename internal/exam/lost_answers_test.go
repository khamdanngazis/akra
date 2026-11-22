package exam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// Perubahan jawaban yang tiba setelah attempt ditutup tercatat sebagai
// jawaban hilang, kecuali yang sudah tersimpan (T155).
func TestLostAnswersRecorded(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	var q1, q2 string
	if err := e.tx.QueryRow(ctx, `SELECT question_order[1]::text, question_order[2]::text FROM exam_attempts WHERE id = $1`, e.attA).Scan(&q1, &q2); err != nil {
		t.Fatal(err)
	}
	token, _ := pageState(t, e.do("hpA", http.MethodGet, "/u/a/"+e.attA, "", "").Body.String())["token"].(string)
	change := func(q, v string, seq int) string {
		b, _ := json.Marshal(map[string]any{"q": q, "v": v, "f": false, "s": seq})
		return string(b)
	}
	save := func(changes ...string) int {
		body := `{"changes":[`
		for i, c := range changes {
			if i > 0 {
				body += ","
			}
			body += c
		}
		return e.saveAs("/u/a/"+e.attA+"/simpan", token, body+`],"events":[],"pos":0}`).Code
	}
	if code := save(change(q1, "satu", 1)); code != http.StatusOK {
		t.Fatalf("save = %d", code)
	}
	if rec := e.form("hpA", "/u/a/"+e.attA+"/kumpul", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("submit = %d", rec.Code)
	}
	lost := func() (n int, value string) {
		t.Helper()
		if err := e.tx.QueryRow(ctx, `SELECT count(*), coalesce(max(value #>> '{}'), '') FROM exam_lost_answers WHERE attempt_id = $1`, e.attA).Scan(&n, &value); err != nil {
			t.Fatal(err)
		}
		return
	}
	// q1 seq 1 sudah tersimpan (kirim ulang), q2 belum pernah: hanya q2.
	if code := save(change(q1, "satu", 1), change(q2, "dua", 2)); code != http.StatusConflict {
		t.Fatalf("late save = %d, want 409", code)
	}
	if n, v := lost(); n != 1 || v != "dua" {
		t.Fatalf("lost = %d %q, want 1 dua", n, v)
	}
	// Kirim ulang yang sama tidak menambah; versi lebih baru menggantikan.
	save(change(q2, "dua", 2))
	save(change(q2, "lima", 5))
	save(change(q2, "lama", 3))
	if n, v := lost(); n != 1 || v != "lima" {
		t.Fatalf("lost after resend = %d %q, want 1 lima", n, v)
	}
	// Attempt yang masih berjalan (mis. ditolak karena perangkat lain) tidak
	// dicatat.
	if n, err := RecordLostAnswers(ctx, e.tx, e.a.ID, e.attB, []AnswerChange{{QuestionID: q1, Value: json.RawMessage(`"x"`), Seq: 9}}, time.Now()); err != nil || n != 0 {
		t.Errorf("running attempt recorded %d, %v", n, err)
	}
	// Masa simpan menghapusnya bersama jawaban lain.
	var sess string
	if err := e.tx.QueryRow(ctx, `SELECT p.session_id::text FROM exam_attempts t JOIN exam_session_participants p ON p.id = t.participant_id WHERE t.id = $1`, e.attA).Scan(&sess); err != nil {
		t.Fatal(err)
	}
	if _, err := AnonymizeSession(ctx, e.tx, e.a.ID, sess, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, _ := lost(); n != 0 {
		t.Errorf("lost answers kept after retention: %d", n)
	}
}
