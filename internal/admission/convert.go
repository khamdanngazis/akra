package admission

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/platform/settings"
	"akra/internal/school"
	"akra/internal/students"
)

// DefaultNISFormat adalah pola NIS bawaan: tahun mulai tahun ajaran + nomor
// urut tiga digit, mis. 2027001.
const DefaultNISFormat = "{yyyy}{seq:3}"

// NISContinue adalah pola khusus: lanjutkan dari NIS angka terbesar yang
// sudah dipakai siswa sekolah (T120), mis. 2526118 → 2526119.
const NISContinue = "{lanjut}"

// Aksi audit penerimaan (T96).
const (
	ActionApplicantEnroll = "admission.applicant_enroll"
	ActionNISFormat       = "admission.nis_format"
)

var (
	seqToken     = regexp.MustCompile(`\{seq:([1-6])\}`)
	nisLiteralRe = regexp.MustCompile(`^[A-Za-z0-9./-]*$`)
	// ErrNotAccepted dikembalikan jika pendaftar belum diterima atau sudah
	// menjadi siswa.
	ErrNotAccepted = errors.New("admission: applicant is not accepted or already enrolled")
)

// ValidateNISFormat memeriksa pola NIS: tepat satu {seq:N} (N 1–6),
// token opsional {yyyy} dan {yy}, sisanya huruf, angka, titik, garis miring,
// atau tanda hubung.
func ValidateNISFormat(format string) error {
	format = strings.TrimSpace(format)
	if format == NISContinue {
		return nil
	}
	if len(seqToken.FindAllString(format, -1)) != 1 {
		return fieldErr("nis_format", "Format harus berisi tepat satu {seq:N}, mis. {yyyy}{seq:3}.")
	}
	rest := seqToken.ReplaceAllString(format, "")
	rest = strings.NewReplacer("{yyyy}", "", "{yy}", "").Replace(rest)
	if !nisLiteralRe.MatchString(rest) {
		return fieldErr("nis_format", "Format hanya boleh berisi huruf, angka, titik, garis miring, tanda hubung, {yyyy}, {yy}, dan {seq:N}.")
	}
	if len(RenderNIS(format, 2027, 999999)) > 30 {
		return fieldErr("nis_format", "NIS hasil format maksimal 30 karakter.")
	}
	return nil
}

// nisPrefix mengembalikan bagian format sebelum dan sesudah nomor urut,
// dengan tahun sudah diganti, serta jumlah digit nomor urut.
func nisParts(format string, year int) (before, after string, digits int) {
	m := seqToken.FindStringSubmatchIndex(format)
	digits, _ = strconv.Atoi(format[m[2]:m[3]])
	r := strings.NewReplacer("{yyyy}", fmt.Sprintf("%04d", year), "{yy}", fmt.Sprintf("%02d", year%100))
	return r.Replace(format[:m[0]]), r.Replace(format[m[1]:]), digits
}

// RenderNIS membuat NIS dari format untuk tahun dan nomor urut tertentu.
func RenderNIS(format string, year, seq int) string {
	before, after, digits := nisParts(format, year)
	return before + fmt.Sprintf("%0*d", digits, seq) + after
}

// NISFormat mengembalikan format NIS sekolah (bawaan jika belum diatur).
func NISFormat(ctx context.Context, q database.DBTX, schoolID string) (string, error) {
	v, ok, err := settings.Get(ctx, q, schoolID, settings.AdmissionNISFormat)
	if err != nil || !ok || v == "" {
		return DefaultNISFormat, err
	}
	return v, nil
}

// SetNISFormat menyimpan format NIS sekolah.
func SetNISFormat(ctx context.Context, db database.DB, actor school.Actor, format string) error {
	format = strings.TrimSpace(format)
	if err := ValidateNISFormat(format); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := settings.Set(ctx, tx, actor.SchoolID, settings.AdmissionNISFormat, format); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionNISFormat, EntityType: "school", EntityID: actor.SchoolID, After: map[string]any{"format": format}})
	})
}

