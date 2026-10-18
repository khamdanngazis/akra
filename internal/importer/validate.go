package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/classes"
	"akra/internal/platform/database"
	"akra/internal/school"
	"akra/internal/students"
)

// Batas panjang sama dengan form siswa dan wali (T20, T23), agar data hasil
// impor tetap bisa diubah lewat form.
const (
	maxNISLen          = 30
	maxFullNameLen     = 100
	maxGuardianNameLen = 100
	minBirthYear       = 1950
)

// ErrNoActiveYear: sekolah belum punya tahun ajaran aktif, sehingga kelas
// tidak bisa dicocokkan.
var ErrNoActiveYear = errors.New("importer: no active academic year")

// ErrBatchValidated: batch sudah divalidasi sebelumnya.
var ErrBatchValidated = errors.New("importer: batch already validated")

// Jenis kesalahan baris, untuk ringkasan per jenis (T56).
const (
	KindNISEmpty      = "nis_kosong"
	KindNISTooLong    = "nis_terlalu_panjang"
	KindNISDuplicate  = "nis_ganda"
	KindNameEmpty     = "nama_kosong"
	KindNameTooLong   = "nama_terlalu_panjang"
	KindBirthDate     = "tanggal_lahir"
	KindGender        = "jenis_kelamin"
	KindClassEmpty    = "kelas_kosong"
	KindClassNotFound = "kelas_tidak_ada"
	KindGuardianName  = "nama_wali"
	KindGuardianPhone = "telepon_wali"
	KindGuardianEmail = "email_wali"
)

