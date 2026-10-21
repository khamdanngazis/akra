package grades

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/academic"
	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	reportConfigPath = "/pengaturan/rapor"
	pageReportConfig = "settings/report"
	maxConfigForm    = 8 << 10
)

// ReportConfigHandler melayani halaman pengaturan rapor.
type ReportConfigHandler struct {
	svc      *ReportConfigService
	db       database.DBTX
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewReportConfigHandler mengembalikan ReportConfigHandler. Route-nya harus
// berada di dalam grup RequireLogin.
func NewReportConfigHandler(db database.DB, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *ReportConfigHandler {
	return &ReportConfigHandler{svc: NewReportConfigService(db), db: db, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route pengaturan rapor. Hanya admin_tu; guru dan peran
// lain 403.
func (h *ReportConfigHandler) Routes(r chi.Router) {
	r.Route(reportConfigPath, func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU))
		// Peran: admin_tu.
		r.Get("/", h.page)
		// Peran: admin_tu.
		r.Post("/", h.save)
	})
}

type formatOption struct {
	Value, Label, Hint string
}

var formatOptions = []formatOption{
	{academic.ReportFormatNumeric, "Angka (K13/KTSP)", "Nilai akhir, huruf, dan KKM per mapel."},
	{academic.ReportFormatMerdeka, "Kurikulum Merdeka", "Ambang disebut KKTP. Deskripsi capaian per siswa belum tersedia; rapor sementara tetap menampilkan nilai dan predikat."},
}

type bandRow struct {
	Index int
	BandInput
	Error string
}

type reportConfigView struct {
	User    auth.Identity
	Form    ReportConfigForm
	Bands   []bandRow
	Formats []formatOption
	Errors  map[string]string
	Broken  bool // pengaturan tersimpan rusak; form diisi default
	Flash   string
	Error   string
}

func (h *ReportConfigHandler) page(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	cfg, err := LoadReportConfig(r.Context(), h.db, id.SchoolID)
	broken := false
	if err != nil {
		if !errors.Is(err, ErrInvalidScale) && !errors.Is(err, ErrScaleCoverage) && !errors.Is(err, ErrScaleGap) && !errors.Is(err, ErrScaleOverlap) {
			h.serverError(w, r, "report config", err)
			return
		}
		cfg, broken = ReportConfig{Format: academic.ReportFormatNumeric, Scale: DefaultScale(), KKMDefault: academic.DefaultKKM, SignerTitle: DefaultSignerTitle}, true
	}
	h.show(w, r, id, http.StatusOK, FormOf(cfg), nil, broken)
}

func (h *ReportConfigHandler) show(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, f ReportConfigForm, errs map[string]string, broken bool) {
	v := reportConfigView{User: id, Form: f, Formats: formatOptions, Errors: errs, Broken: broken}
	if errs == nil {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	} else {
		v.Error = "Pengaturan belum disimpan. Periksa isian yang ditandai."
	}
	for i := range MaxScaleRows {
		row := bandRow{Index: i}
		if i < len(f.Bands) {
			row.BandInput = f.Bands[i]
		}
		row.Error = errs["band_"+strconv.Itoa(i)]
		v.Bands = append(v.Bands, row)
	}
	h.render.Render(w, r, status, pageReportConfig, v)
}

func (h *ReportConfigHandler) save(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	p := r.PostForm
	f := ReportConfigForm{Format: p.Get("format"), KKMDefault: p.Get("kkm_default"), PrincipalName: p.Get("principal_name"), SignerTitle: p.Get("signer_title")}
	for i := range MaxScaleRows {
		k := strconv.Itoa(i)
		f.Bands = append(f.Bands, BandInput{Letter: p.Get("letter_" + k), Min: p.Get("min_" + k), Max: p.Get("max_" + k)})
	}
	changed, err := h.svc.Save(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, f)
	var ce *ConfigError
	switch {
	case errors.As(err, &ce):
		h.show(w, r, id, http.StatusUnprocessableEntity, f, ce.Fields, false)
		return
	case err != nil:
		h.serverError(w, r, "save report config", err)
		return
	}
	msg := "Pengaturan rapor tidak berubah."
	if changed {
		msg = "Pengaturan rapor disimpan. Huruf dan KKM di rekap nilai langsung memakai pengaturan baru."
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, reportConfigPath, http.StatusSeeOther)
}

func (h *ReportConfigHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}
