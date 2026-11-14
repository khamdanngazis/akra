package students

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/audit"
	"akra/internal/dapodik/ref"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/school"
)

// Aksi audit data rinci wali (T87). Audit hanya memuat nama field yang
// berubah, bukan nilainya.
const (
	ActionGuardianDetailsUpdate = "guardian.details_update"
	ActionGuardianBiological    = "guardian.biological"
)

// guardianNIKAAD mengikat ciphertext NIK wali ke kolomnya (T85b).
const guardianNIKAAD = "guardians.nik"

var (
	// ErrDuplicateBiological dikembalikan jika siswa sudah punya ayah atau
	// ibu kandung lain.
	ErrDuplicateBiological  = errors.New("students: student already has a biological parent with this relationship")
	errDuplicateGuardianNIK = errors.New("students: guardian NIK already exists in this school")
)

// GuardianDetails adalah data rinci orang tua/wali sesuai F-PD.
type GuardianDetails struct {
	GuardianID   string
	NIK          pii.Value // tercetak ter-mask; Reveal untuk nilai utuh
	BirthYear    int       // 0 jika tidak diisi
	Education    string    // kode ref.Pendidikan
	Occupation   string    // kode ref.Pekerjaan
	Income       string    // kode ref.Penghasilan
	SpecialNeeds []string  // kode ref.KebutuhanKhusus
}

// Deceased melaporkan apakah pekerjaan diisi "Meninggal Dunia".
func (d GuardianDetails) Deceased() bool { return d.Occupation == ref.KodeMeninggal }

// GuardianDetailsForm adalah isian form data rinci wali apa adanya.
type GuardianDetailsForm struct {
	NIK          string
	BirthYear    string
	Education    string
	Occupation   string
	Income       string
	SpecialNeeds []string
}

// GuardianDetailService menyimpan data rinci wali dengan NIK terenkripsi.
type GuardianDetailService struct {
	db    database.DB
	keys  *pii.Keyring
	today func() time.Time
}

// NewGuardianDetailService mengembalikan GuardianDetailService.
func NewGuardianDetailService(db database.DB, keys *pii.Keyring) *GuardianDetailService {
	return &GuardianDetailService{db: db, keys: keys, today: clock.Today}
}

// Get mengembalikan data rinci wali milik schoolID.
func (s *GuardianDetailService) Get(ctx context.Context, schoolID, guardianID string) (GuardianDetails, error) {
	return s.load(ctx, s.db, schoolID, guardianID, false)
}

// FindByNIK mencari wali di sekolah lewat blind index NIK (dipakai untuk
// mencocokkan wali kakak-adik, T96/T101). ErrGuardianNotFound jika tidak ada.
func (s *GuardianDetailService) FindByNIK(ctx context.Context, schoolID, nik string) (Guardian, error) {
	nik = strings.ReplaceAll(strings.TrimSpace(nik), " ", "")
	if nik == "" {
		return Guardian{}, ErrGuardianNotFound
	}
	rows, err := s.db.Query(ctx, `SELECT `+guardianColumns+` FROM guardians g WHERE g.school_id = $1 AND g.nik_idx = $2`,
		schoolID, s.keys.BlindIndex("nik", nik))
	if err != nil {
		return Guardian{}, mapGuardianError("find guardian by nik", err)
	}
	g, err := pgx.CollectExactlyOneRow(rows, scanGuardian)
	if err != nil {
		return Guardian{}, mapGuardianError("find guardian by nik", err)
	}
	return g, nil
}

// Save memvalidasi lalu menyimpan data rinci wali.
func (s *GuardianDetailService) Save(ctx context.Context, actor school.Actor, guardianID string, f GuardianDetailsForm) (GuardianDetails, error) {
	in, err := s.validate(f)
	if err != nil {
		return GuardianDetails{}, err
	}
	var saved GuardianDetails
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := s.load(ctx, tx, actor.SchoolID, guardianID, true)
		if err != nil {
			return err
		}
		changed := diffGuardianDetails(before, in)
		if len(changed) == 0 {
			saved = before
			return nil
		}
		var ct, idx []byte
		if nik := in.NIK.Reveal(); nik != "" {
			if ct, err = s.keys.Encrypt(nik, guardianNIKAAD); err != nil {
				return err
			}
			idx = s.keys.BlindIndex("nik", nik)
		}
		var birthYear any
		if in.BirthYear > 0 {
			birthYear = in.BirthYear
		}
		_, err = tx.Exec(ctx, `
			UPDATE guardians SET nik_enc = $3, nik_idx = $4, birth_year = $5, education = $6, occupation = $7,
			       income = $8, special_needs = $9, updated_at = now()
			WHERE school_id = $1 AND id = $2`,
			actor.SchoolID, guardianID, ct, idx, birthYear, nullIfEmpty(in.Education), nullIfEmpty(in.Occupation),
			nullIfEmpty(in.Income), nonNil(in.SpecialNeeds))
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "guardians_school_nik_key" {
				return errDuplicateGuardianNIK
			}
			return mapGuardianError("save guardian details", err)
		}
		if err := audit.Record(ctx, tx, guardianAudit(actor, ActionGuardianDetailsUpdate, guardianID, nil,
			map[string]any{"changed_fields": changed})); err != nil {
			return err
		}
		saved, err = s.load(ctx, tx, actor.SchoolID, guardianID, false)
		return err
	})
	if errors.Is(err, errDuplicateGuardianNIK) {
		return GuardianDetails{}, fieldErr("nik", "NIK sudah dipakai data wali lain di sekolah ini. Pakai data wali yang sudah ada.")
	}
	if err != nil {
		return GuardianDetails{}, err
	}
	return saved, nil
}

