package academic

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
	"akra/internal/school"
)

type fixture struct {
	tx               pgx.Tx
	schoolA, schoolB string
	actor            school.Actor // admin/TU sekolah A
	actorB           school.Actor // admin/TU sekolah B
	svc              *SubjectService
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	f := &fixture{tx: tx, svc: NewSubjectService(tx)}
	for _, s := range []struct {
		dst   *string
		actor *school.Actor
		code  string
	}{{&f.schoolA, &f.actor, "test-mapel-a"}, {&f.schoolB, &f.actorB, "test-mapel-b"}} {
		if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ($1, $1) RETURNING id`, s.code).Scan(s.dst); err != nil {
			t.Fatal(err)
		}
		*s.actor = school.Actor{SchoolID: *s.dst, IP: netip.MustParseAddr("192.0.2.5")}
		if err := tx.QueryRow(ctx, `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, $2, 'x', 'Admin') RETURNING id`,
			*s.dst, "admin@"+s.code+".uji").Scan(&s.actor.UserID); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// sp menjalankan fn di savepoint yang selalu dibatalkan, agar error
// constraint tidak membatalkan transaksi uji.
func (f *fixture) sp(t *testing.T, fn func(pgx.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	s, err := f.tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Rollback(ctx) }()
	return fn(s)
}

func (f *fixture) rawInsert(t *testing.T, schoolID, name string, code any) error {
	t.Helper()
	return f.sp(t, func(q pgx.Tx) error {
		_, err := q.Exec(context.Background(), `INSERT INTO subjects (school_id, name, code) VALUES ($1, $2, $3)`, schoolID, name, code)
		return err
	})
}

func (f *fixture) audits(schoolID, action string) int {
	var n int
	_ = f.tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, schoolID, action).Scan(&n)
	return n
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %v, want *ValidationError", err)
	}
	return ve.Fields
}

func TestValidateSubject(t *testing.T) {
	for _, tc := range []struct {
		name       string
		form       SubjectForm
		wantName   string
		wantCode   string
		wantFields []string
	}{
		{name: "spasi dirapikan", form: SubjectForm{Name: "  Bahasa   Indonesia ", Code: " b.ind "}, wantName: "Bahasa Indonesia", wantCode: "b.ind"},
		{name: "kode boleh kosong", form: SubjectForm{Name: "Seni Budaya", Code: "   "}, wantName: "Seni Budaya"},
		{name: "nama kosong", form: SubjectForm{Name: "   "}, wantFields: []string{"name"}},
		{name: "nama terlalu panjang", form: SubjectForm{Name: strings.Repeat("a", 61)}, wantFields: []string{"name"}},
		{name: "nama 60 karakter", form: SubjectForm{Name: strings.Repeat("é", 60)}, wantName: strings.Repeat("é", 60)},
		{name: "kode terlalu panjang", form: SubjectForm{Name: "IPA", Code: strings.Repeat("A", 21)}, wantFields: []string{"code"}},
		{name: "nama dan kode salah", form: SubjectForm{Name: "", Code: strings.Repeat("A", 21)}, wantFields: []string{"code", "name"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := ValidateSubject(tc.form)
			if len(tc.wantFields) > 0 {
				got := fieldsOf(t, err)
				if len(got) != len(tc.wantFields) {
					t.Fatalf("fields = %v, want %v", got, tc.wantFields)
				}
				for _, k := range tc.wantFields {
					if got[k] == "" {
						t.Errorf("no error for field %q: %v", k, got)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if in.Name != tc.wantName || in.Code != tc.wantCode {
				t.Errorf("input = %+v, want name %q code %q", in, tc.wantName, tc.wantCode)
			}
		})
	}
}

func TestParseStatus(t *testing.T) {
	for in, want := range map[string]bool{"aktif": true, " aktif ": true, "nonaktif": false} {
		got, err := parseStatus(in)
		if err != nil || got != want {
			t.Errorf("parseStatus(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	// Isian kosong atau tidak dikenal tidak boleh dianggap "nonaktif".
	for _, in := range []string{"", "  ", "AKTIF", "ya", "0"} {
		if _, err := parseStatus(in); err == nil || fieldsOf(t, err)["status"] == "" {
			t.Errorf("parseStatus(%q) error = %v, want status validation error", in, err)
		}
	}
}

func TestSubjects_DatabaseConstraints(t *testing.T) {
	f := newFixture(t)

	if err := f.rawInsert(t, f.schoolA, "   ", nil); err == nil || !strings.Contains(err.Error(), "subjects_name_check") {
		t.Errorf("blank name: error = %v, want name check violation", err)
	}
	if err := f.rawInsert(t, f.schoolA, "Matematika", "  "); err == nil || !strings.Contains(err.Error(), "subjects_code_check") {
		t.Errorf("blank code: error = %v, want code check violation", err)
	}
	ctx := context.Background()
	for _, name := range []string{"Seni", "Olahraga"} { // banyak mapel tanpa kode
		if _, err := f.tx.Exec(ctx, `INSERT INTO subjects (school_id, name) VALUES ($1, $2)`, f.schoolA, name); err != nil {
			t.Fatalf("subject without code %q: %v", name, err)
		}
	}
	if _, err := f.tx.Exec(ctx, `INSERT INTO subjects (school_id, name, code) VALUES ($1, 'Matematika', 'MTK')`, f.schoolA); err != nil {
		t.Fatal(err)
	}
	if err := f.rawInsert(t, f.schoolA, "matematika", nil); err == nil || !strings.Contains(err.Error(), "subjects_school_id_name_key") {
		t.Errorf("same name, other case: error = %v, want name unique violation", err)
	}
	if err := f.rawInsert(t, f.schoolA, "Aljabar", "mtk"); err == nil || !strings.Contains(err.Error(), "subjects_school_id_code_key") {
		t.Errorf("same code, other case: error = %v, want code unique violation", err)
	}
	// Sekolah lain bebas memakai nama dan kode yang sama.
	if err := f.rawInsert(t, f.schoolB, "Matematika", "MTK"); err != nil {
		t.Errorf("same name and code in another school: %v", err)
	}
}

func TestSubjects_Create(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	got, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: "  Bahasa   Indonesia ", Code: " B.IND "})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Bahasa Indonesia" || got.Code != "B.IND" || !got.IsActive || got.SchoolID != f.schoolA {
		t.Errorf("created = %+v", got)
	}
	if n := f.audits(f.schoolA, ActionSubjectCreate); n != 1 {
		t.Errorf("subject.create audits = %d, want 1", n)
	}

	// Dua mapel tanpa kode diizinkan.
	for _, name := range []string{"Seni", "Olahraga"} {
		s, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: name})
		if err != nil || s.Code != "" {
			t.Errorf("create %q without code = %+v, %v", name, s, err)
		}
	}

	t.Run("same name in the same school is rejected", func(t *testing.T) {
		for _, name := range []string{"Bahasa Indonesia", "bahasa indonesia", "  BAHASA   INDONESIA "} {
			_, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: name})
			if fieldsOf(t, err)["name"] == "" {
				t.Errorf("name %q: want error on field name, got %v", name, err)
			}
		}
		list, _ := f.svc.ListAll(ctx, f.schoolA)
		if len(list) != 3 {
			t.Errorf("subjects after rejected duplicates = %d, want 3", len(list))
		}
		if n := f.audits(f.schoolA, ActionSubjectCreate); n != 3 {
			t.Errorf("subject.create audits = %d, want 3 (rejected creates are not audited)", n)
		}
	})

	t.Run("same code in the same school is rejected", func(t *testing.T) {
		_, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: "Bahasa Daerah", Code: "b.ind"})
		if fieldsOf(t, err)["code"] == "" {
			t.Errorf("want error on field code, got %v", err)
		}
	})

	t.Run("another school may reuse the name and code", func(t *testing.T) {
		s, err := f.svc.Create(ctx, f.actorB, SubjectForm{Name: "Bahasa Indonesia", Code: "B.IND"})
		if err != nil || s.SchoolID != f.schoolB {
			t.Errorf("school B create = %+v, %v", s, err)
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		if _, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: " "}); fieldsOf(t, err)["name"] == "" {
			t.Errorf("empty name: %v", err)
		}
		if _, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: strings.Repeat("x", 61)}); fieldsOf(t, err)["name"] == "" {
			t.Errorf("long name: %v", err)
		}
	})
}

func TestSubjects_ListsAndSchoolIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mk := func(actor school.Actor, name string) Subject {
		t.Helper()
		s, err := f.svc.Create(ctx, actor, SubjectForm{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	mtk, ipa, seni := mk(f.actor, "Matematika"), mk(f.actor, "IPA"), mk(f.actor, "Seni Budaya")
	other := mk(f.actorB, "Rahasia Sekolah B")

	if _, err := f.svc.Update(ctx, f.actor, ipa.ID, SubjectForm{Name: "IPA", Status: StatusInactive}); err != nil {
		t.Fatal(err)
	}

	names := func(list []Subject) string {
		var n []string
		for _, s := range list {
			n = append(n, s.Name)
		}
		return strings.Join(n, ",")
	}
	active, err := f.svc.ListActive(ctx, f.schoolA)
	if err != nil {
		t.Fatal(err)
	}
	// Mapel nonaktif tidak muncul di pilihan penugasan dan jadwal.
	if got := names(active); got != "Matematika,Seni Budaya" {
		t.Errorf("ListActive = %q, want %q", got, "Matematika,Seni Budaya")
	}
	all, _ := f.svc.ListAll(ctx, f.schoolA)
	if got := names(all); got != "Matematika,Seni Budaya,IPA" { // aktif dulu, nonaktif di bawah
		t.Errorf("ListAll = %q, want %q", got, "Matematika,Seni Budaya,IPA")
	}
	for _, s := range append(active, all...) {
		if s.ID == other.ID {
			t.Error("a subject of school B is listed for school A")
		}
	}

	t.Run("get", func(t *testing.T) {
		if got, err := f.svc.Get(ctx, f.schoolA, seni.ID); err != nil || got.Name != "Seni Budaya" {
			t.Errorf("Get own = %+v, %v", got, err)
		}
		// Mapel nonaktif tetap bisa dibuka lewat ID (untuk penugasan lama).
		if got, err := f.svc.Get(ctx, f.schoolA, ipa.ID); err != nil || got.IsActive {
			t.Errorf("Get inactive = %+v, %v", got, err)
		}
		if _, err := f.svc.Get(ctx, f.schoolB, mtk.ID); !errors.Is(err, ErrSubjectNotFound) {
			t.Errorf("Get of another school's subject: error = %v, want ErrSubjectNotFound", err)
		}
		if _, err := f.svc.Get(ctx, f.schoolA, "bukan-uuid"); !errors.Is(err, ErrSubjectNotFound) {
			t.Errorf("Get with invalid id: error = %v, want ErrSubjectNotFound", err)
		}
	})

	t.Run("update by another school is not found and changes nothing", func(t *testing.T) {
		_, err := f.svc.Update(ctx, f.actorB, mtk.ID, SubjectForm{Name: "Diretas", Status: StatusInactive})
		if !errors.Is(err, ErrSubjectNotFound) {
			t.Errorf("error = %v, want ErrSubjectNotFound", err)
		}
		// ID sekolah lain juga "tidak ada" walau isian form tidak valid.
		_, err = f.svc.Update(ctx, f.actorB, mtk.ID, SubjectForm{Name: "", Status: ""})
		if !errors.Is(err, ErrSubjectNotFound) {
			t.Errorf("invalid form, error = %v, want ErrSubjectNotFound", err)
		}
		got, _ := f.svc.Get(ctx, f.schoolA, mtk.ID)
		if got.Name != "Matematika" || !got.IsActive {
			t.Errorf("subject of school A was changed: %+v", got)
		}
	})
}

func TestSubjects_UpdateAndAudit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	mtk, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: "Matematika", Code: "MTK"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: "IPA", Code: "IPA"})
	if err != nil {
		t.Fatal(err)
	}
	update := func(id string, form SubjectForm) (Subject, error) {
		return f.svc.Update(ctx, f.actor, id, form)
	}

	t.Run("saving without changes is not audited", func(t *testing.T) {
		got, err := update(mtk.ID, SubjectForm{Name: "Matematika", Code: "MTK", Status: StatusActive})
		if err != nil || got.Name != "Matematika" {
			t.Fatalf("update = %+v, %v", got, err)
		}
		if n := f.audits(f.schoolA, ActionSubjectUpdate); n != 0 {
			t.Errorf("subject.update audits = %d, want 0", n)
		}
	})

	t.Run("name and code change", func(t *testing.T) {
		got, err := update(mtk.ID, SubjectForm{Name: "  Matematika   Wajib ", Code: "mtk-w", Status: StatusActive})
		if err != nil || got.Name != "Matematika Wajib" || got.Code != "mtk-w" || !got.IsActive {
			t.Fatalf("update = %+v, %v", got, err)
		}
		if n := f.audits(f.schoolA, ActionSubjectUpdate); n != 1 {
			t.Errorf("subject.update audits = %d, want 1", n)
		}
		if n := f.audits(f.schoolA, ActionSubjectDeactivate) + f.audits(f.schoolA, ActionSubjectActivate); n != 0 {
			t.Errorf("status audits = %d, want 0", n)
		}
	})

	t.Run("code can be cleared", func(t *testing.T) {
		got, err := update(mtk.ID, SubjectForm{Name: "Matematika Wajib", Code: "  ", Status: StatusActive})
		if err != nil || got.Code != "" {
			t.Fatalf("update = %+v, %v", got, err)
		}
	})

	t.Run("deactivate then activate", func(t *testing.T) {
		got, err := update(mtk.ID, SubjectForm{Name: "Matematika Wajib", Status: StatusInactive})
		if err != nil || got.IsActive {
			t.Fatalf("deactivate = %+v, %v", got, err)
		}
		if n := f.audits(f.schoolA, ActionSubjectDeactivate); n != 1 {
			t.Errorf("subject.deactivate audits = %d, want 1", n)
		}
		// Menyimpan lagi dengan status yang sama tidak menambah audit.
		if _, err := update(mtk.ID, SubjectForm{Name: "Matematika Wajib", Status: StatusInactive}); err != nil {
			t.Fatal(err)
		}
		if n := f.audits(f.schoolA, ActionSubjectDeactivate); n != 1 {
			t.Errorf("subject.deactivate audits after no-op = %d, want 1", n)
		}
		got, err = update(mtk.ID, SubjectForm{Name: "Matematika Wajib", Status: StatusActive})
		if err != nil || !got.IsActive {
			t.Fatalf("activate = %+v, %v", got, err)
		}
		if n := f.audits(f.schoolA, ActionSubjectActivate); n != 1 {
			t.Errorf("subject.activate audits = %d, want 1", n)
		}
	})

	t.Run("name and status change together write two audits", func(t *testing.T) {
		before := f.audits(f.schoolA, ActionSubjectUpdate)
		got, err := update(other.ID, SubjectForm{Name: "IPA Terpadu", Code: "IPA", Status: StatusInactive})
		if err != nil || got.Name != "IPA Terpadu" || got.IsActive {
			t.Fatalf("update = %+v, %v", got, err)
		}
		if n := f.audits(f.schoolA, ActionSubjectUpdate) - before; n != 1 {
			t.Errorf("new subject.update audits = %d, want 1", n)
		}
		if n := f.audits(f.schoolA, ActionSubjectDeactivate); n != 2 {
			t.Errorf("subject.deactivate audits = %d, want 2", n)
		}
	})

	t.Run("duplicates and bad status are rejected and nothing is saved", func(t *testing.T) {
		auditsBefore := f.audits(f.schoolA, ActionSubjectUpdate) + f.audits(f.schoolA, ActionSubjectDeactivate) + f.audits(f.schoolA, ActionSubjectActivate)

		_, err := update(mtk.ID, SubjectForm{Name: "ipa terpadu", Status: StatusActive})
		if fieldsOf(t, err)["name"] == "" {
			t.Errorf("duplicate name: %v", err)
		}
		_, err = update(mtk.ID, SubjectForm{Name: "Matematika Wajib", Code: "ipa", Status: StatusActive})
		if fieldsOf(t, err)["code"] == "" {
			t.Errorf("duplicate code: %v", err)
		}
		// Status hilang atau tidak dikenal tidak boleh menonaktifkan mapel.
		for _, status := range []string{"", "ya"} {
			_, err = update(mtk.ID, SubjectForm{Name: "Matematika Wajib", Status: status})
			if fieldsOf(t, err)["status"] == "" {
				t.Errorf("status %q: %v", status, err)
			}
		}
		_, err = update(mtk.ID, SubjectForm{Name: " ", Status: StatusActive})
		if fieldsOf(t, err)["name"] == "" {
			t.Errorf("blank name: %v", err)
		}

		got, _ := f.svc.Get(ctx, f.schoolA, mtk.ID)
		if got.Name != "Matematika Wajib" || got.Code != "" || !got.IsActive {
			t.Errorf("subject changed by rejected updates: %+v", got)
		}
		if n := f.audits(f.schoolA, ActionSubjectUpdate) + f.audits(f.schoolA, ActionSubjectDeactivate) + f.audits(f.schoolA, ActionSubjectActivate); n != auditsBefore {
			t.Errorf("audits after rejected updates = %d, want %d", n, auditsBefore)
		}
	})

	t.Run("a rejected name change does not apply the status change", func(t *testing.T) {
		// Nama bentrok dengan mapel lain sambil dinonaktifkan: seluruh
		// perubahan batal.
		_, err := update(mtk.ID, SubjectForm{Name: "IPA Terpadu", Status: StatusInactive})
		if fieldsOf(t, err)["name"] == "" {
			t.Fatalf("error = %v", err)
		}
		if got, _ := f.svc.Get(ctx, f.schoolA, mtk.ID); !got.IsActive {
			t.Error("subject was deactivated although the update failed")
		}
	})
}

func TestSubjects_KKM(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sub, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: "Matematika", KKM: "75"})
	if err != nil || sub.KKM == nil || *sub.KKM != 75 {
		t.Fatalf("create with KKM: %v %+v", err, sub)
	}
	plain, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: "IPA"})
	if err != nil || plain.KKM != nil {
		t.Fatalf("create without KKM: %v %+v", err, plain)
	}
	for _, bad := range []string{"101", "-1", "tujuh", "7.5"} {
		_, err := f.svc.Create(ctx, f.actor, SubjectForm{Name: "Seni " + bad, KKM: bad})
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Fields["kkm"] == "" {
			t.Errorf("KKM %q: err = %v", bad, err)
		}
	}
	// Mengubah KKM saja tetap diaudit; mengosongkan kembali ke default (NULL).
	before := f.audits(f.schoolA, ActionSubjectUpdate)
	sub, err = f.svc.Update(ctx, f.actor, sub.ID, SubjectForm{Name: "Matematika", KKM: "80", Status: StatusActive})
	if err != nil || *sub.KKM != 80 || f.audits(f.schoolA, ActionSubjectUpdate) != before+1 {
		t.Fatalf("update KKM: %v %+v", err, sub)
	}
	sub, err = f.svc.Update(ctx, f.actor, sub.ID, SubjectForm{Name: "Matematika", KKM: " ", Status: StatusActive})
	if err != nil || sub.KKM != nil {
		t.Errorf("clear KKM: %v %+v", err, sub)
	}
	if got := EffectiveKKM(sub, 70); got != 70 {
		t.Errorf("EffectiveKKM default = %d", got)
	}
	if kkm, label, err := SchoolThreshold(ctx, f.tx, f.schoolA); err != nil || kkm != DefaultKKM || label != "KKM" {
		t.Errorf("school threshold: %d %s %v", kkm, label, err)
	}
	if ThresholdLabel(ReportFormatMerdeka) != "KKTP" || ThresholdLabel("") != "KKM" {
		t.Error("threshold labels")
	}
	// Database menolak KKM di luar 0–100.
	if _, err := f.tx.Exec(ctx, `SAVEPOINT k`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.Exec(ctx, `UPDATE subjects SET kkm = 101 WHERE id = $1`, sub.ID); err == nil || !strings.Contains(err.Error(), "subjects_kkm_check") {
		t.Errorf("kkm 101: %v", err)
	}
	_, _ = f.tx.Exec(ctx, `ROLLBACK TO SAVEPOINT k`)
}
