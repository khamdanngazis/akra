package exam

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/exam/mathrender"
	"akra/internal/platform/database"
)

// Paket "Coba halaman ujian" (T136, nama sejak T159): setiap sekolah mendapat paket latihan
// antarmuka berisi semua jenis soal, agar siswa mencoba halaman ujian
// (ragu-ragu, grid nomor, offline, kumpulkan) sebelum ujian sungguhan.

// practiceQuestions adalah isi paket latihan: satu soal per jenis.
var practiceQuestions = []url.Values{
	{"kind": {"pg"}, "body": {`Hasil dari \(\frac{1}{2} + \frac{1}{4}\) adalah …`}, "choice_0": {`\(\frac{3}{4}\)`}, "choice_1": {`\(\frac{2}{6}\)`},
		"choice_2": {`\(\frac{1}{8}\)`}, "choice_3": {"1"}, "correct": {"0"}, "difficulty": {"mudah"},
		"explanation": {`Samakan penyebut: \(\frac{2}{4} + \frac{1}{4} = \frac{3}{4}\).`}},
	{"kind": {"pg_kompleks"}, "body": {"Pilih semua hewan yang hidup di air."}, "choice_0": {"Ikan"}, "choice_1": {"Kucing"},
		"choice_2": {"Lumba-lumba"}, "choice_3": {"Ayam"}, "correct": {"0", "2"}, "scoring": {"proporsional"}, "difficulty": {"mudah"}},
	{"kind": {"benar_salah"}, "body": {"Tentukan benar atau salah pernyataan berikut."}, "statement_0": {"Matahari terbit di timur."},
		"truth_0": {"B"}, "statement_1": {"Satu minggu ada delapan hari."}, "truth_1": {"S"}, "difficulty": {"mudah"}},
	{"kind": {"isian"}, "body": {"Ibu kota Indonesia saat ini adalah …"}, "accepted": {"Jakarta | DKI Jakarta"}, "difficulty": {"mudah"}},
	{"kind": {"menjodohkan"}, "body": {"Jodohkan hewan dengan makanannya."}, "left_0": {"Sapi"}, "right_0": {"Rumput"},
		"left_1": {"Kucing"}, "right_1": {"Ikan"}, "distractor_0": {"Batu"}, "difficulty": {"mudah"}},
	{"kind": {"esai"}, "body": {"Ceritakan singkat kegiatanmu pagi ini (2–3 kalimat)."}, "rubric": {"Jawaban runtut dan lengkap."},
		"points": {"5"}, "difficulty": {"mudah"}},
}

// PracticeName adalah nama paket latihan bawaan.
const PracticeName = "Coba Halaman Ujian"

// PracticeBankName adalah nama bank contoh paket latihan bawaan (T161).
const PracticeBankName = "Contoh — " + PracticeName

// GetPractice mengembalikan paket latihan bawaan sekolah, bila ada.
func GetPractice(ctx context.Context, q database.DBTX, schoolID string) (Exam, bool, error) {
	var id string
	err := q.QueryRow(ctx, `SELECT id FROM exams WHERE school_id = $1 AND builtin AND archived_at IS NULL`, schoolID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Exam{}, false, nil
	}
	if err != nil {
		return Exam{}, false, fmt.Errorf("exam: get practice: %w", err)
	}
	e, err := GetExam(ctx, q, schoolID, id)
	return e, err == nil, err
}

