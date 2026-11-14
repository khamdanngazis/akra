package admission

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/dapodik/ref"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/students"
	"akra/internal/wilayah"
)

// Status pendaftar (perpindahannya diatur di T93).
const (
	ApplicantDraft     = "draf"
	ApplicantSubmitted = "dikirim"
	ApplicantFix       = "perlu_perbaikan"
	ApplicantVerified  = "diverifikasi"
	ApplicantAccepted  = "diterima"
	ApplicantReserve   = "cadangan"
	ApplicantRejected  = "ditolak"
	ApplicantWithdrawn = "mundur"
)

// Langkah formulir pendaftaran, berurutan.
const (
	StepChild    = "calon"
	StepAddress  = "alamat"
	StepParents  = "orangtua"
	StepSchool   = "sekolah"
	StepDocument = "dokumen"
	StepReview   = "kirim"
)

// Steps adalah langkah formulir yang berisi isian, urut tampil.
var Steps = []struct{ Key, Title string }{
	{StepChild, "Data calon siswa"},
	{StepAddress, "Alamat dan keluarga"},
	{StepParents, "Orang tua/wali"},
	{StepSchool, "Sekolah asal dan kontak"},
	{StepDocument, "Dokumen"},
}

// stepSections memetakan langkah ke bagian students.ProfileFields.
var stepSections = map[string][]string{
	StepChild:   {students.SectionIdentity},
	StepAddress: {students.SectionAddress, students.SectionFamily, students.SectionAssistance},
	StepSchool:  {students.SectionRegistration, students.SectionContact},
}

// Kode jenis pendaftaran "Siswa Baru" (ref.JenisPendaftaran).
const registrationNew = "01"

// Field data rinci yang diatur sekolah, bukan diisi pendaftar.
var schoolManagedFields = []string{"registration_type", "entry_date"}

// guardianFields adalah isian per orang tua/wali di formulir.
var guardianFields = []string{"full_name", "phone", "nik", "birth_year", "education", "occupation", "income", "special_needs"}

const (
	maxNameLen      = 100
	resumeAlphabet  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // tanpa 0/O dan 1/I
	resumeCodeLen   = 6
	maxResumeFails  = 5
	resumeLockout   = 15 * time.Minute
	editableMessage = "Pendaftaran ini sudah dikirim dan tidak bisa diubah lagi."
)

var (
	// ErrClosed dikembalikan jika periode tidak sedang menerima pendaftaran.
	ErrClosed = errors.New("admission: period is not open")
	// ErrNotEditable dikembalikan jika pendaftar tidak dalam status yang bisa
	// diubah (draf atau perlu perbaikan).
	ErrNotEditable = errors.New("admission: applicant cannot be edited")
	// ErrResumeLocked dikembalikan jika terlalu banyak kode lanjut yang salah.
	ErrResumeLocked = errors.New("admission: resume locked")
	// ErrDuplicateNIK dikembalikan jika calon siswa dengan NIK yang sama sudah
	// terdaftar di periode ini.
	ErrDuplicateNIK = errors.New("admission: NIK already registered in this period")
)

// Applicant adalah satu pendaftar. Values berisi data rinci calon siswa
// (key students.ProfileFields), termasuk nomor identitas yang sudah
// didekripsi; Guardians berisi data orang tua per peran (ayah, ibu, wali).
type Applicant struct {
	ID                 string
	SchoolID           string
	PeriodID           string
	TrackID            string
	RegistrationNumber string
	Status             string
	FullName           string
	Gender             string
	BirthDate          time.Time
	Values             map[string]string
	Guardians          map[string]map[string]string
	// Kind adalah jenis pendaftar (KindNew/KindTransfer), sama dengan jenis
	// periodenya; Transfer berisi isian khusus pindahan (T109).
	Kind         string
	Transfer     map[string]string
	ContactPhone string
	EnteredBy    string
	ConsentAt    time.Time
	SubmittedAt  *time.Time
	CreatedAt    time.Time
}

// Editable melaporkan apakah pendaftar masih bisa mengubah isian.
func (a Applicant) Editable() bool {
	return a.Status == ApplicantDraft || a.Status == ApplicantFix
}

