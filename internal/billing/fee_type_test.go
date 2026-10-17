package billing

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
	actor            school.Actor
	svc              *FeeTypeService
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
	f := &fixture{tx: tx, svc: NewFeeTypeService(tx)}
	for _, s := range []struct {
		dst  *string
		code string
	}{{&f.schoolA, "test-fee-a"}, {&f.schoolB, "test-fee-b"}} {
		if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ($1, $1) RETURNING id`, s.code).Scan(s.dst); err != nil {
			t.Fatal(err)
		}
	}
	f.actor = school.Actor{SchoolID: f.schoolA, IP: netip.MustParseAddr("192.0.2.5")}
	if err := tx.QueryRow(ctx, `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'bendahara@uji.id', 'x', 'Bendahara') RETURNING id`,
		f.schoolA).Scan(&f.actor.UserID); err != nil {
		t.Fatal(err)
	}
	return f
}

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

func (f *fixture) rawInsert(t *testing.T, name string, amount int64, dueDay any) error {
	t.Helper()
	return f.sp(t, func(q pgx.Tx) error {
		_, err := q.Exec(context.Background(), `INSERT INTO fee_types (school_id, name, default_amount, default_due_day) VALUES ($1, $2, $3, $4)`,
			f.schoolA, name, amount, dueDay)
		return err
	})
}

func TestFeeTypes_DatabaseConstraints(t *testing.T) {
	f := newFixture(t)

	if err := f.rawInsert(t, "Minus", -1, nil); err == nil || !strings.Contains(err.Error(), "fee_types_default_amount_check") {
		t.Errorf("amount -1: error = %v, want check violation", err)
	}
	if err := f.rawInsert(t, "Gratis", 0, nil); err != nil {
		t.Errorf("amount 0: error = %v, want nil", err)
	}
	if err := f.rawInsert(t, "Tgl 29", 1000, 29); err == nil || !strings.Contains(err.Error(), "fee_types_default_due_day_check") {
		t.Errorf("due day 29: error = %v, want check violation", err)
	}
	if err := f.rawInsert(t, "Tgl 0", 1000, 0); err == nil {
		t.Error("due day 0 accepted")
	}
	for _, d := range []int{1, 28} {
		if err := f.rawInsert(t, "Tgl", 1000, d); err != nil {
			t.Errorf("due day %d: error = %v", d, err)
		}
	}
}

func TestFeeTypes_Service(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	spp, err := f.svc.Create(ctx, f.actor, FeeTypeForm{Name: "  SPP  Bulanan ", DefaultAmount: "Rp 150.000", DefaultDueDay: "10"})
	if err != nil {
		t.Fatal(err)
	}
	if spp.Name != "SPP Bulanan" || spp.DefaultAmount != 150000 || spp.DefaultDueDay != 10 || !spp.IsActive {
		t.Errorf("created = %+v", spp)
	}
	free, err := f.svc.Create(ctx, f.actor, FeeTypeForm{Name: "Ekskul Gratis", DefaultAmount: "0"})
	if err != nil || free.DefaultAmount != 0 || free.DefaultDueDay != 0 {
		t.Errorf("free fee type = %+v, %v", free, err)
	}
	if _, err := f.svc.Create(ctx, f.actor, FeeTypeForm{Name: "Uang Pangkal", DefaultAmount: "5.000.000"}); err != nil {
		t.Fatal(err)
	}

	t.Run("same name in the same school is rejected", func(t *testing.T) {
		for _, name := range []string{"SPP Bulanan", "spp bulanan"} {
			_, err := f.svc.Create(ctx, f.actor, FeeTypeForm{Name: name, DefaultAmount: "1"})
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Fields["name"] == "" {
				t.Errorf("%q: error = %v, want name validation error", name, err)
			}
		}
		other := f.actor
		other.SchoolID = f.schoolB
		if _, err := f.svc.Create(ctx, other, FeeTypeForm{Name: "SPP Bulanan", DefaultAmount: "200.000"}); err != nil {
			t.Errorf("same name in another school: %v", err)
		}
	})

	t.Run("validation", func(t *testing.T) {
		cases := map[string]FeeTypeForm{
			"name":            {Name: "", DefaultAmount: "1"},
			"default_amount":  {Name: "X", DefaultAmount: "-1"},
			"default_due_day": {Name: "X", DefaultAmount: "1", DefaultDueDay: "29"},
		}
		for field, form := range cases {
			_, err := f.svc.Create(ctx, f.actor, form)
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Fields[field] == "" {
				t.Errorf("%s: error = %v", field, err)
			}
		}
		_, err := f.svc.Create(ctx, f.actor, FeeTypeForm{Name: strings.Repeat("a", 61), DefaultAmount: "1,5", DefaultDueDay: "x"})
		var ve *ValidationError
		if !errors.As(err, &ve) || len(ve.Fields) != 3 {
			t.Errorf("multiple errors = %v", err)
		}
	})

	t.Run("deactivate hides from ListActive", func(t *testing.T) {
		if _, changed, err := f.svc.SetActive(ctx, f.actor, free.ID, false); err != nil || !changed {
			t.Fatalf("deactivate: changed=%v err=%v", changed, err)
		}
		if _, changed, _ := f.svc.SetActive(ctx, f.actor, free.ID, false); changed {
			t.Error("deactivating twice reported a change")
		}
		active, _ := f.svc.ListActive(ctx, f.schoolA)
		all, _ := f.svc.ListAll(ctx, f.schoolA)
		var activeNames []string
		for _, ft := range active {
			activeNames = append(activeNames, ft.Name)
		}
		if strings.Join(activeNames, ",") != "SPP Bulanan,Uang Pangkal" {
			t.Errorf("ListActive = %v", activeNames)
		}
		if len(all) != 3 || all[2].Name != "Ekskul Gratis" || all[2].IsActive {
			t.Errorf("ListAll = %+v (inactive last)", all)
		}
		if _, changed, err := f.svc.SetActive(ctx, f.actor, free.ID, true); err != nil || !changed {
			t.Errorf("reactivate: changed=%v err=%v", changed, err)
		}
	})

	t.Run("other school's fee types are invisible", func(t *testing.T) {
		list, _ := f.svc.ListAll(ctx, f.schoolB)
		if len(list) != 1 || list[0].SchoolID != f.schoolB {
			t.Errorf("school B list = %+v", list)
		}
		if _, err := f.svc.Get(ctx, f.schoolB, spp.ID); !errors.Is(err, ErrFeeTypeNotFound) {
			t.Errorf("get from other school: %v", err)
		}
		other := f.actor
		other.SchoolID = f.schoolB
		if _, err := f.svc.Update(ctx, other, spp.ID, FeeTypeForm{Name: "Diretas", DefaultAmount: "1"}); !errors.Is(err, ErrFeeTypeNotFound) {
			t.Errorf("update from other school: %v", err)
		}
		if _, _, err := f.svc.SetActive(ctx, other, spp.ID, false); !errors.Is(err, ErrFeeTypeNotFound) {
			t.Errorf("deactivate from other school: %v", err)
		}
		if _, err := f.svc.Get(ctx, f.schoolA, "bukan-uuid"); !errors.Is(err, ErrFeeTypeNotFound) {
			t.Errorf("invalid id: %v", err)
		}
	})

	t.Run("update and audit", func(t *testing.T) {
		u, err := f.svc.Update(ctx, f.actor, spp.ID, FeeTypeForm{Name: "SPP Bulanan", DefaultAmount: "175.000", DefaultDueDay: ""})
		if err != nil || u.DefaultAmount != 175000 || u.DefaultDueDay != 0 {
			t.Errorf("update = %+v, %v", u, err)
		}
		// Tanpa perubahan: tidak ada audit baru.
		if _, err := f.svc.Update(ctx, f.actor, spp.ID, FeeTypeForm{Name: "SPP Bulanan", DefaultAmount: "175000"}); err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		rows, _ := f.tx.Query(ctx, `SELECT action, count(*) FROM audit_logs WHERE school_id = $1 GROUP BY action`, f.schoolA)
		for rows.Next() {
			var a string
			var n int
			_ = rows.Scan(&a, &n)
			counts[a] = n
		}
		if counts[ActionFeeTypeCreate] != 3 || counts[ActionFeeTypeUpdate] != 1 || counts[ActionFeeTypeDeactivate] != 1 || counts[ActionFeeTypeActivate] != 1 {
			t.Errorf("audit counts = %v", counts)
		}
		var before, after int64
		_ = f.tx.QueryRow(ctx, `SELECT (before->>'default_amount')::bigint, (after->>'default_amount')::bigint FROM audit_logs WHERE action = $1 AND school_id = $2`,
			ActionFeeTypeUpdate, f.schoolA).Scan(&before, &after)
		if before != 150000 || after != 175000 {
			t.Errorf("update audit amounts = %d -> %d", before, after)
		}
	})
}
