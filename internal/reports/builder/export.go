package builder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"akra/internal/platform/database"
	"akra/internal/reports/export"
)

// Lebar halaman PDF yang bisa dipakai tabel (A4 dikurangi margin 12 mm).
const (
	pdfPortraitWidth  = 186
	pdfLandscapeWidth = 273
	// pdfFixedWidth adalah lebar kolom angka dan tanggal.
	pdfFixedWidth = 26
	// pdfMinText adalah lebar minimum kolom teks.
	pdfMinText = 20
	// landscapeFrom: tabel dengan kolom sebanyak ini atau lebih dicetak mendatar.
	landscapeFrom = 6
)

// ExportTable menjalankan konfigurasi c lewat Compile (jalur yang sama dengan
// pratinjau T78) dan menyusun tabel ekspor T47. rows adalah jumlah baris yang
// terbaca; dengan Options bawaan query berhenti di MaxRows+1, sehingga rows >
// MaxRows berarti laporan harus dibuat sebagai job (T49), yang memanggil
// fungsi ini dengan NoLimit.
//
// Pemeriksaan hak akses (pemilik, berbagi, peran sumber) dilakukan pemanggil.
func ExportTable(ctx context.Context, q database.DBTX, reg *Registry, schoolID, schoolName, title string, c Config, printedAt time.Time, opts Options) (export.Table, int, error) {
	qr, err := Compile(reg, schoolID, c, opts)
	if err != nil {
		return export.Table{}, 0, err
	}
	src, _ := reg.Source(c.Source)
	t := export.Table{Title: title, SchoolName: schoolName, PrintedAt: printedAt, Meta: describe(src, c),
		Landscape: len(qr.Columns) >= landscapeFrom}
	t.Columns = pdfColumns(qr.Columns, t.Landscape)
	rows, err := q.Query(ctx, qr.SQL, qr.Args...)
	if err != nil {
		return export.Table{}, 0, fmt.Errorf("builder: export %s: %w", c.Source, err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return export.Table{}, 0, fmt.Errorf("builder: export %s: %w", c.Source, err)
		}
		n++
		if !opts.NoLimit && n > MaxRows {
			continue // hanya dihitung: laporan ini harus lewat job
		}
		cells := make([]any, len(vals))
		for i, v := range vals {
			cells[i] = exportCell(qr.Columns[i].Type, v)
		}
		t.Rows = append(t.Rows, cells)
	}
	if err := rows.Err(); err != nil {
		return export.Table{}, 0, fmt.Errorf("builder: export %s: %w", c.Source, err)
	}
	return t, n, nil
}

// exportCell mengubah nilai database menjadi sel ekspor: nominal sebagai
// export.Money (angka di Excel, rupiah di PDF), bilangan bulat sebagai
// int64, tanggal dengan format Indonesia, NULL sebagai sel kosong.
func exportCell(t ColumnType, v any) any {
	if v == nil {
		return ""
	}
	switch t {
	case TypeMoney:
		if n, ok := toInt64(v); ok {
			return export.Money(n)
		}
	case TypeInt:
		if n, ok := toInt64(v); ok {
			return n
		}
	}
	return FormatCell(t, v)
}

// pdfColumns membagi lebar halaman: kolom angka/tanggal tetap, sisanya
// dibagi rata ke kolom teks.
func pdfColumns(cols []Column, landscape bool) []export.Column {
	total := pdfPortraitWidth
	if landscape {
		total = pdfLandscapeWidth
	}
	fixed, texts := 0, 0
	for _, c := range cols {
		if c.Type == TypeText {
			texts++
		} else {
			fixed += pdfFixedWidth
		}
	}
	textW := pdfMinText
	if texts > 0 && (total-fixed)/texts > pdfMinText {
		textW = (total - fixed) / texts
	}
	out := make([]export.Column, len(cols))
	for i, c := range cols {
		w := pdfFixedWidth
		if c.Type == TypeText {
			w = textW
		}
		if texts == 0 {
			w = total / len(cols)
		}
		out[i] = export.Column{Header: c.Label, Width: float64(w), Numeric: c.Type == TypeInt || c.Type == TypeMoney}
	}
	return out
}

// describe menjelaskan sumber, filter, dan urutan untuk kepala laporan.
func describe(src *Source, c Config) []string {
	label := func(key string) string {
		if col, ok := src.Column(key); ok {
			return col.Label
		}
		return key
	}
	meta := []string{"Sumber: " + src.Label}
	var filters []string
	for _, f := range c.Filters {
		v := f.Value
		if f.Op == OpIn {
			v = strings.Join(f.Values, ", ")
		}
		filters = append(filters, label(f.Column)+" "+opLabels[f.Op]+" "+v)
	}
	if len(filters) > 0 {
		meta = append(meta, "Filter: "+strings.Join(filters, "; "))
	}
	var order []string
	for _, o := range c.Order {
		dir := "naik"
		if o.Desc {
			dir = "turun"
		}
		order = append(order, label(o.Column)+" ("+dir+")")
	}
	if len(order) > 0 {
		meta = append(meta, "Urutan: "+strings.Join(order, ", "))
	}
	// Tanggal cetak ditulis export (kaki PDF, sheet Info) dari Table.PrintedAt.
	return meta
}
