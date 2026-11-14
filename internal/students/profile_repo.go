package students

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/school"
)

var (
	errDuplicateNISN = errors.New("students: NISN already exists in this school")
	errDuplicateNIK  = errors.New("students: NIK already exists in this school")
)

// PIIColumns mengembalikan kolom terenkripsi milik paket ini, untuk rotasi
// kunci (cmd/rotate-pii-key).
func PIIColumns() []pii.Column {
	var cols []pii.Column
	for _, f := range ProfileFields {
		if f.Sensitive {
			cols = append(cols, pii.Column{Table: "student_profiles", Key: "student_id", Column: f.Column + "_enc", AADName: f.aad()})
		}
	}
	return cols
}

// selectExpr mengembalikan ekspresi SELECT untuk field: ciphertext untuk
// field sensitif, selain itu teks.
func (f ProfileField) selectExpr() string {
	switch {
	case f.Sensitive:
		return f.Column + "_enc"
	case f.Kind == kindRefMulti:
		return "NULLIF(array_to_string(" + f.Column + ", ','), '')"
	default:
		return f.Column + "::text"
	}
}

// load membaca dan mendekripsi data rinci. forUpdate mengunci baris agar
// dua penyimpanan bersamaan tidak saling menimpa diam-diam.
func (s *ProfileService) load(ctx context.Context, q database.DBTX, schoolID, studentID string, forUpdate bool) (Profile, error) {
	exprs := make([]string, len(ProfileFields))
	for i, f := range ProfileFields {
		exprs[i] = f.selectExpr()
	}
	sql := `SELECT updated_at, ` + strings.Join(exprs, ", ") + ` FROM student_profiles WHERE school_id = $1 AND student_id = $2`
	if forUpdate {
		sql += ` FOR UPDATE`
	}

	var updatedAt time.Time
	texts := make([]*string, len(ProfileFields))
	blobs := make([][]byte, len(ProfileFields))
	dest := make([]any, 0, len(ProfileFields)+1)
	dest = append(dest, &updatedAt)
	for i, f := range ProfileFields {
		if f.Sensitive {
			dest = append(dest, &blobs[i])
		} else {
			dest = append(dest, &texts[i])
		}
	}

	p := Profile{StudentID: studentID, values: map[string]string{}}
	err := q.QueryRow(ctx, sql, schoolID, studentID).Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return Profile{}, fmt.Errorf("students: load profile: %w", err)
	}
	p.Exists, p.UpdatedAt = true, updatedAt
	for i, f := range ProfileFields {
		switch {
		case f.Sensitive:
			if blobs[i] == nil {
				continue
			}
			v, err := s.keys.Decrypt(blobs[i], f.aad())
			if err != nil {
				return Profile{}, fmt.Errorf("students: decrypt profile field %s: %w", f.Key, err)
			}
			p.values[f.Key] = v
		case texts[i] != nil:
			p.values[f.Key] = canonicalDBText(f, *texts[i])
		}
	}
	return p, nil
}

// canonicalDBText menyamakan teks dari database dengan bentuk kanonik hasil
// validasi (mis. numeric "-6.200000" menjadi "-6.2").
func canonicalDBText(f ProfileField, v string) string {
	if f.Kind == kindDecimal {
		if x, err := strconv.ParseFloat(v, 64); err == nil {
			return fmtDecimal(x)
		}
	}
	return v
}

// insertExpr mengembalikan ekspresi nilai untuk parameter $n.
func (f ProfileField) insertExpr(n int) string {
	p := "$" + strconv.Itoa(n)
	switch f.Kind {
	case kindInt:
		return p + "::smallint"
	case kindDecimal:
		return p + "::numeric"
	case kindDate:
		return p + "::date"
	case kindBool:
		return "COALESCE(" + p + "::boolean, false)"
	case kindRefMulti:
		return "COALESCE(string_to_array(" + p + ", ','), '{}')"
	}
	return p
}

// upsert menulis semua field (nilai sudah divalidasi dan kanonik).
func (s *ProfileService) upsert(ctx context.Context, tx pgx.Tx, actor school.Actor, studentID string, v map[string]string) error {
	cols := []string{"student_id", "school_id", "updated_by", "updated_at"}
	vals := []string{"$1", "$2", "$3", "now()"}
	args := []any{studentID, actor.SchoolID, nullIfEmpty(actor.UserID)}
	add := func(col, expr string, arg any) {
		args = append(args, arg)
		cols = append(cols, col)
		vals = append(vals, strings.ReplaceAll(expr, "$?", "$"+strconv.Itoa(len(args))))
	}
	for _, f := range ProfileFields {
		val := v[f.Key]
		if !f.Sensitive {
			add(f.Column, f.insertExpr(len(args)+1), nullIfEmpty(val))
			continue
		}
		var ct, idx []byte
		if val != "" {
			var err error
			if ct, err = s.keys.Encrypt(val, f.aad()); err != nil {
				return err
			}
			if f.IndexKind != "" {
				idx = s.keys.BlindIndex(f.IndexKind, val)
			}
		}
		add(f.Column+"_enc", "$?", ct)
		if f.IndexKind != "" {
			add(f.Column+"_idx", "$?", idx)
		}
	}
	sets := make([]string, 0, len(cols))
	for _, c := range cols[1:] {
		sets = append(sets, c+" = EXCLUDED."+c)
	}
	sql := `INSERT INTO student_profiles (` + strings.Join(cols, ", ") + `) VALUES (` + strings.Join(vals, ", ") + `)
		ON CONFLICT (student_id) DO UPDATE SET ` + strings.Join(sets, ", ")
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			switch pgErr.ConstraintName {
			case "student_profiles_school_nisn_key":
				return errDuplicateNISN
			case "student_profiles_school_nik_key":
				return errDuplicateNIK
			}
		}
		if errors.As(err, &pgErr) {
			// Jangan bungkus err asli: detail constraint bisa memuat nilai baris.
			return fmt.Errorf("students: save profile: database error %s (constraint %q)", pgErr.Code, pgErr.ConstraintName)
		}
		return fmt.Errorf("students: save profile: %w", err)
	}
	return nil
}
