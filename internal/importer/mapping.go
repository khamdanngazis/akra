package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Mapping impor (T81). Mapping hanya mengubah judul kolom: baris hasil
// mapping memakai kolom baku yang sama dengan template, lalu masuk validasi
// yang tidak berubah (T54). Kolom baku ditetapkan di kode (Targets); mapping
// tidak bisa menambah kolom baru.

// Target impor yang bisa dipetakan.
const (
	TargetStudents = "students"
	TargetGrades   = "grades"
)

// Indeks kolom baku impor nilai (T82).
const (
	GradeColNIS = iota
	GradeColSubjectCode
	GradeColComponent
	GradeColScore
	// NumGradeColumns adalah jumlah kolom baku impor nilai.
	NumGradeColumns
)

// GradeColumns adalah kolom baku impor nilai (dipakai T82). Nilai kosong
// disimpan sebagai belum dinilai, jadi kolomnya tidak wajib diisi per baris,
// tetapi tetap wajib ada di mapping.
var GradeColumns = [NumGradeColumns]Column{
	GradeColNIS:         {Header: "NIS", Required: true, Format: "Teks, misalnya 007"},
	GradeColSubjectCode: {Header: "Kode mapel", Required: true, Format: "Kode mata pelajaran, misalnya MTK"},
	GradeColComponent:   {Header: "Nama komponen", Required: true, Format: "Nama komponen nilai, misalnya UTS"},
	GradeColScore:       {Header: "Nilai", Required: true, Format: "0–100, boleh koma; kosong = belum dinilai"},
}

// Target adalah satu jenis data impor beserta kolom bakunya.
type Target struct {
	Key, Label string
	Columns    []Column
}

// Targets adalah target yang bisa dipetakan, urut tampil. Jenis tagihan
// (fee_types) belum punya kolom baku dan impor, jadi belum didukung.
var Targets = []Target{
	{Key: TargetStudents, Label: "Siswa", Columns: StudentColumns[:]},
	{Key: TargetGrades, Label: "Nilai", Columns: GradeColumns[:]},
}

// TargetByKey mengembalikan target dengan kunci key.
func TargetByKey(key string) (Target, bool) {
	for _, t := range Targets {
		if t.Key == key {
			return t, true
		}
	}
	return Target{}, false
}

// ColumnMap memetakan judul kolom baku ke judul kolom di file sekolah.
// Kolom baku yang tidak wajib boleh tidak dipetakan.
type ColumnMap map[string]string

// Mapping adalah mapping impor tersimpan.
type Mapping struct {
	ID          string
	Name        string
	Target      string
	Columns     ColumnMap
	CreatedBy   string
	CreatorName string
	CreatedAt   time.Time
}

const (
	maxMappingName  = 80
	maxSourceHeader = 100
)

var (
	// ErrMappingNotFound: mapping tidak ada atau milik sekolah lain.
	ErrMappingNotFound = errors.New("importer: mapping not found")
)

// MappingError berisi pesan per field form ("name", "target", atau judul
// kolom baku).
type MappingError struct {
	Fields map[string]string
}

func (e *MappingError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return "importer: invalid mapping: " + strings.Join(keys, ", ")
}

// ValidateMapping memeriksa nama, target, dan column_map: setiap kolom wajib
// target punya sumber, kunci hanya kolom baku target, sumber tidak kosong
// dan tidak dipakai dua kolom baku. Jika header (judul kolom file contoh)
// diberikan, setiap sumber harus ada di header itu.
func ValidateMapping(name, target string, m ColumnMap, header []string) (string, ColumnMap, error) {
	fields := map[string]string{}
	name = strings.Join(strings.Fields(name), " ")
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		fields["name"] = "Isi nama mapping."
	case n > maxMappingName:
		fields["name"] = fmt.Sprintf("Nama maksimal %d karakter.", maxMappingName)
	}
	t, ok := TargetByKey(target)
	if !ok {
		fields["target"] = "Pilih jenis data dari daftar."
		return "", nil, &MappingError{Fields: fields}
	}
	known := map[string]bool{}
	for _, c := range t.Columns {
		known[c.Header] = true
	}
	inHeader := map[string]bool{}
	for _, h := range header {
		inHeader[headerKey(h)] = true
	}
	out := ColumnMap{}
	usedBy := map[string]string{}
	for std, src := range m {
		src = clean(src)
		if !known[std] {
			fields["target"] = fmt.Sprintf("Kolom baku %q tidak dikenal untuk %s.", std, t.Label)
			continue
		}
		if src == "" {
			continue
		}
		key := headerKey(src)
		switch {
		case utf8.RuneCountInString(src) > maxSourceHeader:
			fields[std] = fmt.Sprintf("Judul kolom maksimal %d karakter.", maxSourceHeader)
		case header != nil && !inHeader[key]:
			fields[std] = fmt.Sprintf("Kolom %q tidak ada di file contoh.", src)
		case usedBy[key] != "":
			fields[std] = fmt.Sprintf("Kolom %q sudah dipakai untuk %q.", src, usedBy[key])
		default:
			usedBy[key] = std
			out[std] = src
		}
	}
	for _, c := range t.Columns {
		if c.Required && out[c.Header] == "" && fields[c.Header] == "" {
			fields[c.Header] = "Kolom wajib: pilih kolom file untuk " + strconv.Quote(c.Header) + "."
		}
	}
	if len(fields) > 0 {
		return "", nil, &MappingError{Fields: fields}
	}
	return name, out, nil
}

