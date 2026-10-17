// Package auditview melayani halaman audit keuangan. Dipisah dari paket
// audit karena paket auth mengimpor audit (handler butuh auth).
package auditview

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
)

const (
	path     = "/keuangan/audit"
	pageName = "audit/index"
	pageSize = 50
)

// Handler melayani halaman audit keuangan.
type Handler struct {
	db     database.DBTX
	render page.Renderer
	logger *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(db database.DBTX, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{db: db, render: render, logger: logger}
}

// Routes mendaftarkan route halaman audit.
func (h *Handler) Routes(r chi.Router) {
	// Peran: kepala_sekolah, admin_tu. Bendahara tidak bisa membuka jejak
	// audit atas tindakannya sendiri.
	r.With(auth.RequireRole(auth.RoleKepalaSekolah, auth.RoleAdminTU)).Get(path, h.index)
}

type option struct {
	Value, Label string
	Selected     bool
}

type detail struct {
	Key, Value string
}

type row struct {
	Time, User, Action, ActionLabel, EntityType, EntityID string
	Before, After                                         []detail
}

type view struct {
	User    auth.Identity
	Actions []option
	Entity  string
	From    string
	To      string
	Rows    []row
	NextURL string
	Error   string
	Flash   string
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	q := r.URL.Query()
	v := view{User: id, Entity: strings.TrimSpace(q.Get("entity")), From: q.Get("dari"), To: q.Get("sampai")}
	f := audit.Filter{Actions: audit.FinanceActions, EntityID: v.Entity, Limit: pageSize}

	action := q.Get("aksi")
	if action != "" && !slices.Contains(audit.FinanceActions, action) {
		v.Error = "Jenis aksi tidak dikenal."
		action = ""
	}
	if action != "" {
		f.Actions = []string{action}
	}
	v.Actions = append(v.Actions, option{Value: "", Label: "Semua aksi keuangan", Selected: action == ""})
	for _, a := range audit.FinanceActions {
		v.Actions = append(v.Actions, option{Value: a, Label: audit.ActionLabels[a], Selected: a == action})
	}

	// Tanggal dibaca sebagai hari Asia/Jakarta; "sampai" termasuk harinya.
	if from, ok := parseDay(v.From); ok {
		f.From = from
	} else if v.From != "" {
		v.Error, v.From = "Tanggal tidak valid.", ""
	}
	if to, ok := parseDay(v.To); ok {
		f.To = to.AddDate(0, 0, 1)
	} else if v.To != "" {
		v.Error, v.To = "Tanggal tidak valid.", ""
	}
	if before, err := strconv.ParseInt(q.Get("sebelum"), 10, 64); err == nil && before > 0 {
		f.BeforeID = before
	}

	rows, more, err := audit.List(r.Context(), h.db, id.SchoolID, f)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list audit", err)
		return
	}
	for _, a := range rows {
		user := a.UserName
		if user == "" {
			user = "Sistem"
		}
		v.Rows = append(v.Rows, row{
			Time: formatTime(a.CreatedAt), User: user, Action: a.Action, ActionLabel: audit.ActionLabels[a.Action],
			EntityType: a.EntityType, EntityID: a.EntityID, Before: details(a.Before), After: details(a.After),
		})
	}
	if more {
		next := url.Values{}
		for _, k := range []string{"aksi", "entity", "dari", "sampai"} {
			if val := q.Get(k); val != "" {
				next.Set(k, val)
			}
		}
		next.Set("sebelum", strconv.FormatInt(rows[len(rows)-1].ID, 10))
		v.NextURL = path + "?" + next.Encode()
	}
	h.render.Render(w, r, http.StatusOK, pageName, v)
}

func parseDay(s string) (time.Time, bool) {
	d, err := time.ParseInLocation("2006-01-02", s, clock.Jakarta)
	return d, err == nil
}

var monthsShort = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// formatTime memformat waktu di Asia/Jakarta, mis. "17 Okt 2026 10.05 WIB".
func formatTime(t time.Time) string {
	t = t.In(clock.Jakarta)
	return fmt.Sprintf("%d %s %d %02d.%02d WIB", t.Day(), monthsShort[t.Month()-1], t.Year(), t.Hour(), t.Minute())
}

// moneyKeys adalah kunci yang nilainya rupiah.
var moneyKeys = []string{"amount", "total", "threshold", "default_amount"}

// details mengubah objek JSON audit menjadi daftar kunci-nilai terurut.
func details(m map[string]any) []detail {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]detail, 0, len(keys))
	for _, k := range keys {
		out = append(out, detail{Key: k, Value: formatValue(k, m[k])})
	}
	return out
}

func formatValue(key string, v any) string {
	switch x := v.(type) {
	case nil:
		return "—"
	case float64:
		if x == float64(int64(x)) {
			for _, mk := range moneyKeys {
				if strings.Contains(key, mk) {
					return money.FormatRupiah(int64(x))
				}
			}
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "ya"
		}
		return "tidak"
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}
