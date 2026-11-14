package students

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/school"
)

// ActionProfileUpdate dicatat setiap data rinci siswa berubah. Audit hanya
// memuat nama field yang berubah, bukan nilainya, karena data rinci berisi
// nomor identitas dan alamat.
const ActionProfileUpdate = "student.profile_update"

// Profile adalah data rinci satu siswa (T86). Nilai disimpan dalam bentuk
// teks kanonik per key ProfileFields; field sensitif hanya bisa dibaca
// lewat Secret.
type Profile struct {
	StudentID string
	// Exists bernilai false jika data rinci belum pernah disimpan.
	Exists    bool
	UpdatedAt time.Time
	values    map[string]string
}

// Get mengembalikan nilai field non-sensitif (string kosong jika tidak
// diisi). Field sensitif selalu mengembalikan versi ter-mask; pakai Secret
// untuk nilai utuh.
func (p Profile) Get(key string) string {
	if f, ok := profileFieldByKey[key]; ok && f.Sensitive {
		return pii.Mask(p.values[key])
	}
	return p.values[key]
}

// Secret mengembalikan nilai field sensitif sebagai pii.Value: tercetak
// ter-mask kecuali Reveal dipanggil secara eksplisit.
func (p Profile) Secret(key string) pii.Value { return pii.New(p.values[key]) }

// Has melaporkan apakah field diisi.
func (p Profile) Has(key string) bool { return p.values[key] != "" }

// List mengembalikan nilai kindRefMulti sebagai daftar kode.
func (p Profile) List(key string) []string {
	if v := p.values[key]; v != "" {
		return strings.Split(v, ",")
	}
	return nil
}

// ProfileService menyimpan data rinci siswa dengan field sensitif
// terenkripsi.
type ProfileService struct {
	db    database.DB
	keys  *pii.Keyring
	today func() time.Time
}

// NewProfileService mengembalikan ProfileService.
func NewProfileService(db database.DB, keys *pii.Keyring) *ProfileService {
	return &ProfileService{db: db, keys: keys, today: clock.Today}
}

// Get mengembalikan data rinci siswa. ErrNotFound jika siswa tidak ada atau
// milik sekolah lain; siswa tanpa data rinci mengembalikan Profile kosong.
func (s *ProfileService) Get(ctx context.Context, schoolID, studentID string) (Profile, error) {
	if _, err := GetByID(ctx, s.db, schoolID, studentID); err != nil {
		return Profile{}, err
	}
	return s.load(ctx, s.db, schoolID, studentID, false)
}

// Save mengubah field yang ada di values; field yang tidak ada di values
// tidak berubah. Validasi dijalankan atas gabungan data lama dan baru, agar
// aturan antar-field (mis. NIK ≠ No KK) tetap terjaga saat form disimpan
// per bagian.
func (s *ProfileService) Save(ctx context.Context, actor school.Actor, studentID string, values map[string]string) (Profile, error) {
	for k := range values {
		if _, ok := profileFieldByKey[k]; !ok {
			return Profile{}, fmt.Errorf("students: unknown profile field %q", k)
		}
	}
	var saved Profile
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := GetByID(ctx, tx, actor.SchoolID, studentID); err != nil {
			return err
		}
		before, err := s.load(ctx, tx, actor.SchoolID, studentID, true)
		if err != nil {
			return err
		}
		merged := make(map[string]string, len(ProfileFields))
		for k, v := range before.values {
			merged[k] = v
		}
		for k, v := range values {
			merged[k] = v
		}
		clean, err := s.validate(merged)
		if err != nil {
			return err
		}
		var changed []string
		for _, f := range ProfileFields {
			if before.values[f.Key] != clean[f.Key] {
				changed = append(changed, f.Key)
			}
		}
		if before.Exists && len(changed) == 0 {
			saved = before
			return nil
		}
		if err := s.upsert(ctx, tx, actor, studentID, clean); err != nil {
			return err
		}
		if len(changed) > 0 {
			if err := audit.Record(ctx, tx, audit.Entry{
				SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
				Action: ActionProfileUpdate, EntityType: "student", EntityID: studentID,
				After: map[string]any{"changed_fields": changed},
			}); err != nil {
				return err
			}
		}
		saved, err = s.load(ctx, tx, actor.SchoolID, studentID, false)
		return err
	})
	if err != nil {
		return Profile{}, profileFieldError(err)
	}
	return saved, nil
}

