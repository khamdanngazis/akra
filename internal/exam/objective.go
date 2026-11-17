package exam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

// Objective adalah satu tujuan pembelajaran (TP).
type Objective struct {
	ID          string
	SubjectID   string
	GradeLevel  int
	Code        string
	Description string
	SortOrder   int
	// Questions adalah jumlah soal aktif yang memakai TP ini.
	Questions int
}

// Label mengembalikan "kode — deskripsi" untuk pilihan form.
func (o Objective) Label() string { return o.Code + " — " + Snippet(o.Description, 80) }

// ListObjectives mengembalikan TP aktif mapel subjectID di tingkat grade
// (0 = semua tingkat), urut tingkat lalu urutan.
func ListObjectives(ctx context.Context, q database.DBTX, schoolID, subjectID string, grade int) ([]Objective, error) {
	if !database.IsUUID(subjectID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT o.id, o.subject_id, o.grade_level, o.code, o.description, o.sort_order,
		       (SELECT count(*) FROM exam_questions q WHERE q.school_id = o.school_id AND q.objective_id = o.id AND q.archived_at IS NULL)
		FROM learning_objectives o
		WHERE o.school_id = $1 AND o.subject_id = $2 AND ($3 = 0 OR o.grade_level = $3) AND o.archived_at IS NULL
		ORDER BY o.grade_level, o.sort_order, lower(o.code)`, schoolID, subjectID, grade)
	if err != nil {
		return nil, fmt.Errorf("exam: list objectives: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Objective, error) {
		var o Objective
		err := r.Scan(&o.ID, &o.SubjectID, &o.GradeLevel, &o.Code, &o.Description, &o.SortOrder, &o.Questions)
		return o, err
	})
	if err != nil {
		return nil, fmt.Errorf("exam: list objectives: %w", err)
	}
	return out, nil
}

// getObjective mengembalikan TP aktif milik sekolah, atau ErrNotFound.
func getObjective(ctx context.Context, q database.DBTX, schoolID, id string) (Objective, error) {
	if !database.IsUUID(id) {
		return Objective{}, ErrNotFound
	}
	var o Objective
	err := q.QueryRow(ctx, `SELECT id, subject_id, grade_level, code, description, sort_order FROM learning_objectives
		WHERE school_id = $1 AND id = $2 AND archived_at IS NULL`, schoolID, id).
		Scan(&o.ID, &o.SubjectID, &o.GradeLevel, &o.Code, &o.Description, &o.SortOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return Objective{}, ErrNotFound
	}
	if err != nil {
		return Objective{}, fmt.Errorf("exam: get objective: %w", err)
	}
	return o, nil
}

// ObjectiveForm adalah isian satu TP.
type ObjectiveForm struct {
	SubjectID   string
	GradeLevel  string
	Code        string
	Description string
}

func validateObjective(f ObjectiveForm) (grade int, code, desc string, err error) {
	fields := map[string]string{}
	grade, ok := parseGrade(f.GradeLevel)
	if !ok || grade == 0 {
		fields["grade_level"] = "Pilih tingkat 1–12."
	}
	code = strings.Join(strings.Fields(f.Code), " ")
	switch {
	case code == "":
		fields["code"] = "Kode TP wajib diisi, mis. TP 3.2."
	case utf8.RuneCountInString(code) > 30:
		fields["code"] = "Kode TP maksimal 30 karakter."
	}
	desc = strings.TrimSpace(f.Description)
	switch {
	case desc == "":
		fields["description"] = "Deskripsi TP wajib diisi."
	case utf8.RuneCountInString(desc) > 1000:
		fields["description"] = "Deskripsi TP maksimal 1.000 karakter."
	}
	if len(fields) > 0 {
		return 0, "", "", &FieldError{Fields: fields}
	}
	return grade, code, desc, nil
}

// checkSubjectWrite memastikan mapel milik sekolah dan sa boleh menulis
// bank mapel itu (TP diatur pengampu mapel, koordinator, admin_tu).
func checkSubjectWrite(ctx context.Context, q database.DBTX, sa StaffActor, subjectID string, now time.Time) error {
	if !database.IsUUID(subjectID) {
		return ErrNotFound
	}
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM subjects WHERE school_id = $1 AND id = $2)`, sa.SchoolID, subjectID).Scan(&ok); err != nil {
		return fmt.Errorf("exam: check subject: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	if sa.Can(ActBank, Resource{SubjectID: subjectID}, now) < Write {
		return ErrNotEditable
	}
	return nil
}

// ErrDuplicateObjective: kode TP sudah ada di mapel dan tingkat yang sama.
var ErrDuplicateObjective = &FieldError{Fields: map[string]string{"code": "Kode TP ini sudah ada di mapel dan tingkat yang sama."}}

// CreateObjective menambah satu TP di akhir urutan.
func CreateObjective(ctx context.Context, db database.DB, sa StaffActor, f ObjectiveForm, now time.Time) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := checkSubjectWrite(ctx, tx, sa, f.SubjectID, now); err != nil {
			return err
		}
		grade, code, desc, err := validateObjective(f)
		if err != nil {
			return err
		}
		id, err = insertObjective(ctx, tx, sa, f.SubjectID, grade, code, desc)
		if err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "objective.create", "learning_objective", id, nil, map[string]any{"code": code, "grade_level": grade})
	})
	return id, err
}

