package exam

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Data ujian untuk ekspor data siswa (hak akses UU PDP, T153): setiap
// attempt siswa dengan ujian, waktu, status, nilai, jawabannya per soal
// (teks soal, jawaban, poin, komentar guru), dan kejadian integritas. Kunci
// jawaban dan pembahasan tidak ikut: itu milik sekolah, bukan data pribadi
// siswa. Data yang sudah dianonimkan (masa simpan) tidak lagi tertaut ke
// siswa sehingga tidak ikut.

// ExportAnswer adalah satu jawaban siswa.
type ExportAnswer struct {
	Number   int      `json:"nomor"`
	Question string   `json:"soal"`
	Kind     string   `json:"jenis"`
	Answer   []string `json:"jawaban"`
	Points   string   `json:"poin,omitempty"`
	Max      int      `json:"poin_maksimal"`
	Comment  string   `json:"komentar_guru,omitempty"`
}

// ExportEvent adalah satu kejadian integritas.
type ExportEvent struct {
	Kind     string    `json:"kejadian"`
	At       time.Time `json:"waktu"`
	Duration string    `json:"lama,omitempty"`
}

// ExportAttempt adalah satu attempt ujian siswa.
type ExportAttempt struct {
	Exam        string         `json:"ujian"`
	Subject     string         `json:"mapel,omitempty"`
	Purpose     string         `json:"jenis_ujian"`
	AttemptNo   int            `json:"percobaan_ke"`
	StartedAt   *time.Time     `json:"mulai,omitempty"`
	SubmittedAt *time.Time     `json:"dikumpulkan,omitempty"`
	Status      string         `json:"status"`
	Score       string         `json:"nilai,omitempty"`
	Answers     []ExportAnswer `json:"jawaban"`
	Events      []ExportEvent  `json:"kejadian"`
}

// StudentExamExport mengembalikan semua attempt ujian siswa studentID.
func StudentExamExport(ctx context.Context, q database.DBTX, schoolID, studentID string) ([]ExportAttempt, error) {
	rows, err := q.Query(ctx, `SELECT t.exam_id::text, t.id::text FROM exam_attempts t
		JOIN exam_session_participants p ON p.id = t.participant_id
		WHERE t.school_id = $1 AND p.student_id = $2 ORDER BY t.started_at, t.attempt_no`, schoolID, studentID)
	if err != nil {
		return nil, fmt.Errorf("exam: student export: %w", err)
	}
	type ref struct{ exam, attempt string }
	refs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ref, error) {
		var x ref
		return x, r.Scan(&x.exam, &x.attempt)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: student export: %w", err)
	}
	out := []ExportAttempt{}
	exams := map[string]Exam{}
	for _, rf := range refs {
		e, ok := exams[rf.exam]
		if !ok {
			if e, err = GetExam(ctx, q, schoolID, rf.exam); err != nil {
				return nil, err
			}
			exams[rf.exam] = e
		}
		p, answers, err := LoadAttemptDetail(ctx, q, e, rf.attempt)
		if err != nil {
			return nil, err
		}
		x := ExportAttempt{Exam: e.Name, Subject: e.SubjectName, Purpose: e.Purpose.Label(), AttemptNo: p.AttemptNo,
			StartedAt: p.StartedAt, SubmittedAt: p.SubmittedAt, Status: p.StatusLabel(), Score: p.Score(), Answers: []ExportAnswer{}, Events: []ExportEvent{}}
		for _, d := range answers {
			if d.Item.Voided {
				continue
			}
			x.Answers = append(x.Answers, ExportAnswer{Number: d.Number, Question: PlainText(d.Item.Question.BodySrc), Kind: d.Item.Question.Kind.Label(),
				Answer: d.Answer, Points: d.EarnedText(), Max: d.Item.Points, Comment: d.Comment})
		}
		events, err := ListAttemptEvents(ctx, q, schoolID, rf.attempt, 1000)
		if err != nil {
			return nil, err
		}
		for _, ev := range events {
			x.Events = append(x.Events, ExportEvent{Kind: ev.Label(), At: ev.OccurredAt, Duration: ev.DurationText()})
		}
		out = append(out, x)
	}
	return out, nil
}

// StudentExamExportAny membungkus StudentExamExport untuk ekspor data siswa
// (paket students tidak bergantung pada modul ujian).
func StudentExamExportAny(ctx context.Context, q database.DBTX, schoolID, studentID string) (any, error) {
	return StudentExamExport(ctx, q, schoolID, studentID)
}
