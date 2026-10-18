// Package testutil berisi data dan daftar route untuk uji otorisasi lintas
// sekolah (T58). Hanya dipakai dari uji.
package testutil

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/announcements"
	"akra/internal/billing"
	"akra/internal/importer"
	"akra/internal/payments"
	"akra/internal/school"
)

// Password adalah password semua pengguna hasil SeedTwoSchools.
const Password = "Rahasia-Uji-Lintas-1"

// User adalah satu akun hasil seed.
type User struct {
	ID    string
	Email string
	Role  string
}

// School adalah satu sekolah hasil seed beserta ID semua entitasnya.
type School struct {
	ID   string
	Code string // kode sekolah untuk login
	// Marker ada di nama sekolah, pengguna, siswa, wali, jenis tagihan,
	// pengumuman, dan catatan pembayaran. Respons untuk pengguna sekolah lain
	// tidak boleh memuat teks ini.
	Marker string

	Admin, Treasurer, Teacher, Principal User
	// Parent adalah wali StudentIDs[0] (anak X); Parent2 wali StudentIDs[1]
	// (anak Y) di kelas lain (T59).
	Parent, Parent2 User

	YearID, ClassID string
	ClassName       string // tanpa Marker, karena form bisa menampilkan ulang isian
	// Class2ID adalah kelas StudentIDs[1]; StudentIDs[0] di ClassID.
	Class2ID    string
	Guardian2ID string
	Invoice2ID  string // tagihan StudentIDs[1]
	Payment2ID  string // pembayaran tunai Invoice2ID
	// Class2AnnouncementTitle adalah judul pengumuman khusus Class2ID.
	Class2AnnouncementTitle string
	StudentIDs              [2]string
	EnrollmentIDs           [2]string
	GuardianID              string
	FeeTypeID               string
	InvoiceID               string
	PaymentID               string
	ProofID                 string
	VoidID                  string
	AnnouncementID          string
	AttendanceSessionID     string
	BulkRunID               string
	ReportJobID             string
	ImportBatchID           string
}

// Users mengembalikan semua pengguna sekolah.
func (s *School) Users() []User {
	return []User{s.Admin, s.Treasurer, s.Teacher, s.Principal, s.Parent, s.Parent2}
}

// SeedTwoSchools membuat sekolah A dan B dengan isi yang setara: admin/TU,
// bendahara, guru (wali kelas), kepala sekolah, satu kelas di tahun ajaran
// aktif, dua siswa, satu wali dengan akun portal, satu tagihan, satu
// pembayaran transfer dengan bukti dan pengajuan pembatalan, satu
// pengumuman, satu sesi absensi, satu proses tagihan massal, satu job
// laporan, dan satu batch impor. passwordHash adalah hash dari Password.
func SeedTwoSchools(t testing.TB, tx pgx.Tx, passwordHash string) (a, b *School) {
	t.Helper()
	return seedSchool(t, tx, "a", passwordHash), seedSchool(t, tx, "b", passwordHash)
}

