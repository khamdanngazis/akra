// Command akra-demoseed membuat sekolah demo lengkap untuk presentasi dan
// wawancara: 12 kelas, 240 siswa dan walinya, tagihan dan pembayaran sejak
// awal tahun ajaran, absensi harian, pengumuman, serta akun aktif untuk
// semua peran (termasuk orang tua, tanpa email aktivasi).
//
//	akra-demoseed [-code demo] [-name "SD Harapan Bangsa"] [-reset]
//
// Butuh DATABASE_URL. Password semua akun dari DEMO_PASSWORD; jika kosong,
// password acak dibuat dan dicetak sekali. Ditolak jika APP_ENV=production.
// -reset menghapus sekolah berkode sama lebih dulu (butuh superuser karena
// tabel keuangan append-only).
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/demo"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "akra-demoseed:", err)
		os.Exit(1)
	}
}

func run() error {
	code := flag.String("code", "demo", "kode sekolah untuk login")
	name := flag.String("name", "SD Harapan Bangsa", "nama sekolah")
	address := flag.String("address", "Jl. Pendidikan No. 12, Bandung", "alamat sekolah (tampil di kuitansi)")
	reset := flag.Bool("reset", false, "hapus sekolah berkode sama lebih dulu")
	flag.Parse()
	if os.Getenv("APP_ENV") == "production" {
		return fmt.Errorf("refusing to seed demo data when APP_ENV=production")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 4)
	if err != nil {
		return err
	}
	defer pool.Close()

	if *reset {
		if err := demo.DeleteSchool(ctx, pool, *code); err != nil {
			return fmt.Errorf("reset: %w", err)
		}
	}
	password, generated := os.Getenv("DEMO_PASSWORD"), false
	if password == "" {
		b := make([]byte, 9)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		password, generated = "Demo-"+base64.RawURLEncoding.EncodeToString(b), true
	}
	hash, err := auth.NewHasher(auth.PasswordParams{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1}).HashPassword(password)
	if err != nil {
		return err
	}
	start := time.Now()
	var res demo.Result
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		res, err = demo.Seed(ctx, tx, demo.Options{Code: *code, Name: *name, Address: *address, PasswordHash: hash, Today: clock.Today(), Seed: 1})
		return err
	})
	if err != nil {
		return err
	}
	fmt.Printf("Sekolah demo dibuat: %s (kode %s) dalam %s\n", *name, *code, time.Since(start).Round(time.Millisecond))
	fmt.Printf("Data: %d siswa, %d tagihan, %d pembayaran, %d catatan absensi, %d pembatalan langsung berlaku, %d menunggu persetujuan\n\n",
		res.Counts["siswa"], res.Counts["tagihan"], res.Counts["pembayaran"], res.Counts["absensi"], res.Counts["pembatalan_berlaku"], res.Counts["pembatalan_menunggu"])
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PERAN\tNAMA\tEMAIL\tCATATAN")
	for _, a := range res.Accounts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.Role, a.Name, a.Email, a.Note)
	}
	_ = w.Flush()
	if generated {
		fmt.Printf("\nPassword semua akun (dibuat acak, hanya ditampilkan sekali): %s\n", password)
	} else {
		fmt.Println("\nPassword semua akun: nilai DEMO_PASSWORD.")
	}
	return nil
}
