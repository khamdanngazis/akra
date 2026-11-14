package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// Kebijakan lockout. Kegagalan dihitung per (kode sekolah, email) dan per IP
// dalam LockoutWindow. Batas IP lebih tinggi dari batas email karena banyak
// pengguna satu sekolah biasanya berbagi satu alamat NAT.
const (
	LockoutWindow       = 15 * time.Minute
	MaxFailuresPerEmail = 5
	MaxFailuresPerIP    = 20
)

var (
	// ErrInvalidCredentials dikembalikan untuk semua kegagalan kredensial
	// (sekolah tidak ada, email tidak terdaftar, password salah, akun nonaktif)
	// agar pemanggil tidak bisa membedakannya.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")

	// ErrTooManyAttempts dikembalikan selama email atau IP sedang terkunci.
	ErrTooManyAttempts = errors.New("auth: too many failed login attempts")

	// ErrNotFound dikembalikan Repository jika baris yang dicari tidak ada.
	ErrNotFound = errors.New("auth: not found")
)

// User adalah pengguna yang sudah terautentikasi. Tidak pernah membawa hash password.
type User struct {
	ID         string
	SchoolID   string
	SchoolName string
	Email      string
	FullName   string
	Roles      []string
}

// UserCredentials adalah data pengguna yang dibutuhkan untuk login.
type UserCredentials struct {
	User
	PasswordHash string
	IsActive     bool
}

// LoginAttempt adalah satu percobaan login yang dicatat.
type LoginAttempt struct {
	SchoolCode string
	Email      string
	IP         netip.Addr // zero value jika tidak diketahui
	Success    bool
}

// Repository adalah akses data yang dibutuhkan Service.
type Repository interface {
	// FindUserForLogin mengembalikan ErrNotFound jika sekolah atau pengguna tidak ada.
	FindUserForLogin(ctx context.Context, schoolCode, email string) (UserCredentials, error)
	// FindSchoolName mengembalikan nama sekolah berkode code, atau ErrNotFound.
	FindSchoolName(ctx context.Context, code string) (string, error)
	// CountRecentFailures mengembalikan jumlah kegagalan dalam window untuk
	// pasangan (schoolCode, email) sejak login sukses terakhirnya, dan untuk ip.
	CountRecentFailures(ctx context.Context, schoolCode, email string, ip netip.Addr, window time.Duration) (byEmail, byIP int, err error)
	RecordLoginAttempt(ctx context.Context, a LoginAttempt) error
}

// Service menjalankan proses login.
type Service struct {
	repo Repository
	// dummyHash diverifikasi saat pengguna tidak ada, agar waktu respons
	// tidak membocorkan apakah sebuah email terdaftar.
	dummyHash string
}

// NewService mengembalikan Service. hasher hanya dipakai untuk membuat hash
// dummy, jadi parameternya harus sama dengan hash password sungguhan.
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

// NormalizeEmail membuang spasi di awal/akhir dan mengubah email ke huruf kecil.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormalizeSchoolCode membuang spasi di awal/akhir dan mengubah kode ke huruf kecil.
func NormalizeSchoolCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

// schoolCodePattern sama dengan constraint schools_code_format.
var schoolCodePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidSchoolCode melaporkan apakah code (sudah dinormalisasi) berformat
// kode sekolah: huruf kecil, angka, dan tanda hubung tunggal, 3–32 karakter.
func ValidSchoolCode(code string) bool {
	return len(code) >= 3 && len(code) <= 32 && schoolCodePattern.MatchString(code)
}

// SchoolName mengembalikan nama sekolah berkode code (link login sekolah).
// ErrNotFound jika format kode salah atau sekolah tidak ada.
func (s *Service) SchoolName(ctx context.Context, code string) (string, error) {
	code = NormalizeSchoolCode(code)
	if !ValidSchoolCode(code) {
		return "", ErrNotFound
	}
	return s.repo.FindSchoolName(ctx, code)
}

// IsLocked mengembalikan ErrTooManyAttempts jika pasangan (schoolCode, email)
// atau ip sudah mencapai batas kegagalan dalam LockoutWindow. Input harus
// sudah dinormalisasi.
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

// Authenticate memeriksa kredensial dan mencatat percobaannya. Mengembalikan
// ErrTooManyAttempts selama terkunci (tanpa memeriksa password) dan
// ErrInvalidCredentials untuk semua kegagalan kredensial.
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
		// Fail closed: tanpa catatan ini lockout tidak bisa ditegakkan.
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

	// Akun yang belum diaktivasi (akun orang tua) belum punya password.
	// Hash dummy tetap diverifikasi agar waktu respons sama.
	hash := creds.PasswordHash
	if hash == "" {
		hash = s.dummyHash
	}
	match, err := VerifyPassword(password, hash)
	if err != nil {
		return User{}, false, err
	}
	if creds.PasswordHash == "" {
		match = false
	}
	if !match || !creds.IsActive {
		return User{}, false, nil
	}
	return creds.User, true, nil
}
