package demo

import (
	"context"
	"os"
	"testing"
	"time"

	"akra/internal/auth"
	"akra/internal/classes"
	"akra/internal/grades"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

func TestSeed(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	hash, _ := auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1}).HashPassword("Demo-Uji-1")
	start := time.Now()
	res, err := Seed(ctx, tx, Options{Code: "test-demo", Name: "SD Demo Uji", PasswordHash: hash, Today: clock.Today(), Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded in %v: %v", time.Since(start), res.Counts)
	count := func(sql string) int {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx, sql, res.SchoolID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM students WHERE school_id = $1`); n != 240 {
		t.Errorf("students = %d", n)
	}
	if n := count(`SELECT count(*) - count(DISTINCT full_name) FROM students WHERE school_id = $1`); n != 0 {
		t.Errorf("%d duplicate student names", n)
	}
	if n := count(`SELECT count(*) FROM classes c JOIN academic_years y ON y.id = c.academic_year_id WHERE c.school_id = $1 AND y.is_active AND c.homeroom_teacher_id IS NOT NULL`); n != 12 {
		t.Errorf("classes with homeroom = %d", n)
	}
	// Status tersimpan sama dengan pembayaran aktif.
	if n := count(`SELECT count(*) FROM invoices i WHERE i.school_id = $1 AND i.status <> CASE
		WHEN coalesce((SELECT sum(p.amount) FROM payments p WHERE p.invoice_id = i.id AND NOT EXISTS (SELECT 1 FROM payment_voids v WHERE v.payment_id = p.id AND v.status = 'approved')), 0) = 0 THEN 'belum_bayar'
		WHEN coalesce((SELECT sum(p.amount) FROM payments p WHERE p.invoice_id = i.id AND NOT EXISTS (SELECT 1 FROM payment_voids v WHERE v.payment_id = p.id AND v.status = 'approved')), 0) >= i.total_amount THEN 'lunas'
		ELSE 'sebagian' END`); n != 0 {
		t.Errorf("%d invoices with inconsistent status", n)
	}
	if n := count(`SELECT count(*) FROM invoices WHERE school_id = $1 AND status = 'belum_bayar'`); n == 0 {
		t.Error("no unpaid invoices for the arrears demo")
	}
	if n := count(`SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'payment.create'`); n != res.Counts["pembayaran"] {
		t.Errorf("payment audits = %d, payments = %d", n, res.Counts["pembayaran"])
	}
	for sql, want := range map[string]int{
		`SELECT count(*) FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id WHERE u.school_id = $1 AND r.code = 'orang_tua' AND u.is_active AND u.guardian_id IS NOT NULL`: 3,
		`SELECT count(*) FROM student_guardians sg JOIN users u ON u.guardian_id = sg.guardian_id WHERE u.school_id = $1 AND u.email LIKE 'ortu.wijaya@%'`:                                                     2,
		`SELECT count(*) FROM invoices i JOIN enrollments e ON e.id = i.enrollment_id JOIN students s ON s.id = e.student_id WHERE i.school_id = $1 AND s.full_name = 'Alya Lestari' AND i.status <> 'lunas'`:  0,
		`SELECT count(*) FROM payment_voids WHERE school_id = $1 AND status = 'pending'`:                                                                                                                       1,
		`SELECT count(*) FROM attendance_sessions WHERE school_id = $1 AND session_date = current_date`:                                                                                                        0,
	} {
		if n := count(sql); n != want {
			t.Errorf("%s = %d, want %d", sql, n, want)
		}
	}
	if n := count(`SELECT count(*) FROM invoices i JOIN enrollments e ON e.id = i.enrollment_id JOIN students s ON s.id = e.student_id WHERE i.school_id = $1 AND s.full_name = 'Naufal Saputra' AND i.status = 'belum_bayar'`); n < 2 {
		t.Errorf("Naufal unpaid invoices = %d, want at least 2", n)
	}
	// Waktu pencatatan pembayaran mengikuti tanggal bayar (bukan saat seed).
	if n := count(`SELECT count(*) FROM payments WHERE school_id = $1 AND (created_at AT TIME ZONE 'Asia/Jakarta')::date <> paid_on`); n != 0 {
		t.Errorf("%d payments recorded on a different day than paid", n)
	}
	if n := count(`SELECT count(*) FROM audit_logs a JOIN payments p ON p.id::text = a.entity_id WHERE a.school_id = $1 AND a.action = 'payment.create' AND a.created_at <> p.created_at`); n != 0 {
		t.Errorf("%d payment audits not aligned with payments", n)
	}
	// Trigger append-only aktif lagi setelah seed.
	if _, err := tx.Exec(ctx, `SAVEPOINT cek`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE payments SET note = 'x' WHERE school_id = $1`, res.SchoolID); err == nil {
		t.Error("payments are writable after seeding; append-only trigger not restored")
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT cek`); err != nil {
		t.Fatal(err)
	}

	// Semua akun bisa login dengan password demo.
	rows, err := tx.Query(ctx, `SELECT password_hash FROM users WHERE school_id = $1`, res.SchoolID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		_ = rows.Scan(&h)
		if ok, err := auth.VerifyPassword("Demo-Uji-1", h); !ok || err != nil {
			t.Fatal("demo account password does not verify")
		}
	}
	// MVP 2.
	for sql, want := range map[string]int{
		`SELECT count(*) FROM subjects WHERE school_id = $1 AND is_active`:                                                         9,
		`SELECT count(*) FROM teaching_assignments WHERE school_id = $1`:                                                           12 * 9,
		`SELECT count(*) FROM (SELECT class_id FROM schedule_slots WHERE school_id = $1 GROUP BY class_id HAVING count(*) = 18) x`: 12,
		// Setiap guru (wali kelas dan guru mapel) punya jadwal mengajar.
		`SELECT count(DISTINCT teacher_id) FROM schedule_slots WHERE school_id = $1`:                                      15,
		`SELECT count(*) FROM grade_components WHERE school_id = $1 AND is_active`:                                        8*3 + 2,
		`SELECT count(*) FROM grades WHERE school_id = $1 AND score IS NULL`:                                              0,
		`SELECT count(*) FROM report_configs WHERE school_id = $1 AND is_shared`:                                          2,
		`SELECT count(*) FROM report_configs WHERE school_id = $1 AND NOT is_shared`:                                      2,
		`SELECT count(*) FROM import_mappings WHERE school_id = $1`:                                                       2,
		`SELECT count(*) FROM school_settings WHERE school_id = $1 AND key = 'attendance_per_subject' AND value = 'true'`: 1,
	} {
		if n := count(sql); n != want {
			t.Errorf("%s = %d, want %d", sql, n, want)
		}
	}
	if n := count(`SELECT count(*) FROM attendance_sessions WHERE school_id = $1 AND subject_id IS NOT NULL`); n == 0 {
		t.Error("no per-subject attendance sessions")
	}
	if n := count(`SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'grade.create'`); n != count(`SELECT count(*) FROM grades WHERE school_id = $1`) {
		t.Errorf("grade audits = %d", n)
	}
	// Rapor: 1A belum lengkap (UAS kosong), kelas lain siap cetak.
	var c1A, c2A classes.Class
	for name, c := range map[string]*classes.Class{"1A": &c1A, "2A": &c2A} {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id FROM classes WHERE school_id = $1 AND name = $2 AND academic_year_id = (SELECT id FROM academic_years WHERE school_id = $1 AND is_active)`, res.SchoolID, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if *c, err = classes.GetByID(ctx, tx, res.SchoolID, id); err != nil {
			t.Fatal(err)
		}
	}
	ready := func(c classes.Class) (ok, total int) {
		rep, err := grades.ClassSummary(ctx, tx, res.SchoolID, c)
		if err != nil {
			t.Fatal(err)
		}
		for _, st := range rep.CardStatuses() {
			total++
			if len(st.Missing) == 0 {
				ok++
			}
		}
		return ok, total
	}
	if ok, total := ready(c2A); total != 20 || ok != 20 {
		t.Errorf("2A report cards ready = %d of %d", ok, total)
	}
	if ok, total := ready(c1A); total != 20 || ok != 0 {
		t.Errorf("1A report cards ready = %d of %d, want none", ok, total)
	}

	if len(res.Accounts) != 3+12+3+3 {
		t.Errorf("accounts = %d", len(res.Accounts))
	}
}
