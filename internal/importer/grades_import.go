package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/grades"
	"akra/internal/platform/database"
	"akra/internal/school"
	"akra/internal/students"
)

// Impor nilai (T82). Baris (NIS, kode mapel, nama komponen, nilai) diperiksa
// terhadap siswa aktif, penugasan guru mapel (T65), dan komponen aktif
// (T69), lalu disimpan lewat grades.InputService.Save, jalur yang sama
// dengan pengisian nilai oleh guru (T70): nilai 0–100, kosong = belum
// dinilai (NULL), satu nilai per siswa per komponen (UNIQUE), dan audit
// grade.create/update per perubahan.

// Jenis kesalahan baris impor nilai.
const (
	KindGradeStudent      = "siswa_tidak_ada"
	KindSubjectEmpty      = "mapel_kosong"
	KindSubjectNotFound   = "mapel_tidak_ada"
	KindNoAssignment      = "penugasan_tidak_ada"
	KindComponentEmpty    = "komponen_kosong"
	KindComponentNotFound = "komponen_tidak_ada"
	KindScore             = "nilai_tidak_valid"
	KindGradeDuplicate    = "nilai_ganda"
)

// GradeRow adalah baris nilai yang lolos validasi.
type GradeRow struct {
	NIS           string `json:"nis"`
	EnrollmentID  string `json:"enrollment_id"`
	ClassID       string `json:"class_id"`
	ClassName     string `json:"class_name"`
	SubjectID     string `json:"subject_id"`
	SubjectName   string `json:"subject_name"`
	ComponentID   string `json:"component_id"`
	ComponentName string `json:"component_name"`
	// Score adalah isian nilai apa adanya ("85,5"); kosong = belum dinilai.
	// Dibaca ulang grades.ParseScore saat disimpan.
	Score string `json:"score"`
	// Update: siswa sudah punya baris nilai untuk komponen ini.
	Update bool `json:"update"`
}

type gradeEnrollment struct{ id, classID, className string }

type gradeSubject struct {
	id, name string
	active   bool
}

type gradeComponent struct{ id, name string }

// gradeContext adalah data pembanding validasi impor nilai.
type gradeContext struct {
	enrollments map[string]gradeEnrollment           // NIS -> enrollment aktif tahun ajaran aktif
	subjects    map[string]gradeSubject              // kode mapel (huruf kecil) -> mapel
	assigned    map[[2]string]bool                   // (kelas, mapel) punya guru pengampu
	components  map[string]map[string]gradeComponent // mapel -> nama komponen (huruf kecil) -> komponen aktif
	existing    map[[2]string]bool                   // (enrollment, komponen) sudah punya baris nilai
	seen        map[[3]string]int                    // (NIS, mapel, komponen) -> baris pertama
}

