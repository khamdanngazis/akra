package students

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/dapodik/ref"
	"akra/internal/platform/pii"
	"akra/internal/school"
)

const (
	nikA = "3201010101010001"
	kkA  = "3201010101010099"
)

func TestProfileFieldsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range ProfileFields {
		if f.Key == "" || f.Label == "" || f.Column == "" || f.Section == "" || seen[f.Key] {
			t.Errorf("bad or duplicate field %+v", f)
		}
		seen[f.Key] = true
		if (f.Kind == kindRef || f.Kind == kindRefMulti) && f.Ref == nil {
			t.Errorf("%s: reference field without list", f.Key)
		}
		if f.Kind == kindText && f.Max < 1 {
			t.Errorf("%s: text field without max length", f.Key)
		}
		if f.IndexKind != "" && !f.Sensitive {
			t.Errorf("%s: blind index on non-sensitive field", f.Key)
		}
	}
	for _, k := range []string{"nik", "kk_number", "birth_cert_number", "kip_number", "kks_number"} {
		if f, ok := LookupProfileField(k); !ok || !f.Sensitive {
			t.Errorf("%s must be sensitive", k)
		}
	}
	if len(PIIColumns()) != 6 {
		t.Errorf("PIIColumns = %d, want 6", len(PIIColumns()))
	}
}

