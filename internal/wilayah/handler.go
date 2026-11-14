package wilayah

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"akra/internal/platform/database"
)

// Handler melayani daftar wilayah untuk isian alamat. Publik (formulir
// pendaftaran tanpa login) dan tidak memuat data pribadi; datanya sama
// untuk semua sekolah sehingga boleh di-cache browser.
type Handler struct {
	db     database.DBTX
	logger *slog.Logger
}

// NewHandler mengembalikan Handler.
func NewHandler(db database.DBTX, logger *slog.Logger) *Handler {
	return &Handler{db: db, logger: logger}
}

// Routes mendaftarkan route wilayah ke r.
func (h *Handler) Routes(r chi.Router) {
	// Publik: daftar wilayah di bawah ?induk= (kosong = provinsi).
	r.Get("/wilayah", h.children)
}

func (h *Handler) children(w http.ResponseWriter, r *http.Request) {
	induk := strings.TrimSpace(r.URL.Query().Get("induk"))
	if induk != "" && (!ValidKode(induk) || TingkatOf(induk) >= Desa) {
		http.Error(w, `{"error":"induk tidak valid"}`, http.StatusBadRequest)
		return
	}
	list, err := Children(r.Context(), h.db, induk)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list wilayah", "error", err)
		http.Error(w, `{"error":"server"}`, http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []Region{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_ = json.NewEncoder(w).Encode(list)
}
