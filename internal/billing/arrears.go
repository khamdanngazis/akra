package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// ArrearsFilter membatasi daftar tunggakan. Field kosong tidak membatasi.
type ArrearsFilter struct {
	ClassID   string    // kelas enrollment tagihan
	FeeTypeID string    // tagihan yang memuat jenis tagihan ini
	DueFrom   time.Time // jatuh tempo >= DueFrom
	DueTo     time.Time // jatuh tempo <= DueTo
}

// Arrear adalah satu tagihan yang menunggak.
type Arrear struct {
	InvoiceID   string
	StudentID   string
	StudentName string
	NIS         string
	ClassName   string
	YearName    string
	Period      string
	FeeNames    string
	DueDate     time.Time
	Total       int64
	Paid        int64 // pembayaran aktif (ActivePaidSQL)
	Remaining   int64
	DaysOverdue int
}

// Arrears adalah daftar tunggakan beserta ringkasannya.
type Arrears struct {
	Items          []Arrear
	TotalRemaining int64
	StudentCount   int
}

// ListArrears mengembalikan tagihan sekolah yang menunggak pada tanggal
// today (Asia/Jakarta, diisi pemanggil): belum lunas, tidak dibatalkan, dan
// jatuh temponya sebelum today (jatuh tempo hari ini belum terlambat, sama
// dengan IsOverdue). Sisa = total - pembayaran aktif; pembatalan yang belum
// disetujui tidak mengurangi pembayaran. Urut kelas, nama siswa, jatuh tempo.
// Tagihan dari semua tahun ajaran ikut.
func ListArrears(ctx context.Context, q database.DBTX, schoolID string, f ArrearsFilter, today time.Time) (Arrears, error) {
	var classID, feeTypeID, from, to any
	if database.IsUUID(f.ClassID) {
		classID = f.ClassID
	} else if f.ClassID != "" {
		return Arrears{}, nil // kelas tidak valid: tidak ada yang cocok
	}
	if database.IsUUID(f.FeeTypeID) {
		feeTypeID = f.FeeTypeID
	} else if f.FeeTypeID != "" {
		return Arrears{}, nil
	}
	if !f.DueFrom.IsZero() {
		from = f.DueFrom
	}
	if !f.DueTo.IsZero() {
		to = f.DueTo
	}
	rows, err := q.Query(ctx, `
		SELECT i.id, st.id, st.full_name, st.nis, c.name, y.name, i.period, coalesce(`+FeeNamesSQL+`, ''),
		       i.due_date, i.total_amount, `+ActivePaidSQL+`
		FROM invoices i
		JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
		JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
		JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id
		JOIN academic_years y ON y.school_id = i.school_id AND y.id = i.academic_year_id
		WHERE i.school_id = $1
		  AND i.status <> 'lunas'
		  AND i.voided_at IS NULL
		  AND i.due_date < $2
		  AND ($3::uuid IS NULL OR e.class_id = $3)
		  AND ($4::uuid IS NULL OR EXISTS (SELECT 1 FROM invoice_items it
		        WHERE it.school_id = i.school_id AND it.invoice_id = i.id AND it.fee_type_id = $4))
		  AND ($5::date IS NULL OR i.due_date >= $5)
		  AND ($6::date IS NULL OR i.due_date <= $6)
		ORDER BY c.grade_level, lower(c.name), lower(st.full_name), st.nis, i.due_date, i.period`,
		schoolID, today, classID, feeTypeID, from, to)
	if err != nil {
		return Arrears{}, fmt.Errorf("billing: list arrears: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Arrear, error) {
		var a Arrear
		err := row.Scan(&a.InvoiceID, &a.StudentID, &a.StudentName, &a.NIS, &a.ClassName, &a.YearName, &a.Period, &a.FeeNames,
			&a.DueDate, &a.Total, &a.Paid)
		return a, err
	})
	if err != nil {
		return Arrears{}, fmt.Errorf("billing: list arrears: %w", err)
	}
	out := Arrears{Items: items}
	students := map[string]bool{}
	for i := range out.Items {
		a := &out.Items[i]
		a.Remaining = a.Total - a.Paid
		a.DaysOverdue = int(dateOnly(today).Sub(dateOnly(a.DueDate)).Hours() / 24)
		out.TotalRemaining += a.Remaining
		students[a.StudentID] = true
	}
	out.StudentCount = len(students)
	return out, nil
}