var (
	digitsRe   = regexp.MustCompile(`^[0-9]+$`)
	kitasRe    = regexp.MustCompile(`^[A-Z0-9]{6,20}$`)
	phoneStrip = strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "")
)

// validate menormalisasi dan memeriksa semua field, lalu mengembalikan
// nilai kanonik. Pesan error per field dalam bahasa Indonesia.
func (s *ProfileService) validate(in map[string]string) (map[string]string, error) {
	return ValidateProfile(in, s.today())
}

// ValidateProfile memeriksa dan menormalisasi data rinci siswa tanpa
// menyimpannya. Field yang tidak ada di in dianggap kosong. Dipakai juga
// formulir pendaftaran (T90) agar aturannya sama persis dengan data siswa.
func ValidateProfile(in map[string]string, today time.Time) (map[string]string, error) {
	out := make(map[string]string, len(ProfileFields))
	fields := map[string]string{}
	for _, f := range ProfileFields {
		v, msg := normalizeField(f, in[f.Key], today)
		if msg != "" {
			fields[f.Key] = msg
			continue
		}
		out[f.Key] = v
	}

	// Aturan antar-field.
	if out["citizenship"] == "" {
		out["citizenship"] = "WNI"
	}
	if nik := out["nik"]; nik != "" && fields["nik"] == "" {
		if out["citizenship"] == "WNI" && (len(nik) != 16 || !digitsRe.MatchString(nik)) {
			fields["nik"] = "NIK harus 16 digit angka."
		}
		if out["citizenship"] == "WNA" && !kitasRe.MatchString(nik) {
			fields["nik"] = "No. KITAS harus 6–20 huruf atau angka."
		}
	}
	if out["nik"] != "" && out["nik"] == out["kk_number"] && fields["kk_number"] == "" {
		fields["kk_number"] = "No KK tidak boleh sama dengan NIK. Periksa apakah keduanya tertukar."
	}
	if out["citizenship"] == "WNA" {
		if out["country"] == "" && fields["country"] == "" {
			fields["country"] = "Nama negara wajib diisi untuk WNA."
		}
	} else {
		out["country"] = ""
	}
	if codes := out["special_needs"]; strings.Contains(codes, ",") && slices.Contains(strings.Split(codes, ","), "01") {
		fields["special_needs"] = "\"Tidak\" tidak bisa dipilih bersama kebutuhan khusus lain."
	}
	if out["has_kip"] != "true" {
		out["kip_number"], out["kip_name"] = "", ""
	}
	if out["pip_eligible"] != "true" {
		out["pip_reason"] = ""
	}

	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	return out, nil
}