// DraftForm adalah isian langkah awal formulir.
type DraftForm struct {
	// Kind adalah jenis formulir yang dipakai (KindNew/KindTransfer);
	// periode yang dipilih harus berjenis sama. Kosong berarti jenis
	// mengikuti periode (input TU).
	Kind      string
	PeriodID  string
	TrackID   string
	FullName  string
	Gender    string
	BirthDate string
	Phone     string
	Consent   bool
}

// ApplicantService menyimpan pendaftar dengan nomor identitas terenkripsi.
type ApplicantService struct {
	db      database.DB
	keys    *pii.Keyring
	periods *Service
	now     func() time.Time
	// feePaid melaporkan apakah biaya formulir pendaftar sudah lunas (T94).
	// nil berarti belum ada tagihan pendaftaran, jadi tidak memblokir.
	feePaid func(ctx context.Context, tx pgx.Tx, a Applicant) (bool, error)
}

// NewApplicantService mengembalikan ApplicantService.
func NewApplicantService(db database.DB, keys *pii.Keyring, periods *Service) *ApplicantService {
	return &ApplicantService{db: db, keys: keys, periods: periods, now: time.Now, feePaid: feePaidTx}
}

// sensitive adalah field data rinci yang disimpan terenkripsi, dengan kolom
// dan aad-nya di tabel applicants.
var sensitive = []struct{ key, column string }{
	{"nik", "nik"},
	{"kk_number", "kk"},
	{"birth_cert_number", "birth_cert"},
	{"kip_number", "kip"},
	{"kks_number", "kks"},
}

func aadFor(column string) string { return "applicants." + column }

const guardianNIKsAAD = "applicants.guardian_niks"

