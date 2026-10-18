// Command loadseed mengisi database dengan data uji beban (T61): tiga
// sekolah, masing-masing 300 siswa di 12 kelas, 6 bulan tagihan untuk 3
// jenis tagihan (5.400 tagihan), sekitar 3.400 pembayaran (10.000 untuk
// ketiga sekolah), dan absensi harian sejak awal tahun ajaran. Daftar akun
// dan kelas ditulis ke file JSON untuk skrip k6.
//
// Hanya untuk database uji. Sekolah berkode "beban-1".."beban-3"; jika
// sudah ada, program berhenti kecuali -reset (menghapus data ketiga sekolah
// itu; butuh superuser karena tabel keuangan append-only).
//
//	DATABASE_URL=... go run ./cmd/loadseed -out loadtest/data.json
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/auth"
)

const (
	schools         = 3
	paymentsPerSch  = 3334
	studentsPerSch  = 300
	classesPerSch   = 12
	defaultPassword = "Beban-Uji-2026"
)

type class struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type schoolOut struct {
	Code      string   `json:"code"`
	Admin     string   `json:"admin"`
	Treasurer string   `json:"treasurer"`
	Principal string   `json:"principal"`
	Teachers  []string `json:"teachers"`
	Classes   []class  `json:"classes"`
	Search    []string `json:"search"`
}

type output struct {
	Password string      `json:"password"`
	From     string      `json:"from"` // rentang absensi untuk ekspor rekap
	To       string      `json:"to"`
	Schools  []schoolOut `json:"schools"`
}

func main() {
	out := flag.String("out", "loadtest/data.json", "file JSON untuk skrip k6")
	reset := flag.Bool("reset", false, "hapus data sekolah beban-1..3 lebih dulu")
	password := flag.String("password", defaultPassword, "password semua akun uji")
	flag.Parse()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if *reset {
		for i := 1; i <= schools; i++ {
			if err := deleteSchool(ctx, pool, fmt.Sprintf("beban-%d", i)); err != nil {
				log.Fatalf("reset: %v", err)
			}
		}
	}
	hasher := auth.NewHasher(auth.PasswordParams{MemoryKiB: 19456, Iterations: 2, Parallelism: 1})
	hash, err := hasher.HashPassword(*password)
	if err != nil {
		log.Fatal(err)
	}
	res := output{Password: *password, From: "2026-07-13", To: "2026-10-16"}
	for i := 1; i <= schools; i++ {
		start := time.Now()
		s, err := seedSchool(ctx, pool, i, hash)
		if err != nil {
			log.Fatalf("seed school %d: %v", i, err)
		}
		res.Schools = append(res.Schools, s)
		log.Printf("seeded %s in %v", s.Code, time.Since(start).Round(time.Millisecond))
	}
	if _, err := pool.Exec(ctx, `ANALYZE`); err != nil {
		log.Fatal(err)
	}
	data, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(*out, data, 0o600); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", *out)
}