// normalizeField mengembalikan nilai kanonik, atau pesan error.
func normalizeField(f ProfileField, raw string, today time.Time) (string, string) {
	v := strings.Join(strings.Fields(raw), " ")
	if f.Kind == kindBool {
		switch strings.ToLower(v) {
		case "true", "on", "1", "ya":
			return "true", ""
		default:
			return "false", ""
		}
	}
	if v == "" {
		return "", ""
	}
	switch f.Kind {
	case kindText:
		if utf8.RuneCountInString(v) > int(f.Max) {
			return "", fmt.Sprintf("%s maksimal %d karakter.", f.Label, int(f.Max))
		}
		return v, ""
	case kindNISN:
		v = strings.ReplaceAll(v, " ", "")
		if len(v) != 10 || !digitsRe.MatchString(v) {
			return "", "NISN harus 10 digit angka. Kosongkan jika belum punya."
		}
		return v, ""
	case kindIdentity:
		// Format dicek di validate setelah kewarganegaraan diketahui.
		return strings.ToUpper(strings.ReplaceAll(v, " ", "")), ""
	case kindKK:
		v = strings.ReplaceAll(v, " ", "")
		if len(v) != 16 || !digitsRe.MatchString(v) {
			return "", "No KK harus 16 digit angka."
		}
		return v, ""
	case kindRef:
		if !f.Ref.Valid(v) {
			return "", fmt.Sprintf("Pilih %s yang tersedia.", strings.ToLower(f.Label))
		}
		return v, ""
	case kindRefMulti:
		var codes []string
		for _, c := range strings.Split(v, ",") {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if !f.Ref.Valid(c) {
				return "", fmt.Sprintf("Pilih %s yang tersedia.", strings.ToLower(f.Label))
			}
			if !slices.Contains(codes, c) {
				codes = append(codes, c)
			}
		}
		slices.Sort(codes)
		return strings.Join(codes, ","), ""
	case kindDigits:
		v = strings.ReplaceAll(v, " ", "")
		n := len(v)
		if !digitsRe.MatchString(v) || n < int(f.Min) || n > int(f.Max) {
			if f.Min == f.Max {
				return "", fmt.Sprintf("%s harus %d digit angka.", f.Label, int(f.Max))
			}
			return "", fmt.Sprintf("%s harus angka, maksimal %d digit.", f.Label, int(f.Max))
		}
		return v, ""
	case kindInt:
		n, err := strconv.Atoi(v)
		if err != nil || float64(n) < f.Min || float64(n) > f.Max {
			return "", fmt.Sprintf("%s harus bilangan bulat %d–%d.", f.Label, int(f.Min), int(f.Max))
		}
		return strconv.Itoa(n), ""
	case kindDecimal:
		x, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64)
		if err != nil || x < f.Min || x > f.Max {
			return "", fmt.Sprintf("%s harus angka %s sampai %s.", f.Label, fmtDecimal(f.Min), fmtDecimal(f.Max))
		}
		return fmtDecimal(x), ""
	case kindDate:
		d, err := time.Parse(dateLayout, v)
		if err != nil || d.Year() < 1990 || d.After(today.AddDate(1, 0, 0)) {
			return "", fmt.Sprintf("%s tidak valid.", f.Label)
		}
		return d.Format(dateLayout), ""
	case kindPhone:
		v = phoneStrip.Replace(v)
		if strings.HasPrefix(v, "+62") {
			v = "0" + v[3:]
		}
		if len(v) < 6 || len(v) > 15 || !digitsRe.MatchString(v) {
			return "", fmt.Sprintf("%s harus 6–15 digit angka.", f.Label)
		}
		return v, ""
	case kindEmail:
		v = strings.ToLower(v)
		if a, err := mail.ParseAddress(v); err != nil || a.Address != v || len(v) > int(f.Max) {
			return "", "Email tidak valid."
		}
		return v, ""
	}
	return "", fmt.Sprintf("%s tidak valid.", f.Label)
}

// fmtDecimal menulis desimal tanpa nol di belakang (6 angka di belakang
// koma, sesuai kolom numeric(9,6)).
func fmtDecimal(x float64) string {
	s := strconv.FormatFloat(x, 'f', 6, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		return "0"
	}
	return s
}

func profileFieldError(err error) error {
	switch {
	case errors.Is(err, errDuplicateNISN):
		return fieldErr("nisn", "NISN sudah dipakai siswa lain di sekolah ini.")
	case errors.Is(err, errDuplicateNIK):
		return fieldErr("nik", "NIK sudah dipakai siswa lain di sekolah ini.")
	}
	return err
}

// ProfileForTest membuat Profile dari nilai kanonik, untuk uji paket lain.
func ProfileForTest(values map[string]string) Profile {
	return Profile{Exists: true, values: values}
}
