// Command akra-modul mengatur aktivasi modul opsional per sekolah (T125).
// Modul ujian diaktifkan pengelola platform, bukan oleh sekolah, supaya
// rilis bisa dibuka bertahap (pilot dulu).
//
// Contoh:
//
//	akra-modul -school sdharapan                       # tampilkan status
//	akra-modul -school sdharapan -exam on -release r1  # aktifkan tahap 1
//	akra-modul -school sdharapan -exam off             # nonaktifkan
//
// Koneksi hanya dari environment DATABASE_URL. Setiap perubahan dicatat di
// audit sebagai aksi sistem. Pengguna sekolah melihat menu baru setelah
// login ulang; route langsung mengikuti status baru.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/exam"
	"akra/internal/platform/database"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(context.Background(), os.Args[1:]); err != nil {
		logger.Error("akra-modul failed", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("akra-modul", flag.ContinueOnError)
	code := fs.String("school", "", "kode sekolah (wajib)")
	examFlag := fs.String("exam", "", "on atau off; kosong = tidak diubah")
	release := fs.String("release", "", "tahap modul ujian: r1, r2, atau r3")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *code == "" {
		return errors.New("-school is required")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("environment variable DATABASE_URL is required")
	}
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		return err
	}
	defer pool.Close()
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return apply(ctx, tx, *code, *examFlag, *release, os.Stdout)
	})
}

// apply membaca dan (bila diminta) mengubah status modul ujian sekolah.
func apply(ctx context.Context, tx pgx.Tx, code, examFlag, release string, out interface{ Write([]byte) (int, error) }) error {
	var schoolID, name string
	err := tx.QueryRow(ctx, `SELECT id, name FROM schools WHERE code = $1`, strings.ToLower(strings.TrimSpace(code))).Scan(&schoolID, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("school %q not found", code)
	}
	if err != nil {
		return err
	}
	before, err := exam.LoadModule(ctx, tx, schoolID)
	if err != nil {
		return err
	}
	after := before
	switch examFlag {
	case "":
	case "on":
		after.Enabled = true
	case "off":
		after.Enabled = false
	default:
		return fmt.Errorf("-exam must be on or off, got %q", examFlag)
	}
	if release != "" {
		after.Release = exam.Release(release)
		if !exam.ValidRelease(after.Release) {
			return fmt.Errorf("-release must be r1, r2, or r3, got %q", release)
		}
	}
	if after != before {
		if err := exam.SetModule(ctx, tx, schoolID, after); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{
			SchoolID: schoolID, Action: "module.exam", EntityType: "school", EntityID: schoolID,
			Before: before, After: after,
		}); err != nil {
			return err
		}
	}
	state := "nonaktif"
	if after.Enabled {
		state = "aktif"
	}
	_, err = fmt.Fprintf(out, "%s (%s): modul ujian %s, tahap %s\n", name, code, state, after.Release)
	return err
}
