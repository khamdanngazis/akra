package exam

import (
	"context"
	"fmt"

	"akra/internal/platform/database"
)

// Sinkron nilai tes seleksi online (T149): nilai attempt terakhir
// pendaftar pada ujian yang ditautkan ke tes gelombang (admission_tests.
// exam_id) ditulis ke applicant_test_scores dengan sumber ujian_online,
// diskalakan ke nilai maksimal tes. Nilai yang belum final (menunggu
// koreksi esai) ditulis NULL (belum lengkap). Peserta yang tidak hadir
// tidak punya attempt sehingga tetap tanpa nilai. Peringkat pendaftaran
// membaca applicant_test_scores, jadi langsung ikut berubah.

// admissionScoreUpsert menulis nilai untuk attempt yang dipilih CTE
// "pick" (kolom id). Hanya attempt terakhir yang sudah dikumpulkan per
// peserta; baris hanya ditulis bila berubah.
const admissionScoreUpsert = `
	, src AS (
		SELECT t.id, t.school_id, t.exam_id, p.applicant_id, t.score_x100, t.score_status, e.owner_id
		FROM pick
		JOIN exam_attempts t ON t.id = pick.id AND t.status = 'dikumpulkan'
		JOIN exam_session_participants p ON p.id = t.participant_id AND p.applicant_id IS NOT NULL
		JOIN exams e ON e.id = t.exam_id AND e.purpose = 'seleksi'
		WHERE t.attempt_no = (SELECT max(t2.attempt_no) FROM exam_attempts t2 WHERE t2.participant_id = t.participant_id))
	INSERT INTO applicant_test_scores (school_id, applicant_id, test_id, score, recorded_by, source, attempt_id)
	SELECT src.school_id, src.applicant_id, x.id,
	       CASE WHEN src.score_status = 'final' THEN round(src.score_x100::numeric * x.max_score / 10000, 2) END,
	       src.owner_id, 'ujian_online', src.id
	FROM src
	JOIN admission_tests x ON x.school_id = src.school_id AND x.exam_id = src.exam_id
	JOIN applicants a ON a.id = src.applicant_id AND a.period_id = x.period_id
	ON CONFLICT (applicant_id, test_id) DO UPDATE SET score = excluded.score, source = excluded.source, attempt_id = excluded.attempt_id,
		recorded_by = excluded.recorded_by, updated_at = now()
	WHERE (applicant_test_scores.score, applicant_test_scores.source, applicant_test_scores.attempt_id)
	      IS DISTINCT FROM (excluded.score, excluded.source, excluded.attempt_id)`

// syncAdmissionScore menulis nilai satu attempt ke tes seleksi yang
// memakai ujiannya (bila ada). Dipanggil setiap kali nilai attempt
// disimpan, termasuk nilai ulang, koreksi esai, dan nilai manual.
func syncAdmissionScore(ctx context.Context, q database.DBTX, attemptID string) error {
	if _, err := q.Exec(ctx, `WITH pick AS (SELECT $1::uuid AS id)`+admissionScoreUpsert, attemptID); err != nil {
		return fmt.Errorf("exam: sync admission score: %w", err)
	}
	return nil
}

// SyncAdmissionExam menulis nilai semua attempt ujian examID ke tes
// seleksi yang memakainya, mis. saat ujian baru ditautkan ke tes.
// Mengembalikan jumlah baris nilai yang berubah.
func SyncAdmissionExam(ctx context.Context, q database.DBTX, schoolID, examID string) (int, error) {
	tag, err := q.Exec(ctx, `WITH pick AS (SELECT id FROM exam_attempts WHERE school_id = $1 AND exam_id = $2)`+admissionScoreUpsert, schoolID, examID)
	if err != nil {
		return 0, fmt.Errorf("exam: sync admission exam: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