// MappingSourceError: file yang diimpor tidak punya kolom yang dirujuk
// mapping.
type MappingSourceError struct {
	Mapping string
	// Missing berisi pasangan "judul file (kolom baku)".
	Missing []string
}

func (e *MappingSourceError) Error() string {
	return "importer: file lacks mapped columns: " + strings.Join(e.Missing, ", ")
}

// Message adalah pesan untuk pengguna.
func (e *MappingSourceError) Message() string {
	return "File tidak punya kolom yang dipakai mapping " + strconv.Quote(e.Mapping) + ": " + joinID(e.Missing) +
		". Periksa judul kolom di file, atau pilih/buat mapping lain."
}

// headerResolver mengembalikan fungsi pencocok header untuk scanSheet: posisi
// kolom sheet untuk setiap kolom baku target menurut mapping. Kolom baku
// yang tidak dipetakan bernilai -1 (sel kosong).
func (mp Mapping) headerResolver(t Target) func(cells []string) ([]int, error) {
	return func(cells []string) ([]int, error) {
		pos := map[string]int{}
		dup := map[string]bool{}
		for i, c := range cells {
			k := headerKey(c)
			if k == "" {
				continue
			}
			if _, seen := pos[k]; seen {
				dup[k] = true
				continue
			}
			pos[k] = i
		}
		cols := make([]int, len(t.Columns))
		var missing []string
		for i, c := range t.Columns {
			src, mapped := mp.Columns[c.Header]
			if !mapped || src == "" {
				cols[i] = -1
				continue
			}
			k := headerKey(src)
			p, ok := pos[k]
			switch {
			case !ok:
				missing = append(missing, strconv.Quote(src)+" ("+c.Header+")")
			case dup[k]:
				return nil, &DuplicateColumnError{Column: src}
			default:
				cols[i] = p
			}
		}
		if missing != nil {
			return nil, &MappingSourceError{Mapping: mp.Name, Missing: missing}
		}
		return cols, nil
	}
}

// StandardMapping adalah mapping identitas: judul kolom file sama dengan
// judul kolom baku target (dipakai jika pengguna tidak memilih mapping).
func StandardMapping(target string) Mapping {
	t, _ := TargetByKey(target)
	m := Mapping{Name: "Judul kolom baku", Target: target, Columns: ColumnMap{}}
	for _, c := range t.Columns {
		m.Columns[c.Header] = c.Header
	}
	return m
}

// MappedRows adalah hasil membaca file lewat mapping: satu baris per baris
// data, Values sesuai urutan kolom baku target.
type MappedRows struct {
	Sheet   string
	Target  string
	Data    []MappedRow
	Skipped []Skip
}

// MappedRow adalah satu baris hasil mapping.
type MappedRow struct {
	Line   int      `json:"line"`
	Values []string `json:"values"`
}

// mappedHeaderTries adalah jumlah baris berisi pertama yang dicoba sebagai
// header file bermapping: file ekspor aplikasi lain sering diawali baris
// judul laporan, nama sekolah, atau tanggal cetak.
const mappedHeaderTries = 10

// ParseMapped membaca file dengan mapping mp: kolom file dipetakan ke kolom
// baku target, dengan batas dan aturan baris kosong/contoh yang sama dengan
// ParseFile. Header adalah baris pertama (di antara mappedHeaderTries baris
// berisi pertama) yang memuat semua kolom yang dirujuk mapping; baris di
// atasnya diabaikan. MappingSourceError jika tidak ada baris seperti itu.
func ParseMapped(r io.Reader, mp Mapping) (MappedRows, error) {
	t, ok := TargetByKey(mp.Target)
	if !ok {
		return MappedRows{}, fmt.Errorf("importer: unknown mapping target %q", mp.Target)
	}
	sheet, data, skipped, err := scanSheet(r, mp.headerResolver(t), mappedHeaderTries)
	if err != nil {
		return MappedRows{}, err
	}
	out := MappedRows{Sheet: sheet, Target: t.Key, Skipped: skipped}
	for _, d := range data {
		out.Data = append(out.Data, MappedRow{Line: d.Line, Values: d.Values})
	}
	return out, nil
}

