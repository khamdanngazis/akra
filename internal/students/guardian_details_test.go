package students

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/dapodik/ref"
	"akra/internal/platform/pii"
)

func TestGuardianDetailsValidate(t *testing.T) {
	s := &GuardianDetailService{today: func() time.Time { return time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC) }}
	d, err := s.validate(GuardianDetailsForm{NIK: " 3201 0101 0101 0001 ", BirthYear: "1980", Education: "09", Occupation: "06", Income: "04", SpecialNeeds: []string{"03", "02", "03", ""}})
	if err != nil || d.NIK.Reveal() != nikA || d.BirthYear != 1980 || strings.Join(d.SpecialNeeds, ",") != "02,03" {
		t.Fatalf("validate = %+v, %v", d, err)
	}
	// Orang tua yang sudah meninggal boleh tanpa NIK dan penghasilan.
	if d, err := s.validate(GuardianDetailsForm{Occupation: ref.KodeMeninggal}); err != nil || !d.Deceased() {
		t.Errorf("deceased parent: %+v, %v", d, err)
	}
	for _, tc := range []struct {
		f     GuardianDetailsForm
		field string
	}{
		{GuardianDetailsForm{NIK: "123"}, "nik"},
		{GuardianDetailsForm{BirthYear: "2020"}, "birth_year"},
		{GuardianDetailsForm{BirthYear: "1900"}, "birth_year"},
		{GuardianDetailsForm{BirthYear: "abc"}, "birth_year"},
		{GuardianDetailsForm{Education: "12"}, "education"},
		{GuardianDetailsForm{Occupation: "13"}, "occupation"},
		{GuardianDetailsForm{Income: "08"}, "income"},
		{GuardianDetailsForm{SpecialNeeds: []string{"01", "02"}}, "special_needs"},
	} {
		_, err := s.validate(tc.f)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Fields[tc.field] == "" {
			t.Errorf("%+v: error = %v, want message on %s", tc.f, err, tc.field)
		}
	}
}

func TestGuardianDetailsSave(t *testing.T) {
	p := newProfileFixture(t)
	ctx := context.Background()
	gsvc := NewGuardianService(p.tx)
	dsvc := NewGuardianDetailService(p.tx, p.keys)

	ibu, err := gsvc.Create(ctx, p.actorA, GuardianForm{FullName: "Sari", Phone: "081234567890"})
	if err != nil {
		t.Fatal(err)
	}
	// Wali tanpa akun portal bisa punya data rinci.
	d, err := dsvc.Save(ctx, p.actorA, ibu.ID, GuardianDetailsForm{NIK: nikA, BirthYear: "1985", Education: "05", Occupation: "07", Income: "03"})
	if err != nil || d.NIK.Reveal() != nikA || d.Education != "05" {
		t.Fatalf("Save = %+v, %v", d, err)
	}

	var enc, idx []byte
	if err := p.tx.QueryRow(ctx, `SELECT nik_enc, nik_idx FROM guardians WHERE id = $1`, ibu.ID).Scan(&enc, &idx); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc, []byte(nikA)) || !bytes.Equal(idx, p.keys.BlindIndex("nik", nikA)) {
		t.Error("guardian NIK not encrypted or blind index wrong")
	}

	// Pencocokan lewat NIK (kakak-adik berbagi wali).
	if g, err := dsvc.FindByNIK(ctx, p.actorA.SchoolID, " 3201 0101 0101 0001"); err != nil || g.ID != ibu.ID {
		t.Errorf("FindByNIK = %+v, %v", g, err)
	}
	if _, err := dsvc.FindByNIK(ctx, p.schoolB, nikA); !errors.Is(err, ErrGuardianNotFound) {
		t.Errorf("FindByNIK in other school: %v", err)
	}

	// NIK ganda di sekolah yang sama ditolak.
	other, _ := gsvc.Create(ctx, p.actorA, GuardianForm{FullName: "Sari Lain"})
	err = sp(t, p.tx, func(q pgx.Tx) error {
		_, err := NewGuardianDetailService(q, p.keys).Save(ctx, p.actorA, other.ID, GuardianDetailsForm{NIK: nikA})
		return err
	})
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Fields["nik"] == "" {
		t.Errorf("duplicate guardian NIK: %v", err)
	}

	// Audit tanpa nilai NIK.
	var after string
	if err := p.tx.QueryRow(ctx, `SELECT after::text FROM audit_logs WHERE entity_id = $1 AND action = $2`, ibu.ID, ActionGuardianDetailsUpdate).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(after, nikA) || !strings.Contains(after, "nik") {
		t.Errorf("audit after = %s", after)
	}

	// Tampilan default ter-mask.
	if d.NIK.String() != "3201********0001" {
		t.Errorf("NIK.String() = %q", d.NIK.String())
	}

	// Wali sekolah lain tidak bisa dibaca atau diubah.
	actorB := p.actorA
	actorB.SchoolID = p.schoolB
	if _, err := dsvc.Get(ctx, p.schoolB, ibu.ID); !errors.Is(err, ErrGuardianNotFound) {
		t.Errorf("Get from other school: %v", err)
	}
	if _, err := dsvc.Save(ctx, actorB, ibu.ID, GuardianDetailsForm{BirthYear: "1990"}); !errors.Is(err, ErrGuardianNotFound) {
		t.Errorf("Save from other school: %v", err)
	}

	// Mengosongkan NIK menghapus ciphertext dan indeks.
	if _, err := dsvc.Save(ctx, p.actorA, ibu.ID, GuardianDetailsForm{BirthYear: "1985", Education: "05", Occupation: "07", Income: "03"}); err != nil {
		t.Fatal(err)
	}
	var cleared bool
	_ = p.tx.QueryRow(ctx, `SELECT nik_enc IS NULL AND nik_idx IS NULL FROM guardians WHERE id = $1`, ibu.ID).Scan(&cleared)
	if !cleared {
		t.Error("clearing NIK left ciphertext")
	}
}

