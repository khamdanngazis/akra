package attendance

import (
	"context"
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
