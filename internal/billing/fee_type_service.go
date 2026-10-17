package billing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
	"akra/internal/school"
)

// Aksi audit jenis tagihan.
const (
	ActionFeeTypeCreate     = audit.ActionFeeTypeCreate
	ActionFeeTypeUpdate     = audit.ActionFeeTypeUpdate
	ActionFeeTypeDeactivate = audit.ActionFeeTypeDeactivate
	ActionFeeTypeActivate   = audit.ActionFeeTypeActivate

	maxFeeTypeNameLen = 60
	// MaxAmount membatasi nominal satu tagihan (Rp1 triliun) untuk menangkap
	// salah ketik; jauh di bawah batas bigint.
	MaxAmount int64 = 1_000_000_000_000
)

// FeeTypeForm adalah isian form jenis tagihan apa adanya.
type FeeTypeForm struct {
	Name          string
	DefaultAmount string // mis. "150.000" atau "Rp150.000"
	DefaultDueDay string // kosong atau 1–28
}

// ValidationError berisi pesan per field untuk ditampilkan di form.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return "billing: invalid input: " + strings.Join(keys, ", ")
}

// FeeTypeService menjalankan aturan bisnis jenis tagihan.
type FeeTypeService struct {
	db database.DB
}

// NewFeeTypeService mengembalikan FeeTypeService.
func NewFeeTypeService(db database.DB) *FeeTypeService {
	return &FeeTypeService{db: db}
}

// ListAll mengembalikan semua jenis tagihan sekolah.
func (s *FeeTypeService) ListAll(ctx context.Context, schoolID string) ([]FeeType, error) {
	return ListAllFeeTypes(ctx, s.db, schoolID)
}

// ListActive mengembalikan jenis tagihan aktif (untuk tagihan baru).
func (s *FeeTypeService) ListActive(ctx context.Context, schoolID string) ([]FeeType, error) {
	return ListActiveFeeTypes(ctx, s.db, schoolID)
}

// Get mengembalikan satu jenis tagihan.
func (s *FeeTypeService) Get(ctx context.Context, schoolID, id string) (FeeType, error) {
	return GetFeeType(ctx, s.db, schoolID, id)
}

// Create memvalidasi form lalu membuat jenis tagihan beserta auditnya.
func (s *FeeTypeService) Create(ctx context.Context, actor school.Actor, f FeeTypeForm) (FeeType, error) {
	in, err := ValidateFeeType(f)
	if err != nil {
		return FeeType{}, err
	}
	var created FeeType
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		ft, err := CreateFeeType(ctx, tx, actor.SchoolID, in)
		if err != nil {
			return err
		}
		created = ft
		return audit.Record(ctx, tx, feeTypeAudit(actor, ActionFeeTypeCreate, ft.ID, nil, feeTypeSnapshot(ft)))
	})
	if err != nil {
		return FeeType{}, feeTypeFieldError(err)
	}
	return created, nil
}

// Update memvalidasi form lalu mengubah jenis tagihan beserta auditnya.
// Tagihan yang sudah dibuat tidak berubah.
func (s *FeeTypeService) Update(ctx context.Context, actor school.Actor, id string, f FeeTypeForm) (FeeType, error) {
	in, err := ValidateFeeType(f)
	if err != nil {
		return FeeType{}, err
	}
	var updated FeeType
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := GetFeeType(ctx, tx, actor.SchoolID, id)
		if err != nil {
			return err
		}
		after, err := UpdateFeeType(ctx, tx, actor.SchoolID, id, in)
		if err != nil {
			return err
		}
		updated = after
		if feeTypeSnapshot(before) == feeTypeSnapshot(after) {
			return nil
		}
		return audit.Record(ctx, tx, feeTypeAudit(actor, ActionFeeTypeUpdate, id, feeTypeSnapshot(before), feeTypeSnapshot(after)))
	})
	if err != nil {
		return FeeType{}, feeTypeFieldError(err)
	}
	return updated, nil
}

