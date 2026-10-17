package billing

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// ActionInvoiceCreate adalah aksi audit pembuatan tagihan.
const ActionInvoiceCreate = "invoice.create"

var periodPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// InvoiceRequest adalah permintaan membuat tagihan untuk satu enrollment.
type InvoiceRequest struct {
	EnrollmentID string
	Period       string    // YYYY-MM
	DueDate      time.Time // zero: dari jatuh tempo default jenis tagihan
	Items        []ItemRequest
}

// InvoiceService menjalankan aturan bisnis tagihan.
type InvoiceService struct {
	db database.DB
}

// NewInvoiceService mengembalikan InvoiceService.
func NewInvoiceService(db database.DB) *InvoiceService {
	return &InvoiceService{db: db}
}

// Get mengembalikan tagihan beserta itemnya.
func (s *InvoiceService) Get(ctx context.Context, schoolID, id string) (Invoice, error) {
	return GetInvoice(ctx, s.db, schoolID, id)
}

// Create membuat satu tagihan beserta auditnya dalam satu transaksi.
func (s *InvoiceService) Create(ctx context.Context, actor school.Actor, req InvoiceRequest) (Invoice, error) {
	var inv Invoice
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		inv, err = CreateInvoice(ctx, tx, actor, req)
		return err
	})
	return inv, err
}

// CreateInvoice membuat tagihan dan itemnya di dalam transaksi tx beserta
// auditnya. Dipakai juga oleh tagihan massal (T32).
//
//   - Nominal item diambil dari default jenis tagihan, kecuali Amount diisi.
//   - Jenis tagihan harus aktif dan milik sekolah; satu tagihan tidak boleh
//     memuat jenis tagihan yang sama dua kali.
//   - total_amount = jumlah nominal item, dihitung di sini (bukan diisi).
//   - Jatuh tempo kosong diambil dari jatuh tempo default jenis tagihan
//     pertama yang punya, pada bulan periode.
//   - Status awal dari ComputeStatus(total, 0): tagihan bertotal 0 lunas.
func CreateInvoice(ctx context.Context, tx pgx.Tx, actor school.Actor, req InvoiceRequest) (Invoice, error) {
	if !periodPattern.MatchString(req.Period) {
		return Invoice{}, ErrInvalidPeriod
	}
	if len(req.Items) == 0 {
		return Invoice{}, ErrNoItems
	}

	items := make([]InvoiceItem, 0, len(req.Items))
	seen := map[string]bool{}
	dueDay := 0
	var total int64
	for _, r := range req.Items {
		if seen[r.FeeTypeID] {
			return Invoice{}, ErrDuplicateItem
		}
		seen[r.FeeTypeID] = true
		ft, err := GetFeeType(ctx, tx, actor.SchoolID, r.FeeTypeID)
		if err != nil {
			return Invoice{}, err
		}
		if !ft.IsActive {
			return Invoice{}, ErrFeeTypeInactive
		}
		amount := ft.DefaultAmount
		if r.Amount != nil {
			amount = *r.Amount
		}
		if amount < 0 {
			return Invoice{}, ErrNegativeAmount
		}
		if amount > MaxAmount || total > MaxAmount-amount {
			return Invoice{}, ErrAmountTooLarge
		}
		total += amount
		if dueDay == 0 {
			dueDay = ft.DefaultDueDay
		}
		items = append(items, InvoiceItem{FeeTypeID: ft.ID, FeeTypeName: ft.Name, Description: r.Description, Amount: amount})
	}

	due := req.DueDate
	if due.IsZero() {
		if dueDay == 0 {
			return Invoice{}, ErrDueDateRequired
		}
		var err error
		if due, err = DueDateFor(req.Period, dueDay); err != nil {
			return Invoice{}, err
		}
	}

	// Belum ada pembayaran; tagihan bertotal 0 langsung lunas.
	status, err := ComputeStatus(total, 0)
	if err != nil {
		return Invoice{}, err
	}
	inv, err := insertInvoice(ctx, tx, actor.SchoolID, newInvoice{
		EnrollmentID: req.EnrollmentID, Period: req.Period, DueDate: due, Status: status, CreatedBy: actor.UserID, Items: items,
	})
	if err != nil {
		return Invoice{}, err
	}
	err = audit.Record(ctx, tx, audit.Entry{
		SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
		Action: ActionInvoiceCreate, EntityType: "invoice", EntityID: inv.ID,
		After: invoiceSnapshot(inv),
	})
	return inv, err
}

// DueDateFor mengembalikan tanggal day pada bulan period (YYYY-MM).
func DueDateFor(period string, day int) (time.Time, error) {
	if !periodPattern.MatchString(period) {
		return time.Time{}, ErrInvalidPeriod
	}
	if day < MinDueDay || day > MaxDueDay {
		return time.Time{}, ErrInvalidDueDay
	}
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrInvalidPeriod, err)
	}
	return t.AddDate(0, 0, day-1), nil
}

type invoiceJSON struct {
	EnrollmentID string            `json:"enrollment_id"`
	Period       string            `json:"period"`
	DueDate      string            `json:"due_date"`
	TotalAmount  int64             `json:"total_amount"`
	Status       Status            `json:"status"`
	Items        []invoiceItemJSON `json:"items"`
}

type invoiceItemJSON struct {
	FeeTypeID   string `json:"fee_type_id"`
	FeeTypeName string `json:"fee_type_name"`
	Amount      int64  `json:"amount"`
}

func invoiceSnapshot(inv Invoice) invoiceJSON {
	j := invoiceJSON{EnrollmentID: inv.EnrollmentID, Period: inv.Period, DueDate: inv.DueDate.Format("2006-01-02"), TotalAmount: inv.TotalAmount, Status: inv.Status}
	for _, it := range inv.Items {
		j.Items = append(j.Items, invoiceItemJSON{FeeTypeID: it.FeeTypeID, FeeTypeName: it.FeeTypeName, Amount: it.Amount})
	}
	return j
}