func seedSchool(t testing.TB, tx pgx.Tx, key, hash string) *School {
	t.Helper()
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("seed school %s: %v: %s", key, err, sql)
		}
		return id
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed school %s: %v: %s", key, err, sql)
		}
	}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed school %s: %v", key, err)
		}
	}

	marker := "RAHASIA-" + map[string]string{"a": "A", "b": "B"}[key]
	s := &School{Code: "test-lintas-" + key, Marker: marker}
	s.ID = one(`INSERT INTO schools (name, code) VALUES ($1, $2) RETURNING id`, "SD Uji "+marker, s.Code)

	user := func(role, name string, guardianID any) User {
		email := role + "@" + key + ".uji"
		if name == "Wali Kedua" {
			email = "orang_tua2@" + key + ".uji"
		}
		id := one(`INSERT INTO users (school_id, email, password_hash, full_name, guardian_id, is_active) VALUES ($1, $2, $3, $4, $5, true) RETURNING id`,
			s.ID, email, hash, name+" "+marker, guardianID)
		exec(`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = $2`, id, role)
		return User{ID: id, Email: email, Role: role}
	}
	s.Admin = user("admin_tu", "Admin", nil)
	s.Treasurer = user("bendahara", "Bendahara", nil)
	s.Teacher = user("guru", "Guru", nil)
	s.Principal = user("kepala_sekolah", "Kepala", nil)

	start := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	s.YearID = one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', $2, '2027-06-25', true) RETURNING id`, s.ID, start)
	s.ClassName = "Kelas " + strings.ToUpper(key)
	s.ClassID = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level, homeroom_teacher_id) VALUES ($1, $2, $3, 1, $4) RETURNING id`,
		s.ID, s.YearID, s.ClassName, s.Teacher.ID)
	s.Class2ID = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 2) RETURNING id`,
		s.ID, s.YearID, s.ClassName+"2")
	for i, class := range []string{s.ClassID, s.Class2ID} {
		s.StudentIDs[i] = one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id`,
			s.ID, fmt.Sprintf("%s-%03d", key, i+1), fmt.Sprintf("Siswa %s %d", marker, i+1))
		s.EnrollmentIDs[i] = one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			s.ID, s.StudentIDs[i], class, s.YearID, start)
	}
	s.GuardianID = one(`INSERT INTO guardians (school_id, full_name, phone, email) VALUES ($1, $2, '+6281200000001', $3) RETURNING id`,
		s.ID, "Wali "+marker, "orang_tua@"+key+".uji")
	exec(`INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship, is_primary) VALUES ($1, $2, $3, 'ibu', true)`,
		s.ID, s.StudentIDs[0], s.GuardianID)
	s.Parent = user("orang_tua", "Wali", s.GuardianID)
	s.Guardian2ID = one(`INSERT INTO guardians (school_id, full_name, phone, email) VALUES ($1, $2, '+6281200000002', $3) RETURNING id`,
		s.ID, "Wali Kedua "+marker, "orang_tua2@"+key+".uji")
	exec(`INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship, is_primary) VALUES ($1, $2, $3, 'ayah', true)`,
		s.ID, s.StudentIDs[1], s.Guardian2ID)
	s.Parent2 = user("orang_tua", "Wali Kedua", s.Guardian2ID)

	treasurer := school.Actor{SchoolID: s.ID, UserID: s.Treasurer.ID}
	ft, err := billing.CreateFeeType(ctx, tx, s.ID, billing.FeeTypeInput{Name: "SPP " + marker, DefaultAmount: 300000, DefaultDueDay: 10})
	check(err)
	s.FeeTypeID = ft.ID
	inv, err := billing.CreateInvoice(ctx, tx, treasurer, billing.InvoiceRequest{EnrollmentID: s.EnrollmentIDs[0], Period: "2026-09", Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}})
	check(err)
	s.InvoiceID = inv.ID
	pay, err := payments.RecordPayment(ctx, tx, treasurer, payments.Request{InvoiceID: inv.ID, Amount: 100000,
		PaidOn: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), Method: payments.MethodTransfer, BankAccount: "BCA " + marker, Note: "Catatan " + marker})
	check(err)
	s.PaymentID = pay.ID
	inv2, err := billing.CreateInvoice(ctx, tx, treasurer, billing.InvoiceRequest{EnrollmentID: s.EnrollmentIDs[1], Period: "2026-09", Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}})
	check(err)
	s.Invoice2ID = inv2.ID
	pay2, err := payments.RecordPayment(ctx, tx, treasurer, payments.Request{InvoiceID: inv2.ID, Amount: 50000,
		PaidOn: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), Method: payments.MethodCash})
	check(err)
	s.Payment2ID = pay2.ID
	sum := make([]byte, 32)
	_, _ = rand.Read(sum)
	s.ProofID = one(`INSERT INTO payment_proofs (school_id, payment_id, storage_key, sha256, size_bytes, content_type, uploaded_by)
		VALUES ($1, $2, $3, $4, 100, 'image/png', $5) RETURNING id`,
		s.ID, s.PaymentID, "schools/"+s.ID+"/proofs/uji-"+key+".png", sum, s.Treasurer.ID)
	s.VoidID = one(`INSERT INTO payment_voids (school_id, payment_id, reason, requested_by, requires_approval, status)
		VALUES ($1, $2, $3, $4, true, 'pending') RETURNING id`, s.ID, s.PaymentID, "Salah catat "+marker, s.Treasurer.ID)

	ann, err := announcements.NewService(tx).Create(ctx, school.Actor{SchoolID: s.ID, UserID: s.Admin.ID},
		announcements.Form{Title: "Pengumuman " + marker, Body: "Isi pengumuman " + marker})
	check(err)
	s.AnnouncementID = ann.ID
	s.Class2AnnouncementTitle = "Khusus kelas " + s.ClassName + "2 " + marker
	_, err = announcements.NewService(tx).Create(ctx, school.Actor{SchoolID: s.ID, UserID: s.Admin.ID},
		announcements.Form{Title: s.Class2AnnouncementTitle, Body: "Isi " + marker, ClassID: s.Class2ID})
	check(err)

	s.AttendanceSessionID = one(`INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by) VALUES ($1, $2, $3, '2026-10-16', $4) RETURNING id`,
		s.ID, s.YearID, s.ClassID, s.Teacher.ID)
	exec(`INSERT INTO attendance_records (school_id, session_id, enrollment_id, status, updated_by) VALUES ($1, $2, $3, 'hadir', $4)`,
		s.ID, s.AttendanceSessionID, s.EnrollmentIDs[0], s.Teacher.ID)
	s.BulkRunID = one(`INSERT INTO bulk_invoice_runs (school_id, academic_year_id, fee_type_id, class_id, period, due_date, mode, status, requested_by)
		VALUES ($1, $2, $3, $4, '2026-10', '2026-10-10', 'sync', 'done', $5) RETURNING id`, s.ID, s.YearID, s.FeeTypeID, s.ClassID, s.Treasurer.ID)
	s.ReportJobID = one(`INSERT INTO report_jobs (school_id, requested_by, report_type, format, status, file_key, file_name, finished_at)
		VALUES ($1, $2, 'arrears', 'xlsx', 'selesai', $3, 'tunggakan.xlsx', now()) RETURNING id`, s.ID, s.Treasurer.ID, "schools/"+s.ID+"/reports/uji.xlsx")

	var rows importer.Rows
	rows.Data = []importer.Row{{Line: 2, Values: [importer.NumColumns]string{key + "-900", "Impor " + marker, "", "", s.ClassName}}}
	s.ImportBatchID, err = importer.NewService(tx, nil).Upload(ctx, school.Actor{SchoolID: s.ID, UserID: s.Admin.ID}, "impor-"+key+".xlsx", rows, time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC))
	check(err)
	return s
}