func TestProfileValidate(t *testing.T) {
	s := &ProfileService{today: func() time.Time { return time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC) }}
	ok, err := s.validate(map[string]string{
		"nisn": " 0091234567 ", "nik": "3201 0101 0101 0001", "kk_number": kkA,
		"religion": "01", "special_needs": "03, 02,03", "rt": "005", "postal_code": "40123",
		"latitude": "-6,2000", "longitude": "106.816666", "home_phone": "(022) 123-456",
		"mobile_phone": "+62 812-3456-7890", "email": " Ani@Contoh.ID ", "has_kip": "on",
		"kip_number": "KIP123", "pip_eligible": "", "pip_reason": "02", "country": "Jepang",
		"distance_km": "1.50", "entry_date": "2026-07-13", "child_order": "02",
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	want := map[string]string{
		"nisn": "0091234567", "nik": nikA, "citizenship": "WNI", "country": "",
		"special_needs": "02,03", "rt": "005", "latitude": "-6.2", "longitude": "106.816666",
		"home_phone": "022123456", "mobile_phone": "081234567890", "email": "ani@contoh.id",
		"has_kip": "true", "kip_number": "KIP123", "pip_eligible": "false", "pip_reason": "",
		"distance_km": "1.5", "child_order": "2",
	}
	for k, v := range want {
		if ok[k] != v {
			t.Errorf("%s = %q, want %q", k, ok[k], v)
		}
	}

	cases := []struct {
		name  string
		in    map[string]string
		field string
	}{
		{"nik equals kk", map[string]string{"nik": nikA, "kk_number": nikA}, "kk_number"},
		{"nik 15 digits", map[string]string{"nik": "320101010101000"}, "nik"},
		{"nik letters", map[string]string{"nik": "32010101010100AB"}, "nik"},
		{"kitas for WNA", map[string]string{"citizenship": "WNA", "country": "Jepang", "nik": "AB-1"}, "nik"},
		{"WNA without country", map[string]string{"citizenship": "WNA"}, "country"},
		{"nisn 9 digits", map[string]string{"nisn": "009123456"}, "nisn"},
		{"nisn letters", map[string]string{"nisn": "00912345AB"}, "nisn"},
		{"kk 15 digits", map[string]string{"kk_number": "320101010101009"}, "kk_number"},
		{"unknown religion", map[string]string{"religion": "08"}, "religion"},
		{"tidak with others", map[string]string{"special_needs": "01,02"}, "special_needs"},
		{"postal code", map[string]string{"postal_code": "4012"}, "postal_code"},
		{"rt letters", map[string]string{"rt": "5a"}, "rt"},
		{"latitude outside Indonesia", map[string]string{"latitude": "40.7"}, "latitude"},
		{"height", map[string]string{"height_cm": "400"}, "height_cm"},
		{"email", map[string]string{"email": "ani@"}, "email"},
		{"phone", map[string]string{"mobile_phone": "0812abc"}, "mobile_phone"},
		{"entry date", map[string]string{"entry_date": "2030-01-01"}, "entry_date"},
		{"birth place too long", map[string]string{"birth_place": strings.Repeat("a", 51)}, "birth_place"},
	}
	for _, tc := range cases {
		_, err := s.validate(tc.in)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Fields[tc.field] == "" {
			t.Errorf("%s: error = %v, want message on %s", tc.name, err, tc.field)
		}
	}

	wna, err := s.validate(map[string]string{"citizenship": "WNA", "country": "Jepang", "nik": "ab 123456"})
	if err != nil || wna["nik"] != "AB123456" {
		t.Errorf("WNA KITAS = %q, %v", wna["nik"], err)
	}
}

type profileFixture struct {
	tx       pgx.Tx
	svc      *ProfileService
	actorA   school.Actor
	keys     *pii.Keyring
	budi     string
	ani      string
	studentB string
	schoolB  string
}

func newProfileFixture(t *testing.T) *profileFixture {
	t.Helper()
	_, tx, f := txFixture(t)
	ctx := context.Background()
	keys := pii.DevKeyring()
	p := &profileFixture{tx: tx, svc: NewProfileService(tx, keys), keys: keys, budi: f.student, studentB: f.studentB, schoolB: f.schoolB}
	var userID string
	if err := tx.QueryRow(ctx, `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'tu@a.test', 'x', 'TU A') RETURNING id`, f.schoolA).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	p.actorA = school.Actor{SchoolID: f.schoolA, UserID: userID, IP: netip.MustParseAddr("192.0.2.10")}
	if err := tx.QueryRow(ctx, `INSERT INTO students (school_id, nis, full_name) VALUES ($1, '002', 'Ani') RETURNING id`, f.schoolA).Scan(&p.ani); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProfileSaveAndLoad(t *testing.T) {
	p := newProfileFixture(t)
	ctx := context.Background()

	empty, err := p.svc.Get(ctx, p.actorA.SchoolID, p.budi)
	if err != nil || empty.Exists || empty.Has("nik") {
		t.Fatalf("empty profile = %+v, %v", empty, err)
	}

	saved, err := p.svc.Save(ctx, p.actorA, p.budi, map[string]string{
		"nisn": "0091234567", "nik": nikA, "kk_number": kkA, "birth_cert_number": "3201-LT-0001",
		"religion": "01", "street": "Jl. Melati 5", "latitude": "-6.2", "special_needs": "01",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !saved.Exists || saved.Secret("nik").Reveal() != nikA || saved.Get("nik") != "3201********0001" || saved.Get("street") != "Jl. Melati 5" || saved.Get("latitude") != "-6.2" {
		t.Errorf("saved = nik %q/%q street %q lat %q", saved.Secret("nik").Reveal(), saved.Get("nik"), saved.Get("street"), saved.Get("latitude"))
	}

	// Simpan per bagian: field lain tidak berubah.
	if _, err := p.svc.Save(ctx, p.actorA, p.budi, map[string]string{"street": "Jl. Mawar 1", "rt": "3"}); err != nil {
		t.Fatal(err)
	}
	got, _ := p.svc.Get(ctx, p.actorA.SchoolID, p.budi)
	if got.Secret("nik").Reveal() != nikA || got.Get("street") != "Jl. Mawar 1" || got.Get("religion") != "01" || got.Get("rt") != "3" {
		t.Errorf("partial save lost data: nik %q street %q religion %q", got.Secret("nik").Reveal(), got.Get("street"), got.Get("religion"))
	}

	// Kolom terenkripsi tidak berisi teks NIK, KK, atau akta.
	var nikEnc, nikIdx, kkEnc, certEnc []byte
	if err := p.tx.QueryRow(ctx, `SELECT nik_enc, nik_idx, kk_enc, birth_cert_enc FROM student_profiles WHERE student_id = $1`, p.budi).Scan(&nikEnc, &nikIdx, &kkEnc, &certEnc); err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"nik_enc": nikEnc, "kk_enc": kkEnc, "birth_cert_enc": certEnc} {
		for _, plain := range []string{nikA, kkA, "3201-LT-0001"} {
			if bytes.Contains(b, []byte(plain)) {
				t.Errorf("%s contains plaintext", name)
			}
		}
	}
	if !bytes.Equal(nikIdx, p.keys.BlindIndex("nik", nikA)) {
		t.Error("nik_idx is not the blind index of the NIK")
	}
	var leaked bool
	if err := p.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM student_profiles p WHERE p.student_id = $1 AND row_to_json(p)::text LIKE '%' || $2 || '%')`, p.budi, nikA).Scan(&leaked); err != nil || leaked {
		t.Errorf("row contains plaintext NIK: %v, %v", leaked, err)
	}

	// Audit: hanya nama field yang berubah, tanpa nilai.
	var after string
	if err := p.tx.QueryRow(ctx, `SELECT after::text FROM audit_logs WHERE entity_id = $1 AND action = $2 ORDER BY created_at, id LIMIT 1`, p.budi, ActionProfileUpdate).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, `"nik"`) || strings.Contains(after, nikA) || strings.Contains(after, kkA) || strings.Contains(after, "Melati") {
		t.Errorf("audit after = %s", after)
	}
	var n int
	_ = p.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE entity_id = $1 AND action = $2`, p.budi, ActionProfileUpdate).Scan(&n)
	if _, err := p.svc.Save(ctx, p.actorA, p.budi, map[string]string{"street": "Jl. Mawar 1"}); err != nil {
		t.Fatal(err)
	}
	var n2 int
	_ = p.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE entity_id = $1 AND action = $2`, p.budi, ActionProfileUpdate).Scan(&n2)
	if n2 != n {
		t.Errorf("saving without changes added an audit row (%d -> %d)", n, n2)
	}

	// Mengosongkan field sensitif menghapus ciphertext dan indeksnya.
	if _, err := p.svc.Save(ctx, p.actorA, p.budi, map[string]string{"kk_number": ""}); err != nil {
		t.Fatal(err)
	}
	var kkIsNull bool
	_ = p.tx.QueryRow(ctx, `SELECT kk_enc IS NULL AND kk_idx IS NULL FROM student_profiles WHERE student_id = $1`, p.budi).Scan(&kkIsNull)
	if !kkIsNull {
		t.Error("clearing kk_number left ciphertext behind")
	}

	// Data rinci tidak mengubah tabel students.
	st, err := GetByID(ctx, p.tx, p.actorA.SchoolID, p.budi)
	if err != nil || st.NIS != "001" {
		t.Errorf("student after profile save = %+v, %v", st, err)
	}
}

func TestProfileUniqueAndTenant(t *testing.T) {
	p := newProfileFixture(t)
	ctx := context.Background()
	if _, err := p.svc.Save(ctx, p.actorA, p.budi, map[string]string{"nik": nikA, "nisn": "0091234567"}); err != nil {
		t.Fatal(err)
	}

	err := sp(t, p.tx, func(q pgx.Tx) error {
		_, err := NewProfileService(q, p.keys).Save(ctx, p.actorA, p.ani, map[string]string{"nik": " 3201 0101 0101 0001"})
		return err
	})
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Fields["nik"] == "" {
		t.Errorf("duplicate NIK: error = %v", err)
	}
	err = sp(t, p.tx, func(q pgx.Tx) error {
		_, err := NewProfileService(q, p.keys).Save(ctx, p.actorA, p.ani, map[string]string{"nisn": "0091234567"})
		return err
	})
	if !errors.As(err, &ve) || ve.Fields["nisn"] == "" {
		t.Errorf("duplicate NISN: error = %v", err)
	}

	// Sekolah lain boleh punya siswa dengan NIK sama (pindahan).
	actorB := p.actorA
	actorB.SchoolID, actorB.UserID = p.schoolB, ""
	if _, err := p.svc.Save(ctx, actorB, p.studentB, map[string]string{"nik": nikA}); err != nil {
		t.Errorf("same NIK in another school: %v", err)
	}

	// Siswa sekolah B tidak bisa dibaca atau diubah lewat sekolah A.
	if _, err := p.svc.Get(ctx, p.actorA.SchoolID, p.studentB); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get other school's student: %v", err)
	}
	if _, err := p.svc.Save(ctx, p.actorA, p.studentB, map[string]string{"street": "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Save other school's student: %v", err)
	}
	if _, err := p.svc.Save(ctx, p.actorA, p.budi, map[string]string{"password": "x"}); err == nil {
		t.Error("unknown field accepted")
	}
}

func TestProfileWrongKeyFails(t *testing.T) {
	p := newProfileFixture(t)
	ctx := context.Background()
	if _, err := p.svc.Save(ctx, p.actorA, p.budi, map[string]string{"nik": nikA}); err != nil {
		t.Fatal(err)
	}
	other, err := pii.NewKeyring(map[byte][]byte{1: bytes.Repeat([]byte{0x33}, pii.KeySize)}, bytes.Repeat([]byte{0x44}, pii.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewProfileService(p.tx, other).Get(ctx, p.actorA.SchoolID, p.budi); !errors.Is(err, pii.ErrDecrypt) {
		t.Errorf("Get with wrong key: error = %v, want ErrDecrypt", err)
	}
}

func TestProfileRefLabels(t *testing.T) {
	// Kode yang disimpan harus bisa diubah ke label Dapodik.
	for _, f := range ProfileFields {
		if f.Kind != kindRef {
			continue
		}
		code := f.Ref.Options[0].Code
		if _, err := f.Ref.Label(code); err != nil {
			t.Errorf("%s: %v", f.Key, err)
		}
	}
	if !ref.KebutuhanKhusus.Multi {
		t.Error("special needs must be multi")
	}
}
