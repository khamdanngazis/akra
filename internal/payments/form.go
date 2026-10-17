package payments

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/money"
	"akra/internal/school"
	"akra/internal/students"
)

// Batas form pembayaran.
const (
	// MinSearchLen adalah panjang minimal kata kunci pencarian siswa.
	MinSearchLen = 2
	searchLimit  = 10
	// MaxInvoicesPerForm membatasi jumlah tagihan dalam satu form.
	MaxInvoicesPerForm = 24
	bankAccountHints   = 10
)

// ErrStudentNotFound dikembalikan jika siswa tidak ada atau milik sekolah lain.
var ErrStudentNotFound = errors.New("payments: student not found")

// Form adalah isian form catat pembayaran apa adanya.
type Form struct {
	StudentID   string
	InvoiceIDs  []string          // tagihan yang dicentang, urut form
	Amounts     map[string]string // nominal per tagihan, kunci: id tagihan
	Method      string
	BankAccount string
	Reference   string
	PaidOn      string // YYYY-MM-DD
	Note        string
}

// ValidationError berisi pesan error per field form. Kunci nominal per
// tagihan adalah "amount_<id tagihan>".
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "payments: invalid payment form" }

// AmountField adalah nama field nominal tagihan invoiceID.
func AmountField(invoiceID string) string { return "amount_" + invoiceID }

// SearchStudents mencari siswa sekolah berdasarkan nama atau awalan NIS.
// Kata kunci lebih pendek dari MinSearchLen tidak mengembalikan hasil.
func (s *Service) SearchStudents(ctx context.Context, schoolID, query string) ([]students.Student, error) {
	if utf8.RuneCountInString(strings.TrimSpace(query)) < MinSearchLen {
		return nil, nil
	}
	return students.Search(ctx, s.db, schoolID, query, searchLimit)
}

// StudentInvoices mengembalikan siswa dan tagihannya yang belum lunas.
func (s *Service) StudentInvoices(ctx context.Context, schoolID, studentID string) (students.Student, []OpenInvoice, error) {
	st, err := students.GetByID(ctx, s.db, schoolID, studentID)
	if errors.Is(err, students.ErrNotFound) {
		return students.Student{}, nil, ErrStudentNotFound
	}
	if err != nil {
		return students.Student{}, nil, err
	}
	list, err := listOpenInvoices(ctx, s.db, schoolID, st.ID)
	return st, list, err
}

// BankAccounts mengembalikan saran rekening tujuan.
func (s *Service) BankAccounts(ctx context.Context, schoolID string) ([]string, error) {
	return recentBankAccounts(ctx, s.db, schoolID, bankAccountHints)
}

// InvoiceDetail mengembalikan tagihan beserta pembayarannya.
func (s *Service) InvoiceDetail(ctx context.Context, schoolID, invoiceID string) (InvoiceDetail, []Payment, error) {
	d, err := getInvoiceDetail(ctx, s.db, schoolID, invoiceID)
	if err != nil {
		return InvoiceDetail{}, nil, err
	}
	list, err := ListByInvoice(ctx, s.db, schoolID, invoiceID)
	return d, list, err
}

