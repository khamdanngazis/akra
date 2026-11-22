package exam

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"akra/internal/platform/database"
)

// Jawaban hilang (T155): perangkat yang masih menyimpan perubahan saat
// attempt sudah ditutup mengirimnya, server menolak (409), dan perubahan
// itu tidak masuk nilai. Dicatat per soal bila server belum punya versi
// dengan seq yang sama atau lebih baru, sehingga kirim ulang yang sudah
// tersimpan tidak terhitung. Attempt yang masih berjalan di perangkat lain
// (pindah perangkat) tidak dicatat: peserta melanjutkan di perangkat baru.

// RecordLostAnswers mencatat perubahan yang ditolak karena attempt sudah
// ditutup. Mengembalikan jumlah soal yang tercatat.
func RecordLostAnswers(ctx context.Context, q database.DBTX, schoolID, attemptID string, changes []AnswerChange, now time.Time) (int, error) {
	if len(changes) == 0 || !database.IsUUID(attemptID) {
		return 0, nil
	}
	latest := map[string]AnswerChange{}
	for _, c := range changes {
		if !database.IsUUID(c.QuestionID) || c.Seq <= 0 || len(c.Value) > MaxAnswerBytes {
			continue
		}
		if p, ok := latest[c.QuestionID]; !ok || c.Seq > p.Seq {
			latest[c.QuestionID] = c
		}
	}
	var qs, vs []string
	var ss []int64
	for id, c := range latest {
		v := strings.TrimSpace(string(c.Value))
		if v == "" {
			v = "null"
		}
		if !json.Valid([]byte(v)) {
			continue
		}
		qs, vs, ss = append(qs, id), append(vs, v), append(ss, c.Seq)
	}
	if len(qs) == 0 {
		return 0, nil
	}
	tag, err := q.Exec(ctx, `
		INSERT INTO exam_lost_answers (school_id, attempt_id, question_id, value, client_seq, reported_at)
		SELECT t.school_id, t.id, u.q, u.v::jsonb, u.s, $3
		FROM exam_attempts t, unnest($4::uuid[], $5::text[], $6::bigint[]) AS u(q, v, s)
		WHERE t.school_id = $1 AND t.id = $2 AND t.status <> 'berjalan' AND u.q = ANY (t.question_order)
		  AND NOT EXISTS (SELECT 1 FROM exam_answers a WHERE a.attempt_id = t.id AND a.question_id = u.q AND a.client_seq >= u.s)
		ON CONFLICT (attempt_id, question_id) DO UPDATE
		SET value = excluded.value, client_seq = excluded.client_seq, reported_at = excluded.reported_at
		WHERE excluded.client_seq > exam_lost_answers.client_seq`,
		schoolID, attemptID, now, qs, vs, ss)
	if err != nil {
		return 0, fmt.Errorf("exam: record lost answers: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
