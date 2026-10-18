package students

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit untuk wali.
const (
	ActionGuardianCreate = "guardian.create"
	ActionGuardianLink   = "guardian.link"
	ActionGuardianUnlink = "guardian.unlink"

	maxGuardianNameLen = 100
	maxAddressLen      = 500
)

// ErrInvalidPhone dikembalikan jika nomor telepon tidak bisa dinormalisasi.
var ErrInvalidPhone = errors.New("students: invalid phone number")

// GuardianForm adalah isian data wali apa adanya.
type GuardianForm struct {
	FullName string
	Phone    string
	Email    string
	Address  string
}

// GuardianService menjalankan aturan bisnis data wali.
type GuardianService struct {
	db database.DB
}

// NewGuardianService mengembalikan GuardianService.
func NewGuardianService(db database.DB) *GuardianService {
	return &GuardianService{db: db}
}

// Create memvalidasi dan menormalisasi data wali lalu menyimpannya.
func (s *GuardianService) Create(ctx context.Context, actor school.Actor, f GuardianForm) (Guardian, error) {
	in, err := validateGuardian(f)
	if err != nil {
		return Guardian{}, err
	}
	var created Guardian
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		g, err := CreateGuardian(ctx, tx, actor.SchoolID, in)
		if err != nil {
			return err
		}
		created = g
		return audit.Record(ctx, tx, guardianAudit(actor, ActionGuardianCreate, g.ID, nil, guardianSnapshot(g)))
	})
	if err != nil {
		return Guardian{}, err
	}
	return created, nil
}

// Link menghubungkan wali ke siswa beserta auditnya.
func (s *GuardianService) Link(ctx context.Context, actor school.Actor, studentID, guardianID, relationship string, isPrimary bool) error {
	if !slices.Contains(Relationships, relationship) {
		return fieldErr("relationship", "Pilih hubungan dengan siswa.")
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := LinkToStudent(ctx, tx, actor.SchoolID, studentID, guardianID, relationship, isPrimary); err != nil {
			return err
		}
		return audit.Record(ctx, tx, guardianAudit(actor, ActionGuardianLink, guardianID, nil, map[string]any{
			"student_id": studentID, "relationship": relationship, "is_primary": isPrimary,
		}))
	})
}

// Unlink memutus hubungan wali dan siswa beserta auditnya.
func (s *GuardianService) Unlink(ctx context.Context, actor school.Actor, studentID, guardianID string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := UnlinkFromStudent(ctx, tx, actor.SchoolID, studentID, guardianID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, guardianAudit(actor, ActionGuardianUnlink, guardianID, map[string]any{"student_id": studentID}, nil))
	})
}

// ListByStudent mengembalikan wali seorang siswa.
func (s *GuardianService) ListByStudent(ctx context.Context, schoolID, studentID string) ([]StudentGuardian, error) {
	return ListByStudent(ctx, s.db, schoolID, studentID)
}

// ListByGuardian mengembalikan anak-anak seorang wali.
func (s *GuardianService) ListByGuardian(ctx context.Context, schoolID, guardianID string) ([]GuardianChild, error) {
	return ListByGuardian(ctx, s.db, schoolID, guardianID)
}

func validateGuardian(f GuardianForm) (GuardianInput, error) {
	fields := map[string]string{}
	in := GuardianInput{
		FullName: strings.Join(strings.Fields(f.FullName), " "),
		Email:    strings.ToLower(strings.TrimSpace(f.Email)),
		Address:  strings.TrimSpace(f.Address),
	}
	switch {
	case in.FullName == "":
		fields["full_name"] = "Nama wali wajib diisi."
	case utf8.RuneCountInString(in.FullName) > maxGuardianNameLen:
		fields["full_name"] = fmt.Sprintf("Nama maksimal %d karakter.", maxGuardianNameLen)
	}
	if strings.TrimSpace(f.Phone) != "" {
		phone, err := NormalizePhone(f.Phone)
		if err != nil {
			fields["phone"] = "Nomor telepon tidak valid. Contoh: 0812-3456-7890."
		}
		in.Phone = phone
	}
	if in.Email != "" {
		if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email {
			fields["email"] = "Email tidak valid."
		}
	}
	if utf8.RuneCountInString(in.Address) > maxAddressLen {
		fields["address"] = fmt.Sprintf("Alamat maksimal %d karakter.", maxAddressLen)
	}
	if len(fields) > 0 {
		return GuardianInput{}, &ValidationError{Fields: fields}
	}
	return in, nil
}

// NormalizePhone mengubah nomor telepon ke satu format: "+" diikuti kode
// negara dan nomor, tanpa pemisah. Nomor Indonesia diberi kode 62:
//
//	0812-3456-7890, +62 812 3456 7890, 62812..., 812...  ->  +6281234567890
//	(021) 555-1234                                      ->  +62215551234
//
// Nomor yang diawali "+" dengan kode negara lain dipertahankan.
func NormalizePhone(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	international := strings.HasPrefix(s, "+")
	var digits strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			if r > unicode.MaxASCII {
				return "", ErrInvalidPhone
			}
			digits.WriteRune(r)
		case r == '+' || r == ' ' || r == '-' || r == '.' || r == '(' || r == ')':
		default:
			return "", ErrInvalidPhone
		}
	}
	d := digits.String()
	switch {
	case international:
		// Sudah berkode negara.
	case strings.HasPrefix(d, "62"):
	case strings.HasPrefix(d, "0"):
		d = "62" + d[1:]
	default:
		d = "62" + d
	}
	if strings.HasPrefix(d, "620") || len(d) < 8 || len(d) > 15 {
		return "", ErrInvalidPhone
	}
	if strings.HasPrefix(d, "62") && (len(d) < 10 || len(d) > 15) {
		return "", ErrInvalidPhone
	}
	return "+" + d, nil
}

// guardianJSON adalah isi audit wali. Alamat tidak disimpan (praktis sama
// dengan alamat rumah siswa); cukup tanda apakah alamat diisi.
type guardianJSON struct {
	FullName   string `json:"full_name"`
	Phone      string `json:"phone"`
	Email      string `json:"email"`
	HasAddress bool   `json:"has_address"`
}

func guardianSnapshot(g Guardian) guardianJSON {
	return guardianJSON{FullName: g.FullName, Phone: g.Phone, Email: g.Email, HasAddress: g.Address != ""}
}

func guardianAudit(actor school.Actor, action, id string, before, after any) audit.Entry {
	return audit.Entry{
		SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
		Action: action, EntityType: "guardian", EntityID: id,
		Before: before, After: after,
	}
}
