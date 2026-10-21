package grades

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit komponen penilaian.
const (
	ActionComponentCreate = "grade_component.create"
	ActionComponentUpdate = "grade_component.update"
)

const (
	// MaxNameLen adalah panjang maksimal nama komponen.
	MaxNameLen = 40
	// MaxRows adalah jumlah baris maksimal dalam satu simpan.
	MaxRows = 20
	// RequiredTotal adalah total bobot komponen aktif yang wajib.
	RequiredTotal = 100
)

var (
	// ErrNoActiveYear: sekolah belum punya tahun ajaran aktif.
	ErrNoActiveYear = errors.New("grades: no active academic year")
	// ErrSubjectNotFound: mapel tidak ada atau milik sekolah lain.
	ErrSubjectNotFound = errors.New("grades: subject not found")
	// ErrComponentNotFound: baris form merujuk komponen yang bukan milik mapel
	// dan tahun ajaran ini.
	ErrComponentNotFound = errors.New("grades: component not found")
)

// ValidationError berisi pesan per baris (indeks baris form) dan pesan
// total bobot.
type ValidationError struct {
	Rows  map[int]map[string]string
	Total string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("grades: invalid components (%d rows, total %q)", len(e.Rows), e.Total)
}

func (e *ValidationError) add(row int, field, msg string) {
	if e.Rows == nil {
		e.Rows = map[int]map[string]string{}
	}
	if e.Rows[row] == nil {
		e.Rows[row] = map[string]string{}
	}
	e.Rows[row][field] = msg
}

// RowInput adalah satu baris form apa adanya. ID kosong untuk komponen baru.
type RowInput struct {
	ID, Name, Weight string
	Active           bool
}

// ComponentService mengatur komponen penilaian.
type ComponentService struct {
	db database.DB
}

// NewComponentService mengembalikan ComponentService.
func NewComponentService(db database.DB) *ComponentService { return &ComponentService{db: db} }

// SubjectComponents adalah isi halaman komponen satu mapel.
type SubjectComponents struct {
	Year       school.AcademicYear
	Subject    academic.Subject
	Components []Component
	Total      int // total bobot komponen aktif
}

// activeYear mengembalikan tahun ajaran aktif atau ErrNoActiveYear.
func activeYear(ctx context.Context, q database.DBTX, schoolID string) (school.AcademicYear, error) {
	y, err := school.GetActiveAcademicYear(ctx, q, schoolID)
	if errors.Is(err, school.ErrNotFound) {
		return school.AcademicYear{}, ErrNoActiveYear
	}
	return y, err
}

func getSubject(ctx context.Context, q database.DBTX, schoolID, subjectID string) (academic.Subject, error) {
	s, err := academic.GetSubjectByID(ctx, q, schoolID, subjectID)
	if errors.Is(err, academic.ErrSubjectNotFound) {
		return academic.Subject{}, ErrSubjectNotFound
	}
	return s, err
}

// Subjects mengembalikan mapel yang boleh diatur id di tahun ajaran aktif:
// semua mapel aktif untuk admin/TU, mapel yang ditugaskan untuk guru (T65).
func (s *ComponentService) Subjects(ctx context.Context, id auth.Identity) ([]academic.Subject, error) {
	if id.HasRole(auth.RoleAdminTU) {
		return academic.ListActiveSubjects(ctx, s.db, id.SchoolID)
	}
	if !id.HasRole(auth.RoleGuru) {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT s.id, s.name, lower(s.name) FROM teaching_assignments ta
		JOIN subjects s ON s.school_id = ta.school_id AND s.id = ta.subject_id
		JOIN academic_years y ON y.school_id = ta.school_id AND y.id = ta.academic_year_id AND y.is_active
		WHERE ta.school_id = $1 AND ta.teacher_id = $2
		ORDER BY lower(s.name)`, id.SchoolID, id.UserID)
	if err != nil {
		return nil, fmt.Errorf("grades: teacher subjects: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (academic.Subject, error) {
		var sub academic.Subject
		var sortKey string
		err := row.Scan(&sub.ID, &sub.Name, &sortKey)
		sub.SchoolID, sub.IsActive = id.SchoolID, true
		return sub, err
	})
}

// CanManage melaporkan apakah id boleh mengatur komponen mapel subjectID:
// admin/TU untuk semua mapel sekolahnya, guru hanya untuk mapel yang
// ditugaskan kepadanya di kelas mana pun pada tahun ajaran aktif.
func CanManage(ctx context.Context, q database.DBTX, id auth.Identity, subjectID string) (bool, error) {
	if id.HasRole(auth.RoleAdminTU) {
		return true, nil
	}
	if !id.HasRole(auth.RoleGuru) || !database.IsUUID(subjectID) {
		return false, nil
	}
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM teaching_assignments ta
		JOIN academic_years y ON y.school_id = ta.school_id AND y.id = ta.academic_year_id AND y.is_active
		WHERE ta.school_id = $1 AND ta.subject_id = $2 AND ta.teacher_id = $3)`,
		id.SchoolID, subjectID, id.UserID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("grades: check subject access: %w", err)
	}
	return ok, nil
}