// StudentRows mengubah hasil mapping target siswa menjadi Rows yang sama
// dengan ParseFile, siap divalidasi T54.
func (m MappedRows) StudentRows() (Rows, error) {
	if m.Target != TargetStudents {
		return Rows{}, fmt.Errorf("importer: mapped rows are %q, not students", m.Target)
	}
	data := make([]rawRow, len(m.Data))
	for i, d := range m.Data {
		data[i] = rawRow{Line: d.Line, Values: d.Values}
	}
	return studentRows(m.Sheet, data, m.Skipped), nil
}

// MappingService menyimpan mapping impor.
type MappingService struct {
	db database.DB
}

// NewMappingService mengembalikan MappingService.
func NewMappingService(db database.DB) *MappingService { return &MappingService{db: db} }

// ActionMappingCreate adalah aksi audit pembuatan mapping.
const ActionMappingCreate = "import_mapping.create"

// Create memvalidasi lalu menyimpan mapping baru beserta auditnya. header
// adalah judul kolom file contoh (nil jika tidak ada).
func (s *MappingService) Create(ctx context.Context, actor school.Actor, name, target string, m ColumnMap, header []string) (Mapping, error) {
	name, m, err := ValidateMapping(name, target, m, header)
	if err != nil {
		return Mapping{}, err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return Mapping{}, err
	}
	var saved Mapping
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO import_mappings (school_id, name, target, column_map, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			actor.SchoolID, name, target, raw, actor.UserID).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return &MappingError{Fields: map[string]string{"name": "Nama mapping sudah dipakai di sekolah ini."}}
		}
		if err != nil {
			return fmt.Errorf("importer: insert mapping: %w", err)
		}
		if saved, err = getMapping(ctx, tx, actor.SchoolID, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionMappingCreate, EntityType: "import_mapping", EntityID: id,
			After: map[string]any{"name": name, "target": target, "column_map": m}})
	})
	return saved, err
}

const mappingSelect = `SELECT m.id, m.name, m.target, m.column_map, m.created_by, u.full_name, m.created_at
	FROM import_mappings m JOIN users u ON u.school_id = m.school_id AND u.id = m.created_by`

func scanMapping(row pgx.CollectableRow) (Mapping, error) {
	var m Mapping
	var raw []byte
	if err := row.Scan(&m.ID, &m.Name, &m.Target, &raw, &m.CreatedBy, &m.CreatorName, &m.CreatedAt); err != nil {
		return m, err
	}
	if err := json.Unmarshal(raw, &m.Columns); err != nil {
		return m, fmt.Errorf("importer: decode mapping %s: %w", m.ID, err)
	}
	return m, nil
}

func getMapping(ctx context.Context, q database.DBTX, schoolID, id string) (Mapping, error) {
	if !database.IsUUID(id) {
		return Mapping{}, ErrMappingNotFound
	}
	rows, err := q.Query(ctx, mappingSelect+` WHERE m.school_id = $1 AND m.id = $2`, schoolID, id)
	if err != nil {
		return Mapping{}, fmt.Errorf("importer: get mapping: %w", err)
	}
	m, err := pgx.CollectExactlyOneRow(rows, scanMapping)
	if errors.Is(err, pgx.ErrNoRows) {
		return Mapping{}, ErrMappingNotFound
	}
	return m, err
}

// Get mengembalikan mapping milik schoolID (sekolah lain: ErrMappingNotFound).
func (s *MappingService) Get(ctx context.Context, schoolID, id string) (Mapping, error) {
	return getMapping(ctx, s.db, schoolID, id)
}

// List mengembalikan mapping sekolah schoolID, urut nama. Mapping sekolah
// lain tidak pernah ikut.
func (s *MappingService) List(ctx context.Context, schoolID string) ([]Mapping, error) {
	if !database.IsUUID(schoolID) {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, mappingSelect+` WHERE m.school_id = $1 ORDER BY lower(m.name)`, schoolID)
	if err != nil {
		return nil, fmt.Errorf("importer: list mappings: %w", err)
	}
	return pgx.CollectRows(rows, scanMapping)
}
