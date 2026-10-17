package students

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"akra/internal/school"
)

func TestNormalizePhone(t *testing.T) {
	ok := map[string]string{
		"0812-3456-7890":    "+6281234567890",
		"+62 812 3456 7890": "+6281234567890",
		"6281234567890":     "+6281234567890",
		"81234567890":       "+6281234567890",
		" 0812.3456.7890 ":  "+6281234567890",
		"(021) 555-1234":    "+62215551234",
		"+65 6123 4567":     "+6561234567",
		"+1 (415) 555-0100": "+14155550100",
	}
	for in, want := range ok {
		got, err := NormalizePhone(in)
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "0812-3456-789O", "+62 0812 3456", "0812", "08123456789012345", "0812/3456/7890", "٠٨١٢٣٤٥٦٧٨٩٠"} {
		if got, err := NormalizePhone(in); !errors.Is(err, ErrInvalidPhone) {
			t.Errorf("NormalizePhone(%q) = %q, %v; want ErrInvalidPhone", in, got, err)
		}
	}
}

func TestValidateGuardian(t *testing.T) {
	in, err := validateGuardian(GuardianForm{FullName: "  Ibu   Sari ", Phone: "0812-3456-7890", Email: " Sari@Contoh.ID "})
	if err != nil || in.FullName != "Ibu Sari" || in.Phone != "+6281234567890" || in.Email != "sari@contoh.id" {
		t.Errorf("validateGuardian = %+v, %v", in, err)
	}
	_, err = validateGuardian(GuardianForm{FullName: "", Phone: "12", Email: "bukan email"})
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Fields["full_name"] == "" || ve.Fields["phone"] == "" || ve.Fields["email"] == "" {
		t.Errorf("invalid form: error = %v", err)
	}
}

type guardianFixture struct {
	tx                  pgx.Tx
	svc                 *GuardianService
	actorA              school.Actor
	schoolA, schoolB    string
	budi, ani, studentB string
}

func newGuardianFixture(t *testing.T) *guardianFixture {
	t.Helper()
	_, tx, f := txFixture(t)
	ctx := context.Background()
	g := &guardianFixture{tx: tx, svc: NewGuardianService(tx), schoolA: f.schoolA, schoolB: f.schoolB, studentB: f.studentB}
	g.actorA = school.Actor{SchoolID: f.schoolA, UserID: "66666666-6666-6666-6666-666666666666", IP: netip.MustParseAddr("192.0.2.10")}
	g.budi = f.student
	if err := tx.QueryRow(ctx, `INSERT INTO students (school_id, nis, full_name) VALUES ($1, '002', 'Ani') RETURNING id`, f.schoolA).Scan(&g.ani); err != nil {
		t.Fatal(err)
	}
	return g
}

func (g *guardianFixture) guardian(t *testing.T, schoolID, name, phone string) Guardian {
	t.Helper()
	actor := g.actorA
	actor.SchoolID = schoolID
	gd, err := g.svc.Create(context.Background(), actor, GuardianForm{FullName: name, Phone: phone})
	if err != nil {
		t.Fatal(err)
	}
	return gd
}

