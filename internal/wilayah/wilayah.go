// Package wilayah menyediakan data wilayah administrasi Indonesia
// (provinsi, kabupaten/kota, kecamatan, desa/kelurahan) untuk isian alamat
// bertingkat (T110). Data disertakan di aplikasi dan dilayani sendiri:
// alamat siswa tidak pernah dikirim ke layanan pihak ketiga.
//
// Sumber data: github.com/cahyadsn/wilayah dan cahyadsn/wilayah_kodepos
// (lisensi MIT, lihat data/LICENSE.md).
package wilayah

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

//go:embed data/wilayah.tsv.gz
var dataGz []byte

// Tingkat wilayah.
const (
	Provinsi  = 1
	Kabupaten = 2
	Kecamatan = 3
	Desa      = 4
)

var (
	kodeRe = regexp.MustCompile(`^[0-9]{2}(\.[0-9]{2}(\.[0-9]{2}(\.[0-9]{4})?)?)?$`)
	desaRe = regexp.MustCompile(`^[0-9]{2}\.[0-9]{2}\.[0-9]{2}\.[0-9]{4}$`)
)

// ErrNotFound dikembalikan jika kode desa tidak ada di data wilayah.
var ErrNotFound = errors.New("wilayah: not found")

// ValidKode melaporkan apakah s berformat kode wilayah (tingkat mana pun).
func ValidKode(s string) bool { return kodeRe.MatchString(s) }

// ValidDesaKode melaporkan apakah s berformat kode desa/kelurahan.
func ValidDesaKode(s string) bool { return desaRe.MatchString(s) }

// TingkatOf mengembalikan tingkat kode (1–4).
func TingkatOf(kode string) int { return strings.Count(kode, ".") + 1 }

// IndukOf mengembalikan kode induk, atau "" untuk provinsi.
func IndukOf(kode string) string {
	if i := strings.LastIndex(kode, "."); i > 0 {
		return kode[:i]
	}
	return ""
}

// Region adalah satu wilayah.
type Region struct {
	Kode    string `json:"kode"`
	Nama    string `json:"nama"`
	KodePos string `json:"kode_pos,omitempty"`
}

// Children mengembalikan wilayah aktif di bawah induk, urut nama. induk
// kosong berarti daftar provinsi.
func Children(ctx context.Context, q database.DBTX, induk string) ([]Region, error) {
	var rows pgx.Rows
	var err error
	if induk == "" {
		rows, err = q.Query(ctx, `SELECT kode, nama, coalesce(kode_pos, '') FROM wilayah WHERE tingkat = 1 AND aktif ORDER BY nama`)
	} else {
		rows, err = q.Query(ctx, `SELECT kode, nama, coalesce(kode_pos, '') FROM wilayah WHERE induk = $1 AND aktif ORDER BY nama`, induk)
	}
	if err != nil {
		return nil, fmt.Errorf("wilayah: children: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Region, error) {
		var x Region
		return x, r.Scan(&x.Kode, &x.Nama, &x.KodePos)
	})
	if err != nil {
		return nil, fmt.Errorf("wilayah: children: %w", err)
	}
	return list, nil
}

// Address adalah satu desa/kelurahan beserta wilayah di atasnya.
type Address struct {
	Kode, Desa, Kecamatan, Kabupaten, Provinsi, KodePos string
}

// Resolve mengembalikan desa dengan kode itu beserta nama kecamatan,
// kabupaten/kota, dan provinsinya. ErrNotFound jika tidak ada.
func Resolve(ctx context.Context, q database.DBTX, kode string) (Address, error) {
	if !ValidDesaKode(kode) {
		return Address{}, ErrNotFound
	}
	a := Address{Kode: kode}
	err := q.QueryRow(ctx, `
		SELECT d.nama, coalesce(d.kode_pos, ''), kc.nama, kb.nama, p.nama
		FROM wilayah d
		JOIN wilayah kc ON kc.kode = d.induk
		JOIN wilayah kb ON kb.kode = kc.induk
		JOIN wilayah p ON p.kode = kb.induk
		WHERE d.kode = $1 AND d.tingkat = 4`, kode).Scan(&a.Desa, &a.KodePos, &a.Kecamatan, &a.Kabupaten, &a.Provinsi)
	if errors.Is(err, pgx.ErrNoRows) {
		return Address{}, ErrNotFound
	}
	if err != nil {
		return Address{}, fmt.Errorf("wilayah: resolve: %w", err)
	}
	return a, nil
}

// normName menyamakan penulisan nama untuk pencocokan: huruf kecil, tanpa
// awalan "desa", "kel.", "kec." dan spasi ganda.
func normName(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	for _, p := range []string{"desa ", "ds. ", "ds ", "kelurahan ", "kel. ", "kel ", "kecamatan ", "kec. ", "kec "} {
		s = strings.TrimPrefix(s, p)
	}
	return strings.TrimSpace(s)
}