// Page menyusun komponen mapel subjectID di tahun ajaran aktif.
func (s *ComponentService) Page(ctx context.Context, schoolID, subjectID string) (SubjectComponents, error) {
	year, err := activeYear(ctx, s.db, schoolID)
	if err != nil {
		return SubjectComponents{}, err
	}
	sub, err := getSubject(ctx, s.db, schoolID, subjectID)
	if err != nil {
		return SubjectComponents{}, err
	}
	p := SubjectComponents{Year: year, Subject: sub}
	if p.Components, err = ListComponents(ctx, s.db, schoolID, year.ID, sub.ID); err != nil {
		return SubjectComponents{}, err
	}
	for _, c := range p.Components {
		if c.IsActive {
			p.Total += c.Weight
		}
	}
	return p, nil
}

// SaveResult merangkum hasil simpan.
type SaveResult struct {
	Created, Updated int
	Total            int // total bobot aktif setelah simpan
}

// Save menyimpan semua baris komponen mapel subjectID (tahun ajaran aktif)
// dalam satu transaksi, semua atau tidak sama sekali:
//
//   - baris kosong (tanpa ID, nama, dan bobot) diabaikan;
//   - nama wajib, maksimal 40 karakter, unik per mapel tanpa membedakan
//     huruf besar/kecil; bobot bilangan bulat 1–100;
//   - komponen tidak dihapus, hanya dinonaktifkan (nilai lama merujuknya);
//   - setelah semua baris diterapkan, total bobot komponen aktif harus 100,
//     kecuali belum ada komponen aktif sama sekali. Pemeriksaan dilakukan
//     pada isi database (bukan hanya isian form), sehingga baris yang tidak
//     ikut terkirim tetap terhitung.
//
// Audit grade_component.create untuk komponen baru dan grade_component.update
// untuk komponen yang berubah (termasuk dinonaktifkan atau diaktifkan lagi).
func (s *ComponentService) Save(ctx context.Context, actor school.Actor, subjectID string, rows []RowInput) (SaveResult, error) {
	var res SaveResult
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		year, err := activeYear(ctx, tx, actor.SchoolID)
		if err != nil {
			return err
		}
		sub, err := getSubject(ctx, tx, actor.SchoolID, subjectID)
		if err != nil {
			return err
		}
		// Kunci mapel agar dua simpan bersamaan untuk mapel yang sama berurutan
		// dan pemeriksaan total melihat hasil simpan sebelumnya.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM subjects WHERE school_id = $1 AND id = $2 FOR UPDATE`, actor.SchoolID, sub.ID); err != nil {
			return fmt.Errorf("grades: lock subject: %w", err)
		}
		existing, err := ListComponents(ctx, tx, actor.SchoolID, year.ID, sub.ID)
		if err != nil {
			return err
		}
		byID := make(map[string]Component, len(existing))
		for _, c := range existing {
			byID[c.ID] = c
		}

		ve := &ValidationError{}
		type change struct {
			row    int
			before *Component
			after  Component
		}
		var changes []change
		seen := map[string]int{} // nama (huruf kecil) -> baris
		order := 0
		for i, r := range rows {
			name := strings.Join(strings.Fields(r.Name), " ")
			weight := strings.TrimSpace(r.Weight)
			if r.ID == "" && name == "" && weight == "" {
				continue
			}
			c := Component{AcademicYearID: year.ID, SubjectID: sub.ID, IsActive: r.Active}
			var before *Component
			if r.ID != "" {
				old, ok := byID[r.ID]
				if !ok {
					return ErrComponentNotFound
				}
				before, c.ID = &old, old.ID
			}
			switch n := len([]rune(name)); {
			case n == 0:
				ve.add(i, "name", "Isi nama komponen.")
			case n > MaxNameLen:
				ve.add(i, "name", fmt.Sprintf("Maksimal %d karakter.", MaxNameLen))
			default:
				key := strings.ToLower(name)
				if j, dup := seen[key]; dup {
					ve.add(i, "name", fmt.Sprintf("Nama sama dengan baris %d.", j+1))
				}
				seen[key] = i
			}
			w, err := strconv.Atoi(weight)
			if err != nil || w < 1 || w > 100 {
				ve.add(i, "weight", "Bobot bilangan bulat 1–100.")
			}
			order++
			c.Name, c.Weight, c.SortOrder = name, w, order
			changes = append(changes, change{row: i, before: before, after: c})
		}
		if len(ve.Rows) > 0 {
			return ve
		}

		for _, ch := range changes {
			c := ch.after
			if ch.before == nil {
				if c.ID, err = insertComponent(ctx, tx, actor.SchoolID, c); err != nil {
					return rowError(err, ch.row)
				}
				res.Created++
				if err := audit.Record(ctx, tx, componentAudit(actor, ActionComponentCreate, sub, c, nil)); err != nil {
					return err
				}
				continue
			}
			b := *ch.before
			if b.Name == c.Name && b.Weight == c.Weight && b.SortOrder == c.SortOrder && b.IsActive == c.IsActive {
				continue
			}
			if err := updateComponent(ctx, tx, actor.SchoolID, c); err != nil {
				return rowError(err, ch.row)
			}
			res.Updated++
			// Urutan saja yang berubah tidak perlu diaudit.
			if b.Name != c.Name || b.Weight != c.Weight || b.IsActive != c.IsActive {
				if err := audit.Record(ctx, tx, componentAudit(actor, ActionComponentUpdate, sub, c, &b)); err != nil {
					return err
				}
			}
		}

		count, total, err := ActiveWeightTotal(ctx, tx, actor.SchoolID, year.ID, sub.ID)
		if err != nil {
			return err
		}
		if count > 0 && total != RequiredTotal {
			return &ValidationError{Total: fmt.Sprintf("Total bobot komponen aktif harus %d%%, sekarang %d%%. Tidak ada yang disimpan.", RequiredTotal, total)}
		}
		res.Total = total
		return nil
	})
	if err != nil {
		return SaveResult{}, err
	}
	return res, nil
}

// rowError memetakan nama ganda dari database (mis. komponen yang ditambah
// orang lain sejak form dibuka) ke pesan di baris row.
func rowError(err error, row int) error {
	if errors.Is(err, ErrDuplicateName) {
		ve := &ValidationError{}
		ve.add(row, "name", "Nama sudah dipakai komponen lain di mapel ini. Muat ulang halaman.")
		return ve
	}
	return err
}

func snapshot(c Component) map[string]any {
	return map[string]any{"name": c.Name, "weight_percent": c.Weight, "is_active": c.IsActive}
}

func componentAudit(actor school.Actor, action string, sub academic.Subject, after Component, before *Component) audit.Entry {
	a := snapshot(after)
	a["subject_id"], a["academic_year_id"] = sub.ID, after.AcademicYearID
	e := audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP, Action: action,
		EntityType: "grade_component", EntityID: after.ID, After: a}
	if before != nil {
		e.Before = snapshot(*before)
	}
	return e
}
