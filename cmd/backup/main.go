// Command akra-backup membuat dan memulihkan backup database (T62).
//
//	akra-backup run       pg_dump -> R2 backups/{tanggal}/schoolos.dump, lalu retensi
//	akra-backup restore   backup terbaru (atau -key) dari R2 -> database kosong
//	akra-backup compare   bandingkan jumlah baris tabel utama sumber vs hasil pemulihan
//	akra-backup list      daftar backup di R2
//
// Koneksi hanya dari environment, tidak pernah dari argumen (agar tidak
// tercatat di riwayat shell atau daftar proses):
//
//	DATABASE_URL          database sumber (run, compare)
//	RESTORE_DATABASE_URL  database tujuan pemulihan (restore, compare); nama
//	                      variabel bisa diganti dengan -target-env
//	R2_ACCOUNT_ID, R2_BUCKET, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY
//	BACKUP_RETENTION      jumlah backup harian yang disimpan (bawaan 14)
//
// pg_dump dan pg_restore harus versi yang sama atau lebih baru dari server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"text/tabwriter"
	"time"

	"akra/internal/backup"
	"akra/internal/platform/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "run":
		err = run(ctx, logger)
	case "restore":
		err = restore(ctx, args, logger)
	case "compare":
		err = compare(ctx, args)
	case "list":
		err = list(ctx)
	default:
		usage()
	}
	if err != nil {
		logger.Error("akra-backup failed", "command", cmd, "error", err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: akra-backup run | restore [-key KEY] [-target-env VAR] | compare [-target-env VAR] | list")
	os.Exit(2)
}

func env(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("environment variable %s is required", name)
	}
	return v, nil
}

func store() (*storage.R2, error) {
	return storage.NewR2(storage.R2Config{
		AccountID: os.Getenv("R2_ACCOUNT_ID"), Bucket: os.Getenv("R2_BUCKET"),
		AccessKeyID: os.Getenv("R2_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		Endpoint: os.Getenv("R2_ENDPOINT"), // hanya untuk uji dengan S3 lokal
	})
}

func run(ctx context.Context, logger *slog.Logger) error {
	dsn, err := env("DATABASE_URL")
	if err != nil {
		return err
	}
	retention := backup.DefaultRetention
	if v := os.Getenv("BACKUP_RETENTION"); v != "" {
		if retention, err = strconv.Atoi(v); err != nil || retention < 1 {
			return fmt.Errorf("BACKUP_RETENTION must be a positive number")
		}
	}
	s, err := store()
	if err != nil {
		return err
	}
	res, err := backup.Run(ctx, s, dsn, time.Now(), retention, logger)
	if err != nil {
		return err
	}
	logger.Info("backup finished", "key", res.Key, "bytes", res.Size, "duration", res.Duration.Round(time.Millisecond).String(),
		"deleted", res.Deleted, "retention", retention)
	return nil
}

func restore(ctx context.Context, args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	key := fs.String("key", "", "kunci backup di R2 (kosong = terbaru)")
	targetEnv := fs.String("target-env", "RESTORE_DATABASE_URL", "nama variabel environment berisi DSN database tujuan")
	_ = fs.Parse(args)
	target, err := env(*targetEnv)
	if err != nil {
		return err
	}
	s, err := store()
	if err != nil {
		return err
	}
	start := time.Now()
	logger.Info("restore started", "key", *key, "at", start.Format(time.RFC3339))
	restored, err := backup.Restore(ctx, s, *key, target)
	if err != nil {
		return err
	}
	end := time.Now()
	logger.Info("restore finished", "key", restored, "started", start.Format(time.RFC3339), "finished", end.Format(time.RFC3339),
		"duration", end.Sub(start).Round(time.Millisecond).String())
	return nil
}

func compare(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	targetEnv := fs.String("target-env", "RESTORE_DATABASE_URL", "nama variabel environment berisi DSN database hasil pemulihan")
	_ = fs.Parse(args)
	src, err := env("DATABASE_URL")
	if err != nil {
		return err
	}
	dst, err := env(*targetEnv)
	if err != nil {
		return err
	}
	a, err := backup.Counts(ctx, src)
	if err != nil {
		return err
	}
	b, err := backup.Counts(ctx, dst)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "tabel\tsumber\tpemulihan\t")
	mismatch := 0
	for _, t := range append(backup.CountTables, "schema_migrations.version") {
		mark := ""
		if a[t] != b[t] {
			mark, mismatch = " BEDA", mismatch+1
		}
		fmt.Fprintf(w, "%s\t%d\t%d%s\t\n", t, a[t], b[t], mark)
	}
	_ = w.Flush()
	if mismatch > 0 {
		return fmt.Errorf("%d table(s) differ", mismatch)
	}
	fmt.Println("Semua jumlah baris sama.")
	return nil
}

func list(ctx context.Context) error {
	s, err := store()
	if err != nil {
		return err
	}
	objs, err := s.List(ctx, backup.Prefix)
	if err != nil {
		return err
	}
	for _, o := range objs {
		fmt.Printf("%s\t%d\t%s\n", o.Key, o.Size, o.LastModified.Format(time.RFC3339))
	}
	return nil
}
