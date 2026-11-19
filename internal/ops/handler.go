package ops

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/exam"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/metrics"
	"akra/internal/platform/page"
)

// Handler melayani halaman status publik, /metrics, dan /internal untuk
// tim AKRA.
type Handler struct {
	DB       database.DB
	Metrics  *metrics.Registry
	Render   page.Renderer
	Logger   *slog.Logger
	Notifier Notifier
	// Token melindungi /metrics dan /internal (Basic auth dengan nama
	// bebas, atau Authorization: Bearer). Kosong: route itu 404.
	Token string
	Env   string
}

// Routes mendaftarkan route. Tanpa sesi dan tanpa CSRF: /status publik;
// route lain hanya dengan token (POST hanya dengan header Bearer, bukan
// Basic auth yang dikirim browser otomatis).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/status", h.status)
	r.Group(func(r chi.Router) {
		r.Use(h.requireToken(false))
		r.Get("/metrics", h.Metrics.Handler().ServeHTTP)
		r.Get("/internal/", h.dashboard)
		r.Get("/internal/active-exams", h.activeExams)
	})
	r.With(h.requireToken(true)).Post("/internal/alert-test", h.alertTest)
}

// requireToken memeriksa token tim. bearerOnly: Basic auth tidak
// diterima (aksi yang mengubah sesuatu).
func (h *Handler) requireToken(bearerOnly bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h.Token == "" {
				http.NotFound(w, r)
				return
			}
			given := ""
			if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
				given = v
			} else if _, pw, ok := r.BasicAuth(); ok && !bearerOnly {
				given = pw
			}
			if subtle.ConstantTimeCompare([]byte(given), []byte(h.Token)) != 1 {
				if !bearerOnly {
					w.Header().Set("WWW-Authenticate", `Basic realm="AKRA internal", charset="UTF-8"`)
				}
				h.Logger.WarnContext(r.Context(), "internal access denied", "path", r.URL.Path)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Robots-Tag", "noindex")
			next.ServeHTTP(w, r)
		})
	}
}

type statusView struct {
	Status
	Now       time.Time
	Zone      *time.Location
	Updated   string
	Windows   []string
	HasIssues bool
}

// statusZone: halaman status memakai WIB (sekolah di zona lain melihat
// keterangan zona).
var statusZone = clock.Location("Asia/Jakarta")

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	st, err := LoadStatus(r.Context(), h.DB, now)
	if err != nil {
		page.ServerError(h.Render, h.Logger, w, r, nil, "load status", err)
		return
	}
	v := statusView{Status: st, Now: now, Updated: now.In(statusZone).Format("15.04") + " WIB"}
	for _, m := range st.Maintenance {
		a, b := m.StartsAt.In(statusZone), m.EndsAt.In(statusZone)
		v.Windows = append(v.Windows, clock.FormatDateID(a)+" "+a.Format("15.04")+"–"+b.Format("15.04")+" WIB: "+m.Message)
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Refresh", "60")
	h.Render.Render(w, r, http.StatusOK, "ops/status", v)
}

func (h *Handler) activeExams(w http.ResponseWriter, r *http.Request) {
	a, err := exam.LoadActiveExams(r.Context(), h.DB, time.Now(), exam.DeployLead)
	if err != nil {
		h.Logger.ErrorContext(r.Context(), "load active exams", "error", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	if a.Sessions == nil {
		a.Sessions = []exam.ActiveSession{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"active_sessions": a.Active, "upcoming_sessions": a.Upcoming,
		"running_attempts": a.RunningAttempts, "deploy_ok": !a.Blocking(), "sessions": a.Sessions})
}

type seasonRow struct {
	exam.SeasonDay
	DayLabel string
	Window   string
}

type dashboardView struct {
	Env       string
	Now       string
	Active    exam.ActiveExams
	ActiveRow []activeRow
	Season    []seasonRow
	Incidents []Incident
	Status    Status
	Save      metrics.Window
	Login     metrics.Window
	Submit    metrics.Window
	Backlog   int
	Oldest    time.Duration
}

type activeRow struct {
	exam.ActiveSession
	When string
}

func wib(t time.Time) string { return t.In(statusZone).Format("15.04") }

// dashboard adalah dasbor internal: ujian aktif (boleh deploy atau tidak),
// kejadian, metrik singkat, dan jadwal musim ujian 60 hari.
func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx, now := r.Context(), time.Now()
	v := dashboardView{Env: h.Env, Now: clock.FormatDateID(now.In(statusZone)) + " " + wib(now) + " WIB",
		Save: h.Metrics.Recent(EndpointSave, 10*time.Minute), Login: h.Metrics.Recent(EndpointLogin, 10*time.Minute),
		Submit: h.Metrics.Recent(EndpointSubmit, 10*time.Minute)}
	var err error
	fail := func(op string) { page.ServerError(h.Render, h.Logger, w, r, nil, op, err) }
	if v.Active, err = exam.LoadActiveExams(ctx, h.DB, now, exam.DeployLead); err != nil {
		fail("active exams")
		return
	}
	for _, s := range v.Active.Sessions {
		v.ActiveRow = append(v.ActiveRow, activeRow{ActiveSession: s, When: wib(s.StartsAt) + "–" + wib(s.EndsAt)})
	}
	if v.Status, err = LoadStatus(ctx, h.DB, now); err != nil {
		fail("status")
		return
	}
	if v.Incidents, err = ListIncidents(ctx, h.DB, now.Add(-7*24*time.Hour)); err != nil {
		fail("incidents")
		return
	}
	if v.Backlog, v.Oldest, err = exam.AutoSubmitBacklog(ctx, h.DB, now); err != nil {
		fail("backlog")
		return
	}
	y, m, d := now.In(statusZone).Date()
	season, err := exam.LoadExamSeason(ctx, h.DB, time.Date(y, m, d, 0, 0, 0, 0, statusZone), 60)
	if err != nil {
		fail("season")
		return
	}
	for _, s := range season {
		v.Season = append(v.Season, seasonRow{SeasonDay: s, DayLabel: clock.FormatDateID(s.Day), Window: wib(s.FirstStart) + "–" + wib(s.LastEnd)})
	}
	h.Render.Render(w, r, http.StatusOK, "ops/internal", v)
}

// alertTest mengirim pesan uji ke kanal alert (memastikan token bot dan
// chat benar).
func (h *Handler) alertTest(w http.ResponseWriter, r *http.Request) {
	if h.Notifier == nil {
		http.Error(w, "no alert channel configured", http.StatusConflict)
		return
	}
	if err := h.Notifier.Notify(r.Context(), "🧪 Uji alert AKRA ("+h.Env+"). Kanal alert berfungsi."); err != nil {
		h.Logger.ErrorContext(r.Context(), "alert test", "error", err)
		http.Error(w, "send failed", http.StatusBadGateway)
		return
	}
	_, _ = w.Write([]byte("sent\n"))
}