// Match mencari kode desa dari nama desa dan kecamatan (mis. data lama atau
// impor Dapodik). ok false jika tidak ada atau tidak unik.
func Match(ctx context.Context, q database.DBTX, kecamatan, desa string) (string, bool, error) {
	kc, ds := normName(kecamatan), normName(desa)
	if kc == "" || ds == "" {
		return "", false, nil
	}
	rows, err := q.Query(ctx, `
		SELECT d.kode FROM wilayah d JOIN wilayah kc ON kc.kode = d.induk
		WHERE d.tingkat = 4 AND d.aktif AND lower(d.nama) = $1 AND lower(kc.nama) = $2
		LIMIT 2`, ds, kc)
	if err != nil {
		return "", false, fmt.Errorf("wilayah: match: %w", err)
	}
	codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", false, fmt.Errorf("wilayah: match: %w", err)
	}
	if len(codes) != 1 {
		return "", false, nil
	}
	return codes[0], true, nil
}

// Apply melengkapi isian alamat values yang memuat "wilayah_kode": nama
// desa dan kecamatan diambil dari data wilayah (bukan isian pengguna) dan
// kode pos diisi jika masih kosong. Tanpa kode, isian manual dibiarkan.
func Apply(ctx context.Context, q database.DBTX, values map[string]string) error {
	kode := strings.TrimSpace(values["wilayah_kode"])
	if kode == "" {
		return nil
	}
	a, err := Resolve(ctx, q, kode)
	if err != nil {
		return err
	}
	values["wilayah_kode"], values["village"], values["district"] = a.Kode, a.Desa, a.Kecamatan
	if strings.TrimSpace(values["postal_code"]) == "" {
		values["postal_code"] = a.KodePos
	}
	return nil
}

// DataVersion adalah versi data yang disertakan (sidik isi file).
func DataVersion() string {
	sum := sha256.Sum256(dataGz)
	return hex.EncodeToString(sum[:8])
}

// record adalah satu baris data yang disertakan.
type record struct {
	kode, nama, kodePos string
}

func readData() ([]record, error) {
	zr, err := gzip.NewReader(bytes.NewReader(dataGz))
	if err != nil {
		return nil, fmt.Errorf("wilayah: open data: %w", err)
	}
	defer zr.Close()
	var out []record
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), "\t")
		if len(parts) != 3 || !ValidKode(parts[0]) || parts[1] == "" {
			return nil, fmt.Errorf("wilayah: bad data line %q", sc.Text())
		}
		out = append(out, record{parts[0], parts[1], parts[2]})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("wilayah: read data: %w", err)
	}
	return out, nil
}

// EnsureLoaded mengimpor data yang disertakan jika versinya berbeda dengan
// yang ada di database. Aman dijalankan bersamaan oleh beberapa instance
// (advisory lock) dan berulang (upsert). loaded true jika impor dilakukan.
func EnsureLoaded(ctx context.Context, db database.DB) (loaded bool, err error) {
	return load(ctx, db, false)
}

// Reload mengimpor ulang data walau versinya sama.
func Reload(ctx context.Context, db database.DB) error {
	_, err := load(ctx, db, true)
	return err
}

func load(ctx context.Context, db database.DB, force bool) (bool, error) {
	version := DataVersion()
	loaded := false
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('akra.wilayah'))`); err != nil {
			return err
		}
		var current string
		err := tx.QueryRow(ctx, `SELECT versi FROM wilayah_meta`).Scan(&current)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if current == version && !force {
			return nil
		}
		recs, err := readData()
		if err != nil {
			return err
		}
		// DROP dulu: di dalam transaksi luar (savepoint) ON COMMIT DROP baru
		// berlaku saat transaksi luar selesai.
		if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS pg_temp.wilayah_baru`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE wilayah_baru (kode text, nama text, kode_pos text) ON COMMIT DROP`); err != nil {
			return err
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"wilayah_baru"}, []string{"kode", "nama", "kode_pos"},
			pgx.CopyFromSlice(len(recs), func(i int) ([]any, error) {
				var pos any
				if recs[i].kodePos != "" {
					pos = recs[i].kodePos
				}
				return []any{recs[i].kode, recs[i].nama, pos}, nil
			})); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO wilayah (kode, nama, tingkat, induk, kode_pos, aktif)
			SELECT kode, nama, length(kode) - length(replace(kode, '.', '')) + 1,
			       nullif(regexp_replace(kode, '\.[0-9]+$', ''), kode), kode_pos, true
			FROM wilayah_baru
			ON CONFLICT (kode) DO UPDATE SET nama = EXCLUDED.nama, kode_pos = EXCLUDED.kode_pos, aktif = true`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE wilayah w SET aktif = false
			WHERE aktif AND NOT EXISTS (SELECT 1 FROM wilayah_baru b WHERE b.kode = w.kode)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO wilayah_meta (id, versi, jumlah, imported_at) VALUES (true, $1, $2, now())
			ON CONFLICT (id) DO UPDATE SET versi = EXCLUDED.versi, jumlah = EXCLUDED.jumlah, imported_at = now()`,
			version, len(recs)); err != nil {
			return err
		}
		loaded = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("wilayah: load: %w", err)
	}
	return loaded, nil
}
