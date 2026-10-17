package attendance

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
	"akra/internal/school"
)

// Service menjalankan aturan absensi harian.
type Service struct {
	db database.DB
}

// NewService mengembalikan Service.
func NewService(db database.DB) *Service {
	return &Service{db: db}
}

// OpenSession mengambil atau membuat sesi kelas classID pada tanggal date
// (tanggal lokal Asia/Jakarta), memastikan setiap siswa yang terdaftar pada
// tanggal itu punya catatan, lalu mengembalikan sesi beserta catatannya.
// Pemeriksaan hak akses guru dan tanggal masa depan dilakukan pemanggil
// (T26).
func (s *Service) OpenSession(ctx context.Context, actor school.Actor, classID string, date time.Time) (Session, []Record, error) {
	var session Session
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if session, err = GetOrCreateSession(ctx, tx, actor.SchoolID, classID, date, actor.UserID); err != nil {
			return err
		}
		_, err = EnsureRecords(ctx, tx, actor.SchoolID, session.ID)
		return err
	})
	if err != nil {
		return Session{}, nil, err
	}
	records, err := ListRecords(ctx, s.db, actor.SchoolID, session.ID)
	if err != nil {
		return Session{}, nil, err
	}
	return session, records, nil
}

// ErrFutureDate dikembalikan untuk tanggal setelah hari ini (Asia/Jakarta).
var ErrFutureDate = errors.New("attendance: date is in the future")

// ErrUnknownEnrollment dikembalikan jika isian memuat siswa yang tidak ada
// di sesi tersebut.
var ErrUnknownEnrollment = errors.New("attendance: enrollment is not part of this session")

// Summary adalah jumlah siswa per status di satu sesi.
type Summary struct {
	Hadir, Sakit, Izin, Alpa int
}

// Total mengembalikan jumlah seluruh siswa.
func (s Summary) Total() int { return s.Hadir + s.Sakit + s.Izin + s.Alpa }

// Summarize menghitung jumlah per status dari records.
func Summarize(records []Record) Summary {
	var s Summary
	for _, r := range records {
		switch r.Status {
		case StatusPresent:
			s.Hadir++
		case StatusSick:
			s.Sakit++
		case StatusExcused:
			s.Izin++
		case StatusAbsent:
			s.Alpa++
		}
	}
	return s
}

// CheckDate menolak tanggal setelah today.
func CheckDate(date, today time.Time) error {
	if date.After(today) {
		return ErrFutureDate
	}
	return nil
}

// SaveStatuses menyimpan status semua siswa di sesi dalam satu transaksi
// dan satu perintah UPDATE. statuses berisi enrollment_id -> status. Jika ada
// satu saja enrollment yang bukan bagian sesi atau status yang tidak valid,
// tidak ada yang disimpan. Baris yang statusnya tidak berubah tidak disentuh.
// Mengembalikan jumlah baris yang berubah dan ringkasan setelah simpan.
func (s *Service) SaveStatuses(ctx context.Context, actor school.Actor, sessionID string, statuses map[string]string) (int64, Summary, error) {
	var changed int64
	var records []Record
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// Kunci sesi agar dua simpan bersamaan untuk sesi yang sama berurutan.
		var locked string
		err := tx.QueryRow(ctx, `SELECT id FROM attendance_sessions WHERE school_id = $1 AND id = $2 FOR UPDATE`,
			actor.SchoolID, sessionID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSessionNotFound
		}
		if err != nil {
			return mapError("lock session", err)
		}

		current, err := ListRecords(ctx, tx, actor.SchoolID, sessionID)
		if err != nil {
			return err
		}
		known := make(map[string]bool, len(current))
		for _, r := range current {
			known[r.EnrollmentID] = true
		}
		ids := make([]string, 0, len(statuses))
		values := make([]string, 0, len(statuses))
		for id, st := range statuses {
			if !known[id] {
				return ErrUnknownEnrollment
			}
			if !slices.Contains(Statuses, st) {
				return ErrInvalidStatus
			}
			ids, values = append(ids, id), append(values, st)
		}

		tag, err := tx.Exec(ctx, `
			UPDATE attendance_records r
			SET status = v.status, updated_by = $3, updated_at = now()
			FROM unnest($4::uuid[], $5::text[]) AS v(enrollment_id, status)
			WHERE r.school_id = $1 AND r.session_id = $2
			  AND r.enrollment_id = v.enrollment_id AND r.status <> v.status`,
			actor.SchoolID, sessionID, actor.UserID, ids, values)
		if err != nil {
			return mapError("save statuses", err)
		}
		changed = tag.RowsAffected()
		if changed > 0 {
			if _, err := tx.Exec(ctx, `UPDATE attendance_sessions SET updated_at = now() WHERE id = $1`, sessionID); err != nil {
				return mapError("touch session", err)
			}
		}
		records, err = ListRecords(ctx, tx, actor.SchoolID, sessionID)
		return err
	})
	if err != nil {
		return 0, Summary{}, err
	}
	return changed, Summarize(records), nil
}

// Records mengembalikan catatan sesi milik schoolID.
func (s *Service) Records(ctx context.Context, schoolID, sessionID string) ([]Record, error) {
	return ListRecords(ctx, s.db, schoolID, sessionID)
}

// Session mengembalikan sesi milik schoolID.
func (s *Service) Session(ctx context.Context, schoolID, sessionID string) (Session, error) {
	return GetSession(ctx, s.db, schoolID, sessionID)
}