func TestOneBiologicalParent(t *testing.T) {
	p := newProfileFixture(t)
	ctx := context.Background()
	gsvc := NewGuardianService(p.tx)
	dsvc := NewGuardianDetailService(p.tx, p.keys)

	ibu1, _ := gsvc.Create(ctx, p.actorA, GuardianForm{FullName: "Ibu Kandung"})
	ibu2, _ := gsvc.Create(ctx, p.actorA, GuardianForm{FullName: "Ibu Sambung"})
	if err := gsvc.Link(ctx, p.actorA, p.budi, ibu1.ID, RelationMother, true); err != nil {
		t.Fatal(err)
	}
	err := sp(t, p.tx, func(q pgx.Tx) error {
		return NewGuardianService(q).Link(ctx, p.actorA, p.budi, ibu2.ID, RelationMother, false)
	})
	if !errors.Is(err, ErrDuplicateBiological) {
		t.Fatalf("second biological mother: error = %v, want ErrDuplicateBiological", err)
	}

	// Ibu sambung bisa ditautkan sebagai "wali", atau sebagai ibu bukan kandung.
	if err := gsvc.Link(ctx, p.actorA, p.budi, ibu2.ID, RelationGuardian, false); err != nil {
		t.Fatal(err)
	}
	if err := dsvc.SetBiological(ctx, p.actorA, p.budi, ibu2.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := gsvc.Link(ctx, p.actorA, p.budi, ibu2.ID, RelationMother, false); err != nil {
		t.Errorf("non-biological mother: %v", err)
	}
	list, err := gsvc.ListByStudent(ctx, p.actorA.SchoolID, p.budi)
	if err != nil {
		t.Fatal(err)
	}
	bio := map[string]bool{}
	for _, g := range list {
		bio[g.ID] = g.IsBiological
	}
	if !bio[ibu1.ID] || bio[ibu2.ID] {
		t.Errorf("IsBiological = %v", bio)
	}
	if err := dsvc.SetBiological(ctx, p.actorA, p.studentB, ibu1.ID, true); !errors.Is(err, ErrNotLinked) {
		t.Errorf("SetBiological on unlinked student: %v", err)
	}

	// Kakak-adik boleh berbagi ibu kandung yang sama.
	if err := gsvc.Link(ctx, p.actorA, p.ani, ibu1.ID, RelationMother, true); err != nil {
		t.Errorf("sibling shares biological mother: %v", err)
	}
}

func TestGuardianWrongKey(t *testing.T) {
	p := newProfileFixture(t)
	ctx := context.Background()
	g, _ := NewGuardianService(p.tx).Create(ctx, p.actorA, GuardianForm{FullName: "Ayah"})
	if _, err := NewGuardianDetailService(p.tx, p.keys).Save(ctx, p.actorA, g.ID, GuardianDetailsForm{NIK: nikA}); err != nil {
		t.Fatal(err)
	}
	other, _ := pii.NewKeyring(map[byte][]byte{1: bytes.Repeat([]byte{9}, pii.KeySize)}, bytes.Repeat([]byte{8}, pii.KeySize))
	if _, err := NewGuardianDetailService(p.tx, other).Get(ctx, p.actorA.SchoolID, g.ID); !errors.Is(err, pii.ErrDecrypt) {
		t.Errorf("wrong key: %v", err)
	}
}
