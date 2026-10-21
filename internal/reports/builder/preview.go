package builder

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/money"
)

const (
	// PreviewRows adalah jumlah baris pratinjau (T78).
	PreviewRows = 50
	// previewTimeout membatasi query pratinjau di database.
	previewTimeout = "5s"
)

// Preview adalah hasil pratinjau: judul kolom dan sel yang sudah diformat.
type Preview struct {
	Columns []Column
	Rows    [][]string
	// More true jika ada lebih dari PreviewRows baris.
	More bool
}

// Preview memvalidasi konfigurasi c untuk id (sumber, kolom, operator, nilai,
// dan hak peran; sama dengan saat menyimpan) lalu menjalankannya dengan
// batas PreviewRows di transaksi baca-saja dengan batas waktu.
func (s *ConfigService) Preview(ctx context.Context, id auth.Identity, c Config) (Preview, error) {
	if fields := s.configFields(id, c); len(fields) > 0 {
		return Preview{}, &ConfigError{Fields: fields}
	}
	q, err := Compile(s.reg, id.SchoolID, c, Options{Limit: PreviewRows + 1})
	if err != nil {
		return Preview{}, err
	}
	p := Preview{Columns: q.Columns}
	// Pratinjau tidak pernah menulis: transaksinya selalu dibatalkan, sehingga
	// pengaturan SET LOCAL di bawah ikut hilang (juga bila db adalah transaksi
	// luar, mis. di uji, karena di sana Begin membuat savepoint).
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Preview{}, fmt.Errorf("builder: preview begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = runPreview(ctx, tx, q, &p)
	if err != nil {
		return Preview{}, fmt.Errorf("builder: preview %s: %w", c.Source, err)
	}
	return p, nil
}

// runPreview menjalankan q baca-saja dan berbatas waktu (berjaga-jaga walau
// SQL-nya dari registry), lalu mengisi p.
func runPreview(ctx context.Context, tx pgx.Tx, q Query, p *Preview) error {
	for _, set := range []string{`SET LOCAL transaction_read_only = on`, `SET LOCAL statement_timeout = '` + previewTimeout + `'`} {
		if _, err := tx.Exec(ctx, set); err != nil {
			return err
		}
	}
	rows, err := tx.Query(ctx, q.SQL, q.Args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return err
		}
		if len(p.Rows) == PreviewRows {
			p.More = true
			continue
		}
		cells := make([]string, len(vals))
		for i, v := range vals {
			cells[i] = FormatCell(q.Columns[i].Type, v)
		}
		p.Rows = append(p.Rows, cells)
	}
	return rows.Err()
}

// FormatCell memformat satu nilai sesuai tipe kolom: rupiah untuk money,
// tanggal Indonesia untuk date, "–" untuk NULL.
func FormatCell(t ColumnType, v any) string {
	if v == nil {
		return "–"
	}
	switch t {
	case TypeMoney:
		if n, ok := toInt64(v); ok {
			return money.FormatRupiah(n)
		}
	case TypeInt:
		if n, ok := toInt64(v); ok {
			return strconv.FormatInt(n, 10)
		}
	case TypeDate:
		if d, ok := v.(time.Time); ok {
			return clock.FormatDateID(d)
		}
	}
	return fmt.Sprint(v)
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case int16:
		return int64(n), true
	case int:
		return int64(n), true
	}
	return 0, false
}