// nextNIS mengambil NIS berikutnya untuk format dan tahun, melewati NIS
// yang sudah dipakai siswa (mis. diisi manual sebelumnya).
func nextNIS(ctx context.Context, tx pgx.Tx, schoolID, format string, year int) (string, error) {
	if format == NISContinue {
		// Kunci per sekolah agar dua penerimaan bersamaan tidak mendapat
		// NIS yang sama.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('nis-lanjut:' || $1))`, schoolID); err != nil {
			return "", fmt.Errorf("admission: lock nis: %w", err)
		}
		return continueNIS(ctx, tx, schoolID)
	}
	before, after, _ := nisParts(format, year)
	// "|" tidak diizinkan di format NIS, jadi aman sebagai pemisah.
	prefix := before + "|" + after
	for i := 0; i < 1000; i++ {
		var seq int
		if err := tx.QueryRow(ctx, `
			INSERT INTO nis_counters (school_id, prefix, last_seq) VALUES ($1, $2, 1)
			ON CONFLICT (school_id, prefix) DO UPDATE SET last_seq = nis_counters.last_seq + 1
			RETURNING last_seq`, schoolID, prefix).Scan(&seq); err != nil {
			return "", fmt.Errorf("admission: next nis: %w", err)
		}
		nis := RenderNIS(format, year, seq)
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM students WHERE school_id = $1 AND nis = $2)`, schoolID, nis).Scan(&used); err != nil {
			return "", err
		}
		if !used {
			return nis, nil
		}
	}
	return "", errors.New("admission: no free NIS after 1000 attempts")
}

// lastNumericNIS mengembalikan NIS angka terbesar di sekolah ("" jika
// belum ada). Panjang dibandingkan dulu agar 1000 > 999 tanpa konversi.
func lastNumericNIS(ctx context.Context, q database.DBTX, schoolID string) (string, error) {
	var nis string
	err := q.QueryRow(ctx, `SELECT nis FROM students WHERE school_id = $1 AND nis ~ '^[0-9]{1,29}$'
		ORDER BY length(nis) DESC, nis DESC LIMIT 1`, schoolID).Scan(&nis)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return nis, err
}

// errNoNumericNIS dikembalikan pola "lanjutkan" jika belum ada NIS angka.
var errNoNumericNIS = fieldErr("nis", "Belum ada NIS berupa angka untuk dilanjutkan. Isi NIS manual atau ubah pola di Pengaturan pendaftaran.")

func continueNIS(ctx context.Context, q database.DBTX, schoolID string) (string, error) {
	last, err := lastNumericNIS(ctx, q, schoolID)
	if err != nil {
		return "", err
	}
	if last == "" {
		return "", errNoNumericNIS
	}
	return incrDigits(last), nil
}

// incrDigits menambah satu pada string angka dengan panjang tetap
// (999 → 1000, 0099 → 0100).
func incrDigits(s string) string {
	b := []byte(s)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < '9' {
			b[i]++
			return string(b)
		}
		b[i] = '0'
	}
	return "1" + string(b)
}

// PeekNIS mengembalikan perkiraan NIS otomatis berikutnya tanpa memakai
// nomor urut, untuk ditampilkan di layar. "" jika belum bisa ditentukan.
func PeekNIS(ctx context.Context, q database.DBTX, schoolID, format string, year int) (string, error) {
	if format == NISContinue {
		nis, err := continueNIS(ctx, q, schoolID)
		if errors.Is(err, errNoNumericNIS) {
			return "", nil
		}
		return nis, err
	}
	if ValidateNISFormat(format) != nil {
		return "", nil
	}
	before, after, _ := nisParts(format, year)
	var last int
	err := q.QueryRow(ctx, `SELECT last_seq FROM nis_counters WHERE school_id = $1 AND prefix = $2`, schoolID, before+"|"+after).Scan(&last)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	for seq := last + 1; seq < last+1000; seq++ {
		nis := RenderNIS(format, year, seq)
		var used bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM students WHERE school_id = $1 AND nis = $2)`, schoolID, nis).Scan(&used); err != nil {
			return "", err
		}
		if !used {
			return nis, nil
		}
	}
	return "", nil
}