// CreateDraft membuat draf pendaftar dan mengembalikan kode lanjut (hanya
// sekali; yang disimpan hanya hash-nya). enteredBy diisi ID petugas TU jika
// diinput di sekolah (T92), kosong jika diisi sendiri secara online.
func (s *ApplicantService) CreateDraft(ctx context.Context, schoolID string, f DraftForm, enteredBy string) (Applicant, string, error) {
	fields := map[string]string{}
	name := strings.Join(strings.Fields(f.FullName), " ")
	switch {
	case name == "":
		fields["full_name"] = "Nama lengkap calon siswa wajib diisi."
	case utf8.RuneCountInString(name) > maxNameLen:
		fields["full_name"] = fmt.Sprintf("Nama maksimal %d karakter.", maxNameLen)
	}
	gender := strings.TrimSpace(f.Gender)
	if gender != "L" && gender != "P" {
		fields["gender"] = "Pilih jenis kelamin."
	}
	birth, err := time.Parse(dateLayout, strings.TrimSpace(f.BirthDate))
	today := clock.Today()
	if err != nil || birth.Year() < today.Year()-25 || !birth.Before(today) {
		fields["birth_date"] = "Tanggal lahir tidak valid."
	}
	phone, err := students.NormalizePhone(f.Phone)
	if err != nil {
		fields["phone"] = "Nomor HP tidak valid. Contoh: 0812-3456-7890."
	}
	if !f.Consent {
		fields["consent"] = "Persetujuan pengolahan data pribadi wajib dicentang."
	}
	if !database.IsUUID(f.TrackID) {
		fields["track_id"] = "Pilih jalur pendaftaran."
	}
	if len(fields) > 0 {
		return Applicant{}, "", &ValidationError{Fields: fields}
	}

	code, err := newResumeCode()
	if err != nil {
		return Applicant{}, "", err
	}
	var id string
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		p, err := getPeriod(ctx, tx, schoolID, f.PeriodID, false)
		if errors.Is(err, ErrNotFound) {
			return fieldErr("track_id", "Pilih jalur pendaftaran.")
		}
		if err != nil {
			return err
		}
		// Pendaftaran online hanya saat periode terbuka; TU boleh menginput
		// pendaftar susulan (T92).
		if enteredBy == "" && !p.IsOpen(s.periods.Today()) {
			return ErrClosed
		}
		if !slices.ContainsFunc(p.Tracks, func(t Track) bool { return t.ID == f.TrackID }) || (f.Kind != "" && f.Kind != p.Kind) {
			return fieldErr("track_id", "Pilih jalur pendaftaran.")
		}
		profile, _ := json.Marshal(map[string]string{"registration_type": registrationFor(p.Kind)})
		err = tx.QueryRow(ctx, `
			INSERT INTO applicants (school_id, period_id, track_id, kind, full_name, gender, birth_date, profile,
			       contact_phone, consent_at, entered_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
			schoolID, p.ID, f.TrackID, p.Kind, name, gender, birth, profile, phone, s.now(), nullIfEmpty(enteredBy)).Scan(&id)
		if err != nil {
			return err
		}
		// Hash diikat ke ID agar kode yang sama di draf lain tidak cocok.
		_, err = tx.Exec(ctx, `UPDATE applicants SET resume_code_hash = $2 WHERE id = $1`, id, hashCode(id, code))
		return err
	})
	if err != nil {
		return Applicant{}, "", mapApplicantError(err)
	}
	a, err := s.Load(ctx, schoolID, id)
	return a, code, err
}

// Load mengembalikan pendaftar milik schoolID dengan data sensitif sudah
// didekripsi.
func (s *ApplicantService) Load(ctx context.Context, schoolID, id string) (Applicant, error) {
	return s.load(ctx, s.db, schoolID, id, false)
}

func (s *ApplicantService) load(ctx context.Context, q database.DBTX, schoolID, id string, forUpdate bool) (Applicant, error) {
	if !database.IsUUID(id) {
		return Applicant{}, ErrNotFound
	}
	sql := `SELECT id, school_id, period_id, track_id, coalesce(registration_number, ''), status, full_name,
		       coalesce(gender, ''), birth_date, profile, guardians, nik_enc, kk_enc, birth_cert_enc, kip_enc, kks_enc,
		       guardian_niks_enc, contact_phone, coalesce(entered_by::text, ''), consent_at, submitted_at, created_at,
		       kind, transfer
		FROM applicants WHERE school_id = $1 AND id = $2`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	var a Applicant
	var profile, guardians, transfer []byte
	enc := make([][]byte, len(sensitive))
	var gNIKs []byte
	err := q.QueryRow(ctx, sql, schoolID, id).Scan(&a.ID, &a.SchoolID, &a.PeriodID, &a.TrackID, &a.RegistrationNumber,
		&a.Status, &a.FullName, &a.Gender, &a.BirthDate, &profile, &guardians, &enc[0], &enc[1], &enc[2], &enc[3], &enc[4],
		&gNIKs, &a.ContactPhone, &a.EnteredBy, &a.ConsentAt, &a.SubmittedAt, &a.CreatedAt, &a.Kind, &transfer)
	if err != nil {
		return Applicant{}, mapApplicantError(err)
	}
	a.Values = map[string]string{}
	a.Guardians = map[string]map[string]string{}
	if err := json.Unmarshal(profile, &a.Values); err != nil {
		return Applicant{}, fmt.Errorf("admission: decode profile: %w", err)
	}
	if err := json.Unmarshal(guardians, &a.Guardians); err != nil {
		return Applicant{}, fmt.Errorf("admission: decode guardians: %w", err)
	}
	a.Transfer = map[string]string{}
	if err := json.Unmarshal(transfer, &a.Transfer); err != nil {
		return Applicant{}, fmt.Errorf("admission: decode transfer: %w", err)
	}
	for i, f := range sensitive {
		if enc[i] == nil {
			continue
		}
		v, err := s.keys.Decrypt(enc[i], aadFor(f.column))
		if err != nil {
			return Applicant{}, fmt.Errorf("admission: decrypt %s: %w", f.key, err)
		}
		a.Values[f.key] = v
	}
	if gNIKs != nil {
		raw, err := s.keys.Decrypt(gNIKs, guardianNIKsAAD)
		if err != nil {
			return Applicant{}, fmt.Errorf("admission: decrypt guardian niks: %w", err)
		}
		var niks map[string]string
		if err := json.Unmarshal([]byte(raw), &niks); err != nil {
			return Applicant{}, fmt.Errorf("admission: decode guardian niks: %w", err)
		}
		for role, nik := range niks {
			if a.Guardians[role] == nil {
				a.Guardians[role] = map[string]string{}
			}
			a.Guardians[role]["nik"] = nik
		}
	}
	return a, nil
}

// SaveStep memvalidasi dan menyimpan isian satu langkah formulir. values
// berisi field langkah itu; untuk StepParents, key berbentuk
// "<peran>.<field>" (mis. "ibu.full_name").
func (s *ApplicantService) SaveStep(ctx context.Context, schoolID, id, step string, values map[string]string) (Applicant, error) {
	var saved Applicant
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		a, err := s.load(ctx, tx, schoolID, id, true)
		if err != nil {
			return err
		}
		if !a.Editable() {
			return ErrNotEditable
		}
		switch step {
		case StepChild:
			if err := applyChildBasics(&a, values); err != nil {
				return err
			}
			fallthrough
		case StepAddress, StepSchool:
			merged := map[string]string{}
			for k, v := range a.Values {
				merged[k] = v
			}
			for _, sec := range stepSections[step] {
				for _, f := range students.ProfileFields {
					if f.Section == sec && !slices.Contains(schoolManagedFields, f.Key) && !slices.Contains(publicHiddenFields, f.Key) {
						merged[f.Key] = values[f.Key]
					}
				}
			}
			clean, err := students.ValidateProfile(merged, clock.Today())
			var transfer map[string]string
			if step == StepSchool && a.Kind == KindTransfer {
				var terr error
				transfer, terr = validateTransfer(values, merged["previous_school"], clock.Today())
				err = joinValidation(onlyStepErrors(err, step), terr)
			}
			if err != nil {
				return onlyStepErrors(err, step)
			}
			if step == StepAddress {
				// Nama desa/kecamatan dari data wilayah (T110).
				if err := wilayah.Apply(ctx, tx, clean); errors.Is(err, wilayah.ErrNotFound) {
					return fieldErr("wilayah_kode", "Desa/kelurahan tidak ditemukan di daftar wilayah. Pilih ulang atau isi manual.")
				} else if err != nil {
					return err
				}
			}
			a.Values = clean
			if transfer != nil {
				a.Transfer = transfer
			}
		case StepParents:
			g, err := validateGuardians(values)
			if err != nil {
				return err
			}
			a.Guardians = g
		default:
			return fmt.Errorf("admission: unknown step %q", step)
		}
		if err := s.write(ctx, tx, a); err != nil {
			return err
		}
		saved, err = s.load(ctx, tx, schoolID, id, false)
		return err
	})
	if err != nil {
		return Applicant{}, mapApplicantError(err)
	}
	return saved, nil
}

// applyChildBasics memvalidasi nama, jenis kelamin, dan tanggal lahir.
func applyChildBasics(a *Applicant, v map[string]string) error {
	fields := map[string]string{}
	name := strings.Join(strings.Fields(v["full_name"]), " ")
	switch {
	case name == "":
		fields["full_name"] = "Nama lengkap calon siswa wajib diisi."
	case utf8.RuneCountInString(name) > maxNameLen:
		fields["full_name"] = fmt.Sprintf("Nama maksimal %d karakter.", maxNameLen)
	}
	gender := strings.TrimSpace(v["gender"])
	if gender != "L" && gender != "P" {
		fields["gender"] = "Pilih jenis kelamin."
	}
	birth, err := time.Parse(dateLayout, strings.TrimSpace(v["birth_date"]))
	today := clock.Today()
	if err != nil || birth.Year() < today.Year()-25 || !birth.Before(today) {
		fields["birth_date"] = "Tanggal lahir tidak valid."
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	a.FullName, a.Gender, a.BirthDate = name, gender, birth
	return nil
}

// onlyStepErrors meneruskan error validasi data rinci apa adanya (nama
// field sama dengan input di langkah itu).
func onlyStepErrors(err error, _ string) error {
	var ve *students.ValidationError
	if errors.As(err, &ve) {
		return &ValidationError{Fields: ve.Fields}
	}
	return err
}

// validateGuardians memvalidasi data ayah, ibu, dan wali. Peran yang semua
// isiannya kosong dilewati. Nama ibu kandung wajib (Dapodik).
func validateGuardians(v map[string]string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	fields := map[string]string{}
	for _, role := range students.ParentRoles {
		get := func(k string) string { return strings.TrimSpace(v[role+"."+k]) }
		empty := true
		for _, k := range guardianFields {
			empty = empty && get(k) == ""
		}
		if empty {
			if role == students.RelationMother {
				fields[role+".full_name"] = "Nama ibu kandung wajib diisi."
			}
			continue
		}
		if get("occupation") == ref.KodeMeninggal {
			// Orang tua sudah meninggal: HP, NIK, penghasilan, dan kebutuhan
			// khusus tidak relevan dan disembunyikan di formulir (T109).
			inner := get
			get = func(k string) string {
				if slices.Contains(guardianDeceasedHidden, k) {
					return ""
				}
				return inner(k)
			}
		}
		g := map[string]string{}
		name := strings.Join(strings.Fields(get("full_name")), " ")
		switch {
		case name == "":
			fields[role+".full_name"] = "Nama wajib diisi."
		case utf8.RuneCountInString(name) > maxNameLen:
			fields[role+".full_name"] = fmt.Sprintf("Nama maksimal %d karakter.", maxNameLen)
		}
		g["full_name"] = name
		if p := get("phone"); p != "" {
			norm, err := students.NormalizePhone(p)
			if err != nil {
				fields[role+".phone"] = "Nomor HP tidak valid."
			}
			g["phone"] = norm
		}
		var needs []string
		if sn := get("special_needs"); sn != "" {
			needs = strings.Split(sn, ",")
		}
		d, err := students.ValidateGuardianDetails(students.GuardianDetailsForm{
			NIK: get("nik"), BirthYear: get("birth_year"), Education: get("education"),
			Occupation: get("occupation"), Income: get("income"), SpecialNeeds: needs,
		}, clock.Today())
		var ve *students.ValidationError
		if errors.As(err, &ve) {
			for k, m := range ve.Fields {
				fields[role+"."+k] = m
			}
		} else if err != nil {
			return nil, err
		}
		g["nik"] = d.NIK.Reveal()
		if d.BirthYear > 0 {
			g["birth_year"] = fmt.Sprint(d.BirthYear)
		}
		g["education"], g["occupation"], g["income"] = d.Education, d.Occupation, d.Income
		g["special_needs"] = strings.Join(d.SpecialNeeds, ",")
		out[role] = g
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	return out, nil
}

// write menyimpan semua isian pendaftar (data sensitif dienkripsi, sisanya
// jsonb).
func (s *ApplicantService) write(ctx context.Context, tx pgx.Tx, a Applicant) error {
	plain := map[string]string{}
	for k, v := range a.Values {
		if v != "" {
			plain[k] = v
		}
	}
	enc := make([][]byte, len(sensitive))
	var nikIdx []byte
	for i, f := range sensitive {
		v := plain[f.key]
		delete(plain, f.key)
		if v == "" {
			continue
		}
		ct, err := s.keys.Encrypt(v, aadFor(f.column))
		if err != nil {
			return err
		}
		enc[i] = ct
		if f.key == "nik" {
			nikIdx = s.keys.BlindIndex("nik", v)
		}
	}
	guardians := map[string]map[string]string{}
	niks := map[string]string{}
	for role, g := range a.Guardians {
		cp := map[string]string{}
		for k, v := range g {
			if k == "nik" {
				if v != "" {
					niks[role] = v
				}
				continue
			}
			if v != "" {
				cp[k] = v
			}
		}
		guardians[role] = cp
	}
	var gNIKs []byte
	if len(niks) > 0 {
		raw, _ := json.Marshal(niks)
		ct, err := s.keys.Encrypt(string(raw), guardianNIKsAAD)
		if err != nil {
			return err
		}
		gNIKs = ct
	}
	profile, _ := json.Marshal(plain)
	gjson, _ := json.Marshal(guardians)
	transfer := map[string]string{}
	for k, v := range a.Transfer {
		if v != "" {
			transfer[k] = v
		}
	}
	tjson, _ := json.Marshal(transfer)
	_, err := tx.Exec(ctx, `
		UPDATE applicants SET full_name = $3, gender = $4, birth_date = $5, profile = $6, guardians = $7,
		       nik_enc = $8, nik_idx = $9, kk_enc = $10, birth_cert_enc = $11, kip_enc = $12, kks_enc = $13,
		       guardian_niks_enc = $14, transfer = $15, updated_at = now()
		WHERE school_id = $1 AND id = $2`,
		a.SchoolID, a.ID, a.FullName, nullIfEmpty(a.Gender), a.BirthDate, profile, gjson,
		enc[0], nikIdx, enc[1], enc[2], enc[3], enc[4], gNIKs, tjson)
	return err
}

// Missing mengembalikan isian wajib yang belum diisi sebelum pendaftaran
// bisa dikirim: field wajib Dapodik yang diisi pendaftar, nama ibu
// kandung, dan untuk pindahan isian sekolah asal (T109). Kunci field
// mengikuti nama input formulir.
func Missing(a Applicant) []students.RequiredField {
	out := missingProfile(a)
	if a.Kind == KindTransfer {
		out = append(out, missingTransfer(a)...)
	}
	return out
}

func missingProfile(a Applicant) []students.RequiredField {
	var out []students.RequiredField
	for _, f := range students.DapodikRequired {
		switch f.Key {
		case "full_name", "gender", "birth_date", "registration_type", "entry_date":
			continue // diisi di langkah awal atau oleh sekolah
		case "mother_name":
			if a.Guardians[students.RelationMother]["full_name"] == "" {
				out = append(out, f)
			}
		default:
			if a.Values[f.Key] == "" {
				out = append(out, f)
			}
		}
	}
	return out
}

// Submit mengirim pendaftaran: memberi nomor pendaftaran dari penghitung
// tahun ajaran periode, lalu status menjadi dikirim.
func (s *ApplicantService) Submit(ctx context.Context, schoolID, id string) (Applicant, error) {
	return s.SubmitBy(ctx, schoolID, id, "")
}

// SubmitBy sama dengan Submit, dengan actorID petugas TU yang mengirimkan
// atas nama pendaftar (T92); kosong berarti pendaftar sendiri.
func (s *ApplicantService) SubmitBy(ctx context.Context, schoolID, id, actorID string) (Applicant, error) {
	var out Applicant
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		a, err := s.load(ctx, tx, schoolID, id, true)
		if err != nil {
			return err
		}
		if a.Status != ApplicantDraft && a.Status != ApplicantFix {
			return ErrNotEditable
		}
		if m := Missing(a); len(m) > 0 {
			return &ValidationError{Fields: map[string]string{"submit": "Masih ada isian wajib yang kosong."}}
		}
		var missingDocs int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM admission_periods p, unnest(p.required_docs) AS req(doc_type)
			WHERE p.id = $1 AND NOT EXISTS (SELECT 1 FROM applicant_documents d WHERE d.applicant_id = $2 AND d.doc_type = req.doc_type)`,
			a.PeriodID, a.ID).Scan(&missingDocs); err != nil {
			return err
		}
		if missingDocs > 0 {
			return &ValidationError{Fields: map[string]string{"submit": "Masih ada dokumen wajib yang belum diunggah."}}
		}
		if a.Status == ApplicantFix {
			// Kiriman ulang setelah perbaikan memakai nomor yang sama.
			_, err := tx.Exec(ctx, `UPDATE applicants SET status = 'dikirim', resume_code_hash = NULL, updated_at = now() WHERE id = $1`, id)
			if err != nil {
				return err
			}
		} else {
			p, err := getPeriod(ctx, tx, schoolID, a.PeriodID, false)
			if err != nil {
				return err
			}
			var seq int
			err = tx.QueryRow(ctx, `
				INSERT INTO admission_counters (school_id, academic_year_id, kind, last_seq) VALUES ($1, $2, $3, 1)
				ON CONFLICT (school_id, academic_year_id, kind) DO UPDATE SET last_seq = admission_counters.last_seq + 1
				RETURNING last_seq`, schoolID, p.AcademicYearID, a.Kind).Scan(&seq)
			if err != nil {
				return err
			}
			year := p.AcademicYearName
			if i := strings.IndexAny(year, "/-"); i > 0 {
				year = year[:i]
			}
			number := fmt.Sprintf("%s-%s-%04d", numberPrefix(a.Kind), year, seq)
			// Biaya formulir di-snapshot saat pertama kali dikirim (T94).
			_, err = tx.Exec(ctx, `UPDATE applicants SET status = 'dikirim', registration_number = $2, submitted_at = $3,
				resume_code_hash = NULL, fee_amount = $4, updated_at = now() WHERE id = $1`, id, number, s.now(), p.FeeAmount)
			if err != nil {
				return err
			}
		}
		if err := recordEvent(ctx, tx, schoolID, id, a.Status, ApplicantSubmitted, "", actorID); err != nil {
			return err
		}
		out, err = s.load(ctx, tx, schoolID, id, false)
		return err
	})
	if err != nil {
		return Applicant{}, mapApplicantError(err)
	}
	return out, nil
}

