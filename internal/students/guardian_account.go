package students

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit untuk akun portal orang tua (aktivasi ada di auth).
const (
	ActionAccountCreate = "guardian_account.create"
	ActionAccountResend = "guardian_account.resend_activation"
)

// Status akun portal seorang wali.
const (
	AccountNone     = ""         // belum dibuatkan akun
	AccountPending  = "pending"  // dibuat, menunggu aktivasi
	AccountActive   = "active"   // sudah aktif
	AccountDisabled = "disabled" // pernah aktif, lalu dinonaktifkan
)

var (
	// ErrGuardianNoEmail dikembalikan jika wali tidak punya email.
	ErrGuardianNoEmail = errors.New("students: guardian has no email")
	// ErrAccountExists dikembalikan jika wali sudah punya akun.
	ErrAccountExists = errors.New("students: guardian already has an account")
	// ErrAccountNotPending dikembalikan saat mengirim ulang aktivasi untuk
	// wali tanpa akun atau dengan akun yang sudah aktif.
	ErrAccountNotPending = errors.New("students: guardian account is not waiting for activation")
	// ErrEmailInUse dikembalikan jika email wali sudah dipakai akun lain di
	// sekolah yang sama (mis. akun guru).
	ErrEmailInUse = errors.New("students: email already used by another account in this school")
)

// AccountResult adalah hasil pembuatan akun atau kirim ulang aktivasi.
type AccountResult struct {
	UserID string
	Email  string
	// MailErr diisi jika email aktivasi gagal dikirim. Akun tetap dibuat;
	// admin bisa mengirim ulang.
	MailErr error
}

// GuardianAccountService membuat akun portal untuk wali.
type GuardianAccountService struct {
	db         database.DB
	activation *auth.ActivationService
	logger     *slog.Logger
}

// NewGuardianAccountService mengembalikan GuardianAccountService.
func NewGuardianAccountService(db database.DB, activation *auth.ActivationService, logger *slog.Logger) *GuardianAccountService {
	return &GuardianAccountService{db: db, activation: activation, logger: logger}
}

// CreateAccount membuat akun portal nonaktif tanpa password untuk wali,
// dengan peran orang_tua, lalu mengirim tautan aktivasi. school_id akun
// diambil dari wali, tidak pernah dari input.
func (s *GuardianAccountService) CreateAccount(ctx context.Context, actor school.Actor, guardianID string) (AccountResult, error) {
	var inv auth.Invitee
	var token string
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		g, err := GetGuardian(ctx, tx, actor.SchoolID, guardianID)
		if err != nil {
			return err
		}
		if g.Email == "" {
			return ErrGuardianNoEmail
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE guardian_id = $1)`, g.ID).Scan(&exists); err != nil {
			return fmt.Errorf("students: check guardian account: %w", err)
		}
		if exists {
			return ErrAccountExists
		}

		var userID string
		err = tx.QueryRow(ctx, `
			INSERT INTO users (school_id, email, password_hash, full_name, is_active, guardian_id)
			VALUES ($1, $2, '', $3, false, $4) RETURNING id`,
			g.SchoolID, g.Email, g.FullName, g.ID).Scan(&userID)
		if err != nil {
			return mapAccountError(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = $2`,
			userID, auth.RoleOrangTua); err != nil {
			return fmt.Errorf("students: assign parent role: %w", err)
		}
		if inv, err = invitee(ctx, tx, userID, g); err != nil {
			return err
		}
		if token, err = s.activation.IssueToken(ctx, tx, userID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, accountAudit(actor, ActionAccountCreate, g.ID, map[string]any{"user_id": userID, "email": g.Email}))
	})
	if err != nil {
		return AccountResult{}, err
	}
	return s.send(ctx, inv, token), nil
}

// ResendActivation menerbitkan tautan aktivasi baru untuk akun wali yang
// belum diaktivasi. Tautan lama tidak berlaku lagi.
func (s *GuardianAccountService) ResendActivation(ctx context.Context, actor school.Actor, guardianID string) (AccountResult, error) {
	var inv auth.Invitee
	var token string
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		g, err := GetGuardian(ctx, tx, actor.SchoolID, guardianID)
		if err != nil {
			return err
		}
		var userID string
		err = tx.QueryRow(ctx, `SELECT id FROM users
			WHERE school_id = $1 AND guardian_id = $2 AND NOT is_active AND password_hash = '' FOR UPDATE`,
			actor.SchoolID, g.ID).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountNotPending
		}
		if err != nil {
			return fmt.Errorf("students: find pending account: %w", err)
		}
		if inv, err = invitee(ctx, tx, userID, g); err != nil {
			return err
		}
		if token, err = s.activation.IssueToken(ctx, tx, userID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, accountAudit(actor, ActionAccountResend, g.ID, map[string]any{"user_id": userID}))
	})
	if err != nil {
		return AccountResult{}, err
	}
	return s.send(ctx, inv, token), nil
}

func (s *GuardianAccountService) send(ctx context.Context, inv auth.Invitee, token string) AccountResult {
	res := AccountResult{UserID: inv.UserID, Email: inv.Email}
	if err := s.activation.SendInvite(ctx, inv, token); err != nil {
		s.logger.ErrorContext(ctx, "send activation email", "user_id", inv.UserID, "error", err)
		res.MailErr = err
	}
	return res
}

func invitee(ctx context.Context, q database.DBTX, userID string, g Guardian) (auth.Invitee, error) {
	inv := auth.Invitee{UserID: userID, Email: g.Email, FullName: g.FullName}
	if err := q.QueryRow(ctx, `SELECT name, code FROM schools WHERE id = $1`, g.SchoolID).Scan(&inv.SchoolName, &inv.SchoolCode); err != nil {
		return auth.Invitee{}, fmt.Errorf("students: get school: %w", err)
	}
	return inv, nil
}

func mapAccountError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		switch pgErr.ConstraintName {
		case "users_guardian_id_key":
			return ErrAccountExists
		case "users_school_id_email_key":
			return ErrEmailInUse
		}
	}
	return fmt.Errorf("students: create guardian account: %w", err)
}

func accountAudit(actor school.Actor, action, guardianID string, after any) audit.Entry {
	return audit.Entry{
		SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
		Action: action, EntityType: "guardian", EntityID: guardianID, After: after,
	}
}
