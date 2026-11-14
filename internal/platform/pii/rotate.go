package pii

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Column menunjuk satu kolom bytea yang berisi ciphertext.
type Column struct {
	Table  string // mis. "student_profiles"
	Key    string // kolom kunci baris, mis. "student_id"
	Column string // mis. "nik_enc"
	// AADName adalah nama logis yang dipakai sebagai aad, mis.
	// "student_profiles.nik". Harus sama dengan aad saat Encrypt.
	AADName string
}

var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func (c Column) validate() error {
	for _, s := range []string{c.Table, c.Key, c.Column} {
		if !identRe.MatchString(s) {
			return fmt.Errorf("pii: invalid identifier %q", s)
		}
	}
	if c.AADName == "" {
		return errors.New("pii: column AADName is required")
	}
	return nil
}

// Rotate mengenkripsi ulang semua nilai di kolom c yang belum memakai kunci
// aktif, per batch dalam transaksi terpisah, dan mengembalikan jumlah baris
// yang diubah. Aman dijalankan ulang dan bersamaan (SKIP LOCKED). Blind
// index tidak berubah karena kuncinya tidak dirotasi.
func Rotate(ctx context.Context, db database.DB, k *Keyring, c Column, batch int) (int, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}
	if batch < 1 {
		batch = 500
	}
	table := pgx.Identifier{c.Table}.Sanitize()
	key := pgx.Identifier{c.Key}.Sanitize()
	col := pgx.Identifier{c.Column}.Sanitize()
	sel := fmt.Sprintf(`SELECT %s::text, %s FROM %s WHERE %s IS NOT NULL AND get_byte(%s, 0) <> $1 LIMIT $2 FOR UPDATE SKIP LOCKED`, key, col, table, col, col)
	upd := fmt.Sprintf(`UPDATE %s SET %s = $1 WHERE %s::text = $2`, table, col, key)

	total := 0
	for {
		n, err := rotateBatch(ctx, db, k, c, sel, upd, batch)
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, nil
		}
	}
}

func rotateBatch(ctx context.Context, db database.DB, k *Keyring, c Column, sel, upd string, batch int) (n int, err error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("pii rotate %s: begin: %w", c.AADName, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	rows, err := tx.Query(ctx, sel, int(k.active), batch)
	if err != nil {
		return 0, fmt.Errorf("pii rotate %s: select: %w", c.AADName, err)
	}
	type item struct {
		key string
		ct  []byte
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (item, error) {
		var it item
		return it, r.Scan(&it.key, &it.ct)
	})
	if err != nil {
		return 0, fmt.Errorf("pii rotate %s: scan: %w", c.AADName, err)
	}
	for _, it := range items {
		plain, err := k.Decrypt(it.ct, c.AADName)
		if err != nil {
			// Kunci baris tidak berisi data pribadi, aman ditulis di error.
			return 0, fmt.Errorf("pii rotate %s: row %s: %w", c.AADName, it.key, err)
		}
		ct, err := k.Encrypt(plain, c.AADName)
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, upd, ct, it.key); err != nil {
			return 0, fmt.Errorf("pii rotate %s: update: %w", c.AADName, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("pii rotate %s: commit: %w", c.AADName, err)
	}
	return len(items), nil
}
