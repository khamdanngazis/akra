// Command akra-seed membuat sekolah (jika belum ada) dan satu pengguna awal,
// misalnya admin TU pertama di staging.
//
//	akra-seed -school-code sd-harapan -school-name "SD Harapan" \
//	          -email admin@sdharapan.sch.id -name "Admin TU"
//
// Password diambil dari AKRA_SEED_PASSWORD. Jika kosong, password acak dibuat
// dan dicetak sekali ke stdout. Password tidak pernah diterima lewat argumen
// agar tidak tersimpan di riwayat shell.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/auth"
	"akra/internal/platform/config"
	"akra/internal/platform/database"
	"akra/internal/platform/module"
)

type options struct {
	schoolCode, schoolName string
	email, fullName, role  string
	// pkg adalah paket modul sekolah baru (T168); diabaikan untuk sekolah
	// yang sudah ada.
	pkg string
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "akra-seed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	opts, err := parseFlags(args)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	password, generated := getenv("AKRA_SEED_PASSWORD"), false
	if password == "" {
		if password, err = randomPassword(); err != nil {
			return err
		}
		generated = true
	}
	if err := auth.ValidatePassword(password); err != nil {
		return fmt.Errorf("AKRA_SEED_PASSWORD: %w", err)
	}

	hash, err := auth.NewHasher(auth.PasswordParams{
		MemoryKiB:   cfg.Argon2MemoryKiB,
		Iterations:  cfg.Argon2Iterations,
		Parallelism: cfg.Argon2Parallelism,
	}).HashPassword(password)
	if err != nil {
		return err
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL, 2)
	if err != nil {
		return err
	}
	defer pool.Close()

	schoolCreated, err := seed(ctx, pool, opts, hash)
	if err != nil {
		return err
	}

	if schoolCreated {
		fmt.Fprintf(out, "school created: %s (%s)\n", opts.schoolName, opts.schoolCode)
	} else {
		fmt.Fprintf(out, "school exists: %s\n", opts.schoolCode)
	}
	fmt.Fprintf(out, "user created: %s (role %s)\n", opts.email, opts.role)
	if generated {
		fmt.Fprintf(out, "generated password (shown once, change it after first login): %s\n", password)
	}
	return nil
}

func parseFlags(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("akra-seed", flag.ContinueOnError)
	fs.StringVar(&o.schoolCode, "school-code", "", "school code used on the login form (required)")
	fs.StringVar(&o.schoolName, "school-name", "", "school name, used when the school does not exist yet")
	fs.StringVar(&o.email, "email", "", "user email (required)")
	fs.StringVar(&o.fullName, "name", "", "user full name (required)")
	fs.StringVar(&o.role, "role", auth.RoleAdminTU, "role code: "+strings.Join(auth.AllRoles, ", "))
	fs.StringVar(&o.pkg, "package", module.DefaultPackage, "module package for a new school (dasar, akademik, keuangan, lengkap)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	o.schoolCode = auth.NormalizeSchoolCode(o.schoolCode)
	o.email = auth.NormalizeEmail(o.email)
	o.fullName = strings.TrimSpace(o.fullName)
	o.schoolName = strings.TrimSpace(o.schoolName)

	var missing []string
	for name, v := range map[string]string{"-school-code": o.schoolCode, "-email": o.email, "-name": o.fullName} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return options{}, fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	if _, ok := module.GetPackage(o.pkg); !ok {
		return options{}, fmt.Errorf("unknown package %q", o.pkg)
	}
	if !slices.Contains(auth.AllRoles, o.role) {
		return options{}, fmt.Errorf("unknown role %q (allowed: %s)", o.role, strings.Join(auth.AllRoles, ", "))
	}
	return o, nil
}

// seed membuat sekolah jika belum ada, lalu pengguna dan perannya, dalam
// satu transaksi. Pengguna yang sudah ada tidak pernah ditimpa.
func seed(ctx context.Context, db *pgxpool.Pool, o options, passwordHash string) (schoolCreated bool, err error) {
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var schoolID string
		err := tx.QueryRow(ctx, `SELECT id FROM schools WHERE code = $1`, o.schoolCode).Scan(&schoolID)
		if errors.Is(err, pgx.ErrNoRows) {
			if o.schoolName == "" {
				return fmt.Errorf("school %q does not exist; pass -school-name to create it", o.schoolCode)
			}
			err = tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ($1, $2) RETURNING id`, o.schoolName, o.schoolCode).Scan(&schoolID)
			schoolCreated = true
		}
		if err != nil {
			return fmt.Errorf("find or create school: %w", err)
		}
		// Sekolah baru mendapat paket modul (T168, bawaan Dasar).
		if schoolCreated {
			if _, err := (module.Admin{By: "akra-seed"}).ApplyPackage(ctx, tx, schoolID, o.pkg, "sekolah baru", false); err != nil {
				return fmt.Errorf("apply package: %w", err)
			}
		}

		if err := checkRoleModules(ctx, tx, schoolID, o.role); err != nil {
			return err
		}

		var userID string
		err = tx.QueryRow(ctx, `
			INSERT INTO users (school_id, email, password_hash, full_name)
			VALUES ($1, $2, $3, $4) RETURNING id`,
			schoolID, o.email, passwordHash, o.fullName).Scan(&userID)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("user %s already exists in school %s; not changed", o.email, o.schoolCode)
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}

		_, err = tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = $2`, userID, o.role)
		return err
	})
	return schoolCreated, err
}

// roleModules adalah modul yang membuat suatu peran berarti (T166): peran
// hanya bisa diberikan bila salah satu modulnya aktif. Pengguna yang sudah
// punya peran itu tidak diubah saat modulnya dimatikan.
var roleModules = map[string][]module.ID{
	auth.RoleBendahara: {module.Billing, module.Admission},
}

// checkRoleModules menolak peran yang modulnya tidak aktif di sekolah.
func checkRoleModules(ctx context.Context, q database.DBTX, schoolID, role string) error {
	need, ok := roleModules[role]
	if !ok {
		return nil
	}
	set, err := module.Load(ctx, q, schoolID)
	if err != nil {
		return err
	}
	for _, m := range need {
		if set.Has(m) {
			return nil
		}
	}
	return fmt.Errorf("role %s needs module %v to be enabled for the school (akra-modul)", role, need)
}

// randomPassword membuat password acak 18 karakter dari crypto/rand.
func randomPassword() (string, error) {
	b := make([]byte, 14)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)[:18], nil
}