// EnrollOptions adalah pilihan saat menjadikan pendaftar siswa.
type EnrollOptions struct {
	NIS     string // kosong = otomatis dari format sekolah
	ClassID string // kosong = belum ditempatkan di kelas
}

// EnrollResult adalah hasil satu pendaftar.
type EnrollResult struct {
	ApplicantID string
	FullName    string
	StudentID   string
	NIS         string
	Err         error
}

// Enroller menjadikan pendaftar yang diterima sebagai siswa (T96).
type Enroller struct {
	db   database.DB
	keys *pii.Keyring
	apps *ApplicantService
}

// NewEnroller mengembalikan Enroller.
func NewEnroller(db database.DB, keys *pii.Keyring, apps *ApplicantService) *Enroller {
	return &Enroller{db: db, keys: keys, apps: apps}
}

// EnrollMany memproses banyak pendaftar; kegagalan satu pendaftar tidak
// membatalkan yang lain (savepoint per pendaftar).
func (e *Enroller) EnrollMany(ctx context.Context, actor school.Actor, opts map[string]EnrollOptions) []EnrollResult {
	var out []EnrollResult
	for id, o := range opts {
		r := EnrollResult{ApplicantID: id}
		r.StudentID, r.NIS, r.FullName, r.Err = e.Enroll(ctx, actor, id, o)
		out = append(out, r)
	}
	return out
}

// Enroll menjadikan satu pendaftar berstatus diterima sebagai siswa dalam
// satu transaksi: data siswa, data rinci (terenkripsi), orang tua (dicocokkan
// lewat NIK atau nama + HP agar kakak-adik berbagi wali), dan opsional kelas.
// Pendaftar yang sudah menjadi siswa ditolak (ErrNotAccepted), sehingga aman
// dijalankan ulang.
func (e *Enroller) Enroll(ctx context.Context, actor school.Actor, applicantID string, o EnrollOptions) (studentID, nis, name string, err error) {
	err = pgx.BeginFunc(ctx, e.db, func(tx pgx.Tx) error {
		txApps := *e.apps
		txApps.db = tx
		a, err := txApps.load(ctx, tx, actor.SchoolID, applicantID, true)
		if err != nil {
			return err
		}
		name = a.FullName
		var existing *string
		if err := tx.QueryRow(ctx, `SELECT student_id::text FROM applicants WHERE id = $1`, a.ID).Scan(&existing); err != nil {
			return err
		}
		if a.Status != ApplicantAccepted || existing != nil {
			return ErrNotAccepted
		}
		var yearID string
		var yearStart time.Time
		var yearName string
		if err := tx.QueryRow(ctx, `SELECT y.id, y.start_date, y.name FROM admission_periods p JOIN academic_years y ON y.id = p.academic_year_id
			WHERE p.id = $1`, a.PeriodID).Scan(&yearID, &yearStart, &yearName); err != nil {
			return err
		}

		nis = strings.TrimSpace(o.NIS)
		if nis == "" {
			format, err := NISFormat(ctx, tx, actor.SchoolID)
			if err != nil {
				return err
			}
			if nis, err = nextNIS(ctx, tx, actor.SchoolID, format, yearStart.Year()); err != nil {
				return err
			}
		}
		birth := a.BirthDate
		st, err := students.Create(ctx, tx, actor.SchoolID, students.StudentInput{NIS: nis, FullName: a.FullName, BirthDate: &birth, Gender: a.Gender})
		if errors.Is(err, students.ErrDuplicateNIS) {
			return fieldErr("nis", "NIS "+nis+" sudah dipakai siswa lain.")
		}
		if err != nil {
			return err
		}
		studentID = st.ID
		if err := audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: students.ActionStudentCreate, EntityType: "student", EntityID: st.ID,
			After: map[string]any{"nis": nis, "source": "pendaftaran", "applicant_id": a.ID}}); err != nil {
			return err
		}

		values := map[string]string{}
		for _, f := range students.ProfileFields {
			values[f.Key] = a.Values[f.Key]
		}
		// Pindahan (T109): jenis pendaftaran "Pindahan" dan tanggal masuk
		// dari rencana tanggal masuk (paling awal awal tahun ajaran).
		values["registration_type"] = registrationFor(a.Kind)
		entry := yearStart
		if d, ok := plannedEntry(a); ok && d.After(yearStart) {
			entry = d
		}
		values["entry_date"] = entry.Format(dateLayout)
		if _, err := students.NewProfileService(tx, e.keys).Save(ctx, actor, st.ID, values); err != nil {
			return fmt.Errorf("admission: save profile: %w", err)
		}

		if err := e.linkParents(ctx, tx, actor, st.ID, a); err != nil {
			return err
		}

		if o.ClassID != "" {
			res, err := students.Place(ctx, tx, actor.SchoolID, st.ID, o.ClassID, yearID, entry)
			if err != nil {
				return fieldErr("class_id", "Kelas tidak bisa dipilih untuk tahun ajaran "+yearName+".")
			}
			_ = res
		}

		if _, err := tx.Exec(ctx, `UPDATE applicants SET student_id = $2, updated_at = now() WHERE id = $1`, a.ID, st.ID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionApplicantEnroll, EntityType: "applicant", EntityID: a.ID,
			After: map[string]any{"student_id": st.ID, "nis": nis, "class_id": o.ClassID}})
	})
	if err != nil {
		return "", "", name, mapApplicantError(err)
	}
	return studentID, nis, name, nil
}