// SetBiological menandai apakah ayah/ibu adalah orang tua kandung siswa.
// Siswa hanya boleh punya satu ayah kandung dan satu ibu kandung.
func (s *GuardianDetailService) SetBiological(ctx context.Context, actor school.Actor, studentID, guardianID string, biological bool) error {
	if !database.IsUUID(studentID) || !database.IsUUID(guardianID) {
		return ErrNotLinked
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE student_guardians SET is_biological = $4
			WHERE school_id = $1 AND student_id = $2 AND guardian_id = $3`,
			actor.SchoolID, studentID, guardianID, biological)
		if err != nil {
			return mapGuardianError("set biological", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotLinked
		}
		return audit.Record(ctx, tx, guardianAudit(actor, ActionGuardianBiological, guardianID, nil,
			map[string]any{"student_id": studentID, "is_biological": biological}))
	})
}

func (s *GuardianDetailService) load(ctx context.Context, q database.DBTX, schoolID, guardianID string, forUpdate bool) (GuardianDetails, error) {
	if !database.IsUUID(guardianID) {
		return GuardianDetails{}, ErrGuardianNotFound
	}
	sql := `SELECT nik_enc, coalesce(birth_year, 0), coalesce(education, ''), coalesce(occupation, ''),
		       coalesce(income, ''), special_needs
		FROM guardians WHERE school_id = $1 AND id = $2`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	var nikEnc []byte
	var birthYear int16
	d := GuardianDetails{GuardianID: guardianID}
	err := q.QueryRow(ctx, sql, schoolID, guardianID).Scan(&nikEnc, &birthYear, &d.Education, &d.Occupation, &d.Income, &d.SpecialNeeds)
	if err != nil {
		return GuardianDetails{}, mapGuardianError("load guardian details", err)
	}
	d.BirthYear = int(birthYear)
	if nikEnc != nil {
		nik, err := s.keys.Decrypt(nikEnc, guardianNIKAAD)
		if err != nil {
			return GuardianDetails{}, fmt.Errorf("students: decrypt guardian nik: %w", err)
		}
		d.NIK = pii.New(nik)
	}
	if len(d.SpecialNeeds) == 0 {
		d.SpecialNeeds = nil
	}
	return d, nil
}

func (s *GuardianDetailService) validate(f GuardianDetailsForm) (GuardianDetails, error) {
	fields := map[string]string{}
	var d GuardianDetails

	if nik := strings.ReplaceAll(strings.TrimSpace(f.NIK), " ", ""); nik != "" {
		if len(nik) != 16 || !digitsRe.MatchString(nik) {
			fields["nik"] = "NIK harus 16 digit angka."
		}
		d.NIK = pii.New(nik)
	}
	if y := strings.TrimSpace(f.BirthYear); y != "" {
		n, err := strconv.Atoi(y)
		if max := s.today().Year() - 10; err != nil || n < 1930 || n > max {
			fields["birth_year"] = fmt.Sprintf("Tahun lahir harus antara 1930 dan %d.", max)
		}
		d.BirthYear = n
	}
	for _, r := range []struct {
		key  string
		list ref.List
		raw  string
		dst  *string
	}{
		{"education", ref.Pendidikan, f.Education, &d.Education},
		{"occupation", ref.Pekerjaan, f.Occupation, &d.Occupation},
		{"income", ref.Penghasilan, f.Income, &d.Income},
	} {
		if v := strings.TrimSpace(r.raw); v != "" {
			if !r.list.Valid(v) {
				fields[r.key] = "Pilih dari daftar yang tersedia."
			}
			*r.dst = v
		}
	}
	for _, c := range f.SpecialNeeds {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !ref.KebutuhanKhusus.Valid(c) {
			fields["special_needs"] = "Pilih kebutuhan khusus yang tersedia."
			break
		}
		if !slices.Contains(d.SpecialNeeds, c) {
			d.SpecialNeeds = append(d.SpecialNeeds, c)
		}
	}
	slices.Sort(d.SpecialNeeds)
	if len(d.SpecialNeeds) > 1 && slices.Contains(d.SpecialNeeds, "01") {
		fields["special_needs"] = "\"Tidak\" tidak bisa dipilih bersama kebutuhan khusus lain."
	}
	if len(fields) > 0 {
		return GuardianDetails{}, &ValidationError{Fields: fields}
	}
	return d, nil
}

func diffGuardianDetails(a, b GuardianDetails) []string {
	var changed []string
	if a.NIK.Reveal() != b.NIK.Reveal() {
		changed = append(changed, "nik")
	}
	if a.BirthYear != b.BirthYear {
		changed = append(changed, "birth_year")
	}
	if a.Education != b.Education {
		changed = append(changed, "education")
	}
	if a.Occupation != b.Occupation {
		changed = append(changed, "occupation")
	}
	if a.Income != b.Income {
		changed = append(changed, "income")
	}
	if !slices.Equal(a.SpecialNeeds, b.SpecialNeeds) {
		changed = append(changed, "special_needs")
	}
	return changed
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