// RowError adalah satu kesalahan pada satu baris.
type RowError struct {
	Column  string `json:"column"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// ValidRow adalah baris yang lolos validasi, sudah dinormalisasi.
type ValidRow struct {
	NIS           string `json:"nis"`
	FullName      string `json:"full_name"`
	BirthDate     string `json:"birth_date,omitempty"` // YYYY-MM-DD
	Gender        string `json:"gender,omitempty"`
	ClassID       string `json:"class_id"`
	ClassName     string `json:"class_name"`
	GuardianName  string `json:"guardian_name,omitempty"`
	GuardianPhone string `json:"guardian_phone,omitempty"` // format +62…
	GuardianEmail string `json:"guardian_email,omitempty"`
	// Update: NIS sudah ada di sekolah ini, siswa akan diperbarui.
	Update bool `json:"update"`
}

// RowContext adalah data pembanding untuk ValidateRow: kelas tahun ajaran
// aktif, NIS yang sudah ada di database, dan NIS yang sudah muncul di file.
type RowContext struct {
	Today    time.Time
	YearName string
	classes  map[string]classes.Class // kunci: nama kelas huruf kecil
	existing map[string]bool
	seen     map[string]int // NIS -> baris pertama
}

// NewRowContext menyiapkan RowContext untuk sekolah: kelas tahun ajaran
// aktif dan NIS di rows yang sudah ada di database. ErrNoActiveYear jika
// belum ada tahun ajaran aktif.
func NewRowContext(ctx context.Context, q database.DBTX, schoolID string, today time.Time, rows []Row) (*RowContext, error) {
	year, err := school.GetActiveAcademicYear(ctx, q, schoolID)
	if errors.Is(err, school.ErrNotFound) {
		return nil, ErrNoActiveYear
	}
	if err != nil {
		return nil, err
	}
	list, err := classes.ListByYear(ctx, q, schoolID, year.ID)
	if err != nil {
		return nil, err
	}
	rc := &RowContext{Today: today, YearName: year.Name, classes: make(map[string]classes.Class, len(list)),
		existing: map[string]bool{}, seen: make(map[string]int, len(rows))}
	for _, c := range list {
		rc.classes[strings.ToLower(c.Name)] = c
	}
	nis := make([]string, 0, len(rows))
	for _, r := range rows {
		if n := students.NormalizeNIS(r.Values[ColNIS]); n != "" {
			nis = append(nis, n)
		}
	}
	found, err := q.Query(ctx, `SELECT nis FROM students WHERE school_id = $1 AND nis = ANY($2)`, schoolID, nis)
	if err != nil {
		return nil, fmt.Errorf("importer: existing nis: %w", err)
	}
	defer found.Close()
	for found.Next() {
		var n string
		if err := found.Scan(&n); err != nil {
			return nil, fmt.Errorf("importer: existing nis: %w", err)
		}
		rc.existing[n] = true
	}
	return rc, found.Err()
}

// ValidateRow memeriksa satu baris. Kesalahan pada satu kolom tidak
// menghentikan pemeriksaan kolom lain, jadi semua kesalahan baris tampil
// sekaligus. Baris dicatat ke rc (deteksi NIS ganda), jadi panggil sesuai
// urutan baris di file.
func ValidateRow(row Row, rc *RowContext) (ValidRow, []RowError) {
	v := row.Values
	var errs []RowError
	add := func(col int, kind, msg string) {
		errs = append(errs, RowError{Column: StudentColumns[col].Header, Kind: kind, Message: msg})
	}
	out := ValidRow{
		NIS:          students.NormalizeNIS(v[ColNIS]),
		FullName:     strings.Join(strings.Fields(v[ColFullName]), " "),
		GuardianName: strings.Join(strings.Fields(v[ColGuardianName]), " "),
	}

	switch {
	case out.NIS == "":
		add(ColNIS, KindNISEmpty, "NIS wajib diisi.")
	case utf8.RuneCountInString(out.NIS) > maxNISLen:
		add(ColNIS, KindNISTooLong, fmt.Sprintf("NIS maksimal %d karakter.", maxNISLen))
	default:
		if first, dup := rc.seen[out.NIS]; dup {
			add(ColNIS, KindNISDuplicate, fmt.Sprintf("NIS %s sudah dipakai di baris %d file ini.", out.NIS, first))
		} else {
			rc.seen[out.NIS] = row.Line
			out.Update = rc.existing[out.NIS]
		}
	}

	switch {
	case out.FullName == "":
		add(ColFullName, KindNameEmpty, "Nama lengkap wajib diisi.")
	case utf8.RuneCountInString(out.FullName) > maxFullNameLen:
		add(ColFullName, KindNameTooLong, fmt.Sprintf("Nama lengkap maksimal %d karakter.", maxFullNameLen))
	}

	if d := v[ColBirthDate]; d != "" {
		bd, err := time.Parse(time.DateOnly, d)
		switch {
		case err != nil:
			add(ColBirthDate, KindBirthDate, fmt.Sprintf("Tanggal lahir %q tidak sesuai format. Gunakan TTTT-BB-HH, misalnya 2019-05-02.", d))
		case bd.After(rc.Today):
			add(ColBirthDate, KindBirthDate, "Tanggal lahir tidak boleh di masa depan.")
		case bd.Year() < minBirthYear:
			add(ColBirthDate, KindBirthDate, fmt.Sprintf("Tanggal lahir sebelum tahun %d tidak valid.", minBirthYear))
		default:
			out.BirthDate = bd.Format(time.DateOnly)
		}
	}

	if g := strings.ToUpper(v[ColGender]); g != "" {
		if g != students.GenderMale && g != students.GenderFemale {
			add(ColGender, KindGender, fmt.Sprintf("Jenis kelamin %q tidak dikenal. Isi L atau P.", v[ColGender]))
		} else {
			out.Gender = g
		}
	}

	if name := strings.TrimSpace(v[ColClass]); name == "" {
		add(ColClass, KindClassEmpty, "Kelas wajib diisi.")
	} else if c, ok := rc.classes[strings.ToLower(name)]; !ok {
		add(ColClass, KindClassNotFound, fmt.Sprintf("Kelas %s tidak ditemukan di tahun ajaran aktif (%s).", name, rc.YearName))
	} else {
		out.ClassID, out.ClassName = c.ID, c.Name
	}

	if p := v[ColGuardianPhone]; p != "" {
		phone, err := students.NormalizePhone(p)
		if err != nil {
			add(ColGuardianPhone, KindGuardianPhone, fmt.Sprintf("Telepon wali %q tidak valid. Contoh: 0812-3456-7890.", p))
		}
		out.GuardianPhone = phone
	}
	if e := v[ColGuardianEmail]; e != "" {
		email := strings.ToLower(e)
		if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
			add(ColGuardianEmail, KindGuardianEmail, fmt.Sprintf("Email wali %q tidak valid.", e))
		}
		out.GuardianEmail = email
	}
	switch {
	case out.GuardianName == "" && (v[ColGuardianPhone] != "" || v[ColGuardianEmail] != ""):
		add(ColGuardianName, KindGuardianName, "Nama wali wajib diisi jika telepon atau email wali diisi.")
	case utf8.RuneCountInString(out.GuardianName) > maxGuardianNameLen:
		add(ColGuardianName, KindGuardianName, fmt.Sprintf("Nama wali maksimal %d karakter.", maxGuardianNameLen))
	}

	if errs != nil {
		return ValidRow{}, errs
	}
	return out, nil
}

// Status baris impor.
const (
	RowStatusValid   = "valid"
	RowStatusError   = "error"
	RowStatusSkipped = "dilewati"
)

// Tindakan untuk baris valid.
const (
	ActionCreate = "baru"
	ActionUpdate = "diperbarui"
)

// ValidateBatch memvalidasi batch berstatus diunggah: setiap baris dicatat di
// import_rows (valid, error, atau dilewati), lalu batch berstatus divalidasi
// dengan ringkasan jumlahnya. Tidak menulis ke students, guardians, atau
// enrollments.
func (s *Service) ValidateBatch(ctx context.Context, schoolID, batchID string, today time.Time) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return validateBatch(ctx, tx, schoolID, batchID, today)
	})
}

func validateBatch(ctx context.Context, q pgx.Tx, schoolID, batchID string, today time.Time) error {
	var (
		status  string
		skipped []byte
	)
	err := q.QueryRow(ctx, `SELECT status, skipped FROM import_batches WHERE school_id = $1 AND id = $2 FOR UPDATE`, schoolID, batchID).Scan(&status, &skipped)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBatchNotFound
	}
	if err != nil {
		return fmt.Errorf("importer: lock batch: %w", err)
	}
	if status != StatusUploaded {
		return ErrBatchValidated
	}
	rows, err := parsedRows(ctx, q, schoolID, batchID)
	if err != nil {
		return err
	}
	var skips []Skip
	if err := json.Unmarshal(skipped, &skips); err != nil {
		return fmt.Errorf("importer: decode skipped: %w", err)
	}
	rc, err := NewRowContext(ctx, q, schoolID, today, rows)
	if err != nil {
		return err
	}

	records := make([][]any, 0, len(rows)+len(skips))
	var valid, invalid, updates int
	for _, r := range rows {
		vr, errs := ValidateRow(r, rc)
		if errs != nil {
			invalid++
			records = append(records, []any{schoolID, batchID, r.Line, r.Values, RowStatusError, nil, nil, errs})
			continue
		}
		valid++
		action := ActionCreate
		if vr.Update {
			action, updates = ActionUpdate, updates+1
		}
		records = append(records, []any{schoolID, batchID, r.Line, r.Values, RowStatusValid, action, vr, []RowError{}})
	}
	for _, sk := range skips {
		records = append(records, []any{schoolID, batchID, sk.Line, []string{}, RowStatusSkipped, nil, nil, []RowError{}})
	}
	_, err = q.CopyFrom(ctx, pgx.Identifier{"import_rows"},
		[]string{"school_id", "batch_id", "line", "raw", "status", "action", "data", "errors"}, pgx.CopyFromRows(records))
	if err != nil {
		return fmt.Errorf("importer: insert rows: %w", err)
	}
	_, err = q.Exec(ctx, `
		UPDATE import_batches
		SET status = $3, valid_rows = $4, error_rows = $5, update_rows = $6, parsed_rows = NULL
		WHERE school_id = $1 AND id = $2`, schoolID, batchID, StatusValidated, valid, invalid, updates)
	if err != nil {
		return fmt.Errorf("importer: update batch: %w", err)
	}
	return nil
}

// ErrorRow adalah baris yang gagal validasi, untuk ditampilkan.
type ErrorRow struct {
	Line   int
	NIS    string
	Name   string
	Class  string
	Errors []RowError
}

// ErrorRows mengembalikan paling banyak limit baris error batch, urut nomor
// baris.
func (s *Service) ErrorRows(ctx context.Context, schoolID, batchID string, limit int) ([]ErrorRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT line, raw, errors FROM import_rows
		WHERE school_id = $1 AND batch_id = $2 AND status = $3
		ORDER BY line LIMIT $4`, schoolID, batchID, RowStatusError, limit)
	if err != nil {
		return nil, fmt.Errorf("importer: list error rows: %w", err)
	}
	defer rows.Close()
	var out []ErrorRow
	for rows.Next() {
		var (
			e   ErrorRow
			raw [NumColumns]string
		)
		if err := rows.Scan(&e.Line, &raw, &e.Errors); err != nil {
			return nil, fmt.Errorf("importer: scan error row: %w", err)
		}
		e.NIS, e.Name, e.Class = raw[ColNIS], raw[ColFullName], raw[ColClass]
		out = append(out, e)
	}
	return out, rows.Err()
}

// FailedRows mengembalikan paling banyak limit baris valid yang gagal
// disimpan (T55), urut nomor baris.
func (s *Service) FailedRows(ctx context.Context, schoolID, batchID string, limit int) ([]ErrorRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT line, raw, coalesce(result_error, '') FROM import_rows
		WHERE school_id = $1 AND batch_id = $2 AND result = 'gagal'
		ORDER BY line LIMIT $3`, schoolID, batchID, limit)
	if err != nil {
		return nil, fmt.Errorf("importer: list failed rows: %w", err)
	}
	defer rows.Close()
	var out []ErrorRow
	for rows.Next() {
		var (
			e   ErrorRow
			raw [NumColumns]string
			msg string
		)
		if err := rows.Scan(&e.Line, &raw, &msg); err != nil {
			return nil, fmt.Errorf("importer: scan failed row: %w", err)
		}
		e.NIS, e.Name, e.Class = raw[ColNIS], raw[ColFullName], raw[ColClass]
		e.Errors = []RowError{{Message: msg}}
		out = append(out, e)
	}
	return out, rows.Err()
}