func newGradeContext(ctx context.Context, q database.DBTX, schoolID string, rows []MappedRow) (*gradeContext, error) {
	year, err := school.GetActiveAcademicYear(ctx, q, schoolID)
	if errors.Is(err, school.ErrNotFound) {
		return nil, ErrNoActiveYear
	}
	if err != nil {
		return nil, err
	}
	gc := &gradeContext{enrollments: map[string]gradeEnrollment{}, subjects: map[string]gradeSubject{}, assigned: map[[2]string]bool{},
		components: map[string]map[string]gradeComponent{}, existing: map[[2]string]bool{}, seen: map[[3]string]int{}}
	var nis []string
	for _, r := range rows {
		if n := students.NormalizeNIS(r.Values[GradeColNIS]); n != "" {
			nis = append(nis, n)
		}
	}
	scan := func(sql string, args []any, fn func(pgx.Rows) error) error {
		rs, err := q.Query(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("importer: grade context: %w", err)
		}
		defer rs.Close()
		for rs.Next() {
			if err := fn(rs); err != nil {
				return fmt.Errorf("importer: grade context: %w", err)
			}
		}
		return rs.Err()
	}
	var enrollmentIDs []string
	if err := scan(`SELECT st.nis, e.id, c.id, c.name FROM enrollments e
		JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
		JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id
		WHERE e.school_id = $1 AND e.academic_year_id = $2 AND e.end_date IS NULL AND st.nis = ANY($3)`,
		[]any{schoolID, year.ID, nis}, func(rs pgx.Rows) error {
			var n string
			var e gradeEnrollment
			if err := rs.Scan(&n, &e.id, &e.classID, &e.className); err != nil {
				return err
			}
			gc.enrollments[n] = e
			enrollmentIDs = append(enrollmentIDs, e.id)
			return nil
		}); err != nil {
		return nil, err
	}
	if err := scan(`SELECT id, name, lower(code), is_active FROM subjects WHERE school_id = $1 AND code IS NOT NULL`, []any{schoolID}, func(rs pgx.Rows) error {
		var s gradeSubject
		var code string
		if err := rs.Scan(&s.id, &s.name, &code, &s.active); err != nil {
			return err
		}
		gc.subjects[code] = s
		return nil
	}); err != nil {
		return nil, err
	}
	if err := scan(`SELECT class_id, subject_id FROM teaching_assignments WHERE school_id = $1 AND academic_year_id = $2`,
		[]any{schoolID, year.ID}, func(rs pgx.Rows) error {
			var k [2]string
			if err := rs.Scan(&k[0], &k[1]); err != nil {
				return err
			}
			gc.assigned[k] = true
			return nil
		}); err != nil {
		return nil, err
	}
	if err := scan(`SELECT id, subject_id, name FROM grade_components WHERE school_id = $1 AND academic_year_id = $2 AND is_active`,
		[]any{schoolID, year.ID}, func(rs pgx.Rows) error {
			var c gradeComponent
			var subjectID string
			if err := rs.Scan(&c.id, &subjectID, &c.name); err != nil {
				return err
			}
			if gc.components[subjectID] == nil {
				gc.components[subjectID] = map[string]gradeComponent{}
			}
			gc.components[subjectID][strings.ToLower(c.name)] = c
			return nil
		}); err != nil {
		return nil, err
	}
	if err := scan(`SELECT enrollment_id, component_id FROM grades WHERE school_id = $1 AND enrollment_id = ANY($2)`,
		[]any{schoolID, enrollmentIDs}, func(rs pgx.Rows) error {
			var k [2]string
			if err := rs.Scan(&k[0], &k[1]); err != nil {
				return err
			}
			gc.existing[k] = true
			return nil
		}); err != nil {
		return nil, err
	}
	return gc, nil
}

