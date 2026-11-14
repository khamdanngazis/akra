// Command import-wilayah mengelola data wilayah (T110).
//
// Tanpa flag: mengimpor ulang data yang disertakan ke DATABASE_URL walau
// versinya sama (server juga mengimpor otomatis saat start jika data
// berubah).
//
// Dengan -convert: membuat internal/wilayah/data/wilayah.tsv.gz dari berkas
// SQL dataset cahyadsn/wilayah (db/wilayah.sql dan, untuk titik ibu kota
// provinsi/kabupaten, db/wilayah_level_1_2.sql) dan cahyadsn/wilayah_kodepos
// (db/wilayah_kodepos.sql):
//
//	go run ./cmd/import-wilayah -convert -sql wilayah.sql -kodepos wilayah_kodepos.sql -coords wilayah_level_1_2.sql
package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"

	"akra/internal/platform/database"
	"akra/internal/wilayah"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	convert := flag.Bool("convert", false, "buat berkas data dari SQL sumber")
	sqlPath := flag.String("sql", "", "berkas db/wilayah.sql")
	kodeposPath := flag.String("kodepos", "", "berkas db/wilayah_kodepos.sql")
	coordsPath := flag.String("coords", "", "berkas db/wilayah_level_1_2.sql (titik provinsi/kabupaten)")
	out := flag.String("out", "internal/wilayah/data/wilayah.tsv.gz", "berkas hasil konversi")
	flag.Parse()
	var err error
	if *convert {
		err = runConvert(*sqlPath, *kodeposPath, *coordsPath, *out, logger)
	} else {
		err = runImport(logger)
	}
	if err != nil {
		logger.Error("import-wilayah failed", "error", err)
		os.Exit(1)
	}
}

func runImport(logger *slog.Logger) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := wilayah.Reload(ctx, pool); err != nil {
		return err
	}
	logger.Info("wilayah imported", "version", wilayah.DataVersion())
	return nil
}

// rowRe membaca pasangan ('a','b') dari INSERT MySQL, termasuk kutip
// tunggal yang di-escape (digandakan atau diawali backslash).
var rowRe = regexp.MustCompile(`\('((?:[^'\\]|\\.|'')*)',\s*'((?:[^'\\]|\\.|'')*)'\)`)

func unquote(s string) string {
	s = strings.ReplaceAll(s, "''", "'")
	s = strings.ReplaceAll(s, `\'`, "'")
	return strings.ReplaceAll(s, `\\`, `\`)
}

func readPairs(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		for _, m := range rowRe.FindAllStringSubmatch(sc.Text(), -1) {
			out[unquote(m[1])] = strings.TrimSpace(unquote(m[2]))
		}
	}
	return out, sc.Err()
}

var posRe = regexp.MustCompile(`^[0-9]{5}$`)

// coordLineRe membaca awal baris INSERT wilayah_level_1_2: kode, nama,
// ibukota, lat, lng (kolom path yang besar diabaikan).
var coordLineRe = regexp.MustCompile(`^\('([0-9]{2}(?:\.[0-9]{2})?)','(?:[^'\\]|\\.|'')*','(?:[^'\\]|\\.|'')*',\s*(-?[0-9]+(?:\.[0-9]+)?),\s*(-?[0-9]+(?:\.[0-9]+)?),`)

// trunc6 memotong desimal menjadi paling banyak 6 angka di belakang koma
// (presisi kolom numeric(9,6)), tanpa konversi float.
func trunc6(s string) string {
	if i := strings.IndexByte(s, '.'); i >= 0 && len(s) > i+7 {
		s = s[:i+7]
	}
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

func readCoords(path string) (map[string][2]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string][2]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26) // baris memuat poligon batas wilayah
	for sc.Scan() {
		if m := coordLineRe.FindStringSubmatch(sc.Text()); m != nil {
			out[m[1]] = [2]string{trunc6(m[2]), trunc6(m[3])}
		}
	}
	return out, sc.Err()
}

func runConvert(sqlPath, kodeposPath, coordsPath, out string, logger *slog.Logger) error {
	if sqlPath == "" || kodeposPath == "" || coordsPath == "" {
		return errors.New("-sql, -kodepos, and -coords are required")
	}
	coords, err := readCoords(coordsPath)
	if err != nil {
		return err
	}
	names, err := readPairs(sqlPath)
	if err != nil {
		return err
	}
	pos, err := readPairs(kodeposPath)
	if err != nil {
		return err
	}
	codes := make([]string, 0, len(names))
	for k, n := range names {
		if !wilayah.ValidKode(k) || n == "" || strings.ContainsAny(n, "\t\n") {
			return fmt.Errorf("bad row %q %q", k, n)
		}
		if p := wilayah.IndukOf(k); p != "" && names[p] == "" {
			return fmt.Errorf("orphan %q", k)
		}
		codes = append(codes, k)
	}
	sort.Strings(codes)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	bw := bufio.NewWriter(zw)
	for _, k := range codes {
		p := ""
		if wilayah.TingkatOf(k) == wilayah.Desa && posRe.MatchString(pos[k]) {
			p = pos[k]
		}
		c := coords[k]
		if wilayah.TingkatOf(k) > wilayah.Kabupaten {
			c = [2]string{}
		}
		fmt.Fprintf(bw, "%s\t%s\t%s\t%s\t%s\n", k, names[k], p, c[0], c[1])
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	logger.Info("wilayah converted", "rows", len(codes), "coordinates", len(coords), "out", out)
	return nil
}