func insertObjective(ctx context.Context, tx pgx.Tx, sa StaffActor, subjectID string, grade int, code, desc string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO learning_objectives (school_id, subject_id, grade_level, code, description, sort_order, created_by)
		VALUES ($1, $2, $3, $4, $5,
		        (SELECT coalesce(max(sort_order), 0) + 1 FROM learning_objectives WHERE school_id = $1 AND subject_id = $2 AND grade_level = $3),
		        $6)
		RETURNING id`, sa.SchoolID, subjectID, grade, code, desc, sa.UserID).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return "", ErrDuplicateObjective
	}
	if err != nil {
		return "", fmt.Errorf("exam: create objective: %w", err)
	}
	return id, nil
}

// PasteResult adalah hasil tempel banyak TP sekaligus.
type PasteResult struct {
	Added   int
	Skipped []string // "baris 3: alasan"
}

// PasteObjectives menambah banyak TP dari teks tempelan (dari Excel atau
// Word): satu TP per baris, kode dan deskripsi dipisah tab, " - ", atau
// titik dua. Kode yang sudah ada dilewati.
func PasteObjectives(ctx context.Context, db database.DB, sa StaffActor, subjectID, gradeLevel, text string, now time.Time) (PasteResult, error) {
	var res PasteResult
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := checkSubjectWrite(ctx, tx, sa, subjectID, now); err != nil {
			return err
		}
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		if len(lines) > 300 {
			return &FieldError{Fields: map[string]string{"paste": "Maksimal 300 baris sekali tempel."}}
		}
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			code, desc, ok := splitObjectiveLine(line)
			if !ok {
				res.Skipped = append(res.Skipped, fmt.Sprintf("baris %d: kode dan deskripsi tidak bisa dipisahkan", i+1))
				continue
			}
			grade, code, desc, err := validateObjective(ObjectiveForm{GradeLevel: gradeLevel, Code: code, Description: desc})
			if err != nil {
				var fe *FieldError
				if errors.As(err, &fe) {
					if msg, ok := fe.Fields["grade_level"]; ok {
						return &FieldError{Fields: map[string]string{"grade_level": msg}}
					}
					for _, msg := range fe.Fields {
						res.Skipped = append(res.Skipped, fmt.Sprintf("baris %d: %s", i+1, msg))
						break
					}
					continue
				}
				return err
			}
			if _, err := tx.Exec(ctx, `SAVEPOINT tp`); err != nil {
				return err
			}
			if _, err := insertObjective(ctx, tx, sa, subjectID, grade, code, desc); err != nil {
				if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT tp`); rerr != nil {
					return rerr
				}
				if err == ErrDuplicateObjective {
					res.Skipped = append(res.Skipped, fmt.Sprintf("baris %d: kode %s sudah ada", i+1, code))
					continue
				}
				return err
			}
			res.Added++
		}
		if res.Added == 0 {
			return nil
		}
		return recordAudit(ctx, tx, sa, "objective.paste", "subject", subjectID, nil, map[string]any{"added": res.Added, "grade_level": gradeLevel})
	})
	return res, err
}

// splitObjectiveLine memisahkan "TP 3.2<tab>Deskripsi", "TP 3.2 - Deskripsi",
// atau "TP 3.2: Deskripsi".
func splitObjectiveLine(line string) (code, desc string, ok bool) {
	for _, sep := range []string{"\t", " – ", " - ", ": "} {
		if c, d, found := strings.Cut(line, sep); found && strings.TrimSpace(c) != "" && strings.TrimSpace(d) != "" {
			return strings.TrimSpace(c), strings.TrimSpace(d), true
		}
	}
	return "", "", false
}

// UpdateObjective mengubah kode dan deskripsi TP.
func UpdateObjective(ctx context.Context, db database.DB, sa StaffActor, id string, f ObjectiveForm, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		o, err := getObjective(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		if err := checkSubjectWrite(ctx, tx, sa, o.SubjectID, now); err != nil {
			return err
		}
		f.GradeLevel = fmt.Sprint(o.GradeLevel)
		_, code, desc, err := validateObjective(f)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE learning_objectives SET code = $3, description = $4, updated_at = now() WHERE school_id = $1 AND id = $2`,
			sa.SchoolID, id, code, desc)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicateObjective
		}
		if err != nil {
			return fmt.Errorf("exam: update objective: %w", err)
		}
		return recordAudit(ctx, tx, sa, "objective.update", "learning_objective", id,
			map[string]any{"code": o.Code, "description": o.Description}, map[string]any{"code": code, "description": desc})
	})
}

// ArchiveObjective mengarsipkan TP. Soal yang memakainya tetap merujuk
// (riwayat analisis tidak hilang).
func ArchiveObjective(ctx context.Context, db database.DB, sa StaffActor, id string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		o, err := getObjective(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		if err := checkSubjectWrite(ctx, tx, sa, o.SubjectID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE learning_objectives SET archived_at = now(), updated_at = now() WHERE school_id = $1 AND id = $2`,
			sa.SchoolID, id); err != nil {
			return fmt.Errorf("exam: archive objective: %w", err)
		}
		return recordAudit(ctx, tx, sa, "objective.archive", "learning_objective", id, map[string]any{"code": o.Code}, nil)
	})
}
