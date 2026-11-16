package admission

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/audit"
	"akra/internal/payments"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit biaya formulir (T94). Termasuk aksi keuangan di halaman audit.
const (
	ActionFeePayment = audit.ActionAdmissionFeePayment
	ActionFeeVoid    = audit.ActionAdmissionFeeVoid
)

var (
	// ErrNoFee dikembalikan jika pendaftar tidak punya biaya formulir.
	ErrNoFee = errors.New("admission: applicant has no registration fee")
	// ErrFeePaid dikembalikan saat mencatat pembayaran yang sudah lunas.
	ErrFeePaid = errors.New("admission: registration fee already paid")
	// ErrNoPayment dikembalikan saat membatalkan tanpa pembayaran aktif.
	ErrNoPayment = errors.New("admission: no active fee payment")
)

// FeePayment adalah satu pembayaran biaya formulir.
type FeePayment struct {
	ID            string
	ApplicantID   string
	Amount        int64
	PaidOn        time.Time
	Method        string
	BankAccount   string
	Reference     string
	ReceiptNumber string
	RecordedBy    string // nama pencatat
	VoidedAt      *time.Time
	VoidReason    string
}

// FeeRow adalah satu baris daftar biaya formulir.
type FeeRow struct {
	ApplicantID        string
	RegistrationNumber string
	FullName           string
	PeriodName         string
	Status             string
	Amount             int64
	Payment            *FeePayment // pembayaran aktif, nil jika belum lunas
}

// FeePaymentForm adalah isian form pembayaran apa adanya.
type FeePaymentForm struct {
	PaidOn      string
	Method      string
	BankAccount string
	Reference   string
}

// FeeService mencatat pembayaran biaya formulir.
type FeeService struct {
	db    database.DB
	today func() time.Time
}

// NewFeeService mengembalikan FeeService.
func NewFeeService(db database.DB) *FeeService {
	return &FeeService{db: db, today: clock.Today}
}

// feePaidTx melaporkan apakah biaya formulir pendaftar sudah lunas. Dipakai
// aturan "wajib lunas sebelum verifikasi" (T93/T94).
func feePaidTx(ctx context.Context, tx pgx.Tx, a Applicant) (bool, error) {
	return feePaid(ctx, tx, a.ID)
}

func feePaid(ctx context.Context, q database.DBTX, applicantID string) (bool, error) {
	var paid bool
	err := q.QueryRow(ctx, `
		SELECT a.fee_amount = 0 OR EXISTS (
			SELECT 1 FROM applicant_fee_payments f WHERE f.applicant_id = a.id AND f.voided_at IS NULL AND f.amount = a.fee_amount)
		FROM applicants a WHERE a.id = $1`, applicantID).Scan(&paid)
	return paid, err
}

const feePaymentColumns = `f.id, f.applicant_id, f.amount, f.paid_on, f.method, coalesce(f.bank_account, ''), coalesce(f.reference, ''),
	f.receipt_number, u.full_name, f.voided_at, coalesce(f.void_reason, '')`

func scanFeePayment(row pgx.Row) (FeePayment, error) {
	var p FeePayment
	err := row.Scan(&p.ID, &p.ApplicantID, &p.Amount, &p.PaidOn, &p.Method, &p.BankAccount, &p.Reference, &p.ReceiptNumber, &p.RecordedBy, &p.VoidedAt, &p.VoidReason)
	return p, err
}