// RecordForm mencatat pembayaran untuk setiap tagihan yang dipilih dalam
// satu transaksi: jika satu tagihan gagal (mis. nominal melebihi sisa),
// tidak ada pembayaran yang tersimpan. Semua tagihan harus milik siswa di
// form; tagihan lain dianggap tidak ditemukan.
func (s *Service) RecordForm(ctx context.Context, actor school.Actor, f Form) ([]Payment, error) {
	if _, err := students.GetByID(ctx, s.db, actor.SchoolID, f.StudentID); err != nil {
		if errors.Is(err, students.ErrNotFound) {
			return nil, ErrStudentNotFound
		}
		return nil, err
	}
	reqs, err := parseForm(f)
	if err != nil {
		return nil, err
	}
	var out []Payment
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		out = out[:0]
		for _, req := range reqs {
			owner, err := invoiceStudentID(ctx, tx, actor.SchoolID, req.InvoiceID)
			if err != nil {
				return err
			}
			if owner != f.StudentID {
				return billing.ErrInvoiceNotFound
			}
			p, err := RecordPayment(ctx, tx, actor, req)
			if err != nil {
				return recordFieldError(req.InvoiceID, err)
			}
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parseForm memvalidasi isian tanpa membaca database.
func parseForm(f Form) ([]Request, error) {
	fields := map[string]string{}
	method := Method(f.Method)
	if method != MethodCash && method != MethodTransfer {
		fields["method"] = "Pilih metode pembayaran."
	}
	bank := strings.TrimSpace(f.BankAccount)
	switch {
	case method == MethodTransfer && bank == "":
		fields["bank_account"] = "Rekening tujuan wajib diisi untuk transfer."
	case utf8.RuneCountInString(bank) > MaxBankAccountLen:
		fields["bank_account"] = "Rekening tujuan terlalu panjang."
	}
	if utf8.RuneCountInString(strings.TrimSpace(f.Reference)) > MaxReferenceLen {
		fields["reference"] = "Nomor referensi terlalu panjang."
	}
	if utf8.RuneCountInString(strings.TrimSpace(f.Note)) > MaxNoteLen {
		fields["note"] = "Catatan paling banyak 500 karakter."
	}
	var paidOn time.Time
	switch d, err := time.Parse("2006-01-02", strings.TrimSpace(f.PaidOn)); {
	case strings.TrimSpace(f.PaidOn) == "":
		fields["paid_on"] = "Tanggal bayar wajib diisi."
	case err != nil:
		fields["paid_on"] = "Tanggal tidak valid."
	case d.After(clock.Today()):
		fields["paid_on"] = "Tanggal bayar tidak boleh setelah hari ini."
	default:
		paidOn = d
	}

	seen := map[string]bool{}
	var reqs []Request
	for _, id := range f.InvoiceIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		amount, err := money.ParseRupiah(f.Amounts[id])
		switch {
		case err != nil || amount <= 0:
			fields[AmountField(id)] = "Isi nominal lebih dari 0, mis. 150000 atau 150.000."
		case amount > billing.MaxAmount:
			fields[AmountField(id)] = "Nominal terlalu besar."
		}
		reqs = append(reqs, Request{InvoiceID: id, Amount: amount, PaidOn: paidOn, Method: method,
			BankAccount: f.BankAccount, Reference: f.Reference, Note: f.Note})
	}
	switch {
	case len(reqs) == 0:
		fields["invoice"] = "Pilih minimal satu tagihan."
	case len(reqs) > MaxInvoicesPerForm:
		fields["invoice"] = "Terlalu banyak tagihan dalam satu form."
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	return reqs, nil
}

// recordFieldError mengubah error RecordPayment yang disebabkan isian
// menjadi ValidationError; error lain dikembalikan apa adanya.
func recordFieldError(invoiceID string, err error) error {
	field, msg := "", ""
	switch {
	case errors.Is(err, billing.ErrOverpayment):
		field, msg = AmountField(invoiceID), "Nominal melebihi sisa tagihan."
	case errors.Is(err, ErrInvalidAmount), errors.Is(err, billing.ErrAmountTooLarge):
		field, msg = AmountField(invoiceID), "Nominal tidak valid."
	case errors.Is(err, ErrInvoiceVoided):
		field, msg = AmountField(invoiceID), "Tagihan ini sudah dibatalkan."
	case errors.Is(err, ErrPaidOnFuture):
		field, msg = "paid_on", "Tanggal bayar tidak boleh setelah hari ini."
	case errors.Is(err, ErrBankAccountRequired):
		field, msg = "bank_account", "Rekening tujuan wajib diisi untuk transfer."
	default:
		return err
	}
	return &ValidationError{Fields: map[string]string{field: msg}}
}
