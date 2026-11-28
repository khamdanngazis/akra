// Command akra-modul mengatur aktivasi modul opsional per sekolah (T125).
// Modul ujian diaktifkan pengelola platform, bukan oleh sekolah, supaya
// rilis bisa dibuka bertahap (pilot dulu).
//
// Contoh:
//
//	akra-modul -school sdharapan                       # tampilkan status
//	akra-modul -school sdharapan -exam on -release r1  # aktifkan tahap 1
//	akra-modul -school sdharapan -exam off             # nonaktifkan
//	akra-modul -school sda,smpb -exam on -release r1   # beberapa sekolah sekaligus
//	akra-modul -file gelombang-2.txt -exam on -release r2
//	akra-modul -list                                   # status semua sekolah
//
// Rollout bertahap (T155): pilot dulu, lalu gelombang 5 → 20 → semua
// sekolah. Satu gelombang diterapkan dalam satu transaksi: bila satu kode
// salah, tidak ada yang berubah. -file berisi satu kode sekolah per baris;
// baris kosong dan baris diawali # diabaikan.
//
// Koneksi hanya dari environment DATABASE_URL. Status disimpan di
// school_modules (T163) dan setiap perubahan dicatat di audit sebagai aksi
// sistem. Pengguna sekolah melihat perubahan tanpa login ulang, paling lama
// 30 detik per replika (cache status modul).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/exam"
	"akra/internal/platform/database"
	"akra/internal/platform/module"
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
	code := fs.String("school", "", "kode sekolah; beberapa dipisah koma")
	file := fs.String("file", "", "berkas berisi kode sekolah, satu per baris")
	list := fs.Bool("list", false, "tampilkan status modul ujian semua sekolah")
	examFlag := fs.String("exam", "", "on atau off; kosong = tidak diubah")
	release := fs.String("release", "", "tahap modul ujian: r1, r2, atau r3")
	if err := fs.Parse(args); err != nil {
		return err
	}
	codes, err := schoolCodes(*code, *file)
	if err != nil {
		return err
	}
	if len(codes) == 0 && !*list {
		return errors.New("-school, -file, or -list is required")
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
	if *list {
		return listSchools(ctx, pool, os.Stdout)
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for _, c := range codes {
			if err := apply(ctx, tx, c, *examFlag, *release, os.Stdout); err != nil {
				return err
			}
		}
		return nil
	})
}

// schoolCodes menggabungkan kode dari -school (dipisah koma) dan -file,
// tanpa duplikat.
func schoolCodes(flagCodes, file string) ([]string, error) {
	raw := strings.Split(flagCodes, ",")
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read -file: %w", err)
		}
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				raw = append(raw, l)
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range raw {
		c = strings.ToLower(strings.TrimSpace(c))
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out, nil
}

// listSchools menulis status modul ujian setiap sekolah.
func listSchools(ctx context.Context, q database.DBTX, out io.Writer) error {
	rows, err := q.Query(ctx, `SELECT s.code, s.name,
			coalesce((SELECT CASE WHEN enabled THEN 'true' ELSE 'false' END FROM school_modules WHERE school_id = s.id AND module = 'exam'), 'false'),
			coalesce((SELECT options->>'release' FROM school_modules WHERE school_id = s.id AND module = 'exam'), '-'),
			(SELECT max(created_at) FROM audit_logs WHERE school_id = s.id AND action = 'module.exam')
		FROM schools s ORDER BY s.code`)
	// Status dari school_modules (T163).
	if err != nil {
		return err
	}
	defer rows.Close()
	active := 0
	for rows.Next() {
		var code, name, on, rel string
		var changed *time.Time
		if err := rows.Scan(&code, &name, &on, &rel, &changed); err != nil {
			return err
		}
		state := "nonaktif"
		if on == "true" {
			state = "aktif"
			active++
		}
		since := ""
		if changed != nil {
			since = " · diubah " + changed.Format("2006-01-02")
		}
		if _, err := fmt.Fprintf(out, "%-24s %-8s %-3s %s%s\n", code, state, rel, name, since); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%d sekolah dengan modul ujian aktif\n", active)
	return err
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
		if err := exam.SetModule(ctx, tx, schoolID, after, operator(), auditTo(tx)); err != nil {
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

// operator adalah pelaku perubahan di school_modules.changed_by.
func operator() string {
	if u := os.Getenv("USER"); u != "" {
		return "akra-modul (" + u + ")"
	}
	return "akra-modul"
}

// auditTo mencatat perubahan modul di tx sebagai aksi sistem (module.<id>).
func auditTo(tx pgx.Tx) module.Auditor {
	return func(ctx context.Context, schoolID, action string, before, after module.State) error {
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: schoolID, Action: action, EntityType: "school", EntityID: schoolID,
			Before: before, After: after,
		})
	}
}