func TestGuardians(t *testing.T) {
	g := newGuardianFixture(t)
	ctx := context.Background()

	ayah := g.guardian(t, g.schoolA, "Pak Joko", "0812-3456-7890")
	ibu := g.guardian(t, g.schoolA, "Bu Rina", "+62 812 3456 7890")
	waliB := g.guardian(t, g.schoolB, "Wali Sekolah B", "")

	t.Run("phones are stored in one format", func(t *testing.T) {
		var p1, p2 string
		_ = g.tx.QueryRow(ctx, `SELECT phone FROM guardians WHERE id = $1`, ayah.ID).Scan(&p1)
		_ = g.tx.QueryRow(ctx, `SELECT phone FROM guardians WHERE id = $1`, ibu.ID).Scan(&p2)
		if p1 != "+6281234567890" || p1 != p2 {
			t.Errorf("stored phones = %q, %q; want both +6281234567890", p1, p2)
		}
	})

	t.Run("one student with two guardians", func(t *testing.T) {
		if err := g.svc.Link(ctx, g.actorA, g.budi, ayah.ID, RelationFather, true); err != nil {
			t.Fatal(err)
		}
		if err := g.svc.Link(ctx, g.actorA, g.budi, ibu.ID, RelationMother, false); err != nil {
			t.Fatal(err)
		}
		list, err := g.svc.ListByStudent(ctx, g.schoolA, g.budi)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].ID != ayah.ID || !list[0].IsPrimary || list[0].Relationship != "ayah" ||
			list[1].ID != ibu.ID || list[1].IsPrimary {
			t.Errorf("guardians of Budi = %+v", list)
		}
	})

	t.Run("one guardian with two children", func(t *testing.T) {
		if err := g.svc.Link(ctx, g.actorA, g.ani, ayah.ID, RelationFather, true); err != nil {
			t.Fatal(err)
		}
		kids, err := g.svc.ListByGuardian(ctx, g.schoolA, ayah.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(kids) != 2 || kids[0].FullName != "Ani" || kids[1].FullName != "Siswa A" {
			t.Errorf("children of Pak Joko = %+v", kids)
		}
	})

	t.Run("database rejects two primary guardians", func(t *testing.T) {
		err := sp(t, g.tx, func(q pgx.Tx) error {
			_, err := q.Exec(ctx, `UPDATE student_guardians SET is_primary = true WHERE student_id = $1`, g.budi)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "student_one_primary") {
			t.Errorf("error = %v, want unique violation on student_one_primary", err)
		}
	})

	t.Run("making another guardian primary moves the flag", func(t *testing.T) {
		if err := g.svc.Link(ctx, g.actorA, g.budi, ibu.ID, RelationMother, true); err != nil {
			t.Fatal(err)
		}
		list, _ := g.svc.ListByStudent(ctx, g.schoolA, g.budi)
		if len(list) != 2 || list[0].ID != ibu.ID || !list[0].IsPrimary || list[1].IsPrimary {
			t.Errorf("after switching primary: %+v", list)
		}
	})

	t.Run("guardians of another school are never listed or linked", func(t *testing.T) {
		if list, _ := g.svc.ListByStudent(ctx, g.schoolB, g.budi); len(list) != 0 {
			t.Errorf("school B sees %d guardians of school A's student", len(list))
		}
		if kids, _ := g.svc.ListByGuardian(ctx, g.schoolA, waliB.ID); len(kids) != 0 {
			t.Errorf("school A sees children of school B's guardian")
		}
		err := sp(t, g.tx, func(q pgx.Tx) error {
			return LinkToStudent(ctx, q, g.schoolA, g.budi, waliB.ID, RelationGuardian, false)
		})
		if !errors.Is(err, ErrGuardianNotFound) {
			t.Errorf("linking school B's guardian: error = %v, want ErrGuardianNotFound", err)
		}
		err = sp(t, g.tx, func(q pgx.Tx) error {
			return LinkToStudent(ctx, q, g.schoolA, g.studentB, ayah.ID, RelationFather, false)
		})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("linking school B's student: error = %v, want ErrNotFound", err)
		}
		// Database menolak walau repository dilewati.
		err = sp(t, g.tx, func(q pgx.Tx) error {
			_, err := q.Exec(ctx, `INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship) VALUES ($1, $2, $3, 'wali')`,
				g.schoolA, g.budi, waliB.ID)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "student_guardians_guardian_fkey") {
			t.Errorf("raw cross-school link: error = %v", err)
		}
	})

	t.Run("relationship must be known", func(t *testing.T) {
		if err := g.svc.Link(ctx, g.actorA, g.ani, ibu.ID, "om", false); err == nil {
			t.Error("unknown relationship accepted")
		}
	})

	t.Run("unlink keeps the guardian", func(t *testing.T) {
		if err := g.svc.Unlink(ctx, g.actorA, g.ani, ayah.ID); err != nil {
			t.Fatal(err)
		}
		if err := g.svc.Unlink(ctx, g.actorA, g.ani, ayah.ID); !errors.Is(err, ErrNotLinked) {
			t.Errorf("second unlink: error = %v, want ErrNotLinked", err)
		}
		if _, err := GetGuardian(ctx, g.tx, g.schoolA, ayah.ID); err != nil {
			t.Errorf("guardian removed by unlink: %v", err)
		}
		if kids, _ := g.svc.ListByGuardian(ctx, g.schoolA, ayah.ID); len(kids) != 1 {
			t.Errorf("children after unlink = %d, want 1", len(kids))
		}
	})

	t.Run("audit", func(t *testing.T) {
		counts := map[string]int{}
		rows, _ := g.tx.Query(ctx, `SELECT action, count(*) FROM audit_logs WHERE school_id = $1 AND action LIKE 'guardian.%' GROUP BY action`, g.schoolA)
		for rows.Next() {
			var a string
			var n int
			_ = rows.Scan(&a, &n)
			counts[a] = n
		}
		if counts[ActionGuardianCreate] != 2 || counts[ActionGuardianLink] != 4 || counts[ActionGuardianUnlink] != 1 {
			t.Errorf("audit counts = %v", counts)
		}
	})
}
