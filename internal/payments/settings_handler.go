package payments

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
	"akra/internal/platform/settings"
	"akra/internal/school"
)

const (
	settingsPath = "/pengaturan/keuangan"
	pageSettings = "payments/settings"
	// ActionSettingsUpdate adalah aksi audit perubahan pengaturan keuangan.
	ActionSettingsUpdate = "settings.update"
)

// FinanceSettings adalah pengaturan keuangan sekolah.
type FinanceSettings struct {
	// VoidApprovalThreshold dalam rupiah; IsSet false berarti belum diatur
	// (DefaultVoidApprovalThreshold berlaku).
	VoidApprovalThreshold int64
	IsSet                 bool
}

// GetFinanceSettings membaca pengaturan keuangan sekolah.
func GetFinanceSettings(ctx context.Context, q database.DBTX, schoolID string) (FinanceSettings, error) {
	v, ok, err := settings.Get(ctx, q, schoolID, settings.VoidApprovalThreshold)
	if err != nil || !ok {
		return FinanceSettings{VoidApprovalThreshold: DefaultVoidApprovalThreshold}, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return FinanceSettings{}, errors.New("payments: stored void approval threshold is not an integer")
	}
	return FinanceSettings{VoidApprovalThreshold: n, IsSet: true}, nil
}

// ErrInvalidThreshold dikembalikan untuk ambang yang bukan nominal rupiah valid.
var ErrInvalidThreshold = errors.New("payments: invalid void approval threshold")

// UpdateFinanceSettings menyimpan ambang persetujuan pembatalan (teks
// rupiah, mis. "500.000") beserta audit settings.update dalam satu transaksi.
func UpdateFinanceSettings(ctx context.Context, db database.DB, actor school.Actor, threshold string) (FinanceSettings, error) {
	n, err := money.ParseRupiah(threshold)
	if err != nil || n < 0 || n > billing.MaxAmount {
		return FinanceSettings{}, ErrInvalidThreshold
	}
	var out FinanceSettings
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		before, err := GetFinanceSettings(ctx, tx, actor.SchoolID)
		if err != nil {
			return err
		}
		if err := settings.Set(ctx, tx, actor.SchoolID, settings.VoidApprovalThreshold, strconv.FormatInt(n, 10)); err != nil {
			return err
		}
		out = FinanceSettings{VoidApprovalThreshold: n, IsSet: true}
		var beforeVal any
		if before.IsSet {
			beforeVal = map[string]any{"void_approval_threshold": before.VoidApprovalThreshold}
		}
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionSettingsUpdate, EntityType: "school_settings", EntityID: actor.SchoolID,
			Before: beforeVal, After: map[string]any{"void_approval_threshold": n},
		})
	})
	return out, err
}

type settingsView struct {
	User      auth.Identity
	Settings  FinanceSettings
	Current   string
	Threshold string // isian form
	CanEdit   bool
	Errors    map[string]string
	Error     string
	Flash     string
}

func (h *Handler) financeSettings(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderSettings(w, r, id, http.StatusOK, "", nil)
}

func (h *Handler) renderSettings(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, input string, errs map[string]string) {
	s, err := GetFinanceSettings(r.Context(), h.svc.db, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "get finance settings", err)
		return
	}
	view := settingsView{
		User: id, Settings: s, Current: money.FormatRupiah(s.VoidApprovalThreshold),
		CanEdit: id.HasRole(auth.RoleKepalaSekolah), Errors: errs, Flash: auth.PopFlash(r.Context(), h.sessions),
		Threshold: input,
	}
	if input == "" && s.IsSet {
		view.Threshold = money.Group(s.VoidApprovalThreshold)
	}
	h.render.Render(w, r, status, pageSettings, view)
}

func (h *Handler) updateFinanceSettings(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	input := strings.TrimSpace(r.PostForm.Get("void_approval_threshold"))
	s, err := UpdateFinanceSettings(r.Context(), h.svc.db, actor(r, id), input)
	if errors.Is(err, ErrInvalidThreshold) {
		h.renderSettings(w, r, id, http.StatusBadRequest, input, map[string]string{
			"void_approval_threshold": "Isi nominal rupiah 0 atau lebih, mis. 500000 atau 500.000.",
		})
		return
	}
	if err != nil {
		h.serverError(w, r, "update finance settings", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Ambang persetujuan pembatalan diubah menjadi "+money.FormatRupiah(s.VoidApprovalThreshold)+".")
	http.Redirect(w, r, settingsPath, http.StatusSeeOther)
}