func seedSchool(ctx context.Context, pool *pgxpool.Pool, n int, hash string) (schoolOut, error) {
	code := fmt.Sprintf("beban-%d", n)
	s := schoolOut{Code: code, Search: []string{"Ahmad", "siti", "Putra", "Dewi 1", "0001", "tidak-ada"}}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var sid string
		err := tx.QueryRow(ctx, `INSERT INTO schools (name, code, address) VALUES ($1, $2, 'Jl. Uji Beban') RETURNING id`,
			fmt.Sprintf("SD Uji Beban %d", n), code).Scan(&sid)
		if err != nil {
			return fmt.Errorf("school %s exists? use -reset: %w", code, err)
		}
		user := func(local, name, role string) (string, error) {
			email := local + "@" + code + ".uji"
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO users (school_id, email, password_hash, full_name, is_active) VALUES ($1, $2, $3, $4, true) RETURNING id`,
				sid, email, hash, name).Scan(&id); err != nil {
				return "", err
			}
			_, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = $2`, id, role)
			return id, err
		}
		if _, err := user("admin", "Admin Beban", "admin_tu"); err != nil {
			return err
		}
		treasurer, err := user("bendahara", "Bendahara Beban", "bendahara")
		if err != nil {
			return err
		}
		if _, err := user("kepala", "Kepala Beban", "kepala_sekolah"); err != nil {
			return err
		}
		s.Admin, s.Treasurer, s.Principal = "admin@"+code+".uji", "bendahara@"+code+".uji", "kepala@"+code+".uji"

		var year string
		if err := tx.QueryRow(ctx, `INSERT INTO academic_years (school_id, name, start_date, end_date, is_active)
			VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, sid).Scan(&year); err != nil {
			return err
		}
		for c := range classesPerSch {
			name := fmt.Sprintf("%d%c", c/2+1, 'A'+c%2)
			teacher, err := user(fmt.Sprintf("guru%d", c+1), "Guru "+name, "guru")
			if err != nil {
				return err
			}
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO classes (school_id, academic_year_id, name, grade_level, homeroom_teacher_id)
				VALUES ($1, $2, $3, $4, $5) RETURNING id`, sid, year, name, c/2+1, teacher).Scan(&id); err != nil {
				return err
			}
			s.Classes = append(s.Classes, class{ID: id, Name: name})
			s.Teachers = append(s.Teachers, fmt.Sprintf("guru%d@%s.uji", c+1, code))
		}

		steps := []struct{ name, sql string }{
			{"students", `
				WITH names AS (SELECT ARRAY['Ahmad','Siti','Budi','Dewi','Eka','Fajar','Gita','Hadi','Intan','Joko','Kartika','Lukman'] AS first,
				                      ARRAY['Putra','Putri','Saputra','Lestari','Pratama','Wijaya','Hidayat','Rahma'] AS last)
				INSERT INTO students (school_id, nis, full_name, gender, birth_date)
				SELECT $1, lpad(g::text, 4, '0'), first[1 + g % 12] || ' ' || last[1 + (g / 12) % 8] || ' ' || g,
				       CASE WHEN g % 2 = 0 THEN 'L' ELSE 'P' END, date '2014-01-01' + (g * 7) % 2000
				FROM names, generate_series(1, 300) g`},
			{"enrollments", `
				INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
				SELECT st.school_id, st.id, c.id, c.academic_year_id, '2026-07-13'
				FROM students st
				JOIN LATERAL (SELECT id, academic_year_id FROM classes WHERE school_id = st.school_id
				              ORDER BY grade_level, name OFFSET (st.nis::int - 1) % 12 LIMIT 1) c ON true
				WHERE st.school_id = $1`},
			{"guardians", `
				WITH g AS (
					INSERT INTO guardians (school_id, full_name, phone)
					SELECT school_id, 'Wali ' || full_name, '+62812' || lpad(nis, 7, '0') FROM students WHERE school_id = $1
					RETURNING id, school_id, phone)
				INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship, is_primary)
				SELECT g.school_id, st.id, g.id, 'wali', true FROM g JOIN students st ON st.school_id = g.school_id AND '+62812' || lpad(st.nis, 7, '0') = g.phone`},
			{"fee types", `
				INSERT INTO fee_types (school_id, name, default_amount, default_due_day)
				VALUES ($1, 'SPP', 300000, 10), ($1, 'Kegiatan', 75000, 10), ($1, 'Katering', 150000, 10)`},
			{"invoices", `
				WITH src AS (
					SELECT e.school_id, e.academic_year_id, e.id AS enrollment_id, f.id AS fee_type_id, f.default_amount,
					       to_char(m, 'YYYY-MM') AS period, (m + interval '9 days')::date AS due_date, gen_random_uuid() AS invoice_id
					FROM enrollments e
					CROSS JOIN fee_types f
					CROSS JOIN generate_series(date '2026-07-01', date '2026-12-01', interval '1 month') m
					WHERE e.school_id = $1 AND f.school_id = $1),
				inv AS (
					INSERT INTO invoices (id, school_id, academic_year_id, enrollment_id, period, due_date, total_amount, created_by)
					SELECT invoice_id, school_id, academic_year_id, enrollment_id, period, due_date, default_amount, $2 FROM src
					RETURNING id)
				INSERT INTO invoice_items (school_id, invoice_id, enrollment_id, period, fee_type_id, amount, position)
				SELECT school_id, invoice_id, enrollment_id, period, fee_type_id, default_amount, 1 FROM src`},
			// Pembayaran penuh untuk tagihan Juli–Oktober, acak sampai 3.334 per sekolah.
			{"payments", `
				WITH pick AS (
					SELECT i.id, i.total_amount, i.due_date, row_number() OVER (ORDER BY i.due_date, md5(i.id::text)) AS n
					FROM invoices i WHERE i.school_id = $1 AND i.period <= '2026-10'
					ORDER BY md5(i.id::text) LIMIT 3334)
				INSERT INTO payments (school_id, invoice_id, amount, paid_on, method, bank_account, receipt_number, recorded_by)
				SELECT $1, id, total_amount, least(due_date - (n % 9)::int, date '2026-10-16'),
				       CASE WHEN n % 3 = 0 THEN 'transfer' ELSE 'tunai' END,
				       CASE WHEN n % 3 = 0 THEN 'BCA 1234567890' END,
				       'KW/2026/' || lpad(n::text, 6, '0'), $2
				FROM pick`},
			{"receipt counter", `INSERT INTO receipt_counters (school_id, year, last_number) SELECT $1, 2026, count(*) FROM payments WHERE school_id = $1`},
			{"invoice status", `UPDATE invoices i SET status = 'lunas' WHERE school_id = $1 AND EXISTS (SELECT 1 FROM payments p WHERE p.invoice_id = i.id)`},
			// Absensi setiap hari sekolah (Senin–Jumat) sejak awal tahun ajaran.
			{"attendance", `
				WITH s AS (
					INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by)
					SELECT c.school_id, c.academic_year_id, c.id, d::date, c.homeroom_teacher_id
					FROM classes c CROSS JOIN generate_series(date '2026-07-13', date '2026-10-16', interval '1 day') d
					WHERE c.school_id = $1 AND extract(isodow FROM d) < 6
					RETURNING id, school_id, class_id, created_by)
				INSERT INTO attendance_records (school_id, session_id, enrollment_id, status, updated_by)
				SELECT s.school_id, s.id, e.id,
				       (ARRAY['hadir','hadir','hadir','hadir','hadir','hadir','hadir','sakit','izin','alpa'])[1 + abs(hashtext(s.id::text || e.id::text)) % 10],
				       s.created_by
				FROM s JOIN enrollments e ON e.school_id = s.school_id AND e.class_id = s.class_id`},
			{"announcements", `
				INSERT INTO announcements (school_id, academic_year_id, title, body, created_by)
				SELECT $1, y.id, 'Pengumuman ' || g, 'Isi pengumuman uji beban ' || g, $2
				FROM academic_years y, generate_series(1, 10) g WHERE y.school_id = $1 AND y.is_active`},
		}
		for _, st := range steps {
			args := []any{sid}
			if st.name == "invoices" || st.name == "payments" || st.name == "announcements" {
				args = append(args, treasurer)
			}
			if _, err := tx.Exec(ctx, st.sql, args...); err != nil {
				return fmt.Errorf("%s: %w", st.name, err)
			}
		}
		return nil
	})
	return s, err
}

// deleteSchool menghapus satu sekolah uji beserta datanya. Trigger
// append-only dan foreign key dimatikan sementara (session_replication_role,
// butuh superuser).
func deleteSchool(ctx context.Context, pool *pgxpool.Pool, code string) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var sid string
		err := tx.QueryRow(ctx, `SELECT id FROM schools WHERE code = $1`, code).Scan(&sid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT c.table_name FROM information_schema.columns c
			JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
			WHERE c.table_schema = 'public' AND c.column_name = 'school_id' AND t.table_type = 'BASE TABLE'`)
		if err != nil {
			return err
		}
		tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id IN (SELECT id FROM users WHERE school_id = $1)`, sid); err != nil {
			return err
		}
		for _, t := range tables {
			if _, err := tx.Exec(ctx, `DELETE FROM `+pgx.Identifier{t}.Sanitize()+` WHERE school_id = $1`, sid); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM schools WHERE id = $1`, sid)
		return err
	})
}