// SetActive mengaktifkan atau menonaktifkan jenis tagihan. changed false
// jika statusnya sudah seperti yang diminta.
func (s *FeeTypeService) SetActive(ctx context.Context, actor school.Actor, id string, active bool) (ft FeeType, changed bool, err error) {
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := GetFeeType(ctx, tx, actor.SchoolID, id)
		if err != nil {
			return err
		}
		ft = before
		if before.IsActive == active {
			return nil
		}
		if ft, err = SetFeeTypeActive(ctx, tx, actor.SchoolID, id, active); err != nil {
			return err
		}
		changed = true
		action := ActionFeeTypeDeactivate
		if active {
			action = ActionFeeTypeActivate
		}
		return audit.Record(ctx, tx, feeTypeAudit(actor, action, id,
			map[string]any{"is_active": before.IsActive}, map[string]any{"is_active": active}))
	})
	return ft, changed, err
}

// ValidateFeeType memeriksa isian form dan mengubahnya menjadi input
// repository.
func ValidateFeeType(f FeeTypeForm) (FeeTypeInput, error) {
	fields := map[string]string{}
	in := FeeTypeInput{Name: strings.Join(strings.Fields(f.Name), " ")}
	switch {
	case in.Name == "":
		fields["name"] = "Nama jenis tagihan wajib diisi."
	case utf8.RuneCountInString(in.Name) > maxFeeTypeNameLen:
		fields["name"] = fmt.Sprintf("Nama maksimal %d karakter.", maxFeeTypeNameLen)
	}

	amount, err := money.ParseRupiah(f.DefaultAmount)
	switch {
	case errors.Is(err, money.ErrNegativeAmount):
		fields["default_amount"] = "Nominal tidak boleh negatif."
	case err != nil:
		fields["default_amount"] = "Isi nominal dalam rupiah bulat, mis. 150.000."
	case amount > MaxAmount:
		fields["default_amount"] = "Nominal terlalu besar."
	default:
		in.DefaultAmount = amount
	}

	if d := strings.TrimSpace(f.DefaultDueDay); d != "" {
		day, err := strconv.Atoi(d)
		if err != nil || day < MinDueDay || day > MaxDueDay {
			fields["default_due_day"] = fmt.Sprintf("Tanggal jatuh tempo %d sampai %d.", MinDueDay, MaxDueDay)
		} else {
			in.DefaultDueDay = day
		}
	}
	if len(fields) > 0 {
		return FeeTypeInput{}, &ValidationError{Fields: fields}
	}
	return in, nil
}

func feeTypeFieldError(err error) error {
	switch {
	case errors.Is(err, ErrDuplicateFeeTypeName):
		return &ValidationError{Fields: map[string]string{"name": "Nama jenis tagihan sudah dipakai."}}
	case errors.Is(err, ErrNegativeAmount):
		return &ValidationError{Fields: map[string]string{"default_amount": "Nominal tidak boleh negatif."}}
	case errors.Is(err, ErrInvalidDueDay):
		return &ValidationError{Fields: map[string]string{"default_due_day": "Tanggal jatuh tempo 1 sampai 28."}}
	}
	return err
}

type feeTypeJSON struct {
	Name          string `json:"name"`
	DefaultAmount int64  `json:"default_amount"`
	DefaultDueDay int    `json:"default_due_day"`
	IsActive      bool   `json:"is_active"`
}

func feeTypeSnapshot(f FeeType) feeTypeJSON {
	return feeTypeJSON{Name: f.Name, DefaultAmount: f.DefaultAmount, DefaultDueDay: f.DefaultDueDay, IsActive: f.IsActive}
}

func feeTypeAudit(actor school.Actor, action, id string, before, after any) audit.Entry {
	return audit.Entry{
		SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
		Action: action, EntityType: "fee_type", EntityID: id,
		Before: before, After: after,
	}
}
