package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

var (
	// ErrSlotNotFound: slot tidak ada atau milik sekolah lain.
	ErrSlotNotFound = errors.New("schedule: slot not found")
	// ErrOverlap: database menolak slot yang bentrok (constraint EXCLUDE).
	// Service biasanya sudah menolak lebih dulu dengan ConflictError.
	ErrOverlap = errors.New("schedule: slot overlaps another slot")
)

// Slot adalah satu jam pelajaran di jadwal mingguan kelas.
type Slot struct {
	ID             string
	AcademicYearID string
	ClassID        string
	ClassName      string
	SubjectID      string
	SubjectName    string
	SubjectCode    string
	TeacherID      string
	TeacherName    string
	Day            int
	Start, End     Minutes
	CreatedAt      time.Time
}

// slotInput adalah data slot yang sudah divalidasi service.
type slotInput struct {
	AcademicYearID, ClassID, SubjectID, TeacherID string
	Day                                           int
	Start, End                                    Minutes
}

const slotSelect = `
SELECT sl.id, sl.academic_year_id, sl.class_id, c.name, sl.subject_id, s.name, coalesce(s.code, ''), sl.teacher_id, u.full_name,
       sl.day_of_week, extract(hour FROM sl.start_time)::int * 60 + extract(minute FROM sl.start_time)::int,
       extract(hour FROM sl.end_time)::int * 60 + extract(minute FROM sl.end_time)::int, sl.created_at
FROM schedule_slots sl
JOIN classes c ON c.school_id = sl.school_id AND c.id = sl.class_id
JOIN subjects s ON s.school_id = sl.school_id AND s.id = sl.subject_id
JOIN users u ON u.school_id = sl.school_id AND u.id = sl.teacher_id`

// ListClassSlots mengembalikan slot kelas classID, urut hari lalu jam.
func ListClassSlots(ctx context.Context, q database.DBTX, schoolID, classID string) ([]Slot, error) {
	if !database.IsUUID(classID) {
		return nil, nil
	}
	return listSlots(ctx, q, slotSelect+` WHERE sl.school_id = $1 AND sl.class_id = $2 ORDER BY sl.day_of_week, sl.start_time`, schoolID, classID)
}

// GetSlot mengembalikan satu slot milik schoolID.
func GetSlot(ctx context.Context, q database.DBTX, schoolID, id string) (Slot, error) {
	if !database.IsUUID(id) {
		return Slot{}, ErrSlotNotFound
	}
	list, err := listSlots(ctx, q, slotSelect+` WHERE sl.school_id = $1 AND sl.id = $2`, schoolID, id)
	if err != nil {
		return Slot{}, err
	}
	if len(list) == 0 {
		return Slot{}, ErrSlotNotFound
	}
	return list[0], nil
}

// overlapping mengembalikan slot lain di hari day yang berpotongan dengan
// [start, end) untuk kelas classID atau guru teacherID. exceptID dikecualikan
// (slot yang sedang diubah).
func overlapping(ctx context.Context, q database.DBTX, schoolID, classID, teacherID string, day int, start, end Minutes, exceptID string) ([]Slot, error) {
	return listSlots(ctx, q, slotSelect+`
		WHERE sl.school_id = $1 AND sl.day_of_week = $4
		  AND (sl.class_id = $2 OR sl.teacher_id = $3)
		  AND sl.start_time < $6::time AND sl.end_time > $5::time
		  AND sl.id IS DISTINCT FROM $7::uuid
		ORDER BY sl.start_time`, schoolID, classID, teacherID, day, start.Input(), end.Input(), nullIfEmpty(exceptID))
}

func insertSlot(ctx context.Context, q database.DBTX, schoolID string, in slotInput) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO schedule_slots (school_id, academic_year_id, class_id, subject_id, teacher_id, day_of_week, start_time, end_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7::time, $8::time) RETURNING id`,
		schoolID, in.AcademicYearID, in.ClassID, in.SubjectID, in.TeacherID, in.Day, in.Start.Input(), in.End.Input()).Scan(&id)
	return id, mapSlotError("insert slot", err)
}

func updateSlot(ctx context.Context, q database.DBTX, schoolID, id string, in slotInput) error {
	_, err := q.Exec(ctx, `
		UPDATE schedule_slots SET subject_id = $3, teacher_id = $4, day_of_week = $5, start_time = $6::time, end_time = $7::time, updated_at = now()
		WHERE school_id = $1 AND id = $2`, schoolID, id, in.SubjectID, in.TeacherID, in.Day, in.Start.Input(), in.End.Input())
	return mapSlotError("update slot", err)
}

func deleteSlot(ctx context.Context, q database.DBTX, schoolID, id string) error {
	tag, err := q.Exec(ctx, `DELETE FROM schedule_slots WHERE school_id = $1 AND id = $2`, schoolID, id)
	if err != nil {
		return mapSlotError("delete slot", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSlotNotFound
	}
	return nil
}

func listSlots(ctx context.Context, q database.DBTX, sql string, args ...any) ([]Slot, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapSlotError("list slots", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Slot, error) {
		var s Slot
		var start, end int
		err := row.Scan(&s.ID, &s.AcademicYearID, &s.ClassID, &s.ClassName, &s.SubjectID, &s.SubjectName, &s.SubjectCode,
			&s.TeacherID, &s.TeacherName, &s.Day, &start, &end, &s.CreatedAt)
		s.Start, s.End = Minutes(start), Minutes(end)
		return s, err
	})
	if err != nil {
		return nil, mapSlotError("list slots", err)
	}
	return list, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func mapSlotError(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSlotNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23P01" { // exclusion_violation
			return ErrOverlap
		}
		return fmt.Errorf("schedule: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("schedule: %s: %w", op, err)
}