// linkParents membuat atau mencocokkan orang tua lalu menautkannya ke siswa.
// Penanggung jawab utama: ibu, lalu ayah, lalu wali (yang pertama ada).
func (e *Enroller) linkParents(ctx context.Context, tx pgx.Tx, actor school.Actor, studentID string, a Applicant) error {
	details := students.NewGuardianDetailService(tx, e.keys)
	primaryDone := false
	for _, role := range []string{students.RelationMother, students.RelationFather, students.RelationGuardian} {
		g := a.Guardians[role]
		if g == nil || g["full_name"] == "" {
			continue
		}
		var guardianID string
		if g["nik"] != "" {
			if found, err := details.FindByNIK(ctx, actor.SchoolID, g["nik"]); err == nil {
				guardianID = found.ID
			} else if !errors.Is(err, students.ErrGuardianNotFound) {
				return err
			}
		}
		if guardianID == "" && g["phone"] != "" {
			if found, err := students.FindMatchingGuardian(ctx, tx, actor.SchoolID, studentID, g["full_name"], g["phone"]); err == nil {
				guardianID = found.ID
			} else if !errors.Is(err, students.ErrGuardianNotFound) {
				return err
			}
		}
		if guardianID == "" {
			created, err := students.CreateGuardian(ctx, tx, actor.SchoolID, students.GuardianInput{FullName: g["full_name"], Phone: g["phone"]})
			if err != nil {
				return err
			}
			guardianID = created.ID
		}
		if err := students.LinkToStudent(ctx, tx, actor.SchoolID, studentID, guardianID, role, !primaryDone); err != nil {
			return err
		}
		primaryDone = true
		var needs []string
		if g["special_needs"] != "" {
			needs = strings.Split(g["special_needs"], ",")
		}
		if _, err := details.Save(ctx, actor, guardianID, students.GuardianDetailsForm{
			NIK: g["nik"], BirthYear: g["birth_year"], Education: g["education"], Occupation: g["occupation"],
			Income: g["income"], SpecialNeeds: needs,
		}); err != nil {
			return fmt.Errorf("admission: save guardian details: %w", err)
		}
	}
	return nil
}