// Resume mencari draf berdasarkan nomor HP dan kode lanjut. Kode yang salah
// dihitung per draf; setelah maxResumeFails kali, draf dikunci sementara.
// Pesan error sengaja sama untuk nomor HP yang tidak dikenal dan kode salah.
func (s *ApplicantService) Resume(ctx context.Context, schoolID, phone, code string) (Applicant, error) {
	norm, err := students.NormalizePhone(phone)
	code = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), " ", ""))
	if err != nil || len(code) != resumeCodeLen {
		return Applicant{}, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `SELECT id, resume_code_hash, resume_locked_until FROM applicants
		WHERE school_id = $1 AND contact_phone = $2 AND status IN ('draf', 'perlu_perbaikan') AND resume_code_hash IS NOT NULL`, schoolID, norm)
	if err != nil {
		return Applicant{}, mapApplicantError(err)
	}
	type cand struct {
		id     string
		hash   []byte
		locked *time.Time
	}
	cands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (cand, error) {
		var c cand
		return c, r.Scan(&c.id, &c.hash, &c.locked)
	})
	if err != nil {
		return Applicant{}, mapApplicantError(err)
	}
	now := s.now()
	for _, c := range cands {
		if c.locked != nil && c.locked.After(now) {
			continue
		}
		if subtle.ConstantTimeCompare(c.hash, hashCode(c.id, code)) == 1 {
			_, _ = s.db.Exec(ctx, `UPDATE applicants SET resume_failures = 0, resume_locked_until = NULL WHERE id = $1`, c.id)
			return s.Load(ctx, schoolID, c.id)
		}
	}
	locked := false
	for _, c := range cands {
		if c.locked != nil && c.locked.After(now) {
			locked = true
			continue
		}
		_, err := s.db.Exec(ctx, `UPDATE applicants SET resume_failures = resume_failures + 1,
			resume_locked_until = CASE WHEN resume_failures + 1 >= $2 THEN $3::timestamptz ELSE NULL END
			WHERE id = $1`, c.id, maxResumeFails, now.Add(resumeLockout))
		if err != nil {
			return Applicant{}, mapApplicantError(err)
		}
	}
	if locked {
		return Applicant{}, ErrResumeLocked
	}
	return Applicant{}, ErrNotFound
}

