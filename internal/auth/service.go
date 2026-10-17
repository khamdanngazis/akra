package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// Lockout policy. Failures are counted per (school code, email) and per IP
// within LockoutWindow. The IP limit is higher than the email limit because
// many users of one school typically share a single NAT address.
const (
	LockoutWindow       = 15 * time.Minute
	MaxFailuresPerEmail = 5
	MaxFailuresPerIP    = 20
)

var (
	// ErrInvalidCredentials is returned for every credential failure (unknown
	// school, unknown email, wrong password, inactive user) so callers cannot
	// tell them apart.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")

	// ErrTooManyAttempts is returned while the email or IP is locked out.
	ErrTooManyAttempts = errors.New("auth: too many failed login attempts")

	// ErrNotFound is returned by Repository when no matching row exists.
	ErrNotFound = errors.New("auth: not found")
)

// User is an authenticated user. It never carries the password hash.
type User struct {
	ID       string
	SchoolID string
	Email    string
	FullName string
	Roles    []string
}

// UserCredentials is a user row as needed for login.
type UserCredentials struct {
	User
	PasswordHash string
	IsActive     bool
}

// LoginAttempt is one recorded login attempt.
type LoginAttempt struct {
	SchoolCode string
	Email      string
	IP         netip.Addr // zero value when unknown
	Success    bool
}

// Repository is the data access needed by Service.
type Repository interface {
	// FindUserForLogin returns ErrNotFound when the school or user does not exist.
	FindUserForLogin(ctx context.Context, schoolCode, email string) (UserCredentials, error)
	// CountRecentFailures returns failed attempts within window for the
	// (schoolCode, email) pair since its last success, and for ip.
	CountRecentFailures(ctx context.Context, schoolCode, email string, ip netip.Addr, window time.Duration) (byEmail, byIP int, err error)
	RecordLoginAttempt(ctx context.Context, a LoginAttempt) error
}

// Service implements login.
type Service struct {
	repo Repository
	// dummyHash is verified when the user does not exist so that the
	// response time does not reveal whether an email is registered.
	dummyHash string
}

// NewService returns a Service. hasher is only used to build the dummy hash,
// so it must use the same parameters as real password hashes.
func NewService(repo Repository, hasher *Hasher) (*Service, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("auth: generate dummy password: %w", err)
	}
	dummy, err := hasher.HashPassword(hex.EncodeToString(b))
	if err != nil {
		return nil, err
	}
	return &Service{repo: repo, dummyHash: dummy}, nil
}

// NormalizeEmail trims surrounding spaces and lower-cases email.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormalizeSchoolCode trims surrounding spaces and lower-cases code.
func NormalizeSchoolCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

// IsLocked returns ErrTooManyAttempts if the (schoolCode, email) pair or ip
// has reached its failure limit within LockoutWindow. Inputs must already be
// normalized.
func (s *Service) IsLocked(ctx context.Context, schoolCode, email string, ip netip.Addr) error {
	byEmail, byIP, err := s.repo.CountRecentFailures(ctx, schoolCode, email, ip, LockoutWindow)
	if err != nil {
		return fmt.Errorf("auth: count login failures: %w", err)
	}
	if byEmail >= MaxFailuresPerEmail || byIP >= MaxFailuresPerIP {
		return ErrTooManyAttempts
	}
	return nil
}

// Authenticate checks the credentials and records the attempt. It returns
// ErrTooManyAttempts while locked out (without checking the password) and
// ErrInvalidCredentials for any credential failure.
func (s *Service) Authenticate(ctx context.Context, schoolCode, email, password string, ip netip.Addr) (User, error) {
	schoolCode = NormalizeSchoolCode(schoolCode)
	email = NormalizeEmail(email)

	if err := s.IsLocked(ctx, schoolCode, email, ip); err != nil {
		return User{}, err
	}

	user, ok, err := s.checkCredentials(ctx, schoolCode, email, password)
	if err != nil {
		return User{}, err
	}

	attempt := LoginAttempt{SchoolCode: schoolCode, Email: email, IP: ip, Success: ok}
	if err := s.repo.RecordLoginAttempt(ctx, attempt); err != nil {
		// Fail closed: without the record the lockout cannot be enforced.
		return User{}, fmt.Errorf("auth: record login attempt: %w", err)
	}

	if !ok {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (s *Service) checkCredentials(ctx context.Context, schoolCode, email, password string) (User, bool, error) {
	creds, err := s.repo.FindUserForLogin(ctx, schoolCode, email)
	if errors.Is(err, ErrNotFound) {
		if _, err := VerifyPassword(password, s.dummyHash); err != nil {
			return User{}, false, err
		}
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("auth: find user: %w", err)
	}

	match, err := VerifyPassword(password, creds.PasswordHash)
	if err != nil {
		return User{}, false, err
	}
	if !match || !creds.IsActive {
		return User{}, false, nil
	}
	return creds.User, true, nil
}