// List mengembalikan pendaftar yang punya biaya formulir. unpaidOnly hanya
// menampilkan yang belum lunas.
func (s *FeeService) List(ctx context.Context, schoolID string, unpaidOnly bool) ([]FeeRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT a.id, coalesce(a.registration_number, ''), a.full_name, p.name, a.status, a.fee_amount, `+feePaymentColumns+`
		FROM applicants a
		JOIN admission_periods p ON p.id = a.period_id
		LEFT JOIN applicant_fee_payments f ON f.applicant_id = a.id AND f.voided_at IS NULL
		LEFT JOIN users u ON u.id = f.recorded_by
		WHERE a.school_id = $1 AND a.fee_amount > 0 AND ($2 = false OR f.id IS NULL)
		ORDER BY f.id IS NOT NULL, a.submitted_at DESC NULLS LAST
		LIMIT 500`, schoolID, unpaidOnly)
	if err != nil {
		return nil, mapApplicantError(err)
	}
	defer rows.Close()
	var out []FeeRow
	for rows.Next() {
		var r FeeRow
		var p FeePayment
		var pid, papp, method, receipt, by *string
		var amount *int64
		var paidOn, voidedAt *time.Time
		var bank, ref, reason string
		if err := rows.Scan(&r.ApplicantID, &r.RegistrationNumber, &r.FullName, &r.PeriodName, &r.Status, &r.Amount,
			&pid, &papp, &amount, &paidOn, &method, &bank, &ref, &receipt, &by, &voidedAt, &reason); err != nil {
			return nil, mapApplicantError(err)
		}
		if pid != nil {
			p = FeePayment{ID: *pid, ApplicantID: *papp, Amount: *amount, PaidOn: *paidOn, Method: *method, BankAccount: bank,
				Reference: ref, ReceiptNumber: *receipt, RecordedBy: *by}
			r.Payment = &p
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Record mencatat pembayaran lunas biaya formulir dan memberi nomor kuitansi.
func (s *FeeService) Record(ctx context.Context, actor school.Actor, applicantID string, f FeePaymentForm) (FeePayment, error) {
	fields := map[string]string{}
	paidOn, err := time.Parse(dateLayout, strings.TrimSpace(f.PaidOn))
	if err != nil || paidOn.After(s.today()) || paidOn.Year() < 2000 {
		fields["paid_on"] = "Tanggal bayar tidak valid atau di masa depan."
	}
	method := strings.TrimSpace(f.Method)
	if method != string(payments.MethodCash) && method != string(payments.MethodTransfer) {
		fields["method"] = "Pilih metode pembayaran."
	}
	bank := strings.TrimSpace(f.BankAccount)
	if method == string(payments.MethodTransfer) && bank == "" {
		fields["bank_account"] = "Rekening tujuan wajib untuk transfer."
	}
	if method == string(payments.MethodCash) {
		bank = ""
	}
	ref := strings.TrimSpace(f.Reference)
	if utf8.RuneCountInString(bank) > 100 || utf8.RuneCountInString(ref) > 100 {
		fields["reference"] = "Rekening dan referensi maksimal 100 karakter."
	}
	if len(fields) > 0 {
		return FeePayment{}, &ValidationError{Fields: fields}
	}
	if !database.IsUUID(applicantID) {
		return FeePayment{}, ErrNotFound
	}
	var out FeePayment
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var amount int64
		var status string
		err := tx.QueryRow(ctx, `SELECT fee_amount, status FROM applicants WHERE school_id = $1 AND id = $2 FOR UPDATE`, actor.SchoolID, applicantID).Scan(&amount, &status)
		if err != nil {
			return mapApplicantError(err)
		}
		if amount == 0 {
			return ErrNoFee
		}
		number, err := payments.NextReceiptNumber(ctx, tx, actor.SchoolID, paidOn.Year())
		if err != nil {
			return err
		}
		var id string
		err = tx.QueryRow(ctx, `
			INSERT INTO applicant_fee_payments (school_id, applicant_id, amount, paid_on, method, bank_account, reference, receipt_number, recorded_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			actor.SchoolID, applicantID, amount, paidOn, method, nullIfEmpty(bank), nullIfEmpty(ref), number, actor.UserID).Scan(&id)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == "applicant_fee_payments_one_active" {
				return ErrFeePaid
			}
			return mapApplicantError(err)
		}
		if err := audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionFeePayment, EntityType: "applicant", EntityID: applicantID,
			After: map[string]any{"payment_id": id, "amount": amount, "method": method, "receipt_number": number, "paid_on": paidOn.Format(dateLayout)},
		}); err != nil {
			return err
		}
		out, err = scanFeePayment(tx.QueryRow(ctx, `SELECT `+feePaymentColumns+` FROM applicant_fee_payments f JOIN users u ON u.id = f.recorded_by WHERE f.id = $1`, id))
		return err
	})
	if err != nil {
		return FeePayment{}, err
	}
	return out, nil
}

