package auth

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// PostgresRepository mengimplementasikan Repository dengan PostgreSQL.
type PostgresRepository struct {
	db database.DB
}

// NewRepository mengembalikan PostgresRepository yang memakai db.
func NewRepository(db database.DB) *PostgresRepository {
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

const findActiveUserForResetSQL = `
SELECT u.id, u.school_id, s.name, u.email, u.full_name
FROM users u
JOIN schools s ON s.id = u.school_id
WHERE s.code = $1 AND u.email = $2 AND u.is_active`

func (r *PostgresRepository) FindActiveUserForReset(ctx context.Context, schoolCode, email string) (User, error) {
	var u User
	err := r.db.QueryRow(ctx, findActiveUserForResetSQL, schoolCode, email).
		Scan(&u.ID, &u.SchoolID, &u.SchoolName, &u.Email, &u.FullName)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (r *PostgresRepository) CountRecentResetTokens(ctx context.Context, userID string, window time.Duration) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT count(*) FROM password_reset_tokens
		WHERE user_id = $1 AND created_at > now() - make_interval(secs => $2)`,
		userID, window.Seconds()).Scan(&n)
	return n, err
}

// CreateResetToken menghitung expires_at dengan jam database agar sama
// dengan jam yang dipakai saat token diperiksa.
func (r *PostgresRepository) CreateResetToken(ctx context.Context, userID string, tokenHash []byte, ttl time.Duration) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() + make_interval(secs => $3))`,
		userID, tokenHash, ttl.Seconds())
	return err
}

// Syarat token bisa dipakai: hash cocok, belum dipakai, belum kedaluwarsa,
// dan pemiliknya masih aktif.
const usableResetTokenSQL = `
FROM password_reset_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = $1 AND t.used_at IS NULL AND t.expires_at > now() AND u.is_active`

func (r *PostgresRepository) CheckResetToken(ctx context.Context, tokenHash []byte) error {
	var ok bool
	err := r.db.QueryRow(ctx, `SELECT true `+usableResetTokenSQL, tokenHash).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidResetToken
	}
	return err
}

func (r *PostgresRepository) ResetPassword(ctx context.Context, tokenHash []byte, passwordHash string) (string, error) {
	var userID string
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// FOR UPDATE mengunci baris token. Request kedua dengan token yang
		// sama menunggu, lalu tidak menemukan baris karena used_at sudah terisi.
		err := tx.QueryRow(ctx, `SELECT t.user_id `+usableResetTokenSQL+` FOR UPDATE OF t`, tokenHash).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidResetToken
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, userID, passwordHash); err != nil {
			return err
		}
		// Semua tautan reset lain milik pengguna ini ikut tidak berlaku.
		_, err = tx.Exec(ctx, `UPDATE password_reset_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, userID)
		return err
	})
	return userID, err
}
