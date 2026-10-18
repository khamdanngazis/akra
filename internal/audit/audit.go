// Package audit mencatat jejak perubahan data ke tabel audit_logs.
//
// Record menerima transaksi milik aksi utama, sehingga baris audit ikut
// di-rollback bila aksi utama gagal. Tabel audit_logs bersifat append-only:
// database menolak UPDATE, DELETE, dan TRUNCATE (migrasi 000007).
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ErrInvalidEntry dikembalikan jika Entry tidak lengkap atau berisi data
// yang tidak boleh dicatat.
var ErrInvalidEntry = errors.New("audit: invalid entry")

// Entry adalah satu baris audit.
type Entry struct {
	SchoolID string // wajib
	UserID   string // kosong untuk aksi sistem
	// Action berformat "<entitas>.<aksi>", mis. "invoice.create" atau
	// "payment.void". Wajib.
	Action     string
	EntityType string // wajib, mis. "invoice"
	EntityID   string // wajib
	// Before dan After berisi keadaan data sebelum dan sesudah aksi. Nilai
	// apa pun yang bisa di-encode ke JSON; nil disimpan sebagai NULL.
	// Jangan isi dengan password, hash, atau token.
	Before any
	After  any
	IP     netip.Addr // zero value disimpan sebagai NULL
}

// sensitiveKeys adalah potongan nama kunci JSON yang ditolak di Before dan
// After, agar rahasia tidak sengaja masuk ke jejak audit.
var sensitiveKeys = []string{"password", "token", "secret", "api_key", "apikey"}

// addressKeys adalah potongan nama kunci alamat. Alamat (data pribadi siswa
// dan wali) ditolak jika nilainya teks; tanda seperti has_address (boolean)
// tetap boleh.
var addressKeys = []string{"address", "alamat"}

// Record menyimpan e di dalam transaksi tx.
func Record(ctx context.Context, tx pgx.Tx, e Entry) error {
	if err := validate(e); err != nil {
		return err
	}
	before, err := encode("before", e.Before)
	if err != nil {
		return err
	}
	after, err := encode("after", e.After)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs (school_id, user_id, action, entity_type, entity_id, before, after, ip)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.SchoolID, nullIfEmpty(e.UserID), e.Action, e.EntityType, e.EntityID, before, after, inet(e.IP))
	if err != nil {
		return fmt.Errorf("audit: insert %s: %w", e.Action, err)
	}
	return nil
}

func validate(e Entry) error {
	var missing []string
	for _, f := range []struct{ name, value string }{
		{"school_id", e.SchoolID},
		{"action", e.Action},
		{"entity_type", e.EntityType},
		{"entity_id", e.EntityID},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: missing %s", ErrInvalidEntry, strings.Join(missing, ", "))
	}
	return nil
}

// encode mengubah v menjadi JSON untuk kolom jsonb, atau nil (NULL) jika v
// nil. Kunci yang tampak sensitif ditolak.
func encode(field string, v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: encode %s: %v", ErrInvalidEntry, field, err)
	}
	if string(b) == "null" {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return nil, fmt.Errorf("%w: decode %s: %v", ErrInvalidEntry, field, err)
	}
	if key, ok := findSensitiveKey(decoded); ok {
		return nil, fmt.Errorf("%w: %s contains sensitive key %q", ErrInvalidEntry, field, key)
	}
	return b, nil
}

// findSensitiveKey mencari kunci sensitif di objek JSON bersarang.
func findSensitiveKey(v any) (string, bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			lk := strings.ToLower(k)
			for _, s := range sensitiveKeys {
				if strings.Contains(lk, s) {
					return k, true
				}
			}
			if _, isText := child.(string); isText {
				for _, s := range addressKeys {
					if strings.Contains(lk, s) {
						return k, true
					}
				}
			}
			if key, ok := findSensitiveKey(child); ok {
				return key, true
			}
		}
	case []any:
		for _, child := range x {
			if key, ok := findSensitiveKey(child); ok {
				return key, true
			}
		}
	}
	return "", false
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func inet(ip netip.Addr) any {
	if !ip.IsValid() {
		return nil
	}
	return ip
}