// validateGradeRow memeriksa satu baris nilai. Semua kesalahan baris tampil
// sekaligus; panggil sesuai urutan baris (deteksi baris ganda).
func validateGradeRow(r MappedRow, gc *gradeContext) (GradeRow, []RowError) {
	var errs []RowError
	add := func(col int, kind, msg string) {
		errs = append(errs, RowError{Column: GradeColumns[col].Header, Kind: kind, Message: msg})
	}
	v := r.Values
	d := GradeRow{NIS: students.NormalizeNIS(v[GradeColNIS]), Score: strings.TrimSpace(v[GradeColScore])}
	e, okStudent := gc.enrollments[d.NIS]
	switch {
	case d.NIS == "":
		add(GradeColNIS, KindNISEmpty, "NIS wajib diisi.")
	case !okStudent:
		add(GradeColNIS, KindGradeStudent, fmt.Sprintf("Siswa dengan NIS %s tidak terdaftar di kelas tahun ajaran aktif.", d.NIS))
	default:
		d.EnrollmentID, d.ClassID, d.ClassName = e.id, e.classID, e.className
	}
	code := strings.ToLower(strings.TrimSpace(v[GradeColSubjectCode]))
	sub, okSubject := gc.subjects[code]
	switch {
	case code == "":
		add(GradeColSubjectCode, KindSubjectEmpty, "Kode mapel wajib diisi.")
	case !okSubject || !sub.active:
		add(GradeColSubjectCode, KindSubjectNotFound, fmt.Sprintf("Mata pelajaran berkode %s tidak ada atau nonaktif.", strings.TrimSpace(v[GradeColSubjectCode])))
		okSubject = false
	default:
		d.SubjectID, d.SubjectName = sub.id, sub.name
	}
	if okStudent && okSubject && !gc.assigned[[2]string{e.classID, sub.id}] {
		add(GradeColSubjectCode, KindNoAssignment, fmt.Sprintf("%s belum punya guru pengampu di kelas %s (Penugasan Guru), jadi nilainya tidak bisa diimpor.", sub.name, e.className))
	}
	name := strings.ToLower(strings.Join(strings.Fields(v[GradeColComponent]), " "))
	switch {
	case name == "":
		add(GradeColComponent, KindComponentEmpty, "Nama komponen wajib diisi.")
	case okSubject:
		c, ok := gc.components[sub.id][name]
		if !ok {
			add(GradeColComponent, KindComponentNotFound, fmt.Sprintf("Komponen %q tidak ada atau nonaktif untuk %s.", strings.TrimSpace(v[GradeColComponent]), sub.name))
		} else {
			d.ComponentID, d.ComponentName = c.id, c.name
		}
	}
	if _, _, ok := grades.ParseScore(d.Score); !ok {
		add(GradeColScore, KindScore, "Nilai 0–100, maksimal dua angka di belakang koma, atau kosong.")
	}
	if d.NIS != "" && d.SubjectID != "" && d.ComponentID != "" {
		key := [3]string{d.NIS, d.SubjectID, d.ComponentID}
		if first, dup := gc.seen[key]; dup {
			add(GradeColNIS, KindGradeDuplicate, fmt.Sprintf("Nilai siswa ini untuk komponen yang sama sudah ada di baris %d.", first))
		} else {
			gc.seen[key] = r.Line
		}
	}
	if errs != nil {
		return GradeRow{}, errs
	}
	d.Update = gc.existing[[2]string{d.EnrollmentID, d.ComponentID}]
	return d, nil
}

// UploadGrades menyimpan baris nilai (hasil ParseMapped target nilai)
// sebagai batch lalu memvalidasinya, dalam satu transaksi.
func (s *Service) UploadGrades(ctx context.Context, actor school.Actor, fileName string, rows MappedRows, mappingID string, today time.Time) (string, error) {
	if rows.Target != TargetGrades {
		return "", fmt.Errorf("importer: rows are %q, not grades", rows.Target)
	}
	var id string
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if id, err = createBatch(ctx, tx, actor, TargetGrades, mappingID, fileName, rows.Data, rows.Skipped); err != nil {
			return err
		}
		return validateBatch(ctx, tx, actor.SchoolID, id, today)
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// validateGradeRecords memvalidasi baris batch nilai dan menyusun baris
// import_rows (kolom sama dengan validasi siswa).
func validateGradeRecords(ctx context.Context, q database.DBTX, schoolID, batchID string, raw []byte) (records [][]any, valid, invalid, updates int, err error) {
	var rows []MappedRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, 0, 0, 0, fmt.Errorf("importer: decode grade rows: %w", err)
	}
	for i := range rows {
		if len(rows[i].Values) < NumGradeColumns {
			rows[i].Values = append(rows[i].Values, make([]string, NumGradeColumns-len(rows[i].Values))...)
		}
	}
	gc, err := newGradeContext(ctx, q, schoolID, rows)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	for _, r := range rows {
		d, errs := validateGradeRow(r, gc)
		if errs != nil {
			invalid++
			records = append(records, []any{schoolID, batchID, r.Line, r.Values, RowStatusError, nil, nil, errs})
			continue
		}
		valid++
		action := ActionCreate
		if d.Update {
			action, updates = ActionUpdate, updates+1
		}
		records = append(records, []any{schoolID, batchID, r.Line, r.Values, RowStatusValid, action, d, []RowError{}})
	}
	return records, valid, invalid, updates, nil
}

