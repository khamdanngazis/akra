package auth

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository mengimplementasikan Repository dengan PostgreSQL.
type PostgresRepository struct {
	db *pgxpool.Pool
}

// NewRepository mengembalikan PostgresRepository yang memakai db.
func NewRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const findUserForLoginSQL = `
SELECT u.id, u.school_id, s.name, u.email, u.full_name, u.password_hash, u.is_active,
       coalesce(array_agg(r.code ORDER BY r.id) FILTER (WHERE r.code IS NOT NULL), '{}')
FROM users u
JOIN schools s ON s.id = u.school_id
LEFT JOIN user_roles ur ON ur.user_id = u.id
LEFT JOIN roles r ON r.id = ur.role_id
WHERE s.code = $1 AND u.email = $2
GROUP BY u.id, s.name`

func (r *PostgresRepository) FindUserForLogin(ctx context.Context, schoolCode, email string) (UserCredentials, error) {
	var c UserCredentials
	err := r.db.QueryRow(ctx, findUserForLoginSQL, schoolCode, email).Scan(
		&c.ID, &c.SchoolID, &c.SchoolName, &c.Email, &c.FullName, &c.PasswordHash, &c.IsActive, &c.Roles,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserCredentials{}, ErrNotFound
	}
	return c, err
}

// Hitungan per email hanya mencakup kegagalan setelah login sukses terakhir,
// jadi login sukses mereset hitungan itu. Hitungan per IP tidak direset.
const countRecentFailuresSQL = `
WITH since AS (
    SELECT now() - make_interval(secs => $4) AS t
)
SELECT
    (SELECT count(*) FROM login_attempts a, since
      WHERE a.school_code = $1 AND a.email_normalized = $2 AND NOT a.success
        AND a.attempted_at > greatest(since.t, coalesce(
            (SELECT max(attempted_at) FROM login_attempts
              WHERE school_code = $1 AND email_normalized = $2 AND success),
            '-infinity'))),
    (SELECT count(*) FROM login_attempts a, since
      WHERE a.ip = $3 AND NOT a.success AND a.attempted_at > since.t)`

func (r *PostgresRepository) CountRecentFailures(ctx context.Context, schoolCode, email string, ip netip.Addr, window time.Duration) (int, int, error) {
	var byEmail, byIP int
	err := r.db.QueryRow(ctx, countRecentFailuresSQL, schoolCode, email, inetArg(ip), window.Seconds()).Scan(&byEmail, &byIP)
	return byEmail, byIP, err
}

const recordLoginAttemptSQL = `
INSERT INTO login_attempts (school_code, email_normalized, ip, success)
VALUES ($1, $2, $3, $4)`

func (r *PostgresRepository) RecordLoginAttempt(ctx context.Context, a LoginAttempt) error {
	_, err := r.db.Exec(ctx, recordLoginAttemptSQL, a.SchoolCode, a.Email, inetArg(a.IP), a.Success)
	return err
}

// inetArg mengubah netip.Addr kosong menjadi SQL NULL.
func inetArg(ip netip.Addr) any {
	if !ip.IsValid() {
		return nil
	}
	return ip
}
