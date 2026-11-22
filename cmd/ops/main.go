// Command akra-ops adalah alat tim AKRA untuk keandalan (T145).
//
//	akra-ops guard                      # pre-deploy: gagal bila ada ujian aktif
//	akra-ops active                     # daftar ujian aktif (JSON)
//	akra-ops maintenance list
//	akra-ops maintenance add -start 2026-11-21T22:00:00+07:00 -end 2026-11-21T23:00:00+07:00 -message "Pembaruan server"
//	akra-ops maintenance cancel -id 3
//	akra-ops pilot -school sdharapan -from 2027-02-01 -to 2027-02-28 [-tickets 3] [-sample 10]
//
// Koneksi hanya dari environment DATABASE_URL. guard membaca
// DEPLOY_EMERGENCY_UNTIL (RFC 3339, paling lama 6 jam ke depan) untuk deploy
// darurat; lihat doc/runbook-ujian.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/exam"
	"akra/internal/ops"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

func main() {
	code, err := run(context.Background(), os.Args[1:], time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "akra-ops:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func run(ctx context.Context, args []string, now time.Time) (int, error) {
	if len(args) == 0 {
		return 2, errors.New("usage: akra-ops guard | active | maintenance list|add|cancel | pilot")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return 1, errors.New("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return 1, fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()
	switch args[0] {
	case "guard":
		res, err := ops.CheckDeploy(ctx, pool, now, os.Getenv("DEPLOY_EMERGENCY_UNTIL"))
		if err != nil {
			return 1, fmt.Errorf("deploy guard: %w", err)
		}
		if !res.Allowed {
			fmt.Fprintln(os.Stderr, "DEPLOY DITOLAK:", res.Reason)
			for _, s := range res.Active.Sessions {
				fmt.Fprintf(os.Stderr, "  %s · %s · %s–%s · %d mengerjakan\n", s.SchoolCode, s.ExamName,
					s.StartsAt.Format(time.RFC3339), s.EndsAt.Format(time.RFC3339), s.Running)
			}
			fmt.Fprintln(os.Stderr, "Tunggu ujian selesai, atau deploy darurat: lihat doc/runbook-ujian.md.")
			return 3, nil
		}
		fmt.Println("deploy diizinkan:", res.Reason)
		return 0, nil
	case "active":
		a, err := exam.LoadActiveExams(ctx, pool, now, exam.DeployLead)
		if err != nil {
			return 1, err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return 0, enc.Encode(a)
	case "pilot":
		return pilot(ctx, pool, args[1:], os.Stdout)
	case "maintenance":
		return maintenance(ctx, pool, args[1:], now)
	}
	return 2, fmt.Errorf("unknown command %q", args[0])
}

func maintenance(ctx context.Context, pool *pgxpool.Pool, args []string, now time.Time) (int, error) {
	if len(args) == 0 {
		return 2, errors.New("usage: akra-ops maintenance list|add|cancel")
	}
	fs := flag.NewFlagSet("maintenance "+args[0], flag.ContinueOnError)
	start := fs.String("start", "", "mulai (RFC 3339)")
	end := fs.String("end", "", "selesai (RFC 3339)")
	msg := fs.String("message", "", "pesan untuk sekolah")
	id := fs.Int64("id", 0, "id pemeliharaan")
	if err := fs.Parse(args[1:]); err != nil {
		return 2, err
	}
	switch args[0] {
	case "list":
		ms, err := ops.UpcomingMaintenance(ctx, pool, now, 365*24*time.Hour)
		if err != nil {
			return 1, err
		}
		for _, m := range ms {
			fmt.Printf("%d\t%s\t%s\t%s\n", m.ID, m.StartsAt.Format(time.RFC3339), m.EndsAt.Format(time.RFC3339), m.Message)
		}
		return 0, nil
	case "add":
		a, err1 := time.Parse(time.RFC3339, *start)
		b, err2 := time.Parse(time.RFC3339, *end)
		if err1 != nil || err2 != nil {
			return 2, errors.New("-start and -end must be RFC 3339, e.g. 2026-11-21T22:00:00+07:00")
		}
		n, err := ops.AddMaintenance(ctx, pool, a, b, *msg)
		if err != nil {
			return 1, err
		}
		fmt.Println("maintenance scheduled, id", n)
		return 0, nil
	case "cancel":
		if err := ops.CancelMaintenance(ctx, pool, *id, now); err != nil {
			return 1, err
		}
		fmt.Println("maintenance cancelled")
		return 0, nil
	}
	return 2, fmt.Errorf("unknown maintenance command %q", args[0])
}

// pilot menulis laporan ukuran keberhasilan pilot satu sekolah (T155) dalam
// Markdown, untuk ditempel ke doc/uji-mvp4.md. Hanya membaca.
func pilot(ctx context.Context, q database.DBTX, args []string, out io.Writer) (int, error) {
	fs := flag.NewFlagSet("pilot", flag.ContinueOnError)
	code := fs.String("school", "", "kode sekolah (wajib)")
	fromS := fs.String("from", "", "tanggal awal YYYY-MM-DD (wajib)")
	toS := fs.String("to", "", "tanggal akhir YYYY-MM-DD, ikut dihitung (wajib)")
	tickets := fs.Int("tickets", -1, "jumlah tiket bantuan dari catatan tim (-1 = belum diisi)")
	sample := fs.Int("sample", 10, "jumlah lembar acak untuk dicocokkan dengan koreksi manual")
	base := fs.String("base", os.Getenv("APP_BASE_URL"), "URL AKRA untuk tautan lembar sampel")
	if err := fs.Parse(args); err != nil {
		return 2, err
	}
	if *code == "" || *fromS == "" || *toS == "" {
		return 2, errors.New("pilot: -school, -from, and -to are required")
	}
	var schoolID, tz string
	if err := q.QueryRow(ctx, `SELECT id, timezone FROM schools WHERE code = $1`, strings.ToLower(strings.TrimSpace(*code))).Scan(&schoolID, &tz); err != nil {
		return 1, fmt.Errorf("pilot: school %q: %w", *code, err)
	}
	loc := clock.Location(tz)
	from, err := time.ParseInLocation("2006-01-02", *fromS, loc)
	if err != nil {
		return 2, fmt.Errorf("pilot: invalid -from %q", *fromS)
	}
	to, err := time.ParseInLocation("2006-01-02", *toS, loc)
	if err != nil || to.Before(from) {
		return 2, fmt.Errorf("pilot: invalid -to %q", *toS)
	}
	r, err := exam.BuildPilotReport(ctx, q, schoolID, from, to.AddDate(0, 0, 1), *sample)
	if err != nil {
		return 1, err
	}
	return 0, r.WriteMarkdown(out, *tickets, strings.TrimRight(*base, "/"))
}