// rowResult adalah hasil simpan satu baris.
type rowResult struct{ result, msg string }

// errSplitGroup: kelompok gagal karena satu barisnya; simpan ulang per baris.
var errSplitGroup = errors.New("importer: split grade group")

// saveGradeRows menyimpan baris nilai satu chunk lewat
// grades.InputService.Save (T70). Baris dikelompokkan per (kelas, komponen)
// agar daftar siswa kelas dibaca sekali per kelompok, bukan sekali per
// baris. Jika kelompok gagal karena satu baris, baris kelompok itu disimpan
// satu per satu sehingga hanya baris itu yang gagal.
func saveGradeRows(ctx context.Context, tx pgx.Tx, actor school.Actor, rows []GradeRow) ([]rowResult, error) {
	out := make([]rowResult, len(rows))
	groups := map[[2]string][]int{}
	var keys [][2]string
	for i, d := range rows {
		k := [2]string{d.ClassID, d.ComponentID}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], i)
	}
	for _, k := range keys {
		idx := groups[k]
		group := make([]GradeRow, len(idx))
		for j, i := range idx {
			group[j] = rows[i]
		}
		err := saveGradeGroup(ctx, tx, actor, group)
		if errors.Is(err, errSplitGroup) {
			for _, i := range idx {
				out[i], err = gradeResult(rows[i], saveGradeGroup(ctx, tx, actor, rows[i:i+1]))
				if err != nil {
					return nil, err
				}
			}
			continue
		}
		for _, i := range idx {
			if out[i], err = gradeResult(rows[i], err); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func gradeResult(d GradeRow, err error) (rowResult, error) {
	var rf *rowFailure
	switch {
	case errors.As(err, &rf):
		return rowResult{ResultFailed, rf.msg}, nil
	case err != nil:
		return rowResult{}, err
	case d.Update:
		return rowResult{result: ResultUpdated}, nil
	}
	return rowResult{result: ResultCreated}, nil
}

// saveGradeGroup menyimpan nilai satu kelas dan satu komponen dalam
// savepoint. Keadaan yang berubah sejak validasi (komponen dinonaktifkan,
// siswa pindah kelas) menjadi kegagalan baris.
func saveGradeGroup(ctx context.Context, tx pgx.Tx, actor school.Actor, group []GradeRow) error {
	d := group[0]
	return pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
		tg, err := grades.ResolveComponent(ctx, sp, actor.SchoolID, d.ClassID, d.ComponentID)
		switch {
		case errors.Is(err, grades.ErrClassNotFound), errors.Is(err, grades.ErrComponentNotFound), errors.Is(err, grades.ErrSubjectNotFound):
			return &rowFailure{fmt.Sprintf("Kelas %s atau komponen %s sudah tidak ada di tahun ajaran aktif. Unggah ulang file.", d.ClassName, d.ComponentName)}
		case err != nil:
			return err
		}
		scores := make(map[string]string, len(group))
		for _, g := range group {
			scores[g.EnrollmentID] = g.Score
		}
		_, err = grades.NewInputService(sp).Save(ctx, actor, tg, scores)
		var se *grades.ScoreError
		switch {
		case errors.Is(err, grades.ErrComponentInactive):
			return &rowFailure{fmt.Sprintf("Komponen %s sudah dinonaktifkan.", d.ComponentName)}
		case len(group) > 1 && (errors.Is(err, grades.ErrUnknownEnrollment) || errors.As(err, &se)):
			return errSplitGroup
		case errors.Is(err, grades.ErrUnknownEnrollment):
			return &rowFailure{fmt.Sprintf("Siswa NIS %s sudah tidak terdaftar di kelas %s.", d.NIS, d.ClassName)}
		case errors.As(err, &se):
			return &rowFailure{"Nilai tidak valid."}
		}
		return err
	})
}