// newResumeCode membuat kode lanjut acak. 256 habis dibagi 32, jadi modulo
// tidak membuat sebaran huruf berat sebelah.
func newResumeCode() (string, error) {
	b := make([]byte, resumeCodeLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("admission: generate resume code: %w", err)
	}
	for i := range b {
		b[i] = resumeAlphabet[int(b[i])%len(resumeAlphabet)]
	}
	return string(b), nil
}

func hashCode(id, code string) []byte {
	h := sha256.Sum256([]byte("akra-resume\x00" + id + "\x00" + code))
	return h[:]
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func mapApplicantError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" && pgErr.ConstraintName == "applicants_period_nik_key" {
			return ErrDuplicateNIK
		}
		return fmt.Errorf("admission: applicant: database error %s (constraint %q)", pgErr.Code, pgErr.ConstraintName)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// PIIColumns mengembalikan kolom terenkripsi milik paket ini, untuk rotasi
// kunci (cmd/rotate-pii-key).
func PIIColumns() []pii.Column {
	cols := make([]pii.Column, 0, len(sensitive)+1)
	for _, f := range sensitive {
		cols = append(cols, pii.Column{Table: "applicants", Key: "id", Column: f.column + "_enc", AADName: aadFor(f.column)})
	}
	return append(cols, pii.Column{Table: "applicants", Key: "id", Column: "guardian_niks_enc", AADName: guardianNIKsAAD})
}