// EnsurePractice mengembalikan paket latihan bawaan, membuatnya bila belum
// ada: bank contoh "Contoh — Coba Halaman Ujian" dengan enam soal (semua
// jenis) dan ujian latihan berisi soal-soal itu. sa menjadi pembuatnya.
func EnsurePractice(ctx context.Context, db database.DB, sa StaffActor, math *mathrender.Renderer, now time.Time) (Exam, error) {
	if e, ok, err := GetPractice(ctx, db, sa.SchoolID); err != nil || ok {
		return e, err
	}
	var out Exam
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var bankID string
		if err := tx.QueryRow(ctx, `INSERT INTO exam_banks (school_id, name, created_by, sample) VALUES ($1, $2, $3, true) RETURNING id`,
			sa.SchoolID, PracticeBankName, sa.UserID).Scan(&bankID); err != nil {
			return fmt.Errorf("exam: practice bank: %w", err)
		}
		writer := sa
		writer.Roles = append(append([]string(nil), sa.Roles...), roleAdmin) // pembuat sistem, bukan hak pengguna
		var ids []string
		for _, v := range practiceQuestions {
			q, _, err := BuildQuestion(ParseQuestionForm(v), math)
			if err != nil {
				return fmt.Errorf("exam: practice question: %w", err)
			}
			c, err := createQuestionTx(ctx, tx, writer, bankID, q, now)
			if err != nil {
				return err
			}
			ids = append(ids, c.ID)
		}
		e := defaultsFor(PurposePractice)
		e.Name, e.OwnerID = PracticeName, sa.UserID
		created, err := insertExam(ctx, tx, sa, e, "")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exams SET builtin = true, status = 'terjadwal', scheduled_at = now(), wizard_step = 'cek' WHERE id = $1`, created.ID); err != nil {
			return fmt.Errorf("exam: mark practice: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_items (school_id, exam_id, section_id, position, kind, question_id)
			SELECT $1, $2, s.id, q.n, 'soal', q.id FROM exam_sections s, unnest($3::uuid[]) WITH ORDINALITY AS q(id, n)
			WHERE s.exam_id = $2`, sa.SchoolID, created.ID, ids); err != nil {
			return fmt.Errorf("exam: practice items: %w", err)
		}
		if out, err = GetExam(ctx, tx, sa.SchoolID, created.ID); err != nil {
			return err
		}
		_, err = Reassemble(ctx, tx, out)
		return err
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		// Dibuat bersamaan oleh pengguna lain.
		e, _, err := GetPractice(ctx, db, sa.SchoolID)
		return e, err
	}
	return out, err
}

// canOpenPractice melaporkan apakah a boleh membuka latihan untuk kelas:
// admin/koordinator semua kelas, guru kelas yang ia ajar (mapel apa pun).
func canOpenPractice(a Actor, classID string, now time.Time) bool {
	if a.Can(ActClassExam, Resource{ClassID: "*"}, now) >= Write {
		return true
	}
	for _, c := range a.Classes {
		if len(c) > len(classID) && c[:len(classID)] == classID && c[len(classID)] == '|' {
			return true
		}
	}
	return false
}

// PracticeWindow adalah lama sesi latihan yang dibuka.
const PracticeWindow = 3 * 24 * time.Hour

// OpenPractice membuka ujian latihan untuk satu kelas: sesi mulai sekarang
// selama PracticeWindow, lokasi jarak jauh (boleh dicoba dari rumah),
// dengan pembuka sebagai pengawas.
func OpenPractice(ctx context.Context, db database.DB, sa StaffActor, practice Exam, classID string, now time.Time) (Session, error) {
	if !practice.Builtin {
		return Session{}, ErrNotFound
	}
	var out Session
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var ok bool
		if database.IsUUID(classID) {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM classes c JOIN academic_years y ON y.id = c.academic_year_id AND y.is_active
				WHERE c.school_id = $1 AND c.id = $2)`, sa.SchoolID, classID).Scan(&ok); err != nil {
				return fmt.Errorf("exam: check class: %w", err)
			}
		}
		if !ok {
			return ErrNotFound
		}
		if !canOpenPractice(sa.Actor, classID, now) {
			return ErrNotYourClass
		}
		var sid string
		err := tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, class_id, label, location, starts_at, ends_at, created_by)
			VALUES ($1, $2, $3, '', 'jarak_jauh', $4, $5, $6) RETURNING id`, sa.SchoolID, practice.ID, classID, now, now.Add(PracticeWindow), sa.UserID).Scan(&sid)
		if err != nil {
			return fmt.Errorf("exam: open practice: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no)
			SELECT $1, $2, s.id, s.nis FROM enrollments en JOIN students s ON s.id = en.student_id AND s.school_id = en.school_id
			WHERE en.school_id = $1 AND en.class_id = $3 AND en.end_date IS NULL AND s.status = 'aktif'`, sa.SchoolID, sid, classID); err != nil {
			return fmt.Errorf("exam: practice participants: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_session_proctors (school_id, session_id, user_id, assigned_by) VALUES ($1, $2, $3, $3)`,
			sa.SchoolID, sid, sa.UserID); err != nil {
			return fmt.Errorf("exam: practice proctor: %w", err)
		}
		if err := assignParticipantVariants(ctx, tx, practice.ID); err != nil {
			return err
		}
		var err2 error
		out, err2 = GetSession(ctx, tx, sa.SchoolID, sid)
		if err2 != nil {
			return err2
		}
		return recordAudit(ctx, tx, sa, "exam.practice_open", "exam_session", sid, nil, map[string]any{"class": classID})
	})
	return out, err
}