// Void membatalkan pembayaran aktif dengan alasan. Kuitansinya tetap ada
// dengan tanda DIBATALKAN; pembayaran baru bisa dicatat setelahnya.
func (s *FeeService) Void(ctx context.Context, actor school.Actor, applicantID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fieldErr("void_reason", "Alasan pembatalan wajib diisi.")
	}
	if utf8.RuneCountInString(reason) > 500 {
		return fieldErr("void_reason", "Alasan maksimal 500 karakter.")
	}
	if !database.IsUUID(applicantID) {
		return ErrNotFound
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var id, number string
		err := tx.QueryRow(ctx, `SELECT id, receipt_number FROM applicant_fee_payments WHERE school_id = $1 AND applicant_id = $2 AND voided_at IS NULL FOR UPDATE`,
			actor.SchoolID, applicantID).Scan(&id, &number)
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM applicants WHERE school_id = $1 AND id = $2)`, actor.SchoolID, applicantID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
			return ErrNoPayment
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE applicant_fee_payments SET voided_at = now(), voided_by = $2, void_reason = $3 WHERE id = $1`,
			id, actor.UserID, reason); err != nil {
			return mapApplicantError(err)
		}
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionFeeVoid, EntityType: "applicant", EntityID: applicantID,
			Before: map[string]any{"payment_id": id, "receipt_number": number}, After: map[string]any{"reason": reason},
		})
	})
}

// Receipt mengembalikan data kuitansi untuk pembayaran paymentID (termasuk
// yang dibatalkan), dalam bentuk kuitansi pembayaran siswa.
func (s *FeeService) Receipt(ctx context.Context, schoolID, applicantID, paymentID string) (payments.Receipt, error) {
	if !database.IsUUID(applicantID) || !database.IsUUID(paymentID) {
		return payments.Receipt{}, ErrNotFound
	}
	var r payments.Receipt
	var method string
	var voidedAt *time.Time
	var tz string
	var regNumber, period, year string
	err := s.db.QueryRow(ctx, `
		SELECT f.receipt_number, sc.name, a.full_name, coalesce(a.registration_number, ''), p.name, y.name, f.amount, f.paid_on, f.method,
		       coalesce(f.bank_account, ''), coalesce(f.reference, ''), u.full_name, f.voided_at, coalesce(f.void_reason, ''), sc.timezone
		FROM applicant_fee_payments f
		JOIN applicants a ON a.id = f.applicant_id
		JOIN admission_periods p ON p.id = a.period_id
		JOIN academic_years y ON y.id = p.academic_year_id
		JOIN schools sc ON sc.id = f.school_id
		JOIN users u ON u.id = f.recorded_by
		WHERE f.school_id = $1 AND f.applicant_id = $2 AND f.id = $3`, schoolID, applicantID, paymentID).
		Scan(&r.Number, &r.SchoolName, &r.StudentName, &regNumber, &period, &year, &r.Amount, &r.PaidOn, &method,
			&r.BankAccount, &r.Reference, &r.Treasurer, &voidedAt, &r.VoidReason, &tz)
	if err != nil {
		return payments.Receipt{}, mapApplicantError(err)
	}
	r.Method = payments.Method(method)
	r.Voided = voidedAt != nil
	r.Applicant = fmt.Sprintf("%s (no. pendaftaran %s), %s, tahun ajaran %s", r.StudentName, regNumber, period, year)
	r.FeeNames = "Biaya formulir pendaftaran"
	r.PrintedAt = clock.TodayIn(clock.Location(tz))
	return r, nil
}

// History mengembalikan semua pembayaran biaya formulir pendaftar, terbaru
// dulu (termasuk yang dibatalkan).
func (s *FeeService) History(ctx context.Context, schoolID, applicantID string) ([]FeePayment, error) {
	rows, err := s.db.Query(ctx, `SELECT `+feePaymentColumns+` FROM applicant_fee_payments f JOIN users u ON u.id = f.recorded_by
		WHERE f.school_id = $1 AND f.applicant_id = $2 ORDER BY f.created_at DESC, f.receipt_number DESC`, schoolID, applicantID)
	if err != nil {
		return nil, mapApplicantError(err)
	}
	defer rows.Close()
	var out []FeePayment
	for rows.Next() {
		p, err := scanFeePayment(rows)
		if err != nil {
			return nil, mapApplicantError(err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
