package audit

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/platform/database"
)

func validEntry() Entry {
	return Entry{SchoolID: "00000000-0000-0000-0000-000000000001", Action: "invoice.create", EntityType: "invoice", EntityID: "inv-1"}
}

func TestRecord_Validation(t *testing.T) {
	cases := map[string]func(*Entry){
		"action":      func(e *Entry) { e.Action = "" },
		"school_id":   func(e *Entry) { e.SchoolID = "" },
		"entity_type": func(e *Entry) { e.EntityType = "  " },
		"entity_id":   func(e *Entry) { e.EntityID = "" },
	}
	for field, mutate := range cases {
		e := validEntry()
		mutate(&e)
		// tx nil cukup: validasi gagal sebelum database dipakai.
		err := Record(context.Background(), nil, e)
		if !errors.Is(err, ErrInvalidEntry) || !strings.Contains(err.Error(), field) {
			t.Errorf("missing %s: error = %v, want ErrInvalidEntry mentioning %s", field, err, field)
		}
	}
}

func TestRecord_RejectsSensitiveKeys(t *testing.T) {
	cases := []any{
		map[string]any{"email": "a@b.id", "password_hash": "$argon2id$..."},
		map[string]any{"user": map[string]any{"ResetToken": "abc"}},
		[]any{map[string]any{"api_key": "re_x"}},
		struct {
			Name   string
			Secret string `json:"client_secret"`
		}{"x", "y"},
	}
	for _, v := range cases {
		e := validEntry()
		e.After = v
		if err := Record(context.Background(), nil, e); !errors.Is(err, ErrInvalidEntry) {
			t.Errorf("After = %#v: error = %v, want ErrInvalidEntry", v, err)
		}
	}
}

func TestRecord_UnencodableValue(t *testing.T) {
	e := validEntry()
	e.Before = map[string]any{"ch": make(chan int)}
	if err := Record(context.Background(), nil, e); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("error = %v, want ErrInvalidEntry", err)
	}
}

// testDB membuka pool ke TEST_DATABASE_URL, atau melewati uji jika kosong.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := database.Connect(context.Background(), dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// inTx menjalankan fn dalam transaksi yang selalu di-rollback, agar uji
// tidak meninggalkan baris audit (yang memang tidak bisa dihapus).
func inTx(t *testing.T, pool *pgxpool.Pool, fn func(tx pgx.Tx, schoolID string)) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var schoolID string
	code := "test-audit-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	if len(code) > 32 {
		code = code[:32]
	}
	code = strings.Trim(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, code), "-")
	if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('Sekolah Audit', $1) RETURNING id`, code).Scan(&schoolID); err != nil {
		t.Fatal(err)
	}
	fn(tx, schoolID)
}

func TestRecord_StoresJSON(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	inTx(t, pool, func(tx pgx.Tx, schoolID string) {
		e := Entry{
			SchoolID:   schoolID,
			UserID:     "11111111-1111-1111-1111-111111111111",
			Action:     "invoice.update",
			EntityType: "invoice",
			EntityID:   "inv-42",
			Before:     map[string]any{"amount": 150000, "status": "belum_bayar"},
			After: struct {
				Amount int64  `json:"amount"`
				Status string `json:"status"`
			}{175000, "sebagian"},
			IP: netip.MustParseAddr("203.0.113.5"),
		}
		if err := Record(ctx, tx, e); err != nil {
			t.Fatalf("Record() error = %v", err)
		}

		var (
			userID, action, entityType, entityID, ip string
			before, after                            []byte
			createdAt                                time.Time
		)
		err := tx.QueryRow(ctx, `
			SELECT user_id::text, action, entity_type, entity_id, before, after, host(ip), created_at
			FROM audit_logs WHERE school_id = $1`, schoolID).
			Scan(&userID, &action, &entityType, &entityID, &before, &after, &ip, &createdAt)
		if err != nil {
			t.Fatal(err)
		}
		if userID != e.UserID || action != "invoice.update" || entityType != "invoice" || entityID != "inv-42" || ip != "203.0.113.5" {
			t.Errorf("row = (%s, %s, %s, %s, %s)", userID, action, entityType, entityID, ip)
		}
		assertJSON(t, "before", before, `{"amount": 150000, "status": "belum_bayar"}`)
		assertJSON(t, "after", after, `{"amount": 175000, "status": "sebagian"}`)
		if time.Since(createdAt) > time.Minute {
			t.Errorf("created_at = %v", createdAt)
		}
	})
}

func TestRecord_SystemActionStoresNulls(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	inTx(t, pool, func(tx pgx.Tx, schoolID string) {
		e := Entry{SchoolID: schoolID, Action: "invoice.mark_overdue", EntityType: "invoice", EntityID: "inv-7"}
		if err := Record(ctx, tx, e); err != nil {
			t.Fatal(err)
		}
		var nullUser, nullBefore, nullAfter, nullIP bool
		err := tx.QueryRow(ctx, `SELECT user_id IS NULL, before IS NULL, after IS NULL, ip IS NULL FROM audit_logs WHERE school_id = $1`, schoolID).
			Scan(&nullUser, &nullBefore, &nullAfter, &nullIP)
		if err != nil {
			t.Fatal(err)
		}
		if !nullUser || !nullBefore || !nullAfter || !nullIP {
			t.Errorf("NULL flags = user %v, before %v, after %v, ip %v; want all true", nullUser, nullBefore, nullAfter, nullIP)
		}
	})
}

func TestRecord_RollbackLeavesNoRow(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	// Sekolah dibuat dan di-commit terpisah agar yang di-rollback hanya aksi
	// utama beserta auditnya.
	var schoolID string
	if err := pool.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('Sekolah Rollback', 'test-audit-rollback') ON CONFLICT (code) DO UPDATE SET name = excluded.name RETURNING id`).Scan(&schoolID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM schools WHERE id = $1`, schoolID) })
	entityID := "rollback-" + time.Now().Format("20060102150405.000000000")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, tx, Entry{SchoolID: schoolID, Action: "payment.create", EntityType: "payment", EntityID: entityID}); err != nil {
		t.Fatal(err)
	}
	// Aksi utama gagal, transaksi di-rollback.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE entity_type = 'payment' AND entity_id = $1`, entityID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d audit rows remain after rollback, want 0", n)
	}
}

func TestAuditLogs_AppendOnly(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	for _, stmt := range []string{
		`UPDATE audit_logs SET action = 'tampered' WHERE school_id = $1`,
		`DELETE FROM audit_logs WHERE school_id = $1`,
		`TRUNCATE audit_logs`,
	} {
		inTx(t, pool, func(tx pgx.Tx, schoolID string) {
			if err := Record(ctx, tx, Entry{SchoolID: schoolID, Action: "student.update", EntityType: "student", EntityID: "s-1"}); err != nil {
				t.Fatal(err)
			}
			args := []any{schoolID}
			if strings.HasPrefix(stmt, "TRUNCATE") {
				args = nil
			}
			_, err := tx.Exec(ctx, stmt, args...)
			if err == nil || !strings.Contains(err.Error(), "append-only") {
				t.Errorf("%s: error = %v, want append-only rejection", strings.Fields(stmt)[0], err)
			}
		})
	}
}

func assertJSON(t *testing.T, name string, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s is not JSON: %s", name, got)
	}
	_ = json.Unmarshal([]byte(want), &w)
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Errorf("%s = %s, want %s", name, gb, wb)
	}
}
