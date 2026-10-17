package audit

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestList(t *testing.T) {
	pool := testDB(t)
	inTx(t, pool, func(tx pgx.Tx, schoolID string) {
		ctx := context.Background()
		var userID, otherSchool string
		if err := tx.QueryRow(ctx, `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'kepala@uji.id', 'x', 'Bu Kepala') RETURNING id`, schoolID).Scan(&userID); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('test-audit-b', 'test-audit-b') RETURNING id`).Scan(&otherSchool); err != nil {
			t.Fatal(err)
		}
		insert := func(school, action, entity, at string, user any) {
			t.Helper()
			if _, err := tx.Exec(ctx, `INSERT INTO audit_logs (school_id, user_id, action, entity_type, entity_id, after, created_at)
				VALUES ($1, $2, $3, 'payment', $4, '{"amount": 150000}', $5)`, school, user, action, entity, at); err != nil {
				t.Fatal(err)
			}
		}
		insert(schoolID, ActionPaymentCreate, "pay-1", "2026-10-01T09:00:00+07:00", userID)
		insert(schoolID, ActionVoidRequest, "pay-1", "2026-10-02T09:00:00+07:00", userID)
		insert(schoolID, ActionVoidApprove, "pay-1", "2026-10-03T09:00:00+07:00", nil)
		insert(schoolID, ActionPaymentCreate, "pay-2", "2026-10-03T23:30:00+07:00", userID)
		insert(schoolID, "student.create", "stu-1", "2026-10-03T10:00:00+07:00", userID)
		insert(otherSchool, ActionPaymentCreate, "pay-1", "2026-10-02T09:00:00+07:00", nil)

		ids := func(rows []Row) (out []string) {
			for _, r := range rows {
				out = append(out, r.Action+"/"+r.EntityID)
			}
			return out
		}
		eq := func(name string, got []Row, want ...string) {
			t.Helper()
			g := ids(got)
			if len(g) != len(want) {
				t.Errorf("%s: got %v, want %v", name, g, want)
				return
			}
			for i := range want {
				if g[i] != want[i] {
					t.Errorf("%s: got %v, want %v", name, g, want)
					return
				}
			}
		}

		// "Siapa membatalkan pembayaran pay-1?"
		rows, more, err := List(ctx, tx, schoolID, Filter{Actions: FinanceActions, EntityID: "pay-1"})
		if err != nil || more {
			t.Fatal(err, more)
		}
		eq("entity pay-1", rows, "payment.void_approve/pay-1", "payment.void_request/pay-1", "payment.create/pay-1")
		if rows[1].UserName != "Bu Kepala" || rows[0].UserName != "" || rows[1].After["amount"] != float64(150000) {
			t.Errorf("row details: %+v", rows[:2])
		}

		rows, _, _ = List(ctx, tx, schoolID, Filter{Actions: FinanceActions})
		eq("finance only", rows, "payment.create/pay-2", "payment.void_approve/pay-1", "payment.void_request/pay-1", "payment.create/pay-1")

		rows, _, _ = List(ctx, tx, schoolID, Filter{Actions: []string{ActionPaymentCreate}})
		eq("one action", rows, "payment.create/pay-2", "payment.create/pay-1")

		wib := time.FixedZone("WIB", 7*3600)
		rows, _, _ = List(ctx, tx, schoolID, Filter{Actions: FinanceActions,
			From: time.Date(2026, 10, 2, 0, 0, 0, 0, wib), To: time.Date(2026, 10, 3, 0, 0, 0, 0, wib)})
		eq("date range", rows, "payment.void_request/pay-1")

		page1, more, _ := List(ctx, tx, schoolID, Filter{Actions: FinanceActions, Limit: 3})
		if !more {
			t.Error("first page must report more rows")
		}
		page2, more, _ := List(ctx, tx, schoolID, Filter{Actions: FinanceActions, Limit: 3, BeforeID: page1[2].ID})
		eq("page 2", page2, "payment.create/pay-1")
		if more {
			t.Error("last page must not report more rows")
		}

		rows, _, _ = List(ctx, tx, otherSchool, Filter{})
		eq("other school", rows, "payment.create/pay-1")
	})
}
